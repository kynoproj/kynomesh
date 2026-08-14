# Sidecar Containers

Additional
[sidecar](https://kubernetes.io/docs/concepts/workloads/pods/#how-pods-manage-multiple-containers)
containers can be added to an agent's pod via `.spec.agents[*].sidecars`. They
run alongside the `broker` container for the lifetime of the pod. Be aware
that these sidecar containers start after the `agent` container has started
(`agent` runs as a native init-container sidecar, ahead of the pod's main
containers) — so don't rely on a `sidecars` entry being up before `agent`
starts.

The following example shows how to add a sidecar container to an agent:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  agents:
    - name: my-agent
      container:
        image: my-agent:latest
      sidecars:
        - name: my-sidecar
          image: busybox:latest
          command:
            [
              "/bin/sh",
              "-c",
              'echo "my-sidecar is running!" && tail -f /dev/null',
            ]
```

There are various use-cases for sidecars. One possible use-case is an agent that
needs functionality from a library written in a different language. The
library's functionality could be made available through gRPC over a Unix Domain
Socket. The following example shows how that could be accomplished using a
shared volume.

It is the sidecar owner's responsibility to come up with a protocol that can be
used with the agent. It could be a volume, gRPC, TCP, HTTP 1.x, etc. Since
`agent` starts before the `my-sidecar` container is guaranteed to be listening
(see the start-order note above), the client side needs to retry the
connection rather than dial once and fail.

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: my-agent
  agents:
    - name: my-agent
      volumes:
        - name: my-agent-socket
          emptyDir:
            medium: Memory
      sidecars:
        - name: my-sidecar
          image: alpine:latest
          command:
            [
              "/bin/sh",
              "-c",
              "apk add socat && socat
              UNIX-LISTEN:/path/to/my-sidecar-mount-path/my.sock - && tail -f
              /dev/null",
            ]
          volumeMounts:
            - mountPath: /path/to/my-sidecar-mount-path
              name: my-agent-socket
      container:
        image: alpine:latest
        command:
          [
            "/bin/sh",
            "-c",
            'apk add socat && echo "hello" | socat -T5
            UNIX-CONNECT:/path/to/my-agent-mount-path/my.sock,retry=30,interval=1,forever
            - && tail -f /dev/null',
          ]
        volumeMounts:
          - mountPath: /path/to/my-agent-mount-path
            name: my-agent-socket
```

Sidecars receive the same Kynomesh-injected
[environment variables](environment-variables.md) as every other container in
the pod, and `sidecars[*].resources` can be set like any other
[container resources](container-resources.md).

## See Also

- [Init Containers](init-containers.md) — run one-shot setup before the agent
  starts.
- [Container Resources](container-resources.md) — set `resources` on any
  container.
- [Environment Variables](environment-variables.md) — env vars injected into
  every container.
