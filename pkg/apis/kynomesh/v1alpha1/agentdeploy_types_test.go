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
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func newFullAgentDeploy() *AgentDeploy {
	return &AgentDeploy{
		TypeMeta: metav1.TypeMeta{Kind: "AgentDeploy", APIVersion: "kynomesh.kyno.sh/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "ns-1",
			Name:            "greeter",
			UID:             "uid-greeter",
			ResourceVersion: "rv-7",
			Labels:          map[string]string{"app.kubernetes.io/name": "greeter"},
			Annotations:     map[string]string{"k": "v"},
			Finalizers:      []string{"kynomesh.kyno.sh/agentdeploy-controller"},
		},
		Spec: AgentDeploySpec{
			AbstractAgentDeploy: AbstractAgentDeploy{
				Name: "greeter",
				BrokerTemplate: &ContainerTemplate{
					ImagePullPolicy: corev1.PullAlways,
				},
				Volumes:        []corev1.Volume{{Name: "data"}},
				InitContainers: []corev1.Container{{Name: "init"}},
				Sidecars:       []corev1.Container{{Name: "side"}},
				UpdateStrategy: UpdateStrategy{Type: RollingUpdateStrategyType},
			},
			Replicas: ptr.To[int32](3),
		},
		Status: AgentDeployStatus{
			Phase:           AgentDeployPhaseRunning,
			Replicas:        3,
			DesiredReplicas: 3,
			ReadyReplicas:   3,
			UpdateHash:      "abc",
		},
	}
}

func TestSimpleCopy_KeepsOnlyNamespaceAndName(t *testing.T) {
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()

	assert.Equal(t, "ns-1", c.Namespace)
	assert.Equal(t, "greeter", c.Name)

	// Everything else on ObjectMeta should be zero.
	assert.Empty(t, c.UID, "UID must be dropped — server-set, churn-prone")
	assert.Empty(t, c.ResourceVersion, "ResourceVersion must be dropped")
	assert.Empty(t, c.Labels)
	assert.Empty(t, c.Annotations)
	assert.Empty(t, c.Finalizers)
}

func TestSimpleCopy_DropsTypeMeta(t *testing.T) {
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()
	assert.Empty(t, c.Kind)
	assert.Empty(t, c.APIVersion)
}

func TestSimpleCopy_DropsStatus(t *testing.T) {
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()
	assert.Equal(t, AgentDeployStatus{}, c.Status,
		"Status is downstream of pod creation — embedding it would create churn")
}

func TestSimpleCopy_ZeroesOrchestrationSpecFields(t *testing.T) {
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()

	// Pod-orchestration knobs that don't describe what the agent IS.
	assert.Nil(t, c.Spec.Replicas, "Replicas drives pod count, not agent identity")
	assert.Empty(t, c.Spec.Sidecars, "Sidecars live in the pod spec; broker doesn't need them")
	assert.Empty(t, c.Spec.InitContainers)
	assert.Empty(t, c.Spec.Volumes)
	assert.Equal(t, UpdateStrategy{}, c.Spec.UpdateStrategy)
}

func TestSimpleCopy_KeepsAgentIdentityFields(t *testing.T) {
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()

	// Agent identity / declared config — broker reads these.
	assert.Equal(t, "greeter", c.Spec.Name)
	require.NotNil(t, c.Spec.BrokerTemplate)
	assert.Equal(t, corev1.PullAlways, c.Spec.BrokerTemplate.ImagePullPolicy)
}

func TestSimpleCopy_DoesNotAliasSource(t *testing.T) {
	// Mutating the returned copy must not affect the source AgentDeploy —
	// the controller reuses ad in subsequent reconcile steps.
	ad := newFullAgentDeploy()
	c := ad.SimpleCopy()

	c.Spec.Name = "mutated"
	if c.Spec.BrokerTemplate != nil {
		c.Spec.BrokerTemplate.ImagePullPolicy = corev1.PullNever
	}

	assert.Equal(t, "greeter", ad.Spec.Name)
	require.NotNil(t, ad.Spec.BrokerTemplate)
	assert.Equal(t, corev1.PullAlways, ad.Spec.BrokerTemplate.ImagePullPolicy)
}

func TestAgentDeploy_HeadlessServiceName(t *testing.T) {
	ad := &AgentDeploy{}
	ad.Name = "myagent"
	assert.Equal(t, "myagent-headless", ad.HeadlessServiceName())

	empty := &AgentDeploy{}
	assert.Equal(t, "-headless", empty.HeadlessServiceName())
}

func TestAgentDeploy_ServiceName(t *testing.T) {
	ad := &AgentDeploy{}
	ad.Name = "myagent"
	assert.Equal(t, "myagent", ad.ServiceName())

	empty := &AgentDeploy{}
	assert.Equal(t, "", empty.ServiceName())
}
