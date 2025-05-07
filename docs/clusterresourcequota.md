# ClusterResourceQuota CRD

The ClusterResourceQuota Custom Resource Definition (CRD) allows you to manage and enforce CPU and memory resource quotas across multiple namespaces in a Kubernetes cluster.

## Overview

The ClusterResourceQuota CRD provides a way to:

- Define CPU and memory limits that span multiple namespaces
- Track resource usage across namespaces
- Enforce quotas at the cluster level
- Monitor total resource consumption

## API Reference

### Group and Version

- Group: `pac.powerhome.com`
- Version: `v1alpha1`
- Kind: `ClusterResourceQuota`
- Short Name: `crq`

### Schema

```yaml
apiVersion: pac.powerhome.com/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: <quota-name>
spec:
  namespaces:
    - <namespace1>
    - <namespace2>
  hard:
    cpu: <limit>
    memory: <limit>
status:
  total:
    cpu: <usage>
    memory: <usage>
  namespaces:
    - namespace: <namespace>
      status:
        used:
          cpu: <usage>
          memory: <usage>
```

## Resource Types

### Compute Resources

- `cpu`: CPU limit in cores or millicores (e.g., "1000m" or "1")
- `memory`: Memory limit in bytes (e.g., "1Gi" or "1000Mi")

## Validation Rules

The ClusterResourceQuota validation enforces several important rules:

1. **Namespace Uniqueness**: A namespace can only be included in one ClusterResourceQuota.
2. **Namespace Existence**: All namespaces specified must exist in the cluster.
3. **Resource Limit Validation**:
   - The webhook validates pod **container limits**, not requests.
   - If a pod doesn't specify resource limits, it won't be counted toward quota usage.
   - CPU is measured in millicores (1 CPU = 1000m).
   - Memory is measured in bytes.
4. **Self-Exclusion**: When updating a ClusterResourceQuota, it will exclude itself from namespace uniqueness validation.

## Usage Examples

### Basic Quota

```yaml
apiVersion: pac.powerhome.com/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: team-a-quota
spec:
  namespaces:
    - team-a-ns1
    - team-a-ns2
  hard:
    cpu: "2000m"
    memory: "4Gi"
```

### Production Quota

```yaml
apiVersion: pac.powerhome.com/v1alpha1
kind: ClusterResourceQuota
metadata:
  name: production-quota
spec:
  namespaces:
    - prod-ns1
    - prod-ns2
    - prod-ns3
  hard:
    cpu: "4000m"
    memory: "8Gi"
```

## Best Practices

1. **Namespace Selection**
   - Group related namespaces together
   - Consider team or project boundaries
   - Avoid overlapping quotas

2. **Resource Limits**
   - Set realistic limits based on cluster capacity
   - Consider growth and scaling needs
   - Monitor usage patterns
   - Ensure pods specify resource limits for accurate quota enforcement

3. **Monitoring**
   - Regularly check quota status
   - Set up alerts for quota usage
   - Review and adjust limits as needed
   - Enable debug logging when troubleshooting: `PAC_QUOTA_CONTROLLER_LOG_LEVEL=debug`

4. **Security**
   - Restrict access to ClusterResourceQuota resources
   - Use RBAC to control who can create/modify quotas
   - Audit quota changes

## Troubleshooting

### Common Issues

1. **Quota Exceeded**
   - Check current usage: `kubectl get clusterresourcequota <n> -o yaml`
   - Review namespace usage in status
   - Consider increasing limits or optimizing resource usage
   - Verify that pod limits (not requests) are within quota constraints

2. **Namespace Not Included**
   - Verify namespace is in the `namespaces` list
   - Check for typos in namespace names
   - Ensure namespace exists

3. **Invalid Resource Values**
   - Verify resource format (e.g., "1000m" for CPU)
   - Ensure values are within cluster capacity

4. **Permission Issues**
   - Verify the webhook has necessary RBAC permissions
   - The webhook needs permissions to list pods, namespaces, and ClusterResourceQuotas

### Debugging Commands

```bash
# Get quota details
kubectl get clusterresourcequota <n> -o yaml

# Check quota status
kubectl describe clusterresourcequota <n>

# List all quotas
kubectl get clusterresourcequota

# Check webhook logs (normal logging)
kubectl logs -n pac-system -l app.kubernetes.io/name=pac-quota-controller

# Enable debug logging
kubectl set env deployment/pac-quota-controller -n pac-system PAC_QUOTA_CONTROLLER_LOG_LEVEL=debug

# Check webhook logs with debug enabled
kubectl logs -n pac-system -l app.kubernetes.io/name=pac-quota-controller -f
```

## Integration with Webhook

The admission webhook validates pod creation and updates against the ClusterResourceQuota limits. It:

1. Checks if the pod's namespace is included in any quota
2. Validates CPU and memory requests against quota limits
3. Enforces quota restrictions
4. Updates quota status with usage information

## Implementation Architecture

The ClusterResourceQuota validation is implemented using a layered architecture:

```text
Webhook Handler → Quota Service → Validators → Repositories → Kubernetes API
```

1. **Repository Layer**: Handles data access to Kubernetes resources
   - `QuotaRepository`: Manages ClusterResourceQuota CRDs
   - `NamespaceRepository`: Interacts with Kubernetes namespaces
   - `PodRepository`: Calculates resource usage from pod limits

2. **Validator Layer**: Contains pure validation logic
   - `NamespaceValidator`: Ensures namespace uniqueness and existence
   - `ResourceValidator`: Validates resource limits against quotas

3. **Service Layer**: Coordinates validation operations
   - `QuotaService`: Orchestrates the validation workflow

## Future Enhancements

Planned improvements for the ClusterResourceQuota CRD:

1. Support for additional resource types
2. Dynamic namespace selection using labels
3. Quota templates and inheritance
4. Advanced monitoring and alerting
5. Integration with cluster autoscaling
6. Resource forecasting and prediction
