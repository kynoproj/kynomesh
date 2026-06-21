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
	"testing"

	"github.com/stretchr/testify/assert"
)

// twoPodFixture builds two pods' worth of evenly-spaced samples,
// 10s apart, three samples each:
//
//	pod-0 (rest): counters 100, 150, 200      gauge: 2, 4, 6
//	pod-0 (grpc): counters  50,  80, 110      gauge: 1, 2, 3
//	pod-1 (rest): counters 200, 250, 300      gauge: 3, 5, 7
//	pod-1 (grpc): counters  80, 110, 140      gauge: 1, 2, 3
//
// Timestamps: t0, t0+10, t0+20. Returns (buffers, t0, nowUsedForQuery).
// "now" is set to t0+30 — beyond the last sample, so any reasonable
// lookback covers the whole fixture.
func twoPodFixture(t *testing.T) (*AgentDeployBuffers, int64, int64) {
	t.Helper()
	b := NewAgentDeployBuffers()
	const t0 int64 = 1_000_000
	b.Append("pod-0", &PodSample{
		Timestamp:          t0,
		ProcessedByTransport: map[string]float64{"rest": 100, "grpc": 50},
		InflightByTransport:   map[string]float64{"rest": 2, "grpc": 1},
	})
	b.Append("pod-1", &PodSample{
		Timestamp:          t0,
		ProcessedByTransport: map[string]float64{"rest": 200, "grpc": 80},
		InflightByTransport:   map[string]float64{"rest": 3, "grpc": 1},
	})
	b.Append("pod-0", &PodSample{
		Timestamp:          t0 + 10,
		ProcessedByTransport: map[string]float64{"rest": 150, "grpc": 80},
		InflightByTransport:   map[string]float64{"rest": 4, "grpc": 2},
	})
	b.Append("pod-1", &PodSample{
		Timestamp:          t0 + 10,
		ProcessedByTransport: map[string]float64{"rest": 250, "grpc": 110},
		InflightByTransport:   map[string]float64{"rest": 5, "grpc": 2},
	})
	b.Append("pod-0", &PodSample{
		Timestamp:          t0 + 20,
		ProcessedByTransport: map[string]float64{"rest": 200, "grpc": 110},
		InflightByTransport:   map[string]float64{"rest": 6, "grpc": 3},
	})
	b.Append("pod-1", &PodSample{
		Timestamp:          t0 + 20,
		ProcessedByTransport: map[string]float64{"rest": 300, "grpc": 140},
		InflightByTransport:   map[string]float64{"rest": 7, "grpc": 3},
	})
	return b, t0, t0 + 30
}

func TestCalculateRate_PerTransport(t *testing.T) {
	b, _, now := twoPodFixture(t)

	// REST rate per pod over 60s window:
	//   pod-0: first sample (100) at t0, last sample (200) at t0+20.
	//          delta = 100, timeDiff = 20s → 5/s.
	//   pod-1: first sample (200), last sample (300). delta = 100,
	//          timeDiff = 20s → 5/s.
	//   total = 10/s.
	assert.InDelta(t, 10.0, CalculateRate(b, now, 60, "rest"), 1e-9)

	// GRPC rate: pod-0 (50→110)/20 = 3/s, pod-1 (80→140)/20 = 3/s → 6/s.
	assert.InDelta(t, 6.0, CalculateRate(b, now, 60, "grpc"), 1e-9)
}

func TestCalculateRate_Total(t *testing.T) {
	b, _, now := twoPodFixture(t)
	// Total per pod = sum of transport counters at first/last.
	//   pod-0: first (100+50)=150, last (200+110)=310. delta=160, /20s = 8/s.
	//   pod-1: first (200+80)=280, last (300+140)=440. delta=160, /20s = 8/s.
	//   total = 16/s.
	assert.InDelta(t, 16.0, CalculateRate(b, now, 60, TransportTotal), 1e-9)
}

func TestCalculateRate_CounterReset_SingleRestart(t *testing.T) {
	// One pod, four samples. A restart happens between samples 2
	// and 3: counter goes 1000 → 1100 → 5 → 50. Total work done in
	// the window is (1100 - 1000) before the restart, plus 50 after.
	//   pre-restart run: 1000 → 1100 → delta 100
	//   post-restart run: 0 → 50 → delta 50
	//   total = 150 over 30s → 5/s.
	b := NewAgentDeployBuffers()
	for _, x := range []struct {
		ts  int64
		val float64
	}{
		{100, 1000}, {110, 1050}, {120, 1100}, {130, 50},
	} {
		b.Append("p", &PodSample{
			Timestamp:            x.ts,
			ProcessedByTransport: map[string]float64{"rest": x.val},
		})
	}
	// Walk: 1000 → 1050 (ok) → 1100 (ok) → 50 (reset, commit
	// 1100-1000=100, runStart=0) → end (commit 50-0=50). total=150.
	// timeDiff=30. rate=5.
	assert.InDelta(t, 5.0, CalculateRate(b, 140, 60, "rest"), 1e-9)
}

func TestCalculateRate_CounterReset_MultipleRestarts(t *testing.T) {
	// Two restarts inside the window:
	//   500 → 600 → 5 → 10 → 3 → 20
	//
	// Run 0: 500 → 600  (delta 100)
	// Run 1: 0 → 10     (delta 10)
	// Run 2 (open): 0 → 20  (delta 20)
	// Total = 130 over 25s → 5.2/s.
	b := NewAgentDeployBuffers()
	for _, x := range []struct {
		ts  int64
		val float64
	}{
		{100, 500}, {105, 600}, {110, 5}, {115, 10}, {120, 3}, {125, 20},
	} {
		b.Append("p", &PodSample{
			Timestamp:            x.ts,
			ProcessedByTransport: map[string]float64{"rest": x.val},
		})
	}
	assert.InDelta(t, 5.2, CalculateRate(b, 130, 60, "rest"), 1e-9)
}

func TestCalculateRate_NoResetMatchesSimpleDelta(t *testing.T) {
	// Sanity: with no reset, the algorithm collapses to (last-first)
	// regardless of intermediate samples.
	b := NewAgentDeployBuffers()
	for _, x := range []struct {
		ts  int64
		val float64
	}{
		{100, 1000}, {110, 1050}, {120, 1100},
	} {
		b.Append("p", &PodSample{
			Timestamp:            x.ts,
			ProcessedByTransport: map[string]float64{"rest": x.val},
		})
	}
	// (1100 - 1000) / 20 = 5/s.
	assert.InDelta(t, 5.0, CalculateRate(b, 130, 60, "rest"), 1e-9)
}

func TestCalculateRate_PodWithSingleSampleSkipped(t *testing.T) {
	b := NewAgentDeployBuffers()
	b.Append("p0", &PodSample{
		Timestamp:          100,
		ProcessedByTransport: map[string]float64{"rest": 100},
	})
	b.Append("p0", &PodSample{
		Timestamp:          110,
		ProcessedByTransport: map[string]float64{"rest": 200},
	})
	// p1 has only one sample — contributes no rate.
	b.Append("p1", &PodSample{
		Timestamp:          105,
		ProcessedByTransport: map[string]float64{"rest": 999},
	})
	// p0 rate alone: delta=100 / 10s = 10/s.
	assert.InDelta(t, 10.0, CalculateRate(b, 120, 60, "rest"), 1e-9)
}

func TestCalculateRate_LookbackOutsideHistory(t *testing.T) {
	b, t0, _ := twoPodFixture(t)
	farFuture := t0 + 100_000
	// Lookback too short to include any sample.
	assert.Equal(t, 0.0, CalculateRate(b, farFuture, 1, "rest"))
}

func TestCalculateInflightAvg_TimeWeighted_SinglePod(t *testing.T) {
	// One pod, samples at 1005, 1010, 1015, 1050, 1055. Window
	// 1000..1060.
	// Time-weighted: only consecutive pairs contribute; last sample
	// has no "next" so it has no weight.
	//   widths:    5,  5, 35, 5
	//   values:    3,  3, 10, 10
	//   weighted: 15, 15, 350, 50 = 430
	//   totalW = 50
	//   avg = 8.6
	b := NewAgentDeployBuffers()
	for _, x := range []struct {
		ts    int64
		value float64
	}{
		{1005, 3}, {1010, 3}, {1015, 10}, {1050, 10}, {1055, 2},
	} {
		b.Append("p", &PodSample{
			Timestamp:        x.ts,
			InflightByTransport: map[string]float64{"rest": x.value},
		})
	}
	got := CalculateInflightAvg(b, 1060, 60, "rest")
	assert.InDelta(t, 8.6, got, 1e-9)
}

func TestCalculateInflightAvg_SingleSampleReturnsValue(t *testing.T) {
	b := NewAgentDeployBuffers()
	b.Append("p", &PodSample{
		Timestamp:        100,
		InflightByTransport: map[string]float64{"rest": 42},
	})
	// One sample → return its value as best-effort.
	assert.Equal(t, 42.0, CalculateInflightAvg(b, 110, 60, "rest"))
}

func TestCalculateInflightAvg_ZeroSamplesReturnsZero(t *testing.T) {
	b := NewAgentDeployBuffers()
	assert.Equal(t, 0.0, CalculateInflightAvg(b, 110, 60, "rest"))
}

func TestCalculateInflightAvg_SumsAcrossPods(t *testing.T) {
	// Two pods, two samples each, evenly spaced. Each pod's avg is
	// constant; the AgentDeploy total is the sum across pods.
	b := NewAgentDeployBuffers()
	b.Append("p0", &PodSample{Timestamp: 100, InflightByTransport: map[string]float64{"rest": 2}})
	b.Append("p0", &PodSample{Timestamp: 110, InflightByTransport: map[string]float64{"rest": 4}})
	b.Append("p1", &PodSample{Timestamp: 100, InflightByTransport: map[string]float64{"rest": 3}})
	b.Append("p1", &PodSample{Timestamp: 110, InflightByTransport: map[string]float64{"rest": 5}})
	// p0 weighted avg: only one pair (100→110), value 2, width 10
	//   → avg = 2.
	// p1: value 3, width 10 → avg = 3.
	// total = 5.
	assert.InDelta(t, 5.0, CalculateInflightAvg(b, 120, 60, "rest"), 1e-9)
}

func TestCalculateInflightAvg_Total(t *testing.T) {
	// Reuse twoPodFixture but verify the total (sum across transports
	// and pods) using the same time-weighted formula.
	b, _, now := twoPodFixture(t)
	// Per pod, "total" gauge = sum across transports at each ts.
	//   pod-0: (2+1)=3 at t0, (4+2)=6 at t0+10, (6+3)=9 at t0+20.
	//   widths: 10, 10. weights: 30 + 60 = 90. avg = 90/20 = 4.5.
	//   pod-1: (3+1)=4, (5+2)=7, (7+3)=10. weights: 40+70=110, avg=5.5.
	//   total = 10.
	assert.InDelta(t, 10.0, CalculateInflightAvg(b, now, 60, TransportTotal), 1e-9)
}

func TestHasUsableHistory(t *testing.T) {
	b := NewAgentDeployBuffers()
	assert.False(t, HasUsableHistory(b))
	b.Append("p", &PodSample{Timestamp: 100, ProcessedByTransport: map[string]float64{"rest": 1}})
	assert.False(t, HasUsableHistory(b), "one sample is not enough")
	b.Append("p", &PodSample{Timestamp: 110, ProcessedByTransport: map[string]float64{"rest": 2}})
	assert.True(t, HasUsableHistory(b))
}

func TestObservedTransports_Deduplicates(t *testing.T) {
	b := NewAgentDeployBuffers()
	b.Append("p", &PodSample{
		Timestamp:          100,
		ProcessedByTransport: map[string]float64{"rest": 1, "grpc": 2},
	})
	b.Append("p", &PodSample{
		Timestamp:        110,
		InflightByTransport: map[string]float64{"jsonrpc": 1, "rest": 0},
	})
	assert.ElementsMatch(t, []string{"rest", "grpc", "jsonrpc"}, ObservedTransports(b))
}

func TestSamplesInWindow_LinearWalk(t *testing.T) {
	samples := []*PodSample{
		{Timestamp: 100},
		{Timestamp: 110},
		{Timestamp: 120},
		{Timestamp: 130},
	}
	// Window [115, 130] → samples at 120, 130.
	got := samplesInWindow(samples, 130, 15)
	assert.Len(t, got, 2)
	assert.Equal(t, int64(120), got[0].Timestamp)
	assert.Equal(t, int64(130), got[1].Timestamp)

	// Window [125, 130] → only 130.
	got = samplesInWindow(samples, 130, 5)
	assert.Len(t, got, 1)
	assert.Equal(t, int64(130), got[0].Timestamp)

	// Cutoff after all samples → nothing in window. This is the
	// "all samples have aged out" case (e.g. a long-dead pod queried
	// after retention).
	got = samplesInWindow(samples, 1000, 100) // cutoff = 900, all samples are older
	assert.Nil(t, got)
}
