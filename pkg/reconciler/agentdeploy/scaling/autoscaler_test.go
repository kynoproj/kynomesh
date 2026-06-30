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
		store.Record(s)
	}
}

func currentReplicas(t *testing.T, c client.Client, name string) int32 {
	t.Helper()
	var got kmv1.AgentDeploy
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: name}, &got))
	return specReplicas(&got)
}

func TestAutoscalerScalesUpAndPatchesSpec(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10), TargetSaturationPercentage: ptrU32(100)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	// Heavy load on a single replica → cold-start target 12 → surge.
	seedStore(t, reg, ad, sample(now, 1, 80, 160))

	NewAutoscaler(c, reg, testLogger()).scaleOnce(context.Background(), now)

	assert.Greater(t, currentReplicas(t, c, "foo"), int32(1), "spec.replicas scaled up")
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

	NewAutoscaler(c, reg, testLogger()).scaleOnce(context.Background(), now)

	assert.Equal(t, int32(4), currentReplicas(t, c, "foo"))
}

func TestAutoscalerSkipsDisabled(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Disabled: true, Max: ptrI32(10)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	seedStore(t, reg, ad, sample(now, 1, 80, 160))

	NewAutoscaler(c, reg, testLogger()).scaleOnce(context.Background(), now)
	assert.Equal(t, int32(1), currentReplicas(t, c, "foo"), "disabled is left untouched")
}

func TestAutoscalerSkipsWhenNotSampled(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 1)
	ad.Spec.Scale = kmv1.Scale{Max: ptrI32(10)}

	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c) // empty — no store for foo

	NewAutoscaler(c, reg, testLogger()).scaleOnce(context.Background(), now)
	assert.Equal(t, int32(1), currentReplicas(t, c, "foo"), "no history → no scaling")
}
