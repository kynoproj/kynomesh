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

package agentset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestBuildAgentDeploys(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha", "beta")
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	require.Len(t, out, 2)

	for _, agent := range []string{"alpha", "beta"} {
		ad, ok := out["greeter-"+agent]
		require.True(t, ok, "missing child for %s", agent)
		assert.Equal(t, testNamespace, ad.Namespace)
		assert.Equal(t, agent, ad.Spec.Name)
		assert.Equal(t, "greeter", ad.Spec.AgentSetName)
		assert.Equal(t, "greeter", ad.Labels[kmv1.KeyAgentSetName])
		assert.Equal(t, kmv1.ControllerAgentSet, ad.Labels[kmv1.KeyManagedBy])
		assert.NotEmpty(t, ad.Annotations[kmv1.KeyHash])
		require.Len(t, ad.OwnerReferences, 1, "controller reference must be set")
		assert.Equal(t, "greeter", ad.OwnerReferences[0].Name)
		assert.True(t, *ad.OwnerReferences[0].Controller)
	}
}

func TestBuildAgentDeploys_TemplateAppliedAsDefault(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	tmplPull := corev1.PullPolicy("Always")
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			BrokerContainer: &kmv1.ContainerTemplate{ImagePullPolicy: tmplPull},
		},
	}
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]
	require.NotNil(t, ad.Spec.BrokerContainer)
	assert.Equal(t, tmplPull, ad.Spec.BrokerContainer.ImagePullPolicy)

	// Per-agent value wins over template.
	perAgent := corev1.PullPolicy("IfNotPresent")
	as.Spec.Agents[0].BrokerContainer = &kmv1.ContainerTemplate{ImagePullPolicy: perAgent}
	out, err = r.buildDesired(as)
	require.NoError(t, err)
	ad = out["greeter-alpha"]
	require.NotNil(t, ad.Spec.BrokerContainer)
	assert.Equal(t, perAgent, ad.Spec.BrokerContainer.ImagePullPolicy,
		"per-agent value should beat the template default")
}

func TestBuildAgentDeploys_BrokerContainerFieldMerge(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			BrokerContainer: &kmv1.ContainerTemplate{
				Env: []corev1.EnvVar{{Name: "FROM_TEMPLATE", Value: "t"}},
			},
		},
	}
	// The agent sets its own broker env; the template's env must still merge in
	// rather than being dropped wholesale.
	as.Spec.Agents[0].BrokerContainer = &kmv1.ContainerTemplate{
		Env: []corev1.EnvVar{{Name: "FROM_AGENT", Value: "a"}},
	}

	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]

	require.NotNil(t, ad.Spec.BrokerContainer)
	names := map[string]string{}
	for _, e := range ad.Spec.BrokerContainer.Env {
		names[e.Name] = e.Value
	}
	assert.Equal(t, "a", names["FROM_AGENT"], "per-agent broker env kept")
	assert.Equal(t, "t", names["FROM_TEMPLATE"], "template broker env merged in, not dropped")
}

func TestBuildAgentDeploys_TemplatePodFieldsApplied(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			AbstractPodTemplate: kmv1.AbstractPodTemplate{
				NodeSelector:       map[string]string{"disktype": "ssd"},
				ServiceAccountName: "tmpl-sa",
				Metadata: &kmv1.Metadata{
					Labels: map[string]string{"team": "platform", "tier": "backend"},
				},
			},
		},
	}

	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]

	// Pod-level template fields are applied when the agent sets none.
	assert.Equal(t, "ssd", ad.Spec.NodeSelector["disktype"])
	assert.Equal(t, "tmpl-sa", ad.Spec.ServiceAccountName)
	require.NotNil(t, ad.Spec.Metadata)
	assert.Equal(t, "platform", ad.Spec.Metadata.Labels["team"])
	assert.Equal(t, "backend", ad.Spec.Metadata.Labels["tier"])
}

func TestBuildAgentDeploys_PerAgentPodFieldsWinOverTemplate(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			AbstractPodTemplate: kmv1.AbstractPodTemplate{
				ServiceAccountName: "tmpl-sa",
				Metadata: &kmv1.Metadata{
					Labels: map[string]string{"team": "platform", "tier": "backend"},
				},
			},
		},
	}
	// Per-agent sets its own SA and a colliding + a new label.
	as.Spec.Agents[0].ServiceAccountName = "agent-sa"
	as.Spec.Agents[0].Metadata = &kmv1.Metadata{
		Labels: map[string]string{"tier": "frontend", "extra": "yes"},
	}

	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]

	assert.Equal(t, "agent-sa", ad.Spec.ServiceAccountName, "per-agent scalar wins")
	require.NotNil(t, ad.Spec.Metadata)
	assert.Equal(t, "frontend", ad.Spec.Metadata.Labels["tier"], "per-agent label key wins on collision")
	assert.Equal(t, "yes", ad.Spec.Metadata.Labels["extra"], "per-agent-only label kept")
	assert.Equal(t, "platform", ad.Spec.Metadata.Labels["team"], "template-only label merged in")
}

func TestComputeTopology(t *testing.T) {
	mk := func(pattern kmv1.AgentPattern, entry string, names ...string) *kmv1.AgentSet {
		as := newAgentSet("set", names...)
		as.Spec.Pattern = pattern
		as.Spec.Entry = entry
		return as
	}
	managed := func(names ...string) []kmv1.Peer {
		if len(names) == 0 {
			return nil
		}
		out := make([]kmv1.Peer, 0, len(names))
		for _, n := range names {
			out = append(out, kmv1.Peer{Name: n, Kind: kmv1.PeerKindManaged})
		}
		return out
	}
	cases := []struct {
		name  string
		as    *kmv1.AgentSet
		agent string
		want  kmv1.Topology
	}{
		{
			name:  "supervisor entry sees all workers",
			as:    mk(kmv1.AgentPatternSupervisor, "alpha", "alpha", "beta", "gamma"),
			agent: "alpha",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternSupervisor, IsEntry: true, Peers: managed("beta", "gamma")},
		},
		{
			name:  "supervisor worker sees nobody",
			as:    mk(kmv1.AgentPatternSupervisor, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternSupervisor},
		},
		{
			name:  "handoff: everyone sees everyone else",
			as:    mk(kmv1.AgentPatternHandoff, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternHandoff, Peers: managed("alpha", "gamma")},
		},
		{
			name:  "sequential: middle sees only the next",
			as:    mk(kmv1.AgentPatternSequential, "alpha", "alpha", "beta", "gamma"),
			agent: "beta",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternSequential, Peers: managed("gamma")},
		},
		{
			name:  "sequential: last sees nobody",
			as:    mk(kmv1.AgentPatternSequential, "alpha", "alpha", "beta", "gamma"),
			agent: "gamma",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternSequential},
		},
		{
			name:  "sequential: entry flag set on first",
			as:    mk(kmv1.AgentPatternSequential, "alpha", "alpha", "beta"),
			agent: "alpha",
			want:  kmv1.Topology{Pattern: kmv1.AgentPatternSequential, IsEntry: true, Peers: managed("beta")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, computeTopology(tc.as, tc.agent))
		})
	}
}

func TestComputeTopology_ExternalAgents(t *testing.T) {
	mk := func(pattern kmv1.AgentPattern, entry string, names ...string) *kmv1.AgentSet {
		as := newAgentSet("set", names...)
		as.Spec.Pattern = pattern
		as.Spec.Entry = entry
		return as
	}
	ext := kmv1.ExternalAgentRef{Name: "ext", URL: "https://ext.example.com"}
	extPeer := kmv1.Peer{Name: ext.Name, Kind: kmv1.PeerKindExternal, URL: ext.URL}

	t.Run("handoff: external agent is a peer for everyone", func(t *testing.T) {
		as := mk(kmv1.AgentPatternHandoff, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{ext}
		got := computeTopology(as, "alpha")
		assert.Contains(t, got.Peers, extPeer)
		assert.Contains(t, got.Peers, kmv1.Peer{Name: "beta", Kind: kmv1.PeerKindManaged})
	})

	t.Run("supervisor: external agent is a peer only for entry", func(t *testing.T) {
		as := mk(kmv1.AgentPatternSupervisor, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{ext}

		entryTopo := computeTopology(as, "alpha")
		assert.Contains(t, entryTopo.Peers, extPeer)

		workerTopo := computeTopology(as, "beta")
		assert.Empty(t, workerTopo.Peers, "non-entry agent must not see the external peer")
	})

	t.Run("sequential: external agent is the final hop after the last managed agent", func(t *testing.T) {
		as := mk(kmv1.AgentPatternSequential, "alpha", "alpha", "beta")
		as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{ext}

		last := computeTopology(as, "beta")
		assert.Equal(t, []kmv1.Peer{extPeer}, last.Peers, "last managed agent's next hop is the external agent")

		first := computeTopology(as, "alpha")
		assert.Equal(t, []kmv1.Peer{{Name: "beta", Kind: kmv1.PeerKindManaged}}, first.Peers,
			"external agent must not be inserted before the last managed agent")
	})

	t.Run("sequential: no external agents leaves the last agent with no peers", func(t *testing.T) {
		as := mk(kmv1.AgentPatternSequential, "alpha", "alpha", "beta")
		got := computeTopology(as, "beta")
		assert.Empty(t, got.Peers)
	})
}

func TestNeedsUpdate(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	desired, err := r.buildDesired(newAgentSet("greeter", "alpha"))
	require.NoError(t, err)
	want := desired["greeter-alpha"]
	existing := want.DeepCopy()
	assert.False(t, needsUpdate(existing, want), "identical children should not trigger update")

	drifted := want.DeepCopy()
	drifted.Annotations[kmv1.KeyHash] = "stale"
	assert.True(t, needsUpdate(drifted, want), "different hash should trigger update")
}

func TestAggregateChildHealth(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha")
	desired, err := r.buildDesired(as)
	require.NoError(t, err)

	running := desired["greeter-alpha"].DeepCopy()
	running.Status.Phase = kmv1.AgentDeployPhaseRunning

	r.aggregateChildHealth(as, desired, map[string]*kmv1.AgentDeploy{running.Name: running})
	assert.Equal(t, kmv1.AgentSetPhaseRunning, as.Status.Phase)

	r.aggregateChildHealth(as, desired, map[string]*kmv1.AgentDeploy{})
	assert.Equal(t, kmv1.AgentSetPhaseFailed, as.Status.Phase, "missing child should mark Failed")
}
