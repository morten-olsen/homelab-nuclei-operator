/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package jobmanager

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
)

const (
	// DefaultScannerImage is the default image used for scanner pods
	DefaultScannerImage = "ghcr.io/morten-olsen/nuclei-operator:latest"

	// DefaultTimeout is the default scan timeout
	DefaultTimeout = 30 * time.Minute

	// DefaultTTLAfterFinished is the default TTL for completed jobs
	DefaultTTLAfterFinished = 3600 // 1 hour

	// DefaultBackoffLimit is the default number of retries for failed jobs
	DefaultBackoffLimit = 2

	// LabelManagedBy is the label key for identifying managed resources
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// LabelComponent is the label key for component identification
	LabelComponent = "app.kubernetes.io/component"

	// LabelScanName is the label key for the scan name
	LabelScanName = "nuclei.homelab.mortenolsen.pro/scan-name"

	// LabelScanNamespace is the label key for the scan namespace
	LabelScanNamespace = "nuclei.homelab.mortenolsen.pro/scan-namespace"
)

// Config holds the configuration for the JobManager
type Config struct {
	// ScannerImage is the default image to use for scanner pods
	ScannerImage string

	// DefaultTimeout is the default scan timeout
	DefaultTimeout time.Duration

	// TTLAfterFinished is the TTL for completed jobs in seconds
	TTLAfterFinished int32

	// BackoffLimit is the number of retries for failed jobs
	BackoffLimit int32

	// MaxConcurrent is the maximum number of concurrent scan jobs
	MaxConcurrent int

	// ServiceAccountName is the service account to use for scanner pods
	ServiceAccountName string

	// OperatorNamespace is the namespace where the operator runs and where scanner jobs will be created
	OperatorNamespace string

	// DefaultResources are the default resource requirements for scanner pods
	DefaultResources corev1.ResourceRequirements

	// DefaultTemplates are the default templates to use for scans
	DefaultTemplates []string

	// DefaultSeverity is the default severity filter
	DefaultSeverity []string
}

// DefaultConfig returns a Config with default values
func DefaultConfig() Config {
	return Config{
		ScannerImage:       DefaultScannerImage,
		DefaultTimeout:     DefaultTimeout,
		TTLAfterFinished:   DefaultTTLAfterFinished,
		BackoffLimit:       DefaultBackoffLimit,
		MaxConcurrent:      5,
		ServiceAccountName: "nuclei-scanner",
		OperatorNamespace:  "nuclei-operator-system",
		DefaultResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}
}

// JobManager manages scanner jobs for NucleiScan resources
type JobManager struct {
	client.Client
	Scheme *runtime.Scheme
	Config Config
}

// NewJobManager creates a new JobManager with the given configuration
func NewJobManager(c client.Client, scheme *runtime.Scheme, config Config) *JobManager {
	return &JobManager{
		Client: c,
		Scheme: scheme,
		Config: config,
	}
}

// CreateScanJob creates a new scanner job for the given NucleiScan
func (m *JobManager) CreateScanJob(ctx context.Context, scan *nucleiv1alpha1.NucleiScan) (*batchv1.Job, error) {
	logger := log.FromContext(ctx)

	job := m.buildJob(scan)

	// Only set owner reference if the job is in the same namespace as the scan
	// Cross-namespace owner references are not allowed in Kubernetes
	if job.Namespace == scan.Namespace {
		if err := controllerutil.SetControllerReference(scan, job, m.Scheme); err != nil {
			return nil, fmt.Errorf("failed to set controller reference: %w", err)
		}
	}
	// When job is in a different namespace (operator namespace), we rely on:
	// 1. TTLSecondsAfterFinished for automatic cleanup of completed jobs
	// 2. Labels (LabelScanName, LabelScanNamespace) to track which scan the job belongs to
	// 3. CleanupOrphanedJobs to clean up jobs whose scans no longer exist

	logger.Info("Creating scanner job",
		"job", job.Name,
		"jobNamespace", job.Namespace,
		"scanNamespace", scan.Namespace,
		"image", job.Spec.Template.Spec.Containers[0].Image,
		"targets", len(scan.Spec.Targets))

	if err := m.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return job, nil
}

// GetJob retrieves a job by name and namespace
func (m *JobManager) GetJob(ctx context.Context, name, namespace string) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	err := m.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// DeleteJob deletes a job by name and namespace
func (m *JobManager) DeleteJob(ctx context.Context, name, namespace string) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return m.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
}

// GetJobPodName returns the name of the pod created by the job
func (m *JobManager) GetJobPodName(ctx context.Context, job *batchv1.Job) (string, error) {
	podList := &corev1.PodList{}
	err := m.List(ctx, podList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"job-name": job.Name})
	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", nil
	}

	// Return the first pod (there should only be one for our jobs)
	return podList.Items[0].Name, nil
}

// IsJobComplete returns true if the job has completed (successfully or failed)
func (m *JobManager) IsJobComplete(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) &&
			condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsJobSuccessful returns true if the job completed successfully
func (m *JobManager) IsJobSuccessful(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsJobFailed returns true if the job failed
func (m *JobManager) IsJobFailed(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// GetJobFailureReason returns the reason for job failure
func (m *JobManager) GetJobFailureReason(job *batchv1.Job) string {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return condition.Message
		}
	}
	return "Unknown failure reason"
}

// CountActiveJobs returns the number of currently active scan jobs
func (m *JobManager) CountActiveJobs(ctx context.Context) (int, error) {
	jobList := &batchv1.JobList{}
	err := m.List(ctx, jobList, client.MatchingLabels{
		LabelManagedBy: "nuclei-operator",
		LabelComponent: "scanner",
	})
	if err != nil {
		return 0, err
	}

	count := 0
	for _, job := range jobList.Items {
		if job.Status.Active > 0 {
			count++
		}
	}
	return count, nil
}

// AtCapacity returns true if the maximum number of concurrent jobs has been reached
func (m *JobManager) AtCapacity(ctx context.Context) (bool, error) {
	count, err := m.CountActiveJobs(ctx)
	if err != nil {
		return false, err
	}
	return count >= m.Config.MaxConcurrent, nil
}

// CleanupOrphanedJobs removes jobs that no longer have an associated NucleiScan
func (m *JobManager) CleanupOrphanedJobs(ctx context.Context) error {
	logger := log.FromContext(ctx)

	jobList := &batchv1.JobList{}
	err := m.List(ctx, jobList, client.MatchingLabels{
		LabelManagedBy: "nuclei-operator",
		LabelComponent: "scanner",
	})
	if err != nil {
		return err
	}

	for _, job := range jobList.Items {
		// Check if the associated NucleiScan still exists using labels
		scanName := job.Labels[LabelScanName]
		scanNamespace := job.Labels[LabelScanNamespace]

		if scanName != "" && scanNamespace != "" {
			// Try to get the associated NucleiScan
			scan := &nucleiv1alpha1.NucleiScan{}
			err := m.Get(ctx, types.NamespacedName{Name: scanName, Namespace: scanNamespace}, scan)
			if err != nil {
				if apierrors.IsNotFound(err) {
					// The scan no longer exists - delete the job
					logger.Info("Deleting orphaned job (scan not found)",
						"job", job.Name,
						"namespace", job.Namespace,
						"scanName", scanName,
						"scanNamespace", scanNamespace)
					if err := m.DeleteJob(ctx, job.Name, job.Namespace); err != nil && !apierrors.IsNotFound(err) {
						logger.Error(err, "Failed to delete orphaned job", "job", job.Name)
					}
					continue
				}
				// Other error - log and continue
				logger.Error(err, "Failed to check if scan exists", "scanName", scanName, "scanNamespace", scanNamespace)
				continue
			}
		} else {
			// Job doesn't have proper labels - check owner reference as fallback
			ownerRef := metav1.GetControllerOf(&job)
			if ownerRef == nil {
				logger.Info("Deleting orphaned job without owner or labels", "job", job.Name, "namespace", job.Namespace)
				if err := m.DeleteJob(ctx, job.Name, job.Namespace); err != nil && !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to delete orphaned job", "job", job.Name)
				}
				continue
			}
		}

		// Check if the job is stuck (running longer than 2x the timeout)
		if job.Status.StartTime != nil {
			maxDuration := 2 * m.Config.DefaultTimeout
			if time.Since(job.Status.StartTime.Time) > maxDuration && job.Status.Active > 0 {
				logger.Info("Deleting stuck job", "job", job.Name, "namespace", job.Namespace,
					"age", time.Since(job.Status.StartTime.Time))
				if err := m.DeleteJob(ctx, job.Name, job.Namespace); err != nil && !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to delete stuck job", "job", job.Name)
				}
			}
		}
	}

	return nil
}

// buildJob creates a Job specification for the given NucleiScan
func (m *JobManager) buildJob(scan *nucleiv1alpha1.NucleiScan) *batchv1.Job {
	// Generate a unique job name that includes the scan namespace to avoid collisions
	jobName := fmt.Sprintf("nucleiscan-%s-%s-%d", scan.Namespace, scan.Name, time.Now().Unix())
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}

	// Determine the namespace for the job - use operator namespace if configured
	jobNamespace := m.Config.OperatorNamespace
	if jobNamespace == "" {
		jobNamespace = scan.Namespace
	}

	// Determine the scanner image
	image := m.Config.ScannerImage
	if scan.Spec.ScannerConfig != nil && scan.Spec.ScannerConfig.Image != "" {
		image = scan.Spec.ScannerConfig.Image
	}

	// Determine timeout
	timeout := m.Config.DefaultTimeout
	if scan.Spec.ScannerConfig != nil && scan.Spec.ScannerConfig.Timeout != nil {
		timeout = scan.Spec.ScannerConfig.Timeout.Duration
	}
	activeDeadlineSeconds := int64(timeout.Seconds())

	// Determine resources
	resources := m.Config.DefaultResources
	if scan.Spec.ScannerConfig != nil && scan.Spec.ScannerConfig.Resources != nil {
		resources = *scan.Spec.ScannerConfig.Resources
	}

	// Build command arguments for scanner mode
	args := []string{
		"--mode=scanner",
		fmt.Sprintf("--scan-name=%s", scan.Name),
		fmt.Sprintf("--scan-namespace=%s", scan.Namespace),
	}

	// Build labels
	labels := map[string]string{
		LabelManagedBy:     "nuclei-operator",
		LabelComponent:     "scanner",
		LabelScanName:      scan.Name,
		LabelScanNamespace: scan.Namespace,
	}

	// Build node selector
	var nodeSelector map[string]string
	if scan.Spec.ScannerConfig != nil && scan.Spec.ScannerConfig.NodeSelector != nil {
		nodeSelector = scan.Spec.ScannerConfig.NodeSelector
	}

	// Build tolerations
	var tolerations []corev1.Toleration
	if scan.Spec.ScannerConfig != nil && scan.Spec.ScannerConfig.Tolerations != nil {
		tolerations = scan.Spec.ScannerConfig.Tolerations
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: jobNamespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To(m.Config.TTLAfterFinished),
			BackoffLimit:            ptr.To(m.Config.BackoffLimit),
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: m.Config.ServiceAccountName,
					NodeSelector:       nodeSelector,
					Tolerations:        tolerations,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(65532)),
						RunAsGroup:   ptr.To(int64(65532)),
						FSGroup:      ptr.To(int64(65532)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:      "scanner",
							Image:     image,
							Args:      args,
							Resources: resources,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								ReadOnlyRootFilesystem:   ptr.To(false), // Nuclei needs temp files
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name:  "NUCLEI_BINARY_PATH",
									Value: "/usr/local/bin/nuclei",
								},
								{
									Name:  "NUCLEI_TEMPLATES_PATH",
									Value: "", // Empty means use default location (~/.nuclei/templates)
								},
							},
						},
					},
				},
			},
		},
	}

	return job
}
