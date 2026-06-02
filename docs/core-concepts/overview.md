# Core Concepts

Kynomesh is a Kubernetes-native platform for orchestrating distributed
multi-agent systems. It provides a small set of custom resources that let you
describe a group of cooperating agents declaratively; the controller handles
placement, peer discovery, and the agent-to-agent
([A2A](https://a2a-protocol.org/)) plumbing.

The following sections introduce the core concepts of Kynomesh.

- [Core Concepts](#core-concepts)
  - [AgentSet](#agentset)
  - [AgentDeploy](#agentdeploy)

## AgentSet

[AgentSet](./agentset.md) is the top-level resource. It declares a group of
cooperating agents, the routing pattern that wires them together (`Supervisor`,
`Handoff`, or `Sequential`), and which agent external callers reach first. The
AgentSet controller materializes each declared agent into its own `AgentDeploy`
and stamps the per-agent peer view onto each child.

## AgentDeploy

[AgentDeploy](./agentdeploy.md) is the unit of deployment for a single agent. It
owns the pods that run the user's agent code together with an injected broker
sidecar that handles A2A transport, plus the headless Service that gives each
replica a stable DNS name.
