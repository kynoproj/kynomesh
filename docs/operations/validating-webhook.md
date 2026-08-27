# Validating Admission Webhook

This validating webhook rejects faulty `AgentSet` specs such as bad `pattern`,
duplicate agent names, a reserved name collision, a malformed external-agent
URL, etc. Instead of the object being persisted and only failing later during
reconciliation, the API server rejects it immediately and `kubectl apply`
returns the validation error right away.

## Installation

To install the validating webhook, run the following command line:

```shell
kubectl apply -n kynomesh-system -f https://raw.githubusercontent.com/kynoproj/kynomesh/stable/config/validating-webhook-install.yaml
```

## Examples

Given an `AgentSet` with an unsupported pattern:

```yaml
apiVersion: kynomesh.kyno.sh/v1alpha1
kind: AgentSet
metadata:
  name: research-assistant
spec:
  pattern: Freeform # not a valid pattern
  entry: researcher
  agents:
    - name: researcher
```

```shell
The AgentSet "research-assistant" is invalid:
* spec.pattern: Unsupported value: "Freeform": supported values: "Supervisor", "Handoff", "Sequential"
* spec.agents[0].container: Required value
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
