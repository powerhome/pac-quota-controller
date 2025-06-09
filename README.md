# ClusterResourceQuota Controller

A Kubernetes operator for implementing cluster-wide resource quotas across multiple namespaces.

## Overview

The ClusterResourceQuota controller mimics the Kubernetes ResourceQuota mechanism to work across multiple namespaces. This allows administrators to define resource constraints that apply to groups of namespaces based on label selectors.

## Features

- Create quotas that apply across multiple namespaces
- Select namespaces using label selectors
- Support for all standard Kubernetes resource types (pods, services, etc.)
- Support for compute resources (CPU, memory)
- Support for storage resources (PVCs)
- Automatic aggregation of resource usage across namespaces

## Usage

### Installation

> **Note:** The Helm chart is the single source of truth for all manifests (CRDs, RBAC, webhooks, cert-manager, etc.). The `config/` folder and Kubebuilder-generated manifests are not used for deployment or testing. Kubebuilder markers in the Go code are for documentation only.

Install the controller using Helm:

```sh
helm install pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system --create-namespace
```

To upgrade:

```sh
helm upgrade pac-quota-controller oci://ghcr.io/powerhome/pac-quota-controller-chart --version <version> -n pac-quota-controller-system
```

## End-to-End (e2e) Testing

All e2e tests use Helm for deployment. The `config/` folder is ignored and not used for testing or production. To run e2e tests:

```sh
# Install chart for testing
helm install pac-quota-controller ./charts/pac-quota-controller -n pac-quota-controller-system --create-namespace
# Run tests
make test-e2e
# Uninstall after tests
helm uninstall pac-quota-controller -n pac-quota-controller-system
```

## Example Usage

Create a ClusterResourceQuota to limit resources across namespaces:

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

## Kubebuilder Markers

Kubebuilder markers in the Go code are for documentation only and are not used for manifest generation or deployment. All configuration is managed in the Helm chart.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
