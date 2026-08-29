# Controller Configuration

There are 2 ConfigMaps are used for controller-wide settings.

- [kynomesh-controller-config.yaml](https://github.com/kynoproj/kynomesh/blob/main/config/base/controller-manager/kynomesh-controller-config.yaml)
- [kynomesh-cmd-params-config.yaml](https://github.com/kynoproj/kynomesh/blob/main/config/base/shared-config/kynomesh-cmd-params-config.yaml)

## kynomesh-controller-config.yaml

The ConfigMap `kynomesh-controller-config` defined in
`kynomesh-controller-config.yaml` is mounted to the controller pod, all the
configuration is under the key `controller-config.yaml`, as a string in yaml
format:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-controller-config
data:
  controller-config.yaml: |+
    defaults:
      containerResources: |
        requests:
          memory: "128Mi"
          cpu: "100m"

```

### Default Controller Configuration

The key `defaults` is used to define default configuration for the controller.
For example, to set the default container resources (if they are not specified)
for all the Kynomesh managed containers such as `init-runtime`, `broker`, and
`daemon`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-controller-config
data:
  controller-config.yaml: |+
    defaults:
      containerResources: |
        limits:
          memory: "256Mi"
          cpu: "200m"
        requests:
          memory: "128Mi"
          cpu: "100m"

```

## kynomesh-cmd-params-config.yaml

The ConfigMap `kynomesh-cmd-params-config` defined in
`kynomesh-cmd-params-config.yaml` is read at the time the controller starts, the
values are passed in as environment variables. Making change to the values
requires the controller restart to take effect.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kynomesh-cmd-params-config
data:
  ### Whether to run the controller and the UX server in namespaced scope, defaults to false.
  # namespaced: "false"
  #
  ### The namespace that the controller watches when "namespaced" is true.
  # managed.namespace: kynomesh-system
  #
  ### Whether to disable leader election for the controller, defaults to false
  # controller.leader.election.disabled: "false"
  #
  ### The duration that non-leader candidates will wait to force acquire leadership.
  #   This is measured against time of last observed ack. Default is 15 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  # controller.leader.election.lease.duration: 15s
  #
  ### The duration that the acting controlplane will retry refreshing leadership before giving up.
  #   Default value is 10 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  # controller.leader.election.lease.renew.deadline: 10s
  #
  ### The duration the LeaderElector clients should wait between tries of actions, which means every
  #   this period of time, it tries to renew the lease. Default is 2 seconds.
  #   The configuration has to be: lease.duration > lease.renew.deadline > lease.renew.period
  # controller.leader.election.lease.renew.period: 2s
```
