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

package v1alpha1

// ComputeTopology derives agentName's topology view from as: the routing
// pattern, whether agentName is the entry, and the peers agentName is
// allowed to discover. Shared by the AgentSet controller (which stamps this
// onto each AgentDeploy it creates) and the daemon (which receives a
// slimmed-down AgentSet, see EncodeAgentSet / AgentSet.SimpleCopy, and needs
// the exact same derivation to know which peers to track for AgentCard
// drift).
func ComputeTopology(as *AgentSet, agentName string) Topology {
	if as == nil {
		return Topology{}
	}
	spec := as.Spec
	t := Topology{
		Pattern: spec.Pattern,
		IsEntry: agentName == spec.Entry,
	}
	switch spec.Pattern {
	case AgentPatternHandoff:
		t.Peers = peersExcluding(spec.Agents, spec.ExternalAgents, agentName)
	case AgentPatternSupervisor:
		if t.IsEntry {
			t.Peers = peersExcluding(spec.Agents, spec.ExternalAgents, agentName)
		}
	case AgentPatternSequential:
		if next, ok := nextAgent(spec.Agents, spec.ExternalAgents, agentName); ok {
			t.Peers = []Peer{next}
		}
	}
	return t
}

// peersExcluding returns every managed agent except self, plus every
// external agent, as Peer entries.
func peersExcluding(agents []AbstractAgentDeploy, externalAgents []ExternalAgentRef, self string) []Peer {
	out := make([]Peer, 0, len(agents)-1+len(externalAgents))
	for _, a := range agents {
		if a.Name == self {
			continue
		}
		out = append(out, Peer{Name: a.Name, Kind: PeerKindManaged})
	}
	for _, e := range externalAgents {
		out = append(out, Peer{Name: e.Name, Kind: PeerKindExternal, URL: e.URL})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nextAgent returns the peer immediately after self in the chain: the next
// managed agent in declaration order, or — if self is the last managed
// agent — the sole external agent, if one is configured.
// Sequential allows at most one, enforced by the validator, and it may only
// be the final hop.
func nextAgent(agents []AbstractAgentDeploy, externalAgents []ExternalAgentRef, self string) (Peer, bool) {
	for i, a := range agents {
		if a.Name != self {
			continue
		}
		if i+1 < len(agents) {
			return Peer{Name: agents[i+1].Name, Kind: PeerKindManaged}, true
		}
		if len(externalAgents) > 0 {
			e := externalAgents[0]
			return Peer{Name: e.Name, Kind: PeerKindExternal, URL: e.URL}, true
		}
		return Peer{}, false
	}
	return Peer{}, false
}
