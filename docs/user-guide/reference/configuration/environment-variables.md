# Environment Variables

Kynomesh injects a common set of preset environment variables into **every**
container of an agent pod — the broker, the agent container, and any
user-defined `sidecars` or `initContainers` — so any container can identify
which pod, agent, and AgentSet it's running in:

- `NAMESPACE` - Namespace the agent runs in.
- `POD_NAME` - Pod name.
- `KYNOMESH_AGENTSET_NAME` - Name of the AgentSet.
- `KYNOMESH_AGENTDEPLOY_NAME` - The agent short name.

These are built-in and always win: if you set an env var with the same name in
your own container spec, the injected value overrides it.

For setting environment variables on pods not owned by an agent, see
[AgentSet Customization](agentset-customization.md).

## Your Own Environment Variables

To add your own environment variables to the agent container, set `env` on the
agent's `container`. The same applies to `sidecars` and `initContainers` — set
`env`/`envFrom` directly on those container entries.

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

## See Also

- [AgentSet Customization](agentset-customization.md) — customize pods not owned
  by an agent.
- [AgentSet](../../../core-concepts/agentset.md) — where the `agents` list
  lives.
