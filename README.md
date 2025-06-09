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

You can install the ClusterResourceQuota operator using the following methods:

#### Using Helm (GitHub Pages)

```bash
helm repo add powerhome https://powerhome.github.io/pac-quota-controller
helm repo update
helm install pac-quota-controller powerhome/pac-quota-controller -n pac-quota-controller-system --create-namespace
```

#### Using kubectl

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

## How it works

```mermaid
graph TD
  A[ClusterResourceQuota CRD] -- selects --> B[Namespaces (label selector)]
  B -- aggregates --> C[ResourceQuota usage]
  C -- reports --> D[ClusterResourceQuota Status]
```

- The controller watches `ClusterResourceQuota` resources.
- It selects namespaces using the `namespaceSelector` field.
- It aggregates resource usage across those namespaces.
- The status of the `ClusterResourceQuota` is updated with the total usage and enforcement.
