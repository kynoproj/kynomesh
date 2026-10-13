# External Agents

Use `.spec.externalAgents` to let an AgentSet's agents talk to an agent this
AgentSet doesn't own - an agent in another AgentSet, or any A2A endpoint
reachable at a URL - without redeclaring its spec or deploying a duplicate.

No pod, Service, or broker is ever created for an external agent. It's purely a
reference: a name (for peer bookkeeping, same as a managed agent's name) and a
URL.

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Handoff
  entry: coordinator
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
    - name: worker
      container:
        image: example/worker:latest
  externalAgents:
    - name: fraud-check
      url: https://fraud-check.example.com
```

`fraud-check` never gets a pod - it's added to `coordinator` and `worker`'s peer
list, and their brokers can reach it directly at the given URL, the same way
they'd reach a managed sibling.

## What an external agent can and can't do

An external agent's code is outside Kynomesh's control, so Kynomesh can't
guarantee it forwards a call onward to another peer. It can be **called**, but
it can never be relied on to **originate** a call to a further peer. Concretely:

- An external agent can never be `entry`.
- It can appear anywhere else in `Handoff` or `Supervisor` - see below.
- In `Sequential`, it can only be the **last** hop in the chain, and at most one
  external agent is allowed.

## Per-pattern behavior

### Handoff

Every managed agent's peer list includes every other managed agent plus every
external agent - external agents are just more peers in the mesh. Managed agents
may call an external one; nothing calls back through it.

### Supervisor

Only `entry`'s peer list includes anyone, and that list includes every external
agent alongside the other managed agents - an external agent works well here as
one of the entry's "workers," since the entry calls it and nothing more is
required of it.

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: planner
  agents:
    - name: planner
      container:
        image: example/planner:latest
    - name: worker
      container:
        image: example/worker:latest
  externalAgents:
    - name: search-api
      url: https://search.example.com
```

`planner` (the entry) sees both `worker` and `search-api` as peers; `worker`
sees nobody, same as any non-entry agent under Supervisor.

### Sequential

The chain runs through `agents[]` in declaration order, and an external agent -
if configured - is always the final hop, regardless of where it's declared in
`externalAgents` (there can only be one):

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Sequential
  entry: intake
  agents:
    - name: intake
      container:
        image: example/intake:latest
    - name: review
      container:
        image: example/review:latest
  externalAgents:
    - name: fraud-check
      url: https://fraud-check.example.com
```

This produces the chain `intake -> review -> fraud-check`. `review` (the last
managed agent) gets `fraud-check` as its one peer; `fraud-check` itself gets no
peers, since nothing is deployed for it.

A chain like `intake -> fraud-check -> review` - an external agent _in the
middle_ of the chain - isn't possible: `fraud-check`'s code has no way to know
it should call `review` next, so the AgentSet would reject a config that tries
to make it a mid-chain hop by requiring it to be last.

## Calling an agent in another AgentSet (TLS)

When your agent calls a peer through the Kynomesh [SDK](../../sdks/overview.md),
the SDK decides whether to verify the peer's TLS certificate:

- **Managed peers** (agents in the same AgentSet) present their broker's
  self-signed certificate, so the SDK skips verification. The connection is
  encrypted, but the peer is not authenticated.
- **External peers** are expected to present a real certificate signed by a CA,
  so the SDK verifies it as usual.

An external agent that lives in **another AgentSet** is the exception. You
connect to it directly through its in-cluster Kubernetes DNS name, for example
`https://tools-calculator.other-namespace.svc:8490` (the form is
`<agentset>-<agent>`, here agent `calculator` in AgentSet `tools`), and its
broker presents a self-signed certificate rather than a valid, CA-signed one.
Verification therefore fails.

To treat such a peer like a managed one, list its name in the
`KYNOMESH_INSECURE_TLS_PEERS` environment variable on the **calling** agent's
container. The value is a comma-separated list of external agent names:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: planner
  agents:
    - name: planner
      container:
        image: example/planner:latest
        env:
          - name: KYNOMESH_INSECURE_TLS_PEERS
            value: calculator
  externalAgents:
    - name: calculator
      url: https://tools-calculator.other-namespace.svc:8490
```

Your agent code does not change.

> [!WARNING] Skipping verification means the SDK cannot tell whether it is
> talking to the real peer. Use it only for agents you operate, and not for
> endpoints reached over an untrusted network.

## Passing headers and credentials

A real external agent usually wants to know who is calling, for example with an
`Authorization: Bearer ...` header or an API key. Kynomesh's API has no field
for this: `externalAgents` carries only a name and a URL. Instead, your agent
code supplies the credentials, and there are two separate places they may be
needed:

- **On calls to the agent.** You attach the headers to each call.
- **On the peer's `AgentCard`.** Some agents also require credentials just to
  read their card. The SDK reads the card when it first builds a client for the
  peer, so you give it headers for that request.

Keep secrets out of the manifest. Store each token in a Kubernetes Secret and
expose it to the calling agent as an environment variable:

```yaml
env:
  - name: FRAUD_CHECK_TOKEN
    valueFrom:
      secretKeyRef:
        name: fraud-check-token
        key: token
```

### Authenticating calls

`peer_client` / `PeerClient` return a standard A2A client, and A2A calls accept
"service parameters", which the client sends as HTTP headers (or as gRPC
metadata when the gRPC transport is used).

=== "Python"

    Pass a `ClientCallContext` to the call:

    ```python
    import os

    from a2a.client.client import ClientCallContext
    from kynomesh import client

    a2a_client = await client.peer_client("fraud-check")
    context = ClientCallContext(
        service_parameters={"Authorization": f"Bearer {os.environ['FRAUD_CHECK_TOKEN']}"}
    )
    async for response in a2a_client.send_message(request, context=context):
        ...
    ```

=== "Go"

    Attach the parameters to the context of the call:

    ```go
    c, err := client.PeerClient(ctx, "fraud-check")
    if err != nil {
        return err
    }
    ctx = a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{
        "Authorization": {"Bearer " + os.Getenv("FRAUD_CHECK_TOKEN")},
    })
    resp, err := c.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
    ```

    To attach the same headers to every call instead, register an
    `a2aclient.CallInterceptor` with
    `client.PeerClient(ctx, "fraud-check", a2aclient.WithCallInterceptors(...))`.
    Options are applied only when the peer's client is first built, because
    clients are cached.

Because you set them per call, you can refresh a token that expires, or send a
different one per request, without rebuilding the cached client. Attach them
only on calls to the peer that needs them: the headers go to whichever peer you
call.

### Peers that protect their AgentCard

If the peer also requires credentials to read its card, building a client fails
with an authorization error, because the SDK requests the card without any
headers. Give it headers for that one request:

=== "Python"

    ```python
    a2a_client = await client.peer_client(
        "fraud-check",
        card_headers={"Authorization": f"Bearer {os.environ['FRAUD_CHECK_TOKEN']}"},
    )
    ```

    To fetch a card on its own, use
    `client.resolve_agent_card("fraud-check", headers=...)`.

=== "Go"

    ```go
    c, err := client.PeerClientWithCardHeaders(ctx, "fraud-check", map[string]string{
        "Authorization": "Bearer " + os.Getenv("FRAUD_CHECK_TOKEN"),
    })
    ```

    To fetch a card on its own, `client.ResolveAgentCard` accepts
    `agentcard.WithRequestHeader(name, value)`.

The card headers are sent **only** on the card request. They are not attached to
calls, so authenticate calls as shown above. A peer that protects both needs
both.

A few things to know:

- **Applied on first build only.** Clients are cached, so card headers passed on
  later calls are ignored. After a token changes, call `client.forget_peer(...)`
  (Python) or `client.ForgetPeer(...)` (Go), then build the client again.
- **Redirects are not followed** on the card request, so the headers are never
  re-sent to another host.
- **Check TLS.** If the peer's certificate is not verified (a Managed peer, or
  one named in `KYNOMESH_INSECURE_TLS_PEERS`), the SDK logs a warning, because
  the headers can be read by anyone on the network path. Prefer a CA-signed
  certificate for a peer that needs credentials.
- **Header rules.** Names may contain only letters, digits, `-`, `_` and `.` and
  must not start with `grpc-`. Values must be printable ASCII, so no line
  breaks. The same name may not appear twice in different cases. A malformed
  header fails before any request is made, and the error never includes the
  value.

> [!NOTE] Card headers need Python SDK 0.1.2 or newer, or Go SDK 0.2.2 or newer.
> With older versions the card must be readable without credentials.

## Validation

- `externalAgents[*].name` follows the same naming rules as `agents[*].name`
  (DNS-1035, not reserved, no duplicates - the two lists share one name
  namespace).
- `externalAgents[*].url` must be an absolute URL (`scheme://host`).
- `entry` must name a managed agent; naming an external agent is rejected.
- Under `Sequential`, at most one `externalAgents` entry is allowed.

## See Also

- [AgentSet](../../../core-concepts/agentset.md) - the `pattern`/`entry`/
  `agents` fields external agents interact with.
- [Agent Discovery](../../../development/specifications/agent-discovery.md) -
  how a managed agent resolves and reaches a peer's broker.
- [Public Base URL](public-base-url.md) - the inverse direction: making one of
  your own agents reachable from outside the cluster.
