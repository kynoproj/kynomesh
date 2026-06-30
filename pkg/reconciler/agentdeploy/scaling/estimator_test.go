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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// curveRate models throughput vs concurrency: rate rises linearly with
// in-flight below the knee (slope 1/svc), then nearly plateaus above it
// (10% residual slope — diminishing returns past saturation).
func curveRate(inflight, knee, svc float64) float64 {
	if inflight <= knee {
		return inflight / svc
	}
	return knee/svc + (inflight-knee)/svc*0.1
}

// genSeries builds `count` samples spanning the given in-flight range, all aged
// `age` before now, following the throughput curve for `knee`.
func genSeries(now time.Time, age time.Duration, count int, knee, svc, lo, hi float64) []Sample {
	out := make([]Sample, count)
	for i := range count {
		frac := float64(i) / float64(count-1)
		inflight := lo + (hi-lo)*frac
		out[i] = Sample{
			Timestamp:      now.Add(-age).Add(time.Duration(i) * 15 * time.Second),
			Replicas:       3,
			InflightPerRep: inflight,
			RatePerRep:     curveRate(inflight, knee, svc),
		}
	}
	return out
}

func TestEstimateColdStart(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	est := EstimateKnee(genSeries(now, time.Minute, 4, 20, 0.1, 2, 10), now)
	assert.Equal(t, 0.0, est.Confidence)
}

func TestEstimateObservedKnee(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Ramp 2→40 in-flight, throughput plateaus at knee 20.
	est := EstimateKnee(genSeries(now, 2*time.Minute, 60, 20, 0.1, 2, 40), now)

	assert.False(t, est.IsLowerBound)
	assert.InDelta(t, 20, est.KneePerReplica, 6, "knee near true 20")
	assert.Greater(t, est.Confidence, 0.5, "should be confident")
}

func TestEstimateLowerBoundWhenNeverSaturated(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Load never exceeds 15, knee is 40 → throughput still climbing.
	est := EstimateKnee(genSeries(now, 2*time.Minute, 60, 40, 0.1, 2, 15), now)

	assert.True(t, est.IsLowerBound)
	assert.InDelta(t, 15, est.KneePerReplica, 1.5, "lower bound near max observed")
	assert.Less(t, est.Confidence, 0.5, "lower-bound confidence is penalized")
	assert.Greater(t, est.Confidence, 0.0)
}

func TestEstimateFavorsRecentAfterCapacityDrop(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Old regime (5 days ago): high capacity, knee 40.
	old := genSeries(now, 5*24*time.Hour, 60, 40, 0.1, 5, 40)
	// New regime (minutes ago): capacity dropped, knee 15.
	recent := genSeries(now, 3*time.Minute, 60, 15, 0.1, 5, 40)

	est := EstimateKnee(append(old, recent...), now)
	// Recency weighting should pull the knee toward the recent, lower value.
	assert.Less(t, est.KneePerReplica, 25.0, "knee tracks recent capacity drop")
	assert.False(t, est.IsLowerBound)
}

func TestRecencyWeight(t *testing.T) {
	assert.InDelta(t, 1.0, recencyWeight(0), 1e-9)
	assert.InDelta(t, 0.5, recencyWeight(decayHalfLife), 1e-9)
	assert.InDelta(t, 0.25, recencyWeight(2*decayHalfLife), 1e-9)
	assert.Equal(t, 1.0, recencyWeight(-time.Hour), "future-dated gets full weight")
}
