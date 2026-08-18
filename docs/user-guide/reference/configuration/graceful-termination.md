# Graceful Termination

When an agent's pod is deleted — scale-down, rolling update, or manual deletion
— the broker drains in-flight requests before the pod actually terminates, so
long-running agentic calls aren't cut off mid-flight. This is controlled by the
standard Kubernetes
[`terminationGracePeriodSeconds`](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination),
set per agent via `.spec.agents[*].terminationGracePeriodSeconds`:

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
      terminationGracePeriodSeconds: 300
```

If unset, it defaults to 120 seconds. Raise it for agents that make long-running
tool calls; lower it (down to Kubernetes' own minimums) for agents where fast
pod turnover matters more than finishing in-flight work.

## How draining works

The grace period is split into two sequential phases, both carved out of the
single `terminationGracePeriodSeconds` budget:

1. **preStop drain.** Before `SIGTERM` is sent, Kubernetes runs the broker's
   `preStop` hook (`kynomesh drain`). It first waits out a fixed propagation
   delay — so Kubernetes has time to remove the pod from Service endpoints
   before the broker starts checking — then polls its own `/metrics` and waits
   for `broker_inflight_requests` to reach zero before returning, up to its
   share of the budget.
2. **Post-`SIGTERM` shutdown.** Once the `preStop` hook returns, Kubernetes
   sends `SIGTERM`. The broker performs a graceful HTTP/gRPC server shutdown
   (stops accepting new connections, lets accepted ones finish), bounded by the
   remaining budget, before the process exits or the kubelet `SIGKILL`s it.

The budget split isn't a fixed fraction — it's computed from the actual grace
period so both phases stay useful at very short or very long settings:

| Grace period   | Propagation wait | Post-`SIGTERM` shutdown | Drain window (the rest)                                                                     |
| -------------- | ---------------- | ----------------------- | ------------------------------------------------------------------------------------------- |
| shrinks toward | 2s floor         | 5s floor                | falls back to the propagation wait alone if the grace period is too small for a real window |
| grows toward   | 10s ceiling      | 15s ceiling             | grows to absorb the remaining budget                                                        |

Concretely: the propagation wait is `grace / 20`, clamped to `[2s, 10s]`; the
post-`SIGTERM` shutdown is `grace / 10`, clamped to `[5s, 15s]`; the drain
window gets whatever's left after both, minus a small safety margin. At the 120s
default, that's a 6s propagation wait, a 12s shutdown budget, and a 106s window
(including the propagation wait) for in-flight requests to finish.

A `preStop` drain that times out with requests still in flight is not treated as
an error — it's expected behavior under load. The post-`SIGTERM` phase and,
ultimately, the kubelet's own `SIGKILL` at the end of the grace period handle
whatever's left. Raising `terminationGracePeriodSeconds` is the lever for giving
slow requests more room before that happens.

> [!NOTE] `broker_inflight_requests` counts a streaming call (gRPC stream or
> SSE) as in-flight for its entire lifetime, not per message. A long-lived
> stream connected to a pod being drained holds the gauge above zero and keeps
> the `preStop` hook waiting for the full drain window even if no new data is
> flowing.

## See Also

- [Rolling Update](rolling-update.md) — how `terminationGracePeriodSeconds`
  interacts with the batched pod replacement during a spec change.
- [Metrics](../../../operations/metrics.md) — `broker_inflight_requests` and the
  other broker metrics the drain hook itself scrapes.
