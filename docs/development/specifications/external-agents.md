# External Agents

> [!NOTE] This document is for Kynomesh contributors. It records the design
> decisions for [#147](https://github.com/kynoproj/kynomesh/issues/147) —
> referencing an existing/external agent as a peer, without the AgentSet
> deploying a duplicate.

## Problem

- Kynomesh is targeted to support "hybrid topologies: mix in-cluster managed
  agents with external agents reachable at a URL".
- `AgentSetSpec.Agents []AbstractAgentDeploy` is homogeneous — every entry is a
  real agent this AgentSet deploys, scales, and rolls. There is no way to
  declare "treat this existing agent (in another AgentSet, or truly outside the
  cluster) as a peer, but don't deploy anything for it."

## What an external agent actually is

No pod, Service, or broker is ever run for an external agent. Concretely:

- It is not scaled, rate-limited, health-probed, or rolled by Kynomesh — there
  is no Kynomesh-owned process there to do any of that to.
- It carries no auth, header, or TLS-trust configuration in the Kynomesh API.
  That configuration would need to live on a broker fronting the external agent,
  and no such broker exists. Any auth/TLS the external endpoint requires is the
  concern of whichever **managed** agent calls it — normal application-level
  configuration on the caller, entirely outside Kynomesh's API surface. This
  isn't deferred as future work; it's out of scope by construction, because
  there is no Kynomesh-managed process on the external side to configure.
- Its only two properties are therefore a **name** (for topology/peer
  bookkeeping, matching how `AbstractAgentDeploy.Name` works for managed agents)
  and a **reference** (currently: a raw URL; see
  [Future: friendlier in-cluster reference](#future-friendlier-in-cluster-reference)).

## Where can an external agent appear in a pattern?

An external agent's code is opaque to Kynomesh — nothing guarantees it reads
`topology.json` or forwards a call onward. So it can only ever **receive** a
call, never be relied on to **originate** one to a further peer. That one rule
determines where it's legal in each pattern:

- **Never `Entry`.** Entry is "the agent external callers reach first" —
  Kynomesh routes to it and, in Supervisor, expects it to fan out to peers. An
  agent Kynomesh doesn't run and doesn't control can't be asked to do that.
- **Handoff** (every agent sees every other agent): an external agent can be any
  of the peers. Managed agents may call it; nothing requires it to call back,
  which is fine — Handoff peers are permitted, not required, to call each other.
- **Supervisor** (only `Entry` gets a peer list): an external agent can be one
  of the entry's peers/workers — the entry calls it, it doesn't need to call
  anyone further. Naturally terminal, so naturally fine.
- **Sequential** (`agents[i]` sees only `agents[i+1]`): an external agent may
  only be the **last** hop in the chain. `managed → managed → external` is a
  legitimate pipeline (e.g. `intake → intake2 → external-fraud-check`) — the
  managed agents do the forwarding, the external agent is a valid dead end.
  `managed → external → managed` is impossible to support: the middle hop is
  opaque, so Kynomesh has no way to guarantee it calls the next agent — the
  chain would silently dead-end at a black box that looks, from the spec, like
  it continues.

Because a Sequential chain can have at most one terminal hop, **Sequential
supports at most one external agent, and only as the final hop.**

## API shape

### Options considered

**A — flag on `AbstractAgentDeploy`, one list.** Add
`External *ExternalAgentRef` directly to the existing `AbstractAgentDeploy`
struct (the `Agents[]` element type), alongside `Container *Container`.
Discriminator: exactly one of `Container`/`External` set.

- ✅ No new top-level list; `agents[]` keeps today's flat `- name: foo` YAML
  shape for every entry.
- ❌ An external entry has 20+ other fields on paper that are meaningless for it
  (`Container`'s required-ness aside: `Scale`, `RateLimit`, `Sidecars`,
  `InitContainers`, `UpdateStrategy`, `PublicBaseURL`, plus everything inlined
  from `AbstractPodTemplate`). "Must be empty when `External` is set" is
  enforceable only by validation, not by the schema — a user can write
  `external: {...}` next to a populated `container:` and only find out at
  admission time which one (if either) wins.

**B — wrapper type inside `agents[]`** (`managed:`/`external:` variants per
element). Rejected quickly: it has the same YAML nesting cost as a separate
top-level list, but without that list's benefit of leaving today's `agents[]`
shape untouched. If nesting is being accepted anyway, a top-level list is
strictly better than hiding the same nesting inside `agents[]`.

**C — separate top-level `externalAgents []ExternalAgentRef` list.** Initially
set aside over the Sequential ordering question — a second, unordered list
seemingly can't express "this external agent is the third link in the chain."
Revisited once the **terminal-only** rule above was established: Sequential
never needs to interleave an external agent mid-chain, only append at most one
after the last managed agent. That ordering need is exactly "how many entries
does `externalAgents` have," not "at what index" — a second list is sufficient
once external participation is constrained to the tail.

### Decision: Option C

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Sequential
  entry: intake1
  agents:
    - name: intake1
      container:
        image: my-intake:latest
    - name: intake2
      container:
        image: my-intake2:latest
  externalAgents:
    - name: fraud-check
      url: https://fraud-check.example.com
```

```go
type AgentSetSpec struct {
    Pattern        AgentPattern
    Entry          string
    Agents         []AbstractAgentDeploy // unchanged
    ExternalAgents []ExternalAgentRef     // NEW
    Templates      *Templates
}

// ExternalAgentRef is a reference to an agent this AgentSet does not deploy,
// scale, or roll — no pod, Service, or broker is created for it.
type ExternalAgentRef struct {
    // +kubebuilder:validation:Required
    Name string `json:"name" ...`
    // +kubebuilder:validation:Required
    URL string `json:"url" ...`
}
```

`AbstractAgentDeploy` is untouched. `agents[]` keeps its current shape for every
existing manifest.

The name `externalAgents` (rather than `peers`, `externalPeers`,
`referencedAgents`, or `agentRefs`) was chosen to match the existing
`PeerKindExternal` vocabulary already in the API
(`pkg/apis/kynomesh/v1alpha1/agentdeploy_types.go`) — what a user writes in
`spec.externalAgents` becomes, one-to-one, a `Peer{Kind: PeerKindExternal}`
entry in the generated `topology.peers`. `peers`/`externalPeers` were passed
over because "peer" is a topology-time (computed) concept — describing `Agents`
as peers of each other too would be equally valid, so scoping "peer" to only the
external list in the spec is misleading.

## Pattern semantics with `externalAgents`

- **Handoff**: every managed agent's peer list = all other managed agents + all
  `externalAgents`. External agents themselves get no peer list (no AgentDeploy
  is created for them, so there's no `Topology` to stamp).
- **Supervisor**: only `Entry`'s peer list includes anyone; that list = all
  other managed agents + all `externalAgents`.
- **Sequential**: chain is `agents[0] -> agents[1] -> ... -> agents[n-1]`,
  optionally followed by exactly one more hop: `externalAgents[0]`, if present.
  `len(externalAgents) > 1` is a validation error under Sequential.

## Validation additions

`pkg/reconciler/validator/agentset.go` (`ValidateAgentSet`) needs:

- Each `ExternalAgentRef.Name`: non-empty, DNS-1035-valid (same rule as managed
  agent names, for consistency — it's a peer identity even though no Service is
  created for it), not colliding with reserved names, and unique across **both**
  `Agents` and `ExternalAgents` (one flat name namespace — an external agent
  named the same as a managed agent would be ambiguous in `topology.peers`).
- Each `ExternalAgentRef.URL`: non-empty. Whether to additionally validate it
  parses as an absolute URL (scheme + host) at admission time, versus leaving it
  fully opaque and letting unreachable/malformed URLs surface as runtime call
  failures, is still open — leaning toward validating parseability at minimum,
  since that's a cheap, unambiguous check and a malformed URL can never be
  correct.
- `Entry` must name an agent in `Agents` — already true today structurally, but
  should get an explicit rejection message ("entry must be a managed agent; got
  external agent %q") if someone names an `ExternalAgents` entry, rather than
  falling through to today's generic "entry does not name any agent" message.
- `Sequential` requires `len(ExternalAgents) <= 1`.
- Still open: should `Handoff`'s and `Sequential`'s existing "requires at least
  2 agents" checks count `len(Agents) + len(ExternalAgents)`, or strictly
  `len(Agents)`? An external agent can't originate calls, so a Handoff of 1
  managed + 1 external isn't obviously "2 agents talking to each other" in the
  sense the current check protects against. Leaning toward counting `Agents`
  only, but not yet decided.

## `computeTopology` changes

`pkg/reconciler/agentset/agentdeploys.go`:

- `peersExcluding` gains a second input (`as.Spec.ExternalAgents`) and appends
  `Peer{Name: e.Name, Kind: PeerKindExternal, URL: e.URL}` for each, in both the
  Handoff case and the Supervisor-entry case.
- The Sequential branch (`nextAgent`) needs a new case: when `self` is the last
  entry in `Agents` and `len(as.Spec.ExternalAgents) == 1`, the peer list
  becomes `[]Peer{{Name, Kind: PeerKindExternal, URL: ...}}` instead of `nil`.
- No AgentDeploy is ever created for an `ExternalAgents` entry — the
  reconciler's `Agents`-only iteration for pod/Service/AgentDeploy creation is
  unaffected; only the peer-list computation reads `ExternalAgents`.

Downstream (`resolvePeerURLs` in `cmd/commands/init_runtime.go`) needs no change
— it already passes `Kind: PeerKindExternal` peers' `URL` through untouched.

## Future: friendlier in-cluster reference

The original issue asks for two ways to point at a peer: a raw URL (for truly
external endpoints), and a friendlier reference for an agent already known to
the cluster — e.g. by its AgentSet + agent name — that the controller resolves
to the existing ClusterIP DNS name
(`https://<agentset>-<agent>.<namespace>.svc.cluster.local:8490`, per
[Agent Discovery](agent-discovery.md)) so users don't hand-write it.

This is deliberately deferred out of v1: raw `url` alone is sufficient to close
the issue's core gap (no user-facing field for external peers at all today), and
the friendlier reference is additive — it can be introduced later as a second,
mutually-exclusive field on `ExternalAgentRef` (e.g.
`AgentSetRef *LocalAgentSetRef`) without touching anything decided above.

## Resolved during implementation

- **URL validation strictness**: validated as an absolute URL (non-empty
  scheme and host via `net/url.Parse`) at admission time. A malformed URL
  can never be correct, so rejecting it early is strictly better than
  letting it surface as a runtime call failure.
- **Whether the "≥2 agents" minimum counts `ExternalAgents`**: no —
  `Handoff`'s and `Sequential`'s minimum-agent checks still count
  `len(Agents)` only. An external agent can't originate a call, so it
  doesn't change whether the managed agents have anyone to talk to among
  themselves.

Implemented in `pkg/apis/kynomesh/v1alpha1/agentset_types.go`
(`ExternalAgentRef`), `pkg/reconciler/validator/agentset.go`, and
`pkg/reconciler/agentset/agentdeploys.go` (`computeTopology`,
`peersExcluding`, `nextAgent`).
