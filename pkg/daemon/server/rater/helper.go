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

// TransportTotal is the synthetic transport key used to represent
// the sum across all observed transports. It is not emitted by the
// broker; the rater computes it on demand by summing real transport
// values within each sample.
const TransportTotal = "__total__"

// samplesInWindow returns the subset of samples whose timestamps
// fall within [nowUnix - lookbackSeconds, nowUnix]. Samples are
// expected in append order (ascending timestamp); the function does
// not re-sort.
func samplesInWindow(samples []*PodSample, nowUnix, lookbackSeconds int64) []*PodSample {
	if len(samples) == 0 {
		return nil
	}
	cutoff := nowUnix - lookbackSeconds
	// Find the first sample at or after cutoff. Linear walk is fine:
	// MaxSamplesPerPod is in the hundreds and we touch this twice per
	// transport per query.
	for i, s := range samples {
		if s.Timestamp >= cutoff {
			return samples[i:]
		}
	}
	return nil
}

// processedValue returns the processed-messages value for the
// given transport, or the sum across transports when transport ==
// TransportTotal.
func processedValue(s *PodSample, transport string) float64 {
	if transport == TransportTotal {
		var v float64
		for _, c := range s.ProcessedByTransport {
			v += c
		}
		return v
	}
	return s.ProcessedByTransport[transport]
}

// inflightValue returns the in-flight value for the given transport,
// or the sum across transports when transport == TransportTotal.
func inflightValue(s *PodSample, transport string) float64 {
	if transport == TransportTotal {
		var v float64
		for _, g := range s.InflightByTransport {
			v += g
		}
		return v
	}
	return s.InflightByTransport[transport]
}

// podRate computes the per-second processing rate for one pod over
// the requested window.
//
// The window may straddle zero or more counter resets (pod/broker
// restarts). Each "run" between resets contributes its own delta:
//
//   - The first run's delta is (peak - first_in_window_value), where
//     "peak" is the last sample of the run.
//   - Subsequent runs start at 0 (post-reset), so their delta is
//     just the peak (or, for the still-open final run, the last
//     sample's value).
//
// Walking samples in order and committing a run's delta on each
// detected reset gives the right answer in all cases — including
// the no-reset case, where the algorithm reduces to (last - first).
//
// Returns (rate, true) when computable; (0, false) when the pod has
// fewer than 2 samples in the window, in which case the caller
// should treat this pod as contributing no signal to the aggregate.
func podRate(samples []*PodSample, nowUnix, lookbackSeconds int64, transport string) (float64, bool) {
	w := samplesInWindow(samples, nowUnix, lookbackSeconds)
	if len(w) < 2 {
		return 0, false
	}
	timeDiff := w[len(w)-1].Timestamp - w[0].Timestamp
	if timeDiff <= 0 {
		return 0, false
	}

	var delta float64
	runStart := processedValue(w[0], transport)
	prev := runStart
	for i := 1; i < len(w); i++ {
		curr := processedValue(w[i], transport)
		if curr < prev {
			// Reset: prev was the peak of the run that just ended.
			delta += prev - runStart
			// New run starts at 0 (the post-reset value at this
			// sample point is curr; the run "starts" at 0 and has
			// already reached curr by this sample).
			runStart = 0
		}
		prev = curr
	}
	// Commit the final (still-open) run.
	delta += prev - runStart

	return delta / float64(timeDiff), true
}

// CalculateRate sums per-pod rates across the AgentDeploy for the
// given transport over the requested window. Pods with insufficient
// in-window samples contribute zero.
func CalculateRate(b *AgentDeployBuffers, nowUnix, lookbackSeconds int64, transport string) float64 {
	var total float64
	for _, pod := range b.Pods() {
		samples := b.Samples(pod)
		if r, ok := podRate(samples, nowUnix, lookbackSeconds, transport); ok {
			total += r
		}
	}
	return total
}

// podInflightAvg computes the time-weighted average gauge value for
// one pod over the window.
//
// A sample's value represents the gauge from its timestamp until
// the NEXT sample's timestamp (step-function semantics). The last
// in-window sample contributes no weight — we don't extrapolate
// past the most recent observation. Likewise the window's leading
// gap (before the first sample) gets no weight — we don't
// extrapolate backwards.
//
// Edge cases:
//   - Zero samples in window: (0, false) — pod contributes nothing.
//   - One sample in window: (its value, true) — single observation
//     IS our best estimate, no duration to weight by.
//   - Multiple samples at the same timestamp (degenerate but
//     possible if a future change adds sub-second timestamping):
//     fall back to the last value rather than dividing by zero.
func podInflightAvg(samples []*PodSample, nowUnix, lookbackSeconds int64, transport string) (float64, bool) {
	w := samplesInWindow(samples, nowUnix, lookbackSeconds)
	if len(w) == 0 {
		return 0, false
	}
	if len(w) == 1 {
		return inflightValue(w[0], transport), true
	}
	var weightedSum float64
	var totalWeight float64
	for i := 0; i < len(w)-1; i++ {
		width := float64(w[i+1].Timestamp - w[i].Timestamp)
		weightedSum += inflightValue(w[i], transport) * width
		totalWeight += width
	}
	if totalWeight == 0 {
		return inflightValue(w[len(w)-1], transport), true
	}
	return weightedSum / totalWeight, true
}

// CalculateInflightAvg sums per-pod time-weighted gauge averages
// across the AgentDeploy. The result is "the average instantaneous
// AgentDeploy-wide in-flight load over the window."
func CalculateInflightAvg(b *AgentDeployBuffers, nowUnix, lookbackSeconds int64, transport string) float64 {
	var total float64
	for _, pod := range b.Pods() {
		samples := b.Samples(pod)
		if v, ok := podInflightAvg(samples, nowUnix, lookbackSeconds, transport); ok {
			total += v
		}
	}
	return total
}

// HasUsableHistory reports whether at least one pod has enough
// samples (≥2) to compute a rate. Used by the rater to decide
// between OK and Unavailable for the gRPC response.
//
// A pod with only one sample can still contribute to in-flight avg
// (single-sample best-effort), but rate math always needs two
// points.
func HasUsableHistory(b *AgentDeployBuffers) bool {
	for _, pod := range b.Pods() {
		if len(b.Samples(pod)) >= 2 {
			return true
		}
	}
	return false
}

// ObservedTransports returns every distinct transport label value
// seen in any sample for any pod.
func ObservedTransports(b *AgentDeployBuffers) []string {
	set := map[string]struct{}{}
	for _, pod := range b.Pods() {
		for _, s := range b.Samples(pod) {
			for k := range s.ProcessedByTransport {
				set[k] = struct{}{}
			}
			for k := range s.InflightByTransport {
				set[k] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
