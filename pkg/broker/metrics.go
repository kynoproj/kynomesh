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
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// retryAfterSeconds is the Retry-After hint returned with an HTTP 429 when the
// broker sheds a request at its in-flight cap. It is a coarse backoff nudge for
// well-behaved clients, not a precise availability estimate.
const retryAfterSeconds = 1

// Transport label values. The set is closed: the broker emits one of
// these on every metric, the daemon's scraper is label-agnostic but
// groups by whichever values it observes.
const (
	TransportJSONRPC     = "jsonrpc"
	TransportREST        = "rest"
	TransportGRPC        = "grpc"
	TransportPassthrough = "passthrough"
)

// requestDurationBuckets covers the typical A2A latency range: a few
// ms for unary RPCs against a hot agent, up to tens of seconds for
// slow tool calls. Bucket bounds match Prometheus' DefBuckets with
// two extra high-end bins (>10s, >30s) since agents legitimately
// take that long.
var requestDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30,
}

// Metrics owns every Prometheus metric the broker exposes.
type Metrics struct {
	inflight        *prometheus.GaugeVec
	requests        *prometheus.CounterVec
	rejected        *prometheus.CounterVec
	streamMessages  *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// NewMetrics registers all broker metrics on the supplied registry.
// Use a fresh registry in tests to avoid bleed-through across cases.
func NewMetrics(registry prometheus.Registerer) *Metrics {
	return &Metrics{
		inflight: promauto.With(registry).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "broker_inflight_requests",
				Help: "Number of in-flight requests the broker is currently proxying, by transport. A streaming call holds its slot for the stream's lifetime, not per message.",
			},
			[]string{"transport"},
		),
		requests: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "broker_requests_total",
				Help: "Total number of requests the broker has handled, by transport. One increment per HTTP request completion or gRPC stream close.",
			},
			[]string{"transport"},
		),
		rejected: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "broker_rejected_total",
				Help: "Total number of requests the broker rejected at admission because the max in-flight cap was reached, by transport. HTTP rejections return 429; gRPC returns RESOURCE_EXHAUSTED.",
			},
			[]string{"transport"},
		),
		streamMessages: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "broker_stream_messages_total",
				Help: "Total number of stream messages observed on the wire, by transport. Counts SSE events for REST/passthrough and server→client frames for gRPC. Stays at 0 for non-streaming responses.",
			},
			[]string{"transport"},
		),
		requestDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "broker_request_duration_seconds",
				Help:    "Wall-clock duration of broker-handled requests, by transport. Observed on HTTP request completion and gRPC stream close.",
				Buckets: requestDurationBuckets,
			},
			[]string{"transport"},
		),
	}
}

// per-transport accessors for the inflight gauge.
func (c *Metrics) JSONRPC() prometheus.Gauge { return c.inflight.WithLabelValues(TransportJSONRPC) }
func (c *Metrics) REST() prometheus.Gauge    { return c.inflight.WithLabelValues(TransportREST) }
func (c *Metrics) GRPC() prometheus.Gauge    { return c.inflight.WithLabelValues(TransportGRPC) }
func (c *Metrics) Passthrough() prometheus.Gauge {
	return c.inflight.WithLabelValues(TransportPassthrough)
}

// transportSet returns the per-transport handles needed by the
// wrappers — inflight gauge, request/stream counters, and duration
// histogram — bundled into one struct so call sites don't have to
// thread four values manually.
type transportSet struct {
	inflight       prometheus.Gauge
	requests       prometheus.Counter
	rejected       prometheus.Counter
	streamMessages prometheus.Counter
	duration       prometheus.Observer
}

func (c *Metrics) setFor(transport string) transportSet {
	return transportSet{
		inflight:       c.inflight.WithLabelValues(transport),
		requests:       c.requests.WithLabelValues(transport),
		rejected:       c.rejected.WithLabelValues(transport),
		streamMessages: c.streamMessages.WithLabelValues(transport),
		duration:       c.requestDuration.WithLabelValues(transport),
	}
}

// JSONRPCSet, RESTSet, GRPCSet, PassthroughSet return the full per-
// transport metric handles, used by the per-transport wrappers.
func (c *Metrics) JSONRPCSet() transportSet     { return c.setFor(TransportJSONRPC) }
func (c *Metrics) RESTSet() transportSet        { return c.setFor(TransportREST) }
func (c *Metrics) GRPCSet() transportSet        { return c.setFor(TransportGRPC) }
func (c *Metrics) PassthroughSet() transportSet { return c.setFor(TransportPassthrough) }

// wrapHTTP brackets h with the broker's HTTP-side metric updates:
//
//   - inflight gauge incremented on entry, decremented on exit.
//   - requests counter incremented on completion.
//   - duration histogram observed on completion.
//   - stream messages counter incremented per SSE event when the
//     response Content-Type is text/event-stream (detection happens
//     after the agent's response headers arrive — see
//     statusRecorder).
func wrapHTTP(limiter Limiter, set transportSet, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter != nil {
			release, ok := limiter.Acquire()
			if !ok {
				set.rejected.Inc()
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
				http.Error(w, "broker at max in-flight capacity", http.StatusTooManyRequests)
				return
			}
			defer release()
		}
		set.inflight.Inc()
		start := time.Now()
		rec := newStreamRecorder(w, set.streamMessages)
		defer func() {
			set.inflight.Dec()
			set.requests.Inc()
			set.duration.Observe(time.Since(start).Seconds())
			rec.Flush()
		}()
		h.ServeHTTP(rec, r)
	})
}
