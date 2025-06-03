# pac-quota-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

A Helm chart for PAC Quota Controller - Managing cluster resource quotas across namespaces

**Homepage:** <https://github.com/powerhome/pac-quota-controller>

## TL;DR

```console
helm repo add powerhome https://powerhome.github.io/pac-quota-controller
helm install pac-quota-controller powerhome/pac-quota-controller -n pac-quota-controller-system --create-namespace
```

## Introduction

This chart bootstraps a [PAC Quota Controller](https://github.com/powerhome/pac-quota-controller) deployment on a [Kubernetes](https://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

The PAC Quota Controller extends Kubernetes with a ClusterResourceQuota custom resource that allows defining resource quotas that span multiple namespaces.

### Container Images

This chart can use container images from either DockerHub or GitHub Container Registry:

```console
# DockerHub (default in this chart)
powerhome/pac-quota-controller:0.1.0
```

You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+

## Installing the Chart

To install the chart with the release name `pac-quota-controller`:

```console
helm repo add powerhome https://powerhome.github.io/pac-quota-controller
helm install pac-quota-controller powerhome/pac-quota-controller -n pac-quota-controller-system --create-namespace
```

The command deploys PAC Quota Controller on the Kubernetes cluster in the default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

> **Tip**: List all releases using `helm list -A`

## Uninstalling the Chart

To uninstall/delete the `pac-quota-controller` deployment:

```console
helm delete pac-quota-controller -n pac-quota-controller-system
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Parameters

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certmanager.enable | bool | `false` |  |
| controllerManager.container.args[0] | string | `"--leader-elect"` |  |
| controllerManager.container.args[1] | string | `"--metrics-bind-address=:8443"` |  |
| controllerManager.container.args[2] | string | `"--health-probe-bind-address=:8081"` |  |
| controllerManager.container.image.repository | string | `"powerhome/pac-quota-controller"` |  |
| controllerManager.container.image.tag | string | `"{{ .Chart.AppVersion }}"` |  |
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
| controllerManager.replicas | int | `1` |  |
| controllerManager.securityContext.runAsNonRoot | bool | `true` |  |
| controllerManager.securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| controllerManager.serviceAccountName | string | `"pac-quota-controller-controller-manager"` |  |
| controllerManager.terminationGracePeriodSeconds | int | `10` |  |
| crd.enable | bool | `true` |  |
| crd.keep | bool | `true` |  |
| metrics.enable | bool | `true` |  |
| networkPolicy.enable | bool | `false` |  |
| prometheus.enable | bool | `false` |  |
| rbac.enable | bool | `true` |  |

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
