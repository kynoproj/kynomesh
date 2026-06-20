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
	"maps"
	"sync"
	"time"
)

// CountWindow is the bucket size for timestamped storage. Per-pod
// scrapes landing within the same CountWindow update the same bucket;
// rate math walks consecutive buckets.
const CountWindow = 10 * time.Second

// Retention is the total horizon kept in the ring buffer. Chosen to
// cover the 15m fixed lookback window with headroom.
const Retention = 30 * time.Minute

// MaxBuckets is the ring buffer depth (Retention / CountWindow).
const MaxBuckets = int(Retention / CountWindow)

// PodSample is one pod's most recent observation for a single scrape
// cycle, broken down by transport label (e.g. "jsonrpc", "rest", ...).
//
// CounterByTransport holds monotonic counter values (messages
// processed); rate is computed from deltas across buckets.
// GaugeByTransport holds instantaneous gauge values (in-flight
// requests); the average over a window is computed directly.
type PodSample struct {
	CounterByTransport map[string]float64
	GaugeByTransport   map[string]float64
}

// TimestampedCounts is one bucket of the ring buffer. It owns the
// per-pod snapshot of counters and gauges observed during the
// bucket's wall-clock window.
//
// A failed scrape MUST NOT clear an existing pod entry — see Update.
type TimestampedCounts struct {
	mu        sync.RWMutex
	timestamp int64
	byPod     map[string]*PodSample
}

func NewTimestampedCounts(timestamp int64) *TimestampedCounts {
	return &TimestampedCounts{
		timestamp: timestamp,
		byPod:     make(map[string]*PodSample),
	}
}

func (tc *TimestampedCounts) Timestamp() int64 {
	return tc.timestamp
}

// Update records a pod's sample for this bucket. A nil sample is
// ignored: a failed scrape must not corrupt the previous successful
// observation, which would otherwise produce a giant counter delta in
// the next bucket.
func (tc *TimestampedCounts) Update(pod string, s *PodSample) {
	if s == nil {
		return
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.byPod[pod] = s
}

// Snapshot returns a shallow copy of the per-pod map. Inner maps are
// not deep-copied; callers must treat them as read-only.
func (tc *TimestampedCounts) Snapshot() map[string]*PodSample {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	out := make(map[string]*PodSample, len(tc.byPod))
	maps.Copy(out, tc.byPod)
	return out
}
