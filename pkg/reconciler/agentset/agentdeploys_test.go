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
		ad, ok := out[childName("greeter", agent)]
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
			BrokerTemplate: &kmv1.ContainerTemplate{ImagePullPolicy: tmplPull},
		},
	}
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out[childName("greeter", "alpha")]
	require.NotNil(t, ad.Spec.BrokerTemplate)
	assert.Equal(t, tmplPull, ad.Spec.BrokerTemplate.ImagePullPolicy)

	// Per-agent value wins over template.
	perAgent := corev1.PullPolicy("IfNotPresent")
	as.Spec.Agents[0].BrokerTemplate = &kmv1.ContainerTemplate{ImagePullPolicy: perAgent}
	out, err = r.buildDesired(as)
	require.NoError(t, err)
	ad = out[childName("greeter", "alpha")]
	require.NotNil(t, ad.Spec.BrokerTemplate)
	assert.Equal(t, perAgent, ad.Spec.BrokerTemplate.ImagePullPolicy,
		"per-agent value should beat the template default")
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

func TestNeedsUpdate(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	desired, err := r.buildDesired(newAgentSet("greeter", "alpha"))
	require.NoError(t, err)
	want := desired[childName("greeter", "alpha")]
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

	running := desired[childName("greeter", "alpha")].DeepCopy()
	running.Status.Phase = kmv1.AgentDeployPhaseRunning

	r.aggregateChildHealth(as, desired, map[string]*kmv1.AgentDeploy{running.Name: running})
	assert.Equal(t, kmv1.AgentSetPhaseRunning, as.Status.Phase)

	r.aggregateChildHealth(as, desired, map[string]*kmv1.AgentDeploy{})
	assert.Equal(t, kmv1.AgentSetPhaseFailed, as.Status.Phase, "missing child should mark Failed")
}
