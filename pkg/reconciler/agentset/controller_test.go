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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

const testNamespace = "test-ns"

// mustScheme is shared across the per-component test files in this
// package (controller_test, agentdeploys_test, entry_service_test,
// daemon_test).
func mustScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kmv1.AddToScheme(scheme))
	return scheme
}

func newAgentSet(name string, agents ...string) *kmv1.AgentSet {
	spec := kmv1.AgentSetSpec{
		Pattern: kmv1.AgentPatternSupervisor,
	}
	for _, a := range agents {
		spec.Agents = append(spec.Agents, kmv1.AbstractAgentDeploy{Name: a})
	}
	if len(agents) > 0 {
		spec.Entry = agents[0]
	}
	return &kmv1.AgentSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			UID:        types.UID("uid-" + name),
			Generation: 1,
		},
		Spec: spec,
	}
}

// newTestReconciler wires up a controller-runtime fake client and Reconciler
// seeded with the supplied objects. The AgentSet status subresource is
// enabled so r.Status().Patch behaves like the real API server.
func newTestReconciler(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := mustScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&kmv1.AgentSet{}, &kmv1.AgentDeploy{}).
		Build()
	r := NewReconciler(c, scheme, nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	return r, c
}

func reconcileRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: name}}
}

func TestReconcile_CreatesChildren(t *testing.T) {
	as := newAgentSet("greeter", "alpha", "beta")
	r, c := newTestReconciler(t, as)

	res, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Zero(t, res.RequeueAfter)

	var list kmv1.AgentDeployList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	assert.Len(t, list.Items, 2)

	var got kmv1.AgentSet
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "greeter"}, &got))
	cond := got.Status.GetCondition(kmv1.AgentSetConditionConfigured)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestReconcile_DeletesOrphans(t *testing.T) {
	as := newAgentSet("greeter", "alpha")
	orphan := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "greeter-gone",
			Labels:    map[string]string{kmv1.KeyAgentSetName: "greeter"},
		},
	}
	r, c := newTestReconciler(t, as, orphan)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var list kmv1.AgentDeployList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	names := make([]string, 0, len(list.Items))
	for _, ad := range list.Items {
		names = append(names, ad.Name)
	}
	assert.ElementsMatch(t, []string{"greeter-alpha"}, names)
}

func TestReconcile_UpdatesDriftedChild(t *testing.T) {
	as := newAgentSet("greeter", "alpha")
	r0 := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	desired, err := r0.buildDesired(as)
	require.NoError(t, err)
	stale := desired["greeter-alpha"].DeepCopy()
	stale.Annotations[kmv1.KeyHash] = "stale"
	stale.Spec.Replicas = nil

	r, c := newTestReconciler(t, as, stale)
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "greeter-alpha"}, &got))
	assert.NotEqual(t, "stale", got.Annotations[kmv1.KeyHash], "controller should refresh hash on drift")
}

func TestReconcile_DeletionTimestampIsNoop(t *testing.T) {
	now := metav1.NewTime(time.Now())
	as := newAgentSet("greeter", "alpha")
	as.DeletionTimestamp = &now
	as.Finalizers = []string{"placeholder"}

	r0 := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{}, "test-image:latest", corev1.PullIfNotPresent)
	desired, err := r0.buildDesired(as)
	require.NoError(t, err)
	child := desired["greeter-alpha"].DeepCopy()

	r, c := newTestReconciler(t, as, child)
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var list kmv1.AgentDeployList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	assert.Len(t, list.Items, 1, "reconciler must not delete children itself; GC handles it")
}

func TestReconcile_DuplicateAgentNameMarksFailed(t *testing.T) {
	as := newAgentSet("greeter", "alpha", "alpha")
	r, c := newTestReconciler(t, as)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got kmv1.AgentSet
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "greeter"}, &got))
	assert.Equal(t, kmv1.AgentSetPhaseFailed, got.Status.Phase)
	cond := got.Status.GetCondition(kmv1.AgentSetConditionConfigured)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
}

func TestReconcile_NotFoundIsNoop(t *testing.T) {
	r, _ := newTestReconciler(t)
	res, err := r.Reconcile(context.Background(), reconcileRequest("missing"))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}
