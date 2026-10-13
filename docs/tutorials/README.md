# Tutorials

These tutorials show how to run agents built with popular frameworks on
Kynomesh. Each one walks through a runnable example, from your laptop to a
Kubernetes cluster.

Whatever the framework, the approach is the same:

1. Build your agent with the framework as usual.
2. Add a small wrapper that serves it over [A2A](https://a2a-protocol.org/) with
   the [Kynomesh SDK](../user-guide/sdks/overview.md).
3. Describe the deployment in an [`AgentSet`](../core-concepts/agentset.md), and
   let Kynomesh run the agents and connect them by name.

## Choose a Framework

| Framework                                        | Tutorials                                                                                   |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| [LangGraph](./langgraph/README.md)               | Single agent, a supervisor with fixed workers, and a supervisor that discovers its workers. |
| [Claude Agent SDK](./claude-agent-sdk/README.md) | Single agent, and a supervisor that discovers its workers.                                  |

More frameworks are planned.

## What You Will Learn

The tutorials follow the same path, so you can pick up what you need from any of
them:

- **Single agent.** Serve one agent over A2A and deploy it as an `AgentSet`.
  Even one agent gets autoscaling, zero-downtime rollouts and rate limiting.
- **Supervisor with workers.** Split work across several agents, each in its own
  pod, and call a worker by name instead of by URL.
- **Dynamic peers.** Let a supervisor discover its workers from their
  `AgentCards`, so you can add or remove a worker without changing code, and
  keep the supervisor in sync with
  [Drift Reload](../user-guide/reference/drift-reload.md).

## Beyond One Framework

Agents in an `AgentSet` only see each other's `AgentCard`s and A2A endpoints, so
they do not have to share a framework, or even a language. The examples
repository includes some other cross-cutting examples:

- [Mixed fleet](./mixed-fleet.md)
  ([example](https://github.com/kynoproj/kynomesh-examples/tree/main/mixed-fleet)):
  a Claude supervisor that delegates to both LangGraph and Claude workers in one
  `AgentSet`.
- [External agent](./external-agent.md)
  ([example](https://github.com/kynoproj/kynomesh-examples/tree/main/external-agent)):
  a supervisor that calls a worker living in another `AgentSet`, referenced with
  [`externalAgents`](../user-guide/reference/configuration/external-agents.md).

## Before You Start

Every tutorial needs:

- Python 3.10+ and [uv](https://docs.astral.sh/uv/)
- An API key for the framework's model provider (`OPENAI_API_KEY` for LangGraph
  and `ANTHROPIC_API_KEY` for Claude Agent SDK)
- [`a2acli`](../user-guide/reference/a2acli.md) to send messages from the
  command line
- Docker, `kubectl`, and a local Kubernetes cluster with Kynomesh installed.
  Follow the [Quick Start](../quick-start.md) if you have not set one up.

The examples live in the
[kynoproj/kynomesh-examples](https://github.com/kynoproj/kynomesh-examples)
repository. Each folder is self-contained: copy it and it runs.
