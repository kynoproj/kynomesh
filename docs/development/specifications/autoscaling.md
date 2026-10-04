# Autoscaling Mechanism

> [!NOTE] This document is for Kynomesh contributors. It explains the design of
> the built-in autoscaler - how load is sampled, how capacity is learned, and
> how replica decisions are made. For the user-facing configuration reference,
> see [Autoscaling](../../user-guide/reference/autoscaling.md).

## Problem

An agent's right-sized replica count isn't knowable in advance: it depends on
the agent's own per-request cost (model latency, tool calls, context size),
which Kynomesh has no prior knowledge of. CPU/memory-based HPA is a poor fit for
agentic workloads - a replica waiting on an upstream LLM call is idle on CPU
while fully occupied on the resource that actually matters: how many concurrent
requests it can hold at once. The autoscaler's job is to learn each
AgentDeploy's actual per-replica concurrency capacity from observed traffic,
with no agent-specific tuning required, and keep replicas near a target fraction
of it.

## Two independent loops

Autoscaling is split into two concerns that run on their own schedules:

- **Sampling** - periodically measures each AgentDeploy's current load and
  records it as history.
- **Deciding** - periodically reads that history, learns capacity from it, and
  adjusts the replica count.

They're independent so that history keeps accumulating even for an AgentDeploy
with autoscaling disabled (`scale.disabled: true`) - turning it back on later
doesn't start from zero. They also run at different cadences: sampling must
happen often enough to build a usable history; deciding can run less often since
replica changes are inherently coarse-grained and must respect cooldowns anyway.

Both loops act per-AgentDeploy, independently - an agent's learned capacity and
replica count are never shared with or influenced by any other AgentDeploy's
history, even one running the same image in the same AgentSet.

## Sampling

On each tick, for every tracked AgentDeploy:

1. Query the AgentSet's daemon for the fleet's current in-flight concurrency and
   processing rate, averaged over a window (see below).
2. Divide by the AgentDeploy's ready replica count to get **per-replica**
   values, and record that as one history sample.
3. Persist the growing history on a slower cadence (not every tick).

A sample carries only concurrency and throughput - no latency. Latency is
dominated by upstream LLM/tool response time and can't distinguish a saturated
replica from a busy-but-healthy one, so it's deliberately excluded from the
signal used to learn capacity.

### Why per-replica, not fleet totals

Capacity is a per-replica property. A measurement taken at 3 replicas and one
taken at 10 replicas are only comparable once both are normalized to per-replica
concurrency and rate - otherwise the learner would be tracking "how much load
did the fleet handle" rather than "how much load does one replica saturate at,"
which is the quantity actually needed to size the fleet.

### Adaptive averaging window

The window used to average load isn't fixed. It's derived from the typical time
a request takes to complete (inferred from recent history via Little's Law:
duration = concurrency / rate), then scaled up by a small multiple and clamped
to a sane range. Before any duration can be inferred (cold start), a short
default window is used instead.

The intent: a fast workload (sub-second requests) is averaged over a short
window so the signal tracks real-time load; a slow workload (tens of seconds per
request, e.g. a multi-step agent call) is averaged over a longer window so a
handful of slow in-flight requests don't look like transient noise that should
be ignored.

### Persistence and rollout isolation

History is persisted per AgentDeploy so a controller restart or leader failover
doesn't force every agent to relearn its capacity from scratch. It's bounded by
both age and size, with recency-weighting (below) making old samples
near-irrelevant long before either bound is hit.

Every recorded sample is tagged with the pod spec it was collected under. When
that spec changes (a new rollout), prior history is dropped rather than kept:
capacity learned from a previous container image, resource request, or config is
not a valid prior for a different one, and must not leak across a rollout.

### Warmup exclusion

Samples taken shortly after a replica-count change are excluded when learning. A
pod that was just added is still warming caches and connections, so its reported
capacity is transient, not steady state, and would otherwise bias the learned
capacity low.

## Learning capacity (the saturation knee)

### What it means

Below saturation, adding in-flight concurrency raises a replica's processing
rate roughly linearly - more requests in flight, proportionally more throughput.
At saturation, the rate plateaus: more concurrency doesn't buy more throughput,
it just means requests queue or compete for the same constrained resource. The
**knee** is the concurrency level at which that plateau begins - the practical
ceiling on what one replica can usefully hold.

This throughput-vs-concurrency relationship is **mix-invariant** - it doesn't
depend on per-request latency or the blend of request types, so it holds up
under a heterogeneous workload, unlike a fixed "N concurrent requests per
replica" knob a user would otherwise have to guess and retune per agent.

### How it's learned

Recent history is weighted more heavily than old history, so the estimate tracks
current capacity rather than a stale multi-day average. Samples are grouped by
observed concurrency level, and the system walks that curve from low to high
concurrency looking for where throughput growth tapers off - the last point
still clearly rising is taken as the knee, which is the conservative choice: it
biases the estimate toward _more_ replicas, not fewer.

If saturation was never actually observed in the available history (load never
got heavy enough), the highest concurrency seen so far is reported as a lower
bound ("at least this much capacity, probably more") rather than a confident
answer.

### Confidence

Every estimate carries a confidence score that reflects how much to trust it,
based on:

- **How long** the system has been observing this agent - a few minutes of data
  is treated as the full trustworthy window; estimates beyond that don't gain
  further confidence from age alone.
- **How wide a concurrency range was actually observed** - a curve glimpsed only
  through a narrow range of load is less trustworthy than one traced across a
  broad range, even with the same number of samples.
- **Whether saturation was actually observed**, discounting a lower-bound
  estimate relative to a confirmed one.

Too little history at all (below a minimum sample count) reports zero confidence
outright, signaling "fall back to a conservative default."

## Deciding the next replica count

Each decision tick, in order:

1. **Fixed replica count** (min equals max): hold at that number.
2. **Current replicas outside the configured min/max** (e.g. after a
   configuration change, or a manual override): snap back into range
   immediately, bypassing cooldowns - this is correcting a configuration
   mismatch, not responding to load, so normal pacing doesn't apply.
3. **No observable load**: drift down toward the minimum over time, still paced
   by the scale-down cooldown and step cap - an idle agent eventually settles at
   its floor rather than staying at whatever count it last needed.
4. **Otherwise, compute a target per-replica load** from the learned capacity,
   the configured target saturation percentage, and the estimate's confidence -
   lower confidence pulls the target down, so a cold start deliberately
   over-provisions (more replicas than strictly necessary) rather than risks
   under-provisioning before enough data exists to know better. Desired replicas
   is the current load divided by that target, rounded up and clamped to [min,
   max].
5. **Scaling up** is refused outright if a configured rate-limit ceiling on
   total in-flight requests is already reached - more replicas can't raise a
   capped external ceiling, so adding them would be wasted capacity (see
   [Rate limiting](../../user-guide/reference/rate-limiting.md)). Otherwise it's
   gated by the scale-up cooldown and capped to a maximum step size per tick. A
   severe spike is flagged for observability but is **not** fast- tracked past
   the cooldown or step cap - it simply keeps stepping up on every subsequent
   tick until it catches up. A single-tick stampede to the maximum on one spike
   would be worse than a bounded, if slightly slower, ramp.
6. **Scaling down** is symmetric: gated by the scale-down cooldown and capped to
   a maximum step size per tick, for the same reason in reverse - a brief dip in
   load shouldn't shed replicas the agent needs back a moment later.

### Why the mechanism leans toward over-provisioning

Running a few replicas more than strictly needed costs money; running fewer than
needed costs request latency or queuing, which is the more visible and
disruptive failure mode for callers. Several independent choices encode the same
bias: the cold-start discount on the scaling target, the conservative
(last-rising-point, not first-plateau-point) choice of knee, and scale-up having
no cooldown-bypassing path for urgency while the "clamp back into range" and
"drift toward minimum" paths are the only ones that skip pacing guards, and both
correct toward configuration, not performance.

## Guardrails before any decision is acted on

Before a scaling decision is computed at all, a tick is skipped when:

- Autoscaling is disabled for this agent (sampling still continues regardless,
  so history isn't lost while disabled).
- The AgentDeploy isn't in a healthy running state yet, or has zero ready
  replicas to measure from.
- A rollout is already in flight for this AgentDeploy - scaling never fights a
  concurrent spec update. This is the same kind of gate the
  [AgentCard drift reload](agentcard-drift-reload.md#proposed-direction)
  mechanism defers to.
- A previous replica-count change hasn't finished converging yet.
- Too little time has passed since the last scaling operation (cooldown) -
  checked cheaply up front, in addition to the more granular per-direction
  cooldown checks in the decision itself.
- There's no history yet, or the freshest sample is too stale - the autoscaler
  refuses to act on absent or outdated data rather than scale blind, guarding
  against a stalled sampling loop or an unreachable metrics source silently
  driving decisions off dead numbers.

A decision that does go through only ever changes the target replica count;
rolling that out to actual pods (creating/terminating them, respecting rollout
batching) is the ordinary deployment reconciliation path, not something the
autoscaler does itself.

## Production cadence

Sampling runs roughly once a minute per agent; scaling decisions are evaluated
roughly every 30 seconds; a scale operation in either direction is followed by a
90-second cooldown by default before another one can happen (in either
direction - a cooldown from any scale event blocks all scaling, not just a
repeat in the same direction). These cadences are fixed in the production
deployment, not user- or environment-configurable - only the per-agent tuning
knobs described in the
[user-facing configuration reference](../../user-guide/reference/autoscaling.md)
(min/max, cooldown seconds, step size, target saturation) are.

One practical consequence: because any scale event's cooldown blocks scaling in
both directions, observing a realistic scale-up-then-scale-down sequence takes
at least as long as the cooldown even under heavy load changes. Tests or demos
that want to exercise both directions quickly need a short cooldown configured
for that agent.

## Observability

Per-AgentDeploy metrics expose the learned capacity, its confidence, the current
and last-computed-desired replica counts, how many load samples have been
recorded, and a count of scale operations by direction. See
[Metrics](../../operations/metrics.md) for the full catalog. These metric series
are cleaned up when an AgentDeploy is deleted, so they don't linger as untracked
series.

## Non-goals

- **No cross-AgentDeploy learning.** Each AgentDeploy's history and learned
  capacity are entirely independent, even between AgentDeploys running the same
  container image in the same AgentSet. An agent whose per-request cost varies
  with its own traffic mix (not just its code) wouldn't be well served by
  sharing another AgentDeploy's learned capacity.
- **No predictive or scheduled scaling.** Capacity is learned only from recorded
  history; there's no time-of-day or calendar-driven pre-scaling.
- **Not a replacement for the Kubernetes `scale` subresource contract.**
  AgentDeploy supports standard HPA/KEDA targeting specifically so operators who
  need predictive, custom-metric, or multi-signal scaling aren't boxed into this
  mechanism - see
  [Kubernetes HPA](../../user-guide/reference/autoscaling.md#kubernetes-hpa).

## See Also

- [Autoscaling (user guide)](../../user-guide/reference/autoscaling.md) -
  configuration reference (`scale` block, defaults, HPA/KEDA opt-out).
- [Rate limiting](../../user-guide/reference/rate-limiting.md) - the in-flight
  ceiling that caps scale-up.
- [Metrics](../../operations/metrics.md) - full Prometheus metrics catalog.
- [AgentCard Drift Detection and Dependent Reload](agentcard-drift-reload.md) -
  another controller-side mechanism that defers to an in-flight rollout the same
  way the autoscaler does.
