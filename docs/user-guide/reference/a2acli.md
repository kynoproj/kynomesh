# A2A Command-Line Client

[`a2acli`](https://github.com/kynoproj/a2acli) is a general-purpose command-line
client for the [A2A](https://a2a-protocol.org/) (Agent-to-Agent) protocol - it
works against **any** A2A-compliant server, not just Kynomesh. It fetches
AgentCards, sends one-shot messages, streams events, and manages tasks over any
of A2A's three transports (JSON-RPC, REST, gRPC), so it's a useful tool any time
you need to talk to an A2A agent from the terminal.

Kynomesh's broker speaks A2A, so `a2acli` also happens to be the tool used
throughout these docs.

## Install

```shell
curl -fsSL https://raw.githubusercontent.com/kynoproj/a2acli/main/install.sh | bash
```

Or via Go: `go install github.com/kynoproj/a2acli@latest`.

## Basic usage

Every command targets an A2A server via `-u/--url`:

```shell
a2acli card -u http://127.0.0.1:9001                        # fetch and print the AgentCard
a2acli send -u http://127.0.0.1:9001 "your message"          # one-shot message
a2acli send --stream -u http://127.0.0.1:9001 "your message" # stream events as they arrive
a2acli task get -u http://127.0.0.1:9001 <task-id>           # fetch a task
a2acli task list -u http://127.0.0.1:9001                    # list tasks
a2acli task cancel -u http://127.0.0.1:9001 <task-id>        # cancel a task
a2acli task subscribe -u http://127.0.0.1:9001 <task-id>     # re-subscribe and stream an existing task
```

Continue a multi-turn conversation with the `taskId`/`contextId` from a prior
response:

```shell
a2acli send -u <url> --task <task-id> "a follow-up"
a2acli send -u <url> --context <context-id> "what did I just ask?"
```

`-o json` prints the raw A2A response for scripting; the default `text` output
is more readable for interactive use.

See the [full command reference](https://github.com/kynoproj/a2acli#usage) for
every flag — protocol selection (`--protocol jsonrpc|rest|grpc`), sending
structured messages (`--json`, `--parts`, `-f/--file`), multi-tenant routing
(`--tenant`), request tracing (`-v/--verbose`), bypassing AgentCard resolution
with a direct `--endpoint`, and overriding the AgentCard's advertised host with
`--override-host` (e.g. when you've port-forwarded an in-cluster agent locally).

## Debugging inside the cluster

When you want to reach an agent's in-cluster address directly, run `a2acli` from
inside the cluster instead. Its image is meant for exactly this:

```shell
kubectl run a2acli-debug --rm -it --restart=Never \
  --image=quay.io/kynoproj/a2acli:latest \
  --command -- bash
# then, inside the pod:
a2acli card -u https://my-agentset-worker.my-namespace.svc.cluster.local:8490 -k
```

Or as an ephemeral debug container against an existing pod:

```shell
kubectl debug -it some-pod --image=quay.io/kynoproj/a2acli:latest -- bash
```

## See Also

- [Quick Start](../../quick-start.md) — a full walkthrough using `a2acli`
  against a real AgentSet.
