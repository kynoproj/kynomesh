# AgentCard Drift Detection and Dependent Reload

## Problem

When agent `worker`'s capabilities change — it gains or loses a skill, changes
its supported transports, anything reflected in its A2A `AgentCard` — every
agent that has `worker` as a peer keeps calling it as if nothing changed,
indefinitely, until something forces those callers to reconstruct their A2A
clients.

This is how the A2A client SDKs are designed to work, and Kynomesh needs to
account for it rather than assume it away.

### Why callers can't just notice on their own

Tracing through the official A2A Python SDK (`a2aproject/a2a-python (v1.1.3)`,
`src/a2a/client/`):

- `ClientFactory.create_from_url` resolves the target's `AgentCard` via
  `A2ACardResolver.get_agent_card()` **exactly once**, at client-construction
  time, and hands the result to `BaseClient.__init__`, which stores it as
  `self._card` — a plain instance attribute.
- Every RPC method (`send_message`, `get_task`, `cancel_task`, the
  push-notification-config methods, …) reads that cached `self._card`. None of
  them re-resolve it.
- There is no TTL, no expiry, no background refresh, and no re-fetch-on-error.
  The only way the cached card is ever replaced is an explicit, caller-invoked
  `get_extended_agent_card()` call — something nothing in Kynomesh or generated
  agent code triggers automatically.

So a capability change on `worker` is invisible to any already-constructed
client for `worker` until that caller's own process restarts (or its code
explicitly reconstructs the client).

### Why this is real for Kynomesh agents

`kynomesh-go` and `kynomesh-py`'s peer-client constructors (`client.NewForPeer`
/ `client.new_for_peer`) are lazy and memoized per process
([kynomesh-go#31](https://github.com/kynoproj/kynomesh-go/issues/31),
[kynomesh-py#6](https://github.com/kynoproj/kynomesh-py/issues/6)): the first
call for a given peer resolves its `AgentCard` and builds a client; every
subsequent call for that same peer reuses it. That's the right behavior for cost
— it avoids redoing a full resolve and construction on every call — but it means
a cached client, once built, holds a peer's `AgentCard` for the rest of the
process's life, exactly per the SDK mechanics above.

### What this means for Kynomesh specifically

Separate from, and not fixed by, anything already in the broker:

- `NewAgentCardProxy` (`pkg/broker/agentcard_proxy.go`) resolves the _local_
  agent's own `AgentCard` fresh on every request to the broker's AgentCard
  endpoint. That's correct and sufficient for serving an accurate card to a
  _new_ caller — but it has nothing to do with what an existing caller's SDK
  client does with a card it already cached.
- Peer pod restarts are a separate, already-solved concern: peer addresses are
  stable per-AgentDeploy ClusterIP Service DNS names (see
  [Agent Discovery](agent-discovery.md)), so a restart doesn't strand a caller
  at a dead address. But a restart is exactly the kind of event that
  _incidentally_ forces a caller to rebuild its client and re-pick-up a changed
  card — the mechanism this proposal deliberately invokes on purpose for agents
  that don't restart on their own.

The upshot: the only way to make a live capability change propagate to existing
callers, short of an upstream SDK change, is for Kynomesh to detect the change
and force those callers' pods to restart.

## Proposed direction

1. **SDK: report the hash behind the cached client.** The first successful
   construction of a peer's client
   ([kynomesh-go#31](https://github.com/kynoproj/kynomesh-go/issues/31),
   [kynomesh-py#6](https://github.com/kynoproj/kynomesh-py/issues/6)) is the one
   moment the SDK actually knows which `AgentCard` it resolved and is now using.
   At that point, the SDK hashes the resolved card and records it — keyed by
   peer name — in a file on the shared `kynomesh-run` volume (e.g.
   `/var/run/kynomesh/peer-hashes.json`), updated incrementally as new
   peers are first resolved over the process's lifetime, not rewritten wholesale
   each time.

   A plain file rather than a `/metrics` gauge: the data is a small
   peer-name-keyed map of string hashes, not a numeric time series — encoding it
   as Prometheus labels would mean one gauge series per peer per pod, a
   cardinality/format mismatch for what's really just "current state of a small
   map." A file matches the pattern already used for `topology.json` (written
   once by an init container, read by another sidecar) and needs no new metrics
   wiring.

   The file must be cleared/truncated at process start, before any peer client
   is constructed, so a stale entry from a previous process incarnation (e.g. a
   peer removed from the topology and no longer called) never lingers. The file
   only ever contains hashes for peers the process actually resolved a client
   for — a peer listed in `topology.json` that the agent's code never calls
   simply has no entry, which the consuming side must treat as "unknown," not
   "drifted."

2. **Broker: expose the file.** The broker already shares the pod's volume and
   already serves introspection data (`/metrics`, `/healthz`, `/readyz`) on its
   introspection port (`pkg/broker/introspection.go`). It reads the
   peer-hashes file and serves its contents on a small new endpoint,
   analogous to the existing ones.

3. **Daemon: do both halves of the comparison, expose one decision-ready API.**
   The daemon already runs a per-pod scrape loop (`pkg/daemon/server/scraper`,
   `pkg/daemon/server/rater`) and already exposes a gRPC+REST-gateway API to the
   controller (`DaemonService.GetAgentDeployMetrics`,
   `pkg/apis/proto/daemon/daemon.proto`). Extend that same service with
   something like:

   ```
   GetPeerCardDrift(AgentDeployName) -> {
     peer_name: {
       latest_hash: string        // daemon's own polled, stability-gated hash for that peer
       reported_hashes: []string  // distinct hash(es) currently reported across this AgentDeploy's live pods
     }
   }
   ```

   The daemon, not the controller, fetches each managed peer's live `AgentCard`
   and hashes it (the same way it already reaches managed agents for metrics
   scraping), and separately scrapes each dependent pod's broker-exposed
   peer-hashes file. `reported_hashes` is plural because different replicas
   of the same AgentDeploy can legitimately be mid-transition — one pod may have
   already restarted and re-resolved, another may not have. The controller needs
   to know if _any_ live pod is still on a stale hash, not get a single
   collapsed value.

   The daemon owns the **stability gate**: don't treat a newly-observed
   server-side hash change as "the latest hash" until it's held steady across N
   consecutive polls (or a minimum duration) — a peer mid-rollout can briefly
   serve an old and new card from different replicas, and that shouldn't itself
   trigger dependent churn.

   The controller never reads a pod's exposed hash file directly, and never
   fetches a peer's `AgentCard` itself — both stay entirely inside the daemon,
   consistent with the daemon owning all pod/agent introspection and the
   controller staying focused on infrastructure reconciliation.

4. **Controller: sole owner of the decision to reload.** For each dependent
   AgentDeploy, the controller calls the daemon's new API, compares
   `latest_hash` against every entry in `reported_hashes` per peer, and:
   - **Defers to any active rollout.** Before acting, the controller checks
     `AgentDeploy.Status.UpdateHash != CurrentHash` — the exact same gate the
     autoscaler already applies before scaling
     (`pkg/reconciler/agentdeploy/scaling/autoscaler.go`:
     `"Skipping scale: AgentDeploy is updating"`). If a rollout is already in
     flight for any reason, skip triggering a card-drift reload this cycle and
     re-check on the next poll. This is controller-local state the daemon has no
     visibility into and doesn't need — the daemon stays purely introspection,
     the controller stays the only place rollout state and reload decisions are
     made.
   - **Never blocks a subsequent spec change.** If a card-drift reload is
     already rolling and the user pushes an unrelated spec change to the same
     AgentDeploy mid-rollout, the spec change wins with no special handling
     needed: a card-drift reload recreates pods on the _same_ desired hash (just
     forcing a restart), while a spec change recreates pods on a _new_ desired
     hash. The existing hash-comparison reconcile loop
     (`pkg/reconciler/agentdeploy/pods.go`) naturally converges on whatever the
     current desired hash is on the next pass — there is nothing to cancel or
     preempt.
   - **Reloads via the existing machinery.** If clear to act, the controller
     recreates the affected pods using the same hash-annotation-driven
     rolling-recreate path already used for pod-spec drift
     (`pkg/reconciler/agentdeploy/pods.go`), batched by `maxUnavailable` rather
     than recreating every dependent pod at once.

## Enabling this: `peerWatch`

This must be opt-in/opt-out, not always-on — for some deployments,
auto-reloading on every capability change is undesirable (agents that
legitimately change their card often, or environments where uncontrolled pod
churn is itself a cost).

The toggle is a grouped field, `peerWatch`, available at both levels:

- **AgentSet level:** `spec.peerWatch` — the default for every agent in the set.
- **Agent level:** `spec.agents[*].peerWatch` — overrides the AgentSet-level
  default for that agent, following the same fill-if-unset pattern already used
  for `BrokerContainer`/`InitContainer` templates
  (`ContainerTemplate.ApplyDefaultsFrom`,
  `pkg/apis/kynomesh/v1alpha1/container_template.go`).

`peerWatch` is deliberately a struct (e.g. `{enabled: bool}` today) rather than
a bare boolean, so related settings in the same "how does this agent react to
its peers" space can be added later without introducing new top-level fields.
The exact field name, shape, precedence details, and default (on vs. off) are
provisional — to be finalized during implementation, whichever is easiest to
build correctly.

What exactly `enabled` gates — whether it stops the daemon from tracking drift
for that agent at all, or only stops the controller from acting on drift it
still detects — is also left to implementation.

## Non-goals

- This does not change anything about the A2A SDKs' caching behavior itself —
  it's a Kynomesh-side workaround for a caching design that's correct SDK
  behavior, not a bug to report upstream.
- This does not change peer-address resolution or pod-restart handling — those
  already work via stable Service DNS (see
  [Agent Discovery](agent-discovery.md)) and are out of scope here.
- **External agents are out of scope for v1.** Detecting drift on an external
  agent (per [#147](https://github.com/kynoproj/kynomesh/issues/147)) means the
  daemon polling a third-party URL on a schedule — which runs into an unresolved
  prerequisite: there is no credentials field anywhere in the AgentSet API today
  for authenticating to an external agent's endpoint, and most external agents
  will require some form of auth to serve their card. This needs a credentials
  story before it can be designed, not just implemented, and is left open rather
  than scoped into v1.

## Open questions

- **Detection cadence.** What interval the daemon polls managed peers'
  `AgentCard`s on, and how that interacts with (or reuses) its existing scrape
  loop (`pkg/daemon/server/rater`'s `DefaultScrapeInterval` = 5s) — left open
  intentionally; not a blocking design decision, tunable during implementation.
- **Daemon poll-trigger model.** Whether the daemon polls peers on its own
  independent background timer (mirroring the reconciler-side `Sampler`'s 60s
  loop, `pkg/reconciler/agentdeploy/scaling/sampler.go`) feeding a cache the new
  API just reads from, or polls synchronously when `GetPeerCardDrift` is called.
- **External-agent drift detection**, per the Non-goals section above — blocked
  on a credentials story for external agents that doesn't exist yet.

## See Also

- [Agent Discovery](agent-discovery.md) — how peers resolve each other's
  addresses today; the DNS stability this proposal deliberately doesn't touch.
- [External Agents](external-agents.md) — `spec.externalAgents`, referenced by
  the external-agent non-goal above.
- [Rolling Update](../../user-guide/reference/configuration/rolling-update.md) —
  the batching behavior a dependent reload respects.
