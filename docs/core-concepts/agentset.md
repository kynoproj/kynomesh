# AgentSet

An **AgentSet** is the top-level Kynomesh resource. It declares a group of
cooperating agents and how they discover one another. The AgentSet controller
materializes each declared agent into its own [AgentDeploy](./agentdeploy.md)
and stamps the per-agent topology onto each child so the agent and its broker
know who they are allowed to talk to.

## Anatomy

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: greeter
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
```

## Patterns

The `pattern` field controls how the controller derives each agent's peer list.

### Supervisor

The entry agent sees every other agent; non-entry agents see nobody. Equivalent
to manager / orchestrator-worker / subagent designs.

```
       planner (entry)
      /    |    \
     v     v     v
   worker1 worker2 worker3
```

### Handoff

Every agent sees every other agent. Equivalent to a swarm or fully connected
network — any agent can hand work off to any other.

```
   alpha <----> beta
      ^          ^
      |          |
      v          v
   gamma <----> delta
```

### Sequential

Each agent sees only the next agent in declaration order. The entry must be
`agents[0]`.

```
   alpha (entry) -> beta -> gamma -> delta
```

Every pattern can also include agents this AgentSet doesn't deploy — see
[External Agents](../user-guide/reference/configuration/external-agents.md).

## Kubectl

To query `AgentSet` objects with `kubectl`:

```sh
kubectl get agentset # or "as" as a short name
```

## See Also

- [AgentDeploy](./agentdeploy.md) — per-agent deployment and broker injection.
- [External Agents](../user-guide/reference/configuration/external-agents.md) —
  reference an agent this AgentSet doesn't deploy.
- [APIs](../APIs.md) — full CRD reference.
