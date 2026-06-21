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

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestNewHeadlessService_SelectorUsesSpecName(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	ad.Name = "greeter-set-greeter"
	ad.Spec.Name = "greeter"

	svc := newHeadlessService(ad)
	assert.Equal(t, "greeter", svc.Spec.Selector[kmv1.KeyAgentDeployName])
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAgentDeployName])
	// Service name still derives from the compound metadata name.
	assert.Equal(t, "greeter-set-greeter-headless", svc.Name)
}

func TestNewHeadlessService(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	svc := newHeadlessService(ad)
	assert.Equal(t, "greeter-headless", svc.Name)
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
	assert.True(t, svc.Spec.PublishNotReadyAddresses)
	// Selector keys on the controller-namespaced AgentDeploy + AgentSet
	// labels so it doesn't conflate with the generic app.kubernetes.io/name.
	assert.Equal(t, "greeter", svc.Spec.Selector[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, svc.Spec.Selector[kmv1.KeyAgentSetName])
	assert.Equal(t, kmv1.ControllerAgentDeploy, svc.Spec.Selector[kmv1.KeyManagedBy])
	// Metadata labels: KeyAppName for kubectl/dashboards; KeyAgentDeployName
	// and KeyAgentSetName for the controller's own selectors.
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAppName])
	assert.Equal(t, "greeter", svc.Labels[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, svc.Labels[kmv1.KeyAgentSetName])
	require.Len(t, svc.OwnerReferences, 1)
}

func TestNewClusterIPService(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	svc := newClusterIPService(ad)
	assert.Equal(t, "greeter", svc.Name)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	assert.NotEqual(t, corev1.ClusterIPNone, svc.Spec.ClusterIP, "must not be headless")
	assert.False(t, svc.Spec.PublishNotReadyAddresses, "only Ready pods receive client traffic")
	// Selector matches the pod labels stamped by newPod, so kube-proxy
	// load-balances across the deploy's replicas.
	assert.Equal(t, "greeter", svc.Spec.Selector[kmv1.KeyAgentDeployName])
	assert.Equal(t, ad.Spec.AgentSetName, svc.Spec.Selector[kmv1.KeyAgentSetName])
	assert.Equal(t, kmv1.ControllerAgentDeploy, svc.Spec.Selector[kmv1.KeyManagedBy])
	// Serving label gates pods into rotation; flipping it drains a pod.
	assert.Equal(t, "true", svc.Spec.Selector[kmv1.KeyServing])
	// Exposes the broker port; introspect stays internal.
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, "broker", svc.Spec.Ports[0].Name)
	assert.Equal(t, int32(kmv1.AgentBrokerPort), svc.Spec.Ports[0].Port)
	assert.Equal(t, corev1.ProtocolTCP, svc.Spec.Ports[0].Protocol)
	require.Len(t, svc.OwnerReferences, 1)
}

func TestNewHeadlessService_DoesNotGateOnServing(t *testing.T) {
	// Headless service serves per-pod DNS for every replica — including
	// drained pods — so its selector must NOT include KeyServing.
	ad := newAgentDeploy("greeter", 1)
	svc := newHeadlessService(ad)
	_, has := svc.Spec.Selector[kmv1.KeyServing]
	assert.False(t, has, "headless selector must not depend on the serving label")
}
