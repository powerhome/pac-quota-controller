# 📝 Pull Request

## 📋 Summary

<!-- Provide a brief summary of your changes. What does this PR do? -->

## 🎯 What type of PR is this?

<!-- Check all that apply -->

- [ ] 🐛 Bug fix (non-breaking change which fixes an issue)
- [ ] ✨ New feature (non-breaking change which adds functionality)
- [ ] 💥 Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] 📚 Documentation update
- [ ] 🧹 Code cleanup/refactoring
- [ ] 🔧 Build/CI changes
- [ ] ⚡ Performance improvement
- [ ] 🛡️ Security fix
- [ ] 🧪 Test improvements

## 🔗 Related Issues

<!-- Link to related issues. Use "Closes #123" or "Fixes #123" to auto-close issues when merged -->

Closes #
Relates to #

## 📖 Description

<!-- 
Provide a detailed description of your changes:
- What problem does this solve?
- How did you solve it?
- What are the key changes?
- Why did you choose this approach?
-->

## 🔄 Changes Made

<!-- 
List the key changes in this PR. Be specific about what was added, modified, or removed.

-->

-
-
-

## 🧪 Testing

<!-- 
Describe how you tested your changes:
- What tests did you add/modify?
- Manual testing steps
- Edge cases considered

-->

### Unit Tests

- [ ] Added unit tests for new functionality
- [ ] Updated existing unit tests
- [ ] All unit tests pass (`make test`)

### E2E Tests

- [ ] Added E2E tests for new functionality
- [ ] Updated existing E2E tests
- [ ] All E2E tests pass (`make test-e2e`)

### Manual Testing

<!-- Describe manual testing steps -->

## 📸 Screenshots/Demo

<!-- 
If applicable, add screenshots or demo output showing your changes in action.
For CLI changes, include before/after command output.
For UI changes, include screenshots.
For new features, consider adding a brief demo or example usage.
-->

## 🔧 Configuration Changes

<!-- 
Describe any configuration changes:
- New environment variables
- Changed default values
- New Helm chart values
- CRD schema changes
- RBAC changes

-->

- [ ] 📊 Helm chart changes (describe below)
- [ ] 🔐 RBAC changes (describe below)
- [ ] 🏗️ CRD schema changes (describe below)
- [ ] ⚙️ Configuration changes (describe below)

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
- [ ] 🔄 Updated Helm chart docs (`make helm-docs`)

## ⚠️ Breaking Changes

<!-- 
Describe any breaking changes and migration steps:
- API changes
- Configuration changes
- Behavior changes
- Upgrade considerations

If no breaking changes, you can remove this section or write "None"
-->

None

## 🔄 Migration Guide

<!-- 
If there are breaking changes, provide step-by-step migration instructions:
1. Step one
2. Step two
3. etc.

If no migration needed, you can remove this section or write "No migration required"
-->

No migration required

## ✅ Pre-submission Checklist

<!-- Check all items before submitting your PR -->

### Code Quality

- [ ] 📝 Code follows project conventions and style guidelines
- [ ] 🧹 Code is clean and well-commented
- [ ] 🔍 Self-reviewed the code changes
- [ ] 📋 PR is focused and addresses a single concern (avoid unrelated changes)
- [ ] ✍️ Clear, descriptive title and summary
- [ ] 🔗 Referenced related issues (if any) in the description
- [ ] 🗂️ Commits are clean and meaningful (squashed/fixuped as needed)

### Testing & Validation

- [ ] ✅ Added or updated tests for new/changed behavior
- [ ] 📚 Updated documentation and Helm chart if APIs, CRDs, or configuration changed
- [ ] 🔧 Ran the following commands and committed any changes:
  - [ ] `make lint` (fix all lint issues)
  - [ ] `make manifests` (commit CRD changes)
  - [ ] `make generate` (commit generated code)
  - [ ] `make test test-e2e` (ensure all tests pass)
  - [ ] `make helm-docs` (update Helm chart docs if needed)
  - [ ] `make helm-lint` (ensure Helm chart is valid)
  - [ ] `make helm-test` (ensure Helm chart installs in Kind)

### Versioning & Release

- [ ] 📊 Bumped chart `version` in `charts/pac-quota-controller/Chart.yaml` if making a chart change
- [ ] 🏷️ Bumped `appVersion` in `charts/pac-quota-controller/Chart.yaml` if releasing a new application version
- [ ] 🔖 Tagged the release as `vX.Y.Z` for app releases or `chart-vX.Y.Z` for chart-only releases (if applicable)

> **📝 Note:** Chart and app versioning are manual. See CONTRIBUTING.md for details on how to release.
> **🐙 Note:** For Helm chart installation, use GHCR as an OCI registry. See README for instructions.

## 🤝 Reviewer Notes

<!-- 
Anything specific you want reviewers to focus on?
Areas where you're unsure and want feedback?
Performance considerations?
Security implications?

-->

## 📝 Additional Notes

<!-- Add any extra context, screenshots, or information here. -->
