# Quick Start

This guide will walk you through the following steps:

1. [Installing Kynomesh](#installing-numaflow)
2. [Creating and running a simple Kynomesh AgentSet](#creating-a-simple-pipeline)

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

## Installing Kynomesh

After completing the prerequisites, follow these steps to install Kynomesh:

### Create a namespace for Kynomesh

```shell
kubectl create ns kynomesh-system
```

### Install Kynomesh components

```shell
kubectl apply -n kynomesh-system -f https://raw.githubusercontent.com/kynoproj/kynomesh/main/config/install.yaml
```

Once this is done, Kynomesh will be ready for use.

## Creating the very first AgentSet

In this section, we will create an `AgentSet` that includes a source vertex to
generate messages, a processing vertex that echoes the messages, and a sink
vertex to log the messages.
