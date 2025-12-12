# Contributing to Nuclei Operator

Thank you for your interest in contributing to the Nuclei Operator! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Code Style Guidelines](#code-style-guidelines)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Reporting Issues](#reporting-issues)

## Code of Conduct

This project follows the [Kubernetes Code of Conduct](https://github.com/kubernetes/community/blob/master/code-of-conduct.md). By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

Before you begin, ensure you have the following installed:

- **Go 1.24+**: [Download Go](https://golang.org/dl/)
- **Docker**: [Install Docker](https://docs.docker.com/get-docker/)
- **kubectl**: [Install kubectl](https://kubernetes.io/docs/tasks/tools/)
- **Kind** (for local testing): [Install Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- **Make**: Usually pre-installed on Linux/macOS

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/<your-username>/nuclei-operator.git
   cd nuclei-operator
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/mortenolsen/nuclei-operator.git
   ```

## Development Setup

### Install Dependencies

```bash
# Download Go modules
go mod download

# Install development tools
make controller-gen
make kustomize
make envtest
```

### Set Up Local Cluster

```bash
# Create a Kind cluster for development
kind create cluster --name nuclei-dev

# Verify cluster is running
kubectl cluster-info
```

### Install CRDs

```bash
# Generate and install CRDs
make manifests
make install
```

### Run the Operator Locally

```bash
# Run outside the cluster (for development)
make run
```

## Making Changes

### Branch Naming

Use descriptive branch names:

- `feature/add-webhook-support` - New features
- `fix/scan-timeout-issue` - Bug fixes
- `docs/update-api-reference` - Documentation updates
- `refactor/scanner-interface` - Code refactoring

### Commit Messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

**Examples:**
```
feat(scanner): add support for custom nuclei templates

fix(controller): handle nil pointer in ingress reconciler

docs(readme): update installation instructions
```

### Keeping Your Fork Updated

```bash
# Fetch upstream changes
git fetch upstream

# Rebase your branch on upstream/main
git checkout main
git rebase upstream/main

# Update your feature branch
git checkout feature/your-feature
git rebase main
```

## Code Style Guidelines

### Go Code Style

- Follow the [Effective Go](https://golang.org/doc/effective_go) guidelines
- Use `gofmt` for formatting (run `make fmt`)
- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use meaningful variable and function names
- Add comments for exported functions and types

### Linting

```bash
# Run the linter
make lint

# Auto-fix linting issues where possible
make lint-fix
```

### Code Organization

- **api/**: CRD type definitions
- **cmd/**: Main entry points
- **internal/controller/**: Reconciliation logic
- **internal/scanner/**: Nuclei scanner implementation
- **config/**: Kubernetes manifests

### Error Handling

- Always handle errors explicitly
- Use `fmt.Errorf` with `%w` for error wrapping
- Log errors with appropriate context

```go
if err != nil {
    return fmt.Errorf("failed to create NucleiScan: %w", err)
}
```

### Logging

Use structured logging with controller-runtime's logger:

```go
log := logf.FromContext(ctx)
log.Info("Processing resource", "name", resource.Name, "namespace", resource.Namespace)
log.Error(err, "Failed to reconcile", "resource", req.NamespacedName)
```

## Testing

### Running Tests

```bash
# Run unit tests
make test

# Run tests with coverage
make test
go tool cover -html=cover.out

# Run end-to-end tests
make test-e2e
```

### Writing Tests

- Write unit tests for all new functionality
- Use table-driven tests where appropriate
- Mock external dependencies
- Test both success and error cases

**Example test structure:**

```go
var _ = Describe("IngressController", func() {
    Context("When reconciling an Ingress", func() {
        It("Should create a NucleiScan", func() {
            // Test implementation
        })

        It("Should handle missing Ingress gracefully", func() {
            // Test implementation
        })
    })
})
```

### Test Coverage

- Aim for at least 70% code coverage
- Focus on testing business logic and edge cases
- Don't test generated code or simple getters/setters

## Pull Request Process

### Before Submitting

1. **Update your branch:**
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. **Run all checks:**
   ```bash
   make manifests generate fmt vet lint test
   ```

3. **Update documentation** if needed

4. **Add/update tests** for your changes

### Submitting a PR

1. Push your branch to your fork:
   ```bash
   git push origin feature/your-feature
   ```

2. Create a Pull Request on GitHub

3. Fill out the PR template with:
   - Description of changes
   - Related issues
   - Testing performed
   - Breaking changes (if any)

### PR Review Process

1. **Automated checks** must pass (CI/CD pipeline)
2. **Code review** by at least one maintainer
3. **Address feedback** and update your PR
4. **Squash commits** if requested
5. **Merge** once approved

### PR Checklist

- [ ] Code follows the project's style guidelines
- [ ] Tests added/updated for the changes
- [ ] Documentation updated if needed
- [ ] Commit messages follow conventional commits
- [ ] All CI checks pass
- [ ] PR description is complete

## Reporting Issues

### Bug Reports

When reporting bugs, include:

1. **Description**: Clear description of the issue
2. **Steps to reproduce**: Minimal steps to reproduce
3. **Expected behavior**: What you expected to happen
4. **Actual behavior**: What actually happened
5. **Environment**:
   - Kubernetes version
   - Operator version
   - Cloud provider (if applicable)
6. **Logs**: Relevant operator logs
7. **Resources**: Related Kubernetes resources (sanitized)

### Feature Requests

When requesting features, include:

1. **Problem statement**: What problem does this solve?
2. **Proposed solution**: How should it work?
3. **Alternatives considered**: Other approaches you've thought of
4. **Additional context**: Any other relevant information

### Security Issues

For security vulnerabilities, please **do not** open a public issue. Instead, email the maintainers directly or use GitHub's private vulnerability reporting feature.

## Development Tips

### Useful Make Targets

```bash
make help           # Show all available targets
make manifests      # Generate CRD manifests
make generate       # Generate code (DeepCopy, etc.)
make fmt            # Format code
make vet            # Run go vet
make lint           # Run linter
make test           # Run tests
make build          # Build binary
make run            # Run locally
make docker-build   # Build container image
make install        # Install CRDs
make deploy         # Deploy to cluster
```

### Debugging

```bash
# Increase log verbosity
go run ./cmd/main.go --zap-log-level=debug

# View controller logs
kubectl logs -f -n nuclei-operator-system deployment/nuclei-operator-controller-manager

# Debug with delve
dlv debug ./cmd/main.go
```

### IDE Setup

**VS Code:**
- Install the Go extension
- Enable `gopls` for language server
- Configure `golangci-lint` as the linter

**GoLand:**
- Import the project as a Go module
- Configure the Go SDK
- Enable `golangci-lint` integration

## Getting Help

- **Documentation**: Check the [README](README.md) and [docs/](docs/) directory
- **Issues**: Search existing [GitHub Issues](https://github.com/mortenolsen/nuclei-operator/issues)
- **Discussions**: Use [GitHub Discussions](https://github.com/mortenolsen/nuclei-operator/discussions) for questions

## Recognition

Contributors will be recognized in:
- The project's README
- Release notes for significant contributions
- GitHub's contributor graph

Thank you for contributing to the Nuclei Operator!