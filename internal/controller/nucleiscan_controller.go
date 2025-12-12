/*
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
*/

package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	"github.com/mortenolsen/nuclei-operator/internal/jobmanager"
)

const (
	// finalizerName is the finalizer used by this controller
	finalizerName = "nuclei.homelab.mortenolsen.pro/finalizer"

	// Default requeue intervals
	defaultRequeueAfter      = 30 * time.Second
	defaultScheduleRequeue   = 1 * time.Minute
	defaultErrorRequeueAfter = 1 * time.Minute

	// Default rescan age (1 week)
	defaultRescanAge = 7 * 24 * time.Hour

	// Default backoff settings for target availability checks
	defaultBackoffInitial    = 10 * time.Second // Initial retry interval
	defaultBackoffMax        = 10 * time.Minute // Maximum retry interval
	defaultBackoffMultiplier = 2.0              // Multiplier for exponential backoff

	// Environment variables
	envRescanAge         = "NUCLEI_RESCAN_AGE"
	envBackoffInitial    = "NUCLEI_BACKOFF_INITIAL"
	envBackoffMax        = "NUCLEI_BACKOFF_MAX"
	envBackoffMultiplier = "NUCLEI_BACKOFF_MULTIPLIER"
)

// Condition types for NucleiScan
const (
	ConditionTypeReady      = "Ready"
	ConditionTypeScanActive = "ScanActive"
)

// Condition reasons
const (
	ReasonScanPending   = "ScanPending"
	ReasonScanRunning   = "ScanRunning"
	ReasonScanCompleted = "ScanCompleted"
	ReasonScanFailed    = "ScanFailed"
	ReasonScanSuspended = "ScanSuspended"
)

// ReconcilerConfig holds configuration for the NucleiScanReconciler
type ReconcilerConfig struct {
	RescanAge         time.Duration
	BackoffInitial    time.Duration
	BackoffMax        time.Duration
	BackoffMultiplier float64
}

// DefaultReconcilerConfig returns a ReconcilerConfig with default values
func DefaultReconcilerConfig() ReconcilerConfig {
	config := ReconcilerConfig{
		RescanAge:         defaultRescanAge,
		BackoffInitial:    defaultBackoffInitial,
		BackoffMax:        defaultBackoffMax,
		BackoffMultiplier: defaultBackoffMultiplier,
	}

	// Override from environment variables
	if envVal := os.Getenv(envRescanAge); envVal != "" {
		if parsed, err := time.ParseDuration(envVal); err == nil {
			config.RescanAge = parsed
		}
	}

	if envVal := os.Getenv(envBackoffInitial); envVal != "" {
		if parsed, err := time.ParseDuration(envVal); err == nil {
			config.BackoffInitial = parsed
		}
	}

	if envVal := os.Getenv(envBackoffMax); envVal != "" {
		if parsed, err := time.ParseDuration(envVal); err == nil {
			config.BackoffMax = parsed
		}
	}

	if envVal := os.Getenv(envBackoffMultiplier); envVal != "" {
		if parsed, err := parseFloat(envVal); err == nil && parsed > 0 {
			config.BackoffMultiplier = parsed
		}
	}

	return config
}

// NucleiScanReconciler reconciles a NucleiScan object
type NucleiScanReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	JobManager *jobmanager.JobManager
	Config     ReconcilerConfig
	HTTPClient *http.Client
}

// NewNucleiScanReconciler creates a new NucleiScanReconciler with the given configuration
func NewNucleiScanReconciler(
	c client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	jobManager *jobmanager.JobManager,
	config ReconcilerConfig,
) *NucleiScanReconciler {
	return &NucleiScanReconciler{
		Client:     c,
		Scheme:     scheme,
		Recorder:   recorder,
		JobManager: jobManager,
		Config:     config,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// parseFloat parses a string to float64
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// calculateBackoff calculates the next backoff duration based on retry count
func (r *NucleiScanReconciler) calculateBackoff(retryCount int) time.Duration {
	if retryCount <= 0 {
		return r.Config.BackoffInitial
	}

	// Calculate exponential backoff: initial * multiplier^retryCount
	backoff := float64(r.Config.BackoffInitial)
	for i := 0; i < retryCount; i++ {
		backoff *= r.Config.BackoffMultiplier
		if backoff > float64(r.Config.BackoffMax) {
			return r.Config.BackoffMax
		}
	}

	return time.Duration(backoff)
}

// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NucleiScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the NucleiScan instance
	nucleiScan := &nucleiv1alpha1.NucleiScan{}
	if err := r.Get(ctx, req.NamespacedName, nucleiScan); err != nil {
		// Resource not found, likely deleted
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !nucleiScan.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, nucleiScan)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(nucleiScan, finalizerName) {
		controllerutil.AddFinalizer(nucleiScan, finalizerName)
		if err := r.Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if scan is suspended
	if nucleiScan.Spec.Suspend {
		log.Info("Scan is suspended, skipping")
		return r.updateCondition(ctx, nucleiScan, ConditionTypeReady, metav1.ConditionFalse,
			ReasonScanSuspended, "Scan is suspended")
	}

	// Initialize status if empty
	if nucleiScan.Status.Phase == "" {
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		nucleiScan.Status.ObservedGeneration = nucleiScan.Generation
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle based on current phase
	switch nucleiScan.Status.Phase {
	case nucleiv1alpha1.ScanPhasePending:
		return r.handlePendingPhase(ctx, nucleiScan)
	case nucleiv1alpha1.ScanPhaseRunning:
		return r.handleRunningPhase(ctx, nucleiScan)
	case nucleiv1alpha1.ScanPhaseCompleted:
		return r.handleCompletedPhase(ctx, nucleiScan)
	case nucleiv1alpha1.ScanPhaseFailed:
		return r.handleFailedPhase(ctx, nucleiScan)
	default:
		log.Info("Unknown phase, resetting to Pending", "phase", nucleiScan.Status.Phase)
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
}

// handleDeletion handles the deletion of a NucleiScan resource
func (r *NucleiScanReconciler) handleDeletion(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(nucleiScan, finalizerName) {
		log.Info("Handling deletion, performing cleanup", "name", nucleiScan.Name, "namespace", nucleiScan.Namespace)

		// Clean up any running scanner job
		if nucleiScan.Status.JobRef != nil {
			jobNamespace := nucleiScan.Status.JobRef.Namespace
			if jobNamespace == "" {
				// Fallback for backwards compatibility
				jobNamespace = nucleiScan.Namespace
			}
			log.Info("Deleting scanner job", "job", nucleiScan.Status.JobRef.Name, "namespace", jobNamespace)
			if err := r.JobManager.DeleteJob(ctx, nucleiScan.Status.JobRef.Name, jobNamespace); err != nil {
				if !apierrors.IsNotFound(err) {
					log.Error(err, "Failed to delete scanner job", "job", nucleiScan.Status.JobRef.Name)
				}
			}
		}

		// Remove finalizer
		log.Info("Removing finalizer", "finalizer", finalizerName)
		controllerutil.RemoveFinalizer(nucleiScan, finalizerName)
		if err := r.Update(ctx, nucleiScan); err != nil {
			log.Error(err, "Failed to remove finalizer", "name", nucleiScan.Name, "namespace", nucleiScan.Namespace)
			return ctrl.Result{}, err
		}
		log.Info("Finalizer removed successfully", "name", nucleiScan.Name, "namespace", nucleiScan.Namespace)
	}

	return ctrl.Result{}, nil
}

// handlePendingPhase handles the Pending phase - creates a scanner job
func (r *NucleiScanReconciler) handlePendingPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("Preparing to scan", "targets", len(nucleiScan.Spec.Targets))

	// Check if we're at capacity
	atCapacity, err := r.JobManager.AtCapacity(ctx)
	if err != nil {
		logger.Error(err, "Failed to check job capacity")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if atCapacity {
		logger.Info("At maximum concurrent scans, requeuing")
		r.Recorder.Event(nucleiScan, corev1.EventTypeNormal, "AtCapacity",
			"Maximum concurrent scans reached, waiting for capacity")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Check if at least one target is available before scanning
	availableTargets, unavailableTargets := r.checkTargetsAvailability(ctx, nucleiScan.Spec.Targets)
	if len(availableTargets) == 0 {
		return r.handleTargetsUnavailable(ctx, nucleiScan, unavailableTargets)
	}

	// Reset retry count since targets are now available
	if nucleiScan.Status.RetryCount > 0 {
		logger.Info("Targets now available, resetting retry count", "previousRetries", nucleiScan.Status.RetryCount)
		nucleiScan.Status.RetryCount = 0
		nucleiScan.Status.LastRetryTime = nil
	}

	logger.Info("Creating scanner job", "availableTargets", len(availableTargets), "unavailableTargets", len(unavailableTargets))

	// Create the scanner job
	job, err := r.JobManager.CreateScanJob(ctx, nucleiScan)
	if err != nil {
		logger.Error(err, "Failed to create scanner job")
		r.Recorder.Event(nucleiScan, corev1.EventTypeWarning, "JobCreationFailed", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Update status to Running with job reference
	now := metav1.Now()
	nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhaseRunning
	nucleiScan.Status.JobRef = &nucleiv1alpha1.JobReference{
		Name:      job.Name,
		Namespace: job.Namespace,
		UID:       string(job.UID),
		StartTime: &now,
	}
	nucleiScan.Status.LastScanTime = &now
	nucleiScan.Status.LastError = ""
	nucleiScan.Status.ObservedGeneration = nucleiScan.Generation

	// Set condition
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeScanActive,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonScanRunning,
		Message:            fmt.Sprintf("Scanner job %s created for %d targets", job.Name, len(nucleiScan.Spec.Targets)),
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(nucleiScan, corev1.EventTypeNormal, "ScanJobCreated",
		fmt.Sprintf("Created scanner job %s", job.Name))

	// Requeue to monitor job status
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// handleTargetsUnavailable handles the case when no targets are available
func (r *NucleiScanReconciler) handleTargetsUnavailable(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan, unavailableTargets []string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Calculate backoff based on retry count
	retryCount := nucleiScan.Status.RetryCount
	backoffDuration := r.calculateBackoff(retryCount)

	logger.Info("No targets are available yet, waiting with backoff...",
		"unavailable", len(unavailableTargets),
		"retryCount", retryCount,
		"backoffDuration", backoffDuration)

	// Update condition and retry count
	now := metav1.Now()
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForTargets",
		Message:            fmt.Sprintf("Waiting for targets to become available (%d unavailable, retry #%d, next check in %v)", len(unavailableTargets), retryCount+1, backoffDuration),
		LastTransitionTime: now,
	})
	nucleiScan.Status.LastError = fmt.Sprintf("Targets not available: %v", unavailableTargets)
	nucleiScan.Status.RetryCount = retryCount + 1
	nucleiScan.Status.LastRetryTime = &now

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue with exponential backoff
	return ctrl.Result{RequeueAfter: backoffDuration}, nil
}

// handleRunningPhase handles the Running phase - monitors the scanner job
func (r *NucleiScanReconciler) handleRunningPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Check if we have a job reference
	if nucleiScan.Status.JobRef == nil {
		logger.Info("No job reference found, resetting to Pending")
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		nucleiScan.Status.LastError = "No job reference found, re-queuing scan"
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get the job - use namespace from JobRef (may be different from scan namespace)
	jobNamespace := nucleiScan.Status.JobRef.Namespace
	if jobNamespace == "" {
		// Fallback for backwards compatibility
		jobNamespace = nucleiScan.Namespace
	}
	job, err := r.JobManager.GetJob(ctx, nucleiScan.Status.JobRef.Name, jobNamespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Scanner job not found, resetting to Pending")
			nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
			nucleiScan.Status.LastError = "Scanner job not found, re-queuing scan"
			nucleiScan.Status.JobRef = nil
			if err := r.Status().Update(ctx, nucleiScan); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Update pod name if available
	if nucleiScan.Status.JobRef.PodName == "" {
		podName, _ := r.JobManager.GetJobPodName(ctx, job)
		if podName != "" {
			nucleiScan.Status.JobRef.PodName = podName
			if err := r.Status().Update(ctx, nucleiScan); err != nil {
				logger.Error(err, "Failed to update pod name")
			}
		}
	}

	// Check job status - the scanner pod updates the NucleiScan status directly
	// We just need to detect completion/failure for events
	if r.JobManager.IsJobSuccessful(job) {
		logger.Info("Scanner job completed successfully")
		r.Recorder.Event(nucleiScan, corev1.EventTypeNormal, "ScanCompleted",
			fmt.Sprintf("Scan completed with %d findings", len(nucleiScan.Status.Findings)))

		// Status is already updated by the scanner pod
		// Just schedule next scan if needed
		return r.scheduleNextScan(ctx, nucleiScan)
	}

	if r.JobManager.IsJobFailed(job) {
		reason := r.JobManager.GetJobFailureReason(job)
		logger.Info("Scanner job failed", "reason", reason)

		// Update status if not already updated by scanner
		if nucleiScan.Status.Phase != nucleiv1alpha1.ScanPhaseFailed {
			now := metav1.Now()
			nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhaseFailed
			nucleiScan.Status.LastError = reason
			nucleiScan.Status.CompletionTime = &now

			// Set conditions
			meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeScanActive,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonScanFailed,
				Message:            reason,
				LastTransitionTime: now,
			})

			meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
				Type:               ConditionTypeReady,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonScanFailed,
				Message:            reason,
				LastTransitionTime: now,
			})

			if err := r.Status().Update(ctx, nucleiScan); err != nil {
				return ctrl.Result{}, err
			}
		}

		r.Recorder.Event(nucleiScan, corev1.EventTypeWarning, "ScanFailed", reason)
		return ctrl.Result{}, nil
	}

	// Job still running, requeue to check again
	logger.V(1).Info("Scanner job still running", "job", job.Name)
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// checkTargetsAvailability checks which targets are reachable
func (r *NucleiScanReconciler) checkTargetsAvailability(ctx context.Context, targets []string) (available []string, unavailable []string) {
	log := logf.FromContext(ctx)

	for _, target := range targets {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
		if err != nil {
			log.V(1).Info("Failed to create request for target", "target", target, "error", err)
			unavailable = append(unavailable, target)
			continue
		}

		resp, err := r.HTTPClient.Do(req)
		if err != nil {
			log.V(1).Info("Target not available", "target", target, "error", err)
			unavailable = append(unavailable, target)
			continue
		}
		_ = resp.Body.Close()

		// Consider any response (even 4xx/5xx) as "available" - the service is responding
		available = append(available, target)
	}

	return available, unavailable
}

// handleCompletedPhase handles the Completed phase - checks for scheduled rescans
func (r *NucleiScanReconciler) handleCompletedPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if spec has changed (new generation)
	if nucleiScan.Generation != nucleiScan.Status.ObservedGeneration {
		log.Info("Spec changed, triggering new scan")
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		nucleiScan.Status.JobRef = nil // Clear old job reference
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if there's a schedule
	if nucleiScan.Spec.Schedule != "" {
		return r.checkScheduledScan(ctx, nucleiScan)
	}

	// Check if scan results are stale (older than RescanAge)
	if nucleiScan.Status.CompletionTime != nil {
		age := time.Since(nucleiScan.Status.CompletionTime.Time)
		if age > r.Config.RescanAge {
			log.Info("Scan results are stale, triggering rescan", "age", age, "maxAge", r.Config.RescanAge)
			nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
			nucleiScan.Status.JobRef = nil // Clear old job reference
			nucleiScan.Status.LastError = fmt.Sprintf("Automatic rescan triggered (results were %v old)", age.Round(time.Hour))
			if err := r.Status().Update(ctx, nucleiScan); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		// Schedule a requeue for when the results will become stale
		timeUntilStale := r.Config.RescanAge - age
		log.V(1).Info("Scan results still fresh, will check again later", "timeUntilStale", timeUntilStale)
		return ctrl.Result{RequeueAfter: timeUntilStale}, nil
	}

	return ctrl.Result{}, nil
}

// handleFailedPhase handles the Failed phase - implements retry logic
func (r *NucleiScanReconciler) handleFailedPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if spec has changed (new generation)
	if nucleiScan.Generation != nucleiScan.Status.ObservedGeneration {
		log.Info("Spec changed, triggering new scan")
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		nucleiScan.Status.JobRef = nil // Clear old job reference
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// For now, don't auto-retry failed scans
	// Users can trigger a retry by updating the spec
	log.Info("Scan failed, waiting for manual intervention or spec change")
	return ctrl.Result{}, nil
}

// scheduleNextScan calculates and sets the next scheduled scan time
func (r *NucleiScanReconciler) scheduleNextScan(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// If there's no schedule, nothing to do
	if nucleiScan.Spec.Schedule == "" {
		return ctrl.Result{}, nil
	}

	// Parse cron schedule
	nextTime, err := getNextScheduleTime(nucleiScan.Spec.Schedule, time.Now())
	if err != nil {
		log.Error(err, "Failed to parse schedule", "schedule", nucleiScan.Spec.Schedule)
		return ctrl.Result{}, nil
	}

	nucleiScan.Status.NextScheduledTime = &metav1.Time{Time: nextTime}
	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	// Calculate requeue duration
	requeueAfter := time.Until(nextTime)
	if requeueAfter < 0 {
		requeueAfter = defaultScheduleRequeue
	}

	log.Info("Scheduled next scan", "nextTime", nextTime, "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// checkScheduledScan checks if it's time for a scheduled scan
func (r *NucleiScanReconciler) checkScheduledScan(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if nucleiScan.Status.NextScheduledTime == nil {
		// No next scheduled time set, calculate it
		return r.scheduleNextScan(ctx, nucleiScan)
	}

	now := time.Now()
	nextTime := nucleiScan.Status.NextScheduledTime.Time

	if now.After(nextTime) {
		log.Info("Scheduled scan time reached, triggering scan")
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		nucleiScan.Status.NextScheduledTime = nil
		nucleiScan.Status.JobRef = nil // Clear old job reference
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Not yet time, requeue until scheduled time
	requeueAfter := time.Until(nextTime)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// updateCondition is a helper to update a condition and return a result
func (r *NucleiScanReconciler) updateCondition(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan,
	condType string, status metav1.ConditionStatus, reason, message string) (ctrl.Result, error) {

	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// getNextScheduleTime parses a cron expression and returns the next scheduled time
// This is a simplified implementation - for production, consider using a proper cron library
func getNextScheduleTime(schedule string, from time.Time) (time.Time, error) {
	// Simple implementation for common intervals
	// Format: "@every <duration>" or standard cron
	if len(schedule) > 7 && schedule[:7] == "@every " {
		duration, err := time.ParseDuration(schedule[7:])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration in schedule: %w", err)
		}
		return from.Add(duration), nil
	}

	// For standard cron expressions, we'd need a cron parser library
	// For now, default to 24 hours if we can't parse
	return from.Add(24 * time.Hour), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NucleiScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nucleiv1alpha1.NucleiScan{}).
		Owns(&batchv1.Job{}). // Watch Jobs owned by NucleiScan
		Named("nucleiscan").
		Complete(r)
}
