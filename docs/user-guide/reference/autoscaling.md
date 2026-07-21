# Autoscaling

Kynomesh autoscales each agent independently based on the load it actually
receives. Every [AgentDeploy](../../core-concepts/agentdeploy.md) is scaled on
its own, so a busy agent grows while its idle peers stay small.

There are three ways to autoscale an agent:

- **Kynomesh autoscaling** — built in and on by default (below).
- **Kubernetes HPA** — drive replicas from CPU/memory or custom metrics.
- **Third-party autoscalers** — e.g. KEDA.

The AgentDeploy exposes a standard Kubernetes `scale` subresource
(`spec.replicas`), so HPA and third-party autoscalers can target it directly.
Only one autoscaler should own an agent at a time — disable Kynomesh autoscaling
when you use another (see [below](#kubernetes-hpa)).

## Kynomesh autoscaling

Kynomesh autoscaling is **on by default** — every agent is autoscaled unless you
opt out. With no `scale` block an agent still autoscales using the defaults
(`min` 1, `max` unbounded). Set a `scale` block to bound or tune it, or set
`disabled: true` to turn it off and pin the replica count.

Unlike Numaflow's `0 - N` autoscaling, Kynomesh does **not** scale to zero:
`min` is floored at 1, so an agent always keeps at least one replica ready to
serve.

### How it works

Each agent's broker reports how many requests it is handling. The controller
samples this per replica, learns the agent's capacity, and adjusts replicas to
keep the fleet near a target utilization.

- **Signal — in-flight concurrency.** For agentic (LLM/tool) workloads the
  binding resource is _slot occupancy_: each request holds a context and an
  upstream connection for the duration of a (often multi-second) call. Kynomesh
  scales on concurrent in-flight requests, not requests-per-second — a better
  fit for long, variable-duration agent calls. Latency is deliberately ignored
  (it is dominated by upstream response time and can't tell a saturated replica
  from a busy-but-healthy one).
- **Learned capacity (the "knee").** From observed load the controller learns
  the per-replica concurrency at which throughput stops rising — the saturation
  knee. It targets a fraction of that knee (see `targetSaturationPercentage`).
  Until enough data is collected, it falls back to a conservative default and
  over-provisions rather than under-provisions.
- **Decision.** Desired replicas trend toward
  `ceil(totalInflight / (knee × targetSaturationPercentage/100))`, clamped to
  `[min, max]`.
- **The controller patches `spec.replicas`** on the AgentDeploy; the normal
  reconcile then rolls pods to match.

Scaling is bounded the same way in both directions: a change happens only after
the relevant cooldown (`scaleUpCooldownSeconds` / `scaleDownCooldownSeconds`)
has elapsed, and moves by at most `replicasPerScaleUp` / `replicasPerScaleDown`
replicas per step. This keeps scaling stable against short-lived fluctuations —
a brief spike or dip that recovers within the cooldown window doesn't move the
replica count — while a sustained trend still reaches the right size across
successive intervals. When load stays at zero, the deployment steps down toward
`min` over time.

### Configuration

`scale` is set per agent, on each entry under the AgentSet's `agents` list:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-assistant
spec:
  pattern: Supervisor
  entry: coordinator
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
      scale:
        min: 1
        max: 10
        targetSaturationPercentage: 80
    - name: searcher
      container:
        image: example/searcher:latest
      # No scale block: searcher still autoscales, with the defaults
      # (min 1, max unbounded).
```

| Field                        | Type        | Default   | Description                                                                                                                                                                                                       |
| ---------------------------- | ----------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `disabled`                   | bool        | `false`   | Turn Kynomesh autoscaling off — set when using HPA or another external autoscaler.                                                                                                                                |
| `min`                        | int         | `1`       | Minimum replicas. The agent never scales below this (floored at 1).                                                                                                                                               |
| `max`                        | int         | unbounded | Maximum replicas.                                                                                                                                                                                                 |
| `targetSaturationPercentage` | int (1–100) | `80`      | Fraction of a replica's learned capacity to run at in steady state. Lower scales out earlier (safer latency, higher cost); higher packs tighter (lower cost, higher latency risk). Values above 100 are rejected. |
| `scaleUpCooldownSeconds`     | int         | `90`      | Minimum seconds between successive scale-up steps.                                                                                                                                                                |
| `scaleDownCooldownSeconds`   | int         | `90`      | Minimum seconds between successive scale-down steps.                                                                                                                                                              |
| `replicasPerScaleUp`         | int         | `2`       | Max replicas added in a single scale-up step.                                                                                                                                                                     |
| `replicasPerScaleDown`       | int         | `2`       | Max replicas removed in a single scale-down step.                                                                                                                                                                 |

## Kubernetes HPA

To drive an agent's replicas with the Kubernetes Horizontal Pod Autoscaler,
disable Kynomesh autoscaling so the two don't fight over `spec.replicas`:

```yaml
scale:
  disabled: true
```

Then point an HPA at the agent's AgentDeploy via its `scale` subresource. The
AgentDeploy is named `<agentset>-<agent>` (e.g.
`research-assistant-coordinator`):

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: coordinator
spec:
  scaleTargetRef:
    apiVersion: kynomesh.kyno.sh/v1alpha1
    kind: AgentDeploy
    name: research-assistant-coordinator
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
```

## Third-party autoscalers

Other autoscalers such as [KEDA](https://keda.sh/) can scale an agent the same
way: disable Kynomesh autoscaling (`scale.disabled: true`) and target the
AgentDeploy's `scale` subresource from the tool's scaling object (e.g. KEDA's
`ScaledObject` `scaleTargetRef`).

## Observing scaling

The controller records the current and desired replica counts, the learned knee,
and confidence as metrics, and emits a log line on each decision. To watch
replicas change:

```sh
kubectl get agentdeploy -w # or "ad" as a short name
```

## See Also

- [AgentDeploy](../../core-concepts/agentdeploy.md) — the unit that gets scaled.
- [AgentSet](../../core-concepts/agentset.md) — where the `scale` block lives.
- [APIs](../../APIs.md) — full CRD reference.
