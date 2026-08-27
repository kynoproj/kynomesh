# Validating Admission Webhook

This validating webhook rejects faulty `AgentSet` specs such as duplicate agent
names, a reserved name collision, a malformed external-agent URL, etc. — the
cross-field rules the CRD's OpenAPI schema can't express on its own. Instead of
the object being persisted and only failing later during reconciliation, the API
server rejects it immediately and `kubectl apply` returns the validation error
right away.

## Installation

To install the validating webhook, run the following command line:

```shell
kubectl apply -n kynomesh-system -f https://raw.githubusercontent.com/kynoproj/kynomesh/stable/config/validating-webhook-install.yaml
```

## Examples

Note that some fields, such as `spec.pattern`'s allowed values and
`spec.agents[].container` being required, are already enforced by the CRD's
OpenAPI schema — the API server rejects those before any admission webhook runs,
with a `"... is invalid"` style error. The validating webhook catches everything
the schema can't express.

For example, given an `AgentSet` with a duplicate agent name:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-assistant
spec:
  pattern: Supervisor
  entry: researcher
  agents:
    - name: researcher
      container:
        image: quay.io/kynoproj/research-assistant:latest
    - name: researcher # duplicate
      container:
        image: quay.io/kynoproj/research-assistant:latest
```

```shell
Error from server (BadRequest): error when creating "agentset.yaml": admission webhook "webhook.kynomesh.kyno.sh" denied the request: duplicate agent name "researcher"
```

Other validations include:

1. `spec.agents` must contain at least one agent
2. agent and external-agent names must be valid DNS-1035 labels, must not
   collide with each other, and must not collide with reserved names (`entry`'s
   own service suffix, the daemon service suffix)
3. `spec.entry` must name an agent in `spec.agents`, and must not be an external
   agent
4. external agent URLs must be absolute (scheme + host)
5. `Handoff` and `Sequential` patterns require at least 2 agents
6. `Sequential` requires `spec.entry` to be `spec.agents[0]` and allows at most
   one external agent, as the final hop
7. `agents[].scale.targetSaturationPercentage` must be in `[1, 100]`
8. etc.
