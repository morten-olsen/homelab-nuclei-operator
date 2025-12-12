# API Reference

This document provides a complete reference for the Nuclei Operator Custom Resource Definitions (CRDs).

## Table of Contents

- [NucleiScan](#nucleiscan)
  - [Metadata](#metadata)
  - [Spec](#spec)
  - [Status](#status)
- [Type Definitions](#type-definitions)
  - [SourceReference](#sourcereference)
  - [ScannerConfig](#scannerconfig)
  - [JobReference](#jobreference)
  - [Finding](#finding)
  - [ScanSummary](#scansummary)
  - [ScanPhase](#scanphase)
- [Examples](#examples)

---

## NucleiScan

`NucleiScan` is the primary custom resource for the Nuclei Operator. It represents a security scan configuration and stores the scan results.

**API Group:** `nuclei.homelab.mortenolsen.pro`  
**API Version:** `v1alpha1`  
**Kind:** `NucleiScan`  
**Short Names:** `ns`, `nscan`

### Metadata

Standard Kubernetes metadata fields apply. The operator automatically sets owner references when creating NucleiScan resources from Ingress or VirtualService resources.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique name within the namespace |
| `namespace` | string | Namespace where the resource resides |
| `labels` | map[string]string | Labels for organizing and selecting resources |
| `annotations` | map[string]string | Annotations for storing additional metadata |
| `ownerReferences` | []OwnerReference | References to owner resources (set automatically) |

### Spec

The `spec` field defines the desired state of the NucleiScan.

```yaml
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: my-ingress
    namespace: default
    uid: "abc123-def456"
  targets:
    - https://example.com
    - https://api.example.com
  templates:
    - cves/
    - vulnerabilities/
  severity:
    - medium
    - high
    - critical
  schedule: "@every 24h"
  suspend: false
  scannerConfig:
    image: "custom-scanner:latest"
    timeout: "1h"
    resources:
      requests:
        cpu: 200m
        memory: 512Mi
      limits:
        cpu: "1"
        memory: 1Gi
```

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sourceRef` | [SourceReference](#sourcereference) | Yes | Reference to the source Ingress or VirtualService |
| `targets` | []string | Yes | List of URLs to scan (minimum 1) |
| `templates` | []string | No | Nuclei templates to use. If empty, uses default templates |
| `severity` | []string | No | Severity filter. Valid values: `info`, `low`, `medium`, `high`, `critical` |
| `schedule` | string | No | Cron schedule for periodic rescanning |
| `suspend` | bool | No | When true, suspends scheduled scans |
| `scannerConfig` | [ScannerConfig](#scannerconfig) | No | Scanner-specific configuration overrides |

#### Schedule Format

The `schedule` field supports two formats:

1. **Simplified interval format:**
   - `@every <duration>` - e.g., `@every 24h`, `@every 6h`, `@every 30m`

2. **Standard cron format:**
   - `* * * * *` - minute, hour, day of month, month, day of week
   - Examples:
     - `0 2 * * *` - Daily at 2:00 AM
     - `0 */6 * * *` - Every 6 hours
     - `0 3 * * 0` - Weekly on Sunday at 3:00 AM

### Status

The `status` field contains the observed state of the NucleiScan, including scan results.

```yaml
status:
  phase: Completed
  conditions:
    - type: Ready
      status: "True"
      reason: ScanCompleted
      message: "Scan completed with 3 findings"
      lastTransitionTime: "2024-01-15T10:35:00Z"
    - type: ScanActive
      status: "False"
      reason: ScanCompleted
      message: "Scan completed successfully"
      lastTransitionTime: "2024-01-15T10:35:00Z"
  lastScanTime: "2024-01-15T10:30:00Z"
  completionTime: "2024-01-15T10:35:00Z"
  nextScheduledTime: "2024-01-16T10:30:00Z"
  scanStartTime: "2024-01-15T10:30:05Z"
  jobRef:
    name: my-app-scan-abc123
    uid: "job-uid-12345"
    podName: my-app-scan-abc123-xyz
    startTime: "2024-01-15T10:30:00Z"
  summary:
    totalFindings: 3
    findingsBySeverity:
      medium: 2
      high: 1
    targetsScanned: 2
    durationSeconds: 300
  findings:
    - templateId: CVE-2021-44228
      templateName: Apache Log4j RCE
      severity: critical
      type: http
      host: https://example.com
      matchedAt: https://example.com/api/login
      timestamp: "2024-01-15T10:32:00Z"
  lastError: ""
  observedGeneration: 1
  retryCount: 0
```

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | [ScanPhase](#scanphase) | Current phase of the scan |
| `conditions` | []Condition | Standard Kubernetes conditions |
| `lastScanTime` | *Time | When the last scan was initiated |
| `completionTime` | *Time | When the last scan completed |
| `nextScheduledTime` | *Time | When the next scheduled scan will run |
| `scanStartTime` | *Time | When the scanner pod actually started scanning |
| `jobRef` | *[JobReference](#jobreference) | Reference to the current or last scanner job |
| `summary` | *[ScanSummary](#scansummary) | Aggregated scan statistics |
| `findings` | [][Finding](#finding) | Array of scan results |
| `lastError` | string | Error message if the scan failed |
| `observedGeneration` | int64 | Generation observed by the controller |
| `retryCount` | int | Number of consecutive availability check retries |
| `lastRetryTime` | *Time | When the last availability check retry occurred |

#### Conditions

The operator maintains the following condition types:

| Type | Description |
|------|-------------|
| `Ready` | Indicates whether the scan has completed successfully |
| `ScanActive` | Indicates whether a scan is currently running |

**Condition Reasons:**

| Reason | Description |
|--------|-------------|
| `ScanPending` | Scan is waiting to start |
| `ScanRunning` | Scan is currently in progress |
| `ScanCompleted` | Scan completed successfully |
| `ScanFailed` | Scan failed with an error |
| `ScanSuspended` | Scan is suspended |

---

## Type Definitions

### SourceReference

`SourceReference` identifies the Ingress or VirtualService that triggered the scan.

```go
type SourceReference struct {
    APIVersion string `json:"apiVersion"`
    Kind       string `json:"kind"`
    Name       string `json:"name"`
    Namespace  string `json:"namespace"`
    UID        string `json:"uid"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiVersion` | string | Yes | API version of the source resource (e.g., `networking.k8s.io/v1`) |
| `kind` | string | Yes | Kind of the source resource. Valid values: `Ingress`, `VirtualService` |
| `name` | string | Yes | Name of the source resource |
| `namespace` | string | Yes | Namespace of the source resource |
| `uid` | string | Yes | UID of the source resource |

### ScannerConfig

`ScannerConfig` defines scanner-specific configuration that can override default settings.

```go
type ScannerConfig struct {
    Image        string                       `json:"image,omitempty"`
    Resources    *corev1.ResourceRequirements `json:"resources,omitempty"`
    Timeout      *metav1.Duration             `json:"timeout,omitempty"`
    TemplateURLs []string                     `json:"templateURLs,omitempty"`
    NodeSelector map[string]string            `json:"nodeSelector,omitempty"`
    Tolerations  []corev1.Toleration          `json:"tolerations,omitempty"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image` | string | No | Override the default scanner image |
| `resources` | ResourceRequirements | No | Resource requirements for the scanner pod |
| `timeout` | Duration | No | Override the default scan timeout |
| `templateURLs` | []string | No | Additional template repositories to clone |
| `nodeSelector` | map[string]string | No | Node selector for scanner pod scheduling |
| `tolerations` | []Toleration | No | Tolerations for scanner pod scheduling |

**Example:**

```yaml
scannerConfig:
  image: "ghcr.io/custom/scanner:v1.0.0"
  timeout: "1h"
  resources:
    requests:
      cpu: 200m
      memory: 512Mi
    limits:
      cpu: "2"
      memory: 2Gi
  nodeSelector:
    node-type: scanner
  tolerations:
    - key: "dedicated"
      operator: "Equal"
      value: "scanner"
      effect: "NoSchedule"
```

### JobReference

`JobReference` contains information about the scanner job for tracking and debugging.

```go
type JobReference struct {
    Name      string       `json:"name"`
    UID       string       `json:"uid"`
    PodName   string       `json:"podName,omitempty"`
    StartTime *metav1.Time `json:"startTime,omitempty"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the Kubernetes Job |
| `uid` | string | Yes | UID of the Job |
| `podName` | string | No | Name of the scanner pod (for log retrieval) |
| `startTime` | *Time | No | When the job was created |

**Example:**

```yaml
jobRef:
  name: my-scan-abc123
  uid: "12345678-1234-1234-1234-123456789012"
  podName: my-scan-abc123-xyz
  startTime: "2024-01-15T10:30:00Z"
```

### Finding

`Finding` represents a single vulnerability or issue discovered during a scan.

```go
type Finding struct {
    TemplateID       string              `json:"templateId"`
    TemplateName     string              `json:"templateName,omitempty"`
    Severity         string              `json:"severity"`
    Type             string              `json:"type,omitempty"`
    Host             string              `json:"host"`
    MatchedAt        string              `json:"matchedAt,omitempty"`
    ExtractedResults []string            `json:"extractedResults,omitempty"`
    Description      string              `json:"description,omitempty"`
    Reference        []string            `json:"reference,omitempty"`
    Tags             []string            `json:"tags,omitempty"`
    Timestamp        metav1.Time         `json:"timestamp"`
    Metadata         *runtime.RawExtension `json:"metadata,omitempty"`
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `templateId` | string | Yes | Nuclei template identifier (e.g., `CVE-2021-44228`) |
| `templateName` | string | No | Human-readable template name |
| `severity` | string | Yes | Severity level: `info`, `low`, `medium`, `high`, `critical` |
| `type` | string | No | Finding type: `http`, `dns`, `ssl`, `tcp`, etc. |
| `host` | string | Yes | Target host that was scanned |
| `matchedAt` | string | No | Specific URL or endpoint where the issue was found |
| `extractedResults` | []string | No | Data extracted by the template |
| `description` | string | No | Detailed description of the finding |
| `reference` | []string | No | URLs to additional information |
| `tags` | []string | No | Tags associated with the finding |
| `timestamp` | Time | Yes | When the finding was discovered |
| `metadata` | RawExtension | No | Additional template metadata (preserved as JSON) |

### ScanSummary

`ScanSummary` provides aggregated statistics about the scan.

```go
type ScanSummary struct {
    TotalFindings      int            `json:"totalFindings"`
    FindingsBySeverity map[string]int `json:"findingsBySeverity,omitempty"`
    TargetsScanned     int            `json:"targetsScanned"`
    DurationSeconds    int64          `json:"durationSeconds,omitempty"`
}
```

| Field | Type | Description |
|-------|------|-------------|
| `totalFindings` | int | Total number of findings |
| `findingsBySeverity` | map[string]int | Breakdown of findings by severity level |
| `targetsScanned` | int | Number of targets that were scanned |
| `durationSeconds` | int64 | Duration of the scan in seconds |

### ScanPhase

`ScanPhase` represents the current phase of the scan lifecycle.

```go
type ScanPhase string

const (
    ScanPhasePending   ScanPhase = "Pending"
    ScanPhaseRunning   ScanPhase = "Running"
    ScanPhaseCompleted ScanPhase = "Completed"
    ScanPhaseFailed    ScanPhase = "Failed"
)
```

| Phase | Description |
|-------|-------------|
| `Pending` | Scan is waiting to be executed |
| `Running` | Scan is currently in progress |
| `Completed` | Scan finished successfully |
| `Failed` | Scan failed with an error |

---

## Examples

### Basic NucleiScan

A minimal NucleiScan configuration:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: basic-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: my-ingress
    namespace: default
    uid: "12345678-1234-1234-1234-123456789012"
  targets:
    - https://example.com
```

### NucleiScan with Severity Filter

Scan only for medium, high, and critical vulnerabilities:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: severity-filtered-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: production-ingress
    namespace: production
    uid: "abcdef12-3456-7890-abcd-ef1234567890"
  targets:
    - https://api.example.com
    - https://www.example.com
  severity:
    - medium
    - high
    - critical
```

### NucleiScan with Specific Templates

Use specific Nuclei template categories:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: cve-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: app-ingress
    namespace: default
    uid: "fedcba98-7654-3210-fedc-ba9876543210"
  targets:
    - https://app.example.com
  templates:
    - cves/
    - vulnerabilities/
    - exposures/
  severity:
    - high
    - critical
```

### Scheduled NucleiScan

Run a scan daily at 2:00 AM:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: daily-security-scan
  namespace: default
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: main-ingress
    namespace: default
    uid: "11111111-2222-3333-4444-555555555555"
  targets:
    - https://example.com
    - https://api.example.com
  severity:
    - medium
    - high
    - critical
  schedule: "0 2 * * *"
  suspend: false
```

### NucleiScan for VirtualService

Scan an Istio VirtualService:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: istio-app-scan
  namespace: istio-apps
spec:
  sourceRef:
    apiVersion: networking.istio.io/v1beta1
    kind: VirtualService
    name: my-virtualservice
    namespace: istio-apps
    uid: "vs-uid-12345"
  targets:
    - https://istio-app.example.com
  severity:
    - low
    - medium
    - high
    - critical
```

### Comprehensive Security Audit

Full security audit with all severity levels and template categories:

```yaml
apiVersion: nuclei.homelab.mortenolsen.pro/v1alpha1
kind: NucleiScan
metadata:
  name: comprehensive-audit
  namespace: security
  labels:
    audit-type: comprehensive
    compliance: required
spec:
  sourceRef:
    apiVersion: networking.k8s.io/v1
    kind: Ingress
    name: production-ingress
    namespace: production
    uid: "prod-uid-67890"
  targets:
    - https://www.example.com
    - https://api.example.com
    - https://admin.example.com
  templates:
    - cves/
    - vulnerabilities/
    - exposures/
    - misconfiguration/
    - default-logins/
    - takeovers/
  severity:
    - info
    - low
    - medium
    - high
    - critical
  schedule: "0 3 * * 0"  # Weekly on Sunday at 3 AM
  suspend: false
```

---

## Print Columns

When listing NucleiScan resources with `kubectl get nucleiscans`, the following columns are displayed:

| Column | JSONPath | Description |
|--------|----------|-------------|
| NAME | `.metadata.name` | Resource name |
| PHASE | `.status.phase` | Current scan phase |
| FINDINGS | `.status.summary.totalFindings` | Total number of findings |
| SOURCE | `.spec.sourceRef.kind` | Source resource kind |
| AGE | `.metadata.creationTimestamp` | Resource age |

**Example output:**

```
NAME                    PHASE       FINDINGS   SOURCE          AGE
my-app-scan            Completed   5          Ingress         2d
api-scan               Running     0          Ingress         1h
istio-app-scan         Completed   2          VirtualService  5d
```

---

## Validation

The CRD includes validation rules enforced by the Kubernetes API server:

### Spec Validation

- `sourceRef.kind` must be either `Ingress` or `VirtualService`
- `targets` must contain at least one item
- `severity` values must be one of: `info`, `low`, `medium`, `high`, `critical`

### Status Validation

- `phase` must be one of: `Pending`, `Running`, `Completed`, `Failed`

---

## RBAC Requirements

To interact with NucleiScan resources, the following RBAC permissions are needed:

### Read-only Access

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nucleiscan-viewer
rules:
  - apiGroups: ["nuclei.homelab.mortenolsen.pro"]
    resources: ["nucleiscans"]
    verbs: ["get", "list", "watch"]
```

### Full Access

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: nucleiscan-editor
rules:
  - apiGroups: ["nuclei.homelab.mortenolsen.pro"]
    resources: ["nucleiscans"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["nuclei.homelab.mortenolsen.pro"]
    resources: ["nucleiscans/status"]
    verbs: ["get"]