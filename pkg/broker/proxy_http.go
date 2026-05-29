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
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

// NewJSONRPCReverseProxy returns the JSON-RPC pass-through handler.
func NewJSONRPCReverseProxy(udsTransport *http.Transport, counters *Counters) http.Handler {
	return newUDSReverseProxy(udsTransport, &counters.jsonRPC)
}

// NewRESTReverseProxy returns the REST pass-through handler.
func NewRESTReverseProxy(udsTransport *http.Transport, counters *Counters) http.Handler {
	return newUDSReverseProxy(udsTransport, &counters.rest)
}

// NewPassthroughReverseProxy returns the catch-all HTTP pass-through
// handler. It forwards every request to the agent's UDS unchanged —
// used for traffic the agent serves outside the canonical A2A routes
// (UIs, custom REST endpoints, WebSocket upgrades, etc.). Increments
// the Passthrough counter so non-A2A load is observable separately
// from A2A traffic.
func NewPassthroughReverseProxy(udsTransport *http.Transport, counters *Counters) http.Handler {
	return newUDSReverseProxy(udsTransport, &counters.passthrough)
}

// newUDSReverseProxy builds a httputil.ReverseProxy that forwards to
// the agent over a Unix Domain Socket. The Director plants a synthetic
// http://kynomesh-agent base on every outgoing request.
func newUDSReverseProxy(udsTransport *http.Transport, counter *atomic.Int64) http.Handler {
	target := &url.URL{Scheme: "http", Host: AgentBackendHost}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = udsTransport
	return wrapHTTP(counter, rp)
}
