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

## Webhook Events

The pac-quota-controller emits Kubernetes Events when resource quotas are violated. Events are recorded on the ClusterResourceQuota object with exponential backoff to prevent event spam.

### Event Types

#### QuotaViolation Events

**Event Type:** `Warning`  
**Reason:** `QuotaViolation`  
**Source:** `pac-quota-controller-webhook`

These events are emitted when a resource request would exceed the defined ClusterResourceQuota limits.

### Events by Webhook

#### Pod Webhook (`pod_webhook.go`)

Emits QuotaViolation events for the following resources:

- `requests.cpu` - CPU requests from all containers in the pod
- `requests.memory` - Memory requests from all containers in the pod  
- `limits.cpu` - CPU limits from all containers in the pod
- `limits.memory` - Memory limits from all containers in the pod
- `pods` - Pod count (always +1 for the pod being created)

#### Service Webhook (`service_webhook.go`)

Emits QuotaViolation events for the following resources:

- `services` - All service types (always +1 for the service being created)
- `services.loadbalancers` - LoadBalancer services specifically
- `services.nodeports` - NodePort services specifically

#### PersistentVolumeClaim Webhook (`persistentvolumeclaim_webhook.go`)

Emits QuotaViolation events for the following resources:

- `requests.storage` - Storage requests from the PVC
- `persistentvolumeclaims` - PVC count (always +1 for the PVC being created)
- `<storageclass>.storageclass.storage.k8s.io/requests.storage` - Storage-class specific storage requests
- `<storageclass>.storageclass.storage.k8s.io/persistentvolumeclaims` - Storage-class specific PVC counts

#### ObjectCount Webhook (`objectcount_webhook.go`)

Emits QuotaViolation events for extended Kubernetes resources (configurable):

- `configmaps` - ConfigMap count
- `secrets` - Secret count
- `replicationcontrollers` - ReplicationController count
- `deployments.apps` - Deployment count
- `statefulsets.apps` - StatefulSet count
- `daemonsets.apps` - DaemonSet count
- And other extended resources as configured

#### Namespace Webhook (`namespace_webhook.go`)

**No QuotaViolation events** - Validates namespace label conflicts with CRQs, but doesn't check resource quotas.

#### ClusterResourceQuota Webhook (`clusterresourcequota_webhook.go`)

**No QuotaViolation events** - Validates CRQ configuration and conflicts, but doesn't check resource quotas.

### Event Message Format

QuotaViolation events include detailed information:

```text
ClusterResourceQuota '<crq-name>' <resource> limit exceeded: requested <amount>, current usage <amount>, quota limit <amount>, total would be <amount>
```

Example:

```text
ClusterResourceQuota 'team-alpha-quota' requests.cpu limit exceeded: requested 500m, current usage 3500m, quota limit 4000m, total would be 4500m
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
