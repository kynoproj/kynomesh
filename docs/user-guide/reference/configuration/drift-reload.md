# Drift Reload

When a peer agent's capabilities change — it gains or loses a skill, changes
its supported transports, anything reflected in its A2A `AgentCard` — any
agent that already built a client for that peer keeps using the old, cached
card until its process restarts. This is how the A2A client SDKs work: an
agent's peer client resolves and caches the peer's `AgentCard` once, at
construction time, and never re-resolves it on its own.

`driftReload` closes that gap: when enabled, Kynomesh detects when a peer's
`AgentCard` has changed and automatically restarts exactly the pods still
using a stale cached copy, so they rebuild their peer client against the
current card.

`driftReload` is disabled by default — auto-restarting pods is a behavior
change with real blast radius, so you opt in explicitly.

```yaml
driftReload:
  enabled: true
```

- `enabled`: turns on automatic reload-on-drift for the agent. Defaults to
  `false`.

## How It Works

Kynomesh tracks each peer's current `AgentCard` hash and compares it against
what each of your agent's live pods last resolved. A pod is considered stale
for a peer once the peer's card has changed and that pod hasn't yet rebuilt
its client against the new one.

When `driftReload.enabled` is `true`, Kynomesh deletes exactly the stale
pods — not the whole fleet — so they restart and re-resolve their peer
client, picking up the peer's current `AgentCard`. Pods already on the
current card, or that haven't yet reported a hash for a given peer, are left
alone.

Reloads respect the same [`maxUnavailable`](rolling-update.md) budget as a
normal rolling update, so a drift-triggered restart never touches more pods
per pass than a spec change would. If a rolling update is already in
progress for the agent, drift-triggered restarts are deferred until the
rollout settles — the two never race each other.

Drift is tracked regardless of whether `driftReload` is enabled — turning it
on later doesn't require anything to "catch up."

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

- [Rolling Update](rolling-update.md) — the `maxUnavailable` batching budget
  drift-triggered reloads respect.
- [AgentSet Customization](agentset-customization.md) — other per-agent
  settings.
