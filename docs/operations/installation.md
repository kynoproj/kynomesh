# Installation

Kynomesh can be installed in different scopes with different approaches.

## Cluster Scope

A cluster scope installation watches [AgentSet](../core-concepts/agentset.md) in
all the namespaces in the cluster.

Run following command line to install latest `stable` Kynomesh in cluster scope.

```shell
kubectl apply -n kynomesh-system -f https://raw.githubusercontent.com/kynoproj/kynomesh/stable/config/install.yaml
```

If you use [kustomize](https://kustomize.io/), use `kustomization.yaml` below.

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - https://github.com/kynoproj/kynomesh/config/cluster-install?ref=stable # Or specify a version

namespace: kynomesh-system
```

## Namespace Scope

A namespace scoped installation only watches
[AgentSet](../core-concepts/agentset.md) in the namespace it is installed
(typically `kynomesh-system`).

Configure the ConfigMap `kynomesh-cmd-params-config` to achieve namespace scoped
installation.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-cmd-params-config
data:
  # Whether to run in namespaced scope, defaults to false.
  namespaced: "true"
```

Another approach to do namespace scoped installation is to add an argument
`--namespaced` to the `kynomesh-controller`. This approach takes precedence over
the ConfigMap approach.

```
      - args:
        - --namespaced
```

## Managed Namespace Scope

A managed namespace installation watches
[AgentSet](../core-concepts/agentset.md) in a specific namespace.

To do managed namespace installation, configure the ConfigMap
`kynomesh-cmd-params-config` as following.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-cmd-params-config
data:
  # Whether to run the controller in namespaced scope, defaults to false.
  namespaced: "true"
  # The namespace that the controller watches when "namespaced" is true, defaults to the installation namespace.
  managed.namespace: my-namespace
```

Similarly, another approach is to add `--managed-namespace` and the specific
namespace to the `kynomesh-controller` deployment arguments. This approach takes
precedence over the ConfigMap approach.

```
      - args:
        - --namespaced
        - --managed-namespace
        - my-namespace
```

## High Availability

By default, the Kynomesh controller is installed with `Active-Passive` HA
strategy enabled, which means you can run the controller with multiple replicas
(defaults to 1 in the manifests).

There are some parameters can be tuned for the leader election mechanism of HA.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-cmd-params-config
data:
  ### The duration that non-leader candidates will wait to force acquire leadership.
  #   This is measured against time of last observed ack. Default is 15 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  controller.leader.election.lease.duration: 15s
  #
  ### The duration that the acting controlplane will retry refreshing leadership before giving up.
  #   Default is 10 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  controller.leader.election.lease.renew.deadline: 10s
  ### The duration the LeaderElector clients should wait between tries of actions, which means every
  #   this period of time, it tries to renew the lease. Default is 2 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  controller.leader.election.lease.renew.period: 2s
```

These parameters are useful when you want to tune the frequency of leader
election renewal calls to K8s API server, which are usually configured at a high
priority level of
[API Priority and Fairness](https://kubernetes.io/docs/concepts/cluster-administration/flow-control/).

To turn off HA, configure the ConfigMap `kynomesh-cmd-params-config` as
following.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-cmd-params-config
data:
  # Whether to disable leader election for the controller, defaults to false
  controller.leader.election.disabled: "true"
```

If HA is turned off, the controller deployment should not run with multiple
replicas.
