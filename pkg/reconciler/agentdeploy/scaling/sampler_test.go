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

func TestSamplerRecordsPerReplicaSample(t *testing.T) {
	ad := scalingAD("foo", 4)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	src := &fakeSource{resp: metricsAt(windowKey1m, 100, 200)}
	s := NewSampler(c, reg, staticDialer(src), testLogger(), WithFlushInterval(time.Hour))

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.sampleOnce(context.Background(), now)

	store, ok := reg.Get(types.NamespacedName{Namespace: "ns", Name: "foo"})
	require.True(t, ok, "store created for sampled AgentDeploy")
	hist := store.History(now)
	require.Len(t, hist, 1)
	assert.Equal(t, 25.0, hist[0].InflightPerRep, "100 total / 4 ready")
	assert.Equal(t, 50.0, hist[0].RatePerRep)
}

func TestSamplerSkipsDisabled(t *testing.T) {
	ad := scalingAD("foo", 4)
	ad.Spec.Scale.Disabled = true
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 100, 200)}), testLogger())

	s.sampleOnce(context.Background(), time.Now())
	_, ok := reg.Get(types.NamespacedName{Namespace: "ns", Name: "foo"})
	assert.False(t, ok, "disabled AgentDeploy is not sampled")
}

func TestSamplerFlushesOnInterval(t *testing.T) {
	ad := scalingAD("foo", 2)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}), testLogger(),
		WithFlushInterval(0)) // flush every tick

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.sampleOnce(context.Background(), now)

	var cm corev1.ConfigMap
	err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: HistoryConfigMapName("foo")}, &cm)
	require.NoError(t, err, "history ConfigMap flushed")
	assert.NotEmpty(t, cm.BinaryData[historyKey])
}

func TestSamplerReapsDeletedAgentDeploys(t *testing.T) {
	ad := scalingAD("foo", 2)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, reg, staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}), testLogger(), WithFlushInterval(time.Hour))

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.sampleOnce(context.Background(), now)
	require.NoError(t, c.Delete(context.Background(), ad))

	s.sampleOnce(context.Background(), now.Add(time.Minute))
	_, ok := reg.Get(types.NamespacedName{Namespace: "ns", Name: "foo"})
	assert.False(t, ok, "deleted AgentDeploy is reaped from the registry")
}
