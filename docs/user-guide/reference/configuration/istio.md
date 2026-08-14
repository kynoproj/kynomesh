# Running On Istio

If you want an agent's pods to run with an Istio sidecar injected, so they can
talk to other services with Istio enabled, whitelist the ports Kynomesh uses so
the Istio sidecar doesn't intercept in-mesh traffic meant for the broker.

Add `traffic.sidecar.istio.io/excludeInboundPorts` and
`traffic.sidecar.istio.io/excludeOutboundPorts` annotations via
`.spec.agents[*].metadata.annotations` (see
[Labels And Annotations](labels-and-annotations.md)):

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
      metadata:
        annotations:
          sidecar.istio.io/inject: "true"
          traffic.sidecar.istio.io/excludeInboundPorts: "8490,8491" # broker + introspection
          traffic.sidecar.istio.io/excludeOutboundPorts: "8490" # broker-to-broker (peer) traffic
      container:
        image: my-agent:latest
```

- `8490` is `AgentBrokerPort` — the broker's `A2A` listener, used for both
  inbound calls into this agent and outbound calls to peer agents' brokers.
- `8491` is `AgentBrokerIntrospectionPort` — `/metrics`, `/healthz`, and
  `/readyz`, scraped in-cluster (e.g. by the per-AgentSet daemon and kubelet
  probes).

If you want the same annotations applied to every agent in the AgentSet, set
them once on the shared template instead, via
`.spec.templates.agent.metadata.annotations`:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: coordinator
  templates:
    agent:
      metadata:
        annotations:
          sidecar.istio.io/inject: "true"
          traffic.sidecar.istio.io/excludeInboundPorts: "8490,8491"
          traffic.sidecar.istio.io/excludeOutboundPorts: "8490"
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
    - name: worker
      container:
        image: example/worker:latest
```

## See Also

- [Labels And Annotations](labels-and-annotations.md) — setting
  `metadata.annotations` on agent and daemon pods.
- [AgentSet Customization](agentset-customization.md) — the `agent` and `daemon`
  shared templates.
