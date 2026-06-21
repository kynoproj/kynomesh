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

package rater

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	sharedqueue "github.com/kynoproj/kynomesh/pkg/shared/queue"
)

// Default scrape parameters. These are intentionally not exposed as
// env vars — they are part of the daemon contract.
const (
	DefaultScrapeInterval = 5 * time.Second
	DefaultScrapeTimeout  = 1 * time.Second
	DefaultScrapeWorkers  = 32
)

// Fixed lookback windows that GetMetrics always reports when data is
// available. The caller can additionally request a "custom" window
// via lookbackSeconds.
const (
	Lookback1m  = int64(60)
	Lookback5m  = int64(300)
	Lookback15m = int64(900)
)

// Window key strings exposed in the gRPC response.
const (
	WindowKey1m     = "1m"
	WindowKey5m     = "5m"
	WindowKey15m    = "15m"
	WindowKeyCustom = "custom"
)

// ErrUnknownAgentDeploy is returned by GetMetrics when the name is
// not in the configured list. The gRPC layer maps this to
// codes.NotFound.
var ErrUnknownAgentDeploy = errors.New("unknown AgentDeploy")

// ErrNoData is returned when the requested AgentDeploy is known but
// the rater holds insufficient samples (fewer than 2 buckets) to
// compute a rate. The gRPC layer maps this to codes.Unavailable.
var ErrNoData = errors.New("not enough samples to compute metrics")

// Scraper is the minimal surface the rater needs from a pod-metrics
// scraper. Implemented by pkg/daemon/scraper.Scraper.
type Scraper interface {
	Scrape(ctx context.Context, host string) (*PodSample, error)
}

// DiscoverFunc resolves the live pod DNS hostnames for an
// AgentDeploy. Implemented by pkg/daemon/discovery.Discover bound to
// a namespace and resolver.
type DiscoverFunc func(ctx context.Context, agentDeploy string) ([]string, error)

// Clock is a test seam. Production passes time.Now.
type Clock func() time.Time

// Options configures a Rater. Zero-value fields take the package
// defaults.
type Options struct {
	AgentSet     string
	AgentDeploys []string
	Namespace    string
	Scraper      Scraper
	Discover     DiscoverFunc
	Logger       *zap.SugaredLogger

	ScrapeInterval time.Duration // default DefaultScrapeInterval
	ScrapeWorkers  int           // default DefaultScrapeWorkers
	Clock          Clock         // default time.Now
}

// Rater maintains per-AgentDeploy ring buffers fed by periodic
// scrapes. It is the only stateful component of the daemon.
type Rater struct {
	opts    Options
	buffers map[string]*sharedqueue.OverflowQueue[*TimestampedCounts]

	// Self-observability counters; nil-safe checks let tests skip
	// wiring metrics.
	selfMetrics *SelfMetrics
}

// NewRater builds a Rater. Pre-allocates one ring buffer per
// configured AgentDeploy so reads never race with writes when the
// caller hasn't seen the AgentDeploy yet.
func NewRater(opts Options) *Rater {
	if opts.ScrapeInterval == 0 {
		opts.ScrapeInterval = DefaultScrapeInterval
	}
	if opts.ScrapeWorkers == 0 {
		opts.ScrapeWorkers = DefaultScrapeWorkers
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop().Sugar()
	}
	buffers := make(map[string]*sharedqueue.OverflowQueue[*TimestampedCounts], len(opts.AgentDeploys))
	for _, ad := range opts.AgentDeploys {
		buffers[ad] = sharedqueue.New[*TimestampedCounts](MaxBuckets)
	}
	return &Rater{opts: opts, buffers: buffers}
}

// WithSelfMetrics wires the daemon's own /metrics counters. Optional.
func (r *Rater) WithSelfMetrics(m *SelfMetrics) *Rater {
	r.selfMetrics = m
	return r
}

// Start runs scrape ticks until ctx is cancelled. It does not return
// errors mid-loop — per-tick failures are logged and counted, the
// loop continues. The caller treats ctx.Done as the shutdown signal.
func (r *Rater) Start(ctx context.Context) {
	log := r.opts.Logger
	log.Infow("Rater starting",
		zap.String("agentSet", r.opts.AgentSet),
		zap.Strings("agentDeploys", r.opts.AgentDeploys),
		zap.Duration("scrapeInterval", r.opts.ScrapeInterval))

	ticker := time.NewTicker(r.opts.ScrapeInterval)
	defer ticker.Stop()

	// First tick immediately so warmup doesn't wait a full interval.
	r.scrapeAllOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			log.Infow("Rater shutting down")
			return
		case <-ticker.C:
			r.scrapeAllOnce(ctx)
		}
	}
}

// scrapeAllOnce fans out per-AgentDeploy scrape passes in parallel.
// Each AgentDeploy's pods are scraped concurrently up to ScrapeWorkers.
func (r *Rater) scrapeAllOnce(ctx context.Context) {
	var wg sync.WaitGroup
	for _, ad := range r.opts.AgentDeploys {
		wg.Add(1)
		go func(ad string) {
			defer wg.Done()
			r.scrapeOneAgentDeploy(ctx, ad)
		}(ad)
	}
	wg.Wait()
}

// scrapeOneAgentDeploy resolves the AgentDeploy's pods, scrapes them
// in parallel, and updates the bucket at the current aligned ts.
func (r *Rater) scrapeOneAgentDeploy(ctx context.Context, ad string) {
	log := r.opts.Logger.With(zap.String("agentDeploy", ad))
	hosts, err := r.opts.Discover(ctx, ad)
	if err != nil {
		log.Warnw("Pod discovery failed", zap.Error(err))
		if r.selfMetrics != nil {
			r.selfMetrics.DiscoveryFailures.WithLabelValues(ad).Inc()
		}
		return
	}
	if len(hosts) == 0 {
		// No ready pods — common during initial bring-up or
		// scaled-to-zero. Not an error, but worth recording a metric
		// so operators can see it.
		if r.selfMetrics != nil {
			r.selfMetrics.PodsObserved.WithLabelValues(ad).Set(0)
		}
		return
	}
	if r.selfMetrics != nil {
		r.selfMetrics.PodsObserved.WithLabelValues(ad).Set(float64(len(hosts)))
	}

	bucketTS := AlignBucket(r.opts.Clock().Unix())

	sem := make(chan struct{}, r.opts.ScrapeWorkers)
	var wg sync.WaitGroup
	q, ok := r.buffers[ad]
	if !ok {
		// Defensive: should never happen given pre-allocation, but
		// avoids nil-map panic on a misconfigured rater.
		log.Errorw("No ring buffer for AgentDeploy")
		return
	}

	for _, host := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			sample, err := r.opts.Scraper.Scrape(ctx, host)
			if err != nil {
				log.Debugw("Scrape failed", zap.String("host", host), zap.Error(err))
				if r.selfMetrics != nil {
					r.selfMetrics.ScrapeFailures.WithLabelValues(ad).Inc()
				}
				// Important: do NOT call UpdateBucket with nil. The
				// existing pod entry stays untouched so the next
				// successful scrape produces a correct delta.
				return
			}
			if r.selfMetrics != nil {
				r.selfMetrics.ScrapeSuccess.WithLabelValues(ad).Inc()
			}
			UpdateBucket(q, bucketTS, host, sample)
		}(host)
	}
	wg.Wait()
}

// WindowedResult is the per-transport aggregate the rater returns to
// the gRPC layer. The Total field is the sum across all observed
// transports, separate from PerTransport so callers don't have to
// recompute it.
type WindowedResult struct {
	Total                    PerWindowValues
	PerTransport             map[string]PerWindowValues
	SamplesCount             int64
	PodsObservedAvg          int64
	CustomWindowEffectiveSec int64
}

// PerWindowValues holds the four configured windows. Each value is
// the daemon's computed rate / average for that window. An empty map
// is intentionally distinct from a map with zero values, so the gRPC
// layer can encode "no data" vs "data, value zero" precisely.
type PerWindowValues struct {
	ProcessingRates  map[string]float64
	InflightAverages map[string]float64
}

// GetMetrics computes all configured windows for the named
// AgentDeploy. A non-zero lookbackSeconds adds a "custom" window
// (clamped to retention).
//
// Returns:
//   - ErrUnknownAgentDeploy if the name is not in the configured
//     list.
//   - ErrNoData if the ring buffer holds fewer than 2 buckets.
//   - A WindowedResult otherwise. Empty per-window maps mean "data
//     exists but not enough to compute that specific window."
func (r *Rater) GetMetrics(name string, lookbackSeconds int64) (*WindowedResult, error) {
	q, ok := r.buffers[name]
	if !ok {
		return nil, ErrUnknownAgentDeploy
	}
	if !HasUsableHistory(q) {
		return nil, ErrNoData
	}

	now := AlignBucket(r.opts.Clock().Unix())
	transports := observedTransports(q)

	// Decide which windows to compute and what their effective
	// lookbacks are. "custom" is clamped to retention so the daemon
	// never reports data beyond what it actually holds.
	retentionSec := int64(Retention.Seconds())
	effectiveCustom := int64(0)
	windows := []struct {
		key      string
		lookback int64
	}{
		{WindowKey1m, Lookback1m},
		{WindowKey5m, Lookback5m},
		{WindowKey15m, Lookback15m},
	}
	if lookbackSeconds > 0 {
		effectiveCustom = min(lookbackSeconds, retentionSec)
		windows = append(windows, struct {
			key      string
			lookback int64
		}{WindowKeyCustom, effectiveCustom})
	}

	total := PerWindowValues{
		ProcessingRates:  make(map[string]float64, len(windows)),
		InflightAverages: make(map[string]float64, len(windows)),
	}
	perTransport := make(map[string]PerWindowValues, len(transports))
	for _, w := range windows {
		total.ProcessingRates[w.key] = CalculateRate(q, now, w.lookback, TransportTotal)
		total.InflightAverages[w.key] = CalculateInflightAvg(q, now, w.lookback, TransportTotal)
	}
	for _, t := range transports {
		v := PerWindowValues{
			ProcessingRates:  make(map[string]float64, len(windows)),
			InflightAverages: make(map[string]float64, len(windows)),
		}
		for _, w := range windows {
			v.ProcessingRates[w.key] = CalculateRate(q, now, w.lookback, t)
			v.InflightAverages[w.key] = CalculateInflightAvg(q, now, w.lookback, t)
		}
		perTransport[t] = v
	}

	return &WindowedResult{
		Total:                    total,
		PerTransport:             perTransport,
		SamplesCount:             int64(q.Length()),
		PodsObservedAvg:          averagePodsObserved(q),
		CustomWindowEffectiveSec: effectiveCustom,
	}, nil
}

// observedTransports walks the ring buffer to collect every distinct
// transport label value seen on either counter or gauge metrics.
// Returns a deduplicated slice (order not guaranteed).
func observedTransports(q *sharedqueue.OverflowQueue[*TimestampedCounts]) []string {
	set := map[string]struct{}{}
	for _, b := range q.Items() {
		for _, sample := range b.Snapshot() {
			if sample == nil {
				continue
			}
			for k := range sample.CounterByTransport {
				set[k] = struct{}{}
			}
			for k := range sample.GaugeByTransport {
				set[k] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// averagePodsObserved returns the mean number of pods observed per
// bucket across the retained history, rounded to int64. Zero when no
// buckets exist.
func averagePodsObserved(q *sharedqueue.OverflowQueue[*TimestampedCounts]) int64 {
	items := q.Items()
	if len(items) == 0 {
		return 0
	}
	var total int
	for _, b := range items {
		total += len(b.Snapshot())
	}
	return int64(total / len(items))
}
