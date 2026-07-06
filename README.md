# Kynomesh

[![Release Version](https://img.shields.io/github/v/release/kynoproj/kynomesh?label=kynomesh&color=dca282)](https://github.com/kynoproj/kynomesh/releases/latest)
[![slack](https://img.shields.io/badge/slack-kynoproj-brightgreen.svg?logo=slack)](https://join.slack.com/t/kynoproj/shared_invite/zt-3zfjq4ok5-d7z2ZyeaD0574LCLXI9mnA)
[![GoDoc](https://godoc.org/github.com/kynoproj/kynomesh?status.svg)](https://godoc.org/github.com/kynoproj/kynomesh/pkg/apis)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/13037/badge)](https://bestpractices.coreinfrastructure.org/projects/13037)

Welcome to Kynomesh! A Kubernetes-native platform for orchestrating distributed
multi-agent systems. Kynomesh lets you declare a group of cooperating agents as
a single Kubernetes resource and takes care of placement, peer discovery, and
agent-to-agent ([A2A](https://a2a-protocol.org/)) traffic — so you can focus on
the agent logic instead of the wiring.

Each agent runs in its own dedicated set of pods, accompanied by an injected
broker sidecar that manages A2A communication and transport. Kynomesh provides
built-in autoscaling capabilities, automatically scaling agent workloads based
on traffic patterns and system load. Agents are language-agnostic and can be
implemented using any programming language.

## Use Cases

- Multi-agent LLM applications: a supervisor coordinating worker agents, or a
  swarm of specialized agents that hand off tasks to one another.
- Sequential agent pipelines: each stage produces input for the next.
- Heterogeneous agent fleets: combine agents written in different languages
  behind a single Kubernetes API.
- Hybrid topologies: mix in-cluster managed agents with external agents
  reachable at a URL.

## Key Features

- **Kubernetes-native:** AgentSet is the primary custom resource in Kynomesh. If
  you're familiar with kubectl, you'll feel right at home managing and operating
  agent workloads.
- **Pattern-based peer discovery:** Define a communication pattern (such as
  Supervisor, Handoff, or Sequential) along with an entry agent, and Kynomesh
  automatically derives and maintains peer relationships. No custom
  service-discovery logic is required in your agents.
- **Language agnostic:** Build agents in any programming language, including Go,
  Python, Node.js, Rust, and more. Managed and external agents: An AgentSet can
  seamlessly combine agents running inside the cluster with external HTTP-based
  agents, providing a unified interface to callers.
- **Built-in autoscaling:** Automatically scales agent workloads in response to
  traffic demand and system load, ensuring efficient resource utilization and
  high availability.
- **Zero-downtime rolling updates:** Kynomesh orchestrates rolling deployments
  and graceful pod termination, enabling seamless upgrades with minimal
  disruption to ongoing traffic.

## Resources

- [APIs](docs/APIs.md)
- [Development](docs/development/development.md)
- [Static code analysis](docs/development/static-code-analysis.md)
- [Contributing](CONTRIBUTING.md)

## License

Apache License Version 2.0, see [LICENSE](LICENSE).
