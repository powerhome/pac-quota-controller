# 📝 Pull Request

## 📖 Description

<!--
Provide a detailed description of your changes:
- What problem does this solve?
- How did you solve it?
- What are the key changes?
- Why did you choose this approach?
-->

## 📚 Documentation

<!--
Describe documentation changes:
- Updated README
- Added new docs
- Updated API documentation
- Updated Helm chart docs

-->

- [ ] 📖 Updated documentation
- [ ] 📄 Added examples

### Testing & Validation

- [ ] ✅ Added or updated tests for new/changed behavior
- [ ] 📚 Updated documentation and Helm chart if APIs, CRDs, or configuration changed
- [ ] 🔧 Ran pre-commit hooks to validate changes:
  - [ ] `pre-commit run --all-files` (runs formatting, linting, generation, and unit tests)
  - [ ] `make test-e2e` (ensure e2e tests pass - not included in pre-commit)

### Versioning & Release

- [ ] 📊 Bumped chart `version` in `charts/pac-quota-controller/Chart.yaml` if making a chart change
- [ ] 🏷️ Bumped `appVersion` in `charts/pac-quota-controller/Chart.yaml` if releasing a new application version
- [ ] 🔖 Tagged the release as `vX.Y.Z` for app releases or `chart-vX.Y.Z` for chart-only releases (if applicable)

> **📝 Note:** Chart and app versioning are manual. See CONTRIBUTING.md for details on how to release.
> **🐙 Note:** For Helm chart installation, use GHCR as an OCI registry. See README for instructions.
