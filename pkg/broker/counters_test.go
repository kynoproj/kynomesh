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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCounters_FreshIsZero(t *testing.T) {
	c := &Counters{}
	assert.Equal(t, int64(0), c.JSONRPC())
	assert.Equal(t, int64(0), c.REST())
	assert.Equal(t, int64(0), c.GRPC())
}

func TestWrapHTTP_BracketsRequest(t *testing.T) {
	// The wrapper must increment exactly once per request and decrement
	// when the handler returns. The mid-call observation needs the handler
	// to capture the count while still executing.
	var counter atomic.Int64
	var mu sync.Mutex
	var midCallCount int64

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		midCallCount = counter.Load()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	wrapped := wrapHTTP(&counter, inner)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, int64(1), midCallCount, "counter must be 1 while the handler runs")
	assert.Equal(t, int64(0), counter.Load(), "counter must return to 0 after the handler returns")
}

func TestWrapHTTP_DecrementsOnPanic(t *testing.T) {
	// Defer-based decrement must survive a handler panic so a misbehaving
	// upstream can't leak in-flight slots forever. We catch the panic at
	// the test level to keep go test happy.
	var counter atomic.Int64
	wrapped := wrapHTTP(&counter, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	defer func() {
		_ = recover()
		assert.Equal(t, int64(0), counter.Load(), "panicking handler must still release the counter slot")
	}()
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
