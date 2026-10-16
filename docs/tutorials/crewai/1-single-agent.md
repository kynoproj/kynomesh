# Tutorial: A Single CrewAI Agent

In this tutorial you run a conversational agent built with
[CrewAI](https://docs.crewai.com/), first on your machine and then on Kynomesh
as a one-agent [`AgentSet`](../../core-concepts/agentset.md). The agent has one
custom tool that converts temperatures. You will also see what to change to
serve your own agent.

The full example is in
[kynoproj/kynomesh-examples](https://github.com/kynoproj/kynomesh-examples/tree/main/crewai/single-agent).
Follow the [overview](./README.md#prerequisites) first if you have not set up
the prerequisites. Running a single agent on Kynomesh already gives you
autoscaling, zero-downtime rollouts and rate limiting. See the
[LangGraph single-agent tutorial](../langgraph/1-single-agent.md#why-run-a-single-agent-on-kynomesh)
for the full list.

```shell
git clone https://github.com/kynoproj/kynomesh-examples.git
cd kynomesh-examples/crewai/single-agent
```

## What's in the Example

| File                        | Purpose                                                                       |
| --------------------------- | ----------------------------------------------------------------------------- |
| `src/crew_chat/agent.py`    | `build_agent` and `run_agent`: one CrewAI run per request.                    |
| `src/crew_chat/tools.py`    | A custom tool the agent can call.                                             |
| `src/crew_chat/executor.py` | Turns an A2A message into a call to `run_agent`, and the result into a reply. |
| `src/crew_chat/main.py`     | The `AgentCard`, and serving the agent over A2A.                              |
| `manifests/agentset.yaml`   | The `AgentSet` that deploys it.                                               |

`agent.py` and `tools.py` are plain CrewAI code with nothing Kynomesh-specific.
Only `main.py` uses the Kynomesh SDK.

## A Custom Tool

The agent's one tool converts a temperature between Celsius, Fahrenheit and
Kelvin. It is a CrewAI tool, declared with the `@tool` decorator:

```python
@tool("convert_temperature")
def convert_temperature(value: float, from_unit: str, to_unit: str) -> str:
    """Convert a temperature between C (Celsius), F (Fahrenheit) and K (Kelvin)."""
    try:
        return f"{convert(value, from_unit, to_unit):.2f} {to_unit.strip().upper()}"
    except ValueError as e:
        return f"Error: {e}"
```

CrewAI builds the tool's schema from the type hints, and shows the model the
docstring. A bad unit comes back as an error string, so the model can recover
instead of failing the whole request.

## The Agent

The agent is a standalone CrewAI `Agent`. It answers each message with
`kickoff_async`, so there is no `Task` or `Crew` to wire up:

```python
def build_agent(llm: LLM | None = None) -> Agent:
    return Agent(
        role="Conversational assistant",
        goal="Answer the user's message helpfully and briefly",
        backstory=(
            "You are a helpful conversational agent. Keep responses brief and engaging. "
            "Use the convert_temperature tool for any temperature conversion."
        ),
        llm=llm or LLM(model=os.getenv("OPENAI_MODEL") or DEFAULT_MODEL, temperature=0),
        tools=[convert_temperature],
        max_iter=MAX_ITER,  # bounds the tool-use loop
        verbose=False,
    )


async def run_agent(prompt: str, *, agent: Agent) -> str:
    set_suppress_console_output(True)  # CrewAI draws panels to stdout; keep pod logs clean
    output = await agent.kickoff_async(prompt)
    if not output.raw:
        raise AgentError("CrewAI finished without a final answer")
    return output.raw
```

A few choices to note:

- **The model.** The agent uses OpenAI. Set `OPENAI_MODEL` to choose a model.
  The default is `gpt-4o-mini`. Other providers work through CrewAI's `LLM`.
- **`max_iter`** bounds the tool-use loop, so one request cannot run forever.
- **No history.** Each request starts from a clean conversation.
- **Quiet logs.** CrewAI draws progress panels to stdout by default, which would
  fill the pod's logs, so `run_agent` turns them off.

## Serving It Over A2A

The executor connects A2A to `run_agent`: it reads the text of the incoming
message and replies with the agent's answer.

```python
class CrewExecutor(AgentExecutor):
    def __init__(self, run: Run) -> None:
        self._run = run

    async def execute(self, context: RequestContext, event_queue: EventQueue) -> None:
        reply = await self._run(message_text(context.message))
        await event_queue.enqueue_event(
            Message(
                message_id=str(uuid.uuid4()),
                role=Role.ROLE_AGENT,
                parts=[Part(text=reply)],
            )
        )
```

`main.py` describes the agent with an `AgentCard`, which tells callers who the
agent is and what it can do, and hands both to the Kynomesh SDK:

```python
AgentCard(
    name="CrewAI Chat Agent",
    description="A conversational CrewAI agent served through Kynomesh",
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
    skills=[AgentSkill(id="chat", name="Chat", description="Brief, friendly replies; converts temperatures with a custom tool", tags=["chat"])],
    # ...
)
```

```python
agent = build_agent()
await server.start(CrewExecutor(run), agent_card())
```

> [!NOTE] The URLs are placeholders for local development. On Kynomesh they are
> replaced with the real addresses, so you never hard-code them.

CrewAI also has its own A2A support, but this example does not use it.
Kynomesh's SDK runs the A2A server, and CrewAI only answers the message.

## Run It Locally

You need Python 3.10 to 3.13, because CrewAI does not support 3.14 yet. Run the
unit tests, which make no API calls, then start the agent:

```shell
uv run pytest
OPENAI_API_KEY=... uv run python -m crew_chat.main
```

The agent listens for HTTP on port 8088 and gRPC on port 8089. In another
terminal, check it is healthy and send it a message:

```shell
curl localhost:8088/healthz
a2acli -u http://localhost:8088 send 'What is 72 F in Celsius?' -o json
```

## Deploy to Kynomesh

The `AgentSet` declares one agent running the image you are about to build:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: crew-chat
spec:
  pattern: Supervisor
  entry: chat-agent
  agents:
    - name: chat-agent
      container:
        image: crew-chat:latest
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

CrewAI keeps state under `$HOME` and sends telemetry by default. The image runs
as an unprivileged user with no home directory, so the Dockerfile gives it a
writable one and turns telemetry and tracing off:

```dockerfile
ENV HOME=/tmp/home \
    CREWAI_STORAGE_DIR=/tmp/home/crewai \
    CREWAI_TRACING_ENABLED=false \
    CREWAI_DISABLE_VERSION_CHECK=true \
    OTEL_SDK_DISABLED=true
RUN mkdir -p /tmp/home && chmod 1777 /tmp/home
USER nobody
```

Build the image, load it into your cluster, create the secret, and deploy:

```shell
make build
# kind:  kind load docker-image crew-chat:latest
# k3d:   k3d image import crew-chat:latest -c <cluster>
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
kubectl port-forward svc/crew-chat-ingress 8490
```

```shell
a2acli -k -u https://localhost:8490 --override-host=localhost:8490 \
  send 'What is 72 F in Celsius?' -o json
```

The reply comes from the model, which called the `convert_temperature` tool to
get 22.22 C. The `-k` flag skips verification of the local TLS certificate, and
`--override-host` makes the agent's advertised address match the forwarded port.

## Clean Up

```shell
make delete
```

## Use Your Own Agent

To serve a different CrewAI agent:

1. Replace the tool in `tools.py`, or add more, and list each one in the agent's
   `tools`. Keep the docstrings clear, since the model reads them.
2. Update the agent's `role`, `goal` and `backstory` to describe the job and
   when to use each tool.
3. Update the `AgentCard` name, description and skills to describe your agent.
4. Change `image` and the `env` entries in the `AgentSet` to match your build
   and the secrets it needs.

If your agent is built differently, for example as a full `Crew` with tasks, you
only need to change what `run_agent` runs. The executor and `main.py` stay the
same.

## Troubleshooting

- **`OPENAI_API_KEY must be set`:** the agent refuses to start without a key.
  Export it locally, or create the `openai-api-key` secret before deploying.
- **The install fails on Python 3.14:** use Python 3.10 to 3.13.
- **`no compatible transports found`:** every `AgentInterface` in the card needs
  `protocol_version` set.
- **`AgentSet` never reaches `Running`:** check `kubectl get pods`. The image
  may not be loaded into your cluster, or the secret may be missing.
- **The pod crashes on a permissions error writing under `$HOME`:** the image
  needs a writable home. Keep the `HOME` settings from the Dockerfile.

## Next Steps

One agent is a good start. In the next tutorial,
[Supervisor](./2-supervisor.md), you split work across several agents, and see
how a CrewAI tool calls another agent.
