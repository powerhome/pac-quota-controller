# Extended Resources Support in pac-quota-controller

## Overview

The pac-quota-controller **already supports** Kubernetes extended resources as defined in the [Kubernetes documentation](https://kubernetes.io/docs/concepts/policy/resource-quotas/#resource-quota-for-extended-resources).

## What are Extended Resources?

Extended resources are custom resources that can be tracked and limited by Kubernetes ResourceQuotas. Common examples include:

- `nvidia.com/gpu` - NVIDIA GPU devices
- `amd.com/gpu` - AMD GPU devices
- `example.com/foo` - Custom application-specific resources
- `intel.com/fpga` - FPGA devices
- Any domain-prefixed resource name

## How it Works

### Resource Calculation

Our implementation in `pkg/kubernetes/pod/pod.go` automatically handles extended resources through the `default` case in `getContainerResourceUsage()`:

```go
default:
    // Handle hugepages and other resource types
    if resourceValue, ok := container.Resources.Requests[resourceName]; ok {
        return resourceValue
    }
    if resourceValue, ok := container.Resources.Limits[resourceName]; ok {
        return resourceValue
    }
```

### Resource Summing

Extended resources follow the same summing behavior as standard resources:

- **All containers** (init + regular) are summed together
- Each container's extended resource usage is added to the total
- Supports both integer and fractional quantities

## Usage in ClusterResourceQuota

Extended resources must be specified with the `requests.` prefix in the `hard` limits:

```yaml
apiVersion: quota.powerapp.cloud/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: gpu-quota
spec:
  namespaceSelector:
    matchLabels:
      team: ml-team
  hard:
    requests.nvidia.com/gpu: "4"       # Limit GPU requests
    requests.example.com/custom: "10"  # Limit custom resource
    requests.cpu: "2"                  # Standard CPU limit
    requests.memory: "4Gi"             # Standard memory limit
```

## Pod Resource Examples

### Basic GPU Usage

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: gpu-container
    resources:
      requests:
        nvidia.com/gpu: "2"
        cpu: "500m"
      limits:
        cpu: "1"
```

### GPU with Init Containers (Summed)

```yaml
apiVersion: v1
kind: Pod
spec:
  initContainers:
  - name: init-setup
    resources:
      requests:
        nvidia.com/gpu: "1"  # This will be summed
  containers:
  - name: main-app
    resources:
      requests:
        nvidia.com/gpu: "2"  # Total: 1 + 2 = 3 GPUs
```

### Fractional Resources

```yaml
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: bandwidth-app
    resources:
      requests:
        example.com/bandwidth: "1.5"  # Fractional quantities supported
```

## Status Reporting

Extended resource usage appears in the ClusterResourceQuota status:

```yaml
status:
  total:
    used:
      requests.nvidia.com/gpu: "3"
      requests.example.com/custom: "7"
      requests.cpu: "1500m"
      requests.memory: "3Gi"
```

## Key Differences from Standard Resources

1. **Requests Only**: Extended resources only support `requests`, not `limits` in quotas
2. **No Overcommit**: Extended resources don't allow overcommitment
3. **Domain Prefixed**: Must use domain-style naming (e.g., `nvidia.com/gpu`)

## Testing

Comprehensive test coverage has been added for extended resources:

- **Unit Tests**: `pkg/kubernetes/pod/pod_test.go` and `usage_test.go`
- **E2E Tests**: `test/e2e/compute_resources_test.go`
- **Example**: `examples/extended-resources-example.yaml`

## Validation

Run the test suite to verify extended resources work correctly:

```bash
# Unit tests
make test

# E2E tests
make test-e2e

# Apply example
kubectl apply -f examples/extended-resources-example.yaml
```

## Supported Extended Resource Types

✅ **Fully Supported:**

- GPU resources (`nvidia.com/gpu`, `amd.com/gpu`)
- Custom domain resources (`example.com/foo`, `company.com/device`)
- FPGA resources (`intel.com/fpga`)
- Fractional quantities (`example.com/bandwidth: "1.5"`)
- Integer quantities (`nvidia.com/gpu: "4"`)
- Init container + regular container summing
- Multiple containers per pod
- Multiple pods per namespace
- Cross-namespace quota enforcement

## Implementation Details

The extended resources support is implemented in:

- `CalculateResourceUsage()` - Individual pod resource calculation
- `CalculatePodUsage()` - Namespace-wide resource aggregation
- Both functions automatically handle any resource type through dynamic resource name lookup

This implementation aligns with Kubernetes' ResourceQuota behavior and follows the official specification for extended resources.
