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

// Estimate is what the learner derives from history: the per-replica
// saturation knee and how much to trust it.
type Estimate struct {
	// KneePerReplica is the in-flight concurrency at which one replica stops
	// converting added concurrency into throughput — the point where the
	// rate-vs-concurrency curve plateaus.
	KneePerReplica float64
	// IsLowerBound is true when throughput was still climbing at the highest
	// observed concurrency, so the knee is "at least this much, probably more"
	// — the controller should treat it cautiously.
	IsLowerBound bool
	// Confidence in [0,1]. 0 means "no usable signal, fall back to default".
	Confidence float64
}

const (
	// decayHalfLife weights recent samples more heavily so the estimate tracks
	// current capacity rather than a stale multi-day average.
	decayHalfLife = 6 * time.Hour
	// minSamplesToLearn is the floor below which we report Confidence 0 and let
	// the controller use its conservative default.
	minSamplesToLearn = 8
	// enoughSamples is the clean-sample count at which the count factor of
	// confidence saturates (≈15 min at 15s sampling).
	enoughSamples = 60
	// numBuckets discretizes the concurrency axis to denoise the curve.
	numBuckets = 10
	// plateauFraction: a concurrency band whose marginal throughput gain falls
	// below this fraction of the low-load slope is considered saturated. The
	// knee is the last band before that happens.
	plateauFraction = 0.2
	// lowerBoundPenalty caps confidence when saturation was never observed.
	lowerBoundPenalty = 0.4
)

// EstimateKnee learns the per-replica saturation knee from history, weighting
// recent samples more heavily. It is pure: all time references derive from the
// supplied now and the samples' own timestamps.
//
// The knee is found from the throughput-vs-concurrency curve: below saturation,
// adding in-flight requests raises the processing rate roughly linearly; at
// saturation, the rate plateaus. This is mix-invariant (it does not depend on
// per-request latency) and so is robust to a heterogeneous request mix.
//
// When history is too thin or shows no concurrency spread, Confidence is 0
// (cold start) — the controller is expected to fall back to a default.
func EstimateKnee(history []Sample, now time.Time) Estimate {
	clean := sanitize(history)
	if len(clean) < minSamplesToLearn {
		return Estimate{Confidence: 0}
	}

	buckets := bucketize(clean, now)
	if len(buckets) < 2 {
		// No concurrency spread: we can't map the curve. Treat the max
		// observed load as a weak lower bound.
		maxInflight := buckets[len(buckets)-1].inflight
		return Estimate{
			KneePerReplica: maxInflight,
			IsLowerBound:   true,
			Confidence:     lowerBoundPenalty * countFactor(len(clean)),
		}
	}

	knee, isLowerBound := findKnee(buckets)
	return Estimate{
		KneePerReplica: knee,
		IsLowerBound:   isLowerBound,
		Confidence:     confidence(clean, buckets, isLowerBound),
	}
}

// bucket aggregates samples that fall in one concurrency band.
type bucket struct {
	inflight float64 // weighted-mean per-replica in-flight
	rate     float64 // weighted-mean per-replica processing rate
	weight   float64 // total recency weight
}

// bucketize bins clean samples by per-replica concurrency and collapses each
// non-empty bin to recency-weighted means. Returned buckets are sorted by
// ascending in-flight.
func bucketize(clean []Sample, now time.Time) []bucket {
	minI, maxI := clean[0].InflightPerRep, clean[0].InflightPerRep
	for _, s := range clean {
		minI = math.Min(minI, s.InflightPerRep)
		maxI = math.Max(maxI, s.InflightPerRep)
	}
	if maxI <= minI {
		// Degenerate: one band only.
		return []bucket{{inflight: maxI, weight: 1}}
	}

	type acc struct{ wInflight, wRate, w float64 }
	accs := make([]acc, numBuckets)
	span := maxI - minI
	for _, s := range clean {
		idx := int((s.InflightPerRep - minI) / span * float64(numBuckets))
		if idx >= numBuckets {
			idx = numBuckets - 1
		}
		w := recencyWeight(now.Sub(s.Timestamp))
		accs[idx].wInflight += s.InflightPerRep * w
		accs[idx].wRate += s.RatePerRep * w
		accs[idx].w += w
	}

	out := make([]bucket, 0, numBuckets)
	for _, a := range accs {
		if a.w <= 0 {
			continue
		}
		out = append(out, bucket{
			inflight: a.wInflight / a.w,
			rate:     a.wRate / a.w,
			weight:   a.w,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].inflight < out[j].inflight })
	return out
}

// findKnee walks buckets from low to high concurrency and returns the in-flight
// level at which throughput stops rising — where the marginal rate gain per
// unit concurrency drops below plateauFraction of the low-load slope. The knee
// is the last band still on the rising part (conservative). If throughput never
// plateaus, the knee is the highest observed concurrency and isLowerBound=true.
func findKnee(buckets []bucket) (knee float64, isLowerBound bool) {
	// Reference slope: throughput per unit concurrency at the lowest observed
	// load (≈ 1 / service time before contention).
	ref := buckets[0].rate / buckets[0].inflight
	if ref <= 0 {
		return buckets[len(buckets)-1].inflight, true
	}
	for i := 1; i < len(buckets); i++ {
		dIn := buckets[i].inflight - buckets[i-1].inflight
		if dIn <= 0 {
			continue
		}
		marginal := (buckets[i].rate - buckets[i-1].rate) / dIn
		if marginal < plateauFraction*ref {
			return buckets[i-1].inflight, false
		}
	}
	// Throughput still climbing: lower bound is the max observed concurrency.
	return buckets[len(buckets)-1].inflight, true
}

// confidence combines clean-sample count and concurrency coverage, penalized
// when saturation was never actually observed.
func confidence(clean []Sample, buckets []bucket, isLowerBound bool) float64 {
	c := countFactor(len(clean)) * coverageFactor(buckets)
	if isLowerBound {
		c *= lowerBoundPenalty
	}
	return clampF(c, 0, 1)
}

// countFactor grows with the number of clean samples, saturating at 1.
func countFactor(n int) float64 {
	return clampF(float64(n)/float64(enoughSamples), 0, 1)
}

// coverageFactor rewards a wide observed concurrency band: a curve mapped over
// a broad range is more trustworthy than one seen through a keyhole.
func coverageFactor(buckets []bucket) float64 {
	lo, hi := buckets[0].inflight, buckets[len(buckets)-1].inflight
	if hi <= 0 {
		return 0
	}
	return clampF((hi-lo)/hi, 0, 1)
}

// recencyWeight is exp(-age/τ) with τ derived from decayHalfLife. Future-dated
// samples (clock skew) get full weight.
func recencyWeight(age time.Duration) float64 {
	if age < 0 {
		return 1
	}
	tau := float64(decayHalfLife) / math.Ln2
	return math.Exp(-float64(age) / tau)
}

func clampF(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
