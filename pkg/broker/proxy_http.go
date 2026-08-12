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
)

func NewJSONRPCReverseProxy(agentTransport *http.Transport, counters *Metrics, limiter Limiter) http.Handler {
	return newAgentReverseProxy(agentTransport, limiter, counters.JSONRPCSet())
}

func NewRESTReverseProxy(agentTransport *http.Transport, counters *Metrics, limiter Limiter) http.Handler {
	return newAgentReverseProxy(agentTransport, limiter, counters.RESTSet())
}

// NewPassthroughReverseProxy is the catch-all for non-A2A routes (custom
// REST, UIs, WebSocket upgrades) so they remain observable separately.
//
// It is deliberately NOT rate-limited.
func NewPassthroughReverseProxy(agentTransport *http.Transport, counters *Metrics) http.Handler {
	return newAgentReverseProxy(agentTransport, nil, counters.PassthroughSet())
}

// newAgentReverseProxy builds a reverse proxy whose Director plants a
// synthetic AgentBackendHost target; the supplied transport handles the
// real dial (UDS or TCP).
func newAgentReverseProxy(agentTransport *http.Transport, limiter Limiter, set transportSet) http.Handler {
	target := &url.URL{Scheme: "http", Host: AgentBackendHost}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = agentTransport
	// FlushInterval = -1 disables proxy-side buffering so server-sent
	// events reach the client byte-for-byte as the agent emits them.
	rp.FlushInterval = -1
	return wrapHTTP(limiter, set, rp)
}
