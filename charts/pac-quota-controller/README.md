# pac-quota-controller

![Version: 0.4.1](https://img.shields.io/badge/Version-0.4.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.4.1](https://img.shields.io/badge/AppVersion-0.4.1-informational?style=flat-square)

A Helm chart for PAC Quota Controller - Managing cluster resource quotas across namespaces

**Homepage:** <https://github.com/powerhome/pac-quota-controller>

## TL;DR

```console
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller --version <version> -n pac-quota-controller-system --create-namespace
```

## Introduction

This chart bootstraps a [PAC Quota Controller](https://github.com/powerhome/pac-quota-controller) deployment on a [Kubernetes](https://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

The PAC Quota Controller extends Kubernetes with a ClusterResourceQuota custom resource that allows defining resource quotas that span multiple namespaces.

### Object Count Quotas (Native & Extended Resources)

You can specify object count quotas for native and extended Kubernetes resources using the `hard` field in the ClusterResourceQuota spec.

#### Supported object count resources

- `pods`                                 (Pod count)
- `services`                             (Service count)
- `services.loadbalancers`               (Service type=LoadBalancer count)
- `services.nodeports`                   (Service type=NodePort count)
- `configmaps`                           (ConfigMap count)
- `secrets`                              (Secret count)
- `persistentvolumeclaims`               (PVC count)
- `replicationcontrollers`               (ReplicationController count)
- `deployments.apps`                     (Deployment count)
- `statefulsets.apps`                    (StatefulSet count)
- `daemonsets.apps`                      (DaemonSet count)
- `jobs.batch`                           (Job count)
- `cronjobs.batch`                       (CronJob count)
- `horizontalpodautoscalers.autoscaling` (HPA count)
- `ingresses.networking.k8s.io`          (Ingress count)

Subtype quotas (e.g., `services.loadbalancers`) cannot exceed the total for the parent resource (e.g., `services`).

Custom CRDs are not supported for object count quotas.

#### Example

```yaml
spec:
  hard:
    pods: "10"                                 # Pod count
    services: "5"                              # Service count
    services.loadbalancers: "2"                # Service type=LoadBalancer count
    services.nodeports: "3"                    # Service type=NodePort count
    configmaps: "20"                           # ConfigMap count
    secrets: "15"                              # Secret count
    persistentvolumeclaims: "8"                # PVC count
    replicationcontrollers: "4"                # ReplicationController count
    deployments.apps: "6"                      # Deployment count
    statefulsets.apps: "2"                     # StatefulSet count
    daemonsets.apps: "2"                       # DaemonSet count
    jobs.batch: "5"                            # Job count
    cronjobs.batch: "3"                        # CronJob count
    horizontalpodautoscalers.autoscaling: "2"  # HPA count
    ingresses.networking.k8s.io: "3"           # Ingress count
```

### Container Images

This chart can use container images from GitHub Container Registry:

```console
ghcr.io/powerhome/pac-quota-controller:0.4.1
```

You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8.0+

## Installing

You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

This chart is the single source of truth for deploying PAC Quota Controller. All manifests (CRDs, RBAC, webhooks, cert-manager, etc.) are managed here. Do not use Kustomize or Kubebuilder-generated manifests for deployment or testing.

To install the chart with the release name `pac-quota-controller`:

```sh
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller --version <version> -n pac-quota-controller-system --create-namespace
```

### Private Images

If you are using a private image registry (such as a private GHCR repository), you can provide image pull secrets:

```yaml
controllerManager:
  imagePullSecrets:
    - name: ghcr-creds
```

Then create the secret in your namespace:

```sh
kubectl create secret docker-registry ghcr-creds \
  --docker-server=https://ghcr.io \
  --docker-username=<your-username> \
  --docker-password=<your-token> \
  --docker-email=<your-email>
```

## Upgrading

To upgrade the chart:

```sh
helm upgrade pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller --version <version> -n pac-quota-controller-system
```

## Uninstalling the Chart

To uninstall/delete the `pac-quota-controller` deployment:

```console
helm delete pac-quota-controller -n pac-quota-controller-system
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

### Metrics Service

The controller exposes a Prometheus-compatible `/metrics` endpoint on a dedicated HTTPS port and service:

- **Service name:** `pac-quota-controller-service`
- **Port name:** `metrics-server` (default: 8443)
- **Path:** `/metrics`
- **Enabled by default:** Set `metrics.enable: true|false` in `values.yaml` to enable or disable the metrics server.
- **ServiceMonitor:** A `ServiceMonitor` resource can be automatically created by setting `prometheus.enable: true`.
- **TLS:** Uses cert-manager or user-provided certificates (see [Certificates](#certificates)).

The controller exposes the following key metrics:
- `pac_quota_controller_reconcile_total`: Total number of reconciliations, with labels for status (started, success, failed, etc.).
- `pac_quota_controller_reconcile_errors_total`: Total number of reconciliation errors.
- `pac_quota_controller_aggregation_duration_seconds`: A histogram of the time taken to aggregate resource usage for a ClusterResourceQuota.
- `pac_quota_controller_crq_usage`: Current usage percentage per resource/namespace.
- `pac_quota_controller_crq_total_usage`: Current aggregated usage percentage per resource.

#### Example Prometheus Scrape Config

```yaml
- job_name: 'pac-quota-controller'
  kubernetes_sd_configs:
    - role: endpoints
  relabel_configs:
    - source_labels: [__meta_kubernetes_service_name, __meta_kubernetes_namespace]
      action: keep
      regex: pac-quota-controller-metrics-service;pac-quota-controller-system
    - source_labels: [__meta_kubernetes_endpoint_port_name]
      action: keep
      regex: metrics-server
  scheme: https
  tls_config:
    insecure_skip_verify: true # or use CA if available
```

You can configure the port and certificate mount path for metrics via `values.yaml`:

```yaml
metrics:
  enable: true
  port: 8443
  certPath: /tmp/k8s-metrics-server/metrics-certs   # Path where the metrics cert secret is mounted
```

The controller will always look for `tls.crt` and `tls.key` in the specified directory.

### Alerting Rules

This chart can optionally deploy a `PrometheusRule` resource containing alerting rules for the controller. To enable alerting, set `prometheus.enable: true` and `prometheus.alerting.enable: true`.

Default alerts include:
- `QuotaControllerReconcileErrors`: Fires when reconciliation errors are detected.
- `QuotaBreached`: Fires when a ClusterResourceQuota limit is breached (usage > 100%).
- `HighAggregationLatency`: Fires when resource aggregation takes longer than the configured threshold.
- `QuotaControllerDown`: Fires when the controller manager deployment has no ready replicas.

You can configure and enable/disable individual rules via `values.yaml`:

```yaml
prometheus:
  alerting:
    enable: true
    rules:
      reconcileErrors:
        enable: true
        threshold: 0
        for: 5m
      quotaBreach:
        enable: true
        for: 1m
      highLatency:
        enable: true
        threshold: 5
        for: 10m
```

## Events

The PAC Quota Controller records Kubernetes Events to improve observability and enable event-driven monitoring. Events are automatically generated when:

- Quota thresholds are reached or exceeded
- Namespace selections change

### Event Configuration

Events are enabled by default and can be configured via `values.yaml`:

```yaml
events:
  enable: true
  cleanup:
    ttl: "24h"                  # Time-to-live for events
    maxEventsPerCRQ: 100        # Maximum events per ClusterResourceQuota
    interval: "1h"              # Cleanup interval
  recording:
    controllerComponent: "pac-quota-controller-controller"
    webhookComponent: "pac-quota-controller-webhook"
    backoff:
      baseInterval: "30s"       # Initial interval for quota violation events
      maxInterval: "15m"        # Maximum interval between events
```

### Event Cleanup

Events are automatically cleaned up based on:

- **TTL**: Events older than the configured TTL are removed
- **Count**: Only the most recent N events per ClusterResourceQuota are retained
- **Interval**: Cleanup runs at the configured interval

Events are recorded on ClusterResourceQuota objects and can be viewed with:

```bash
kubectl describe clusterresourcequota <name>
```

## Usage

Once installed, you can create a ClusterResourceQuota to limit resources across namespaces:

```yaml
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: team-quota
spec:
  namespaceSelector:
    matchLabels:
      team: frontend
  hard:
    pods: "50"
    requests.cpu: "10"
    requests.memory: 20Gi
    limits.cpu: "20"
    limits.memory: 40Gi
```

## Certificates

### Certmanager

This chart supports integration with [cert-manager](https://cert-manager.io/) for automatic provisioning and management of TLS certificates for webhooks and metrics endpoints. It is **strongly recommended** to use cert-manager.

If `certmanager.enable` is `true` (default), the chart will create `Certificate` resources, and cert-manager will be responsible for issuing and injecting the CA bundle and server certificates.

```sh
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install \
  cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version <CERT_MANAGER_VERSION> \
  --set crds.enabled=true
```

Replace `<CERT_MANAGER_VERSION>` with a compatible version (e.g., v1.18.0 or later).

### Manual Certificate Provisioning (Not Recommended)

If you choose not to use cert-manager (`certmanager.enable: false`), you must provide your own TLS certificates. This involves:

1. Creating Kubernetes `Secret` resources containing `tls.crt`, `tls.key`, and `ca.crt`.
2. Configure the following values in your `values.yaml` file:
    - `certmanager.enable`: Set to `false` to disable cert-manager integration.
    - `webhook.customTLS`:
        - `secretName`: Name of the Secret for the webhook server (must contain `tls.crt`, `tls.key`, `ca.crt`).
        - `caBundle`: Base64 encoded CA bundle (content of `ca.crt`) that the Kubernetes API server will use to trust your webhook.
    - `metrics.customTLS.secretName`: Name of the Secret for the metrics server (must contain `tls.crt`, `tls.key`).

| Name                        | Description                                                                                                                              | Type    | Default |
|-----------------------------|------------------------------------------------------------------------------------------------------------------------------------------|---------|---------|
| `certmanager.enable`        | Enable support for cert-manager. If `false`, manual certificate provisioning is required via `webhook.customTLS` and `metrics.customTLS`. | `bool`  | `true`  |
| `webhook.customTLS.secretName` | Secret name for webhook TLS certs if `certmanager.enable` is `false`.                                                                      | `string`| `""`    |
| `webhook.customTLS.caBundle`   | Base64 CA bundle for webhook if `certmanager.enable` is `false`.                                                                           | `string`| `""`    |
| `metrics.customTLS.secretName` | Secret name for metrics TLS certs if `certmanager.enable` is `false` and metrics are HTTPS.                                                | `string`| `""`    |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certmanager.enable | bool | `true` |  |
| controllerManager.container.args[0] | string | `"--leader-elect"` |  |
| controllerManager.container.image.pullPolicy | string | `"IfNotPresent"` |  |
| controllerManager.container.image.repository | string | `"ghcr.io/powerhome/pac-quota-controller"` |  |
| controllerManager.container.image.tag | string | `"latest"` |  |
| controllerManager.container.livenessProbe.httpGet.path | string | `"/healthz"` |  |
| controllerManager.container.livenessProbe.httpGet.port | int | `9443` |  |
| controllerManager.container.livenessProbe.httpGet.scheme | string | `"HTTPS"` |  |
| controllerManager.container.livenessProbe.initialDelaySeconds | int | `15` |  |
| controllerManager.container.livenessProbe.periodSeconds | int | `20` |  |
| controllerManager.container.readinessProbe.httpGet.path | string | `"/readyz"` |  |
| controllerManager.container.readinessProbe.httpGet.port | int | `9443` |  |
| controllerManager.container.readinessProbe.httpGet.scheme | string | `"HTTPS"` |  |
| controllerManager.container.readinessProbe.initialDelaySeconds | int | `5` |  |
| controllerManager.container.readinessProbe.periodSeconds | int | `10` |  |
| controllerManager.container.resources.limits.cpu | string | `"500m"` |  |
| controllerManager.container.resources.limits.memory | string | `"128Mi"` |  |
| controllerManager.container.resources.requests.cpu | string | `"10m"` |  |
| controllerManager.container.resources.requests.memory | string | `"64Mi"` |  |
| controllerManager.container.securityContext.allowPrivilegeEscalation | bool | `false` |  |
| controllerManager.container.securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| controllerManager.container.webhookCertPath | string | `"/tmp/k8s-webhook-server/serving-certs"` |  |
| controllerManager.excludeNamespaceLabelKey | string | `"pac-quota-controller.powerapp.cloud/exclude"` |  |
| controllerManager.replicas | int | `1` |  |
| controllerManager.securityContext.runAsNonRoot | bool | `true` |  |
| controllerManager.securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| controllerManager.serviceAccount.annotations | object | `{}` |  |
| controllerManager.serviceAccount.name | string | `"pac-quota-controller-manager"` |  |
| controllerManager.terminationGracePeriodSeconds | int | `15` |  |
| events.cleanup.interval | string | `"1h"` |  |
| events.cleanup.maxEventsPerCRQ | int | `100` |  |
| events.cleanup.ttl | string | `"24h"` |  |
| events.enable | bool | `true` |  |
| events.recording.backoff.baseInterval | string | `"30s"` |  |
| events.recording.backoff.maxInterval | string | `"15m"` |  |
| events.recording.controllerComponent | string | `"pac-quota-controller-controller"` |  |
| events.recording.webhookComponent | string | `"pac-quota-controller-webhook"` |  |
| excludedNamespaces[0] | string | `"kube-system"` |  |
| metrics.enable | bool | `true` |  |
| metrics.port | int | `8443` |  |
| prometheus.alerting.enable | bool | `true` |  |
| prometheus.alerting.rules.highLatency.enable | bool | `true` |  |
| prometheus.alerting.rules.highLatency.for | string | `"10m"` |  |
| prometheus.alerting.rules.highLatency.threshold | int | `5` |  |
| prometheus.alerting.rules.quotaBreach.enable | bool | `true` |  |
| prometheus.alerting.rules.quotaBreach.for | string | `"1m"` |  |
| prometheus.alerting.rules.reconcileErrors.enable | bool | `true` |  |
| prometheus.alerting.rules.reconcileErrors.for | string | `"5m"` |  |
| prometheus.alerting.rules.reconcileErrors.threshold | int | `0` |  |
| prometheus.enable | bool | `true` |  |
| prometheus.serviceMonitor.enable | bool | `true` |  |
| rbac.enable | bool | `true` |  |
| webhook.dryRunOnly | bool | `false` |  |
| webhook.enable | bool | `true` |  |
