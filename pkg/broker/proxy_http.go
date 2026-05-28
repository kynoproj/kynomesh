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
// Requests are forwarded over the supplied UDS transport to the agent;
// the JSON-RPC in-flight counter is bumped for each request's lifetime.
func NewJSONRPCReverseProxy(udsTransport *http.Transport, counters *Counters) http.Handler {
	return newUDSReverseProxy(udsTransport, &counters.jsonRPC)
}

// NewRESTReverseProxy returns the REST pass-through handler. Same
// transport / counter model as NewJSONRPCReverseProxy.
func NewRESTReverseProxy(udsTransport *http.Transport, counters *Counters) http.Handler {
	return newUDSReverseProxy(udsTransport, &counters.rest)
}

// newUDSReverseProxy builds a httputil.ReverseProxy that forwards to
// the agent over a Unix Domain Socket. The Director plants a synthetic
// http://kynomesh-agent base on every outgoing request — UDS has no
// real host, so net/http needs *something* in the URL, and the supplied
// transport ignores host/port anyway and dials the socket.
//
// The inbound request's path and query are preserved verbatim, so the
// broker's external path layout matches the agent's internal one (e.g.
// /rpc on the broker becomes /rpc on the agent).
func newUDSReverseProxy(udsTransport *http.Transport, counter *atomic.Int64) http.Handler {
	target := &url.URL{Scheme: "http", Host: AgentBackendHost}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = udsTransport
	return wrapHTTP(counter, rp)
}
