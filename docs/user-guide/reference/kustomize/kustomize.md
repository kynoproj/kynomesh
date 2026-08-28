# Kustomize Integration

## Transformers

Kustomize
[Transformer Configurations](https://github.com/kubernetes-sigs/kustomize/tree/master/examples/transformerconfigs)
can be used to do lots of powerful operations such as ConfigMap and Secret
generations, applying common labels and annotations, updating image names and
tags. To use these features with Kynomesh CRD objects, download
[kynomesh-transformer-config.yaml](kynomesh-transformer-config.yaml) into your
kustomize directory, and add it to `configurations` section.

```yaml
kind: Kustomization
apiVersion: kustomize.config.k8s.io/v1beta1

configurations:
  - kynomesh-transformer-config.yaml
  # Or reference the remote configuration directly.
  # - https://raw.githubusercontent.com/kynoproj/kynomesh/main/docs/user-guide/reference/kustomize/kynomesh-transformer-config.yaml
```

Here is an
[example](https://github.com/kynoproj/kynomesh/tree/main/docs/user-guide/reference/kustomize/examples/transformer)
to use transformers with an AgentSet.

## Patch

Starting from version 4.5.5, kustomize can use Kubernetes
[OpenAPI schema](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/openapi/)
to provide merge key and patch strategy information. To use that with Kynomesh
CRD objects, download
[schema.json](https://raw.githubusercontent.com/kynoproj/kynomesh/main/api/json-schema/schema.json)
into your kustomize directory, and add it to `openapi` section.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

openapi:
  path: schema.json
  # Or reference the remote configuration directly.
  # path: https://raw.githubusercontent.com/kynoproj/kynomesh/main/api/json-schema/schema.json
```

For example, given the following AgentSet spec:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: my-agentset
spec:
  pattern: Supervisor
  entry: research
  agents:
    - name: researcher
      container:
        image: my-researcher-image:v0.1.2
    - name: worker
      container:
        image: my-worker-image:v1.0.0
```

You can update the `agents` spec via a patch in a kustomize file.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - my-agentset.yaml

openapi:
  path: https://raw.githubusercontent.com/kynoproj/kynomesh/main/api/json-schema/schema.json

patches:
  - patch: |-
      apiVersion: kynomesh.kyno.sh/v1alpha1
      kind: AgentSet
      metadata:
        name: my-agentset
      spec:
        agents:
          - name: worker
            container:
              imagePullPolicy: Never
```

See the full example
[here](https://github.com/kynoproj/kynomesh/tree/main/docs/user-guide/reference/kustomize/examples/patch).
