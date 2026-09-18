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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/history"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/metrics"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/sampling"
)

// startReturns runs fn(ctx) in a goroutine and returns a channel that receives
// its error once it returns.
func startReturns(ctx context.Context, fn func(context.Context) error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	return done
}

func TestAutoscalerStartStopsOnCancel(t *testing.T) {
	ad := scalingAD("foo", 1)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := history.NewRegistry(c)
	a := NewAutoscaler(c, reg, testLogger(), WithScaleInterval(time.Millisecond))
	a.Track(nn("foo"))

	ctx, cancel := context.WithCancel(context.Background())
	done := startReturns(ctx, a.Start)
	// Let at least one cycle run so scaleKey is exercised via the loop.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Autoscaler.Start did not return after cancel")
	}
}

func TestAutoscalerOptionSetters(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	reg := history.NewRegistry(c)
	m := &metrics.Metrics{}
	clock := func() time.Time { return time.Unix(0, 0) }

	a := NewAutoscaler(c, reg, testLogger(),
		WithScaleWorkers(5),
		WithScaleInterval(21*time.Second),
		WithMaxSampleAge(22*time.Second),
		WithAutoscalerMetrics(m),
		WithAutoscalerClock(clock),
	)

	assert.Equal(t, 5, a.workers)
	assert.Equal(t, 21*time.Second, a.taskInterval)
	assert.Equal(t, 22*time.Second, a.maxSampleAge)
	assert.Same(t, m, a.metrics)
	require.NotNil(t, a.clock)
	assert.Equal(t, time.Unix(0, 0), a.clock())
}

func TestTrackerFansOutTrackAndForget(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	reg := history.NewRegistry(c)
	s := sampling.NewSampler(c, reg, staticDialer(noopSource{}), testLogger())
	a := NewAutoscaler(c, reg, testLogger())
	tracker := NewTracker(s, a)

	tracker.Track(nn("foo"))
	assert.True(t, s.Contains(nn("foo")), "sampler tracks foo")
	assert.True(t, a.watch.Contains(nn("foo")), "autoscaler tracks foo")

	tracker.Forget(nn("foo"))
	assert.False(t, s.Contains(nn("foo")), "sampler forgets foo")
	assert.False(t, a.watch.Contains(nn("foo")), "autoscaler forgets foo")
}
