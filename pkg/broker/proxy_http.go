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
// Forwards every request under broker.JSONRPCEndpoint to the agent at
// backendBase, with the broker's JSON-RPC counter bumped for the
// request's lifetime. The proxy is byte-level: it copies headers, body,
// and status verbatim and does not parse the JSON-RPC payload.
func NewJSONRPCReverseProxy(backendBase *url.URL, counters *Counters) http.Handler {
	return newReverseProxy(backendBase, &counters.jsonRPC)
}

// NewRESTReverseProxy returns the REST pass-through handler. Forwards
// every request under broker.RESTEndpoint to the agent at backendBase
// with the broker's REST counter bumped for the request's lifetime. The
// agent must expose the same path layout the broker advertises — no
// prefix translation happens here.
func NewRESTReverseProxy(backendBase *url.URL, counters *Counters) http.Handler {
	return newReverseProxy(backendBase, &counters.rest)
}

// newReverseProxy is the shared httputil.NewSingleHostReverseProxy
// builder. backendBase must already be a parsed *url.URL pointing at
// the agent's base (scheme + host + optional path prefix). The standard
// library's SingleHostReverseProxy concatenates this with the inbound
// request's path, so the broker's external path layout passes through
// unchanged (e.g. /rpc on the broker becomes <backendBase>/rpc on the
// agent).
func newReverseProxy(backendBase *url.URL, counter *atomic.Int64) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(backendBase)
	return wrapHTTP(counter, rp)
}
