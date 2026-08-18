# Quota Scopes Support in pac-quota-controller

## Overview

`ClusterResourceQuota` supports the standard Kubernetes ResourceQuota `scopes` and `scopeSelector` fields. Scopes restrict which **pods** count toward the quota; a pod is tracked only if it matches **all** declared scopes. Scopes never filter non-pod resources (services, PVCs, object counts), and quotas combining scopes with non-pod resources in `hard` are rejected at admission.

## Supported Scopes

| Scope | Matches pods where |
|---|---|
| `Terminating` | `spec.activeDeadlineSeconds >= 0` |
| `NotTerminating` | `spec.activeDeadlineSeconds` is nil |
| `BestEffort` | computed QoS class is BestEffort (no cpu/memory requests or limits) |
| `NotBestEffort` | computed QoS class is Burstable or Guaranteed |
| `PriorityClass` | a `spec.priorityClassName` is set (or matches the selector values) |
| `CrossNamespacePodAffinity` | any pod (anti)affinity term sets `namespaces` or `namespaceSelector` |

Entries in `scopes` behave as `Exists` requirements. `scopeSelector.matchExpressions` supports operators per scope: `PriorityClass` accepts `In`, `NotIn`, `Exists`, `DoesNotExist`; all other scopes accept only `Exists`. All requirements from both fields are ANDed.

## How it Works

- **Controller:** during aggregation the pod list of each selected namespace is filtered through the scope requirements before usage is computed, so `status.total.used` reflects only in-scope, non-terminal pods.
- **Pod webhook:** at admission, an out-of-scope pod is allowed without charging the quota — even when the quota is full. In-scope pods are charged against `status.total.used` as usual.
- **QoS classification** is always computed from the pod spec (never `status.qosClass`), so admission and reconciliation agree.

## Validation Rules

Rejected at CRQ admission (mirroring upstream `ResourceQuota` validation):

- unknown scope names or duplicate scopes
- `BestEffort` + `NotBestEffort`, or `Terminating` + `NotTerminating`, within `scopes` alone or within `scopeSelector` alone (the `scopes`-only case is also enforced by CRD CEL rules, which hold even if the webhook is unavailable)
- unsupported operators (only `PriorityClass` accepts more than `Exists`), missing/extra `values`
- non-pod resources in `hard` when any scope is set: allowed keys are `pods` plus compute resources (`requests.`/`limits.` cpu and memory only — ephemeral-storage is not scope-eligible, matching upstream). `BestEffort`-scoped quotas may only limit `pods` (best-effort pods have no requests/limits by definition)
- extended resources (`requests.<domain>/<resource>`, `hugepages-*`) bypass the restriction, exactly as upstream, and are scope-filtered like other pod resources

Contradictory combinations across `scopes` and `scopeSelector` (e.g. `scopes: [BestEffort]` with a `NotBestEffort` expression) are legal but produce an admission **warning** — such a quota matches no pods.

If an invalid scope spec reaches storage anyway (the webhooks are fail-open), the controller fails reconciliation with an `InvalidScopeSelector` event and the pod webhook allows pods for that CRQ; non-pod resources in `hard` are counted unfiltered.

## Usage in ClusterResourceQuota

```yaml
# Cap CI/batch pods (those with an active deadline)
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: ci-jobs-quota
spec:
  namespaceSelector:
    matchLabels:
      team: ci
  scopes: [Terminating]
  hard:
    pods: "20"
    requests.cpu: "10"
```

```yaml
# Limit best-effort pods (only 'pods' is allowed under BestEffort)
spec:
  namespaceSelector:
    matchLabels:
      team: web
  scopes: [BestEffort]
  hard:
    pods: "5"
```

```yaml
# Quota only for high-priority workloads
spec:
  namespaceSelector:
    matchLabels:
      team: web
  scopeSelector:
    matchExpressions:
      - scopeName: PriorityClass
        operator: In
        values: [high, critical]
  hard:
    pods: "10"
    requests.cpu: "8"
```

See `examples/scoped-quota-example.yaml` for a complete example.

## Status Reporting

`status.total.used` and the per-namespace breakdown count only in-scope pods. An out-of-scope pod is invisible to the quota: it is neither counted nor denied.

## Key Differences and Limitations

- A namespace can be owned by only **one** CRQ, so a BestEffort quota and a NotBestEffort quota cannot both cover the same namespace (stock ResourceQuota allows multiple scoped quotas per namespace).
- Quota is enforced at admission time. A pod that enters a scope mid-life (e.g. `activeDeadlineSeconds` patched onto a running pod) is picked up by the next reconcile and can push `used` above `hard` — the same behavior as stock ResourceQuota.
- **Upgrade note:** scopes stored before this feature existed were silently ignored; after upgrading they are enforced. `used` may drop (out-of-scope pods stop counting) and previously-stored scoped CRQs with non-pod `hard` keys will be rejected on their next update.

## Testing

```sh
# Unit tests (matcher, QoS, validation, resource classification)
go test ./pkg/kubernetes/pod/ ./pkg/kubernetes/quota/ ./pkg/kubernetes/usage/

# E2E tests
make test-e2e  # includes test/e2e/scoped_quota_test.go
```
