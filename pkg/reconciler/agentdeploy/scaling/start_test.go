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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// startReturns runs fn(ctx) in a goroutine and returns a channel that receives
// its error once it returns.
func startReturns(ctx context.Context, fn func(context.Context) error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	return done
}

func TestRunnerStartTicksThenStopsOnCancel(t *testing.T) {
	reg := NewRegistry(fake.NewClientBuilder().WithScheme(storeScheme(t)).Build())
	watch := NewWatchSet(reg, nil)
	watch.Track(nn("foo"))

	ticked := make(chan types.NamespacedName, 1)
	r := &runner{
		name:         "test",
		watch:        watch,
		workers:      1,
		taskInterval: time.Millisecond,
		logger:       testLogger(),
		process: func(_ context.Context, k types.NamespacedName) error {
			select {
			case ticked <- k:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startReturns(ctx, r.start)

	select {
	case k := <-ticked:
		assert.Equal(t, "foo", k.Name, "runner hands the tracked key to process")
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("runner did not tick within timeout")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "start returns nil on ctx cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("start did not return after cancel")
	}
}

func TestRunnerStartEmptyWatchSetStopsOnCancel(t *testing.T) {
	reg := NewRegistry(fake.NewClientBuilder().WithScheme(storeScheme(t)).Build())
	r := &runner{
		name:         "test",
		watch:        NewWatchSet(reg, nil), // empty: exercises the len==0 sleep branch
		workers:      1,
		taskInterval: time.Millisecond,
		logger:       testLogger(),
		process:      func(context.Context, types.NamespacedName) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := startReturns(ctx, r.start)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("start did not return after cancel")
	}
}

func TestAutoscalerStartStopsOnCancel(t *testing.T) {
	ad := scalingAD("foo", 1)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	a := NewAutoscaler(c, NewWatchSet(reg, nil), reg, testLogger(),
		WithScaleInterval(time.Millisecond))
	a.watch.Track(nn("foo"))

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

func TestSamplerStartStopsOnCancel(t *testing.T) {
	ad := scalingAD("foo", 1)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	s := NewSampler(c, NewWatchSet(reg, nil), reg,
		staticDialer(&fakeSource{resp: metricsAt(windowKey1m, 40, 80)}), testLogger(),
		WithTaskInterval(time.Millisecond), WithReapInterval(time.Millisecond))
	s.watch.Track(nn("foo"))

	ctx, cancel := context.WithCancel(context.Background())
	done := startReturns(ctx, s.Start)
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "Start returns nil and runs flushAll/closeAllSources on cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("Sampler.Start did not return after cancel")
	}
}

func TestSamplerOptionSetters(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	reg := NewRegistry(c)
	metrics := &Metrics{}
	clock := func() time.Time { return time.Unix(0, 0) }

	s := NewSampler(c, NewWatchSet(reg, nil), reg, staticDialer(&fakeSource{}), testLogger(),
		WithWorkers(7),
		WithTaskInterval(11*time.Second),
		WithFlushInterval(12*time.Second),
		WithScrapeTimeout(13*time.Second),
		WithReapInterval(14*time.Second),
		WithSamplerMetrics(metrics),
		WithSamplerClock(clock),
	)

	assert.Equal(t, 7, s.workers)
	assert.Equal(t, 11*time.Second, s.taskInterval)
	assert.Equal(t, 12*time.Second, s.flushInterval)
	assert.Equal(t, 13*time.Second, s.scrapeTimeout)
	assert.Equal(t, 14*time.Second, s.reapInterval)
	assert.Same(t, metrics, s.metrics)
	require.NotNil(t, s.clock)
	assert.Equal(t, time.Unix(0, 0), s.clock())
	// The runner picks up the configured worker count and cadence.
	assert.Equal(t, 7, s.runner.workers)
	assert.Equal(t, 11*time.Second, s.runner.taskInterval)
}

func TestAutoscalerOptionSetters(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	reg := NewRegistry(c)
	metrics := &Metrics{}
	clock := func() time.Time { return time.Unix(0, 0) }

	a := NewAutoscaler(c, NewWatchSet(reg, nil), reg, testLogger(),
		WithScaleWorkers(5),
		WithScaleInterval(21*time.Second),
		WithMaxSampleAge(22*time.Second),
		WithAutoscalerMetrics(metrics),
		WithAutoscalerClock(clock),
	)

	assert.Equal(t, 5, a.workers)
	assert.Equal(t, 21*time.Second, a.taskInterval)
	assert.Equal(t, 22*time.Second, a.maxSampleAge)
	assert.Same(t, metrics, a.metrics)
	require.NotNil(t, a.clock)
	assert.Equal(t, time.Unix(0, 0), a.clock())
	assert.Equal(t, 5, a.runner.workers)
	assert.Equal(t, 21*time.Second, a.runner.taskInterval)
}
