# GitHub Copilot Instructions

## Repository Overview

This repository contains the PAC Resource Sharing Validation Webhook, an internal project for Powerhouse. It provides a webhook service for validating resource sharing requests. The project is built in Go and follows modern best practices and tools.

## Technical Stack

- **Programming Language**: Go 1.24.2
- **Libraries/Frameworks**:
  - Cobra for CLI commands
  - Viper for configuration management
  - Autoenv for environment management
  - Docker for containerization

## Project Structure

```text
.
├── cmd/                    # CLI commands
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── handlers/         # HTTP handlers
│   ├── models/           # Data models
│   └── services/         # Business logic
├── pkg/                   # Public packages
├── api/                   # API definitions
├── docs/                  # Documentation
├── scripts/               # Utility scripts
└── test/                  # Test data and fixtures
```

## Environment Variables

All environment variables are prefixed with `PAC_QUOTA_CONTROLLER_`:

- `PORT`: Service port
- `LOG_LEVEL`: Logging level
- `ENV`: Environment (dev/staging/prod)

## Coding Standards

1. Follow Go best practices and conventions.
2. Use meaningful variable and function names.
3. Write clear and concise comments.
4. Keep functions focused and single-purpose.
5. Use proper error handling patterns.
6. Maintain modular and testable code.
7. Use semantic versioning.
8. Follow security best practices.

## Documentation

1. Keep README.md up to date with usage instructions and a high-level project description.
2. Maintain CONTRIBUTING.md with development guidelines.
3. Document all public APIs with examples where appropriate.
4. Maintain a changelog for significant changes.
5. Document environment variables clearly.

## Security

1. Never hardcode sensitive information.
2. Use environment variables for configuration.
3. Keep dependencies updated.
4. Use proper authentication and authorization mechanisms.

## Testing

1. Write unit tests for new functionality.
2. Maintain good test coverage.
3. Include integration tests where appropriate.
4. Keep test data separate from production data.
5. Document test requirements clearly.
