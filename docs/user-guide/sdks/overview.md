# SDKs

Kynomesh agents are plain [A2A](https://a2a-protocol.org/) agents — you can
write one in any language with an A2A implementation, and Kynomesh will run it.
The SDKs are optional conveniences, not a requirement.

What they save you is the wiring. Without an SDK you have to pick the right
listener for the environment, mount the transports your `AgentCard` advertises,
register with the broker so peers can find you, and resolve a peer's URL and
transport before you can call it. The SDKs do all of that, so your code deals
with agent logic and peer _names_.

| SDK    | Install                                  | Source                                                          |
| ------ | ---------------------------------------- | --------------------------------------------------------------- |
| Python | `pip install kynomesh`                   | [kynoproj/kynomesh-py](https://github.com/kynoproj/kynomesh-py) |
| Go     | `go get github.com/kynoproj/kynomesh-go` | [kynoproj/kynomesh-go](https://github.com/kynoproj/kynomesh-go) |

Both SDKs expose the same two halves:

- **Server** — start an A2A agent the broker can reach.
- **Client** — call other agents in the same AgentSet by name, with no URLs,
  transports, or `AgentCard` plumbing in your code.

## Writing an Agent

Implement the A2A executor interface, describe your agent with an `AgentCard`,
and hand both to the SDK's start function.

=== "Python"

    ```python
    import asyncio
    import signal
    import uuid

    from a2a.server.agent_execution.agent_executor import AgentExecutor
    from a2a.types.a2a_pb2 import AgentCard, AgentInterface, Message, Part, Role
    from a2a.utils.constants import TransportProtocol

    from kynomesh import server


    class HelloWorldAgentExecutor(AgentExecutor):
        async def execute(self, context, event_queue) -> None:
            await event_queue.enqueue_event(
                Message(
                    message_id=str(uuid.uuid4()),
                    role=Role.ROLE_AGENT,
                    parts=[Part(text="Hello, world!")],
                )
            )

        async def cancel(self, context, event_queue) -> None:
            pass


    def hello_world_card() -> AgentCard:
        return AgentCard(
            name="Hello World Agent",
            version="0.0.1",
            supported_interfaces=[
                AgentInterface(
                    url="http://127.0.0.1:8088/a2a/jsonrpc",
                    protocol_binding=TransportProtocol.JSONRPC.value,
                ),
                AgentInterface(
                    url="127.0.0.1:8089",
                    protocol_binding=TransportProtocol.GRPC.value,
                ),
            ],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )


    async def main() -> None:
        loop = asyncio.get_running_loop()
        serve_task = asyncio.ensure_future(
            server.start(HelloWorldAgentExecutor(), hello_world_card())
        )
        for sig in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(sig, serve_task.cancel)
        try:
            await serve_task
        except asyncio.CancelledError:
            pass


    if __name__ == "__main__":
        asyncio.run(main())
    ```

=== "Go"

    ```go
    package main

    import (
        "context"
        "iter"
        "log"
        "os/signal"
        "syscall"

        "github.com/a2aproject/a2a-go/v2/a2a"
        "github.com/a2aproject/a2a-go/v2/a2asrv"
        "github.com/kynoproj/kynomesh-go/pkg/server"
    )

    type agentExecutor struct{}

    func (*agentExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
        return func(yield func(a2a.Event, error) bool) {
            yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Hello, world!")), nil)
        }
    }

    func (*agentExecutor) Cancel(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
        return func(yield func(a2a.Event, error) bool) {}
    }

    func main() {
        ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
        defer stop()

        card := &a2a.AgentCard{
            Name:    "Hello World Agent",
            Version: "0.0.1",
            SupportedInterfaces: []*a2a.AgentInterface{
                a2a.NewAgentInterface("http://127.0.0.1:8088/a2a/jsonrpc", a2a.TransportProtocolJSONRPC),
                a2a.NewAgentInterface("127.0.0.1:8089", a2a.TransportProtocolGRPC),
            },
        }
        if err := server.Start(ctx, &agentExecutor{}, card); err != nil {
            log.Fatalf("agent server: %v", err)
        }
    }
    ```

The start function handles the environment differences for you: it selects the
right listeners (Unix domain sockets in-cluster, localhost ports when you run
locally), mounts the JSON-RPC, REST, and gRPC transports your card advertises,
and registers the agent for peer discovery.

The URLs in the `AgentCard` above are development addresses. In-cluster,
Kynomesh's broker sidecar fronts your agent and advertises the reachable address
to peers — see
[Agent Discovery](../../development/specifications/agent-discovery.md).

### Exposing Transports

List every transport you want callers to be able to use in
`supported_interfaces` / `SupportedInterfaces` — most agents only need one, but
you can advertise more than one and let each caller pick:

| Transport        | Python constant               | Go constant                     |
| ---------------- | ----------------------------- | ------------------------------- |
| JSON-RPC         | `TransportProtocol.JSONRPC`   | `a2a.TransportProtocolJSONRPC`  |
| REST (HTTP+JSON) | `TransportProtocol.HTTP_JSON` | `a2a.TransportProtocolHTTPJSON` |
| gRPC             | `TransportProtocol.GRPC`      | `a2a.TransportProtocolGRPC`     |

=== "Python"

    ```python
    AgentCard(
        name="Hello World Agent",
        version="0.0.1",
        supported_interfaces=[
            AgentInterface(
                url="http://127.0.0.1:8088/a2a/jsonrpc",
                protocol_binding=TransportProtocol.JSONRPC.value,
            ),
            AgentInterface(
                url="http://127.0.0.1:8088/a2a/rest",
                protocol_binding=TransportProtocol.HTTP_JSON.value,
            ),
            AgentInterface(
                url="127.0.0.1:8089",
                protocol_binding=TransportProtocol.GRPC.value,
            ),
        ],
        default_input_modes=["text"],
        default_output_modes=["text"],
    )
    ```

=== "Go"

    ```go
    card := &a2a.AgentCard{
        Name:    "Hello World Agent",
        Version: "0.0.1",
        SupportedInterfaces: []*a2a.AgentInterface{
            a2a.NewAgentInterface("http://127.0.0.1:8088/a2a/jsonrpc", a2a.TransportProtocolJSONRPC),
            a2a.NewAgentInterface("http://127.0.0.1:8088/a2a/rest", a2a.TransportProtocolHTTPJSON),
            a2a.NewAgentInterface("127.0.0.1:8089", a2a.TransportProtocolGRPC),
        },
    }
    ```

The URLs here are only used for local development — running your agent
in-cluster, Kynomesh's broker fronts it and advertises the reachable address to
peers, so you don't need to construct those addresses yourself. If your agent
needs to be reachable from outside the cluster too, set `publicBaseURL` on the
agent (see the [API reference](../../APIs.md)).

### Health Checks

By default an agent reports `SERVING` as soon as it starts. If readiness depends
on something else — a model endpoint, a warm cache, a downstream dependency —
own the signal yourself and flip it as that dependency changes. Both the gRPC
and HTTP health endpoints reflect it, so Kubernetes readiness probes and the
broker see the same state.

=== "Python"

    ```python
    from kynomesh import server

    health = server.Health()

    async def watch_dependency() -> None:
        # Replace with your own readiness check (a model endpoint, a cache
        # warm-up, etc). Mark not-ready while it's down, ready once it recovers.
        while True:
            ready = await check_llm_endpoint()
            health.set_serving(ready)
            await asyncio.sleep(5)

    async def main() -> None:
        asyncio.ensure_future(watch_dependency())
        await server.start(
            HelloWorldAgentExecutor(),
            hello_world_card(),
            server.with_health(health),
        )
    ```

=== "Go"

    ```go
    health := server.NewHealth()
    go watchHealth(ctx, health, checkLLM) // your own dependency check

    if err := server.Start(ctx, &agentExecutor{}, card,
        server.WithHealth(health),
    ); err != nil {
        log.Fatalf("agent server: %v", err)
    }
    ```

## Calling a Peer Agent

Inside an AgentSet you address peers by the name declared in
`spec.agents[*].name` — no URL, no transport negotiation. Kynomesh resolves the
peer and picks a transport both sides support.

=== "Python"

    ```python
    import asyncio
    import uuid

    from a2a.types.a2a_pb2 import Message, Part, Role, SendMessageRequest

    from kynomesh import client


    async def main() -> None:
        a2a_client = await client.peer_client("worker-a")
        request = SendMessageRequest(
            message=Message(
                message_id=str(uuid.uuid4()),
                role=Role.ROLE_USER,
                parts=[Part(text="Hello, world")],
            )
        )
        async for response in a2a_client.send_message(request):
            print(response)


    asyncio.run(main())
    ```

=== "Go"

    ```go
    package main

    import (
        "context"
        "log"

        "github.com/a2aproject/a2a-go/v2/a2a"
        "github.com/kynoproj/kynomesh-go/pkg/client"
    )

    func main() {
        ctx := context.Background()

        c, err := client.PeerClient(ctx, "worker-a")
        if err != nil {
            log.Fatalf("create client: %v", err)
        }

        msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Hello, world"))
        resp, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
        if err != nil {
            log.Fatalf("send message: %v", err)
        }
        log.Printf("response: %+v", resp)
    }
    ```

### Clients Are Built Once and Reused

The first call for a given peer name resolves that peer's `AgentCard` and builds
a client; every later call for the same name reuses it, for the life of the
process. This is deliberate — re-resolving the card on every request would add a
round-trip and client construction to each call for no benefit when the peer
hasn't changed.

The tradeoff is that a cached client keeps serving the card it resolved at build
time. If a peer's capabilities change while your agent is running, your agent
won't notice on its own. Two ways to handle that:

- **Automatically** — turn on [Drift Reload](../reference/drift-reload.md), and
  Kynomesh restarts exactly the pods holding a stale card when a peer's card
  actually changes.
- **Manually** — drop the cached client yourself with
  `client.forget_peer("worker-a")` (Python) or `client.ForgetPeer("worker-a")`
  (Go); the next call rebuilds it.

### Peer Discovery Helpers

When you don't need a full client:

| Purpose                              | Python                            | Go                                   |
| ------------------------------------ | --------------------------------- | ------------------------------------ |
| Build (or reuse) a client for a peer | `client.peer_client(name)`        | `client.PeerClient(ctx, name)`       |
| Get just the peer's URL              | `client.peer_url(name)`           | `client.PeerURL(name)`               |
| Fetch a peer's `AgentCard`           | `client.resolve_agent_card(name)` | `client.ResolveAgentCard(ctx, name)` |
| List reachable peers                 | `client.peers()`                  | `client.Peers()`                     |
| Drop a cached client                 | `client.forget_peer(name)`        | `client.ForgetPeer(name)`            |

### Error Handling

The realistic failure case inside a running agent is calling a peer that isn't
in your topology — either it doesn't exist, or the routing pattern forbids you
from reaching it. Check for that specifically and treat anything else as a
generic failure:

=== "Python"

    ```python
    from kynomesh import client

    try:
        a2a_client = await client.peer_client("worker-a")
    except client.PeerNotFoundError:
        ...  # no such peer, or the routing pattern forbids reaching it
    except Exception:
        ...  # something else went wrong resolving the peer
    ```

=== "Go"

    ```go
    c, err := client.PeerClient(ctx, "worker-a")
    switch {
    case errors.Is(err, client.ErrPeerNotFound):
        // no such peer, or the routing pattern forbids reaching it
    case err != nil:
        // something else went wrong resolving the peer
    }
    ```

## See Also

- [Quick Start](../../quick-start.md) — run your first AgentSet end to end.
- [AgentSet](../../core-concepts/agentset.md) — where peer names and the
  communication pattern are declared.
- [Agent Discovery](../../development/specifications/agent-discovery.md) — how
  peers resolve each other's addresses.
- [Drift Reload](../reference/drift-reload.md) — keeping cached peer clients
  from going stale.
