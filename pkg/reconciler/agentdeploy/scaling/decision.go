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

// Package scaling implements the AgentDeploy autoscaler.
//
// It splits into two layers:
//
//   - Estimator (EstimateKnee): learns each replica's saturation capacity from
//     historical load, weighting recent samples and reporting how much to trust
//     the result.
//   - Controller (Decide): a pure function that turns the estimate plus the live
//     snapshot into the next replica count, applying the operator's saturation
//     target, a latency safety guard, a surge fast-path, and per-direction
//     cooldowns/step caps.
//
// Decide does no I/O and reads no clocks beyond the supplied Now/LastScaledAt,
// so it is deterministic and fully testable. Orchestration (metrics collection,
// deployment patching) lives elsewhere.
package scaling

import (
	"math"
	"time"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// Reason classifies why Decide produced its output.
type Reason string

const (
	ReasonDisabled       Reason = "disabled"
	ReasonMinEqualsMax   Reason = "min equals max"
	ReasonNotReady       Reason = "no ready replicas"
	ReasonCooldownUp     Reason = "scale-up cooldown"
	ReasonCooldownDown   Reason = "scale-down cooldown"
	ReasonManualOutRange Reason = "manual scale outside [min,max]"
	ReasonScaleUp        Reason = "scale up"
	ReasonScaleDown      Reason = "scale down"
	ReasonSurge          Reason = "surge"
	ReasonNoChange       Reason = "no change"
	ReasonDriftToMin     Reason = "idle drift toward min"
)

// Inputs carries everything a per-tick decision needs.
type Inputs struct {
	// SpecifiedReplicas is what the AgentDeploy spec currently asks for.
	SpecifiedReplicas int32
	// ReadyReplicas is what's actually serving traffic.
	ReadyReplicas int32
	// History is the rolling window of past samples used to learn capacity.
	History []Sample
	// Current is the latest live snapshot, the load to act on now.
	Current Sample
	// Spec is the Scale spec from the AgentDeploy.
	Spec kmv1.Scale
	// Now is the decision timestamp. Passed in (not time.Now()) to keep
	// Decide pure and deterministically testable.
	Now time.Time
	// LastScaledAt is the timestamp of the last successful scale operation,
	// used to enforce cooldowns.
	LastScaledAt time.Time
}

// Decision is the output.
type Decision struct {
	DesiredReplicas int32
	Reason          Reason
	Skip            bool
	// Estimate is the capacity estimate behind this decision, surfaced for
	// logging/metrics.
	Estimate Estimate
}

// Default cooldown / step / capacity values applied when Spec leaves a field
// unset. Conservative on purpose — operators opt into faster scaling by
// setting explicit values.
const (
	defaultScaleUpCooldownSec   uint32 = 90
	defaultScaleDownCooldownSec uint32 = 90
	defaultReplicasPerScaleUp   uint32 = 2
	defaultReplicasPerScaleDown uint32 = 2
	// defaultKneePerReplica is the assumed per-replica capacity before the
	// estimator has learned anything (cold start). The controller blends away
	// from it as confidence grows.
	defaultKneePerReplica float64 = 20
	// defaultTargetSaturationPct is the steady-state fraction of capacity to
	// run at when the operator sets no explicit target.
	defaultTargetSaturationPct uint32 = 80
)

// Tuning constants for the safety mechanisms.
const (
	// confFloor is the saturation discount applied at zero confidence: even a
	// fully-trusted target is pulled to confFloor*target when we know nothing,
	// so a cold start over-provisions rather than under-provisions.
	confFloor = 0.6
	// surgeRatio: provisioned capacity overshot by this factor triggers the
	// fast-path (bypass cooldown, jump straight to desired).
	surgeRatio = 1.5
)

// Decide returns the next replica count and the reason behind it. Pure: same
// inputs always produce the same output.
//
// Order of checks (each short-circuits with Skip=true unless noted):
//  1. Spec.Disabled
//  2. min == max (sets DesiredReplicas=min; Skip iff current already equals it)
//  3. current outside [min,max] — manual scale, patch toward clamp, no cooldown
//  4. ReadyReplicas == 0
//  5. no observable load — drift toward min, step-capped, scale-down cooldown
//  6. desired = ceil(totalInflight / target); target derived from the learned
//     knee, the saturation lever, and confidence
//  7. scale up: surge fast-path, else step cap + scale-up cooldown
//  8. scale down: step cap + scale-down cooldown (no fast-path — asymmetric)
func Decide(in Inputs) Decision {
	if in.Spec.Disabled {
		return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonDisabled, Skip: true}
	}

	minR, maxR := minMax(in.Spec)
	if minR == maxR {
		skip := in.SpecifiedReplicas == minR
		return Decision{DesiredReplicas: minR, Reason: ReasonMinEqualsMax, Skip: skip}
	}

	// Manual scaling escape hatch: spec.replicas outside [min,max] is pulled
	// back into range immediately, bypassing cooldowns and step caps.
	if in.SpecifiedReplicas < minR || in.SpecifiedReplicas > maxR {
		desired := clamp(in.SpecifiedReplicas, minR, maxR)
		return Decision{DesiredReplicas: desired, Reason: ReasonManualOutRange, Skip: false}
	}

	//TODO: need to revisit
	if in.ReadyReplicas == 0 {
		return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonNotReady, Skip: true}
	}

	scaleUpCooldown := time.Duration(getOr(in.Spec.ScaleUpCooldownSeconds, defaultScaleUpCooldownSec)) * time.Second
	scaleDownCooldown := time.Duration(getOr(in.Spec.ScaleDownCooldownSeconds, defaultScaleDownCooldownSec)) * time.Second
	stepUp := int32(getOr(in.Spec.ReplicasPerScaleUp, defaultReplicasPerScaleUp))
	stepDown := int32(getOr(in.Spec.ReplicasPerScaleDown, defaultReplicasPerScaleDown))
	sinceLast := in.Now.Sub(in.LastScaledAt)

	totalInflight := in.Current.InflightPerRep * float64(inflightBasis(in))

	// Idle: no observable load. Drift toward Min over time, step-capped, gated
	// by the scale-down cooldown.
	if totalInflight <= 0 {
		if in.SpecifiedReplicas <= minR {
			return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonNoChange, Skip: true}
		}
		if sinceLast < scaleDownCooldown {
			return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonCooldownDown, Skip: true}
		}
		next := max(in.SpecifiedReplicas-stepDown, minR)
		return Decision{DesiredReplicas: next, Reason: ReasonDriftToMin, Skip: false}
	}

	est := EstimateKnee(in.History, in.Now)
	target := scalingTarget(in, est)
	desired := clamp(int32(math.Ceil(totalInflight/target)), minR, maxR)

	if desired == in.SpecifiedReplicas {
		return Decision{DesiredReplicas: desired, Reason: ReasonNoChange, Skip: true, Estimate: est}
	}

	if desired > in.SpecifiedReplicas {
		return scaleUp(in, est, desired, target, totalInflight, stepUp, scaleUpCooldown, sinceLast)
	}
	return scaleDown(in, est, desired, stepDown, scaleDownCooldown, sinceLast)
}

// scalingTarget computes the per-replica in-flight level to aim for: the
// learned (or default) knee, scaled by the operator's saturation lever and made
// more conservative when confidence is low. Never returns <= 0.
func scalingTarget(in Inputs, est Estimate) float64 {
	knee := lerp(defaultKneePerReplica, est.KneePerReplica, est.Confidence)
	s := targetSaturation(in.Spec)
	effectiveS := s * (confFloor + (1-confFloor)*clampF(est.Confidence, 0, 1))
	return math.Max(knee*effectiveS, 1e-9)
}

// scaleUp handles desired > current. A severe under-provision takes the surge
// fast-path: ignore the cooldown and jump straight to desired. Otherwise the
// normal step cap and scale-up cooldown apply.
func scaleUp(in Inputs, est Estimate, desired int32, target, totalInflight float64, stepUp int32, cooldown, sinceLast time.Duration) Decision {
	if isSurge(in, target, totalInflight) {
		return Decision{DesiredReplicas: desired, Reason: ReasonSurge, Skip: false, Estimate: est}
	}
	if sinceLast < cooldown {
		return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonCooldownUp, Skip: true, Estimate: est}
	}
	diff := min(desired-in.SpecifiedReplicas, stepUp)
	return Decision{DesiredReplicas: in.SpecifiedReplicas + diff, Reason: ReasonScaleUp, Skip: false, Estimate: est}
}

// scaleDown handles desired < current. Deliberately has no fast-path: shedding
// capacity is always gentle (step cap + scale-down cooldown) to avoid tearing
// down replicas a brief dip will need back.
func scaleDown(in Inputs, est Estimate, desired, stepDown int32, cooldown, sinceLast time.Duration) Decision {
	if sinceLast < cooldown {
		return Decision{DesiredReplicas: in.SpecifiedReplicas, Reason: ReasonCooldownDown, Skip: true, Estimate: est}
	}
	diff := min(in.SpecifiedReplicas-desired, stepDown)
	return Decision{DesiredReplicas: in.SpecifiedReplicas - diff, Reason: ReasonScaleDown, Skip: false, Estimate: est}
}

// isSurge reports whether the load is severe enough to bypass scale-up
// throttling: provisioned capacity is overshot by surgeRatio.
func isSurge(in Inputs, target, totalInflight float64) bool {
	capacity := float64(in.SpecifiedReplicas) * target
	return capacity > 0 && totalInflight/capacity >= surgeRatio
}

// inflightBasis is the replica count to multiply per-replica in-flight by to
// recover total in-flight: the count the metrics were measured over, falling
// back to ready then specified replicas.
func inflightBasis(in Inputs) int32 {
	if in.Current.Replicas > 0 {
		return in.Current.Replicas
	}
	if in.ReadyReplicas > 0 {
		return in.ReadyReplicas
	}
	return in.SpecifiedReplicas
}

// targetSaturation resolves the saturation lever to a fraction in (0,1].
// Unset or 0 falls back to the default; values above 100 are clamped (the
// admission webhook is expected to reject them outright).
func targetSaturation(s kmv1.Scale) float64 {
	pct := getOr(s.TargetSaturationPercentage, defaultTargetSaturationPct)
	if pct == 0 {
		pct = defaultTargetSaturationPct
	}
	if pct > 100 {
		pct = 100
	}
	return float64(pct) / 100
}

// minMax resolves the Scale spec's min/max into concrete int32s.
func minMax(s kmv1.Scale) (int32, int32) {
	minR := int32(1)
	if s.Min != nil && *s.Min > 1 {
		minR = *s.Min
	}
	maxR := int32(math.MaxInt32)
	if s.Max != nil {
		maxR = *s.Max
	}
	if maxR < minR {
		maxR = minR
	}
	return minR, maxR
}

func clamp(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func getOr(p *uint32, fallback uint32) uint32 {
	if p == nil {
		return fallback
	}
	return *p
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*clampF(t, 0, 1)
}
