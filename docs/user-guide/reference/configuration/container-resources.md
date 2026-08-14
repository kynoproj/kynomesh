# Container Resources

[Container Resources](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
can be customized for all the containers in an agent pod.

For configuring container resources on pods not owned by an agent (e.g. the
per-AgentSet daemon), see [AgentSet Customization](agentset-customization.md).

## Broker Container

To specify `resources` for the `broker` container of every agent's pod, set it
on the shared template:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  templates:
    agent:
      brokerContainer:
        resources:
          limits:
            cpu: "1"
            memory: 512Mi
          requests:
            cpu: "500m"
            memory: 256Mi
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
```

To override it for a single agent, set `brokerContainer` directly under that
agent:

```yaml
agents:
  - name: my-agent
    container:
      image: my-agent:latest
    brokerContainer:
      resources:
        limits:
          cpu: "2"
          memory: 1Gi
```

## Agent Container

To specify `resources` for the agent's own container, set it on
`spec.agents[*].container`:

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
        resources:
          limits:
            cpu: "2"
            memory: 2Gi
          requests:
            cpu: "1"
            memory: 1Gi
```

## Sidecar And Init Containers

Container resources for [sidecar containers](sidecar-containers.md) and
[init containers](init-containers.md) are specified the same way, directly under
`.spec.agents[*].sidecars[*].resources` and
`.spec.agents[*].initContainers[*].resources` respectively — these are plain
Kubernetes container specs.

## Daemon Container

To specify `resources` for the per-AgentSet daemon's container, set it on
`.spec.templates.daemon.container`:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  templates:
    daemon:
      container:
        resources:
          limits:
            cpu: "500m"
            memory: 256Mi
          requests:
            cpu: "100m"
            memory: 128Mi
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
```

## See Also

- [AgentSet Customization](agentset-customization.md) — the `brokerContainer`
  and daemon `container` templates, and how per-agent settings override shared
  ones.
- [Environment Variables](environment-variables.md) — env vars injected into
  every container.
