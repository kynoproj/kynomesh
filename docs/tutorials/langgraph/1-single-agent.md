# Tutorial: A Single LangGraph Agent

In this tutorial you run a conversational
[LangGraph](https://langchain-ai.github.io/langgraph/) agent backed by OpenAI,
first on your machine and then on Kynomesh as a one-agent
[`AgentSet`](../../core-concepts/agentset.md). You will also see what to change
to serve your own graph.

The full example is in
[kynoproj/kynomesh-examples](https://github.com/kynoproj/kynomesh-examples/tree/main/langgraph/single-agent).
Follow the [overview](./README.md#prerequisites) first if you have not set up
the prerequisites.

```shell
git clone https://github.com/kynoproj/kynomesh-examples.git
cd kynomesh-examples/langgraph/single-agent
```

## Why Run a Single Agent on Kynomesh?

Kynomesh is built for multi-agent systems, but you don't need several agents to
benefit from it. A one-agent `AgentSet` still gets:

- **A standard A2A endpoint.** Any A2A client can call your agent through a
  stable ingress service, without you writing server or transport code.
- **Autoscaling.** The agent scales with the load it receives, and
  [scales back down](../../user-guide/reference/autoscaling.md) when it is idle.
- **Safe rollouts.** Shipping a new image replaces pods
  [without dropping requests](../../user-guide/reference/zero-downtime-pod-replacement.md),
  and in-flight LLM calls get time to finish.
- **Load protection.** Cap concurrent requests with
  [rate limiting](../../user-guide/reference/rate-limiting.md) to stay within
  your LLM provider's quota.
- **Health checks and metrics.** Probes are preconfigured, and
  [Prometheus metrics](../../operations/metrics.md) are exposed for every agent.
- **Kubernetes-native operations.** Deploy, inspect and delete the agent with
  `kubectl`, and request resources such as a
  [GPU](../../user-guide/reference/gpu.md) in the manifest.

It also leaves room to grow. When one agent is no longer enough, you add more to
the same `AgentSet` and your existing agent code does not change. The next
tutorials show how.

## What's in the Example

| File                      | Purpose                                                          |
| ------------------------- | ---------------------------------------------------------------- |
| `src/chat_agent/graph.py` | The LangGraph agent. Plain LangGraph, nothing Kynomesh-specific. |
| `src/chat_agent/main.py`  | Serves the graph over A2A.                                       |
| `manifests/agentset.yaml` | The `AgentSet` that deploys it.                                  |

## The Agent

The graph has one node that sends the latest user message to OpenAI and appends
the reply to the conversation:

```python
@dataclass
class State:
    messages: List[Dict[str, Any]]


async def call_model(state: State, runtime: Runtime[Context]) -> Dict[str, Any]:
    ...
    return {"messages": [*state.messages, {"role": "assistant", "content": ai_response}]}


graph = (
    StateGraph(State, context_schema=Context)
    .add_node(call_model)
    .add_edge("__start__", "call_model")
    .compile()
)
```

The only requirement Kynomesh places on a graph is a `messages` key in its
state. Incoming text arrives as `{"role": "user", "content": ...}`, and the last
message in the result is sent back as the reply. Set `OPENAI_MODEL` to use a
model other than the default.

## Serving It Over A2A

`main.py` wraps the graph in an A2A executor and starts it with the Kynomesh
SDK. The executor does the mapping described above:

```python
class LangGraphExecutor(AgentExecutor):
    def __init__(self, graph) -> None:
        self._graph = graph

    async def execute(self, context: RequestContext, event_queue: EventQueue) -> None:
        result = await self._graph.ainvoke(
            {"messages": [{"role": "user", "content": message_text(context.message)}]}
        )
        await event_queue.enqueue_event(
            Message(
                message_id=str(uuid.uuid4()),
                role=Role.ROLE_AGENT,
                parts=[Part(text=result["messages"][-1]["content"])],
            )
        )
```

The agent also publishes an `AgentCard`, which tells callers who the agent is
and what it can do:

```python
AgentCard(
    name="LangGraph Chat Agent",
    description="A conversational LangGraph agent (OpenAI) served through Kynomesh",
    version="0.1.0",
    supported_interfaces=[
        AgentInterface(
            url="http://127.0.0.1:8088/a2a/jsonrpc",
            protocol_binding=TransportProtocol.JSONRPC.value,
            protocol_version=PROTOCOL_VERSION_CURRENT,
        ),
        AgentInterface(
            url="127.0.0.1:8089",
            protocol_binding=TransportProtocol.GRPC.value,
            protocol_version=PROTOCOL_VERSION_CURRENT,
        ),
    ],
    skills=[AgentSkill(id="chat", name="Chat", description="Brief, friendly conversational replies", tags=["chat"])],
    # ...
)
```

> [!NOTE] The URLs are placeholders for local development. On Kynomesh they are
> replaced with the real addresses, so you never hard-code them.

Finally, `main()` hands both to the SDK:

```python
await server.start(LangGraphExecutor(graph), agent_card())
```

## Run It Locally

Run the unit tests, which mock OpenAI, then start the agent:

```shell
uv run pytest
OPENAI_API_KEY=... uv run python -m chat_agent.main
```

The agent listens for HTTP on port 8088 and gRPC on port 8089. In another
terminal, check it is healthy and send it a message:

```shell
curl localhost:8088/healthz
a2acli -u http://localhost:8088 send 'What is Kubernetes?' -o json
```

## Deploy to Kynomesh

The `AgentSet` declares one agent running the image you are about to build:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: langgraph-chat
spec:
  pattern: Supervisor
  entry: chat-agent
  agents:
    - name: chat-agent
      container:
        image: langgraph-chat:latest
        imagePullPolicy: IfNotPresent
        env:
          - name: OPENAI_API_KEY
            valueFrom:
              secretKeyRef:
                name: openai-api-key
                key: OPENAI_API_KEY
```

`entry` names the agent that outside callers reach. With one agent, that is
`chat-agent`. The API key comes from a Kubernetes secret rather than the
manifest.

Build the image, load it into your cluster, create the secret, and deploy:

```shell
make build
# kind:  kind load docker-image langgraph-chat:latest
# k3d:   k3d image import langgraph-chat:latest -c <cluster>
kubectl create secret generic openai-api-key --from-literal=OPENAI_API_KEY=...
make deploy
```

Wait for the `AgentSet` to report `Running`:

```shell
kubectl get agentset
```

## Talk to the Agent

Forward the agent's ingress service to your machine and send it a message:

```shell
kubectl port-forward svc/langgraph-chat-ingress 8490
```

```shell
a2acli -k -u https://localhost:8490 --override-host=localhost:8490 \
  send 'What is Kubernetes?' -o json
```

The `-k` flag skips verification of the local TLS certificate, and
`--override-host` makes the agent's advertised address match the forwarded port.

## Clean Up

```shell
make delete
```

## Use Your Own Graph

To serve a different LangGraph agent:

1. Give your graph a `messages` state key, or adapt the two lines in
   `LangGraphExecutor.execute` that build the input and read the result.
2. Update the `AgentCard` name, description and skills to describe your agent.
3. Change `image` and the `env` entries in the `AgentSet` to match your build
   and the secrets it needs.

## Troubleshooting

- **`OPENAI_API_KEY must be set`:** the agent refuses to start without a key.
  Export it locally, or create the `openai-api-key` secret before deploying.
- **`no compatible transports found`:** every `AgentInterface` in the card needs
  `protocol_version` set.
- **`AgentSet` never reaches `Running`:** check `kubectl get pods`. The image
  may not be loaded into your cluster, or the secret may be missing.

## Next Steps

One agent is a good start. In the next tutorial you split work across several
agents with a [supervisor](./2-supervisor.md).
