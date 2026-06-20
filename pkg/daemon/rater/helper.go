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

// TransportTotal is the synthetic transport key used to represent the
// sum across all observed transports. It is not emitted by the broker;
// the rater computes it from real transport buckets.
const TransportTotal = "__total__"

// AlignBucket rounds a unix-second timestamp down to the closest
// CountWindow boundary. All samples observed within a single window
// land in the same bucket.
func AlignBucket(unixSeconds int64) int64 {
	w := int64(CountWindow.Seconds())
	return (unixSeconds / w) * w
}

// UpdateBucket finds (or appends) the bucket whose timestamp matches
// ts and records the pod sample into it. Caller is responsible for
// passing an already-bucket-aligned ts (use AlignBucket).
func UpdateBucket(q *OverflowQueue[*TimestampedCounts], ts int64, pod string, sample *PodSample) {
	for _, b := range q.Items() {
		if b.Timestamp() == ts {
			b.Update(pod, sample)
			return
		}
	}
	bucket := NewTimestampedCounts(ts)
	bucket.Update(pod, sample)
	q.Append(bucket)
}

// findStartIndex returns the index of the earliest bucket whose
// timestamp is still inside the lookback window ending at nowAligned.
// The returned index is suitable for use with CalculateRate; -1 means
// no usable buckets exist.
//
// The most recent bucket (n-1) is excluded from the search because it
// may still be filling — CalculateRate uses n-2 as the right
// boundary. If n < 2, no rate can be computed.
func findStartIndex(buckets []*TimestampedCounts, nowAligned, lookbackSeconds int64) int {
	n := len(buckets)
	if n < 2 {
		return -1
	}
	cutoff := nowAligned - lookbackSeconds
	// Bucket n-2 is the most-recent complete boundary. If even that
	// is older than cutoff, the window contains no buckets.
	if buckets[n-2].Timestamp() < cutoff {
		return -1
	}
	// Linear walk is fine for MaxBuckets = 180.
	for i := range n - 1 {
		if buckets[i].Timestamp() >= cutoff {
			return i
		}
	}
	return -1
}

// CalculateRate returns the per-second rate of the counter for the
// given transport over the requested lookback. Walking consecutive
// buckets, for each pod that appears in both, accumulate delta =
// curr - prev. If curr < prev (counter reset, e.g. pod restart),
// use curr as the delta — this matches Prometheus' rate() semantics
// for counter resets.
//
// The returned rate is (sum of pod deltas across the window) divided
// by (timestamp diff in seconds). Returns 0 when no rate can be
// computed (fewer than 2 buckets, or no buckets in window).
func CalculateRate(q *OverflowQueue[*TimestampedCounts], nowAligned, lookbackSeconds int64, transport string) float64 {
	buckets := q.Items()
	startIdx := findStartIndex(buckets, nowAligned, lookbackSeconds)
	if startIdx < 0 {
		return 0
	}
	endIdx := len(buckets) - 2 // exclude most recent (possibly incomplete) bucket
	if endIdx <= startIdx {
		return 0
	}
	timeDiff := buckets[endIdx].Timestamp() - buckets[startIdx].Timestamp()
	if timeDiff <= 0 {
		return 0
	}
	var delta float64
	for i := startIdx; i < endIdx; i++ {
		delta += bucketDelta(buckets[i], buckets[i+1], transport)
	}
	return delta / float64(timeDiff)
}

func bucketDelta(prev, curr *TimestampedCounts, transport string) float64 {
	if prev == nil || curr == nil {
		return 0
	}
	pSnap := prev.Snapshot()
	cSnap := curr.Snapshot()
	var sum float64
	for pod, sample := range cSnap {
		if sample == nil {
			continue
		}
		var currVal, prevVal float64
		if transport == TransportTotal {
			currVal = sumCounters(sample)
			if prev := pSnap[pod]; prev != nil {
				prevVal = sumCounters(prev)
			}
		} else {
			currVal = sample.CounterByTransport[transport]
			if prev := pSnap[pod]; prev != nil {
				prevVal = prev.CounterByTransport[transport]
			}
		}
		// Counter reset: assume the pod was restarted and use curr as
		// the delta. Anything else would either silently drop traffic
		// (delta = 0) or report negative throughput.
		if currVal >= prevVal {
			sum += currVal - prevVal
		} else {
			sum += currVal
		}
	}
	return sum
}

func sumCounters(s *PodSample) float64 {
	var total float64
	for _, v := range s.CounterByTransport {
		total += v
	}
	return total
}

func sumGauges(s *PodSample) float64 {
	var total float64
	for _, v := range s.GaugeByTransport {
		total += v
	}
	return total
}

// CalculateInflightAvg returns the average in-flight value for the
// given transport over the lookback window. For each bucket in the
// window we sum the per-pod gauge values, then average those per-
// bucket sums.
//
// In-flight is a gauge so no delta math is needed — a single bucket
// is enough. The most recent bucket is still excluded for consistency
// with rate calc (it may be filling).
func CalculateInflightAvg(q *OverflowQueue[*TimestampedCounts], nowAligned, lookbackSeconds int64, transport string) float64 {
	buckets := q.Items()
	startIdx := findStartIndex(buckets, nowAligned, lookbackSeconds)
	if startIdx < 0 {
		return 0
	}
	endIdx := len(buckets) - 2
	if endIdx < startIdx {
		return 0
	}
	var total float64
	var n int
	for i := startIdx; i <= endIdx; i++ {
		snap := buckets[i].Snapshot()
		var bucketSum float64
		for _, sample := range snap {
			if sample == nil {
				continue
			}
			if transport == TransportTotal {
				bucketSum += sumGauges(sample)
			} else {
				bucketSum += sample.GaugeByTransport[transport]
			}
		}
		total += bucketSum
		n++
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

// HasUsableHistory reports whether the queue holds enough buckets to
// compute a non-trivial rate for any window. The rater uses this to
// decide between returning data and returning Unavailable.
func HasUsableHistory(q *OverflowQueue[*TimestampedCounts]) bool {
	return q.Length() >= 2
}
