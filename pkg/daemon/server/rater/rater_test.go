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
)

// scrapeStep is the wall-clock spacing driveScrapes uses between
// successive synchronous scrape passes. Chosen smaller than the
// shortest lookback (1m) so a handful of ticks already populates
// every window with samples.
const scrapeStep = 5 * time.Second

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
	// Return a copy so the rater can safely stamp Timestamp without
	// mutating the test fixture.
	src := list[i]
	cp := &PodSample{
		RequestsByTransport: src.RequestsByTransport,
		InflightByTransport: src.InflightByTransport,
	}
	return cp, nil
}

func stubDiscover(hosts map[string][]string) DiscoverFunc {
	return func(_ context.Context, as, ad string) ([]string, error) {
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

// fakeClock advances by the user pushing through .Advance().
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

// driveScrapes performs N synchronous scrape passes spaced scrapeStep
// apart. It bypasses the Start() ticker so tests run deterministically
// without sleeping.
func driveScrapes(t *testing.T, r *Rater, fc *fakeClock, n int) {
	t.Helper()
	ctx := context.Background()
	for range n {
		r.scrapeAllOnce(ctx)
		fc.Advance(scrapeStep)
	}
}

func TestGetMetrics_HappyPath_SingleTransport(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)

	// One pod that processes 10 messages every scrapeStep; in-flight = 3.
	samples := []*PodSample{
		{RequestsByTransport: map[string]float64{"rest": 0}, InflightByTransport: map[string]float64{"rest": 3}},
		{RequestsByTransport: map[string]float64{"rest": 10}, InflightByTransport: map[string]float64{"rest": 3}},
		{RequestsByTransport: map[string]float64{"rest": 20}, InflightByTransport: map[string]float64{"rest": 3}},
		{RequestsByTransport: map[string]float64{"rest": 30}, InflightByTransport: map[string]float64{"rest": 3}},
	}
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"greeter"},
		Discover:     stubDiscover(map[string][]string{"greeter": {"greeter-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"greeter-0": samples}, idx: map[string]int{}},
		Clock:        fc.Now,
	})

	driveScrapes(t, r, fc, 4)
	// After 4 scrapes spaced 5s apart starting at t=start, samples
	// landed at offsets 0, 5, 10, 15 from start.Unix(). Clock now at
	// start+20s. With a 60s lookback every sample is in-window.
	// Rate: (last 30 - first 0) / (15 - 0) = 2.0/s.
	res, err := r.GetMetrics("greeter", 0)
	require.NoError(t, err)

	assert.InDelta(t, 2.0, res.Total.ProcessingRates[WindowKey1m], 1e-9)
	assert.InDelta(t, 2.0, res.PerTransport["rest"].ProcessingRates[WindowKey1m], 1e-9)
	// In-flight gauge constant at 3 across all observed samples.
	assert.InDelta(t, 3.0, res.Total.Inflights[WindowKey1m], 1e-9)
	assert.InDelta(t, 3.0, res.PerTransport["rest"].Inflights[WindowKey1m], 1e-9)
}

func TestGetMetrics_CustomWindowClampedToRetention(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	sample := &PodSample{
		RequestsByTransport: map[string]float64{"rest": 0},
		InflightByTransport: map[string]float64{"rest": 1},
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
		RequestsByTransport: map[string]float64{"rest": 0},
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
	// Two transports: rest grows by 10 per scrapeStep, grpc by 5.
	samples := []*PodSample{
		{RequestsByTransport: map[string]float64{"rest": 0, "grpc": 0}, InflightByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{RequestsByTransport: map[string]float64{"rest": 10, "grpc": 5}, InflightByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{RequestsByTransport: map[string]float64{"rest": 20, "grpc": 10}, InflightByTransport: map[string]float64{"rest": 1, "grpc": 2}},
		{RequestsByTransport: map[string]float64{"rest": 30, "grpc": 15}, InflightByTransport: map[string]float64{"rest": 1, "grpc": 2}},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"a-0"}}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{"a-0": samples}, idx: map[string]int{}},
		Clock:        fc.Now,
	})
	driveScrapes(t, r, fc, 4)

	// First sample at offset 0, last at offset 15 → timeDiff = 15.
	// REST: (30-0)/15 = 2.0/s. GRPC: (15-0)/15 = 1.0/s. Total = 3.0/s.
	res, err := r.GetMetrics("a", 0)
	require.NoError(t, err)
	assert.InDelta(t, 2.0, res.PerTransport["rest"].ProcessingRates[WindowKey1m], 1e-9)
	assert.InDelta(t, 1.0, res.PerTransport["grpc"].ProcessingRates[WindowKey1m], 1e-9)
	assert.InDelta(t, 3.0, res.Total.ProcessingRates[WindowKey1m], 1e-9)
}

func TestScrapeAllOnce_DiscoveryFailureSkipsAD(t *testing.T) {
	called := atomic.Int32{}
	scr := &stubScraper{
		samples: map[string][]*PodSample{},
		idx:     map[string]int{},
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover: func(_ context.Context, _, _ string) ([]string, error) {
			called.Add(1)
			return nil, errors.New("dns down")
		},
		Scraper: scr,
	})
	r.scrapeAllOnce(context.Background())
	assert.Equal(t, int32(1), called.Load())
	// No pods appended when discovery failed.
	assert.Empty(t, r.buffers["a"].Pods())
}

func TestScrapeOneAgentDeploy_ScrapeFailureKeepsPreviousValue(t *testing.T) {
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)
	scr := &stubScraper{
		samples: map[string][]*PodSample{
			"a-0": {{RequestsByTransport: map[string]float64{"rest": 100}}},
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
	// Mark failure for subsequent passes.
	scr.failure = errors.New("dropped")
	fc.Advance(scrapeStep)
	r.scrapeAllOnce(context.Background())

	// Pod still has exactly one sample — the failed scrape did not
	// overwrite or duplicate the previous successful observation.
	samples := r.buffers["a"].Samples("a-0")
	require.Len(t, samples, 1, "failed scrape must not append a new sample")
	assert.Equal(t, float64(100), samples[0].RequestsByTransport["rest"])
}

// clockAdvancingScraper advances the fake clock when a specific host
// is scraped. Lets tests prove each pod's sample is stamped at its
// own scrape-completion time.
type clockAdvancingScraper struct {
	mu          sync.Mutex
	clock       *fakeClock
	perHostSkew map[string]time.Duration
	sample      *PodSample
}

func (s *clockAdvancingScraper) Scrape(_ context.Context, host string) (*PodSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.perHostSkew[host]; ok {
		s.clock.Advance(d)
	}
	// Return a fresh copy so the rater's Timestamp stamp doesn't
	// mutate the shared fixture.
	src := s.sample
	cp := &PodSample{
		RequestsByTransport: src.RequestsByTransport,
		InflightByTransport: src.InflightByTransport,
	}
	return cp, nil
}

func TestScrape_PerPodTimestamping(t *testing.T) {
	// Two pods, scraped in one tick. pod-1's scrape "takes" 12 wall-
	// clock seconds. With per-pod timestamping, pod-1's sample must
	// have a strictly later Timestamp than pod-0's.
	start := time.Unix(2_000_000, 0)
	fc := newFakeClock(start)

	sample := &PodSample{
		RequestsByTransport: map[string]float64{"rest": 1},
		InflightByTransport: map[string]float64{"rest": 1},
	}
	scr := &clockAdvancingScraper{
		clock: fc,
		perHostSkew: map[string]time.Duration{
			"pod-1": 12 * time.Second,
		},
		sample: sample,
	}
	r := NewRater(Options{
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{"a": {"pod-0", "pod-1"}}),
		Scraper:      scr,
		Clock:        fc.Now,
		// Serialize: pod-0 finishes before pod-1 starts, so pod-1's
		// clock advance can't affect pod-0's timestamp.
		ScrapeWorkers: 1,
	})
	r.scrapeAllOnce(context.Background())

	pod0 := r.buffers["a"].Samples("pod-0")
	pod1 := r.buffers["a"].Samples("pod-1")
	require.Len(t, pod0, 1)
	require.Len(t, pod1, 1)
	assert.Equal(t, start.Unix(), pod0[0].Timestamp,
		"pod-0 should be stamped at start time")
	assert.Equal(t, start.Unix()+12, pod1[0].Timestamp,
		"pod-1 should be stamped 12s later (its own completion time)")
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
