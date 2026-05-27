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

func mustScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kmv1.AddToScheme(scheme))
	return scheme
}

func newAgentSet(name string, agents ...string) *kmv1.AgentSet {
	spec := kmv1.AgentSetSpec{}
	for _, a := range agents {
		spec.Agents = append(spec.Agents, kmv1.AbstractAgentDeploy{Name: a})
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
	r := NewReconciler(c, scheme, nil, &events.FakeRecorder{})
	return r, c
}

func reconcileRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: name}}
}

func TestBuildAgentDeploys(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
	as := newAgentSet("greeter", "alpha", "beta")
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	require.Len(t, out, 2)

	for _, agent := range []string{"alpha", "beta"} {
		ad, ok := out[childName("greeter", agent)]
		require.True(t, ok, "missing child for %s", agent)
		assert.Equal(t, testNamespace, ad.Namespace)
		assert.Equal(t, agent, ad.Spec.Name)
		// AgentSetName lives in spec (source of truth, used by AgentDeploy
		// controller to project the label onto pods) and as a metadata
		// label (so kubectl get agentdeploy -l ... works).
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
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
	tmplPull := corev1.PullPolicy("Always")
	as := newAgentSet("greeter", "alpha")
	as.Spec.Templates = &kmv1.Templates{
		AgentDeployTemplate: &kmv1.AgentDeployTemplate{
			ContainerTemplate: &kmv1.ContainerTemplate{ImagePullPolicy: tmplPull},
		},
	}
	out, err := r.buildDesired(as)
	require.NoError(t, err)
	ad := out[childName("greeter", "alpha")]
	require.NotNil(t, ad.Spec.ContainerTemplate)
	assert.Equal(t, tmplPull, ad.Spec.ContainerTemplate.ImagePullPolicy)

	// Per-agent value wins over template.
	perAgent := corev1.PullPolicy("IfNotPresent")
	as.Spec.Agents[0].ContainerTemplate = &kmv1.ContainerTemplate{ImagePullPolicy: perAgent}
	out, err = r.buildDesired(as)
	require.NoError(t, err)
	ad = out[childName("greeter", "alpha")]
	require.NotNil(t, ad.Spec.ContainerTemplate)
	assert.Equal(t, perAgent, ad.Spec.ContainerTemplate.ImagePullPolicy,
		"per-agent value should beat the template default")
}

func TestNeedsUpdate(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
	desired, err := r.buildDesired(newAgentSet("greeter", "alpha"))
	require.NoError(t, err)
	want := desired[childName("greeter", "alpha")]
	existing := want.DeepCopy()
	assert.False(t, needsUpdate(existing, want), "identical children should not trigger update")

	drifted := want.DeepCopy()
	drifted.Annotations[kmv1.KeyHash] = "stale"
	assert.True(t, needsUpdate(drifted, want), "different hash should trigger update")
}

func TestValidateAgentSet(t *testing.T) {
	tests := []struct {
		name    string
		agents  []string
		wantErr string
	}{
		{name: "no agents", agents: nil},
		{name: "ok", agents: []string{"a", "b"}},
		{name: "empty name", agents: []string{""}, wantErr: "non-empty"},
		{name: "duplicate", agents: []string{"a", "a"}, wantErr: "duplicate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAgentSet(newAgentSet("x", tc.agents...))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestAddRemoveFinalizer(t *testing.T) {
	as := newAgentSet("g")
	addFinalizer(as)
	addFinalizer(as) // idempotent
	assert.Equal(t, []string{FinalizerName}, as.Finalizers)
	removeFinalizer(as)
	assert.Empty(t, as.Finalizers)
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
	assert.Contains(t, got.Finalizers, FinalizerName)
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
	r0 := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
	desired, err := r0.buildDesired(as)
	require.NoError(t, err)
	stale := desired[childName("greeter", "alpha")].DeepCopy()
	stale.Annotations[kmv1.KeyHash] = "stale"
	stale.Spec.Replicas = nil

	r, c := newTestReconciler(t, as, stale)
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "greeter-alpha"}, &got))
	assert.NotEqual(t, "stale", got.Annotations[kmv1.KeyHash], "controller should refresh hash on drift")
}

func TestReconcile_DeletionCleansChildrenAndFinalizer(t *testing.T) {
	now := metav1.NewTime(time.Now())
	as := newAgentSet("greeter", "alpha")
	as.DeletionTimestamp = &now
	as.Finalizers = []string{FinalizerName}

	r0 := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
	desired, err := r0.buildDesired(as)
	require.NoError(t, err)
	child := desired[childName("greeter", "alpha")].DeepCopy()

	r, c := newTestReconciler(t, as, child)
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var list kmv1.AgentDeployList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	assert.Empty(t, list.Items, "children should be deleted on AgentSet deletion")

	// Removing the last finalizer while DeletionTimestamp is set lets the
	// API server (and the fake client) complete deletion immediately, so
	// either Get-not-found or finalizer-absent is acceptable.
	var got kmv1.AgentSet
	err = c.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "greeter"}, &got)
	if err == nil {
		assert.NotContains(t, got.Finalizers, FinalizerName)
	}
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

func TestAggregateChildHealth(t *testing.T) {
	r := NewReconciler(nil, mustScheme(t), nil, &events.FakeRecorder{})
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
