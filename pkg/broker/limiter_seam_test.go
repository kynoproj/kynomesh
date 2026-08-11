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
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapHTTP_AdmitsUnderCap(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()

	wrapped := wrapHTTP(NewLimiter(1), set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(1), testutil.ToFloat64(set.requests))
	assert.Equal(t, float64(0), testutil.ToFloat64(set.rejected))
}

func TestWrapHTTP_RejectsAtCapWith429AndRetryAfter(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()

	// Hold the only slot open for the duration of the second request so the
	// limiter is genuinely at capacity when it arrives.
	block := make(chan struct{})
	entered := make(chan struct{})
	limiter := NewLimiter(1)
	wrapped := wrapHTTP(limiter, set, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			close(entered)
			<-block
		}
		w.WriteHeader(http.StatusOK)
	}))

	go wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/hold", nil))
	<-entered

	// The held request occupies the only slot and the inflight gauge; a
	// rejected request must not perturb that gauge.
	inflightBefore := testutil.ToFloat64(set.inflight)
	require.Equal(t, float64(1), inflightBefore, "held request must occupy the inflight gauge")

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/other", nil))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "request over the cap must be rejected with 429")
	assert.Equal(t, strconv.Itoa(retryAfterSeconds), rec.Header().Get("Retry-After"),
		"429 must carry a Retry-After hint")
	assert.Equal(t, float64(1), testutil.ToFloat64(set.rejected), "rejection must be counted")
	assert.Equal(t, inflightBefore, testutil.ToFloat64(set.inflight),
		"a rejected request must not touch the inflight gauge")

	close(block)
}

func TestWrapHTTP_ReleasesSlotOnCompletion(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()

	limiter := NewLimiter(1)
	wrapped := wrapHTTP(limiter, set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Two sequential requests: the first must release its slot so the second
	// is admitted, not rejected.
	for i := range 2 {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		require.Equalf(t, http.StatusOK, rec.Code, "sequential request %d must be admitted", i)
	}
	assert.Equal(t, float64(0), testutil.ToFloat64(set.rejected))
	assert.Equal(t, float64(2), testutil.ToFloat64(set.requests))
}

func TestWrapHTTP_NilLimiterAlwaysAdmits(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.JSONRPCSet()

	wrapped := wrapHTTP(nil, set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for range 5 {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, float64(0), testutil.ToFloat64(set.rejected))
}
