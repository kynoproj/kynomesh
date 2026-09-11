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

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestAgentSet(pattern AgentPattern, entry string, names ...string) *AgentSet {
	as := &AgentSet{Spec: AgentSetSpec{Pattern: pattern, Entry: entry}}
	for _, n := range names {
		as.Spec.Agents = append(as.Spec.Agents, AbstractAgentDeploy{Name: n})
	}
	return as
}

func TestComputeTopology(t *testing.T) {
	managed := func(names ...string) []Peer {
		if len(names) == 0 {
			return nil
		}
		out := make([]Peer, 0, len(names))
		for _, n := range names {
			out = append(out, Peer{Name: n, Kind: PeerKindManaged})
		}
		return out
	}
	cases := []struct {
		name  string
		as    *AgentSet
		agent string
		want  Topology
	}{
		{
			name:  "supervisor entry sees all workers",
			as:    newTestAgentSet(AgentPatternSupervisor, "alpha", "alpha", "beta", "gamma"),
			agent: "alpha",
			want:  Topology{Pattern: AgentPatternSupervisor, IsEntry: true, Peers: managed("beta", "gamma")},
		},
		{
			name:  "supervisor worker sees nobody",
			as:    newTestAgentSet(AgentPatternSupervisor, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  Topology{Pattern: AgentPatternSupervisor},
		},
		{
			name:  "handoff: everyone sees everyone else",
			as:    newTestAgentSet(AgentPatternHandoff, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  Topology{Pattern: AgentPatternHandoff, Peers: managed("alpha", "gamma")},
		},
		{
			name:  "sequential: middle sees only the next",
			as:    newTestAgentSet(AgentPatternSequential, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  Topology{Pattern: AgentPatternSequential, Peers: managed("gamma")},
		},
		{
			name:  "sequential: last sees nobody",
			as:    newTestAgentSet(AgentPatternSequential, "alpha", "alpha", "beta", "gamma"),
			agent: "gamma",
			want:  Topology{Pattern: AgentPatternSequential},
		},
		{
			name:  "sequential: entry flag set on first",
			as:    newTestAgentSet(AgentPatternSequential, "alpha", "alpha", "beta"),
			agent: "alpha",
			want:  Topology{Pattern: AgentPatternSequential, IsEntry: true, Peers: managed("beta")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComputeTopology(tc.as, tc.agent))
		})
	}
}

func TestComputeTopology_ExternalAgents(t *testing.T) {
	ext := ExternalAgentRef{Name: "ext", URL: "https://ext.example.com"}
	extPeer := Peer{Name: ext.Name, Kind: PeerKindExternal, URL: ext.URL}

	t.Run("handoff: external agent is a peer for everyone", func(t *testing.T) {
		as := newTestAgentSet(AgentPatternHandoff, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []ExternalAgentRef{ext}
		got := ComputeTopology(as, "alpha")
		assert.Contains(t, got.Peers, extPeer)
		assert.Contains(t, got.Peers, Peer{Name: "beta", Kind: PeerKindManaged})
	})

	t.Run("supervisor: external agent is a peer only for entry", func(t *testing.T) {
		as := newTestAgentSet(AgentPatternSupervisor, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []ExternalAgentRef{ext}

		entryTopo := ComputeTopology(as, "alpha")
		assert.Contains(t, entryTopo.Peers, extPeer)

		workerTopo := ComputeTopology(as, "beta")
		assert.Empty(t, workerTopo.Peers, "non-entry agent must not see the external peer")
	})

	t.Run("sequential: external agent is the final hop after the last managed agent", func(t *testing.T) {
		as := newTestAgentSet(AgentPatternSequential, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []ExternalAgentRef{ext}

		last := ComputeTopology(as, "beta")
		assert.Equal(t, []Peer{extPeer}, last.Peers, "last managed agent's next hop is the external agent")

		first := ComputeTopology(as, "alpha")
		assert.Equal(t, []Peer{{Name: "beta", Kind: PeerKindManaged}}, first.Peers,
			"external agent must not be inserted before the last managed agent")
	})

	t.Run("sequential: no external agents leaves the last agent with no peers", func(t *testing.T) {
		as := newTestAgentSet(AgentPatternSequential, "alpha", "alpha", "beta")
		got := ComputeTopology(as, "beta")
		assert.Empty(t, got.Peers)
	})
}
