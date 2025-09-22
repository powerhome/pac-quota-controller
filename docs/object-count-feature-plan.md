# ClusterResourceQuota Object Count Feature Implementation Plan

## Overview

This document tracks the step-by-step implementation plan for adding object count support for core and extended native Kubernetes resources to the ClusterResourceQuota (CRQ) controller. The plan is broken down into the smallest actionable steps, with clarifications and requirements noted.

## Rules & Principles

- **No assumptions:** Ask for clarification when needed.
- **No core structure changes:** Follow existing project patterns.
- **No code repetition:** Reuse logic and abstractions.
- **Interfaces & structures:** Use interfaces for testability and maintainability.
- **No custom CRDs:** Only native/extended Kubernetes resources.
- **Testing:** Strict unit and e2e coverage.

## Step-by-Step Plan

### 0. Planning & Tracking

- [ ] Create this implementation plan as a Markdown file in the repo (`docs/object-count-feature-plan.md`).
- [ ] Update the plan as work progresses.

### 1. Resource Inventory

### Native Kubernetes Resource Types (for object counting)

- Pod
- PersistentVolumeClaim (PVC)
- Service
- ConfigMap
- Secret
- ReplicationController
- ReplicaSet
- Deployment
- StatefulSet
- DaemonSet
- Job
- CronJob
- EndpointSlice
- Endpoints
- Ingress
- ServiceAccount
- Lease
- Event

#### Extended/Subtype Resources

##### Explicit Extended Resource Types (for object counting)

- services.loadbalancers (Service objects with type=LoadBalancer)
- services.nodeports (Service objects with type=NodePort)
- ingresses (Ingress objects)
- services.externalname (Service objects with type=ExternalName)
- services.clusterip (Service objects with type=ClusterIP)

**Note:** These keys are for quota specification and status reporting. The implementation should ensure that subtype counts do not exceed the total for the parent resource (e.g., services.loadbalancers ≤ services).

**Note:** Custom CRDs are explicitly excluded.

User will trim or adjust this list as needed before implementation.

### 2. API & CRD Changes

- [ ] Update the CRQ API spec to allow specifying object count quotas for the selected resources.
- [ ] Update CRD YAML in Helm chart.
- [ ] Update Helm chart documentation (`README.md.gotmpl`, `values.yaml`).
- [ ] Run `make generate` and `make helm-docs`.

### 3. Controller Logic

- [ ] Implement logic to count objects for each supported resource type across namespaces.
- [ ] Update reconciliation loop to aggregate and update usage in CRQ status.
- [ ] Ensure code is modular and testable (interfaces, etc.).
- [ ] Add/extend unit tests for new logic.

### 4. Admission Webhook

- [ ] Update webhook to validate create/update requests for supported resources against CRQ limits.
- [ ] Implement live calculation to block/prevent over-quota deployments.
- [ ] Add/extend unit tests for webhook logic.

### 5. Controller Watches

- [ ] Update controller to watch for changes to the new resource types (controller-gen).
- [ ] Ensure watches are efficient and follow project patterns.

### 6. Helm Chart & Docs

- [ ] Update Helm chart templates and documentation for new CRQ fields and behaviors.
- [ ] Ensure CRD, RBAC, and values are up-to-date.
- [ ] Run `make helm-docs` and `make helm-lint`.

### 7. Testing

- [ ] Add/extend unit tests for all new logic (controller, webhook, utils).
- [ ] List use-cases for e2e tests in this plan (before implementation).
- [ ] Implement e2e tests for all critical scenarios.
- [ ] Ensure all tests pass (`make test`, `make test-e2e`).

### 8. Review & Finalization

- [ ] Review code for adherence to project principles.
- [ ] Update this plan and project documentation as needed.
- [ ] Prepare for PR review (conventional commits, detailed PR description).

## Clarifications Needed

### Clarifications (2025-09-18)

- **Resource List:** Support all native Kubernetes resources (core and extended). User will trim the list as needed after initial implementation.
- **Scope:** Object counting is both namespace-scoped and cluster-scoped. Blocking logic is based on cluster-scope, as in current CRQ usage.
- **Extended Resources:** For resources like Service type=LoadBalancer, support both total and subtype (e.g., max services, max loadbalancers). Validation should ensure subtypes do not exceed total.
- **Active Objects:** Only active objects are counted. For pods, this is already implemented (terminal pods are excluded). For other resources, count all existing objects unless otherwise specified by Kubernetes conventions.
- **Performance:** No special performance or scalability requirements for large clusters.

---

## Use-Case List for E2E Tests (to be completed before implementation)

- [ ] To be filled after resource list and API finalized.

---
*This plan will be updated as the implementation progresses and as clarifications are provided.*
