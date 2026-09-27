# Liveness, Readiness, and Startup

`Liveness`, `Readiness`, and `Startup` probes are pre-configured on the
**agent** container. The probe handler itself is not customizable - it always
runs Kynomesh's bundled probe binary against the broker over the shared Unix
Domain Socket - but the timing can be, via `.spec.agents[*].container`:

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
        startupProbe:
          periodSeconds: 5
          failureThreshold: 60
        readinessProbe:
          initialDelaySeconds: 30
          periodSeconds: 60
        livenessProbe:
          initialDelaySeconds: 60
          periodSeconds: 120
          failureThreshold: 5
```

`startupProbe` gates `readinessProbe` and `livenessProbe`: neither runs until
the startup probe succeeds once. Use it instead of a large
`livenessProbe.initialDelaySeconds` when startup time is unpredictable (e.g.
loading a large model) - it gives the agent as long as
`periodSeconds * failureThreshold` to come up, without weakening how quickly
liveness detects a hang once the agent is actually running. If your agent starts
quickly and predictably, the defaults are enough and you likely don't need to
customize `startupProbe` at all.

By default, the SDK reports the agent as healthy as soon as the process starts,
whether or not it has actually finished initializing (loading a model,
connecting to a dependency, etc.). If that's not accurate for your agent,
customize the health check in your SDK - see each SDK's README.

## See Also

- [Init Containers](init-containers.md) - the `agent` container's start order
  relative to init containers.
- [Sidecar Containers](sidecar-containers.md) - the `agent` container's start
  order relative to sidecars.
- [Container Resources](container-resources.md) - set `resources` on any
  container.
- [Zero-Downtime Pod Replacement](../zero-downtime-pod-replacement.md) - how
  readiness timing affects when a rollout starts routing traffic to a new pod.
