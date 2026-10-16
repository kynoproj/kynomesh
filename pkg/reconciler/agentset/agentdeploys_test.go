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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func ptrBool(v bool) *bool { return &v }

func TestBuildAgentDeploys(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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

func TestBuildAgentDeploys_DriftReloadFillIfUnset(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)

	t.Run("both nil defaults to disabled", func(t *testing.T) {
		as := newAgentSet("greeter", "alpha")
		out, err := r.buildDesired(as)
		require.NoError(t, err)
		assert.Nil(t, out["greeter-alpha"].Spec.DriftReload)
	})

	t.Run("per-agent nil takes the AgentSet default", func(t *testing.T) {
		as := newAgentSet("greeter", "alpha")
		as.Spec.DriftReload = &kmv1.DriftReload{Enabled: ptrBool(true)}
		out, err := r.buildDesired(as)
		require.NoError(t, err)
		require.NotNil(t, out["greeter-alpha"].Spec.DriftReload)
		assert.True(t, out["greeter-alpha"].Spec.DriftReload.IsEnabled())
	})

	t.Run("per-agent empty struct also takes the AgentSet default", func(t *testing.T) {
		// driftReload: {} on the agent (non-nil struct, Enabled unset) must
		// not be mistaken for an explicit opt-out - it should inherit the
		// AgentSet-level value just like an entirely absent per-agent field.
		as := newAgentSet("greeter", "alpha")
		as.Spec.DriftReload = &kmv1.DriftReload{Enabled: ptrBool(true)}
		as.Spec.Agents[0].DriftReload = &kmv1.DriftReload{}
		out, err := r.buildDesired(as)
		require.NoError(t, err)
		require.NotNil(t, out["greeter-alpha"].Spec.DriftReload)
		assert.True(t, out["greeter-alpha"].Spec.DriftReload.IsEnabled(),
			"an empty per-agent driftReload must inherit the AgentSet default, not silently disable")
	})

	t.Run("per-agent explicit value wins outright over the AgentSet default", func(t *testing.T) {
		as := newAgentSet("greeter", "alpha")
		as.Spec.DriftReload = &kmv1.DriftReload{Enabled: ptrBool(true)}
		as.Spec.Agents[0].DriftReload = &kmv1.DriftReload{Enabled: ptrBool(false)}
		out, err := r.buildDesired(as)
		require.NoError(t, err)
		require.NotNil(t, out["greeter-alpha"].Spec.DriftReload)
		assert.False(t, out["greeter-alpha"].Spec.DriftReload.IsEnabled(),
			"an explicit per-agent DriftReload must win even though it disables what the AgentSet default enables")
	})
}

func TestBuildAgentDeploys_BrokerContainerFieldMerge(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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

func TestBuildAgentDeploys_InitContainerTemplateAppliedAsDefault(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	tmplPull := corev1.PullPolicy("Always")
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			InitContainer: &kmv1.ContainerTemplate{ImagePullPolicy: tmplPull},
		},
	}
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]
	require.NotNil(t, ad.Spec.InitContainer)
	assert.Equal(t, tmplPull, ad.Spec.InitContainer.ImagePullPolicy)

	// Per-agent value wins over template.
	perAgent := corev1.PullPolicy("IfNotPresent")
	as.Spec.Agents[0].InitContainer = &kmv1.ContainerTemplate{ImagePullPolicy: perAgent}
	out, err = r.buildDesired(as)
	require.NoError(t, err)
	ad = out["greeter-alpha"]
	require.NotNil(t, ad.Spec.InitContainer)
	assert.Equal(t, perAgent, ad.Spec.InitContainer.ImagePullPolicy,
		"per-agent value should beat the template default")
}

func TestBuildAgentDeploys_InitContainerFieldMerge(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			InitContainer: &kmv1.ContainerTemplate{
				Env: []corev1.EnvVar{{Name: "FROM_TEMPLATE", Value: "t"}},
			},
		},
	}
	// The agent sets its own init container env; the template's env must still
	// merge in rather than being dropped wholesale.
	as.Spec.Agents[0].InitContainer = &kmv1.ContainerTemplate{
		Env: []corev1.EnvVar{{Name: "FROM_AGENT", Value: "a"}},
	}

	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out["greeter-alpha"]

	require.NotNil(t, ad.Spec.InitContainer)
	names := map[string]string{}
	for _, e := range ad.Spec.InitContainer.Env {
		names[e.Name] = e.Value
	}
	assert.Equal(t, "a", names["FROM_AGENT"], "per-agent init container env kept")
	assert.Equal(t, "t", names["FROM_TEMPLATE"], "template init container env merged in, not dropped")
}

func TestBuildAgentDeploys_TemplatePodFieldsApplied(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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

func TestNeedsUpdate(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
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
	child := func(desired map[string]*kmv1.AgentDeploy, name string, phase kmv1.AgentDeployPhase, ready, want uint32) *kmv1.AgentDeploy {
		ad := desired[name].DeepCopy()
		ad.Status.Phase = phase
		ad.Status.ReadyReplicas = ready
		ad.Status.DesiredReplicas = want
		return ad
	}

	tests := []struct {
		name          string
		agents        []string
		existing      func(desired map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy
		wantPhase     kmv1.AgentSetPhase
		wantCondition metav1.ConditionStatus
		wantReason    string
		wantMessage   string
	}{
		{
			name:   "all children running",
			agents: []string{"alpha", "beta"},
			existing: func(d map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{
					"greeter-alpha": child(d, "greeter-alpha", kmv1.AgentDeployPhaseRunning, 1, 1),
					"greeter-beta":  child(d, "greeter-beta", kmv1.AgentDeployPhaseRunning, 1, 1),
				}
			},
			wantPhase:     kmv1.AgentSetPhaseRunning,
			wantCondition: metav1.ConditionTrue,
			wantReason:    "Successful",
		},
		{
			name:   "children not created yet are starting, not failed",
			agents: []string{"alpha"},
			existing: func(map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{}
			},
			wantPhase:     kmv1.AgentSetPhaseUnknown,
			wantCondition: metav1.ConditionUnknown,
			wantReason:    "AgentsStarting",
			wantMessage:   "waiting for agents: [greeter-alpha: starting]",
		},
		{
			name:   "children with pods not ready are starting, not failed",
			agents: []string{"alpha", "beta"},
			existing: func(d map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{
					"greeter-alpha": child(d, "greeter-alpha", kmv1.AgentDeployPhaseRunning, 1, 1),
					"greeter-beta":  child(d, "greeter-beta", kmv1.AgentDeployPhaseUnknown, 0, 1),
				}
			},
			wantPhase:     kmv1.AgentSetPhaseUnknown,
			wantCondition: metav1.ConditionUnknown,
			wantReason:    "AgentsStarting",
			wantMessage:   "waiting for agents: [greeter-beta: 0/1 ready]",
		},
		{
			name:   "a partially ready child and a missing child are both listed, sorted",
			agents: []string{"alpha", "beta"},
			existing: func(d map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{
					"greeter-alpha": child(d, "greeter-alpha", kmv1.AgentDeployPhaseUnknown, 1, 2),
				}
			},
			wantPhase:     kmv1.AgentSetPhaseUnknown,
			wantCondition: metav1.ConditionUnknown,
			wantReason:    "AgentsStarting",
			wantMessage:   "waiting for agents: [greeter-alpha: 1/2 ready, greeter-beta: starting]",
		},
		{
			name:   "a failed child fails the set",
			agents: []string{"alpha", "beta"},
			existing: func(d map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{
					"greeter-alpha": child(d, "greeter-alpha", kmv1.AgentDeployPhaseRunning, 1, 1),
					"greeter-beta":  child(d, "greeter-beta", kmv1.AgentDeployPhaseFailed, 0, 1),
				}
			},
			wantPhase:     kmv1.AgentSetPhaseFailed,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "AgentsUnhealthy",
			wantMessage:   "unhealthy agents: [greeter-beta: Failed]",
		},
		{
			name:   "failed wins over starting, and names are sorted",
			agents: []string{"alpha", "beta", "gamma"},
			existing: func(d map[string]*kmv1.AgentDeploy) map[string]*kmv1.AgentDeploy {
				return map[string]*kmv1.AgentDeploy{
					"greeter-gamma": child(d, "greeter-gamma", kmv1.AgentDeployPhaseFailed, 0, 1),
					"greeter-alpha": child(d, "greeter-alpha", kmv1.AgentDeployPhaseFailed, 0, 1),
					"greeter-beta":  child(d, "greeter-beta", kmv1.AgentDeployPhaseUnknown, 0, 1),
				}
			},
			wantPhase:     kmv1.AgentSetPhaseFailed,
			wantCondition: metav1.ConditionFalse,
			wantReason:    "AgentsUnhealthy",
			wantMessage:   "unhealthy agents: [greeter-alpha: Failed, greeter-gamma: Failed]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
			as := newAgentSet("greeter", tt.agents...)
			as.Status.InitializeConditions(kmv1.AgentSetConditionAgentsHealthy)
			desired, err := r.buildDesired(as)
			require.NoError(t, err)

			r.aggregateChildHealth(as, desired, tt.existing(desired))

			assert.Equal(t, tt.wantPhase, as.Status.Phase)
			assert.Equal(t, tt.wantMessage, as.Status.Message)
			cond := as.Status.GetCondition(kmv1.AgentSetConditionAgentsHealthy)
			require.NotNil(t, cond)
			assert.Equal(t, tt.wantCondition, cond.Status)
			assert.Equal(t, tt.wantReason, cond.Reason)
		})
	}
}

func TestAggregateChildHealth_NoAgents(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter")
	as.Status.InitializeConditions(kmv1.AgentSetConditionAgentsHealthy)

	r.aggregateChildHealth(as, map[string]*kmv1.AgentDeploy{}, map[string]*kmv1.AgentDeploy{})

	assert.Equal(t, kmv1.AgentSetPhaseUnknown, as.Status.Phase)
	cond := as.Status.GetCondition(kmv1.AgentSetConditionAgentsHealthy)
	require.NotNil(t, cond)
	assert.Equal(t, "NoAgents", cond.Reason)
}
