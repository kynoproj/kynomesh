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

// Package scraper fetches Prometheus-format metrics from broker pods
// and extracts the in-flight gauge and processed-messages counter,
// grouped by the broker's "transport" label. The output feeds the
// rater's per-bucket storage.
package scraper

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/daemon/rater"
)

// Metric names emitted by the broker. Hardcoded per design: these are
// part of the broker contract and must not be configurable from the
// daemon side.
const (
	// MetricInflightName is the in-flight gauge (currently present in
	// pkg/broker/counters.go).
	MetricInflightName = "kynomesh_broker_inflight_requests"

	// MetricProcessedName is the processed-messages counter. The
	// broker does not emit this yet; until it does, the scraper will
	// simply observe no counter samples and rate calculations return
	// zero. The daemon does not need a config change when the broker
	// starts emitting this metric.
	MetricProcessedName = "kynomesh_broker_messages_processed_total"

	// TransportLabelName is the label the broker uses to distinguish
	// JSONRPC, REST, gRPC, and passthrough buckets on both metrics.
	TransportLabelName = "transport"
)

// Scraper fetches one pod's /metrics endpoint and parses it. Concrete
// instances are cheap to construct; share one across the daemon.
type Scraper struct {
	client *http.Client
	port   int
}

// New returns a Scraper. timeout caps the total HTTP fetch (default
// behavior matches numaflow's 1s). port is the broker introspection
// port to hit on each pod.
func New(timeout time.Duration) *Scraper {
	return &Scraper{
		client: &http.Client{
			Transport: &http.Transport{
				// The broker terminates TLS with a self-signed cert,
				// generated at broker startup. No CA distribution
				// exists yet; cluster-internal access is the trust
				// boundary.
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec
					MinVersion:         tls.VersionTLS12,
				},
				IdleConnTimeout: 30 * time.Second,
			},
			Timeout: timeout,
		},
		port: kmv1.AgentBrokerIntrospectionPort,
	}
}

// Scrape fetches https://<host>:<port>/metrics and returns the parsed
// PodSample. Returns (nil, err) on scrape failure — the rater treats
// this as "keep the previous bucket value" per numaflow's pattern.
//
// A pod that's reachable but exposes neither configured metric (e.g.
// broker without the counter yet) yields an empty PodSample, not nil,
// so the rater records a zero-value observation rather than holding
// stale data forever.
func (s *Scraper) Scrape(ctx context.Context, host string) (*rater.PodSample, error) {
	url := fmt.Sprintf("https://%s:%d/metrics", host, s.port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", host, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: status %d", host, resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse metrics from %s: %w", host, err)
	}

	sample := &rater.PodSample{
		CounterByTransport: make(map[string]float64),
		GaugeByTransport:   make(map[string]float64),
	}

	if fam, ok := families[MetricInflightName]; ok {
		for _, m := range fam.GetMetric() {
			transport := labelValue(m.GetLabel(), TransportLabelName)
			if transport == "" {
				continue
			}
			if g := m.GetGauge(); g != nil {
				sample.GaugeByTransport[transport] = g.GetValue()
			}
		}
	}

	if fam, ok := families[MetricProcessedName]; ok {
		for _, m := range fam.GetMetric() {
			transport := labelValue(m.GetLabel(), TransportLabelName)
			if transport == "" {
				continue
			}
			if c := m.GetCounter(); c != nil {
				sample.CounterByTransport[transport] = c.GetValue()
			}
		}
	}

	return sample, nil
}

func labelValue(labels []*dto.LabelPair, name string) string {
	for _, l := range labels {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
