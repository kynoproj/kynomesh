# Configuring GPU

GPU resources can be configured on an agent's container, its `brokerContainer`,
or the per-AgentSet daemon's container.

## Prerequisites

Your cluster must support GPU scheduling and have the appropriate device
plugin installed. See the
[Kubernetes device plugins documentation](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/)
and [NVIDIA device plugin guide](https://github.com/NVIDIA/k8s-device-plugin)
for details.

## Specifying GPU Resource Requests and Limits

Request a GPU by setting the `nvidia.com/gpu` resource in the `limits` field
under the container's `resources`:

```yaml
resources:
  limits:
    nvidia.com/gpu: 1
```

> **Important:** for GPUs, Kubernetes requires that `requests` and `limits`
> be the same (or that you specify only `limits`).

### Example: Agent Requesting A GPU (With Annotations And Node Selector)

Node selector, tolerations, and annotations are pod-level fields set directly
on the agent — see
[AgentSet Customization](configuration/agentset-customization.md) and
[Labels And Annotations](configuration/labels-and-annotations.md).

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
      metadata:
        annotations:
          mycompany.com/gpu-enabled: "true" # example annotation, use only if required by your cluster
      nodeSelector:
        nvidia.com/gpu.present: "true" # replace with your cluster's GPU node label
      container:
        image: my-ml-image:latest
        resources:
          limits:
            nvidia.com/gpu: 1
```

Adjust the `nodeSelector` and annotations as required by your cluster setup.

## Dynamic Resource Allocation (Advanced)

For advanced GPU scheduling using Dynamic Resource Allocation (DRA), see
[Dynamic Resource Allocation](configuration/dra.md).

## Troubleshooting

If your agent is not using GPU resources as expected:

- **Pod Pending:** check for available GPU nodes and device plugin status.
- **Pod does not detect GPU:** ensure your container image includes the
  necessary GPU drivers and libraries (e.g. CUDA).
- **Still having issues?** consult your cluster documentation or
  administrator.

## See Also

- [Container Resources](configuration/container-resources.md)
- [Dynamic Resource Allocation](configuration/dra.md)
- [Kubernetes: Managing Resources for Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
