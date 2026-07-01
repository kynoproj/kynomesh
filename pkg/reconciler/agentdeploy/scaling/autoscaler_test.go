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

package scaling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// seedStore creates a store for ad in reg and records the given samples.
func seedStore(t *testing.T, reg *Registry, ad *kmv1.AgentDeploy, samples ...Sample) {
	t.Helper()
	store, err := reg.StoreFor(context.Background(), ad)
	require.NoError(t, err)
	for _, s := range samples {
		store.Record(s, "h")
	}
}

func specReplicasOf(t *testing.T, c client.Client, name string) int32 {
	t.Helper()
	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: name}, &got))
	return specReplicas(&got)
}

// newTestAutoscaler builds an Autoscaler with a fixed clock over the registry.
func newTestAutoscaler(c client.Client, reg *Registry, now time.Time) *Autoscaler {
	return NewAutoscaler(c, NewWatchSet(reg), reg, testLogger(),
		WithAutoscalerClock(func() time.Time { return now }))
}

func TestAutoscalerScalesUpAndPatchesSpec(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10), TargetSaturationPercentage: ptrU32(100)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	// Heavy load on a single replica → cold-start target 12 → surge.
	seedStore(t, reg, ad, sample(now, 1, 80, 160))

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))

	assert.Greater(t, specReplicasOf(t, c, "foo"), int32(1), "spec.replicas scaled up")
}

func TestAutoscalerNoChangeLeavesSpecAlone(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 4)
	ad.Spec.Replicas = ptrI32(4)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10), TargetSaturationPercentage: ptrU32(100)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	// total 48, target 12 → desired ceil(48/12)=4 == current → no change.
	seedStore(t, reg, ad, sample(now, 4, 12, 60))

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))

	assert.Equal(t, int32(4), specReplicasOf(t, c, "foo"))
}

func TestAutoscalerSkipsDisabled(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Disabled: true, Max: ptrI32(10)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	seedStore(t, reg, ad, sample(now, 1, 80, 160))

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))
	assert.Equal(t, int32(1), specReplicasOf(t, c, "foo"), "disabled is left untouched")
}

func TestAutoscalerSkipsWhenNotSampled(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c) // empty — no store for foo

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))
	assert.Equal(t, int32(1), specReplicasOf(t, c, "foo"), "no history → no scaling")
}

func TestAutoscalerUsesStatusReplicasAsCurrent(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 5)
	ad.Status.Replicas = 5       // actual running count
	ad.Spec.Replicas = ptrI32(1) // stale spec value
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10), TargetSaturationPercentage: ptrU32(100)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	// total 55, cold target 12 → desired ceil(55/12)=5 == current(status 5) → no change.
	// Had it used Spec.Replicas=1, desired 5 != 1 would have patched spec upward.
	seedStore(t, reg, ad, sample(now, 5, 11, 220))

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))
	assert.Equal(t, int32(1), specReplicasOf(t, c, "foo"),
		"decided from Status.Replicas=5 (no change), not Spec.Replicas=1")
}

func TestAutoscalerSkipsStaleSamples(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10), TargetSaturationPercentage: ptrU32(100)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	// Freshest sample is 10 minutes old; default maxSampleAge is 2 minutes.
	seedStore(t, reg, ad, sample(now.Add(-10*time.Minute), 1, 80, 160))

	require.NoError(t, newTestAutoscaler(c, reg, now).scaleKey(context.Background(), nn("foo")))
	assert.Equal(t, int32(1), specReplicasOf(t, c, "foo"), "stale metrics → no scaling")
}
