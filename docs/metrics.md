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
- **Labels:** `crq_name`, `resource`
- **Description:** Aggregated usage of a resource across all namespaces for a ClusterResourceQuota, as a ratio (0-1) of `hard`.

> This previously documented `namespace`/`namespaces` labels that were never
> actually emitted (a comma-joined `namespaces` label was considered and
> rejected — it would churn a new series on every namespace add/remove). To
> resolve a CRQ's current namespace set from Prometheus alone, use
> `pac_quota_controller_crq_used_by_namespace` instead:
> `group by (crq_name, namespace) (pac_quota_controller_crq_used_by_namespace)`.

### `pac_quota_controller_crq_hard`

- **Type:** Gauge
- **Labels:** `crq_name`, `resource`
- **Description:** The enforced hard limit of a resource for a ClusterResourceQuota, as an absolute quantity.

### `pac_quota_controller_crq_used`

- **Type:** Gauge
- **Labels:** `crq_name`, `resource`
- **Description:** Aggregated usage of a resource across all namespaces for a ClusterResourceQuota, as an absolute quantity (the numerator behind `crq_total_usage`).

### `pac_quota_controller_crq_used_by_namespace`

- **Type:** Gauge
- **Labels:** `crq_name`, `namespace`, `resource`
- **Description:** Usage of a resource for a ClusterResourceQuota in a namespace, as an absolute quantity (the numerator behind `crq_usage`).

> **Why both ratio and absolute gauges**: the ratio (`crq_usage` /
> `crq_total_usage`) is convenient for a live snapshot, but `hard` can change
> over time and isn't recorded on the ratio series — `ratio * hard_now` is
> wrong for any past window where the quota was edited. The absolute gauges
> make historical analysis (e.g. recommending a `hard` value from observed
> usage over a 14/30d window) a plain `max_over_time`/`quantile_over_time`
> query instead of reconstructing usage from kube-state-metrics. Both are
> kept: they cost no extra cardinality over `crq_usage`/`crq_total_usage`
> alone, since each absolute gauge shares its predecessor's label set.

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
> cluster-scoped webhooks (Namespace, ClusterResourceQuota) the label is left
> empty, since those resources have no namespace of their own. Emitting the
> object name there would be misleading for dashboards and alert routing that
> treat the label as a real namespace, and would explode cardinality with one
> series per cluster-scoped object.
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
