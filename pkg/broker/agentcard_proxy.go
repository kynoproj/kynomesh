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

// NewAgentCardProxy serves the agent's AgentCard with SupportedInterfaces
// filtered to enabled transports and URLs rewritten for external clients.
// When publicBaseURL is non-empty it overrides advertiseHost:port so the
// AgentCard advertises an ingress/gateway address rather than the
// in-cluster headless one; pass "" to advertise the in-cluster address.
func NewAgentCardProxy(agentClient *http.Client, publicBaseURL, advertiseHost string, port int, enabled map[a2a.TransportProtocol]bool) http.Handler {
	resolver := agentcard.NewResolver(agentClient)
	agentBaseURL := "http://" + AgentBackendHost
	return &agentCardProxy{
		publicBaseURL: publicBaseURL,
		advertiseHost: advertiseHost,
		port:          port,
		enabled:       enabled,
		resolve: func(r *http.Request) (*a2a.AgentCard, error) {
			return resolver.Resolve(r.Context(), agentBaseURL)
		},
	}
}

type agentCardProxy struct {
	publicBaseURL string
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
	card.SupportedInterfaces = rewriteAgentCardInterfaces(card.SupportedInterfaces, p.publicBaseURL, p.advertiseHost, p.port, p.enabled)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(card); err != nil {
		// Header is already on the wire if Encode flushed; nothing to do.
		return
	}
}

// rewriteAgentCardInterfaces keeps only enabled interfaces and rewrites
// each URL to the broker's endpoint. Order is preserved.
func rewriteAgentCardInterfaces(in []*a2a.AgentInterface, publicBaseURL, advertiseHost string, port int, enabled map[a2a.TransportProtocol]bool) []*a2a.AgentInterface {
	out := make([]*a2a.AgentInterface, 0, len(in))
	for _, iface := range in {
		if !enabled[iface.ProtocolBinding] {
			continue
		}
		rewritten := *iface
		rewritten.URL = AdvertisedURL(publicBaseURL, advertiseHost, port, iface.ProtocolBinding)
		out = append(out, &rewritten)
	}
	return out
}
