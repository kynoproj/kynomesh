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
`https://calculator-ingress.other-namespace.svc:8490`, and its broker presents a
self-signed certificate rather than a valid, CA-signed one. Verification
therefore fails.

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
      url: https://calculator-ingress.other-namespace.svc:8490
```

Your agent code does not change.

> [!WARNING] Skipping verification means the SDK cannot tell whether it is
> talking to the real peer. Use it only for agents you operate, and not for
> endpoints reached over an untrusted network.

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
