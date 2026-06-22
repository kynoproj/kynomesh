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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestWrapHTTP_SSEEvents covers the SSE event-counting hook. The
// handler writes three event-delimited chunks; the wrapper must
// observe exactly 3 stream messages.
func TestWrapHTTP_SSEEvents(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.PassthroughSet()
	wrapped := wrapHTTP(set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := range 3 {
			_, _ = fmt.Fprintf(w, "event: tick\ndata: %d\n\n", i)
		}
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sse", nil))

	assert.Equal(t, float64(3), testutil.ToFloat64(set.streamMessages),
		"SSE events delimited by \\n\\n must each increment the stream messages counter")
	assert.Equal(t, float64(1), testutil.ToFloat64(set.requests),
		"the streaming response counts as one request")
}

// TestWrapHTTP_SSEEventsAcrossWrites verifies event boundaries that
// straddle individual Write calls are still counted correctly —
// agents emit events via short flushes and the wrapper must hold
// any byte tail until the matching "\n\n" arrives.
func TestWrapHTTP_SSEEventsAcrossWrites(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.PassthroughSet()
	wrapped := wrapHTTP(set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Split a single event across two writes so the "\n\n"
		// boundary spans the chunk gap.
		_, _ = w.Write([]byte("data: hello\n"))
		_, _ = w.Write([]byte("\nevent: bye\ndata: ok\n\n"))
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sse", nil))

	assert.Equal(t, float64(2), testutil.ToFloat64(set.streamMessages),
		"events spanning multiple Writes must still each count as one")
}

// TestWrapHTTP_NonSSEResponseDoesNotCountEvents ensures the common-
// case unary response doesn't inadvertently trigger event counting.
func TestWrapHTTP_NonSSEResponseDoesNotCountEvents(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.RESTSet()
	wrapped := wrapHTTP(set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// A regular JSON body that happens to contain "\n\n" must
		// not produce phantom event increments.
		_, _ = w.Write([]byte(`{"events": "\n\nfoo\n\nbar"}`))
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	assert.Equal(t, float64(0), testutil.ToFloat64(set.streamMessages),
		"non-SSE Content-Type must keep the stream messages counter at 0")
	assert.Equal(t, float64(1), testutil.ToFloat64(set.requests))
}

// TestWrapHTTP_SSEContentTypeWithParameters verifies the
// Content-Type prefix match handles the realistic
// "text/event-stream; charset=utf-8" case.
func TestWrapHTTP_SSEContentTypeWithParameters(t *testing.T) {
	c := NewMetrics(prometheus.NewRegistry())
	set := c.RESTSet()
	wrapped := wrapHTTP(set, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/sse", nil))

	assert.Equal(t, float64(1), testutil.ToFloat64(set.streamMessages))
}

func TestSplitSSEEvents(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantEvents int
		wantTail   string
	}{
		{"empty", "", 0, ""},
		{"single LF-LF event", "data: a\n\n", 1, ""},
		{"two LF-LF events", "data: a\n\ndata: b\n\n", 2, ""},
		{"CRLF-CRLF event", "data: a\r\n\r\n", 1, ""},
		{"partial trailing", "data: a\n\ndata: b\n", 1, "data: b\n"},
		{"no delimiter", "data: a", 0, "data: a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events, tail := splitSSEEvents([]byte(tc.in))
			assert.Equal(t, tc.wantEvents, events)
			assert.Equal(t, tc.wantTail, string(tail))
		})
	}
}
