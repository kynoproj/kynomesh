# Quick Start

This guide will walk you through the following steps:

1. [Installing Kynomesh](#install-kynomesh)
2. [Creating and running a simple Kynomesh AgentSet](#create-the-very-first-agentset)

## Before You Begin: Prerequisites

To get started with Kynomesh, make sure you have the following tools and setups
ready:

### Container Runtime

You need a container runtime to run container images. Choose one of the
following options:

- [Docker Desktop](https://docs.docker.com/get-docker/)
- [Podman](https://podman.io/)

### Local Kubernetes Cluster

Set up a local Kubernetes cluster using one of these tools:

- [Docker Desktop Kubernetes](https://docs.docker.com/desktop/kubernetes/)
- [k3d](https://k3d.io/)
- [kind](https://kind.sigs.k8s.io/)
- [minikube](https://minikube.sigs.k8s.io/docs/start/)

### Kubernetes CLI (`kubectl`)

Install `kubectl` to manage your Kubernetes cluster. Follow the
[official guide](https://kubernetes.io/docs/tasks/tools/#kubectl) for
installation instructions. If you're unfamiliar with `kubectl`, refer to the
[kubectl Quick Reference Page](https://kubernetes.io/docs/reference/kubectl/quick-reference/)
for a list of commonly used commands.

Once these prerequisites are in place, you're ready to proceed with installing
and using Kynomesh.

### A2A Protocol CLI (`a2acli`)

`a2acli` is a command-line client for the [A2A](https://a2a-protocol.org/)
(Agent-to-Agent) Protocol. Run the following command to install it.

```shell
curl -fsSL https://raw.githubusercontent.com/kynoproj/a2acli/main/install.sh | bash
```

## Install Kynomesh

After completing the prerequisites, follow this
[guide](./operations/installation.md) to install the latest stable Kynomesh.

Or follow the next steps to install it from the `main` branch.

```shell
# Create a namespace for Kynomesh
kubectl create ns kynomesh-system

# Install Kynomesh components
kubectl apply -n kynomesh-system -f https://raw.githubusercontent.com/kynoproj/kynomesh/main/config/install.yaml
```

Once this is done, Kynomesh will be ready for use.

## Create the very first AgentSet

We'll use the
[`research-assistant`](https://github.com/kynoproj/kynomesh-go/tree/main/examples/research-assistant)
example: a two-agent set where a `coordinator` agent delegates each incoming
question to a `searcher` worker and returns the combined answer. It's the
smallest example that shows one agent calling another.

### Deploy the AgentSet manifest

Apply the manifest directly from the example repository:

```shell
kubectl apply -f https://raw.githubusercontent.com/kynoproj/kynomesh-go/refs/heads/main/examples/research-assistant/manifests/agentset.yaml
```

### Verify the AgentSet deployment

To view the `AgentSet` you just created, use the following command:

```shell
kubectl get agentset # Or the short name "as"
```

You should see an output similar to this, where `AGE` indicates the time elapsed
since the `AgentSet` was created:

```
NAME                 PHASE     AGENTS   AGE   MESSAGE
research-assistant   Running            83s
```

You can also list the `AgentDeploy` components orchestrated by the `AgentSet`.
Each one represents a single agent deployment:

```
kubectl get agentdeploy # Or the short name "ad"

NAME                             PHASE     DESIRED   CURRENT   READY   AGE     REASON   MESSAGE
research-assistant-coordinator   Running   1         1         1       2m27s
research-assistant-searcher      Running   1         1         1       2m27s
```

Next, inspect the underlying pods. Note that the pod names in your environment
may differ from the example below:

```
kubectl get pods

NAME                                     READY   STATUS    RESTARTS   AGE
research-assistant-coordinator-0-epzoc   2/2     Running   0          5m16s
research-assistant-searcher-0-jbblr      2/2     Running   0          5m16s
```

### Talk to it

The AgentSet's front door is the auto-created `research-assistant-ingress`
service. Port-forward it to your local machine:

```shell
kubectl port-forward svc/research-assistant-ingress 8490
```

In another terminal, send a question with `a2acli`:

```shell
a2acli -k -u https://localhost:8490 --override-host=localhost:8490 send 'tell me about kynomesh'
```

The `coordinator` forwards your question to the `searcher`, wraps the result,
and returns something like:

```json
{
  "messageId": "019ea01f-96ea-7590-963d-7fc6b05e569e",
  "parts": [
    {
      "text": "coordinator: handled \"tell me about kynomesh\" via \"searcher\"\n---\nsearcher: 1 hit(s)\n- kynomesh: a Kubernetes-native orchestrator for AI agents."
    }
  ],
  "role": "ROLE_AGENT"
}
```

### Clean up

To delete the `AgentSet`, run:

```shell
kubectl delete -f https://raw.githubusercontent.com/kynoproj/kynomesh-go/refs/heads/main/examples/research-assistant/manifests/agentset.yaml
```

### Next steps

- Read the full example, including the Go source, at
  [kynomesh-go/examples/research-assistant](https://github.com/kynoproj/kynomesh-go/tree/main/examples/research-assistant).
- Explore other patterns (`Handoff`, `Sequential`) in
  [AgentSet](./core-concepts/agentset.md). The agent code does not change — only
  the AgentSet `pattern` does.
