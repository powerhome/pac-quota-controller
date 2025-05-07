# PAC Resource Sharing Validation Webhook: Sequence Diagrams

This document provides sequence diagrams illustrating the key workflows of the PAC Resource Sharing Validation Webhook (pac-quota-controller).

## Pod Validation Workflow

The following sequence diagram shows the validation flow when a Pod is created:

```mermaid
sequenceDiagram
    actor User
    participant ApiServer as Kubernetes API Server
    participant Webhook as PAC Quota Controller Webhook
    participant QuotaService as Quota Service
    participant Validators as Validators
    participant Repositories as Repositories
    participant K8sApi as Kubernetes API

    User->>ApiServer: Create Pod
    ApiServer->>Webhook: AdmissionReview Request

    Webhook->>Webhook: Parse Pod Object
    Webhook->>Webhook: Extract Resource Limits

    Webhook->>QuotaService: ValidatePodAgainstQuotas()

    QuotaService->>Repositories: FindQuotasContainingNamespace()
    Repositories->>K8sApi: List ClusterResourceQuotas
    K8sApi-->>Repositories: Quota List
    Repositories-->>QuotaService: Quotas for Namespace

    loop For Each Quota
        QuotaService->>Validators: ValidateQuotaNotExceeded()

        Validators->>Repositories: CalculateResourceUsageForNamespaces()
        Repositories->>K8sApi: List Pods in Namespaces
        K8sApi-->>Repositories: Pod List
        Repositories-->>Validators: Current CPU/Memory Usage

        Validators->>Validators: Calculate Total with New Pod
        Validators->>Validators: Compare with Quota Limits
        Validators-->>QuotaService: Validation Result
    end

    QuotaService-->>Webhook: Validation Result

    alt Validation Passed
        Webhook-->>ApiServer: AdmissionResponse (Allowed: true)
        ApiServer-->>User: Pod Created
    else Validation Failed
        Webhook-->>ApiServer: AdmissionResponse (Allowed: false, Reason)
        ApiServer-->>User: Error - Quota Exceeded
    end
```

## ClusterResourceQuota Validation Workflow

When creating or updating a ClusterResourceQuota, the system validates that namespaces are not already associated with other quotas:

```mermaid
sequenceDiagram
    actor User
    participant ApiServer as Kubernetes API Server
    participant Webhook as PAC Quota Controller Webhook
    participant QuotaService as Quota Service
    participant NSValidator as Namespace Validator
    participant K8sApi as Kubernetes API

    User->>ApiServer: Create ClusterResourceQuota
    ApiServer->>Webhook: AdmissionReview Request

    Webhook->>Webhook: Parse ClusterResourceQuota

    Webhook->>QuotaService: ValidateQuotaCreation()

    QuotaService->>NSValidator: ValidateNamespacesExist()
    NSValidator->>K8sApi: Get Namespaces
    K8sApi-->>NSValidator: Namespace Exists Result
    NSValidator-->>QuotaService: Existence Validation Result

    QuotaService->>NSValidator: ValidateNamespacesUniqueness()
    NSValidator->>K8sApi: List ClusterResourceQuotas
    K8sApi-->>NSValidator: Quota List
    NSValidator->>NSValidator: Check for Namespace Conflicts
    NSValidator-->>QuotaService: Uniqueness Validation Result

    QuotaService-->>Webhook: Validation Result

    alt Validation Passed
        Webhook-->>ApiServer: AdmissionResponse (Allowed: true)
        ApiServer-->>User: ClusterResourceQuota Created
    else Validation Failed
        Webhook-->>ApiServer: AdmissionResponse (Allowed: false, Reason)
        ApiServer-->>User: Error - Validation Failed
    end
```

## Key Components

The sequence diagrams illustrate the clean separation between the different layers of the PAC Quota Controller architecture:

1. **Webhook Handlers**: Process incoming AdmissionReview requests
2. **Services**: Implement business logic for validating resources
3. **Validators**: Contain specific validation logic for different scenarios
4. **Repositories**: Interact with the Kubernetes API to retrieve and calculate resource data

This layered approach ensures that the codebase remains modular, testable, and maintainable as the application grows.
