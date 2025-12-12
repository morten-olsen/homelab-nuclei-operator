# Pod-Based Scanning Architecture Design

## Executive Summary

This document describes the new architecture for the nuclei-operator that moves from synchronous subprocess-based scanning to asynchronous pod-based scanning. This change improves scalability, reliability, and operational flexibility while maintaining backward compatibility.

## 1. Architecture Overview

### 1.1 Current State Problems

The current implementation has several limitations:

1. **Blocking Reconcile Loop**: Scans execute synchronously within the operator pod, blocking the reconcile loop for up to 30 minutes
2. **Single Point of Failure**: All scans run in the operator pod - if it restarts, running scans are lost
3. **Resource Contention**: Multiple concurrent scans compete for operator pod resources
4. **No Horizontal Scaling**: Cannot distribute scan workload across multiple pods
5. **Limited Configuration**: No annotation-based configuration for individual Ingress/VirtualService resources

### 1.2 New Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           KUBERNETES CLUSTER                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐    ┌──────────────────┐    ┌─────────────────────────┐   │
│  │   Ingress    │───▶│ IngressReconciler │───▶│                         │   │
│  └──────────────┘    └──────────────────┘    │                         │   │
│                              │                │     NucleiScan CRD      │   │
│  ┌──────────────┐    ┌──────────────────┐    │                         │   │
│  │VirtualService│───▶│   VSReconciler   │───▶│  spec:                  │   │
│  └──────────────┘    └──────────────────┘    │    sourceRef            │   │
│                              │                │    targets[]            │   │
│                              │                │    templates[]          │   │
│                              ▼                │    severity[]           │   │
│                      ┌──────────────────┐    │    schedule             │   │
│                      │  Owner Reference │    │  status:                │   │
│                      │  (GC on delete)  │    │    phase                │   │
│                      └──────────────────┘    │    findings[]           │   │
│                                              │    summary              │   │
│                                              │    jobRef               │   │
│                                              └───────────┬─────────────┘   │
│                                                          │                  │
│                                                          ▼                  │
│                                              ┌─────────────────────────┐   │
│                                              │ NucleiScanReconciler    │   │
│                                              │                         │   │
│                                              │  1. Check phase         │   │
│                                              │  2. Create/monitor Job  │   │
│                                              │  3. Handle completion   │   │
│                                              └───────────┬─────────────┘   │
│                                                          │                  │
│                                                          ▼                  │
│                                              ┌─────────────────────────┐   │
│                                              │   Scanner Jobs          │   │
│                                              │   (Kubernetes Jobs)     │   │
│                                              │                         │   │
│                                              │   - Isolated execution  │   │
│                                              │   - Direct status update│   │
│                                              │   - Auto cleanup (TTL)  │   │
│                                              └─────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 Key Design Decisions

#### Decision 1: Kubernetes Jobs vs Bare Pods

**Choice: Kubernetes Jobs with TTLAfterFinished**

Rationale:
- Jobs provide built-in completion tracking and retry mechanisms
- TTLAfterFinished enables automatic cleanup of completed jobs
- Jobs maintain history for debugging and auditing
- Better integration with Kubernetes ecosystem tools

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: nucleiscan-myapp-abc123
  namespace: default
spec:
  ttlSecondsAfterFinished: 3600  # Clean up 1 hour after completion
  backoffLimit: 2                 # Retry failed scans twice
  activeDeadlineSeconds: 1800     # 30 minute timeout
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: scanner
        image: ghcr.io/morten-olsen/homelab-nuclei-operator:latest
        args: ["--mode=scanner", "--scan-id=myapp-abc123"]
```

#### Decision 2: Result Communication

**Choice: Dual-mode operator image with direct API access**

Rationale:
- Single image simplifies deployment and versioning
- Scanner mode has direct Kubernetes API access to update NucleiScan status
- No intermediate storage needed (ConfigMaps or logs)
- Results are immediately available in the CRD status
- Consistent error handling and status updates

The operator binary supports two modes:
1. **Controller Mode** (default): Runs the operator controllers
2. **Scanner Mode** (`--mode=scanner`): Executes a single scan and updates the NucleiScan status

#### Decision 3: Template Distribution

**Choice: Hybrid approach with configurable options**

1. **Default**: Use projectdiscovery/nuclei built-in templates (updated with each nuclei release)
2. **Custom Templates**: Mount via ConfigMap for small template sets
3. **Git Sync**: Init container that clones template repositories at runtime
4. **Custom Image**: For air-gapped environments, bake templates into a custom scanner image

Configuration hierarchy:
```
Operator Defaults < NucleiScan Spec < Ingress/VS Annotations
```

## 2. Component Design

### 2.1 NucleiScan Controller Changes

The controller transitions from executing scans to managing scan jobs:

```go
// Simplified reconciliation flow
func (r *NucleiScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    nucleiScan := &nucleiv1alpha1.NucleiScan{}
    if err := r.Get(ctx, req.NamespacedName, nucleiScan); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    switch nucleiScan.Status.Phase {
    case ScanPhasePending:
        return r.handlePending(ctx, nucleiScan)    // Create Job
    case ScanPhaseRunning:
        return r.handleRunning(ctx, nucleiScan)    // Monitor Job
    case ScanPhaseCompleted, ScanPhaseFailed:
        return r.handleCompleted(ctx, nucleiScan)  // Schedule next or cleanup
    }
}
```

### 2.2 Job Manager Component

New component responsible for:
- Creating scanner jobs with proper configuration
- Monitoring job status and updating NucleiScan accordingly
- Cleaning up orphaned jobs on operator restart
- Enforcing concurrency limits

```go
type JobManager struct {
    client.Client
    Scheme          *runtime.Scheme
    ScannerImage    string
    MaxConcurrent   int
    DefaultTimeout  time.Duration
}

func (m *JobManager) CreateScanJob(ctx context.Context, scan *nucleiv1alpha1.NucleiScan) (*batchv1.Job, error) {
    job := m.buildJob(scan)
    if err := controllerutil.SetControllerReference(scan, job, m.Scheme); err != nil {
        return nil, err
    }
    return job, m.Create(ctx, job)
}
```

### 2.3 Scanner Mode Implementation

The operator binary in scanner mode:

```go
func runScannerMode(scanID string) error {
    // 1. Initialize Kubernetes client
    config, _ := rest.InClusterConfig()
    client, _ := client.New(config, client.Options{})
    
    // 2. Fetch the NucleiScan resource
    scan := &nucleiv1alpha1.NucleiScan{}
    client.Get(ctx, types.NamespacedName{...}, scan)
    
    // 3. Execute the scan
    result, err := scanner.Scan(ctx, scan.Spec.Targets, options)
    
    // 4. Update NucleiScan status directly
    scan.Status.Phase = ScanPhaseCompleted
    scan.Status.Findings = result.Findings
    scan.Status.Summary = result.Summary
    client.Status().Update(ctx, scan)
    
    return nil
}
```

## 3. API Changes

### 3.1 NucleiScan CRD Updates

New fields added to the spec and status:

```go
// NucleiScanSpec additions
type NucleiScanSpec struct {
    // ... existing fields ...
    
    // ScannerConfig allows overriding scanner settings for this scan
    // +optional
    ScannerConfig *ScannerConfig `json:"scannerConfig,omitempty"`
}

// ScannerConfig defines scanner-specific configuration
type ScannerConfig struct {
    // Image overrides the default scanner image
    // +optional
    Image string `json:"image,omitempty"`
    
    // Resources defines resource requirements for the scanner pod
    // +optional
    Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
    
    // Timeout overrides the default scan timeout
    // +optional
    Timeout *metav1.Duration `json:"timeout,omitempty"`
    
    // TemplateURLs specifies additional template repositories to clone
    // +optional
    TemplateURLs []string `json:"templateURLs,omitempty"`
    
    // NodeSelector for scanner pod scheduling
    // +optional
    NodeSelector map[string]string `json:"nodeSelector,omitempty"`
    
    // Tolerations for scanner pod scheduling
    // +optional
    Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// NucleiScanStatus additions
type NucleiScanStatus struct {
    // ... existing fields ...
    
    // JobRef references the current or last scanner job
    // +optional
    JobRef *JobReference `json:"jobRef,omitempty"`
    
    // ScanStartTime is when the scanner pod actually started scanning
    // +optional
    ScanStartTime *metav1.Time `json:"scanStartTime,omitempty"`
}

// JobReference contains information about the scanner job
type JobReference struct {
    // Name of the Job
    Name string `json:"name"`
    
    // UID of the Job
    UID string `json:"uid"`
    
    // PodName is the name of the scanner pod (for log retrieval)
    // +optional
    PodName string `json:"podName,omitempty"`
    
    // StartTime when the job was created
    StartTime *metav1.Time `json:"startTime,omitempty"`
}
```

## 4. Annotation Schema

### 4.1 Supported Annotations

Annotations on Ingress/VirtualService resources to configure scanning:

| Annotation | Type | Default | Description |
|------------|------|---------|-------------|
| `nuclei.homelab.mortenolsen.pro/enabled` | bool | `true` | Enable/disable scanning for this resource |
| `nuclei.homelab.mortenolsen.pro/templates` | string | - | Comma-separated list of template paths or tags |
| `nuclei.homelab.mortenolsen.pro/severity` | string | - | Comma-separated severity filter: info,low,medium,high,critical |
| `nuclei.homelab.mortenolsen.pro/schedule` | string | - | Cron schedule for periodic scans |
| `nuclei.homelab.mortenolsen.pro/timeout` | duration | `30m` | Scan timeout |
| `nuclei.homelab.mortenolsen.pro/scanner-image` | string | - | Override scanner image |
| `nuclei.homelab.mortenolsen.pro/exclude-templates` | string | - | Templates to exclude |
| `nuclei.homelab.mortenolsen.pro/rate-limit` | int | `150` | Requests per second limit |
| `nuclei.homelab.mortenolsen.pro/tags` | string | - | Template tags to include |
| `nuclei.homelab.mortenolsen.pro/exclude-tags` | string | - | Template tags to exclude |

### 4.2 Example Annotated Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: myapp-ingress
  namespace: production
  annotations:
    nuclei.homelab.mortenolsen.pro/enabled: "true"
    nuclei.homelab.mortenolsen.pro/severity: "medium,high,critical"
    nuclei.homelab.mortenolsen.pro/schedule: "0 2 * * *"
    nuclei.homelab.mortenolsen.pro/templates: "cves/,vulnerabilities/,exposures/"
    nuclei.homelab.mortenolsen.pro/exclude-tags: "dos,fuzz"
    nuclei.homelab.mortenolsen.pro/timeout: "45m"
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

## 5. State Machine

### 5.1 Updated Scan Lifecycle

```
                    ┌─────────────────────────────────────┐
                    │                                     │
                    ▼                                     │
┌─────────┐    ┌─────────┐    ┌───────────┐    ┌────────┴─┐
│ Created │───▶│ Pending │───▶│  Running  │───▶│ Completed│
└─────────┘    └────┬────┘    └─────┬─────┘    └──────────┘
                    │               │                │
                    │               │                │ (schedule/rescanAge)
                    │               ▼                │
                    │          ┌─────────┐           │
                    │          │ Failed  │◀──────────┘
                    │          └────┬────┘
                    │               │
                    └───────────────┘ (spec change triggers retry)
```

### 5.2 Phase Definitions

| Phase | Description | Job State | Actions |
|-------|-------------|-----------|---------|
| `Pending` | Waiting to start | None | Create scanner job |
| `Running` | Scan in progress | Active | Monitor job, check timeout |
| `Completed` | Scan finished successfully | Succeeded | Parse results, schedule next |
| `Failed` | Scan failed | Failed | Record error, retry logic |

## 6. Error Handling

### 6.1 Failure Scenarios

| Scenario | Detection | Recovery |
|----------|-----------|----------|
| Job creation fails | API error | Retry with backoff, update status |
| Pod fails to schedule | Job pending timeout | Alert, manual intervention |
| Scan timeout | activeDeadlineSeconds | Mark failed, retry |
| Scanner crashes | Job failed status | Retry based on backoffLimit |
| Operator restarts | Running phase with no job | Reset to Pending |
| Target unavailable | HTTP check fails | Exponential backoff retry |
| Results too large | Status update fails | Truncate findings, log warning |

### 6.2 Operator Restart Recovery

On startup, the operator must handle orphaned state:

```go
func (r *NucleiScanReconciler) RecoverOrphanedScans(ctx context.Context) error {
    // List all NucleiScans in Running phase
    scanList := &nucleiv1alpha1.NucleiScanList{}
    if err := r.List(ctx, scanList); err != nil {
        return err
    }
    
    for _, scan := range scanList.Items {
        if scan.Status.Phase != ScanPhaseRunning {
            continue
        }
        
        // Check if the referenced job still exists
        if scan.Status.JobRef != nil {
            job := &batchv1.Job{}
            err := r.Get(ctx, types.NamespacedName{
                Name:      scan.Status.JobRef.Name,
                Namespace: scan.Namespace,
            }, job)
            
            if apierrors.IsNotFound(err) {
                // Job is gone - reset scan to Pending
                scan.Status.Phase = ScanPhasePending
                scan.Status.LastError = "Recovered from operator restart - job not found"
                scan.Status.JobRef = nil
                r.Status().Update(ctx, &scan)
            }
            // If job exists, normal reconciliation will handle it
        }
    }
    
    return nil
}
```

### 6.3 Job Cleanup

Orphaned jobs are cleaned up via:

1. **Owner References**: Jobs have NucleiScan as owner - deleted when scan is deleted
2. **TTLAfterFinished**: Kubernetes automatically cleans up completed jobs
3. **Periodic Cleanup**: Background goroutine removes stuck jobs

## 7. Security Considerations

### 7.1 RBAC Updates

The operator needs additional permissions for Job management:

```yaml
# Additional rules for config/rbac/role.yaml
rules:
  # Job management
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # Pod logs for debugging
  - apiGroups: [""]
    resources: ["pods", "pods/log"]
    verbs: ["get", "list", "watch"]
```

Scanner pods need minimal RBAC - only to update their specific NucleiScan:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: nuclei-scanner-role
rules:
  - apiGroups: ["nuclei.homelab.mortenolsen.pro"]
    resources: ["nucleiscans"]
    verbs: ["get"]
  - apiGroups: ["nuclei.homelab.mortenolsen.pro"]
    resources: ["nucleiscans/status"]
    verbs: ["get", "update", "patch"]
```

### 7.2 Pod Security

Scanner pods run with restricted security context:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: false  # Nuclei needs temp files
  seccompProfile:
    type: RuntimeDefault
  capabilities:
    drop:
      - ALL
```

## 8. Migration Path

### 8.1 Version Strategy

| Version | Changes | Compatibility |
|---------|---------|---------------|
| v0.x | Current synchronous scanning | - |
| v1.0 | Pod-based scanning, new status fields | Backward compatible |
| v1.1 | Annotation support | Additive |
| v2.0 | Remove synchronous mode | Breaking |

### 8.2 Migration Steps

1. **Phase 1**: Add new fields to CRD (non-breaking, all optional)
2. **Phase 2**: Dual-mode operation with feature flag
3. **Phase 3**: Add annotation support
4. **Phase 4**: Deprecate synchronous mode
5. **Phase 5**: Remove synchronous mode (v2.0)

### 8.3 Rollback Plan

If issues are discovered:
1. **Immediate**: Set `scanner.mode: sync` in Helm values
2. **Short-term**: Pin to previous operator version
3. **Long-term**: Fix issues in pod-based mode

## 9. Configuration Reference

### 9.1 Helm Values

```yaml
# Scanner configuration
scanner:
  # Scanning mode: "pod" or "sync" (legacy)
  mode: "pod"
  
  # Default scanner image
  image: ghcr.io/morten-olsen/homelab-nuclei-operator:latest
  
  # Default scan timeout
  timeout: 30m
  
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

# Template configuration
templates:
  # Built-in templates to use
  defaults:
    - cves/
    - vulnerabilities/
  
  # Git repositories to clone (init container)
  repositories: []
  # - url: https://github.com/projectdiscovery/nuclei-templates
  #   branch: main
  #   path: /templates/community

# Operator configuration
operator:
  # Rescan age - trigger rescan if results older than this
  rescanAge: 168h
  
  # Backoff for target availability checks
  backoff:
    initial: 10s
    max: 10m
    multiplier: 2.0
```

### 9.2 Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SCANNER_MODE` | pod or sync | pod |
| `SCANNER_IMAGE` | Default scanner image | operator image |
| `SCANNER_TIMEOUT` | Default scan timeout | 30m |
| `MAX_CONCURRENT_SCANS` | Max parallel jobs | 5 |
| `JOB_TTL_AFTER_FINISHED` | Job cleanup TTL | 3600 |
| `NUCLEI_TEMPLATES_PATH` | Template directory | /nuclei-templates |

## 10. Observability

### 10.1 Metrics

New Prometheus metrics:
- `nuclei_scan_jobs_created_total` - Total scanner jobs created
- `nuclei_scan_job_duration_seconds` - Duration histogram of scan jobs
- `nuclei_active_scan_jobs` - Currently running scan jobs

### 10.2 Events

Kubernetes events for key state transitions:
- `ScanJobCreated` - Scanner job created
- `ScanCompleted` - Scan finished successfully
- `ScanFailed` - Scan failed

### 10.3 Logging

Structured logging with consistent fields:
- `scan` - NucleiScan name
- `namespace` - Namespace
- `targets` - Number of targets
- `timeout` - Scan timeout