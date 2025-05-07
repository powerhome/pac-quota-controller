# Contributing to PAC Resource Sharing Validation Webhook

Thank you for your interest in contributing to our project! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please be respectful and considerate of others.

## Development Process

1. Create a new branch from `main`
2. Make your changes
3. Run tests and linting
4. Commit your changes
5. Push to your fork
6. Create a Pull Request

## Branch Naming Convention

Use the following format for branch names:

- `feature/` for new features
- `bugfix/` for bug fixes
- `hotfix/` for urgent fixes
- `docs/` for documentation changes
- `crd/` for CRD-related changes

Example: `crd/add-resource-quota-validation`

## Commit Messages

Follow the Conventional Commits specification:

```text
<type>[optional scope]: <description>

[optional body]

[optional footer]
```

Types:

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Adding or modifying tests
- `chore`: Maintenance tasks
- `crd`: Changes to CRDs

Example:

```text
feat(crd): add ClusterResourceQuota validation

- Add validation for resource quotas across namespaces
- Update CRD schema with new fields
- Add tests for quota validation

Closes #123
```

## Code Style

- Follow Go standard formatting
- Use meaningful variable and function names
- Write clear and concise comments
- Keep functions focused and single-purpose
- Use proper error handling patterns
- Follow Kubernetes API conventions for CRDs

## Testing

- Write unit tests for new functionality
- Maintain good test coverage
- Include integration tests where appropriate
- Document test requirements
- Test CRD validation and webhook behavior
- Test resource quota enforcement

## CRD Development Guidelines

When working with the ClusterResourceQuota CRD:

1. Update the CRD schema in `charts/pac-quota-controller/crds/`
2. Add validation logic in the webhook
3. Update documentation
4. Add example manifests
5. Test CRD creation and updates
6. Test resource quota enforcement

## Pull Request Process

1. Update the README.md with details of changes if needed
2. Update the documentation if needed
3. The PR must pass all CI checks
4. The PR must be reviewed by at least one maintainer
5. The PR must be approved before merging

## Environment Setup

1. Install Go 1.24.2 or later
1. Install dependencies:

   ```bash
   make deps
   ```

1. Set up local Kubernetes cluster:

   ```bash
   make kind-create
   ```

## Development Workflow

1. Create a new branch
2. Make your changes
3. Run tests:

   ```bash
   make test
   ```

4. Run linter:

   ```bash
   make lint
   ```

5. Test CRD changes:

   ```bash
   make kind-deploy
   kubectl apply -f charts/pac-quota-controller/crds/
   ```

6. Commit your changes
7. Push to your fork
8. Create a Pull Request

## Documentation

- Keep documentation up to date
- Document all public APIs
- Include examples where appropriate
- Update README.md for significant changes
- Document CRD usage and examples
- Document resource quota configuration

## Questions?

If you have any questions, please contact the development team.
