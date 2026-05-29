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
	"github.com/stretchr/testify/assert"
)

func TestCounters_FreshIsZero(t *testing.T) {
	c := NewCounters(prometheus.NewRegistry())
	assert.Equal(t, float64(0), testutil.ToFloat64(c.JSONRPC()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.REST()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.GRPC()))
	assert.Equal(t, float64(0), testutil.ToFloat64(c.Passthrough()))
}

func TestWrapHTTP_BracketsRequest(t *testing.T) {
	// The wrapper must increment exactly once per request and decrement
	// when the handler returns. The mid-call observation needs the
	// handler to capture the count while still executing.
	c := NewCounters(prometheus.NewRegistry())
	gauge := c.JSONRPC()
	var mu sync.Mutex
	var midCallCount float64

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		midCallCount = testutil.ToFloat64(gauge)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapHTTP(gauge, inner)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, float64(1), midCallCount, "gauge must be 1 while the handler runs")
	assert.Equal(t, float64(0), testutil.ToFloat64(gauge), "gauge must return to 0 after the handler returns")
}

func TestWrapHTTP_DecrementsOnPanic(t *testing.T) {
	// Defer-based decrement must survive a handler panic so a
	// misbehaving upstream can't leak in-flight slots forever.
	c := NewCounters(prometheus.NewRegistry())
	gauge := c.JSONRPC()
	wrapped := wrapHTTP(gauge, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	defer func() {
		_ = recover()
		assert.Equal(t, float64(0), testutil.ToFloat64(gauge),
			"panicking handler must still release the counter slot")
	}()
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
