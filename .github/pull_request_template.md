# Pull Request Checklist & Best Practices

Before submitting your PR, please:

- [ ] Ensure your PR is focused and addresses a single concern (avoid unrelated changes)
- [ ] Write a clear, descriptive title and summary for your PR
- [ ] Reference related issues (if any) in the description
- [ ] Keep commits clean and meaningful (squash/fixup as needed)
- [ ] Add or update tests for new/changed behavior
- [ ] Update documentation and Helm chart if you change APIs, CRDs, or configuration
- [ ] Run the following commands and commit any changes:
  - `make lint` (fix all lint issues)
  - `make manifests` (commit CRD changes)
  - `make generate` (commit generated code)
  - `make test` and/or `make test-e2e` (ensure all tests pass)
  - `make helm-docs` (update Helm chart docs if needed)
  - `make helm-lint` (ensure Helm chart is valid)
  - `make helm-test` (ensure Helm chart installs in Kind)
- [ ] I have bumped the chart `version` in `charts/pac-quota-controller/Chart.yaml` if making a chart change
- [ ] I have bumped the `appVersion` in `charts/pac-quota-controller/Chart.yaml` if releasing a new application version
- [ ] I have tagged the release as `vX.Y.Z` for app releases or `chart-vX.Y.Z` for chart-only releases

> **Note:** Chart and app versioning are manual. See CONTRIBUTING.md for details on how to release.

## Additional Notes (optional)

<!-- Add any extra context, screenshots, or information here. -->
