# AgentSet Customization

There is an optional `.spec.templates` field in the `AgentSet` resource which
may be used to customize the Kubernetes resources owned by the AgentSet.

Per-agent customization is described separately in more detail (i.e.
[Environment Variables](environment-variables.md),
[Container Resources](container-resources.md), etc.).

## Agents

Use `.spec.templates.agent` to set defaults shared by every agent in the
AgentSet, and override them per-agent under `spec.agents`.

The `.spec.templates.agent` field and all fields directly under it are optional.

`.spec.templates.agent` sets pod-level defaults (`nodeSelector`, `tolerations`,
`affinity`, `serviceAccountName`, etc.) shared by every agent pod, plus a
`brokerContainer` template applied to every agent's **broker container**. Any of
these fields set directly on an individual agent under `spec.agents` override
the shared default for that agent.

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: coordinator
  templates:
    agent:
      # Pod-level defaults shared by every agent's pod
      metadata:
        labels:
          my-label-name: shared-label-value
        annotations:
          my-annotation-name: shared-annotation-value
      nodeSelector:
        my-node-label-name: my-node-label-value
      serviceAccountName: my-service-account
      terminationGracePeriodSeconds: 180
      # Broker container of every agent
      brokerContainer:
        env:
          - name: MY_ENV_NAME
            value: my-env-value
        resources:
          limits:
            memory: 500Mi
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
      # Pod-level configuration for this agent's pods
      metadata:
        labels:
          my-label-name: my-label-value
        annotations:
          my-annotation-name: my-annotation-value
      nodeSelector:
        my-node-label-name: my-node-label-value
      imagePullSecrets:
        - name: regcred
      terminationGracePeriodSeconds: 120
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: kynomesh.kyno.sh/agentset-name
                    operator: In
                    values:
                      - my-agentset
              topologyKey: kubernetes.io/hostname
```

## Daemon

Use `.spec.templates.daemon` to customize the per-AgentSet daemon Deployment —
the singleton pod that scrapes agent metrics and serves them to the autoscaler.
All fields under `.spec.templates.daemon` are optional.

`container` customizes the daemon's single container (resources, env, security
context). The remaining fields (`nodeSelector`, `tolerations`, `affinity`,
`serviceAccountName`, etc.) customize the daemon pod itself, the same pod-level
fields available under `spec.agents[]`.

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: coordinator
  templates:
    daemon:
      container:
        env:
          - name: MY_ENV_NAME
            value: my-env-value
        resources:
          limits:
            memory: 200Mi
      nodeSelector:
        my-node-label-name: my-node-label-value
      tolerations:
        - key: "my-example-key"
          operator: "Exists"
          effect: "NoSchedule"
      serviceAccountName: my-service-account
  agents:
    - name: coordinator
      container:
        image: example/coordinator:latest
```

Changing `.spec.templates.daemon` recreates the daemon Deployment's pod (the
daemon is a Recreate-strategy singleton, not a rolling update).

## See Also

- [Labels And Annotations](labels-and-annotations.md) — set custom labels and
  annotations on agent pods.
- [AgentSet](../../../core-concepts/agentset.md) — where `templates` and the
  `agents` list live.
- [AgentDeploy](../../../core-concepts/agentdeploy.md) — the unit these pods
  belong to.
