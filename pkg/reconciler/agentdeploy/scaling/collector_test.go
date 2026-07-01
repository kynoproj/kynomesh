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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	rater "github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
)

type fakeSource struct {
	resp *pb.AgentDeployMetrics
	err  error

	mu          sync.Mutex
	gotName     string
	gotLookback int64
}

func (f *fakeSource) GetAgentDeployMetrics(_ context.Context, name string, lookback int64) (*pb.AgentDeployMetrics, error) {
	f.mu.Lock()
	f.gotName, f.gotLookback = name, lookback
	f.mu.Unlock()
	return f.resp, f.err
}

func metricsAt(window string, inflight, rate float64) *pb.AgentDeployMetrics {
	return &pb.AgentDeployMetrics{
		Inflights:       map[string]*wrapperspb.DoubleValue{window: wrapperspb.Double(inflight)},
		ProcessingRates: map[string]*wrapperspb.DoubleValue{window: wrapperspb.Double(rate)},
	}
}

func agentDeploy(ready uint32) *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns"},
		Status:     kmv1.AgentDeployStatus{ReadyReplicas: ready},
	}
}

func TestCollectNormalizesToPerReplica(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{resp: metricsAt(windowKey1m, 100, 200)}

	got, ok, err := collectSample(context.Background(), src, agentDeploy(4), now)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int32(4), got.Replicas)
	assert.Equal(t, 25.0, got.InflightPerRep, "100 total / 4 ready")
	assert.Equal(t, 50.0, got.RatePerRep, "200 total / 4 ready")
	assert.Equal(t, int64(0), src.gotLookback, "no lookback set → default window")
}

func TestCollectUsesCustomWindowWhenLookbackSet(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{resp: metricsAt(windowKeyCustom, 60, 120)}
	ad := agentDeploy(2)
	ad.Spec.Scale.LookbackSeconds = ptrU32(300)

	got, ok, err := collectSample(context.Background(), src, ad, now)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(300), src.gotLookback)
	assert.Equal(t, 30.0, got.InflightPerRep)
}

func TestCollectSkipsOnColdStart(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.NotFound} {
		src := &fakeSource{err: status.Error(code, "not ready")}
		_, ok, err := collectSample(context.Background(), src, agentDeploy(3), time.Now())
		require.NoError(t, err, code.String())
		assert.False(t, ok)
	}
}

func TestCollectSkipsWhenNoReadyReplicas(t *testing.T) {
	src := &fakeSource{resp: metricsAt(windowKey1m, 100, 200)}
	_, ok, err := collectSample(context.Background(), src, agentDeploy(0), time.Now())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCollectSkipsWhenWindowMissing(t *testing.T) {
	// Daemon returned data, but not for the window we read.
	src := &fakeSource{resp: metricsAt(windowKey5mForTest(), 100, 200)}
	_, ok, err := collectSample(context.Background(), src, agentDeploy(3), time.Now())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCollectReturnsRealErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("connection refused")}
	_, ok, err := collectSample(context.Background(), src, agentDeploy(3), time.Now())
	assert.Error(t, err)
	assert.False(t, ok)
}

// TestWindowKeysMatchDaemon guards the locally-duplicated window keys against
// drift from the daemon rater's canonical values.
func TestWindowKeysMatchDaemon(t *testing.T) {
	assert.Equal(t, rater.WindowKey1m, windowKey1m)
	assert.Equal(t, rater.WindowKeyCustom, windowKeyCustom)
}

func windowKey5mForTest() string { return rater.WindowKey5m }
