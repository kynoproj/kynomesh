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
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

const (
	testNamespace   = "test-ns"
	testBrokerImage = "quay.io/kynoproj/kynomesh:test"
)

// mustScheme is shared across the per-component test files in this
// package (controller_test, pods_test, pod_spec_test, services_test).
func mustScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kmv1.AddToScheme(scheme))
	return scheme
}

func newAgentDeploy(name string, replicas int32) *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  testNamespace,
			UID:        types.UID("uid-" + name),
			Generation: 1,
		},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: name},
			Replicas:            &replicas,
			AgentSetName:        name + "-set",
		},
	}
}

func newTestReconciler(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := mustScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&kmv1.AgentDeploy{}).
		Build()
	r := NewReconciler(c, scheme, nil, nil, &events.FakeRecorder{}, testBrokerImage, "", nil)
	return r, c
}

func reconcileRequest(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: name}}
}

func listPods(t *testing.T, c client.Client) []corev1.Pod {
	t.Helper()
	var list corev1.PodList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	return list.Items
}

func TestReconcile_CreatesPodsAndService(t *testing.T) {
	ad := newAgentDeploy("greeter", 3)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods := listPods(t, c)
	assert.Len(t, pods, 3, "one pod per replica")

	indices := map[string]bool{}
	for _, p := range pods {
		indices[p.Annotations[kmv1.KeyReplica]] = true
		require.Len(t, p.OwnerReferences, 1)
		assert.True(t, *p.OwnerReferences[0].Controller)
	}
	assert.True(t, indices["0"] && indices["1"] && indices["2"])

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &svc))
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)

	var clusterIP corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &clusterIP))
	assert.Equal(t, corev1.ServiceTypeClusterIP, clusterIP.Spec.Type)
	require.Len(t, clusterIP.Spec.Ports, 1)
	assert.Equal(t, int32(kmv1.AgentBrokerPort), clusterIP.Spec.Ports[0].Port)
}

func TestReconcile_ScaleUp(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 1)

	// Bump replicas to 3 and reconcile again.
	var live kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &live))
	three := int32(3)
	live.Spec.Replicas = &three
	require.NoError(t, c.Update(context.Background(), &live))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 3)
}

func TestReconcile_ScaleDown(t *testing.T) {
	ad := newAgentDeploy("greeter", 3)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.Len(t, listPods(t, c), 3)

	var live kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &live))
	one := int32(1)
	live.Spec.Replicas = &one
	require.NoError(t, c.Update(context.Background(), &live))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	pods := listPods(t, c)
	assert.Len(t, pods, 1)
	assert.Equal(t, "0", pods[0].Annotations[kmv1.KeyReplica], "replica 0 should survive scale-down")
}

func TestReconcile_HashDriftRecreatesPod(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	pods := listPods(t, c)
	require.Len(t, pods, 1)
	originalName := pods[0].Name

	pods[0].Annotations[kmv1.KeyHash] = "stale"
	require.NoError(t, c.Update(context.Background(), &pods[0]))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods = listPods(t, c)
	require.Len(t, pods, 1, "stale pod replaced by current-hash one")
	assert.NotEqual(t, originalName, pods[0].Name, "delete-and-recreate produces a new name")
	assert.NotEqual(t, "stale", pods[0].Annotations[kmv1.KeyHash])
}

func TestReconcile_DeletesOrphanedReplica(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	orphan := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "greeter-5-zzzzz",
			Labels: map[string]string{
				kmv1.KeyAppName:         "greeter",
				kmv1.KeyAgentDeployName: "greeter",
				kmv1.KeyAgentSetName:    "greeter-set",
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
				kmv1.KeyReplica:         "5",
			},
			Annotations: map[string]string{kmv1.KeyReplica: "5"},
		},
	}
	r, c := newTestReconciler(t, ad, orphan)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods := listPods(t, c)
	for _, p := range pods {
		idx, _ := strconv.Atoi(p.Annotations[kmv1.KeyReplica])
		assert.Less(t, idx, 1, "orphan replica index should be deleted")
	}
}

func TestReconcile_ServiceDriftRecreates(t *testing.T) {
	ad := newAgentDeploy("greeter", 1)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var svc corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &svc))
	svc.Annotations[kmv1.KeyHash] = "stale"
	require.NoError(t, c.Update(context.Background(), &svc))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got corev1.Service
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter-headless"}, &got))
	assert.NotEqual(t, "stale", got.Annotations[kmv1.KeyHash], "stale service should be replaced")
}

func TestReconcile_DeletionTimestampIsNoop(t *testing.T) {
	now := metav1.NewTime(time.Now())
	ad := newAgentDeploy("greeter", 2)
	ad.DeletionTimestamp = &now
	ad.Finalizers = []string{"placeholder"}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "greeter-0-aaaaa",
			Labels: map[string]string{
				kmv1.KeyAppName:         "greeter",
				kmv1.KeyAgentDeployName: "greeter",
				kmv1.KeyAgentSetName:    "greeter-set",
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
				kmv1.KeyReplica:         "0",
			},
			Annotations: map[string]string{kmv1.KeyReplica: "0"},
		},
	}
	r, c := newTestReconciler(t, ad, pod)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	// Reconciler must not have deleted the child — that's GC's job.
	assert.Len(t, listPods(t, c), 1, "reconciler must not delete children itself; GC handles it")
}

func TestReconcile_StatusReadyCount(t *testing.T) {
	ad := newAgentDeploy("greeter", 2)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	pods := listPods(t, c)
	require.Len(t, pods, 2)
	pods[0].Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	require.NoError(t, c.Status().Update(context.Background(), &pods[0]))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &got))
	assert.Equal(t, uint32(2), got.Status.DesiredReplicas)
	assert.Equal(t, uint32(2), got.Status.Replicas)
	assert.Equal(t, uint32(1), got.Status.ReadyReplicas)
	wantSelector := fmt.Sprintf("%s=%s,%s=greeter,%s=%s",
		kmv1.KeyAgentSetName, got.Spec.AgentSetName,
		kmv1.KeyAgentDeployName,
		kmv1.KeyManagedBy, kmv1.ControllerAgentDeploy)
	assert.Equal(t, wantSelector, got.Status.Selector)
}

func TestReconcile_NotFoundIsNoop(t *testing.T) {
	r, _ := newTestReconciler(t)
	res, err := r.Reconcile(context.Background(), reconcileRequest("missing"))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)
}

// markAllPodsReady flips every pod's PodReady condition to True via the
// status subresource. Used to advance the rolling-update wait gate
// between batches.
func markAllPodsReady(t *testing.T, c client.Client) {
	t.Helper()
	var list corev1.PodList
	require.NoError(t, c.List(context.Background(), &list, client.InNamespace(testNamespace)))
	for i := range list.Items {
		p := &list.Items[i]
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		require.NoError(t, c.Status().Update(context.Background(), p))
	}
}

func TestReconcile_RollingUpdate_RespectsMaxUnavailable(t *testing.T) {
	ad := newAgentDeploy("greeter", 4)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.Len(t, listPods(t, c), 4)
	markAllPodsReady(t, c)

	pods := listPods(t, c)
	for i := range pods {
		pods[i].Annotations[kmv1.KeyHash] = "stale"
		require.NoError(t, c.Update(context.Background(), &pods[i]))
	}

	stalePerPass := []int{3, 2, 1, 0}
	for pass, wantStale := range stalePerPass {
		_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
		require.NoError(t, err, "pass %d", pass)

		gotStale := 0
		for _, p := range listPods(t, c) {
			if p.Annotations[kmv1.KeyHash] == "stale" {
				gotStale++
			}
		}
		assert.Equal(t, wantStale, gotStale, "pass %d: only one slot replaced per pass", pass)
		markAllPodsReady(t, c)
	}

	finalPods := listPods(t, c)
	assert.Len(t, finalPods, 4)
	for _, p := range finalPods {
		assert.NotEqual(t, "stale", p.Annotations[kmv1.KeyHash])
	}
}

func TestReconcile_RollingUpdate_WaitGateBlocksUntilReady(t *testing.T) {
	ad := newAgentDeploy("greeter", 3)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	markAllPodsReady(t, c)

	for _, p := range listPods(t, c) {
		p.Annotations[kmv1.KeyHash] = "stale"
		require.NoError(t, c.Update(context.Background(), &p))
	}

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	staleAfter1 := countStale(listPods(t, c))
	assert.Equal(t, 2, staleAfter1)

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Equal(t, 2, countStale(listPods(t, c)), "wait gate must block until the new pod is Ready")
}

func countStale(pods []corev1.Pod) int {
	n := 0
	for _, p := range pods {
		if p.Annotations[kmv1.KeyHash] == "stale" {
			n++
		}
	}
	return n
}

func TestReconcile_RollingUpdate_InitialCreateNotGated(t *testing.T) {
	ad := newAgentDeploy("greeter", 4)
	ad.Spec.UpdateStrategy = kmv1.UpdateStrategy{
		Type: kmv1.RollingUpdateStrategyType,
		RollingUpdate: &kmv1.RollingUpdateStrategy{
			MaxUnavailable: ptr.To(intstr.FromInt(1)),
		},
	}
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	assert.Len(t, listPods(t, c), 4, "initial bring-up creates all slots in one pass")
}

func TestReconcile_RollingUpdate_NewSpecResetsUpdateCursor(t *testing.T) {
	ad := newAgentDeploy("greeter", 2)
	r, c := newTestReconciler(t, ad)

	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var afterInitial kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &afterInitial))
	firstHash := afterInitial.Status.UpdateHash
	require.NotEmpty(t, firstHash)

	afterInitial.Spec.Name = "renamed"
	require.NoError(t, c.Update(context.Background(), &afterInitial))

	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)

	var afterChange kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "greeter"}, &afterChange))
	assert.NotEqual(t, firstHash, afterChange.Status.UpdateHash, "UpdateHash must track the new spec")
}

type fakeScaler struct {
	tracked, forgot []types.NamespacedName
}

func (f *fakeScaler) Track(k types.NamespacedName)  { f.tracked = append(f.tracked, k) }
func (f *fakeScaler) Forget(k types.NamespacedName) { f.forgot = append(f.forgot, k) }

func TestReconcile_ManagesWatchSet(t *testing.T) {
	key := reconcileRequest("greeter").NamespacedName

	t.Run("scaling-enabled starts watching", func(t *testing.T) {
		r, _ := newTestReconciler(t, newAgentDeploy("greeter", 1))
		fs := &fakeScaler{}
		r.scaler = fs
		_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
		require.NoError(t, err)
		assert.Contains(t, fs.tracked, key)
		assert.Empty(t, fs.forgot)
	})

	t.Run("scaling-disabled is still watched", func(t *testing.T) {
		ad := newAgentDeploy("greeter", 1)
		ad.Spec.Scale.Disabled = true
		r, _ := newTestReconciler(t, ad)
		fs := &fakeScaler{}
		r.scaler = fs
		_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
		require.NoError(t, err)
		assert.Contains(t, fs.tracked, key, "sampling continues even while scaling is disabled")
		assert.Empty(t, fs.forgot)
	})

	t.Run("missing AgentDeploy is forgotten", func(t *testing.T) {
		r, _ := newTestReconciler(t) // no objects
		fs := &fakeScaler{}
		r.scaler = fs
		_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
		require.NoError(t, err)
		assert.Contains(t, fs.forgot, key)
	})
}

func TestReconcile_StampsLastScaledAtOnChange(t *testing.T) {
	r, c := newTestReconciler(t, newAgentDeploy("greeter", 1))
	adKey := client.ObjectKey{Namespace: testNamespace, Name: "greeter"}

	// Initial bring-up sets the target (0 → 1), which counts as a scale and
	// stamps LastScaledAt.
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	var ad kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), adKey, &ad))
	require.Equal(t, uint32(1), ad.Status.DesiredReplicas)
	assert.False(t, ad.Status.LastScaledAt.IsZero(), "initial target set stamps LastScaledAt")

	// Reconcile again with no change: LastScaledAt must not move.
	prev := ad.Status.LastScaledAt
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.NoError(t, c.Get(context.Background(), adKey, &ad))
	assert.Equal(t, prev, ad.Status.LastScaledAt, "no re-stamp when target is unchanged")

	// Change the target replica count: stamps again.
	three := int32(3)
	ad.Spec.Replicas = &three
	require.NoError(t, c.Update(context.Background(), &ad))
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	require.NoError(t, c.Get(context.Background(), adKey, &ad))
	require.Equal(t, uint32(3), ad.Status.DesiredReplicas)
	assert.False(t, ad.Status.LastScaledAt.IsZero(), "stamped when replica target changes")
}

func TestReconcile_ReplicaStatusFields(t *testing.T) {
	r, c := newTestReconciler(t, newAgentDeploy("greeter", 3))
	adKey := client.ObjectKey{Namespace: testNamespace, Name: "greeter"}
	get := func() kmv1.AgentDeploy {
		var g kmv1.AgentDeploy
		require.NoError(t, c.Get(context.Background(), adKey, &g))
		return g
	}

	// Create: 3 pods on the desired hash, none ready yet.
	_, err := r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	g := get()
	assert.Equal(t, uint32(3), g.Status.DesiredReplicas)
	assert.Equal(t, uint32(3), g.Status.Replicas)
	assert.Equal(t, uint32(3), g.Status.UpdatedReplicas, "all pods on the desired hash")
	assert.Equal(t, uint32(0), g.Status.ReadyReplicas, "fake pods start not-ready")
	assert.Equal(t, uint32(0), g.Status.UpdatedReadyReplicas)
	assert.NotEmpty(t, g.Status.UpdateHash)
	assert.Equal(t, g.Status.UpdateHash, g.Status.CurrentHash, "fully on desired hash after create")

	// All pods ready → ready counts fill in.
	markAllPodsReady(t, c)
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	g = get()
	assert.Equal(t, uint32(3), g.Status.ReadyReplicas)
	assert.Equal(t, uint32(3), g.Status.UpdatedReadyReplicas)

	// Scale down to 1 → every count tracks down together.
	g.Spec.Replicas = ptr.To[int32](1)
	require.NoError(t, c.Update(context.Background(), &g))
	_, err = r.Reconcile(context.Background(), reconcileRequest("greeter"))
	require.NoError(t, err)
	g = get()
	assert.Equal(t, uint32(1), g.Status.DesiredReplicas)
	assert.Equal(t, uint32(1), g.Status.Replicas)
	assert.Equal(t, uint32(1), g.Status.UpdatedReplicas)
	assert.Equal(t, uint32(1), g.Status.ReadyReplicas)
	assert.Equal(t, uint32(1), g.Status.UpdatedReadyReplicas)
}
