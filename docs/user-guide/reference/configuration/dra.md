# Dynamic Resource Allocation

[Dynamic Resource Allocation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
(DRA) is supported on an agent's pod: declare a claim at the pod level via
`.spec.agents[*].resourceClaims`, then reference it by name from
`resources.claims` on any container that needs it — the agent container, the
`brokerContainer` template, or the per-AgentSet `.spec.templates.daemon.container`.

```yaml
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceClaimTemplate
metadata:
  name: my-gpu
spec:
  spec:
    devices:
      requests:
        - name: gpu
          deviceClassName: gpu.nvidia.com
---
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  agents:
    - name: my-agent
      resourceClaims:
        - name: gpu
          resourceClaimTemplateName: my-gpu
      container:
        image: my-agent:latest
        resources:
          claims:
            - name: gpu
```

To give the `broker` container access to the same claim, reference it from
`brokerContainer.resources.claims` (either per-agent or on the shared
`.spec.templates.agent.brokerContainer`):

```yaml
      brokerContainer:
        resources:
          claims:
            - name: gpu
```

## See Also

- [Container Resources](container-resources.md) — set `resources` on any
  container.
- [AgentSet Customization](agentset-customization.md) — the `agent` and
  `daemon` shared templates.
