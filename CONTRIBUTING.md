# Contributing to pac-quota-controller

Thank you for your interest in contributing! Please follow these guidelines to help us review and accept your changes quickly.

## Prerequisites

- Install [pre-commit](https://pre-commit.com/) and run `pre-commit install` after cloning the repo.
- Ensure you have Go, Docker, Kind, Helm, and other required tools installed (see project README).

## Pull Request Checklist

Before submitting your PR, please:

- Ensure your PR is focused and addresses a single concern (avoid unrelated changes)
- Write a clear, descriptive title and summary for your PR
- Reference related issues (if any) in the description
- Keep commits clean and meaningful (squash/fixup as needed)
- Add or update tests for new/changed behavior
- Update documentation and Helm chart if you change APIs, CRDs, or configuration
- Run the following commands and commit any changes:
  - `make lint` (fix all lint issues)
  - `make manifests` (commit CRD changes)
  - `make generate` (commit generated code)
  - `make test` and/or `make test-e2e` (ensure all tests pass)
  - `make helm-docs` (update Helm chart docs if needed)
  - `make helm-lint` (ensure Helm chart is valid)
  - `make helm-test` (ensure Helm chart installs in Kind)

## Helm Chart Maintenance

- The Helm chart is maintained manually. If you make changes to CRDs, APIs, or configuration options, update the chart in `charts/pac-quota-controller`.

## Good Practices

- Keep PRs small and focused for easier review.
- Add tests for new features or bug fixes.
- Update documentation and examples as needed.
- Use clear commit messages.

## Releasing

To release a new version:

- For an application release (new container image and chart):
  1. Bump the application version in your code and update `appVersion` in `charts/pac-quota-controller/Chart.yaml` to match.
  2. Optionally bump the chart `version` if you want to release a new chart version.
  3. Commit and merge your changes to `main`.
  4. Tag the release with `vX.Y.Z` (e.g., `v1.2.3`).
  5. Push the tag to GitHub. This will trigger the pipeline to build and publish the container image and the chart to GHCR as an OCI artifact.

- For a chart-only release (no new container image):
  1. Bump the chart `version` in `charts/pac-quota-controller/Chart.yaml`.
  2. Commit and merge your changes to `main`.
  3. Tag the release with `chart-vX.Y.Z` (e.g., `chart-v1.2.4`).
  4. Push the tag to GitHub. This will trigger the pipeline to publish the chart to GHCR as an OCI artifact.

> **Note:** Always update `appVersion` in `Chart.yaml` to match the container image version for app releases. For chart-only releases, leave `appVersion` unchanged.
> **Note:** For Helm chart installation, use GHCR as an OCI registry. See README for instructions.

---

Thank you for helping improve pac-quota-controller!
