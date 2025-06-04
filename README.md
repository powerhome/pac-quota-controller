# ClusterResourceQuota Controller

A Kubernetes operator for implementing cluster-wide resource quotas across multiple namespaces.

## Overview

The ClusterResourceQuota controller extends the Kubernetes ResourceQuota mechanism to work across multiple namespaces. This allows administrators to define resource constraints that apply to groups of namespaces based on label selectors.

## Features

- Create quotas that apply across multiple namespaces
- Select namespaces using label selectors
- Support for all standard Kubernetes resource types (pods, services, etc.)
- Support for compute resources (CPU, memory)
- Support for storage resources (PVCs)
- Automatic aggregation of resource usage across namespaces

## Installation

You can install the ClusterResourceQuota operator using the following methods:

### Using Helm

```bash
# Add the Helm repository
helm repo add powerhome https://powerhome.github.io/pac-quota-controller
helm repo update

# Install the chart
helm install pac-quota-controller powerhome/pac-quota-controller -n pac-quota-controller-system --create-namespace
```

For more information about the chart, see the [Helm chart documentation](./charts/pac-quota-controller/README.md).

### Using kubectl

```bash
kubectl apply -f https://github.com/powerhome/pac-quota-controller/releases/latest/download/install.yaml
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

## Development

### CI/CD Workflow

This project uses GitHub Actions for continuous integration and deployment:

1. **Linting**: Runs on every push and pull request to check code quality
2. **Helm Chart Testing**: Validates chart structure and configuration
3. **Release**: Automatically creates binaries, Docker images, and updates the Helm chart when a new tag is pushed

### Helm Chart Generation

The Helm chart is automatically generated using the Kubebuilder Helm plugin:

```bash
# Manually generate the Helm chart
make generate-helm

# Generate Helm chart documentation
make helm-docs

# Lint the Helm chart
make helm-lint

# Test the Helm chart installation in a Kind cluster
make helm-test
```

### Docker Images

The Docker images for this project are available from both GitHub Container Registry and DockerHub:

```bash
# Pull from DockerHub
docker pull powerhome/pac-quota-controller:latest
```

The CI/CD pipeline automatically builds and pushes Docker images to both registries when a new tag is pushed.
