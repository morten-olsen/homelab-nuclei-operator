# Nuclei Operator Helm Chart

A Helm chart for deploying the Nuclei Operator - a Kubernetes operator that automatically scans Ingress and VirtualService resources using Nuclei security scanner.

## Features

- **Pod-based Scanning Architecture**: Each scan runs in an isolated Kubernetes Job for better scalability and reliability
- **Annotation-based Configuration**: Configure scanning behavior per-resource using annotations
- **Automatic Discovery**: Watches Kubernetes Ingress and Istio VirtualService resources
- **Scheduled Scans**: Support for cron-based scheduled rescanning
- **Flexible Configuration**: Configurable templates, severity filters, and scan options

## Prerequisites

- Kubernetes 1.26+
- Helm 3.0+

## Installation

### Add the Helm Repository

```bash
helm repo add nuclei-operator https://morten-olsen.github.io/homelab-nuclei-operator
helm repo update
```

### Install the Chart

```bash
helm install nuclei-operator nuclei-operator/nuclei-operator \
  --namespace nuclei-operator-system \
  --create-namespace
```

### Install with Custom Values

```bash
helm install nuclei-operator nuclei-operator/nuclei-operator \
  --namespace nuclei-operator-system \
  --create-namespace \
  -f values.yaml
```

## Configuration

The following table lists the configurable parameters of the Nuclei Operator chart and their default values.

### General

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `nameOverride` | Override the name of the chart | `""` |
| `fullnameOverride` | Override the full name of the chart | `""` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image repository | `ghcr.io/morten-olsen/homelab-nuclei-operator` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (defaults to chart appVersion) | `""` |
| `imagePullSecrets` | Image pull secrets | `[]` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |

### Pod Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |
| `podSecurityContext.runAsNonRoot` | Run as non-root | `true` |
| `podSecurityContext.seccompProfile.type` | Seccomp profile type | `RuntimeDefault` |

### Container Security Context

| Parameter | Description | Default |
|-----------|-------------|---------|
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `false` |
| `securityContext.allowPrivilegeEscalation` | Allow privilege escalation | `false` |
| `securityContext.runAsNonRoot` | Run as non-root | `true` |
| `securityContext.runAsUser` | User ID | `65532` |
| `securityContext.capabilities.drop` | Dropped capabilities | `["ALL"]` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `"2"` |
| `resources.limits.memory` | Memory limit | `"2Gi"` |
| `resources.requests.cpu` | CPU request | `"500m"` |
| `resources.requests.memory` | Memory request | `"512Mi"` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |

### Leader Election

| Parameter | Description | Default |
|-----------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |

### Health Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `healthProbes.livenessProbe.httpGet.path` | Liveness probe path | `/healthz` |
| `healthProbes.livenessProbe.httpGet.port` | Liveness probe port | `8081` |
| `healthProbes.livenessProbe.initialDelaySeconds` | Initial delay | `15` |
| `healthProbes.livenessProbe.periodSeconds` | Period | `20` |
| `healthProbes.readinessProbe.httpGet.path` | Readiness probe path | `/readyz` |
| `healthProbes.readinessProbe.httpGet.port` | Readiness probe port | `8081` |
| `healthProbes.readinessProbe.initialDelaySeconds` | Initial delay | `5` |
| `healthProbes.readinessProbe.periodSeconds` | Period | `10` |

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.service.type` | Metrics service type | `ClusterIP` |
| `metrics.service.port` | Metrics service port | `8443` |

### Nuclei Scanner Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nuclei.binaryPath` | Path to nuclei binary | `/usr/local/bin/nuclei` |
| `nuclei.templatesPath` | Path to nuclei templates | `/nuclei-templates` |
| `nuclei.timeout` | Scan timeout | `30m` |
| `nuclei.rescanAge` | Age before automatic rescan | `168h` |
| `nuclei.backoff.initial` | Initial backoff interval | `10s` |
| `nuclei.backoff.max` | Maximum backoff interval | `10m` |
| `nuclei.backoff.multiplier` | Backoff multiplier | `2.0` |

### Scanner Pod Configuration

The operator uses a pod-based scanning architecture where each scan runs in its own Kubernetes Job. Configure scanner pod behavior with these parameters:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `scanner.enabled` | Enable scanner RBAC resources | `true` |
| `scanner.image` | Scanner image (defaults to operator image) | `""` |
| `scanner.timeout` | Default scan timeout | `30m` |
| `scanner.maxConcurrent` | Maximum concurrent scan jobs | `5` |
| `scanner.ttlAfterFinished` | Job TTL after completion (seconds) | `3600` |
| `scanner.resources.requests.cpu` | Scanner pod CPU request | `100m` |
| `scanner.resources.requests.memory` | Scanner pod memory request | `256Mi` |
| `scanner.resources.limits.cpu` | Scanner pod CPU limit | `1` |
| `scanner.resources.limits.memory` | Scanner pod memory limit | `1Gi` |
| `scanner.defaultTemplates` | Default templates to use | `[]` |
| `scanner.defaultSeverity` | Default severity filter | `[]` |

### ServiceMonitor (Prometheus Operator)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceMonitor.enabled` | Enable ServiceMonitor | `false` |
| `serviceMonitor.labels` | Additional labels | `{}` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |

### Network Policy

| Parameter | Description | Default |
|-----------|-------------|---------|
| `networkPolicy.enabled` | Enable network policy | `false` |

## Examples

### Basic Installation

```bash
helm install nuclei-operator nuclei-operator/nuclei-operator \
  --namespace nuclei-operator-system \
  --create-namespace
```

### With Prometheus Monitoring

```yaml
# values.yaml
metrics:
  enabled: true

serviceMonitor:
  enabled: true
  labels:
    release: prometheus
```

```bash
helm install nuclei-operator nuclei-operator/nuclei-operator \
  --namespace nuclei-operator-system \
  --create-namespace \
  -f values.yaml
```

### With Custom Resource Limits

```yaml
# values.yaml
resources:
  limits:
    cpu: "4"
    memory: "4Gi"
  requests:
    cpu: "1"
    memory: "1Gi"

nuclei:
  timeout: "1h"
  rescanAge: "24h"
```

### With Custom Scanner Configuration

```yaml
# values.yaml
scanner:
  enabled: true
  timeout: "1h"
  maxConcurrent: 10
  ttlAfterFinished: 7200
  resources:
    requests:
      cpu: 200m
      memory: 512Mi
    limits:
      cpu: "2"
      memory: 2Gi
  defaultSeverity:
    - medium
    - high
    - critical
```

### With Node Affinity

```yaml
# values.yaml
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
        - matchExpressions:
            - key: kubernetes.io/arch
              operator: In
              values:
                - amd64
                - arm64
```

## Annotation-Based Configuration

You can configure scanning behavior for individual Ingress or VirtualService resources using annotations:

| Annotation | Description |
|------------|-------------|
| `nuclei.homelab.mortenolsen.pro/enabled` | Enable/disable scanning (`true`/`false`) |
| `nuclei.homelab.mortenolsen.pro/templates` | Comma-separated list of template paths |
| `nuclei.homelab.mortenolsen.pro/severity` | Comma-separated severity filter |
| `nuclei.homelab.mortenolsen.pro/schedule` | Cron schedule for periodic scans |
| `nuclei.homelab.mortenolsen.pro/timeout` | Scan timeout duration |
| `nuclei.homelab.mortenolsen.pro/scanner-image` | Override scanner image |

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

## Uninstallation

```bash
helm uninstall nuclei-operator -n nuclei-operator-system
```

To also remove the CRDs:

```bash
kubectl delete crd nucleiscans.nuclei.homelab.mortenolsen.pro
```

## Links

- [GitHub Repository](https://github.com/morten-olsen/homelab-nuclei-operator)
- [Nuclei Scanner](https://github.com/projectdiscovery/nuclei)