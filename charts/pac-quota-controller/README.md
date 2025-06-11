# pac-quota-controller

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

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
ghcr.io/powerhome/pac-quota-controller:0.1.0
```

You can configure which registry to use by modifying the `controllerManager.container.image.repository` value.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+

## Installation

This chart is the single source of truth for deploying PAC Quota Controller. All manifests (CRDs, RBAC, webhooks, cert-manager, etc.) are managed here. Do not use Kustomize or Kubebuilder-generated manifests for deployment or testing.

To install the chart with the release name `pac-quota-controller`:

```sh
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system --create-namespace
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

## Cert-Manager

| Name                      | Description                                                                                 | Type    | Default |
|---------------------------|---------------------------------------------------------------------------------------------|---------|---------|
| certmanager.enable        | Enable support for cert-manager (required for webhooks and certificate management).          | bool    | true    |
| certmanager.install       | Install cert-manager in this namespace. **Should only be true if cert-manager is not already installed in your cluster.** If you already have a running cert-manager, set this to false to avoid conflicts. | bool    | true    |

> **Note:**
>
> - `certmanager.enable` controls whether the chart configures resources to use cert-manager for certificates.
> - `certmanager.install` controls whether the chart will deploy cert-manager itself into the same namespace. Only set this to `true` if you do **not** already have cert-manager running in your cluster. If you already have cert-manager, set this to `false` to avoid duplicate installations.
