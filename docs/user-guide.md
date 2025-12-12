# User Guide

This guide provides detailed instructions for using the Nuclei Operator to automate security scanning of your Kubernetes applications.

## Table of Contents

- [Introduction](#introduction)
- [Installation](#installation)
- [Basic Usage](#basic-usage)
- [Scanner Architecture](#scanner-architecture)
- [Annotation-Based Configuration](#annotation-based-configuration)
- [Configuration Options](#configuration-options)
- [Working with Ingress Resources](#working-with-ingress-resources)
- [Working with VirtualService Resources](#working-with-virtualservice-resources)
- [Scheduled Scans](#scheduled-scans)
- [Viewing Scan Results](#viewing-scan-results)
- [Best Practices](#best-practices)
- [Security Considerations](#security-considerations)
- [Troubleshooting](#troubleshooting)

---

## Introduction

The Nuclei Operator automates security scanning by watching for Kubernetes Ingress and Istio VirtualService resources. When a new resource is created or updated, the operator automatically:

1. Extracts target URLs from the resource
2. Creates a NucleiScan custom resource
3. Creates a Kubernetes Job to execute the Nuclei security scan in an isolated pod
4. Stores the results in the NucleiScan status

This enables continuous security monitoring of your web applications without manual intervention.

The operator uses a **pod-based scanning architecture** where each scan runs in its own isolated Kubernetes Job, providing better scalability, reliability, and resource control.

---

## Installation

### Prerequisites

Before installing the Nuclei Operator, ensure you have:

- A Kubernetes cluster (v1.26 or later)
- `kubectl` configured to access your cluster
- Cluster admin permissions (for CRD installation)

### Quick Installation

```bash
# Clone the repository
git clone https://github.com/mortenolsen/nuclei-operator.git
cd nuclei-operator

# Install CRDs
make install

# Deploy the operator
make deploy IMG=ghcr.io/mortenolsen/nuclei-operator:latest
```

### Verify Installation

```bash
# Check that the operator is running
kubectl get pods -n nuclei-operator-system

# Verify CRDs are installed
kubectl get crd nucleiscans.nuclei.homelab.mortenolsen.pro
```

Expected output:
```
NAME                                              CREATED AT
nucleiscans.nuclei.homelab.mortenolsen.pro        2024-01-15T10:00:00Z
```

---

## Basic Usage

### Automatic Scanning via Ingress

The simplest way to use the operator is to create an Ingress resource. The operator will automatically create a NucleiScan.

**Step 1: Create an Ingress**

```yaml
# my-app-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
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
kubectl apply -f my-app-ingress.yaml
```

**Step 2: View the Created NucleiScan**

```bash
# List NucleiScans
kubectl get nucleiscans

# View details
kubectl describe nucleiscan my-app-scan
```

### Manual NucleiScan Creation

You can also create NucleiScan resources manually for more control:

```yaml
# manual-scan.yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: manual-security-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: my-app
    namespace: default
    uid: "your-ingress-uid"  # Get with: kubectl get ingress my-app -o jsonpath='{.metadata.uid}'
  targets:
    - https://myapp.example.com
  severity:
    - high
    - critical
```

```bash
kubectl apply -f manual-scan.yaml
```

---

## Scanner Architecture

The nuclei-operator uses a pod-based scanning architecture for improved scalability and reliability:

1. **Operator Pod**: Manages NucleiScan resources and creates scanner jobs
2. **Scanner Jobs**: Kubernetes Jobs that execute nuclei scans in isolated pods
3. **Direct Status Updates**: Scanner pods update NucleiScan status directly via the Kubernetes API

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                          │
│                                                                     │
│  ┌──────────────────┐     ┌──────────────────────────────────────┐ │
│  │  Operator Pod    │     │           Scanner Jobs               │ │
│  │                  │     │                                      │ │
│  │  ┌────────────┐  │     │  ┌─────────┐  ┌─────────┐           │ │
│  │  │ Controller │──┼─────┼─▶│  Job 1  │  │  Job 2  │  ...      │ │
│  │  │  Manager   │  │     │  │(Scanner)│  │(Scanner)│           │ │
│  │  └────────────┘  │     │  └────┬────┘  └────┬────┘           │ │
│  │        │         │     │       │            │                 │ │
│  └────────┼─────────┘     └───────┼────────────┼─────────────────┘ │
│           │                       │            │                   │
│           ▼                       ▼            ▼                   │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    Kubernetes API Server                      │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │  │
│  │  │ NucleiScan  │  │ NucleiScan  │  │ NucleiScan  │  ...      │  │
│  │  │  Resource   │  │  Resource   │  │  Resource   │           │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Benefits

- **Scalability**: Multiple scans can run concurrently across the cluster
- **Isolation**: Each scan runs in its own pod with dedicated resources
- **Reliability**: Scans survive operator restarts
- **Resource Control**: Per-scan resource limits and quotas
- **Observability**: Individual pod logs for each scan

### Scanner Configuration

Configure scanner behavior via Helm values:

```yaml
scanner:
  # Enable scanner RBAC resources
  enabled: true
  
  # Scanner image (defaults to operator image)
  image: "ghcr.io/morten-olsen/nuclei-operator:latest"
  
  # Default scan timeout
  timeout: "30m"
  
  # Maximum concurrent scan jobs
  maxConcurrent: 5
  
  # Job TTL after completion (seconds)
  ttlAfterFinished: 3600
  
  # Default resource requirements for scanner pods
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      cpu: "1"
      memory: 1Gi
  
  # Default templates to use
  defaultTemplates: []
  
  # Default severity filter
  defaultSeverity: []
```

### Per-Scan Scanner Configuration

You can override scanner settings for individual scans using the `scannerConfig` field in the NucleiScan spec:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: custom-scan
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: my-ingress
    namespace: default
    uid: "abc123"
  targets:
    - https://example.com
  scannerConfig:
    # Override scanner image
    image: "custom-scanner:latest"
    # Override timeout
    timeout: "1h"
    # Custom resource requirements
    resources:
      requests:
        cpu: 200m
        memory: 512Mi
      limits:
        cpu: "2"
        memory: 2Gi
    # Node selector for scanner pod
    nodeSelector:
      node-type: scanner
    # Tolerations for scanner pod
    tolerations:
      - key: "scanner"
        operator: "Equal"
        value: "true"
        effect: "NoSchedule"
```

---

## Annotation-Based Configuration

You can configure scanning behavior for individual Ingress or VirtualService resources using annotations.

### Supported Annotations

| Annotation | Type | Default | Description |
|------------|------|---------|-------------|
| `nuclei.homelab.mortenolsen.pro/enabled` | bool | `true` | Enable/disable scanning for this resource |
| `nuclei.homelab.mortenolsen.pro/templates` | string | - | Comma-separated list of template paths or tags |
| `nuclei.homelab.mortenolsen.pro/severity` | string | - | Comma-separated severity filter: info,low,medium,high,critical |
| `nuclei.homelab.mortenolsen.pro/schedule` | string | - | Cron schedule for periodic scans |
| `nuclei.homelab.mortenolsen.pro/timeout` | duration | `30m` | Scan timeout |
| `nuclei.homelab.mortenolsen.pro/scanner-image` | string | - | Override scanner image |
| `nuclei.homelab.mortenolsen.pro/exclude-templates` | string | - | Templates to exclude |
| `nuclei.homelab.mortenolsen.pro/tags` | string | - | Template tags to include |
| `nuclei.homelab.mortenolsen.pro/exclude-tags` | string | - | Template tags to exclude |

### Example Annotated Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  annotations:
    nuclei.homelab.mortenolsen.pro/enabled: "true"
    nuclei.homelab.mortenolsen.pro/severity: "medium,high,critical"
    nuclei.homelab.mortenolsen.pro/schedule: "0 2 * * *"
    nuclei.homelab.mortenolsen.pro/templates: "cves/,vulnerabilities/"
spec:
  rules:
    - host: myapp.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: myapp
                port:
                  number: 80
```

### Example Annotated VirtualService

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: myapp-vs
  annotations:
    nuclei.homelab.mortenolsen.pro/enabled: "true"
    nuclei.homelab.mortenolsen.pro/severity: "high,critical"
    nuclei.homelab.mortenolsen.pro/timeout: "1h"
    nuclei.homelab.mortenolsen.pro/tags: "cve,oast"
spec:
  hosts:
    - myapp.example.com
  gateways:
    - my-gateway
  http:
    - route:
        - destination:
            host: myapp
            port:
              number: 80
```

### Disabling Scanning

To disable scanning for a specific resource:

```yaml
metadata:
  annotations:
    nuclei.homelab.mortenolsen.pro/enabled: "false"
```

This is useful when you want to temporarily exclude certain resources from scanning without removing them from the cluster.

### Annotation Precedence

When both annotations and NucleiScan spec fields are present, the following precedence applies:

1. **NucleiScan spec fields** (highest priority) - Direct configuration in the NucleiScan resource
2. **Annotations** - Configuration from the source Ingress/VirtualService
3. **Helm values** - Default configuration from the operator deployment
4. **Built-in defaults** (lowest priority) - Hardcoded defaults in the operator

---

## Configuration Options

### Severity Filtering

Filter scan results by severity level:

```yaml
spec:
  severity:
    - info      # Informational findings
    - low       # Low severity
    - medium    # Medium severity
    - high      # High severity
    - critical  # Critical severity
```

**Recommended configurations:**

| Use Case | Severity Levels |
|----------|-----------------|
| Production monitoring | `medium`, `high`, `critical` |
| Security audit | `info`, `low`, `medium`, `high`, `critical` |
| Quick check | `high`, `critical` |

### Template Selection

Specify which Nuclei templates to use:

```yaml
spec:
  templates:
    - cves/                    # CVE checks
    - vulnerabilities/         # General vulnerabilities
    - exposures/              # Exposed services/files
    - misconfiguration/       # Misconfigurations
    - default-logins/         # Default credentials
    - takeovers/              # Subdomain takeovers
```

**Template categories:**

| Category | Description |
|----------|-------------|
| `cves/` | Known CVE vulnerabilities |
| `vulnerabilities/` | General vulnerability checks |
| `exposures/` | Exposed sensitive files and services |
| `misconfiguration/` | Security misconfigurations |
| `default-logins/` | Default credential checks |
| `takeovers/` | Subdomain takeover vulnerabilities |
| `technologies/` | Technology detection |
| `ssl/` | SSL/TLS issues |

### Environment Variables

Configure the operator using environment variables in the deployment:

```yaml
# In config/manager/manager.yaml
env:
  - name: NUCLEI_BINARY_PATH
    value: "/usr/local/bin/nuclei"
  - name: NUCLEI_TEMPLATES_PATH
    value: "/nuclei-templates"
  - name: NUCLEI_TIMEOUT
    value: "30m"
```

| Variable | Description | Default |
|----------|-------------|---------|
| `NUCLEI_BINARY_PATH` | Path to Nuclei binary | `nuclei` |
| `NUCLEI_TEMPLATES_PATH` | Custom templates directory | (Nuclei default) |
| `NUCLEI_TIMEOUT` | Scan timeout duration | `30m` |

---

## Working with Ingress Resources

### URL Extraction

The operator extracts URLs from Ingress resources based on:

1. **TLS configuration**: Hosts in `spec.tls[].hosts` are scanned with HTTPS
2. **Rules**: Hosts in `spec.rules[].host` are scanned
3. **Paths**: Individual paths from `spec.rules[].http.paths[]` are included

**Example Ingress:**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: multi-path-app
spec:
  tls:
    - hosts:
        - secure.example.com
      secretName: secure-tls
  rules:
    - host: secure.example.com
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: api-service
                port:
                  number: 8080
          - path: /admin
            pathType: Prefix
            backend:
              service:
                name: admin-service
                port:
                  number: 8081
    - host: public.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: public-service
                port:
                  number: 80
```

**Extracted URLs:**
- `https://secure.example.com/api`
- `https://secure.example.com/admin`
- `http://public.example.com/`

### Naming Convention

NucleiScan resources are named based on the Ingress:

```
<ingress-name>-scan
```

For example, an Ingress named `my-app` creates a NucleiScan named `my-app-scan`.

### Owner References

The operator sets owner references on NucleiScan resources, enabling:

- **Automatic cleanup**: When an Ingress is deleted, its NucleiScan is also deleted
- **Relationship tracking**: Easy identification of which Ingress created which scan

---

## Working with VirtualService Resources

### Prerequisites

VirtualService support requires Istio to be installed in your cluster.

### URL Extraction

The operator extracts URLs from VirtualService resources based on:

1. **Hosts**: All hosts in `spec.hosts[]`
2. **HTTP routes**: Paths from `spec.http[].match[].uri`

**Example VirtualService:**

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: my-istio-app
  namespace: default
spec:
  hosts:
    - myapp.example.com
  gateways:
    - my-gateway
  http:
    - match:
        - uri:
            prefix: /api
      route:
        - destination:
            host: api-service
            port:
              number: 8080
    - match:
        - uri:
            prefix: /web
      route:
        - destination:
            host: web-service
            port:
              number: 80
```

**Extracted URLs:**
- `https://myapp.example.com/api`
- `https://myapp.example.com/web`

### Naming Convention

NucleiScan resources for VirtualServices follow the same pattern:

```
<virtualservice-name>-scan
```

---

## Scheduled Scans

### Enabling Scheduled Scans

Add a `schedule` field to run scans periodically:

```yaml
spec:
  schedule: "@every 24h"
```

### Schedule Formats

**Simplified interval format:**

| Format | Description |
|--------|-------------|
| `@every 1h` | Every hour |
| `@every 6h` | Every 6 hours |
| `@every 24h` | Every 24 hours |
| `@every 168h` | Every week |

**Standard cron format:**

```
┌───────────── minute (0 - 59)
│ ┌───────────── hour (0 - 23)
│ │ ┌───────────── day of month (1 - 31)
│ │ │ ┌───────────── month (1 - 12)
│ │ │ │ ┌───────────── day of week (0 - 6) (Sunday = 0)
│ │ │ │ │
* * * * *
```

**Examples:**

| Schedule | Description |
|----------|-------------|
| `0 2 * * *` | Daily at 2:00 AM |
| `0 */6 * * *` | Every 6 hours |
| `0 3 * * 0` | Weekly on Sunday at 3:00 AM |
| `0 0 1 * *` | Monthly on the 1st at midnight |

### Suspending Scheduled Scans

Temporarily pause scheduled scans without deleting the resource:

```yaml
spec:
  schedule: "@every 24h"
  suspend: true  # Scans are paused
```

To resume:

```bash
kubectl patch nucleiscan my-scan -p '{"spec":{"suspend":false}}'
```

### Viewing Next Scheduled Time

```bash
kubectl get nucleiscan my-scan -o jsonpath='{.status.nextScheduledTime}'
```

---

## Viewing Scan Results

### List All Scans

```bash
# Basic listing
kubectl get nucleiscans

# With additional details
kubectl get nucleiscans -o wide

# In all namespaces
kubectl get nucleiscans -A
```

### View Scan Details

```bash
# Full details
kubectl describe nucleiscan my-app-scan

# JSON output
kubectl get nucleiscan my-app-scan -o json

# YAML output
kubectl get nucleiscan my-app-scan -o yaml
```

### Extract Specific Information

```bash
# Get scan phase
kubectl get nucleiscan my-app-scan -o jsonpath='{.status.phase}'

# Get total findings count
kubectl get nucleiscan my-app-scan -o jsonpath='{.status.summary.totalFindings}'

# Get findings by severity
kubectl get nucleiscan my-app-scan -o jsonpath='{.status.summary.findingsBySeverity}'

# Get all findings
kubectl get nucleiscan my-app-scan -o jsonpath='{.status.findings}' | jq .

# Get critical findings only
kubectl get nucleiscan my-app-scan -o json | jq '.status.findings[] | select(.severity == "critical")'
```

### Export Results

```bash
# Export to JSON file
kubectl get nucleiscan my-app-scan -o json > scan-results.json

# Export findings only
kubectl get nucleiscan my-app-scan -o jsonpath='{.status.findings}' > findings.json

# Export as CSV (using jq)
kubectl get nucleiscan my-app-scan -o json | jq -r '.status.findings[] | [.templateId, .severity, .host, .matchedAt] | @csv' > findings.csv
```

### Watch Scan Progress

```bash
# Watch scan status changes
kubectl get nucleiscans -w

# Watch specific scan
watch kubectl get nucleiscan my-app-scan
```

---

## Best Practices

### 1. Use Severity Filters in Production

Avoid scanning for `info` level findings in production to reduce noise:

```yaml
spec:
  severity:
    - medium
    - high
    - critical
```

### 2. Schedule Scans During Off-Peak Hours

Run scheduled scans during low-traffic periods:

```yaml
spec:
  schedule: "0 3 * * *"  # 3 AM daily
```

### 3. Use Namespaces for Organization

Organize scans by environment or team:

```bash
# Development scans
kubectl get nucleiscans -n development

# Production scans
kubectl get nucleiscans -n production
```

### 4. Label Your Resources

Add labels for better organization and filtering:

```yaml
metadata:
  labels:
    environment: production
    team: security
    compliance: pci-dss
```

```bash
# Filter by label
kubectl get nucleiscans -l environment=production
```

### 5. Monitor Scan Failures

Set up alerts for failed scans:

```bash
# Find failed scans
kubectl get nucleiscans --field-selector status.phase=Failed
```

### 6. Regular Template Updates

Keep Nuclei templates updated for the latest vulnerability checks. The operator uses the templates bundled in the container image.

### 7. Resource Limits

Ensure the operator has appropriate resource limits:

```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

---

## Security Considerations

### Network Access

The operator needs network access to scan targets. Consider:

1. **Network Policies**: Ensure the operator can reach scan targets
2. **Egress Rules**: Allow outbound traffic to target hosts
3. **Internal vs External**: Be aware of scanning internal vs external endpoints

### RBAC Permissions

The operator requires specific permissions:

- **Read** Ingress and VirtualService resources
- **Full control** over NucleiScan resources
- **Create** events for logging

Review the RBAC configuration in `config/rbac/role.yaml`.

### Scan Impact

Consider the impact of security scans:

1. **Rate Limiting**: Nuclei respects rate limits, but be aware of target capacity
2. **WAF/IDS Alerts**: Scans may trigger security alerts on targets
3. **Logging**: Scan traffic will appear in target access logs

### Sensitive Data

Scan results may contain sensitive information:

1. **Access Control**: Restrict access to NucleiScan resources
2. **Data Retention**: Consider cleanup policies for old scan results
3. **Audit Logging**: Enable Kubernetes audit logging for compliance

### Container Security

The operator container includes the Nuclei binary:

1. **Image Updates**: Regularly update the operator image
2. **Vulnerability Scanning**: Scan the operator image itself
3. **Non-root User**: The operator runs as a non-root user

---

## Troubleshooting

### Scan Stuck in Pending

**Symptoms:** NucleiScan remains in `Pending` phase

**Solutions:**

1. Check operator logs:
   ```bash
   kubectl logs -n nuclei-operator-system deployment/nuclei-operator-controller-manager
   ```

2. Verify the operator is running:
   ```bash
   kubectl get pods -n nuclei-operator-system
   ```

3. Check for resource constraints:
   ```bash
   kubectl describe pod -n nuclei-operator-system -l control-plane=controller-manager
   ```

### Scan Failed

**Symptoms:** NucleiScan shows `Failed` phase

**Solutions:**

1. Check the error message:
   ```bash
   kubectl get nucleiscan my-scan -o jsonpath='{.status.lastError}'
   ```

2. Common errors:
   - **Timeout**: Increase timeout or reduce targets
   - **Network error**: Check connectivity to targets
   - **Binary not found**: Verify Nuclei is installed in the container

3. Retry the scan:
   ```bash
   # Trigger a new scan by updating the spec
   kubectl patch nucleiscan my-scan -p '{"spec":{"targets":["https://example.com"]}}'
   ```

### No NucleiScan Created for Ingress

**Symptoms:** Ingress exists but no NucleiScan is created

**Solutions:**

1. Verify the Ingress has hosts defined:
   ```bash
   kubectl get ingress my-ingress -o jsonpath='{.spec.rules[*].host}'
   ```

2. Check operator RBAC:
   ```bash
   kubectl auth can-i list ingresses --as=system:serviceaccount:nuclei-operator-system:nuclei-operator-controller-manager
   ```

3. Check operator logs for errors:
   ```bash
   kubectl logs -n nuclei-operator-system deployment/nuclei-operator-controller-manager | grep -i error
   ```

### Empty Scan Results

**Symptoms:** Scan completes but has no findings

**Possible causes:**

1. **Targets not accessible**: Verify targets are reachable from the operator pod
2. **Severity filter too strict**: Try including more severity levels
3. **Templates not matching**: Ensure templates are appropriate for the targets

**Verification:**

```bash
# Test connectivity from operator pod
kubectl exec -n nuclei-operator-system deployment/nuclei-operator-controller-manager -- curl -I https://your-target.com
```

### High Resource Usage

**Symptoms:** Operator consuming excessive CPU/memory

**Solutions:**

1. Reduce concurrent scans by adjusting controller concurrency
2. Increase resource limits:
   ```yaml
   resources:
     limits:
       cpu: 1000m
       memory: 1Gi
   ```
3. Reduce scan scope (fewer targets or templates)

### Scheduled Scans Not Running

**Symptoms:** Scheduled scan time passes but scan doesn't start

**Solutions:**

1. Verify scan is not suspended:
   ```bash
   kubectl get nucleiscan my-scan -o jsonpath='{.spec.suspend}'
   ```

2. Check the schedule format:
   ```bash
   kubectl get nucleiscan my-scan -o jsonpath='{.spec.schedule}'
   ```

3. Verify next scheduled time:
   ```bash
   kubectl get nucleiscan my-scan -o jsonpath='{.status.nextScheduledTime}'
   ```

### Getting Help

If you're still experiencing issues:

1. Check the [GitHub Issues](https://github.com/mortenolsen/nuclei-operator/issues)
2. Review the [Architecture documentation](../ARCHITECTURE.md)
3. Enable debug logging and collect logs
4. Open a new issue with:
   - Kubernetes version
   - Operator version
   - Relevant resource YAML (sanitized)
   - Operator logs
   - Steps to reproduce