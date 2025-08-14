# pac-quota-controller

![Version: 0.1.1](https://img.shields.io/badge/Version-0.1.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.1](https://img.shields.io/badge/AppVersion-0.1.1-informational?style=flat-square)

A Helm chart for PAC Quota Controller - Managing cluster resource quotas across namespaces

**Homepage:** <https://github.com/powerhome/pac-quota-controller>

## TL;DR

```console
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system --create-namespace
```

## Introduction

This chart bootstraps a [PAC Quota Controller](https://github.com/powerhome/pac-quota-controller) deployment on a [Kubernetes](https://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

The PAC Quota Controller extends Kubernetes with a ClusterResourceQuota custom resource that allows defining resource quotas that span multiple namespaces.

### Container Images

This chart can use container images from GitHub Container Registry:

```console
ghcr.io/powerhome/pac-quota-controller:0.1.1
```

You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8.0+

## Installation
You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

This chart is the single source of truth for deploying PAC Quota Controller. All manifests (CRDs, RBAC, webhooks, cert-manager, etc.) are managed here. Do not use Kustomize or Kubebuilder-generated manifests for deployment or testing.

To install the chart with the release name `pac-quota-controller`:

```sh
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system --create-namespace
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
helm upgrade pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system
```

## Uninstalling the Chart

To uninstall/delete the `pac-quota-controller` deployment:

```console
helm delete pac-quota-controller -n pac-quota-controller-system
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

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
| controllerManager.container.args[1] | string | `"--metrics-bind-address=:8443"` |  |
| controllerManager.container.args[2] | string | `"--health-probe-bind-address=:8081"` |  |
| controllerManager.container.image.pullPolicy | string | `"IfNotPresent"` |  |
| controllerManager.container.image.repository | string | `"ghcr.io/powerhome/pac-quota-controller"` |  |
| controllerManager.container.image.tag | string | `"latest"` |  |
| controllerManager.container.livenessProbe.httpGet.path | string | `"/healthz"` |  |
| controllerManager.container.livenessProbe.httpGet.port | int | `8081` |  |
| controllerManager.container.livenessProbe.initialDelaySeconds | int | `15` |  |
| controllerManager.container.livenessProbe.periodSeconds | int | `20` |  |
| controllerManager.container.readinessProbe.httpGet.path | string | `"/readyz"` |  |
| controllerManager.container.readinessProbe.httpGet.port | int | `8081` |  |
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
| controllerManager.terminationGracePeriodSeconds | int | `10` |  |
| crd.enable | bool | `true` |  |
| crd.keep | bool | `true` |  |
| metrics.enable | bool | `true` |  |
| networkPolicy.enable | string | `"enable"` |  |
| prometheus.enable | bool | `false` |  |
| rbac.enable | bool | `true` |  |
| webhook.dryRunOnly | bool | `false` |  |
| webhook.enable | bool | `true` |  |
