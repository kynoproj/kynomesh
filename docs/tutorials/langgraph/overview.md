# Running LangGraph Agents on Kynomesh

These tutorials walk you through running
[LangGraph](https://langchain-ai.github.io/langgraph/) agents on Kynomesh, from
a single chat agent to a team of agents that find each other on their own.

Your LangGraph code stays plain LangGraph. You add a small wrapper that serves
it over [A2A](https://a2a-protocol.org/), then describe the deployment in an
[`AgentSet`](../../core-concepts/agentset.md). Kynomesh takes care of running
the agents and letting them call each other by name.

Multi-agent is not a requirement. A single agent already benefits from
autoscaling, zero-downtime rollouts and rate limiting, and it can grow into a
team later without code changes.

## The Tutorials

Each tutorial builds on the previous one and has a matching, runnable example in
the
[kynoproj/examples](https://github.com/kynoproj/examples/tree/main/langgraph)
repository.

| Tutorial                          | What you build                                                              | What you learn                                                                                               |
| --------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| [Single agent](./single-agent.md) | A conversational agent backed by OpenAI.                                    | Serving a LangGraph graph over A2A and deploying it as an `AgentSet`.                                        |
| [Supervisor](./supervisor.md)     | A supervisor that delegates to a researcher and a calculator, one pod each. | Splitting work across agents and calling a peer by name.                                                     |
| [Dynamic peers](./dynamic-peers.md) | A travel-planning supervisor that finds its workers at startup.             | Adding or removing a worker without changing code, and keeping the supervisor in sync when a worker changes. |

## Prerequisites

- Python 3.10+ and [uv](https://docs.astral.sh/uv/)
- An `OPENAI_API_KEY`
- [`a2acli`](../../user-guide/reference/a2acli.md) to send messages from the
  command line
- Docker, `kubectl`, and a local Kubernetes cluster with Kynomesh installed.
  Follow the [Quick Start](../../quick-start.md) if you have not set one up.

## Get the Examples

```shell
git clone https://github.com/kynoproj/examples.git
cd examples/langgraph
```

Each example is self-contained and works the same way:

| Path / target | Purpose                                         |
| ------------- | ----------------------------------------------- |
| `src/`        | Your LangGraph agent code and its A2A wrapper.  |
| `manifests/`  | The `AgentSet` to deploy.                       |
| `tests/`      | Unit tests. The LLM and agent calls are mocked. |
| `make build`  | Build the container image.                      |
| `make deploy` | Deploy the `AgentSet`.                          |
| `make delete` | Remove it.                                      |

## Before You Deploy

- Create the API key secret the manifests read:

  ```shell
  kubectl create secret generic openai-api-key --from-literal=OPENAI_API_KEY=...
  ```

- Load the image into your cluster after `make build`. With kind, run
  `kind load docker-image <image>`; with k3d, run
  `k3d image import <image> -c <cluster>`.

> [!TIP] Behind a TLS-intercepting proxy the image build fails certificate
> checks. Pass the proxy CA with `make build CA_CERT=/path/to/ca.pem`.

## Troubleshooting

- **`no compatible transports found` from `a2acli`:** set `protocol_version` on
  each interface in your agent's `AgentCard`. The examples use
  `PROTOCOL_VERSION_CURRENT` from `a2a.utils.constants`.
- **Pods stay in `ImagePullBackOff` or `CreateContainerConfigError`:** the image
  was not loaded into the cluster, or the `openai-api-key` secret is missing.

## Next Steps

Start with the [Single agent](./single-agent.md) tutorial.
