# Tutorial: A Single ADK Agent

In this tutorial you run a conversational agent built with the
[Google Agent Development Kit (ADK)](https://google.github.io/adk-docs/), first
on your machine and then on Kynomesh as a one-agent
[`AgentSet`](../../core-concepts/agentset.md). The agent has one custom tool
that converts temperatures. You will also see what to change to serve your own
agent.

The full example is in
[kynoproj/kynomesh-examples](https://github.com/kynoproj/kynomesh-examples/tree/main/google-adk/single-agent).
Follow the [overview](./README.md#prerequisites) first if you have not set up
the prerequisites. Running a single agent on Kynomesh already gives you
autoscaling, zero-downtime rollouts and rate limiting. See the
[LangGraph single-agent tutorial](../langgraph/1-single-agent.md#why-run-a-single-agent-on-kynomesh)
for the full list.

```shell
git clone https://github.com/kynoproj/kynomesh-examples.git
cd kynomesh-examples/google-adk/single-agent
```

## What's in the Example

| File                       | Purpose                                                                       |
| -------------------------- | ----------------------------------------------------------------------------- |
| `src/adk_chat/agent.py`    | `run_agent`: one fresh session per request, returning the final text.         |
| `src/adk_chat/tools.py`    | A plain Python function the agent can call as a tool.                         |
| `src/adk_chat/executor.py` | Turns an A2A message into a call to `run_agent`, and the result into a reply. |
| `src/adk_chat/main.py`     | The `AgentCard`, and serving the agent over A2A.                              |
| `manifests/agentset.yaml`  | The `AgentSet` that deploys it.                                               |

`agent.py` and `tools.py` are plain ADK code with nothing Kynomesh-specific.
Only `main.py` uses the Kynomesh SDK.

## A Custom Tool

An ADK tool is a plain Python function. ADK builds the tool's schema from the
signature and the docstring, so the docstring is what the model reads to decide
when and how to call it:

```python
def convert_temperature(value: float, from_unit: str, to_unit: str) -> dict[str, str]:
    """Convert a temperature between C (Celsius), F (Fahrenheit) and K (Kelvin).

    Args:
        value: The temperature to convert.
        from_unit: The unit of `value`: C, F or K.
        to_unit: The unit to convert to: C, F or K.

    Returns:
        {"status": "success", "result": "<value> <unit>"}, or
        {"status": "error", "error": "<message>"} for an unknown unit.
    """
    try:
        result = convert(value, from_unit, to_unit)
    except ValueError as e:
        return {"status": "error", "error": str(e)}
    return {"status": "success", "result": f"{result:.2f} {to_unit.strip().upper()}"}
```

Describe every argument in the docstring and return a dictionary. A bad unit
comes back as an error result, so the model can recover instead of failing the
whole request.

## The Agent

The agent is an `LlmAgent` that uses Gemini and the tool above:

```python
def build_agent() -> LlmAgent:
    return LlmAgent(
        name="chat_agent",
        model=os.getenv("GOOGLE_MODEL") or DEFAULT_MODEL,
        instruction=INSTRUCTION,
        tools=[convert_temperature],
    )


def build_runner() -> Runner:
    return InMemoryRunner(agent=build_agent(), app_name=APP_NAME)
```

Set `GOOGLE_MODEL` to choose a model. The default is `gemini-2.5-flash`.

`run_agent` runs the agent once for each request. It creates a fresh in-memory
session, sends the prompt, and returns the text of the final response:

```python
async def run_agent(prompt: str, *, runner: Runner | None = None) -> str:
    runner = runner or build_runner()
    session = await runner.session_service.create_session(
        app_name=APP_NAME, user_id=USER_ID, session_id=str(uuid.uuid4())
    )
    message = types.Content(role="user", parts=[types.Part(text=prompt)])
    reply = ""
    async for event in runner.run_async(user_id=USER_ID, session_id=session.id, new_message=message):
        if event.error_code or event.error_message:
            raise AgentError(f"ADK run failed: {event.error_code} {event.error_message or ''}".strip())
        if event.is_final_response() and event.content and event.content.parts:
            reply = "".join(part.text or "" for part in event.content.parts)
    if not reply:
        raise AgentError("ADK finished without a final response")
    return reply
```

> [!NOTE] Each request gets its own session, so requests do not share history.
> For multi-turn conversations, use a persistent session service and reuse the
> A2A context ID as the session ID.

## Serving It Over A2A

The executor connects A2A to `run_agent`: it reads the text of the incoming
message and replies with the agent's answer.

```python
class AdkExecutor(AgentExecutor):
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
    name="ADK Chat Agent",
    description="A conversational Google ADK agent served through Kynomesh",
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
    skills=[AgentSkill(id="chat", name="Chat", description="Brief, friendly replies; converts temperatures with a function tool", tags=["chat"])],
    # ...
)
```

```python
runner = build_runner()
await server.start(AdkExecutor(run), agent_card())
```

> [!NOTE] The URLs are placeholders for local development. On Kynomesh they are
> replaced with the real addresses, so you never hard-code them.

## Run It Locally

Run the unit tests, which make no API calls, then start the agent:

```shell
uv run pytest
GOOGLE_API_KEY=... uv run python -m adk_chat.main
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
  name: adk-chat
spec:
  pattern: Supervisor
  entry: chat-agent
  agents:
    - name: chat-agent
      container:
        image: adk-chat:latest
        imagePullPolicy: IfNotPresent
        env:
          - name: GOOGLE_API_KEY
            valueFrom:
              secretKeyRef:
                name: google-api-key
                key: GOOGLE_API_KEY
```

`entry` names the agent that outside callers reach. With one agent, that is
`chat-agent`. The API key comes from a Kubernetes secret rather than the
manifest.

Build the image, load it into your cluster, create the secret, and deploy:

```shell
make build
# kind:  kind load docker-image adk-chat:latest
# k3d:   k3d image import adk-chat:latest -c <cluster>
kubectl create secret generic google-api-key --from-literal=GOOGLE_API_KEY=...
make deploy
```

Wait for the `AgentSet` to report `Running`:

```shell
kubectl get agentset
```

## Talk to the Agent

Forward the agent's ingress service to your machine and send it a message:

```shell
kubectl port-forward svc/adk-chat-ingress 8490
```

```shell
a2acli -k -u https://localhost:8490 --override-host=localhost:8490 \
  send 'What is 72 F in Celsius?' -o json
```

The reply comes from Gemini, which called the `convert_temperature` tool to get
22.22 C. The `-k` flag skips verification of the local TLS certificate, and
`--override-host` makes the agent's advertised address match the forwarded port.

## Clean Up

```shell
make delete
```

## Use Your Own Agent

To serve a different ADK agent:

1. Replace the tool in `tools.py`, or add more, and list each one in the agent's
   `tools`. Keep the docstrings clear, since the model reads them.
2. Update `INSTRUCTION` to tell the agent when to use each tool.
3. Update the `AgentCard` name, description and skills to describe your agent.
4. Change `image` and the `env` entries in the `AgentSet` to match your build
   and the secrets it needs.

If your agent is built differently, for example with several sub-agents, you
only need to change what `run_agent` runs. The executor and `main.py` stay the
same.

## Troubleshooting

- **`GOOGLE_API_KEY must be set`:** the agent refuses to start without a key.
  Export it locally, or create the `google-api-key` secret before deploying.
- **`no compatible transports found`:** every `AgentInterface` in the card needs
  `protocol_version` set.
- **`AgentSet` never reaches `Running`:** check `kubectl get pods`. The image
  may not be loaded into your cluster, or the secret may be missing.
- **The reply says the run failed:** the error from Gemini, such as an invalid
  key or an unknown model name, is raised from `run_agent`. Check the agent's
  logs with `kubectl logs`.

## Next Steps

One agent is a good start. In the next tutorial,
[Supervisor](./2-supervisor.md), you split work across several agents.
