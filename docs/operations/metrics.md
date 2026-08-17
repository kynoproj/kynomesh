# Metrics

Kynomesh exposes Prometheus metrics from three components: the **broker**
sidecar in every agent pod, the per-AgentSet **daemon**, and the
**controller-manager** (the agentdeploy/agentset reconcilers, including the
autoscaler).

| Component          | Port                                    | Path       | TLS              |
| ------------------ | --------------------------------------- | ---------- | ---------------- |
| broker             | `8491` (`AgentBrokerIntrospectionPort`) | `/metrics` | yes, self-signed |
| daemon             | `9433` (`DaemonMetricsPort`)            | `/metrics` | yes, self-signed |
| controller-manager | `9090`                                  | `/metrics` | no               |

The broker's and daemon's metrics endpoints are TLS-only, using a certificate
generated fresh per process — Prometheus scrape configs need `scheme: https` and
`insecure_skip_verify: true` (there's no shared CA to validate against). The
controller-manager's endpoint is plain HTTP, served by controller-runtime's
default metrics server.

The controller-manager's endpoint also carries controller-runtime's own built-in
metrics (reconcile counts/durations, workqueue depth, etc.) in addition to
Kynomesh's custom autoscaler metrics below.

## Broker Metrics

Emitted by the broker sidecar in every agent pod. All are labeled by
`transport`, one of `jsonrpc`, `rest`, `grpc`, or `passthrough`.

| Metric                            | Type              | Description                                                                                                                                                                                                                                                                |
| --------------------------------- | ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `broker_inflight_requests`        | Gauge             | Requests the broker is currently proxying. A streaming call holds its slot for the stream's whole lifetime, not per message.                                                                                                                                               |
| `broker_requests_total`           | Counter           | Requests handled, incremented once per HTTP request completion or gRPC stream close.                                                                                                                                                                                       |
| `broker_rejected_total`           | Counter           | Requests rejected at admission because the max in-flight cap was reached (HTTP 429 / gRPC `RESOURCE_EXHAUSTED`).                                                                                                                                                           |
| `broker_stream_messages_total`    | Counter           | Stream messages observed on the wire — SSE events for REST/passthrough, server→client frames for gRPC. Stays 0 for non-streaming responses.                                                                                                                                |
| `broker_request_duration_seconds` | Histogram         | Wall-clock duration of broker-handled requests, observed on completion.                                                                                                                                                                                                    |
| `broker_errors_total`             | Counter           | Requests that completed with an error, additionally labeled by `code`: HTTP status class (`4xx`, `5xx`) or gRPC status code name (e.g. `Unavailable`, `Internal`). Never incremented for successful responses or admission rejections (those are `broker_rejected_total`). |
| `broker_agent_server_info`        | Gauge, always `1` | Static info series labeled `protocol`, `language`, `version`, sourced from the agent's server-info file. Only present when the agent published one.                                                                                                                        |

## Daemon Metrics

Emitted by the per-AgentSet daemon, all labeled by `agentdeploy`.

| Metric                            | Type    | Description                                                               |
| --------------------------------- | ------- | ------------------------------------------------------------------------- |
| `daemon_scrape_success_total`     | Counter | Pod `/metrics` scrapes that completed without error.                      |
| `daemon_scrape_failures_total`    | Counter | Pod `/metrics` scrapes that failed (HTTP error, timeout, or parse error). |
| `daemon_discovery_failures_total` | Counter | Headless-DNS lookup failures while discovering an AgentDeploy's pods.     |
| `daemon_pods_observed`            | Gauge   | Ready pods discovered for an AgentDeploy on the last scrape tick.         |

## Autoscaler Metrics

Emitted by the AgentDeploy autoscaler, part of the controller-manager process.
All are labeled by `namespace`, `agentSet`, `agentDeploy`.

| Metric                          | Type                                                       | Description                                                                  |
| ------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `autoscaler_knee_per_replica`   | Gauge                                                      | Learned per-replica saturation knee (in-flight requests) for an AgentDeploy. |
| `autoscaler_confidence`         | Gauge, `0`–`1`                                             | Confidence in the learned knee.                                              |
| `autoscaler_desired_replicas`   | Gauge                                                      | Replica count the autoscaler last computed.                                  |
| `autoscaler_current_replicas`   | Gauge                                                      | Replica count the autoscaler last scaled from (observed running replicas).   |
| `autoscaler_samples_total`      | Counter                                                    | Load samples recorded.                                                       |
| `autoscaler_scale_events_total` | Counter, labeled additionally by `direction` (`up`/`down`) | Replica scale operations applied.                                            |

## Prometheus Operator Integration

Example `PodMonitor` for agent pods (broker metrics) — matched by the common
labels every agent pod carries:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: kynomesh-broker
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: agent
      app.kubernetes.io/managed-by: agentdeploy-controller
      app.kubernetes.io/part-of: kynomesh
  podMetricsEndpoints:
    - port: introspect
      path: /metrics
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
```

Or equivalent `ServiceMonitor` for the broker:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kynomesh-broker
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: agent
      app.kubernetes.io/managed-by: agentdeploy-controller
      app.kubernetes.io/part-of: kynomesh
      kynomesh.kyno.sh/service-kind: headless
  endpoints:
    - port: introspect
      path: /metrics
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
```

Example `PodMonitor` for the per-AgentSet daemon:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: kynomesh-daemon
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: daemon
      app.kubernetes.io/managed-by: agentset-controller
      app.kubernetes.io/part-of: kynomesh
  podMetricsEndpoints:
    - port: metrics
      path: /metrics
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
```

Or equivalent `ServiceMonitor` for the daemon:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kynomesh-daemon
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: daemon
      app.kubernetes.io/managed-by: agentset-controller
      app.kubernetes.io/part-of: kynomesh
  endpoints:
    - port: metrics
      path: /metrics
      scheme: https
      tlsConfig:
        insecureSkipVerify: true
```

Example `PodMonitor` for the controller-manager (plain HTTP, no TLS, served by
controller-runtime's metrics server):

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: kynomesh-controller-manager
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: controller-manager
  podMetricsEndpoints:
    - port: metrics
      path: /metrics
```
