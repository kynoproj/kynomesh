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
	"math"
	"sort"
	"time"
)

// Sample is one observation of a replica set, taken at the Sampler's best-effort
// cadence (default 30s). For an I/O-bound agentic workload the binding resource
// is slot occupancy — how many requests a replica holds concurrently — so the two
// signals that matter are concurrency (InflightPerRep) and throughput
// (RatePerRep). Latency is deliberately absent: it is dominated by upstream
// LLM/tool response time, so it cannot distinguish a saturated replica from a
// busy-but-healthy one.
//
// Metrics are per-replica on purpose: capacity is a per-replica property, and
// points taken at different fleet sizes are only comparable once normalized.
type Sample struct {
	Timestamp time.Time
	// Replicas is the fleet size the metrics were averaged over.
	Replicas int32
	// InflightPerRep is the average in-flight (concurrent) requests per replica.
	InflightPerRep float64
	// RatePerRep is the average processing rate per replica in requests/second.
	RatePerRep float64
}

// warmupAfterScale discards samples taken shortly after a replica-count change:
// pods are still warming caches/connections, so their metrics describe a
// transient, not steady state.
const warmupAfterScale = 60 * time.Second

// valid reports whether a sample carries usable, positive load signal.
func (s Sample) valid() bool {
	return s.Replicas > 0 &&
		s.InflightPerRep > 0 &&
		s.RatePerRep > 0 &&
		!math.IsNaN(s.InflightPerRep) &&
		!math.IsNaN(s.RatePerRep)
}

// sanitize returns the subset of history safe to learn from: positive,
// steady-state readings sorted by time, with at most one reading per instant.
// Samples within warmupAfterScale of a replica-count change are dropped because
// the fleet had not yet settled.
//
// The input slice is not mutated.
func sanitize(history []Sample) []Sample {
	if len(history) == 0 {
		return nil
	}

	sorted := make([]Sample, len(history))
	copy(sorted, history)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	sorted = dedupByTimestamp(sorted)

	clean := make([]Sample, 0, len(sorted))
	var lastChange time.Time
	prevReplicas := sorted[0].Replicas
	for i, s := range sorted {
		if i > 0 && s.Replicas != prevReplicas {
			lastChange = s.Timestamp
		}
		prevReplicas = s.Replicas

		if !s.valid() {
			continue
		}
		// Skip the warmup window after a scale event.
		if !lastChange.IsZero() && s.Timestamp.Sub(lastChange) < warmupAfterScale {
			continue
		}
		clean = append(clean, s)
	}
	return clean
}

// dedupByTimestamp collapses samples that share a Timestamp down to the last
// one, assuming the input is sorted ascending by Timestamp. Duplicate instants
// arise when persisted history is reloaded after a leader failover and then
// re-recorded, or when overlapping adaptive scrape windows land on the same
// clock reading; keeping one per instant stops a single moment from being
// counted more than once in the learned curve and in coverage/time-span
// confidence. It edits sorted in place (already a private copy) and returns the
// truncated slice.
func dedupByTimestamp(sorted []Sample) []Sample {
	out := sorted[:0]
	for _, s := range sorted {
		if n := len(out); n > 0 && s.Timestamp.Equal(out[n-1].Timestamp) {
			out[n-1] = s // last write for this instant wins
			continue
		}
		out = append(out, s)
	}
	return out
}
