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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func storeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, kmv1.AddToScheme(s))
	return s
}

func testAgentDeploy() *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "foo", Generation: 2},
	}
}

func TestConfigMapStoreFlushCreatesAndLoadRehydrates(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	ad := testAgentDeploy()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	s := NewConfigMapStore(c, ad)
	s.Record(sample(now, 3, 12, 200), "h")
	s.Record(sample(now.Add(15*time.Second), 3, 13, 210), "h")
	require.NoError(t, s.Flush(ctx))

	// The backing ConfigMap exists, owned and labeled.
	var cm corev1.ConfigMap
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: "ns", Name: HistoryConfigMapName("foo")}, &cm))
	require.Len(t, cm.OwnerReferences, 1)
	assert.Equal(t, "foo", cm.OwnerReferences[0].Name)
	assert.Equal(t, "foo", cm.Labels[kmv1.KeyAgentDeployName])
	assert.NotEmpty(t, cm.BinaryData[historyKey])

	// A fresh store rehydrates the same history.
	s2 := NewConfigMapStore(c, ad)
	require.NoError(t, s2.Load(ctx))
	got := s2.History(now.Add(time.Minute))
	require.Len(t, got, 2)
	assert.Equal(t, 12.0, got[0].InflightPerRep)
	assert.Equal(t, 13.0, got[1].InflightPerRep)
}

func TestConfigMapStoreLoadMissingIsNotError(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()

	s := NewConfigMapStore(c, testAgentDeploy())
	require.NoError(t, s.Load(ctx), "absent ConfigMap on first run is fine")
	assert.Empty(t, s.History(time.Now()))
}

func TestConfigMapStoreFlushUpdatesExisting(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	ad := testAgentDeploy()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	s := NewConfigMapStore(c, ad)
	s.Record(sample(now, 3, 12, 200), "h")
	require.NoError(t, s.Flush(ctx))

	s.Record(sample(now.Add(15*time.Second), 3, 13, 210), "h")
	require.NoError(t, s.Flush(ctx), "second flush updates the existing object")

	s2 := NewConfigMapStore(c, ad)
	require.NoError(t, s2.Load(ctx))
	assert.Len(t, s2.History(now.Add(time.Minute)), 2)
}

func TestConfigMapStoreResetsHistoryOnSpecHashChange(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	s := NewConfigMapStore(c, testAgentDeploy())

	s.Record(sample(now, 3, 12, 200), "hashA")
	s.Record(sample(now.Add(15*time.Second), 3, 13, 210), "hashA")
	require.Len(t, s.History(now.Add(time.Minute)), 2)

	// New deployment (different pod-spec hash) drops the old history.
	s.Record(sample(now.Add(30*time.Second), 3, 20, 300), "hashB")
	got := s.History(now.Add(time.Minute))
	require.Len(t, got, 1, "history reset on spec-hash change")
	assert.Equal(t, 20.0, got[0].InflightPerRep)
}

func TestConfigMapStoreSpecHashSurvivesReload(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	ad := testAgentDeploy()

	s := NewConfigMapStore(c, ad)
	s.Record(sample(now, 3, 12, 200), "hashA")
	require.NoError(t, s.Flush(ctx))

	// A fresh store reloads the persisted hash: same hash keeps history, a
	// changed hash (a deploy during downtime) resets it.
	s2 := NewConfigMapStore(c, ad)
	require.NoError(t, s2.Load(ctx))
	s2.Record(sample(now.Add(15*time.Second), 3, 13, 210), "hashB")
	assert.Len(t, s2.History(now.Add(time.Minute)), 1, "reset when reloaded hash differs")
}
