# Nuclei Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/mortenolsen/nuclei-operator)](https://goreportcard.com/report/github.com/mortenolsen/nuclei-operator)

A Kubernetes operator that automates security scanning of web applications exposed through Kubernetes Ingress resources and Istio VirtualService CRDs using [Nuclei](https://github.com/projectdiscovery/nuclei), a fast and customizable vulnerability scanner.

## Overview

The Nuclei Operator watches for Ingress and VirtualService resources in your Kubernetes cluster and automatically creates security scans for the exposed endpoints. Scan results are stored in custom `NucleiScan` resources, making it easy to track and monitor vulnerabilities across your infrastructure.

### Key Features

- **Automatic Discovery**: Watches Kubernetes Ingress and Istio VirtualService resources for new endpoints
- **Automated Scanning**: Automatically creates and runs Nuclei scans when new endpoints are discovered
- **Scheduled Scans**: Support for cron-based scheduled rescanning
- **Automatic Rescans**: Automatically rescans when results become stale (configurable age threshold)
- **Target Availability Checking**: Waits for targets to be available before scanning
- **Stale Scan Recovery**: Automatically resets interrupted scans on operator restart
- **Flexible Configuration**: Configurable templates, severity filters, and scan options
- **Native Kubernetes Integration**: Results stored as Kubernetes custom resources
- **Owner References**: Automatic cleanup when source resources are deleted

### How It Works

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│    Ingress /    │────▶│  Nuclei Operator │────▶│   NucleiScan    │
│ VirtualService  │     │   Controllers    │     │    Resource     │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                │                        │
                                ▼                        ▼
                        ┌─────────────────┐     ┌─────────────────┐
                        │  Nuclei Engine  │────▶│  Scan Results   │
                        │    (Scanner)    │     │   (Findings)    │
                        └─────────────────┘     └─────────────────┘
```

1. **Watch**: The operator watches for Ingress and VirtualService resources
2. **Extract**: URLs are extracted from the resource specifications
3. **Create**: A NucleiScan custom resource is created with the target URLs
4. **Scan**: The Nuclei scanner executes security scans against the targets
5. **Store**: Results are stored in the NucleiScan status for easy access

## Prerequisites

- Kubernetes cluster v1.26+
- kubectl configured to access your cluster
- [Istio](https://istio.io/) (optional, required for VirtualService support)
- Container runtime (Docker, containerd, etc.)

## Installation

### Using kubectl/kustomize

1. **Install the CRDs:**

```bash
make install
```

2. **Deploy the operator:**

```bash
# Using the default image
make deploy IMG=ghcr.io/mortenolsen/nuclei-operator:latest

# Or build and deploy your own image
make docker-build docker-push IMG=<your-registry>/nuclei-operator:tag
make deploy IMG=<your-registry>/nuclei-operator:tag
```

### Using a Single YAML File

Generate and apply a consolidated installation manifest:

```bash
# Generate the installer
make build-installer IMG=<your-registry>/nuclei-operator:tag

# Apply to your cluster
kubectl apply -f dist/install.yaml
```

### Building from Source

```bash
# Clone the repository
git clone https://github.com/mortenolsen/nuclei-operator.git
cd nuclei-operator

# Build the binary
make build

# Build the container image
make docker-build IMG=<your-registry>/nuclei-operator:tag

# Push to your registry
make docker-push IMG=<your-registry>/nuclei-operator:tag
```

## Quick Start

### 1. Deploy the Operator

```bash
make deploy IMG=ghcr.io/mortenolsen/nuclei-operator:latest
```

### 2. Create an Ingress Resource

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app-ingress
  namespace: default
spec:
  tls:
    - hosts:
        - myapp.example.com
      secretName: myapp-tls
  rules:
    - host: myapp.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app
                port:
                  number: 80
```

```bash
kubectl apply -f my-ingress.yaml
```

### 3. View the NucleiScan Results

The operator automatically creates a NucleiScan resource:

```bash
# List all NucleiScans
kubectl get nucleiscans

# View detailed scan results
kubectl describe nucleiscan my-app-ingress-scan

# Get scan findings in JSON format
kubectl get nucleiscan my-app-ingress-scan -o jsonpath='{.status.findings}'
```

Example output:

```
NAME                  PHASE       FINDINGS   SOURCE    AGE
my-app-ingress-scan   Completed   3          Ingress   5m
```

## Configuration

### Environment Variables

The operator can be configured using the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `NUCLEI_BINARY_PATH` | Path to the Nuclei binary | `nuclei` |
| `NUCLEI_TEMPLATES_PATH` | Path to Nuclei templates directory | (uses Nuclei default) |
| `NUCLEI_TIMEOUT` | Default scan timeout | `30m` |
| `NUCLEI_RESCAN_AGE` | Maximum age of scan results before automatic rescan | `168h` (1 week) |
| `NUCLEI_BACKOFF_INITIAL` | Initial retry interval for target availability checks | `10s` |
| `NUCLEI_BACKOFF_MAX` | Maximum retry interval for target availability checks | `10m` |
| `NUCLEI_BACKOFF_MULTIPLIER` | Multiplier for exponential backoff | `2.0` |

### Automatic Rescan Behavior

The operator automatically rescans targets when:

1. **Stale Results**: Scan results are older than `NUCLEI_RESCAN_AGE` (default: 1 week)
2. **Operator Restart**: Any scans that were in "Running" state when the operator restarted are automatically re-queued
3. **Spec Changes**: When the NucleiScan spec is modified

### Target Availability with Exponential Backoff

Before running a scan, the operator checks if targets are reachable:

- Uses HTTP HEAD requests to verify target availability
- If no targets are available, the scan waits and retries with **exponential backoff**
- Backoff sequence with defaults: 10s → 20s → 40s → 80s → 160s → 320s → 600s (max)
- Scans proceed with available targets even if some are unreachable
- Any HTTP response (including 4xx/5xx) is considered "available" - the service is responding
- Retry count is tracked in `status.retryCount` and reset when targets become available

**Backoff Configuration Example:**

```bash
# Set initial retry to 5 seconds, max to 5 minutes, multiplier to 1.5
export NUCLEI_BACKOFF_INITIAL=5s
export NUCLEI_BACKOFF_MAX=5m
export NUCLEI_BACKOFF_MULTIPLIER=1.5
```

### NucleiScan Spec Options

| Field | Type | Description |
|-------|------|-------------|
| `sourceRef` | SourceReference | Reference to the source Ingress/VirtualService |
| `targets` | []string | List of URLs to scan |
| `templates` | []string | Nuclei templates to use (optional) |
| `severity` | []string | Severity filter: info, low, medium, high, critical |
| `schedule` | string | Cron schedule for periodic scans (optional) |
| `suspend` | bool | Suspend scheduled scans |

### Example NucleiScan

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: my-security-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: my-ingress
    namespace: default
    uid: "abc123"
  targets:
    - https://myapp.example.com
    - https://api.example.com
  severity:
    - medium
    - high
    - critical
  templates:
    - cves/
    - vulnerabilities/
  schedule: "@every 24h"
  suspend: false
```

## CRD Reference

### NucleiScan

The `NucleiScan` custom resource represents a security scan configuration and its results.

**Short names:** `ns`, `nscan`

**Print columns:**
- `Phase`: Current scan phase (Pending, Running, Completed, Failed)
- `Findings`: Total number of findings
- `Source`: Source resource kind (Ingress/VirtualService)
- `Age`: Resource age

For detailed API documentation, see [docs/api.md](docs/api.md).

## Development

### Prerequisites

- Go 1.24+
- Docker or Podman
- kubectl
- Access to a Kubernetes cluster (kind, minikube, or remote)

### Building the Project

```bash
# Generate manifests and code
make manifests generate

# Build the binary
make build

# Run tests
make test

# Run linter
make lint
```

### Running Locally

```bash
# Install CRDs
make install

# Run the operator locally (outside the cluster)
make run
```

### Running Tests

```bash
# Unit tests
make test

# End-to-end tests (requires Kind)
make test-e2e
```

### Project Structure

```
nuclei-operator/
├── api/v1alpha1/           # CRD type definitions
├── cmd/                    # Main entry point
├── config/                 # Kubernetes manifests
│   ├── crd/               # CRD definitions
│   ├── default/           # Default kustomization
│   ├── manager/           # Operator deployment
│   ├── rbac/              # RBAC configuration
│   └── samples/           # Example resources
├── internal/
│   ├── controller/        # Reconciliation logic
│   └── scanner/           # Nuclei scan execution
└── test/                  # Test suites
```

## Troubleshooting

### Common Issues

#### Operator not creating NucleiScan resources

1. Check operator logs:
   ```bash
   kubectl logs -n nuclei-operator-system deployment/nuclei-operator-controller-manager
   ```

2. Verify RBAC permissions:
   ```bash
   kubectl auth can-i list ingresses --as=system:serviceaccount:nuclei-operator-system:nuclei-operator-controller-manager
   ```

3. Ensure the Ingress has valid hosts defined

#### Scans stuck in Pending/Running state

1. Check if Nuclei binary is available in the container
2. Verify network connectivity to scan targets
3. Check for timeout issues in operator logs

#### No findings in completed scans

1. Verify targets are accessible from the operator pod
2. Check if severity filters are too restrictive
3. Ensure Nuclei templates are properly configured

### Debugging Tips

```bash
# View operator logs
kubectl logs -f -n nuclei-operator-system deployment/nuclei-operator-controller-manager

# Check NucleiScan status
kubectl describe nucleiscan <scan-name>

# View events
kubectl get events --field-selector involvedObject.kind=NucleiScan

# Check operator metrics
kubectl port-forward -n nuclei-operator-system svc/nuclei-operator-controller-manager-metrics-service 8080:8080
curl localhost:8080/metrics
```

## Uninstallation

```bash
# Remove all NucleiScan resources
kubectl delete nucleiscans --all --all-namespaces

# Undeploy the operator
make undeploy

# Remove CRDs
make uninstall
```

## Documentation

- [Architecture](ARCHITECTURE.md) - Detailed architecture documentation
- [API Reference](docs/api.md) - Complete CRD API reference
- [User Guide](docs/user-guide.md) - Detailed usage instructions
- [Contributing](CONTRIBUTING.md) - Contribution guidelines

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

## Acknowledgments

- [Nuclei](https://github.com/projectdiscovery/nuclei) - The vulnerability scanner powering this operator
- [Kubebuilder](https://book.kubebuilder.io/) - Framework used to build this operator
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) - Kubernetes controller library
