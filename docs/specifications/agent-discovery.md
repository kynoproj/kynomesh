# Agent Discovery

> [!NOTE] This document is for Kynomesh contributors. It describes the internal
> mechanics of how callers and peer agents find and reach agents inside an
> AgentSet.

There are three distinct discovery paths in Kynomesh, each backed by a different
Kubernetes Service object:

1. **External callers → AgentSet entry.** A normal ClusterIP Service owned by
   the AgentSet, routing to the _entry_ AgentDeploy's pods.
2. **Peer-to-peer (agent → sibling broker).** A normal ClusterIP Service owned
   by each sibling AgentDeploy, load-balancing across that sibling's pods.
3. **Per-pod introspection (scraper → individual pod).** A headless Service
   owned by each AgentDeploy, exposing each pod's broker introspection port
   under a stable DNS name. Used by per-pod metrics scrapers, primarily for
   autoscaling.

## Services per AgentSet/AgentDeploy

For an AgentSet `greeter` with agents `planner` (entry) and `worker`, the
following objects are created in the namespace:

| Service                    | Owner                         | Type      | Selector matches                          |
| -------------------------- | ----------------------------- | --------- | ----------------------------------------- |
| `greeter-ingress`          | AgentSet `greeter`            | ClusterIP | entry pods of `greeter` (any AgentDeploy) |
| `greeter-planner`          | AgentDeploy `greeter-planner` | ClusterIP | pods of the `planner` AgentDeploy         |
| `greeter-planner-headless` | AgentDeploy `greeter-planner` | Headless  | pods of the `planner` AgentDeploy         |
| `greeter-worker`           | AgentDeploy `greeter-worker`  | ClusterIP | pods of the `worker` AgentDeploy          |
| `greeter-worker-headless`  | AgentDeploy `greeter-worker`  | Headless  | pods of the `worker` AgentDeploy          |

The naming convention is fixed:

- Per-AgentSet entry service: `<agentset>-ingress`.
- Per-AgentDeploy ClusterIP service: `<agentdeploy>`.
- Per-AgentDeploy headless service: `<agentdeploy>-headless`.

> [!IMPORTANT] Because the entry service is `<agentset>-ingress`, the agent name
> `ingress` is reserved by the AgentSet validator. Without that guard, a child
> AgentDeploy `<agentset>-ingress` and its ClusterIP Service would collide with
> the AgentSet's entry service.

## External path: AgentSet entry service

The entry service is the front door of an AgentSet. Anything outside the set
(other AgentSets, HTTP gateways, manual `curl` from a developer pod) reaches the
set through `<agentset>-ingress.<namespace>.svc.cluster.local:8490`.

### Port

The entry service exposes a single `broker` port (`8490`) targeting the broker
sidecar's named `broker` port.

## Peer path: per-AgentDeploy ClusterIP service

When an agent needs to send a message to a sibling agent inside the same
AgentSet, it resolves the sibling's per-AgentDeploy ClusterIP Service DNS name
directly and opens an A2A connection to it. The sibling's ClusterIP service
load-balances the connection across the sibling's ready pods, where the
sibling's broker terminates the A2A call and hands it down to the sibling agent
over their shared UDS.

The traversal in one line is:

```
agent  ->  <sibling-agentdeploy>.<ns>.svc.cluster.local:8490  ->  sibling broker  ->  sibling agent
```

The agent learns the sibling's DNS name from its topology file (see below); no
Kubernetes API lookup is involved at request time.

## Per-pod introspection: headless service

Each AgentDeploy also owns a headless Service named `<agentdeploy>-headless`
(`clusterIP: None`, `publishNotReadyAddresses: true`), so each AgentDeploy pod
gets an A record:

```
<agentdeploy>-<replica>.<agentdeploy>-headless.<ns>.svc.cluster.local
```

The headless service exposes the broker's **introspection port** (`8491`) for
precise pod-level metrics scraping, used for autoscaling.

## Topology file

The agent itself never reads the Kubernetes API. The AgentSet controller encodes
per-child peer information into each AgentDeploy spec when it builds children.
At pod startup, an init container materializes that payload into
`/var/run/kynomesh/topology.json`, which the agent and broker read.

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

Peer URLs use the per-AgentDeploy ClusterIP Service of the sibling —
load-balanced across the sibling's replicas and readiness-gated.

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

For a sequential pattern (`planner -> reviewer -> publisher`), the agent in
`planner` reads its topology file and finds a single peer entry pointing at
`greeter-reviewer.<ns>.svc.cluster.local:8490`, and so on down the chain.
