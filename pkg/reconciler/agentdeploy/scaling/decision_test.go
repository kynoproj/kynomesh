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

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func ptrI32(v int32) *int32   { return &v }
func ptrU32(v uint32) *uint32 { return &v }

// baseSpec returns a Scale spec with explicit values so tests override one
// field at a time. With pct=100 and no history (cold start), the per-replica
// target is defaultKneePerReplica(20) * 1.0 * confFloor(0.6) = 12.
func baseSpec() kmv1.Scale {
	return kmv1.Scale{
		Min:                        ptrI32(1),
		Max:                        ptrI32(10),
		TargetSaturationPercentage: ptrU32(100),
		ScaleUpCooldownSeconds:     ptrU32(60),
		ScaleDownCooldownSeconds:   ptrU32(120),
		ReplicasPerScaleUp:         ptrU32(2),
		ReplicasPerScaleDown:       ptrU32(2),
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	longAgo := now.Add(-time.Hour)

	tests := []struct {
		name         string
		mutateSpec   func(s *kmv1.Scale)
		specified    int32
		ready        int32
		curReplicas  int32
		curInflight  float64
		lastScaledAt time.Time
		wantRepl     int32
		wantSkip     bool
		wantWhy      Reason
	}{
		{
			name:         "min equals max and current matches",
			mutateSpec:   func(s *kmv1.Scale) { s.Min, s.Max = ptrI32(4), ptrI32(4) },
			specified:    4,
			ready:        4,
			curReplicas:  4,
			curInflight:  25,
			lastScaledAt: longAgo,
			wantRepl:     4,
			wantSkip:     true,
			wantWhy:      ReasonMinEqualsMax,
		},
		{
			name:         "min equals max and current diverges",
			mutateSpec:   func(s *kmv1.Scale) { s.Min, s.Max = ptrI32(4), ptrI32(4) },
			specified:    7,
			ready:        7,
			curReplicas:  7,
			curInflight:  25,
			lastScaledAt: longAgo,
			wantRepl:     4,
			wantSkip:     false,
			wantWhy:      ReasonMinEqualsMax,
		},
		{
			name:         "current below min snaps to min",
			specified:    0,
			ready:        0,
			curReplicas:  0,
			curInflight:  0,
			lastScaledAt: longAgo,
			wantRepl:     1,
			wantSkip:     false,
			wantWhy:      ReasonManualOutRange,
		},
		{
			name:         "current above max snaps to max ignoring cooldown",
			specified:    50,
			ready:        50,
			curReplicas:  50,
			curInflight:  0,
			lastScaledAt: now,
			wantRepl:     10,
			wantSkip:     false,
			wantWhy:      ReasonManualOutRange,
		},
		{
			name:         "idle at min stays put",
			specified:    1,
			ready:        1,
			curReplicas:  1,
			curInflight:  0,
			lastScaledAt: longAgo,
			wantRepl:     1,
			wantSkip:     true,
			wantWhy:      ReasonNoChange,
		},
		{
			name:         "idle drifts toward min one step at a time",
			specified:    5,
			ready:        5,
			curReplicas:  5,
			curInflight:  0,
			lastScaledAt: longAgo,
			wantRepl:     3,
			wantSkip:     false,
			wantWhy:      ReasonDriftToMin,
		},
		{
			name:         "idle drift respects scale-down cooldown",
			specified:    5,
			ready:        5,
			curReplicas:  5,
			curInflight:  0,
			lastScaledAt: now.Add(-30 * time.Second),
			wantRepl:     5,
			wantSkip:     true,
			wantWhy:      ReasonCooldownDown,
		},
		{
			name:         "idle drift clamps last step to min",
			specified:    2,
			ready:        2,
			curReplicas:  2,
			curInflight:  0,
			lastScaledAt: longAgo,
			wantRepl:     1,
			wantSkip:     false,
			wantWhy:      ReasonDriftToMin,
		},
		{
			name:         "scale up by step cap without surge",
			mutateSpec:   func(s *kmv1.Scale) { s.Max = ptrI32(50) },
			specified:    8,
			ready:        8,
			curReplicas:  8,
			curInflight:  17.5, // total 140, target 12, ratio 1.46 < surge; desired 12, +2 cap
			lastScaledAt: longAgo,
			wantRepl:     10,
			wantSkip:     false,
			wantWhy:      ReasonScaleUp,
		},
		{
			name:         "scale-up cooldown blocks",
			mutateSpec:   func(s *kmv1.Scale) { s.Max = ptrI32(50) },
			specified:    8,
			ready:        8,
			curReplicas:  8,
			curInflight:  17.5,
			lastScaledAt: now.Add(-30 * time.Second), // need 60s
			wantRepl:     8,
			wantSkip:     true,
			wantWhy:      ReasonCooldownUp,
		},
		{
			name:         "surge still respects scale-up cooldown",
			specified:    2,
			ready:        2,
			curReplicas:  2,
			curInflight:  50, // total 100, capacity 24, ratio 4.17 → surge, but cooldown not elapsed
			lastScaledAt: now,
			wantRepl:     2,
			wantSkip:     true,
			wantWhy:      ReasonCooldownUp,
		},
		{
			name:         "surge scales up by step cap once cooldown elapsed",
			specified:    2,
			ready:        2,
			curReplicas:  2,
			curInflight:  50, // total 100, target 12, ratio 4.17 → surge; desired ceil(100/12)=9, +2 step cap
			lastScaledAt: longAgo,
			wantRepl:     4,
			wantSkip:     false,
			wantWhy:      ReasonSurge,
		},
		{
			name:         "scale down by step cap",
			specified:    8,
			ready:        8,
			curReplicas:  8,
			curInflight:  3, // total 24, desired ceil(24/12)=2, -2 cap
			lastScaledAt: longAgo,
			wantRepl:     6,
			wantSkip:     false,
			wantWhy:      ReasonScaleDown,
		},
		{
			name:         "scale-down cooldown blocks",
			specified:    8,
			ready:        8,
			curReplicas:  8,
			curInflight:  3,
			lastScaledAt: now.Add(-60 * time.Second), // need 120s
			wantRepl:     8,
			wantSkip:     true,
			wantWhy:      ReasonCooldownDown,
		},
		{
			name:         "no change when desired matches current",
			specified:    4,
			ready:        4,
			curReplicas:  4,
			curInflight:  12, // total 48, desired ceil(48/12)=4
			lastScaledAt: longAgo,
			wantRepl:     4,
			wantSkip:     true,
			wantWhy:      ReasonNoChange,
		},
		{
			name: "default saturation (unset) scales up",
			mutateSpec: func(s *kmv1.Scale) {
				s.Max = ptrI32(50)
				s.TargetSaturationPercentage = nil // s=0.8 → target 20*0.8*0.6=9.6
			},
			specified:    10,
			ready:        10,
			curReplicas:  10,
			curInflight:  13, // total 130, capacity 96, ratio 1.35 < surge; desired ceil(130/9.6)=14, +2 cap
			lastScaledAt: longAgo,
			wantRepl:     12,
			wantSkip:     false,
			wantWhy:      ReasonScaleUp,
		},
		{
			name: "default step and cooldown applied when unset",
			mutateSpec: func(s *kmv1.Scale) {
				s.Max = ptrI32(50)
				s.ReplicasPerScaleUp = nil     // default 2
				s.ScaleUpCooldownSeconds = nil // default 90s, elapsed 1h
			},
			specified:    5,
			ready:        5,
			curReplicas:  5,
			curInflight:  17, // total 85, capacity 60, ratio 1.42 < surge; desired ceil(85/12)=8, +2 cap
			lastScaledAt: longAgo,
			wantRepl:     7,
			wantSkip:     false,
			wantWhy:      ReasonScaleUp,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := baseSpec()
			if tc.mutateSpec != nil {
				tc.mutateSpec(&spec)
			}
			got := Decide(Inputs{
				CurrentReplicas: tc.specified,
				ReadyReplicas:   tc.ready,
				Current: Sample{
					Timestamp:      now,
					Replicas:       tc.curReplicas,
					InflightPerRep: tc.curInflight,
				},
				Spec:         spec,
				Now:          now,
				LastScaledAt: tc.lastScaledAt,
			})
			assert.Equal(t, tc.wantRepl, got.DesiredReplicas, "desired replicas")
			assert.Equal(t, tc.wantSkip, got.Skip, "skip flag")
			assert.Equal(t, tc.wantWhy, got.Reason, "reason")
		})
	}
}

// TestDecideUsesLearnedCapacity verifies the estimate flows into the decision:
// at comparable confidence, a lower learned knee (less capacity per replica)
// demands more replicas for the same load than a higher learned knee.
func TestDecideUsesLearnedCapacity(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	spec := baseSpec()
	spec.Max = ptrI32(50)

	decideWith := func(hist []Sample) Decision {
		return Decide(Inputs{
			CurrentReplicas: 5,
			ReadyReplicas:   5,
			History:         hist,
			Current:         Sample{Timestamp: now, Replicas: 5, InflightPerRep: 20, RatePerRep: 200},
			Spec:            spec,
			Now:             now,
			LastScaledAt:    now.Add(-time.Hour),
		})
	}

	lowKnee := decideWith(genSeries(now, 2*time.Minute, 60, 5, 0.1, 2, 25))   // knee ≈ 5
	highKnee := decideWith(genSeries(now, 2*time.Minute, 60, 20, 0.1, 2, 40)) // knee ≈ 20

	assert.InDelta(t, 5, lowKnee.Estimate.KneePerReplica, 3, "low learned knee")
	assert.Greater(t, lowKnee.Estimate.Confidence, 0.5, "should be confident")
	assert.Greater(t, lowKnee.DesiredReplicas, highKnee.DesiredReplicas,
		"lower per-replica capacity demands more replicas for the same load")
}

// TestDecideColdStartReactsToSurge verifies a brand-new deployment with no
// history reacts to a load surge once the scale-up cooldown is clear, scaling
// up by the step cap and tagging the decision as a surge. The surge does not
// bypass the cooldown or the step cap — it only ramps faster by continuing to
// step up on each tick while the load persists.
func TestDecideColdStartReactsToSurge(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	spec := baseSpec()
	spec.Max = ptrI32(50)
	inputs := Inputs{
		CurrentReplicas: 1,
		ReadyReplicas:   1,
		History:         nil, // cold start
		Current:         Sample{Timestamp: now, Replicas: 1, InflightPerRep: 80},
		Spec:            spec,
		Now:             now,
	}

	// Cooldown not elapsed: even a surge is held.
	inputs.LastScaledAt = now
	held := Decide(inputs)
	assert.Equal(t, ReasonCooldownUp, held.Reason)
	assert.Equal(t, int32(1), held.DesiredReplicas, "surge does not bypass the cooldown")

	// Cooldown elapsed: react to the surge, step-capped (+2), tagged surge.
	inputs.LastScaledAt = now.Add(-time.Hour)
	acted := Decide(inputs)
	assert.Equal(t, ReasonSurge, acted.Reason)
	assert.Equal(t, int32(3), acted.DesiredReplicas, "cold start ramps by the step cap, not straight to desired")
}
