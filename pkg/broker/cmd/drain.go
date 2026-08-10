/*
Copyright 2026 The Kynoproj Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

// inflightMetricName is the inflight metric name exposed by the broker.
const inflightMetricName = "broker_inflight_requests"

// DrainConfig configures the preStop drain. Zero values fall back to defaults.
type DrainConfig struct {
	// IntrospectionPort is the broker's introspection TLS port serving /metrics.
	IntrospectionPort int
	// PropagationDelay is a fixed initial wait that lets Kubernetes finish
	// removing this pod from Service endpoints before we start checking, so no
	// new request lands after we begin draining. It is NOT tied to request
	// duration — it only covers endpoint-propagation lag.
	PropagationDelay time.Duration
	// Budget bounds the whole drain (propagation + poll). Must stay within the
	// pod's terminationGracePeriodSeconds (minus the post-SIGTERM shutdown), or
	// the kubelet SIGKILLs mid-drain anyway.
	Budget time.Duration
	// PollInterval is how often to re-check in-flight while waiting.
	PollInterval time.Duration
}

// drainPollInterval is how often the preStop drain re-checks in-flight. Fixed
// cadence (not derived from the grace period): a couple seconds balances
// responsiveness against scrape overhead.
const drainPollInterval = 2 * time.Second

// RunDrain is the entrypoint for the `drain` preStop subcommand. The
// propagation and budget are derived from the pod's terminationGracePeriodSeconds
// (injected as an env var) so there is a single knob. It sets up logging and
// runs Drain to completion. It never fails the hook: a drain that times out is
// expected behavior (the post-SIGTERM shutdown handles residue), so the preStop
// exits 0 regardless.
func RunDrain(introspectionPort int) {
	logger := logging.WithAgentLabels(logging.NewLogger().Named("broker-drain"))
	// No signal handling on purpose: preStop runs before SIGTERM is delivered,
	// so this process should drain for its full budget, not exit on SIGTERM.
	ctx := logging.WithLogger(context.Background(), logger)
	b := resolveBudgets()
	Drain(ctx, DrainConfig{
		IntrospectionPort: introspectionPort,
		PropagationDelay:  b.Propagation,
		Budget:            b.Drain,
		PollInterval:      drainPollInterval,
	})
}

// Drain runs the preStop drain: wait out endpoint propagation, then poll the
// broker's own /metrics until in-flight hits 0 or the budget elapses. It always
// returns nil error semantics for the caller (a drain that times out is not a
// failure — SIGTERM + the post-SIGTERM shutdown handle the residue); errors are
// logged. The returned bool reports whether the pod reached zero in-flight.
//
// Limitation: broker_inflight_requests counts a streaming call (gRPC stream or
// SSE) as in-flight for its entire lifetime, so a long-lived stream keeps the
// gauge above zero and this drain waits out the full budget before terminating.
func Drain(ctx context.Context, cfg DrainConfig) bool {
	logger := logging.FromContext(ctx)

	ctx, cancel := context.WithTimeout(ctx, cfg.Budget)
	defer cancel()

	logger.Infow("Drain: waiting for endpoint propagation",
		zap.Duration("propagationDelay", cfg.PropagationDelay))
	if !sleepCtx(ctx, cfg.PropagationDelay) {
		logger.Infow("Drain: budget elapsed during propagation wait")
		return false
	}

	url := fmt.Sprintf("https://localhost:%d/metrics", cfg.IntrospectionPort)
	client := insecureClient()

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		inflight, err := scrapeInflight(ctx, client, url)
		if err != nil {
			// If our own budget elapsed, that's a timeout, not a clean drain.
			if ctx.Err() != nil {
				logger.Warnw("Drain: budget elapsed before draining", zap.Error(ctx.Err()))
				return false
			}
			// Otherwise the broker most likely already stopped serving /metrics
			// — nothing left to drain.
			logger.Infow("Drain: metrics scrape failed, assuming drained", zap.Error(err))
			return true
		}
		if inflight <= 0 {
			logger.Infow("Drain: no in-flight requests, proceeding to terminate")
			return true
		}
		logger.Infow("Drain: waiting for in-flight requests to finish",
			zap.Float64("inflight", inflight))
		select {
		case <-ctx.Done():
			logger.Warnw("Drain: budget elapsed with requests still in flight",
				zap.Float64("inflight", inflight))
			return false
		case <-ticker.C:
		}
	}
}

// insecureClient returns an HTTP client that skips TLS verification.
func insecureClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // localhost self-signed
	}}
}

// scrapeInflight fetches /metrics and sums broker_inflight_requests across all
// transport labels.
func scrapeInflight(ctx context.Context, client *http.Client, url string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics returned status %d", resp.StatusCode)
	}
	return sumInflight(resp.Body)
}

// sumInflight scans Prometheus text-format metrics and sums every
// broker_inflight_requests sample (across all transport labels).
// Samples look like:
//
//	broker_inflight_requests{transport="jsonrpc"} 3
//
// An absent metric yields 0 (nothing in flight).
func sumInflight(r io.Reader) (float64, error) {
	var total float64
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, inflightMetricName) {
			continue
		}
		// The value is the last whitespace-separated field on the line.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse inflight value %q: %w", fields[len(fields)-1], err)
		}
		total += v
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("read metrics: %w", err)
	}
	return total, nil
}

// sleepCtx waits for d or ctx cancellation, returning false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
