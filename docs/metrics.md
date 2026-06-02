# Metrics Exposed by pac-quota-controller

This controller exposes Prometheus metrics at the `/metrics` endpoint. Below are the key metrics, their types, labels, and descriptions.

---

## Controller Metrics

### `pac_quota_controller_crq_usage`

- **Type:** Gauge
- **Labels:** `crq_name`, `namespace`, `resource`
- **Description:** Current usage of a resource for a ClusterResourceQuota in a namespace.

### `pac_quota_controller_crq_total_usage`

- **Type:** Gauge
- **Labels:** `crq_name`, `resource`, `namespace`, `namespaces`
- **Description:** Aggregated usage of a resource across all namespaces for a ClusterResourceQuota.
  - `namespace`: One of the selected namespaces (first alphabetically). Useful for AlertManager routing when routing is based on namespace.
  - `namespaces`: Comma-separated list of all selected namespaces for the CRQ.

---

## Webhook Metrics

### `pac_quota_controller_webhook_validation_total`

- **Type:** Counter
- **Labels:** `webhook`, `operation`, `namespace`
- **Description:** Total number of webhook validation requests.

### `pac_quota_controller_webhook_validation_duration_seconds`

- **Type:** Histogram
- **Labels:** `webhook`, `operation`, `namespace`
- **Description:** Duration of webhook validation requests.

### `pac_quota_controller_webhook_admission_decision_total`

- **Type:** Counter
- **Labels:** `webhook`, `operation`, `decision`, `namespace`
- **Description:** Total number of webhook admission decisions (allowed/denied).

> **Namespace label semantics**: For namespaced webhooks (Pod, PVC, Service,
> ResourceQuota) the value is the admitted object's namespace. For
> cluster-scoped webhooks (Namespace, ClusterResourceQuota) it is the admitted
> object's name, since those resources have no namespace of their own.
>
> **Cardinality note**: In large clusters (~1000 namespaces × 6 webhooks ×
> 2 operations × 2 decisions ≈ 24k series for
> `pac_quota_controller_webhook_admission_decision_total`). Prometheus handles
> this well, but operators should be aware when sizing storage and alerts.

---

### Event Message Format

QuotaViolation events include detailed information:

```text
ClusterResourceQuota '<crq-name>' <resource> limit exceeded: requested <amount>, current usage <amount>, quota limit <amount>, total would be <amount>
```

Example:

```text
ClusterResourceQuota 'team-alpha-quota' requests.cpu limit exceeded: requested <x>, current usage <y>, quota limit <z>, total would be <x+y>
```

### Event Backoff Strategy

Events use exponential backoff to prevent spam:

- Initial: 30 seconds
- Progression: 30s → 1m → 2m → 4m → 8m → 15m (max)
- Same violation type for same resource in same namespace will be throttled
- Different violations or different namespaces are tracked separately

---

## Example Prometheus Queries

- **Total webhook requests per type:**
  `sum by (webhook) (pac_quota_controller_webhook_validation_total)`

- **Admission decisions breakdown:**
  `sum by (webhook, decision) (pac_quota_controller_webhook_admission_decision_total)`

- **Average validation duration:**
  `avg by (webhook) (rate(pac_quota_controller_webhook_validation_duration_seconds_sum[5m]) / rate(pac_quota_controller_webhook_validation_duration_seconds_count[5m]))`

---

## How to Use

- Scrape the `/metrics` endpoint using Prometheus.
- Use the above queries to monitor quota usage and webhook performance.

---
