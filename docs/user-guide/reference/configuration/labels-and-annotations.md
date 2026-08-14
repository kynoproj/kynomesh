# Labels And Annotations

Sometimes customized _Labels_ or _Annotations_ are needed for an agent's pods —
for example, adding an annotation to enable or disable
[Istio](https://istio.io/) sidecar injection. To do that, add a `metadata` block
with `labels` or `annotations` to the agent.

Set them on a single agent, under its entry in the AgentSet's `agents` list:

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
      metadata:
        labels:
          key1: val1
          key2: val2
        annotations:
          key3: val3
          key4: val4
```

To apply the same `metadata` to every agent in the AgentSet, set it once on the
shared agent template under `spec.templates.agent`:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-assistant
spec:
  pattern: Supervisor
  entry: coordinator
  templates:
    agent:
      metadata:
        labels:
          key1: val1
        annotations:
          key3: val3
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
```

A per-agent `metadata` value takes precedence over the shared template for the
same key.

The labels and annotations are **added** to the agent's pods — Kynomesh's own
labels and annotations (used to manage and route the pods) are never overridden.

## See Also

- [AgentSet](../../../core-concepts/agentset.md) — where the `agents` list and
  `templates` live.
- [AgentDeploy](../../../core-concepts/agentdeploy.md) — the unit these pods
  belong to.
