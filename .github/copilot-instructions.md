# GitHub Copilot Custom Instructions

## Project Overview

The `pac-quota-controller` is a Kubernetes controller that extends Kubernetes with a `ClusterResourceQuota` custom resource. This allows defining resource quotas that span multiple namespaces. The project aims to provide robust, well-tested, and performant quota management capabilities.

## Key Technologies and Structure

- **Language:** Go
- **Framework:** Kubebuilder
- **Deployment:** Helm (manual chart, no Kustomize for deployment artifacts)
- **CI/CD:** GitHub Actions
- **Testing:** Ginkgo/Gomega for e2e and unit tests.

## Development Principles & Preferences

1. **Kubebuilder First:**
    - Adhere to the Kubebuilder project structure and conventions.
    - APIs and Webhooks should primarily be scaffolded using `kubebuilder create api` and `kubebuilder create webhook`. Modifications to the generated code are expected to fit the project's needs, but the initial scaffolding should come from the CLI.
2. **Kubernetes Native APIs:**
    - Whenever possible, utilize native Kubernetes APIs and libraries (e.g., `k8s.io/api`, `k8s.io/apimachinery`, `sigs.k8s.io/controller-runtime`).
    - Avoid unnecessary third-party libraries if a native solution exists and is suitable.
3. **No Kustomize for Deployment Artifacts:**
    - The project **does not** use Kustomize for generating or managing deployment YAMLs (CRDs, RBAC, deployment, etc.).
    - All deployment manifests are managed manually within the Helm chart located at `charts/pac-quota-controller/`.

4. **Helm Chart is Source of Truth:**
    - The Helm chart is the single source of truth for deploying the `pac-quota-controller`.
    - Pay close attention to how changes in Go code (e.g., new CRD fields, RBAC permissions, webhook configurations) might impact the Helm chart. Updates to the chart (templates, `values.yaml`, `Chart.yaml`, README) are often necessary.
    - CRDs are installed via a Helm hook or directly from the chart, not via `kustomize build config/crd | kubectl apply -f -`.
5. **Documentation is Crucial:**
    - **Code Comments:** Write clear and concise Go comments, especially for public APIs and complex logic.
    - **Helm Chart Documentation:** Ensure `charts/pac-quota-controller/README.md.gotmpl` and `values.yaml` are well-documented and user-friendly. Also, remember to run `make helm-docs` to keep the documentation updated
    - **Commit Messages:** Follow conventional commit guidelines.
    - **Pull Request Descriptions:** Provide detailed explanations of changes.

6. **Testing Rigor:**
    - **Unit Tests:** Strive for comprehensive unit test coverage for business logic, especially in controllers and webhooks.
    - **E2E Tests:** Ensure robust end-to-end tests for critical user workflows. E2E tests are located in `test/e2e/`.
    - All tests must pass before merging. `make test` and `make test-e2e`.

7. **Linting and Formatting:**
    - Code must pass lint checks: `make lint`.
    - Code must be formatted: `make fmt`.
    - Follow Go best practices and the Kubernetes Go project structure where it doesn't conflict with Kubebuilder's structure.
      - Go code lines should not be longer than 120 characters.
8. **Performance:**
    - Be mindful of performance implications, especially in the controller's reconciliation loop and webhook handlers.
    - Optimize API calls and resource usage.

9. **Makefile Driven:**
    - The `Makefile` is central to the development workflow (building, testing, linting, deploying). Ensure changes are compatible with Makefile targets.

10. **Cert-Manager:**
    - Cert-manager is used for webhook certificate management. The Helm chart includes options to install cert-manager or use an existing installation.

11. **Instruction Maintenance:** After interactions where new project conventions, critical file paths, or development preferences are established or significantly clarified, I (GitHub Copilot) should be mindful of these changes. If these changes are persistent and generally applicable, I should suggest or, if requested, directly update this `copilot-instructions.md` file to ensure it remains current and accurately reflects the project's context. The user may also explicitly request updates to this file.

## Workflow for Changes

1. **Understand the Request:** Clarify requirements if needed.
2. **Identify Affected Components:**
    - Go code (API types, controller logic, webhook logic).
    - Helm chart (templates, values, CRDs, RBAC).
    - Documentation (code comments, READMEs).
    - Tests (unit, e2e).
3. **Implement Changes:**
    - Follow Kubebuilder for scaffolding if creating new APIs/webhooks.
    - Manually update Helm chart files.
    - Write/update tests.
4. **Verify:**
    - `make fmt`
    - `make lint`
    - `make test`
    - `make test-e2e` (if relevant)
    - `make docker-build` (if controller changes)
    - `make helm-lint`
    - `make helm-package`
5. **Iterate based on feedback.**

## Important Files/Directories

- `api/v1alpha1/`: CRD definitions.
- `internal/controller/`: Controller logic.
- `pkg/webhook/`: Webhook logic.
- `charts/pac-quota-controller/`: Helm chart.
  - `charts/pac-quota-controller/templates/`: Helm templates.
  - `charts/pac-quota-controller/values.yaml`: Default Helm values.
  - `charts/pac-quota-controller/README.md.gotmpl`: Helm chart documentation template.
- `Makefile`: Build and test automation.
- `test/e2e/`: End-to-end tests.

By following these guidelines, Copilot can provide more relevant and accurate assistance for this project.

## Pull Request Review Guidelines for Copilot

When you request my assistance in reviewing a Pull Request, providing the following information and focusing on these areas will help me give more effective feedback:

**1. Information to Provide in the PR or Request:**

*   **Clear PR Description:**
    *   What problem does this PR solve?
    *   What are the main changes?
    *   How were these changes tested (unit, E2E, manual steps)?
*   **Linked Issues:** Reference any GitHub issues this PR addresses.
*   **Specific Focus Areas:** Let me know if there are particular files, functions, or logic you'd like me to pay close attention to.
*   **Self-Review Notes:** Any observations, trade-offs made, or areas of uncertainty you've already identified.

**2. Key Aspects for Copilot to Check During Review:**

*   **Adherence to Project Principles (as outlined in these instructions):**
    *   **Kubebuilder Conventions:** For new/modified APIs and webhooks.
    *   **Kubernetes Native APIs:** Prefer native libraries.
    *   **No Kustomize for Deployment:** Ensure deployment artifacts are managed via Helm.
    *   **Helm Chart Integrity:**
        *   Are CRD changes reflected in `charts/pac-quota-controller/templates/crd/`?
        *   Are RBAC changes updated in `charts/pac-quota-controller/templates/rbac/`?
        *   Are `values.yaml` and `Chart.yaml` updated if necessary?
        *   Is `charts/pac-quota-controller/README.md.gotmpl` updated, and has `make helm-docs` been run?
*   **Code Quality & Go Best Practices:**
    *   Clarity, readability, and maintainability.
    *   Proper error handling.
    *   Effective use of Go idioms.
    *   Adherence to `make fmt` and `make lint`.
*   **Testing Rigor:**
    *   Are there new or updated unit tests for the changed logic in controllers and webhooks?
    *   Are there new or updated E2E tests for critical user workflows or significant changes?
    *   Do tests cover edge cases and failure scenarios?
*   **Documentation:**
    *   Are Go comments clear, especially for public APIs and complex logic?
    *   Is Helm chart documentation (`README.md.gotmpl`, `values.yaml`) comprehensive and up-to-date?
*   **Makefile Integration:**
    *   Are changes compatible with existing Makefile targets?
    *   Are any new Makefile targets or modifications needed and correctly implemented?
*   **Performance Considerations:**
    *   Any obvious performance bottlenecks in reconciliation loops or webhook handlers?
    *   Efficient use of API calls and resource management.
*   **API Changes (for `ClusterResourceQuota` CRD):**
    *   Are changes to `api/v1alpha1/` well-justified and backward-compatible if possible?
    *   Are `zz_generated.deepcopy.go` files updated via `make generate`?
*   **Security (Basic Checks):**
    *   Any obvious misconfigurations or insecure practices (note: I am not a security expert).

**3. How Copilot Should Present Feedback:**

*   Provide specific code suggestions where applicable.
*   Clearly state which guideline or best practice a suggestion relates to.
*   Ask clarifying questions if the intent or implementation is unclear.
*   Identify areas that might benefit from further testing or documentation.
