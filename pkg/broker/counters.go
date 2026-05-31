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

package broker

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Counters owns the broker's per-transport in-flight gauge, labeled by
// transport: jsonrpc, rest, grpc, passthrough. A streaming gRPC call
// holds its slot for the stream's lifetime, not per request.
type Counters struct {
	inflight *prometheus.GaugeVec
}

// NewCounters registers the in-flight gauge. Use a fresh registry in
// tests to avoid bleed-through across test cases.
func NewCounters(registry prometheus.Registerer) *Counters {
	return &Counters{
		inflight: promauto.With(registry).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kynomesh_broker_inflight_requests",
				Help: "Number of in-flight requests the broker is currently proxying, by transport.",
			},
			[]string{"transport"},
		),
	}
}

func (c *Counters) JSONRPC() prometheus.Gauge     { return c.inflight.WithLabelValues("jsonrpc") }
func (c *Counters) REST() prometheus.Gauge        { return c.inflight.WithLabelValues("rest") }
func (c *Counters) GRPC() prometheus.Gauge        { return c.inflight.WithLabelValues("grpc") }
func (c *Counters) Passthrough() prometheus.Gauge { return c.inflight.WithLabelValues("passthrough") }

func (c *Counters) Collector() prometheus.Collector { return c.inflight }

// wrapHTTP brackets h with Gauge.Inc/Dec; defer keeps the count balanced through panics.
func wrapHTTP(gauge prometheus.Gauge, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gauge.Inc()
		defer gauge.Dec()
		h.ServeHTTP(w, r)
	})
}
