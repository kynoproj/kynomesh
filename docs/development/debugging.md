# How To Debug

## Debug Logging

To enable debug logs on a container, set the `LOG_LEVEL` environment variable to
`debug`. It's read by every Kynomesh process — broker, agent init containers,
the per-AgentSet daemon, and the controller-manager — so the same variable works
regardless of which container you're debugging.

To enable it on an agent's broker (via the shared template, so it applies to
every agent in the AgentSet, or per agent):

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  templates:
    agent:
      brokerContainer:
        env:
          - name: LOG_LEVEL
            value: debug
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
```

To enable it on the per-AgentSet daemon:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  templates:
    daemon:
      container:
        env:
          - name: LOG_LEVEL
            value: debug
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
```

For the controller-manager, set `LOG_LEVEL=debug` on its Deployment
(`config/base/controller-manager/controller-manager-deployment.yaml`).

## Profiling

`pprof` is available on the broker's introspection port, gated separately from
debug logging by the `KYNOMESH_PPROF_ENABLED` environment variable
(unset/`false` by default — the endpoints are disabled unless explicitly turned
on):

```yaml
brokerContainer:
  env:
    - name: KYNOMESH_PPROF_ENABLED
      value: "true"
```

With that set, profile the broker's memory usage:

```sh
# Port-forward the broker's introspection port.
kubectl port-forward my-agent-0-7jzbn 8491:8491

go tool pprof -http localhost:8081 https+insecure://localhost:8491/debug/pprof/heap
```

Tracing is also available:

```sh
# Add optional "&seconds=n" to specify the duration.
curl -sk https://localhost:8491/debug/pprof/trace?debug=1 -o trace.out

go tool trace -http localhost:8082 trace.out
```

The broker's introspection endpoints are TLS-only with a self-signed certificate
(see [Metrics](../operations/metrics.md)), which is why the examples above use
`https+insecure://` / `curl -k`.

`pprof` is only wired up on the broker today — the daemon and controller-manager
don't expose `/debug/pprof/*`.

## Peer AgentCard Hashes

The broker's introspection port also serves `/peer-hashes`, the contents of the
peer-hashes file the agent's SDK writes to the shared `kynomesh-run` volume
(`/var/run/kynomesh/peer-hashes.json`) — a peer-name-keyed map of the
`AgentCard` hash behind each peer client the agent process has resolved (see
[AgentCard Drift Detection and Dependent Reload](specifications/agentcard-drift-reload.md)).
The broker serves the file's contents verbatim and doesn't interpret them; if
the file doesn't exist yet (no peer clients resolved since the last agent
restart), the endpoint returns an empty JSON object rather than an error.

```sh
curl -sk https://localhost:8491/peer-hashes
```

## Debug Inside The Container

When doing local [development](development.md) using `make image`, the built
`kynomesh` image is based on `alpine` (the Makefile's `DEV_BASE_IMAGE`), which
allows you to exec into the container for debugging:

```sh
kubectl exec -it <pod-name> -c <container-name> -- sh
```

This is not possible with official released images, which are built
`FROM scratch`.

This is unrelated to the **agent** container, whose image is supplied by you and
may or may not include a shell.
