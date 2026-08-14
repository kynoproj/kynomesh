# Volumes

`Volumes` can be mounted to the agent container, and to any
[sidecar](sidecar-containers.md) or [init container](init-containers.md), of an
agent's pod. Declare the volume once at `.spec.agents[*].volumes`, then mount it
with `volumeMounts` on whichever containers need it.

The following example shows how to mount a ConfigMap to the agent container:

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
      volumes:
        - name: my-agent-config
          configMap:
            name: agent-config
      container:
        image: my-agent:latest
        volumeMounts:
          - mountPath: /path/to/my-agent-config
            name: my-agent-config
```

## PVC Example

Example showing how to attach an existing Persistent Volume Claim (PVC) to the
agent container:

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
      volumes:
        - name: mypd
          persistentVolumeClaim:
            claimName: myclaim
      container:
        image: my-agent:latest
        volumeMounts:
          - mountPath: /path/to/my-data
            name: mypd
```

## See Also

- [Sidecar Containers](sidecar-containers.md) — a shared-volume example between
  the agent and a sidecar.
- [Init Containers](init-containers.md) — a shared-volume example between an
  init container and the agent.
