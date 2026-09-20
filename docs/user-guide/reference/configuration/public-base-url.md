# Public Base URL

By default, an agent's broker advertises its in-cluster address on the
`AgentCard` it serves - reachable from other pods in the cluster, but not from
outside it. `publicBaseURL` overrides that address so external callers, reaching
the agent through an ingress or gateway, get a URL they can actually use.

Set it per agent:

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
      publicBaseURL: "https://my-agent.example.com"
```

- `publicBaseURL`: the externally reachable base URL of this agent's broker.
  Unset by default, in which case the broker advertises its in-cluster address.
  There is no AgentSet-level default - set it on each agent that needs one.

## How It Works

The broker rewrites every transport URL on the `AgentCard.SupportedInterfaces`
it serves, deriving each one from `publicBaseURL`:

| Transport | Advertised URL                                            |
| --------- | --------------------------------------------------------- |
| JSON-RPC  | `publicBaseURL` + `/a2a/jsonrpc`                          |
| REST      | `publicBaseURL` + `/a2a/rest`                             |
| gRPC      | `publicBaseURL`'s host and port only (no scheme, no path) |

A trailing slash on `publicBaseURL` is stripped before the path is appended. For
gRPC, the broker reduces the URL to the bare `host:port` form gRPC clients
expect: an explicit port is kept as-is, `https://` implies `:443`, `http://`
implies `:80`, and a value with no scheme (already `host:port`) is used
unchanged.

For example, `publicBaseURL: "https://my-agent.example.com"` advertises:

- JSON-RPC: `https://my-agent.example.com/a2a/jsonrpc`
- REST: `https://my-agent.example.com/a2a/rest`
- gRPC: `my-agent.example.com:443`

Only the transports your agent's own `AgentCard` actually declares are
advertised - see
[Exposing Transports](../../sdks/overview.md#exposing-transports) in the SDK
guide.

## See Also

- [SDKs](../../sdks/overview.md) - writing an agent and declaring its supported
  transports.
- [External Agents](external-agents.md) - referencing agents outside the
  cluster, the inverse direction of this setting.
