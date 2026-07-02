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
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func testLogger() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// scalingAD builds a scaling-enabled AgentDeploy for the loop tests.
func scalingAD(name string, ready uint32) *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: name},
			AgentSetName:        "set",
			Replicas:            ptrI32(1),
		},
		Status: kmv1.AgentDeployStatus{ReadyReplicas: ready},
	}
}

func staticDialer(src MetricsSource) DaemonDialer {
	return func(string, string) (MetricsSource, error) { return src, nil }
}

func nn(name string) types.NamespacedName {
	return types.NamespacedName{Namespace: "ns", Name: name}
}

func fixedClock(t time.Time) SamplerOption {
	return WithSamplerClock(func() time.Time { return t })
}

func TestSamplerSampleKeyRecordsPerReplica(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 4)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, NewWatchSet(reg), reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 100, 200)}),
		testLogger(), WithFlushInterval(time.Hour), fixedClock(now))

	require.NoError(t, s.sampleKey(context.Background(), nn("foo")))

	store, ok := reg.Get(nn("foo"))
	require.True(t, ok)
	hist := store.History(now.Add(time.Minute))
	require.Len(t, hist, 1)
	assert.Equal(t, 25.0, hist[0].InflightPerRep, "100 total / 4 ready")
	assert.Equal(t, 50.0, hist[0].RatePerRep)
}

func TestSamplerSampleKeySamplesDisabled(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 2)
	ad.Spec.Scale.Disabled = true
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, NewWatchSet(reg), reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}),
		testLogger(), WithFlushInterval(time.Hour), fixedClock(now))

	require.NoError(t, s.sampleKey(context.Background(), nn("foo")))
	store, ok := reg.Get(nn("foo"))
	require.True(t, ok)
	assert.Len(t, store.History(now.Add(time.Minute)), 1, "history collected even while disabled")
}

func TestSamplerSampleKeyForgetsDeleted(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build() // no objects
	reg := NewRegistry(c)
	watch := NewWatchSet(reg)
	watch.Track(nn("ghost"))
	s := NewSampler(c, watch, reg, staticDialer(&fakeSource{}), testLogger())

	require.NoError(t, s.sampleKey(context.Background(), nn("ghost")))
	assert.False(t, watch.Contains(nn("ghost")), "missing AgentDeploy is forgotten")
}

func TestSamplerFlushesOnInterval(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ad := scalingAD("foo", 2)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, NewWatchSet(reg), reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}),
		testLogger(), WithFlushInterval(0), fixedClock(now)) // flush every sample

	require.NoError(t, s.sampleKey(context.Background(), nn("foo")))

	var cm corev1.ConfigMap
	err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: HistoryConfigMapName("foo")}, &cm)
	require.NoError(t, err, "history ConfigMap flushed")
	assert.NotEmpty(t, cm.BinaryData[historyKey])
}

func TestSamplerStartSamplesAllWatched(t *testing.T) {
	objs := make([]client.Object, 0, 3)
	for _, n := range []string{"a", "b", "c"} {
		objs = append(objs, scalingAD(n, 4))
	}
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(objs...).Build()
	reg := NewRegistry(c)
	watch := NewWatchSet(reg)
	for _, n := range []string{"a", "b", "c"} {
		watch.Track(nn(n))
	}
	s := NewSampler(c, watch, reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 100, 200)}),
		testLogger(), WithWorkers(2), WithTaskInterval(5*time.Millisecond), WithFlushInterval(time.Hour))

	go func() { _ = s.Start(t.Context()) }()

	require.Eventually(t, func() bool {
		for _, n := range []string{"a", "b", "c"} {
			st, ok := reg.Get(nn(n))
			if !ok || len(st.History(time.Now())) == 0 {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond, "all watched AgentDeploys get sampled")
}

func TestSamplerFlushesAllOnShutdown(t *testing.T) {
	ad := scalingAD("foo", 2)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	watch := NewWatchSet(reg)
	watch.Track(nn("foo"))
	// Periodic flush interval far in the future, so any persisted history must
	// have come from the shutdown flush.
	s := NewSampler(c, watch, reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}),
		testLogger(), WithWorkers(1), WithTaskInterval(5*time.Millisecond), WithFlushInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Start(ctx); close(done) }()

	// Wait until at least one sample is recorded, then stop.
	require.Eventually(t, func() bool {
		st, ok := reg.Get(nn("foo"))
		return ok && len(st.History(time.Now())) > 0
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	<-done // Start returns after flushAll completes

	var cm corev1.ConfigMap
	err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: HistoryConfigMapName("foo")}, &cm)
	require.NoError(t, err, "history flushed on shutdown despite the 1h flush interval")
	assert.NotEmpty(t, cm.BinaryData[historyKey])
}

func TestSamplerFlushAllPersistsAllStores(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	names := []string{"a", "b", "c", "d", "e"}
	objs := make([]client.Object, 0, len(names))
	for _, n := range names {
		objs = append(objs, scalingAD(n, 2))
	}
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(objs...).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, NewWatchSet(reg), reg, staticDialer(&fakeSource{}), testLogger(), WithWorkers(3))

	for _, n := range names {
		store, err := reg.StoreFor(context.Background(), scalingAD(n, 2))
		require.NoError(t, err)
		store.Record(sample(now, 2, 10, 100), "h")
	}

	s.flushAll()

	for _, n := range names {
		var cm corev1.ConfigMap
		require.NoError(t, c.Get(context.Background(),
			client.ObjectKey{Namespace: "ns", Name: HistoryConfigMapName(n)}, &cm), n)
		assert.NotEmpty(t, cm.BinaryData[historyKey], n)
	}
}
