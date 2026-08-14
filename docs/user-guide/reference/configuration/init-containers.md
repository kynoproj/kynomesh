# Init Containers

[Init Containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
can be added to an agent's pod via `.spec.agents[*].initContainers`.

Kynomesh already runs two built-in init containers on every agent pod:
`init-runtime` (prepares the shared runtime directory) runs first, then any
containers listed in `initContainers` run, and `agent` (the user's agent
container, run as a
[native sidecar](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)
via `restartPolicy: Always`) runs last — since it never completes, anything
meant to prepare state for it must run before it starts.

The following example adds an init container that seeds a volume before the
agent starts:

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
      initContainers:
        - name: my-init
          image: busybox:latest
          command: ["/bin/sh", "-c", 'echo "my-init is running!" && sleep 5']
```

The following example uses an init container together with a
[volume](https://kubernetes.io/docs/concepts/storage/volumes/) to provide the
agent container files on startup:

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
        - name: my-agent-data
          emptyDir: {}
      initContainers:
        - name: my-init
          image: amazon/aws-cli:latest
          command:
            [
              "/bin/sh",
              "-c",
              "aws s3 sync s3://path/to/my-s3-data /path/to/my-init-data",
            ]
          volumeMounts:
            - mountPath: /path/to/my-init-data
              name: my-agent-data
      container:
        image: my-agent:latest
        volumeMounts:
          - mountPath: /path/to/my-data
            name: my-agent-data
```

## See Also

- [Sidecar Containers](sidecar-containers.md) — add extra long-running
  containers to an agent pod.
- [Container Resources](container-resources.md) — set `resources` on any
  container.
- [Environment Variables](environment-variables.md) — env vars injected into
  every container.
