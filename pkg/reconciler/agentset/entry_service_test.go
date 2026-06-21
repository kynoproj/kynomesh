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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestNewEntryService(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	as := newAgentSet("greeter", "alpha", "beta")

	svc, err := r.newEntryService(as)
	require.NoError(t, err)

	assert.Equal(t, "greeter-ingress", svc.Name)
	assert.Equal(t, testNamespace, svc.Namespace)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)

	assert.Equal(t, map[string]string{
		kmv1.KeyAgentSetName: "greeter",
		kmv1.KeyManagedBy:    kmv1.ControllerAgentDeploy,
		kmv1.KeyEntry:        "true",
		kmv1.KeyServing:      "true",
	}, svc.Spec.Selector)

	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, "broker", svc.Spec.Ports[0].Name)
	assert.Equal(t, int32(kmv1.AgentBrokerPort), svc.Spec.Ports[0].Port)

	require.Len(t, svc.OwnerReferences, 1, "controller reference must be set")
	assert.Equal(t, "greeter", svc.OwnerReferences[0].Name)
	assert.True(t, *svc.OwnerReferences[0].Controller)
}

func TestReconcileEntryService_Creates(t *testing.T) {
	as := newAgentSet("greeter", "alpha")
	r, c := newTestReconciler(t, as)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Namespace: testNamespace, Name: "greeter-ingress"}, &svc))
	assert.Equal(t, "true", svc.Spec.Selector[kmv1.KeyEntry])
	assert.NotEmpty(t, svc.Annotations[kmv1.KeyHash])
}

func TestReconcileEntryService_RecreatesOnDrift(t *testing.T) {
	as := newAgentSet("greeter", "alpha")
	drifted := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testNamespace,
			Name:        as.EntryServiceName(),
			Annotations: map[string]string{kmv1.KeyHash: "stale"},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				kmv1.KeyAgentSetName: "greeter",
				kmv1.KeyEntry:        "wrong",
			},
			Ports: []corev1.ServicePort{{Name: "broker", Port: 1}},
		},
	}
	r, c := newTestReconciler(t, as, drifted)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Namespace: testNamespace, Name: "greeter-ingress"}, &svc))
	assert.NotEqual(t, "stale", svc.Annotations[kmv1.KeyHash], "stale hash should be refreshed")
	assert.Equal(t, "true", svc.Spec.Selector[kmv1.KeyEntry])
}

