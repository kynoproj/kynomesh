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
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// streamRecorder is the http.ResponseWriter wrapper that detects
// SSE responses and counts events as they flow.
//
// SSE detection: on the first Write/WriteHeader, the response's
// Content-Type is inspected. If it's text/event-stream (case-
// insensitive, with or without parameters), subsequent Writes are
// scanned for "\n\n" event delimiters and the per-transport
// streamMessages counter is incremented once per event.
//
// Non-SSE responses (the common case) take a fast path: no scanning,
// just a passthrough Write to the wrapped ResponseWriter.
type streamRecorder struct {
	http.ResponseWriter
	counter    prometheus.Counter
	headersSet bool
	sse        bool
	// statusCode is the response status, defaulting to 200 to match
	// net/http's own behavior when a handler never calls WriteHeader.
	statusCode int
	// carry holds bytes from a previous Write that ended without a
	// newline; SSE event boundaries can split across Write calls.
	carry []byte
}

func newStreamRecorder(w http.ResponseWriter, counter prometheus.Counter) *streamRecorder {
	return &streamRecorder{ResponseWriter: w, counter: counter, statusCode: http.StatusOK}
}

// WriteHeader is the earliest point we can read Content-Type set by
// the upstream handler.
func (r *streamRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.detectSSE()
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write is also a header-flush point if WriteHeader hasn't been
// called yet, so we re-check SSE here in case the upstream skipped
// the explicit WriteHeader.
func (r *streamRecorder) Write(p []byte) (int, error) {
	if !r.headersSet {
		r.detectSSE()
	}
	if r.sse {
		r.countEvents(p)
	}
	return r.ResponseWriter.Write(p)
}

// StatusCode returns the response status written so far — 200 if the
// handler never called WriteHeader, matching net/http's own default.
func (r *streamRecorder) StatusCode() int {
	return r.statusCode
}

// Flush forwards the underlying flusher if present. Required for SSE
// to actually push events to the client — net/http buffers writes
// until either flush or end-of-response. The reverse proxy installs
// its own Flush hook (FlushInterval), but we still want to forward
// agent-side flushes through transparently.
func (r *streamRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *streamRecorder) detectSSE() {
	r.headersSet = true
	ct := r.ResponseWriter.Header().Get("Content-Type")
	// Content-Type may carry parameters (e.g. "text/event-stream; charset=utf-8").
	// Match the prefix case-insensitively.
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	r.sse = strings.EqualFold(strings.TrimSpace(ct), "text/event-stream")
}

// countEvents scans p (concatenated with any carry from the previous
// Write) for "\n\n" event delimiters and increments the counter once
// per event. Trailing bytes without a terminator are retained for
// the next Write.
//
// SSE spec uses "\n\n" or "\r\n\r\n" as event separators. We accept
// either by normalizing CRLF to LF before splitting — cheap because
// we're already iterating the bytes.
func (r *streamRecorder) countEvents(p []byte) {
	buf := r.carry
	if len(buf) > 0 {
		// Concatenate; allocation is unavoidable when an event spans
		// two Writes but rare in practice (agent-side flushers tend
		// to align with event boundaries).
		buf = append(buf, p...)
	} else {
		buf = p
	}
	// Normalize CRLF → LF in place by scanning once.
	events, remainder := splitSSEEvents(buf)
	if events > 0 {
		r.counter.Add(float64(events))
	}
	if len(remainder) > 0 {
		// Hold the remainder for the next Write.
		r.carry = append(r.carry[:0], remainder...)
	} else {
		r.carry = r.carry[:0]
	}
}

// splitSSEEvents returns (numEvents, remainder). Events are
// delimited by "\n\n" (LF-LF) or "\r\n\r\n" (CRLF-CRLF). Trailing
// bytes without a terminator go into remainder for the next Write.
//
// Implementation is byte-level for speed — SSE bodies can be high-
// frequency and we don't want per-event allocations.
func splitSSEEvents(buf []byte) (int, []byte) {
	count := 0
	start := 0
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == '\n' && buf[i+1] == '\n' {
			count++
			start = i + 2
			i++ // skip the second LF
			continue
		}
		// CRLF-CRLF: buf[i..i+3] == "\r\n\r\n"
		if i+3 < len(buf) && buf[i] == '\r' && buf[i+1] == '\n' && buf[i+2] == '\r' && buf[i+3] == '\n' {
			count++
			start = i + 4
			i += 3
			continue
		}
	}
	return count, buf[start:]
}
