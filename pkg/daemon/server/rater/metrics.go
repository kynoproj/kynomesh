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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SelfMetrics is the daemon's own observability surface exposed on
// the /metrics port. Labeled by AgentDeploy so operators can spot a
// single struggling deploy without grep'ing logs.
type SelfMetrics struct {
	ScrapeSuccess     *prometheus.CounterVec
	ScrapeFailures    *prometheus.CounterVec
	DiscoveryFailures *prometheus.CounterVec
	PodsObserved      *prometheus.GaugeVec
}

// NewSelfMetrics registers the daemon's metrics on the given
// registry and returns the wired collectors.
func NewSelfMetrics(registry prometheus.Registerer) *SelfMetrics {
	return &SelfMetrics{
		ScrapeSuccess: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "daemon_scrape_success_total",
				Help: "Pod /metrics scrapes that completed without error, by AgentDeploy.",
			},
			[]string{"agentdeploy"},
		),
		ScrapeFailures: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "daemon_scrape_failures_total",
				Help: "Pod /metrics scrapes that failed (HTTP error, timeout, parse error), by AgentDeploy.",
			},
			[]string{"agentdeploy"},
		),
		DiscoveryFailures: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "daemon_discovery_failures_total",
				Help: "Headless-DNS lookup failures, by AgentDeploy.",
			},
			[]string{"agentdeploy"},
		),
		PodsObserved: promauto.With(registry).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "daemon_pods_observed",
				Help: "Number of ready pods discovered for an AgentDeploy on the last scrape tick.",
			},
			[]string{"agentdeploy"},
		),
	}
}
