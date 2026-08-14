# Environment Variables

In the agent container, some preset environment variables are injected by
Kynomesh and can be used directly:

- `NAMESPACE` - Namespace the agent runs in.
- `POD_NAME` - Pod name.
- `KYNOMESH_AGENTSET_NAME` - Name of the AgentSet.
- `KYNOMESH_AGENTDEPLOY_NAME` - Name of the agent short name.

For setting environment variables on pods not owned by an agent, see
[AgentSet Customization](agentset-customization.md).

## Your Own Environment Variables

To add your own environment variables to the agent container, set `env` on the
agent's `container`:

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
        env:
          - name: env01
            value: value01
          - name: env02
            valueFrom:
              secretKeyRef:
                name: my-secret
                key: my-key
```

Similarly, `envFrom` can be specified on the agent's `container`:

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
        envFrom:
          - configMapRef:
              name: my-config
          - secretRef:
              name: my-secret
```

The preset variables above take precedence: if you set an env var with the same
name as a Kynomesh-injected one, the injected value wins.

## See Also

- [AgentSet Customization](agentset-customization.md) — customize pods not owned
  by an agent.
- [AgentSet](../../../core-concepts/agentset.md) — where the `agents` list
  lives.
