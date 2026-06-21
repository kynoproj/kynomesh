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

package agentdeploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestNewPod_NamingAndDNSWiring(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	hash := "abc123"
	pod := newPod(ad, 2, corev1.PodSpec{Containers: []corev1.Container{{Name: kmv1.ContainerNameAgent}}}, hash)

	// Pod name: <deploy>-<replica>-<rand5>
	assert.Regexp(t, `^greeter-2-[a-z0-9]{5}$`, pod.Name)
	// Stable DNS hostname: <deploy>-<replica>
	assert.Equal(t, "greeter-2", pod.Spec.Hostname)
	assert.Equal(t, "greeter-headless", pod.Spec.Subdomain)
	// Replica index carried in both label and annotation.
	assert.Equal(t, "2", pod.Labels[kmv1.KeyReplica])
	assert.Equal(t, "2", pod.Annotations[kmv1.KeyReplica])
	assert.Equal(t, hash, pod.Annotations[kmv1.KeyHash])
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAppName])
	// Controller-namespaced identity labels: this is what listOwnedPods,
	// the headless Service selector, and Status.Selector all key on.
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, pod.Labels[kmv1.KeyAgentSetName])
	assert.Equal(t, kmv1.ControllerAgentDeploy, pod.Labels[kmv1.KeyManagedBy])
	require.Len(t, pod.OwnerReferences, 1)
	assert.True(t, *pod.OwnerReferences[0].Controller)
}

func TestNewPod_LabelsUseSpecNameNotMetadataName(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Name = "greeter-set-greeter" // compound metadata name (what AgentSet controller produces)
	ad.Spec.Name = "greeter"        // bare agent name

	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "greeter", pod.Labels[kmv1.KeyAgentDeployName],
		"KeyAgentDeployName must be the bare ad.Spec.Name, not the compound ad.Name")
	// KeyAppName intentionally keeps the compound metadata name — it's
	// the kubectl/dashboards-facing convention.
	assert.Equal(t, "greeter-set-greeter", pod.Labels[kmv1.KeyAppName])
	// Pod name still derives from the compound metadata name so it's
	// globally unique within the namespace.
	assert.Contains(t, pod.Name, "greeter-set-greeter-")
}

func TestNewPod_ProjectsAgentSetNameFromSpec(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.AgentSetName = "greeter-set"
	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "greeter-set", pod.Labels[kmv1.KeyAgentSetName])
}

func TestNewPod_EntryLabelStampedWhenIsEntry(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Topology.IsEntry = true
	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "true", pod.Labels[kmv1.KeyEntry])
}

func TestNewPod_EntryLabelAbsentWhenNotEntry(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Spec.Topology.IsEntry = false
	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	_, ok := pod.Labels[kmv1.KeyEntry]
	assert.False(t, ok, "non-entry pods must not carry the entry label")
}

func TestNewPod_StampsServingLabel(t *testing.T) {
	// Pods join the ClusterIP rotation by default. Operators flip this to
	// "false" (or remove it) to drain a pod without deleting it.
	ad := newAgentDeploy("greeter", 1)
	pod := newPod(ad, 0, corev1.PodSpec{}, "h")
	assert.Equal(t, "true", pod.Labels[kmv1.KeyServing])
}

func TestDesiredReplicas(t *testing.T) {
	zero, neg := int32(0), int32(-3)
	cases := []struct {
		name string
		in   *int32
		want int
	}{
		{"nil defaults to 1", nil, 1},
		{"zero", &zero, 0},
		{"negative clamps to 0", &neg, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ad := &kmv1.AgentDeploy{Spec: kmv1.AgentDeploySpec{Replicas: tc.in}}
			assert.Equal(t, tc.want, desiredReplicas(ad))
		})
	}
}

func TestGroupPodsByReplica(t *testing.T) {
	pods := []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "0"}}},
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "1"}}},
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "1"}}}, // duplicate
		{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{kmv1.KeyReplica: "notanint"}}},
	}
	grouped := groupPodsByReplica(pods)
	assert.Len(t, grouped[0], 1)
	assert.Len(t, grouped[1], 2)
	assert.Len(t, grouped[-1], 1, "invalid annotation bucketed under -1")
}
