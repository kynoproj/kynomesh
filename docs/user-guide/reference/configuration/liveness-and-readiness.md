# Liveness and Readiness

`Liveness` and `Readiness` probes are pre-configured on every container in an
agent pod. For the **agent** container, the probe handler itself is not
customizable — it always runs Kynomesh's bundled probe binary against the broker
over the shared Unix Domain Socket — but the timing can be, via
`.spec.agents[*].container`:

- `initialDelaySeconds`
- `timeoutSeconds`
- `periodSeconds`
- `successThreshold`
- `failureThreshold`

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
          periodSeconds: 60
        livenessProbe:
          initialDelaySeconds: 60
          periodSeconds: 120
          failureThreshold: 5
```

## See Also

- [Init Containers](init-containers.md) — the `agent` container's start order
  relative to init containers.
- [Sidecar Containers](sidecar-containers.md) — the `agent` container's start
  order relative to sidecars.
- [Container Resources](container-resources.md) — set `resources` on any
  container.
