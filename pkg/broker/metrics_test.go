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
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_FreshIsZero(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	assert.Equal(t, float64(0), testutil.ToFloat64(c.JSONRPC()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.REST()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.GRPC()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.Passthrough()))
}

func TestWrapHTTP_BracketsRequestAndCountsCompletion(t *testing.T) {
	// The wrapper must increment exactly once per request and decrement
	// when the handler returns. The requests counter increments once
	// per completion regardless of in-flight observation.
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()
	var mu sync.Mutex
	var midCallInflight float64

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		midCallInflight = testutil.ToFloat64(set.inflight)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapHTTP(set, inner)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, float64(1), midCallInflight, "inflight must be 1 while the handler runs")
	assert.Equal(t, float64(0), testutil.ToFloat64(set.inflight), "inflight must return to 0 after the handler returns")
	assert.Equal(t, float64(1), testutil.ToFloat64(set.requests), "requests counter must increment once on completion")
}

func TestWrapHTTP_DecrementsOnPanic(t *testing.T) {
	// Defer-based decrement must survive a handler panic so a
	// misbehaving upstream can't leak in-flight slots forever.
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()
	wrapped := wrapHTTP(set, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	defer func() {
		_ = recover()
		assert.Equal(t, float64(0), testutil.ToFloat64(set.inflight),
			"panicking handler must still release the in-flight slot")
		assert.Equal(t, float64(1), testutil.ToFloat64(set.requests),
			"panicking handler must still count as a completed request")
	}()
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestWrapHTTP_ObservesDuration(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.RESTSet()

	wrapped := wrapHTTP(set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	count := histogramSampleCount(t, c.requestDuration, TransportREST)
	assert.Equal(t, uint64(1), count, "exactly one duration observation per request")
}

// histogramSampleCount returns the sample count for a labeled
// histogram series. Used to assert "we observed N times" without
// pinning the observed values. Exported within-package so sse_test
// can reuse the helper for streaming-response duration assertions.
func histogramSampleCount(t *testing.T, h *prometheus.HistogramVec, transport string) uint64 {
	t.Helper()
	m, err := h.GetMetricWithLabelValues(transport)
	require.NoError(t, err)
	pb := &dto.Metric{}
	require.NoError(t, m.(prometheus.Histogram).Write(pb))
	require.NotNil(t, pb.Histogram)
	return pb.Histogram.GetSampleCount()
}
