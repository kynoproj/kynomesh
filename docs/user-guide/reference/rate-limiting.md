# Rate limiting

`Kynomesh` can cap how much load each agent admits, shedding the excess rather
than letting it queue. Set a per-agent **maximum in-flight** limit and the
agent's broker rejects requests beyond the cap instead of forwarding them to
your agent container.

This is enforcement, not just observation: once an agent is at its limit, new
requests are refused immediately so the agent — and whatever it depends on —
stays within budget.

## When to use it

The typical reason is an **external dependency with a hard ceiling**: an LLM
provider quota, a rate-limited downstream API, or a shared database you don't
want a single agent to overwhelm. A concurrency cap bounds how many requests can
be in flight against that dependency at once.

For agentic (LLM/tool) workloads the binding resource is _slot occupancy_ — each
in-flight request holds a context and an upstream connection for the duration of
a (often multi-second) call. Capping concurrency bounds that directly, which is
why the limit is expressed as **max in-flight requests** rather than
requests-per-second.

## What it caps

Only **A2A traffic** (JSON-RPC, REST, and gRPC) counts against the limit.
Passthrough traffic — custom REST endpoints, UIs, WebSocket upgrades the broker
also proxies — is **not** gated, so a long-lived UI or WebSocket connection
never consumes an in-flight slot meant for A2A requests.

## Configuration

`rateLimit` is set per agent, on each entry under the AgentSet's `agents` list:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-assistant
spec:
  pattern: Supervisor
  entry: coordinator
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
      rateLimit:
        maxInFlight: 20 # Max concurrent in-flight A2A requests for this agent.
    - name: searcher
      container:
        image: example/searcher:latest
      # No rateLimit block: searcher admits requests without limit.
```

- `maxInFlight` — the maximum number of concurrent in-flight A2A requests the
  agent admits across its whole fleet. `0` or unset means **unlimited**.

## What happens at the limit

When an agent is already handling `maxInFlight` requests, the broker rejects the
next one immediately, before it reaches your agent container:

| Transport       | Rejection                                       |
| --------------- | ----------------------------------------------- |
| JSON-RPC / REST | HTTP `429 Too Many Requests` with `Retry-After` |
| gRPC            | `RESOURCE_EXHAUSTED`                            |

Clients should treat these as back-pressure and retry with backoff.

## Fleet-wide, not per-pod

`maxInFlight` is the cap for the **whole agent**, not per replica. When an agent
runs multiple replicas, they share the limit so the fleet together stays near
it.

The cap is **approximate**: it can drift briefly while the agent is scaling, and
under heavy load the fleet may admit slightly more than `maxInFlight`. Treat it
as a target rather than a hard ceiling — if you're protecting a strict external
limit, set `maxInFlight` with a little headroom below it.

## Interaction with autoscaling

Rate limiting and [autoscaling](./autoscaling.md) compose. When an agent reaches
its `maxInFlight` cap, **scale-up is suppressed** — adding replicas won't raise
a fixed external ceiling, so the agent settles at the replica count that serves
the admitted load instead of climbing toward `max`.

Scale-**down** is unaffected: an idle rate-limited agent still sheds replicas
normally.

## Observability

The broker exposes a counter for rejected requests, labeled by transport:

- `broker_rejected_total{transport}` — requests refused at admission because the
  agent was at its in-flight cap.

Read it alongside `broker_inflight_requests{transport}` (the current in-flight
count the limit is compared against). A rising `broker_rejected_total` with
`broker_inflight_requests` pinned near the cap means the agent is actively
shedding load — the signal that demand exceeds the configured limit.

## See Also

- [Autoscaling](./autoscaling.md) — how rate limiting bounds scale-up.
- [AgentDeploy](../../core-concepts/agentdeploy.md) — the unit the limit applies
  to.
- [AgentSet](../../core-concepts/agentset.md) — where the `rateLimit` block
  lives.
