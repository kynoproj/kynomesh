# Rolling Update

When an agent's pod spec changes, `RollingUpdate` is the update strategy used to
roll the agent's pods, and it's the only strategy supported today. The default
configuration is:

```yaml
updateStrategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 25%
```

- `maxUnavailable`: the maximum number of existing pods that can be replaced at
  once during a rolling update. Value can be an absolute number (e.g. `5`) or a
  percentage of the agent's replica count (e.g. `10%`). A percentage is rounded
  up to an absolute number, with a floor of `1` so a rollout always makes
  forward progress. Defaults to `25%`.

## How It Works

`maxUnavailable` only gates the **replacement** of pods that are already running
an outdated pod spec — it does not gate initial bring-up (a fresh AgentDeploy,
or new replica slots created by scaling up) or scale-down (removing replica
slots), both of which happen immediately and in full.

For replacement, the controller batches replica slots that are still on the old
spec, up to `maxUnavailable` slots per pass, and replaces each one
delete-then-create (unlike a Kubernetes `Deployment`, which creates the new pod
before removing the old one). The controller waits for every pod in a batch to
become Ready before starting the next batch.

For example, if an agent has 20 replicas and `maxUnavailable` is the default
`25%` (5 pods), a spec change replaces 5 pods at a time: those 5 are deleted and
recreated with the new spec, and the controller waits for all 5 to become Ready
before touching the next 5. If your agent has a long startup time and you're
sensitive to the reduced capacity during a rollout, set `maxUnavailable` to a
smaller value.

Autoscaling is paused for an agent while a rolling update is in progress — the
autoscaler skips scaling decisions until every replica slot is back on the
desired pod spec — so a rollout and an autoscaling decision never race each
other.

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
      updateStrategy:
        type: RollingUpdate
        rollingUpdate:
          maxUnavailable: 2
```

## See Also

- [Zero-Downtime Pod Replacement](../zero-downtime-pod-replacement.md) — how
  `maxUnavailable` combines with readiness and graceful termination so a
  rollout doesn't drop traffic.
- [AgentSet Customization](agentset-customization.md) — other per-agent
  settings.
