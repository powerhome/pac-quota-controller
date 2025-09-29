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
- **Description:** Aggregated usage of a resource across all namespaces for a ClusterResourceQuota.

---

## Webhook Metrics

### `pac_quota_controller_webhook_validation_total`

- **Type:** Counter
- **Labels:** `webhook`, `operation`
- **Description:** Total number of webhook validation requests.

### `pac_quota_controller_webhook_validation_duration_seconds`

- **Type:** Histogram
- **Labels:** `webhook`, `operation`
- **Description:** Duration of webhook validation requests.

### `pac_quota_controller_webhook_admission_decision_total`

- **Type:** Counter
- **Labels:** `webhook`, `operation`, `decision`
- **Description:** Total number of webhook admission decisions (allowed/denied).

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
