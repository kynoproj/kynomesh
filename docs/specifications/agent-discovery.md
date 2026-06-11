# Agent Discovery

> [!NOTE]
> This document is for Kynomesh contributors. It describes the internal
> mechanics of how callers and peer agents find and reach agents inside an
> AgentSet. End users only need to know the public addresses described in
> [AgentSet](../core-concepts/agentset.md) and [AgentDeploy](../core-concepts/agentdeploy.md).

There are two distinct discovery paths in Kynomesh, each backed by a different
Kubernetes Service object:

1. **External callers → AgentSet entry.** A normal ClusterIP Service owned by
   the AgentSet, routing to the *entry* AgentDeploy's pods.
2. **Peer-to-peer (broker → broker).** Per-replica DNS records served by a
   headless Service owned by each AgentDeploy.

Each path serves a different audience, has different stability guarantees, and
is owned by a different controller. Knowing which is which is the difference
between a service-discovery bug being a one-liner and a multi-day rabbit hole.

## Services per AgentSet/AgentDeploy

For an AgentSet `greeter` with agents `planner` (entry) and `worker`, the
following objects are created in the namespace:

| Service                       | Owner                  | Type      | Selector matches                            |
| ----------------------------- | ---------------------- | --------- | ------------------------------------------- |
| `greeter-ingress`             | AgentSet `greeter`     | ClusterIP | entry pods of `greeter` (any AgentDeploy)   |
| `greeter-planner`             | AgentDeploy `greeter-planner` | ClusterIP | pods of the `planner` AgentDeploy           |
| `greeter-planner-headless`    | AgentDeploy `greeter-planner` | Headless  | pods of the `planner` AgentDeploy           |
| `greeter-worker`              | AgentDeploy `greeter-worker`  | ClusterIP | pods of the `worker` AgentDeploy            |
| `greeter-worker-headless`     | AgentDeploy `greeter-worker`  | Headless  | pods of the `worker` AgentDeploy            |

The naming convention is fixed:

- Per-AgentSet entry service: `<agentset>-ingress` — see
  `EntryServiceSuffix` in `pkg/apis/kynomesh/v1alpha1/agentset_types.go`.
- Per-AgentDeploy ClusterIP: `<agentdeploy>` — `(*AgentDeploy).ServiceName()`.
- Per-AgentDeploy headless: `<agentdeploy>-headless` — `(*AgentDeploy).HeadlessServiceName()`.

> [!IMPORTANT]
> Because the entry service is `<agentset>-ingress`, the agent name `ingress`
> is reserved and rejected by `validator.ValidateAgentSet`. Without that
> guard, a child AgentDeploy `<agentset>-ingress` and its ClusterIP Service
> would collide with the AgentSet's entry service.

## External path: AgentSet entry service

The entry service is the front door of an AgentSet. Anything outside the set
(other AgentSets, HTTP gateways, manual `curl` from a developer pod) reaches
the set through `<agentset>-ingress.<namespace>.svc.cluster.local:8490`.

### Lifecycle

- Created and reconciled by the **AgentSet controller** in
  `pkg/reconciler/agentset/controller.go` (`reconcileEntryService`).
- Owned by the parent AgentSet via `ctrl.SetControllerReference`. It survives
  child-AgentDeploy deletion and recreation, so the ClusterIP — and therefore
  the DNS name's resolved address — is stable across rollouts and entry
  recreation.
- Deleted by the AgentSet controller on the AgentSet's deletion path
  (`deleteEntryService`), before the finalizer is dropped.
- Drift-corrected by hash compare: each reconcile re-derives the desired spec,
  hashes it into the `kynomesh.kyno.sh/hash` annotation, and recreates the
  service if the hash on the live object differs.

### Selector design

The entry service does **not** select on `agentdeploy-name`. Instead it
selects on a label that the AgentDeploy controller stamps onto pods of the
entry agent:

```yaml
selector:
  kynomesh.kyno.sh/agentset-name: greeter
  app.kubernetes.io/managed-by: agentdeploy-controller
  kynomesh.kyno.sh/entry: "true"
  kynomesh.kyno.sh/serving: "true"
```

This decouples the entry service from the identity of the current entry
AgentDeploy. If `spec.entry` ever changes from `planner` to `worker`, the
service's selector does not change at all — the AgentSet controller relabels
its children, pods of the new entry get `entry=true`, pods of the old entry
lose it, and Kubernetes' endpoint controller silently re-targets the service.

The `entry` label is stamped by the AgentDeploy controller in `newPod` based
on `ad.Spec.Topology.IsEntry`, which is itself set by the AgentSet controller
in `computeTopology`. Source of truth: `AgentSet.Spec.Entry`. Materialized:
pod label.

The `serving=true` filter ensures the service only routes to pods the
controller considers ready to take traffic. The headless service deliberately
publishes not-ready addresses (see below); the entry service does not.

### Port

The entry service exposes a single `broker` port (`AgentBrokerPort = 8490`)
targeting the named port `broker` on the broker sidecar. Introspection
(`8491`) is intentionally not exposed — it's for metrics and probes, not
A2A traffic.

## Peer path: per-AgentDeploy headless service

When a broker needs to reach a sibling agent inside the same AgentSet, it
does **not** go through the entry service. It resolves the sibling's
per-replica DNS name directly.

For each AgentDeploy `greeter-worker`:

- A headless Service `greeter-worker-headless` is created with
  `clusterIP: None` and `publishNotReadyAddresses: true`.
- Each pod in the AgentDeploy has `subdomain=<agentdeploy>-headless` and
  `hostname=<agentdeploy>-<replica>`, so kubelet creates a stable A record at:

  ```
  <agentdeploy>-<replica>.<agentdeploy>-headless.<ns>.svc.cluster.local
  ```

### Why headless, not ClusterIP

The broker needs per-replica addressability — stickiness to a specific
agent instance during a multi-turn conversation, observability that names
the instance that handled a request, and the ability to round-robin
explicitly rather than relying on kube-proxy. A ClusterIP service hides all
that behind a single VIP.

### Why `publishNotReadyAddresses: true`

Agents in an AgentSet typically need to address each other during bootstrap,
*before* any of them has passed its readiness probe. If headless DNS waited
on readiness, peers couldn't discover each other to come up. Once each agent
is up, the broker's exec probe (over the broker UDS) flips it to ready;
external callers wait for that via the entry service's readiness filter, but
peers do not.

This is also why the per-AgentDeploy *ClusterIP* service (`<agentdeploy>`) is
provided alongside the headless one: code paths that do want load-balanced,
readiness-gated access to a specific AgentDeploy (not the AgentSet's entry)
should target that service instead of the headless one.

## Topology file

The agent itself never reads the Kubernetes API. The AgentSet controller
encodes the peer URLs into the AgentDeploy spec when it builds children, the
AgentDeploy controller mounts that encoded payload into the `init-runtime`
container, and `init-runtime` writes the resolved file to
`/var/run/kynomesh/topology.json` for the agent and broker to read.

```json
{
  "pattern": "Supervisor",
  "isEntry": true,
  "peers": [
    {
      "name": "worker",
      "url": "https://greeter-worker.ns.svc.cluster.local:8490"
    }
  ]
}
```

Peer URLs use the per-AgentDeploy ClusterIP Service of the sibling — load
balanced across the sibling's replicas, readiness-gated. Per-replica
headless addresses are used only when the broker needs to pin to a specific
instance.

## End-to-end request flow

For an external HTTP call landing on the entry service of an AgentSet:

```
   external caller
        |
        v
   greeter-ingress.<ns>.svc.cluster.local:8490    (AgentSet-owned, ClusterIP)
        |  selector matches: agentset-name=greeter, entry=true, serving=true
        v
   pod: greeter-planner-0-xxxxx  (broker sidecar)
        |  forwards over UDS
        v
   container: agent  (user code)
        |  hands off to a worker via topology peer URL
        v
   greeter-worker.<ns>.svc.cluster.local:8490     (AgentDeploy-owned, ClusterIP)
        |
        v
   pod: greeter-worker-0-yyyyy   (broker sidecar) -> agent
```

For a sequential pattern (`planner -> reviewer -> publisher`), the broker on
`planner` resolves the topology file to a single peer entry pointing at
`greeter-reviewer.<ns>.svc.cluster.local:8490`, and so on down the chain.

## Watch graph

Both controllers watch their owned Services so that out-of-band mutations
(an operator deleting a service by hand, another controller editing it) are
healed:

- AgentSet controller watches `corev1.Service` with `OnlyControllerOwner`
  filtering for AgentSet ownership.
- AgentDeploy controller watches `corev1.Service` with `OnlyControllerOwner`
  filtering for AgentDeploy ownership.

A delete or spec edit re-enqueues the owning resource, which re-runs its
service reconcile and either no-ops on hash match or recreates on drift.

## Pitfalls to avoid

- **Do not point external callers at `<agentdeploy>`.** That service is
  ClusterIP and load-balances across the entry AgentDeploy's pods *but* its
  identity is tied to the AgentDeploy, not the AgentSet. If `spec.entry`
  flips to a different agent, callers pointing at the old name silently
  keep going to the old (now non-entry) agent.
- **Do not point peers at `<agentset>-ingress`.** Peers want to send
  messages to a specific sibling agent, not "whatever is the current entry
  of the set." Use the sibling's per-AgentDeploy ClusterIP service.
- **Do not assume the headless service load-balances.** It returns every
  pod IP and lets the client choose. Brokers do that explicitly; user code
  reading the headless DNS name will see all replicas at once.
- **Do not strip the `entry` or `serving` labels on pods.** They are the
  selector inputs for the entry service; if a hook or mutator removes them,
  the pod silently stops receiving entry traffic without any controller-side
  error.

## Code map

| Concern                         | File                                                              |
| ------------------------------- | ----------------------------------------------------------------- |
| Entry service spec & reconcile  | `pkg/reconciler/agentset/controller.go` (`reconcileEntryService`, `newEntryService`) |
| Entry service deletion          | `pkg/reconciler/agentset/controller.go` (`deleteEntryService`)    |
| Per-AgentDeploy ClusterIP svc   | `pkg/reconciler/agentdeploy/controller.go` (`newClusterIPService`) |
| Per-AgentDeploy headless svc    | `pkg/reconciler/agentdeploy/controller.go` (`newHeadlessService`) |
| Entry/serving pod labels        | `pkg/reconciler/agentdeploy/controller.go` (`newPod`)             |
| Topology stamping               | `pkg/reconciler/agentset/controller.go` (`computeTopology`)       |
| Reserved agent name guard       | `pkg/reconciler/validator/agentset.go` (`ValidateAgentSet`)       |
| Label and constant definitions  | `pkg/apis/kynomesh/v1alpha1/const.go`                             |
| Service name helpers            | `pkg/apis/kynomesh/v1alpha1/agentset_types.go`, `agentdeploy_types.go` |
