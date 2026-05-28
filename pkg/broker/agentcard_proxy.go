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
	"encoding/json"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

// NewAgentCardProxy returns an http.Handler that, for every incoming
// request, fetches the AgentCard from the user's agent, filters and
// rewrites SupportedInterfaces down to the transports the broker
// actually exposes, and serves the result.
func NewAgentCardProxy(agentBaseURL, advertiseHost string, port int, enabled map[a2a.TransportProtocol]bool) http.Handler {
	resolver := agentcard.NewResolver(http.DefaultClient)
	return &agentCardProxy{
		agentBaseURL:  agentBaseURL,
		advertiseHost: advertiseHost,
		port:          port,
		enabled:       enabled,
		resolve: func(r *http.Request) (*a2a.AgentCard, error) {
			return resolver.Resolve(r.Context(), agentBaseURL)
		},
	}
}

type agentCardProxy struct {
	agentBaseURL  string
	advertiseHost string
	port          int
	enabled       map[a2a.TransportProtocol]bool
	// resolve is a seam for tests; production uses agentcard.NewResolver.
	resolve func(r *http.Request) (*a2a.AgentCard, error)
}

func (p *agentCardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	card, err := p.resolve(r)
	if err != nil {
		http.Error(w, "failed to fetch agent card: "+err.Error(), http.StatusBadGateway)
		return
	}
	card.SupportedInterfaces = rewriteAgentCardInterfaces(card.SupportedInterfaces, p.advertiseHost, p.port, p.enabled)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(card); err != nil {
		// Header was already written if Encode flushed; nothing useful
		// we can do here — the client will see a truncated body.
		return
	}
}

// rewriteAgentCardInterfaces returns a new slice containing only the
// interfaces whose ProtocolBinding is in enabled, with each URL rewritten
// to the broker's external endpoint for that transport. Order is
// preserved.
func rewriteAgentCardInterfaces(in []*a2a.AgentInterface, advertiseHost string, port int, enabled map[a2a.TransportProtocol]bool) []*a2a.AgentInterface {
	out := make([]*a2a.AgentInterface, 0, len(in))
	for _, iface := range in {
		if !enabled[iface.ProtocolBinding] {
			continue
		}
		rewritten := *iface
		switch iface.ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			rewritten.URL = JSONRPCAddr(advertiseHost, port)
		case a2a.TransportProtocolHTTPJSON:
			rewritten.URL = RESTAddr(advertiseHost, port)
		case a2a.TransportProtocolGRPC:
			rewritten.URL = GRPCAddr(advertiseHost, port)
		}
		out = append(out, &rewritten)
	}
	return out
}
