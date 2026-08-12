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
	"github.com/stretchr/testify/require"
)

// TestDecide_RateLimitCeiling covers the autoscaler coupling: when a
// max-in-flight cap is set, scale-up is suppressed once total in-flight reaches
// it, because the cap protects an external dependency that more replicas cannot
// relieve.
func TestDecide_RateLimitCeiling(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	longAgo := now.Add(-time.Hour)

	// current=3, InflightPerRep=40 -> totalInflight=120. With the cold-start
	// target this wants a scale-up, so the rate-limit branch is exercised.
	const curReplicas = int32(3)
	const inflightPerRep = 40.0
	const totalInflight = 120.0

	build := func(maxInFlight int32) Inputs {
		return Inputs{
			CurrentReplicas: curReplicas,
			ReadyReplicas:   curReplicas,
			Current: Sample{
				Timestamp:      now,
				Replicas:       curReplicas,
				InflightPerRep: inflightPerRep,
			},
			Spec:         baseSpec(),
			MaxInFlight:  maxInFlight,
			Now:          now,
			LastScaledAt: longAgo,
		}
	}

	// A scale-up is either ReasonScaleUp or, under heavy load, ReasonSurge —
	// both are genuine scale-ups (surge just tags severity).
	isScaleUp := func(r Reason) bool { return r == ReasonScaleUp || r == ReasonSurge }

	t.Run("no cap scales up normally", func(t *testing.T) {
		got := Decide(build(0))
		require.False(t, got.Skip)
		assert.Greater(t, got.DesiredReplicas, curReplicas)
		assert.Truef(t, isScaleUp(got.Reason), "expected a scale-up reason, got %q", got.Reason)
	})

	t.Run("under cap scales up normally", func(t *testing.T) {
		got := Decide(build(int32(totalInflight) + 1)) // 121 > 120
		require.False(t, got.Skip)
		assert.Greater(t, got.DesiredReplicas, curReplicas)
		assert.Truef(t, isScaleUp(got.Reason), "expected a scale-up reason, got %q", got.Reason)
	})

	t.Run("at cap suppresses scale-up", func(t *testing.T) {
		got := Decide(build(int32(totalInflight))) // 120 == 120
		assert.True(t, got.Skip)
		assert.Equal(t, curReplicas, got.DesiredReplicas, "must hold at current")
		assert.Equal(t, ReasonRateLimited, got.Reason)
	})

	t.Run("over cap suppresses scale-up", func(t *testing.T) {
		got := Decide(build(int32(totalInflight) - 20)) // 100 < 120
		assert.True(t, got.Skip)
		assert.Equal(t, curReplicas, got.DesiredReplicas)
		assert.Equal(t, ReasonRateLimited, got.Reason)
	})

	t.Run("cap takes precedence over scale-up cooldown", func(t *testing.T) {
		in := build(int32(totalInflight))
		in.LastScaledAt = now // inside cooldown window
		got := Decide(in)
		assert.True(t, got.Skip)
		assert.Equal(t, ReasonRateLimited, got.Reason,
			"external-dependency ceiling is checked before the cooldown")
	})
}

// TestDecide_RateLimitDoesNotBlockScaleDown confirms the cap only gates
// scale-up: a fleet that should shed replicas still does, even at/over the cap.
func TestDecide_RateLimitDoesNotBlockScaleDown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	longAgo := now.Add(-time.Hour)

	// current=10 but only light load -> desired < current -> scale-down path.
	got := Decide(Inputs{
		CurrentReplicas: 10,
		ReadyReplicas:   10,
		Current: Sample{
			Timestamp:      now,
			Replicas:       10,
			InflightPerRep: 1, // total 10, well under capacity -> scale down
		},
		Spec:         baseSpec(),
		MaxInFlight:  5, // total in-flight (10) is over the cap, but that must not matter here
		Now:          now,
		LastScaledAt: longAgo,
	})
	require.False(t, got.Skip)
	assert.Less(t, got.DesiredReplicas, int32(10))
	assert.Equal(t, ReasonScaleDown, got.Reason)
}
