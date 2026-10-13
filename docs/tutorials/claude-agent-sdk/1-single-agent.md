# Tutorial: A Single Claude Agent

In this tutorial you run a conversational agent built with the
[Claude Agent SDK](https://code.claude.com/docs/en/agent-sdk/python), first on
your machine and then on Kynomesh as a one-agent
[`AgentSet`](../../core-concepts/agentset.md). The agent has one custom tool
that converts temperatures. You will also see how to lock the agent down so it
is safe to run in a pod, and what to change to serve your own agent.

The full example is in
[kynoproj/kynomesh-examples](https://github.com/kynoproj/kynomesh-examples/tree/main/claude-agent-sdk/single-agent).
Follow the [overview](./0-overview.md#prerequisites) first if you have not set
up the prerequisites. Running a single agent on Kynomesh already gives you
autoscaling, zero-downtime rollouts and rate limiting. See the
[LangGraph single-agent tutorial](../langgraph/1-single-agent.md#why-run-a-single-agent-on-kynomesh)
for the full list.

```shell
git clone https://github.com/kynoproj/kynomesh-examples.git
cd kynomesh-examples/claude-agent-sdk/single-agent
```

## What's in the Example

| File                          | Purpose                                                                       |
| ----------------------------- | ----------------------------------------------------------------------------- |
| `src/claude_chat/agent.py`    | `run_agent`: one Claude run per request, locked down for a pod.               |
| `src/claude_chat/tools.py`    | A custom in-process tool the agent can call.                                  |
| `src/claude_chat/executor.py` | Turns an A2A message into a call to `run_agent`, and the result into a reply. |
| `src/claude_chat/main.py`     | The `AgentCard`, and serving the agent over A2A.                              |
| `manifests/agentset.yaml`     | The `AgentSet` that deploys it.                                               |

`agent.py` and `tools.py` are plain Claude Agent SDK code with nothing
Kynomesh-specific. Only `main.py` uses the Kynomesh SDK.

## A Custom Tool

The agent's one tool converts a temperature between Celsius, Fahrenheit and
Kelvin. It is an ordinary Claude Agent SDK tool that runs inside your process:

```python
@tool(
    "convert_temperature",
    "Convert a temperature between C (Celsius), F (Fahrenheit) and K (Kelvin).",
    {"value": float, "from_unit": str, "to_unit": str},
)
async def convert_temperature(args: dict[str, Any]) -> dict[str, Any]:
    try:
        result = convert(args["value"], args["from_unit"], args["to_unit"])
    except (KeyError, ValueError) as e:
        return {"content": [{"type": "text", "text": f"Error: {e}"}], "is_error": True}
    return {"content": [{"type": "text", "text": f"{result:.2f} {args['to_unit'].strip().upper()}"}]}
```

A bad unit is returned to the model as a tool error, so it can recover instead
of failing the whole request.

## The Agent

`run_agent` runs one Claude query for each request and returns the final text.
The options are where the agent is locked down:

```python
def build_options() -> ClaudeAgentOptions:
    return ClaudeAgentOptions(
        system_prompt=SYSTEM_PROMPT,
        model=os.getenv("ANTHROPIC_MODEL") or None,
        tools=[],  # disable every built-in tool
        mcp_servers={SERVER_NAME: create_sdk_mcp_server(name=SERVER_NAME, tools=[convert_temperature])},
        strict_mcp_config=True,  # ignore any other MCP config on disk
        allowed_tools=[f"mcp__{SERVER_NAME}__{convert_temperature.name}"],
        permission_mode="dontAsk",  # deny anything not allowed instead of prompting
        setting_sources=[],  # load no user/project/local settings
        max_turns=MAX_TURNS,
    )
```

An agent in a pod serves requests from other agents and clients, so it should do
only what you intend. Each option closes one gap:

| Option                      | Why                                                                      |
| --------------------------- | ------------------------------------------------------------------------ |
| `tools=[]`                  | Removes every built-in tool, such as Bash, Read and Write.               |
| `allowed_tools`             | Allows only your own tool.                                               |
| `permission_mode="dontAsk"` | Denies anything else instead of waiting for an approval nobody can give. |
| `setting_sources=[]`        | Loads no settings from disk.                                             |
| `strict_mcp_config=True`    | Ignores any other MCP configuration on disk.                             |
| `max_turns`                 | Bounds how long a single run can go on.                                  |

Set `ANTHROPIC_MODEL` to choose a model. Otherwise the SDK's default is used.

> [!NOTE] Each request starts the Claude Code CLI bundled with the SDK, so
> expect a few seconds of startup per request.

## Serving It Over A2A

The executor connects A2A to `run_agent`: it reads the text of the incoming
message and replies with the agent's answer.

```python
class ClaudeExecutor(AgentExecutor):
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
    name="Claude Chat Agent",
    description="A conversational Claude Agent SDK agent served through Kynomesh",
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
await server.start(ClaudeExecutor(run_agent), agent_card())
```

> [!NOTE] The URLs are placeholders for local development. On Kynomesh they are
> replaced with the real addresses, so you never hard-code them.

## Run It Locally

Run the unit tests, which make no API calls, then start the agent:

```shell
uv run pytest
ANTHROPIC_API_KEY=... uv run python -m claude_chat.main
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
  name: claude-chat
spec:
  pattern: Supervisor
  entry: chat-agent
  agents:
    - name: chat-agent
      container:
        image: claude-chat:latest
        imagePullPolicy: IfNotPresent
        env:
          - name: ANTHROPIC_API_KEY
            valueFrom:
              secretKeyRef:
                name: anthropic-api-key
                key: ANTHROPIC_API_KEY
```

`entry` names the agent that outside callers reach. With one agent, that is
`chat-agent`. The API key comes from a Kubernetes secret rather than the
manifest.

The image runs as an unprivileged user, and the bundled Claude Code CLI writes
state under `$HOME`, so the Dockerfile points `HOME` at a writable directory:

```dockerfile
ENV HOME=/tmp/home
RUN mkdir -p /tmp/home && chmod 1777 /tmp/home
USER nobody
```

Build the image, load it into your cluster, create the secret, and deploy:

```shell
make build
# kind:  kind load docker-image claude-chat:latest
# k3d:   k3d image import claude-chat:latest -c <cluster>
kubectl create secret generic anthropic-api-key --from-literal=ANTHROPIC_API_KEY=...
make deploy
```

Wait for the `AgentSet` to report `Running`:

```shell
kubectl get agentset
```

## Talk to the Agent

Forward the agent's ingress service to your machine and send it a message:

```shell
kubectl port-forward svc/claude-chat-ingress 8490
```

```shell
a2acli -k -u https://localhost:8490 --override-host=localhost:8490 \
  send 'What is 72 F in Celsius?' -o json
```

The reply comes from Claude, which called the `convert_temperature` tool to get
22.22 C. The `-k` flag skips verification of the local TLS certificate, and
`--override-host` makes the agent's advertised address match the forwarded port.

## Clean Up

```shell
make delete
```

## Use Your Own Agent

To serve a different Claude Agent SDK agent:

1. Replace the tool in `tools.py`, or add more, and list each one in
   `build_options()`: once in `mcp_servers` and once in `allowed_tools`. A tool
   that is not in `allowed_tools` is denied.
2. Update `SYSTEM_PROMPT` to tell the agent when to use each tool.
3. Update the `AgentCard` name, description and skills to describe your agent.
4. Change `image` and the `env` entries in the `AgentSet` to match your build
   and the secrets it needs.

> [!WARNING] Keep `tools=[]` and `permission_mode="dontAsk"` unless you mean to
> give the agent more. Re-enabling built-in tools such as Bash lets a request
> run commands inside your pod.

## Troubleshooting

- **`ANTHROPIC_API_KEY must be set`:** the agent refuses to start without a key.
  Export it locally, or create the `anthropic-api-key` secret before deploying.
- **`no compatible transports found`:** every `AgentInterface` in the card needs
  `protocol_version` set.
- **`AgentSet` never reaches `Running`:** check `kubectl get pods`. The image
  may not be loaded into your cluster, or the secret may be missing.
- **Requests are slow:** each request starts the Claude Code CLI, so a few
  seconds of overhead is expected.

## Next Steps

One agent is a good start. In the next tutorial, [Dynamic peers](./2-dynamic-peers.md),
you build a supervisor that finds its Claude workers on its own.
