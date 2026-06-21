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

package rater

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedqueue "github.com/kynoproj/kynomesh/pkg/shared/queue"
)

// stubScraper returns canned samples per host. Sequential calls to
// the same host advance through its sample list.
type stubScraper struct {
	mu      sync.Mutex
	samples map[string][]*PodSample
	idx     map[string]int
	failure error
}

func (s *stubScraper) Scrape(_ context.Context, host string) (*PodSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return nil, s.failure
	}
	list := s.samples[host]
	i := s.idx[host]
	if i >= len(list) {
		i = len(list) - 1 // hold last
	}
	s.idx[host] = i + 1
	return list[i], nil
}

func stubDiscover(hosts map[string][]string) DiscoverFunc {
	return func(_ context.Context, ad string) ([]string, error) {
		return hosts[ad], nil
	}
}

func TestGetMetrics_UnknownAgentDeploy(t *testing.T) {
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	_, err := r.GetMetrics("nope", 0)
	require.ErrorIs(t, err, ErrUnknownAgentDeploy)
}

func TestGetMetrics_NoDataYet(t *testing.T) {
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	_, err := r.GetMetrics("a", 0)
	require.ErrorIs(t, err, ErrNoData)
}

// fakeClock advances by the user pushing through .Tick().
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// driveScrapes performs N synchronous scrape passes at clock-aligned
// times spaced by CountWindow. It bypasses the Start() ticker so
// tests can run deterministically without sleeping.
func driveScrapes(t *testing.T, r *Rater, fc *fakeClock, n int) {
	t.Helper()
	ctx := context.Background()
	for range n {
		r.scrapeAllOnce(ctx)
		fc.Advance(CountWindow)
	}
}

func TestGetMetrics_HappyPath_SingleTransport(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)

	// One pod that processes 10 messages every 10s; in-flight = 3.
	samples := []*PodSample{
		{CounterByTransport: map[string]float64{"rest": 0}, GaugeByTransport: map[string]float64{"rest": 3}},
		{CounterByTransport: map[string]float64{"rest": 10}, GaugeByTransport: map[string]float64{"rest": 3}},
		{CounterByTransport: map[string]float64{"rest": 20}, GaugeByTransport: map[string]float64{"rest": 3}},
		{CounterByTransport: map[string]float64{"rest": 30}, GaugeByTransport: map[string]float64{"rest": 3}},
	}
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"greeter"},
		Discover:     stubDiscover(map[string][]string{"greeter": {"greeter-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"greeter-0": samples}, idx: map[string]int{}},
		Clock:        fc.Now,
	})

	driveScrapes(t, r, fc, 4)
	res, err := r.GetMetrics("greeter", 0)
	require.NoError(t, err)

	// rate = (delta across buckets) / (timeDiff). 4 buckets, endIdx=2,
	// startIdx=0 → walk buckets 0→1, 1→2. Each delta is 10. timeDiff = 20.
	// rate = 20/20 = 1.0.
	assert.InDelta(t, 1.0, res.Total.ProcessingRates[WindowKey1m], 1e-9)
	// per-transport must match total when only one transport exists.
	assert.InDelta(t, 1.0, res.PerTransport["rest"].ProcessingRates[WindowKey1m], 1e-9)
	// in-flight is constant 3.
	assert.InDelta(t, 3.0, res.Total.InflightAverages[WindowKey1m], 1e-9)
	assert.InDelta(t, 3.0, res.PerTransport["rest"].InflightAverages[WindowKey1m], 1e-9)
}

func TestGetMetrics_CustomWindowClampedToRetention(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	sample := &PodSample{
		CounterByTransport: map[string]float64{"rest": 0},
		GaugeByTransport:   map[string]float64{"rest": 1},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"a-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"a-0": {sample, sample}}, idx: map[string]int{}},
		Clock:        fc.Now,
	})
	driveScrapes(t, r, fc, 3)

	res, err := r.GetMetrics("a", 99999) // way above retention
	require.NoError(t, err)
	assert.Equal(t, int64(Retention.Seconds()), res.CustomWindowEffectiveSec)
	_, hasCustom := res.Total.ProcessingRates[WindowKeyCustom]
	assert.True(t, hasCustom)
}

func TestGetMetrics_NoCustomWindowWhenLookbackZero(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	sample := &PodSample{
		CounterByTransport: map[string]float64{"rest": 0},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"a-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"a-0": {sample, sample}}, idx: map[string]int{}},
		Clock:        fc.Now,
	})
	driveScrapes(t, r, fc, 3)

	res, err := r.GetMetrics("a", 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), res.CustomWindowEffectiveSec)
	_, hasCustom := res.Total.ProcessingRates[WindowKeyCustom]
	assert.False(t, hasCustom)
}

func TestGetMetrics_MultipleTransports(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	// Two transports, separate rates: rest=10/10s=1, grpc=5/10s=0.5,
	// total = 1.5.
	samples := []*PodSample{
		{CounterByTransport: map[string]float64{"rest": 0, "grpc": 0}, GaugeByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{CounterByTransport: map[string]float64{"rest": 10, "grpc": 5}, GaugeByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{CounterByTransport: map[string]float64{"rest": 20, "grpc": 10}, GaugeByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{CounterByTransport: map[string]float64{"rest": 30, "grpc": 15}, GaugeByTransport: map[string]float64{"rest": 1, "grpc": 2}},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"a-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"a-0": samples}, idx: map[string]int{}},
		Clock:        fc.Now,
	})
	driveScrapes(t, r, fc, 4)

	res, err := r.GetMetrics("a", 0)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, res.PerTransport["rest"].ProcessingRates[WindowKey1m], 1e-9)
	assert.InDelta(t, 0.5, res.PerTransport["grpc"].ProcessingRates[WindowKey1m], 1e-9)
	assert.InDelta(t, 1.5, res.Total.ProcessingRates[WindowKey1m], 1e-9)
}

func TestScrapeAllOnce_DiscoveryFailureSkipsAD(t *testing.T) {
	called := atomic.Int32{}
	scr := &stubScraper{
		samples: map[string][]*PodSample{},
		idx:     map[string]int{},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover: func(_ context.Context, _ string) ([]string, error) {
			called.Add(1)
			return nil, errors.New("dns down")
		},
		Scraper: scr,
	})
	r.scrapeAllOnce(context.Background())
	assert.Equal(t, int32(1), called.Load())
	// Buffer must remain empty since discovery failed.
	assert.Equal(t, 0, r.buffers["a"].Length())
}

func TestScrapeOneAgentDeploy_ScrapeFailureKeepsPreviousValue(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	scr := &stubScraper{
		samples: map[string][]*PodSample{
			"a-0": {{CounterByTransport: map[string]float64{"rest": 100}}},
		},
		idx: map[string]int{},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"a-0"}}),
		Scraper:      scr,
		Clock:        fc.Now,
	})
	// 1st pass: successful scrape stores 100.
	r.scrapeAllOnce(context.Background())
	// Mark failure.
	scr.failure = errors.New("dropped")
	fc.Advance(CountWindow)
	r.scrapeAllOnce(context.Background())

	items := r.buffers["a"].Items()
	require.Len(t, items, 1, "failed scrape must not create a new bucket")
	snap := items[0].Snapshot()
	assert.Equal(t, float64(100), snap["a-0"].CounterByTransport["rest"])
}

func TestObservedTransports_Deduplicates(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	UpdateBucket(q, 100, "p", &PodSample{CounterByTransport: map[string]float64{"rest": 1, "grpc": 2}})
	UpdateBucket(q, 110, "p", &PodSample{GaugeByTransport: map[string]float64{"jsonrpc": 1, "rest": 0}})
	got := observedTransports(q)
	assert.ElementsMatch(t, []string{"rest", "grpc", "jsonrpc"}, got)
}

func TestAveragePodsObserved(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	UpdateBucket(q, 100, "p1", &PodSample{})
	UpdateBucket(q, 100, "p2", &PodSample{})
	UpdateBucket(q, 110, "p1", &PodSample{})
	UpdateBucket(q, 110, "p2", &PodSample{})
	UpdateBucket(q, 110, "p3", &PodSample{})
	// bucket 1: 2 pods, bucket 2: 3 pods → avg = 2.
	assert.Equal(t, int64(2), averagePodsObserved(q))
}

func TestStart_ShutsDownOnContextCancel(t *testing.T) {
	r := NewRater(Options{
		AgentDeploys:   []string{"a"},
		Discover:       stubDiscover(map[string][]string{"a": {}}),
		Scraper:        &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		ScrapeInterval: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Start(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}
