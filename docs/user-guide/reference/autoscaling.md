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
(`min` 1, `max` 50). Set a `scale` block to bound or tune it, or set
`disabled: true` to turn it off and pin the replica count.

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
        disabled: false # Optional, defaults to false.
        min: 1 # Optional, minimum replicas, defaults to 1.
        max: 10 # Optional, maximum replicas, defaults to 50.
        targetSaturationPercentage: 80 # Optional, how aggressively to scale, default to 80.
        scaleUpCooldownSeconds: 90 # Optional, defaults to 90.
        scaleDownCooldownSeconds: 90 # Optional, defaults to 90.
        replicasPerScaleUp: 2 # Optional, defaults to 2.
        replicasPerScaleDown: 2 # Optional, defaults to 2.
    - name: searcher
      container:
        image: example/searcher:latest
      # No scale block: searcher still autoscales, with the defaults
      # (min 1, max 50).
```

- `disabled` - Whether to disable Kynomesh autoscaling, defaults to `false`.
- `min` - Minimum replicas, valid value could be an integer >= 1. Defaults to
  `1`.
- `max` - Maximum replicas, positive integer which should not be less than
  `min`, defaults to `50`. if `max` and `min` are the same, that will be the
  fixed replica number.
- `targetSaturationPercentage` - Aggressiveness of the autoscaling. It is the
  fraction (1-100) of a replica's learned capacity (the saturation knee) to run
  at in steady state.
- `scaleUpCooldownSeconds` - After a scaling operation, how many seconds to wait
  for the same AgentDeploy, if the follow-up operation is a scaling up, defaults
  to `90`.
- `scaleDownCooldownSeconds` - After a scaling operation, how many seconds to
  wait for the same AgentDeploy, if the follow-up operation is a scaling down,
  defaults to `90`.
- `replicasPerScaleUp` - Maximum number of replica change happens in one scale
  up operation, defaults to `2`. For example, if current replica number is 3,
  the calculated desired replica number is 8; instead of scaling up the
  AgentDeploy to 8, it only does 5.
- `replicasPerScaleDown` - Maximum number of replica change happens in one scale
  down operation, defaults to `2`. For example, if current replica number is 9,
  the calculated desired replica number is 4; instead of scaling down the
  AgentDeploy to 4, it only does 7.

## Kubernetes HPA

To drive an AgentDeloy's replicas with the
[Kubernetes Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/),
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

## See Also

- [AgentDeploy](../../core-concepts/agentdeploy.md) — the unit that gets scaled.
- [AgentSet](../../core-concepts/agentset.md) — where the `scale` block lives.
