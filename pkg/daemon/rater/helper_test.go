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

	sharedqueue "github.com/kynoproj/kynomesh/pkg/shared/queue"
)

func TestAlignBucket(t *testing.T) {
	tests := []struct {
		in, out int64
	}{
		{0, 0},
		{1, 0},
		{9, 0},
		{10, 10},
		{15, 10},
		{20, 20},
		{1234567, 1234560},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.out, AlignBucket(tc.in), "AlignBucket(%d)", tc.in)
	}
}

func TestUpdateBucket_NewAndExisting(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	UpdateBucket(q, 100, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 1}})
	UpdateBucket(q, 100, "pod-1", &PodSample{CounterByTransport: map[string]float64{"rest": 2}})
	UpdateBucket(q, 110, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 5}})

	items := q.Items()
	assert.Len(t, items, 2)
	assert.Equal(t, int64(100), items[0].Timestamp())
	assert.Len(t, items[0].Snapshot(), 2)
	assert.Equal(t, int64(110), items[1].Timestamp())
	assert.Len(t, items[1].Snapshot(), 1)
}

// twoPodSnapshot builds buckets at t0+0, t0+10, t0+20 with rest+grpc
// counters and rest+grpc gauges per pod. Returns the queue, t0, and
// the now-aligned timestamp (= t0+30, one bucket past the latest).
func twoPodFixture(t *testing.T) (*sharedqueue.OverflowQueue[*TimestampedCounts], int64, int64) {
	t.Helper()
	q := sharedqueue.New[*TimestampedCounts](10)
	const t0 int64 = 1_000_000
	// bucket t0
	UpdateBucket(q, t0, "pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 100, "grpc": 50},
		GaugeByTransport:   map[string]float64{"rest": 2, "grpc": 1},
	})
	UpdateBucket(q, t0, "pod-1", &PodSample{
		CounterByTransport: map[string]float64{"rest": 200, "grpc": 80},
		GaugeByTransport:   map[string]float64{"rest": 3, "grpc": 1},
	})
	// bucket t0+10: each pod processed 50 rest + 30 grpc more
	UpdateBucket(q, t0+10, "pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 150, "grpc": 80},
		GaugeByTransport:   map[string]float64{"rest": 4, "grpc": 2},
	})
	UpdateBucket(q, t0+10, "pod-1", &PodSample{
		CounterByTransport: map[string]float64{"rest": 250, "grpc": 110},
		GaugeByTransport:   map[string]float64{"rest": 5, "grpc": 2},
	})
	// bucket t0+20: pod-0 processed 50 rest + 30 grpc more; pod-1 same
	UpdateBucket(q, t0+20, "pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 200, "grpc": 110},
		GaugeByTransport:   map[string]float64{"rest": 6, "grpc": 3},
	})
	UpdateBucket(q, t0+20, "pod-1", &PodSample{
		CounterByTransport: map[string]float64{"rest": 300, "grpc": 140},
		GaugeByTransport:   map[string]float64{"rest": 7, "grpc": 3},
	})
	return q, t0, t0 + 30
}

func TestCalculateRate_PerTransport(t *testing.T) {
	q, _, now := twoPodFixture(t)

	// Lookback covers all buckets. EndIdx=1 (n-2), startIdx=0.
	// REST delta between bucket 0 and bucket 1: pod-0 (150-100)=50, pod-1 (250-200)=50 → 100.
	// timeDiff = 10s → rate = 10/s.
	r := CalculateRate(q, now, 30, "rest")
	assert.InDelta(t, 10.0, r, 1e-9)

	// GRPC delta: pod-0 (80-50)=30, pod-1 (110-80)=30 → 60. rate = 6/s.
	r = CalculateRate(q, now, 30, "grpc")
	assert.InDelta(t, 6.0, r, 1e-9)
}

func TestCalculateRate_Total(t *testing.T) {
	q, _, now := twoPodFixture(t)
	// Total: rest+grpc per pod between bucket 0 and 1: pod-0 (230-150)=80, pod-1 (360-280)=80 → 160. rate=16/s.
	r := CalculateRate(q, now, 30, TransportTotal)
	assert.InDelta(t, 16.0, r, 1e-9)
}

func TestCalculateRate_CounterReset(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	UpdateBucket(q, 100, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 1000}})
	UpdateBucket(q, 110, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 5}}) // restarted
	UpdateBucket(q, 120, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 15}})

	// Walk buckets [0..1] (endIdx = n-2 = 1):
	// bucket 0→1: curr=5, prev=1000 → reset → delta = 5.
	// timeDiff = 10 → rate = 0.5/s.
	r := CalculateRate(q, 130, 60, "rest")
	assert.InDelta(t, 0.5, r, 1e-9)
}

func TestCalculateRate_InsufficientBuckets(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	UpdateBucket(q, 100, "pod-0", &PodSample{CounterByTransport: map[string]float64{"rest": 10}})
	assert.Equal(t, 0.0, CalculateRate(q, 110, 60, "rest"))
}

func TestCalculateRate_LookbackOutsideHistory(t *testing.T) {
	q, t0, _ := twoPodFixture(t)
	// "now" far in the future; lookback too small to include any bucket.
	farFuture := t0 + 10_000
	r := CalculateRate(q, farFuture, 1, "rest")
	assert.Equal(t, 0.0, r)
}

func TestCalculateInflightAvg_PerTransport(t *testing.T) {
	q, _, now := twoPodFixture(t)
	// Bucket sums (rest): t0: 5, t0+10: 9, t0+20: 13. endIdx=1 → use buckets 0..1.
	// avg = (5+9)/2 = 7.
	avg := CalculateInflightAvg(q, now, 30, "rest")
	assert.InDelta(t, 7.0, avg, 1e-9)
}

func TestCalculateInflightAvg_Total(t *testing.T) {
	q, _, now := twoPodFixture(t)
	// Per pod gauges sum across transports.
	// t0:  pod-0 (2+1)=3, pod-1 (3+1)=4 → 7
	// t0+10: pod-0 (4+2)=6, pod-1 (5+2)=7 → 13
	// avg of {7, 13} = 10.
	avg := CalculateInflightAvg(q, now, 30, TransportTotal)
	assert.InDelta(t, 10.0, avg, 1e-9)
}

func TestHasUsableHistory(t *testing.T) {
	q := sharedqueue.New[*TimestampedCounts](10)
	assert.False(t, HasUsableHistory(q))
	UpdateBucket(q, 100, "p", &PodSample{CounterByTransport: map[string]float64{"rest": 1}})
	assert.False(t, HasUsableHistory(q))
	UpdateBucket(q, 110, "p", &PodSample{CounterByTransport: map[string]float64{"rest": 2}})
	assert.True(t, HasUsableHistory(q))
}
