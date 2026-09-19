# Drift Reload

When a peer agent's capabilities change — it gains or loses a skill, changes its
supported transports, anything reflected in its A2A `AgentCard` — any agent that
already built a client for that peer keeps using the old, cached card
indefinitely, because Kynomesh's peer clients are built once and reused for the
life of the process (see [Why This Matters](#why-this-matters) below).

`driftReload` closes that gap: when enabled, Kynomesh detects when a peer's
`AgentCard` has changed and automatically restarts exactly the pods still using
a stale cached copy, so they rebuild their peer client against the current card.

`driftReload` is disabled by default — auto-restarting pods is a behavior change
with real blast radius, so you opt in explicitly.

```yaml
driftReload:
  enabled: true
```

- `enabled`: turns on automatic reload-on-drift for the agent. Defaults to
  `false`.

## Why This Matters

The Kynomesh SDKs build a peer's client once, the first time your agent code
calls that peer, and reuse it for the lifetime of the process — see
[Python SDK](https://github.com/kynoproj/kynomesh-py). That first call resolves
the peer's `AgentCard` and caches it inside the client; every subsequent call to
that same peer reuses the cached client and its cached card, with no
re-resolution. The only way to make the SDK drop a cached client and rebuild it
is an explicit, caller-invoked forget call — nothing in a normal A2A call path
triggers that on its own.

This is deliberate, and it's the right design: re-fetching a peer's `AgentCard`
and reconstructing its client on every A2A call would add a resolve round-trip
and client-construction cost to every request, for no benefit in the common case
where the peer hasn't changed. Caching is correct here.

The tradeoff is that nothing about a normal A2A call ever checks whether the
cached card is still accurate. If a peer's capabilities change while your agent
is running, your agent keeps calling it as if nothing changed, indefinitely,
until its process happens to restart for some unrelated reason. `driftReload` is
what closes that gap without giving up the performance benefit of caching:
instead of re-resolving on every call, it detects when a peer's card has
actually changed and forces a restart only then, only for the pods still holding
a stale copy.

## Setting the Default for a Fleet

`driftReload` can be set once at the AgentSet level as the default for every
agent, and overridden per agent:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-team
spec:
  pattern: Supervisor
  entry: planner
  driftReload:
    enabled: true # fleet-wide default
  agents:
    - name: planner
      container:
        image: planner:latest
      driftReload:
        enabled: false # this agent opts out even though the fleet default is on
    - name: worker
      container:
        image: worker:latest
      # omitted — inherits the fleet default (enabled: true)
```

A per-agent `driftReload` that is set always wins outright over the
AgentSet-level default, whether it turns the setting on or off.

## See Also

- [Rolling Update](configuration/rolling-update.md) — the `maxUnavailable`
  batching budget drift-triggered reloads respect.
- [AgentSet Customization](configuration/agentset-customization.md) — other
  per-agent settings.
