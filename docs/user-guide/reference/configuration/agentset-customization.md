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

Currently the shared template applies to the **broker container** of every
agent, via `brokerTemplate`. To customize the agent pods themselves (labels,
annotations, scheduling, etc.), set those fields on the individual agent under
`spec.agents` — see [Labels And Annotations](labels-and-annotations.md).

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
      # Broker container of every agent
      brokerTemplate:
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
      tolerations:
        - key: "my-example-key"
          operator: "Exists"
          effect: "NoSchedule"
      securityContext: {}
      imagePullSecrets:
        - name: regcred
      priorityClassName: my-priority-class-name
      priority: 50
      serviceAccountName: my-service-account
      runtimeClassName: my-runtime-class
      automountServiceAccountToken: false
      dnsPolicy: ClusterFirst
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

A `brokerTemplate` set on an individual agent (`spec.agents[].brokerTemplate`)
overrides the shared template for that agent.

## See Also

- [Labels And Annotations](labels-and-annotations.md) — set custom labels and
  annotations on agent pods.
- [AgentSet](../../../core-concepts/agentset.md) — where `templates` and the
  `agents` list live.
- [AgentDeploy](../../../core-concepts/agentdeploy.md) — the unit these pods
  belong to.
