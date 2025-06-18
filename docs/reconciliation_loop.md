# ClusterResourceQuota Reconciliation Loop

This document provides a detailed explanation of the reconciliation loop for the `ClusterResourceQuota` controller. Understanding this process is key to debugging and extending the controller's functionality.

## Overview

The controller's primary responsibility is to monitor `ClusterResourceQuota` (CRQ) objects and the resources they track across selected namespaces. It ensures that the status of each CRQ accurately reflects the aggregated usage of resources like Pods, Services, and ConfigMaps.

The reconciliation process is triggered by two main types of events:

1. Changes to a `ClusterResourceQuota` object itself.
2. Changes (create, update, delete) to a tracked resource (e.g., `Pod`, `Namespace`) in the cluster.

## Main Reconciliation Loop

The `Reconcile` function is the entry point for the main loop. It is triggered when a reconciliation request is added to the work queue by the event handlers.

```mermaid
graph TD
    A[Start Reconciliation] --> B{Fetch ClusterResourceQuota};
    B --> C{Get Selected Namespaces};
    C --> D{Calculate Aggregated Usage};
    D --> E{Update CRQ Status};
    E --> F[End Reconciliation];

    B --> G[CRQ Not Found?];
    G -- Yes --> H[Stop, Object Deleted];
    G -- No --> C;

    C --> I[Error?];
    I -- Yes --> J[Requeue Request];
    I -- No --> D;

    D --> K[Error?];
    K -- Yes --> J;
    K -- No --> E;

    E --> L[Error?];
    L -- Yes --> J;
    L -- No --> F;
```

### Steps Explained

1. **Fetch ClusterResourceQuota**: The controller starts by fetching the `ClusterResourceQuota` instance that triggered the reconciliation. If it's not found, the process stops, as the object was likely deleted.
2. **Get Selected Namespaces**: It identifies all namespaces that match the `namespaceSelector` defined in the CRQ's spec.
3. **Calculate Aggregated Usage**: The controller calculates the total usage of tracked resources (e.g., `pods`, `services`) across all selected namespaces.
    - *Note: Currently, this is a placeholder and returns zeroed-out data. The actual calculation logic will be implemented here.*
4. **Update CRQ Status**: The controller updates the `.status` field of the CRQ with the newly calculated total usage and the per-namespace usage breakdown. It uses a server-side patch to prevent write conflicts.
5. **End Reconciliation**: If all steps are successful, the reconciliation is complete. If any step fails, the request is requeued for a later attempt.

## Event Handlers and Watchers

The controller uses watchers to monitor changes to Kubernetes objects and trigger reconciliations.

```mermaid
graph TD
    subgraph Watchers
        W1(Watch ClusterResourceQuotas)
        W2(Watch Namespaces)
        W3(Watch Pods, Services, etc.)
    end

    subgraph Event Handlers
        H1[findQuotasForNamespace]
        H2[findQuotasForObject]
    end

    W1 --> R[Enqueue CRQ for Reconciliation]
    W2 --> H1
    W3 --> H2

    H1 --> R
    H2 --> R

    R --> Q(Reconciliation Work Queue)
```

### Handler Logic Explained

- **`findQuotasForNamespace`**:
  - **Triggered by**: Changes to `Namespace` objects.
  - **Logic**: When a namespace is created, updated, or deleted, this handler checks if the namespace's labels match any `ClusterResourceQuota`'s `namespaceSelector`. If a match is found, it enqueues that CRQ for reconciliation. This ensures that the controller reacts to namespaces being added to or removed from a quota's scope.
  - **Logging**: Logs a "Processing namespace event" message.

- **`findQuotasForObject`**:
  - **Triggered by**: Changes to tracked resources like `Pods`, `Services`, `ConfigMaps`, etc.
  - **Logic**: When a tracked resource is changed, this handler first gets the namespace of that resource. It then finds all `ClusterResourceQuota` objects that select that namespace and enqueues them for reconciliation.
  - **Logging**: Logs a "Processing object event" message, including the kind and name of the object that triggered the event, making it clear why the reconciliation is happening.

This dual-handler approach ensures that the controller remains responsive to all relevant changes in the cluster, keeping the `ClusterResourceQuota` status up-to-date.
