# Zero-Downtime Pod Replacement

Kynomesh never drops a request when an agent pod goes away — whether that's a
rolling update replacing pods with a new spec, the autoscaler scaling down, or
the controller recreating a pod for any other reason. This works out of the box:
there's no feature flag to turn on and no minimum configuration required. Every
AgentSet gets batched rolling replacement, readiness-gated traffic cutover, and
request draining on outgoing pods for free. This page explains how the pieces
work together and how to tune them for agents with slower startup or
longer-running calls.

The three pieces:

- **[Graceful Termination](configuration/graceful-termination.md)** decides how
  any outgoing pod leaves — the broker drains in-flight requests before the
  process exits, so it finishes the work it already accepted instead of dropping
  it. This applies to every pod deletion the controller performs: rolling
  replacement, scale-down, and anything else.
- **[Liveness And Readiness](configuration/liveness-and-readiness.md)** decides
  when a new pod is actually serving traffic — its readiness probe must pass
  before the broker is added to the agent's Service endpoints.
- **[Rolling Update](configuration/rolling-update.md)** is what layers on top of
  the two above specifically for spec changes: it bounds how many pods are
  replaced at once (`maxUnavailable`) and gates each batch on the replacement
  pods becoming Ready before moving on.

Together, these mean a request landing on an agent is always served by a pod
that's either fully Ready or still draining — never one that's half-started or
abruptly killed, regardless of why the pod is being replaced. You don't have to
wire any of this up yourself; it's the default behavior for every agent.

## How it works

Draining and readiness gating apply independently of what triggered the pod
change:

- **Rolling update** (spec change): for each batch of up to `maxUnavailable`
  replica slots still on the old spec, the controller deletes the old pod (which
  drains, see below) and creates its replacement, waiting for every pod in the
  batch to reach Ready before starting the next batch — see
  [Rolling Update](configuration/rolling-update.md).
- **Scale-down** (fewer desired replicas): the controller deletes the excess
  pods outright — the same draining behavior applies, just without a replacement
  pod to wait on.
- **Any other pod recreation** (duplicate cleanup, manual deletion, node drain,
  etc.): the pod being removed always goes through the same termination path, so
  it drains the same way.

For any pod being removed:

1. **preStop drain.** The controller deletes the pod. Its broker runs the
   `preStop` hook, which waits for Kubernetes to propagate the endpoint removal
   and then drains in-flight requests — see
   [Graceful Termination](configuration/graceful-termination.md) for the exact
   timing.

For a rolling update specifically, there's a second half:

2. **New pod: create and wait for Ready.** In the same pass, the controller
   creates the replacement pod on the new spec. The new pod isn't added to the
   agent's Service endpoints until its readiness probe passes — see
   [Liveness And Readiness](configuration/liveness-and-readiness.md) for the
   probe timing knobs.
3. **Batch gate.** Before starting the next batch of `maxUnavailable` slots, the
   controller waits for every replacement pod created in this batch to reach
   Ready — see [Rolling Update](configuration/rolling-update.md).

## Tuning guidelines

The defaults work well for typical agents, and most users never need to touch
them. But if your agent is slow to start, handles long-running tool calls, or
you're sensitive to reduced capacity during a rollout, these three settings
interact and are worth tuning together:

| Setting                                       | Where                            | Effect                                                                                                                                                                                             |
| --------------------------------------------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `terminationGracePeriodSeconds`               | `.spec.agents[*]`                | How long any outgoing pod is given to drain in-flight requests before `SIGKILL`, whether it's being replaced, scaled down, or otherwise removed. Raise it for agents with long-running tool calls. |
| `container.readinessProbe.*`                  | `.spec.agents[*].container`      | How long a new pod takes to be considered Ready and start receiving traffic. Raise `initialDelaySeconds` for agents with slow startup so they aren't marked Unready prematurely.                   |
| `updateStrategy.rollingUpdate.maxUnavailable` | `.spec.agents[*].updateStrategy` | How much capacity is replaced (and potentially unavailable) at once during a rolling update. Lower it if you're capacity-sensitive during rollouts.                                                |

If a rollout feels like it drops requests, check readiness first — a probe that
passes before the agent can actually handle traffic will route requests into a
pod that isn't really ready yet, regardless of how conservative `maxUnavailable`
or the grace period are. Two independent things can cause this, and both are
worth checking:

- **Timing.** The probe mechanism itself is fixed (see
  [Liveness And Readiness](configuration/liveness-and-readiness.md)), but its
  `initialDelaySeconds`/`periodSeconds`/`failureThreshold` are not — raise them
  to match how long your agent's process actually takes to come up.
- **Reported status.** By default, the SDK reports the agent as healthy as
  soon as the process starts, regardless of whether it's actually finished
  initializing. If your agent depends on something that takes time — loading
  a model, connecting to a database — customize the health check in your SDK
  (see each SDK's README) so it only reports healthy once the agent can
  really serve. No amount of `initialDelaySeconds` tuning fixes a health check
  that doesn't know it should wait.

## Example

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
        readinessProbe:
          initialDelaySeconds: 30
          periodSeconds: 10
          failureThreshold: 3
      terminationGracePeriodSeconds: 180
      updateStrategy:
        type: RollingUpdate
        rollingUpdate:
          maxUnavailable: 1
```

This gives new pods 30s before the first readiness check and gives outgoing pods
up to 180s to drain in-flight requests — whether they're being replaced during a
rollout or removed during a scale-down — and replaces one pod at a time during a
rolling update. A conservative profile for an agent that's slow to start and
handles long-running calls.

## See Also

- [Graceful Termination](configuration/graceful-termination.md) — the drain
  sequence for any outgoing pod.
- [Liveness And Readiness](configuration/liveness-and-readiness.md) — probe
  timing for incoming pods.
- [Rolling Update](configuration/rolling-update.md) — batching and
  `maxUnavailable` for spec changes.
- [Autoscaling](autoscaling.md) — how scale-down decides how many replicas to
  remove.
- [Metrics](../../operations/metrics.md) — `broker_inflight_requests` and other
  broker metrics useful for watching a rollout or scale-down in progress.
