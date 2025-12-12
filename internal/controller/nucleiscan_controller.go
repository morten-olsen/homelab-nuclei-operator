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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	"github.com/mortenolsen/nuclei-operator/internal/scanner"
)

const (
	// finalizerName is the finalizer used by this controller
	finalizerName = "nuclei.homelab.mortenolsen.pro/finalizer"

	// Default requeue intervals
	defaultRequeueAfter      = 30 * time.Second
	defaultScheduleRequeue   = 1 * time.Minute
	defaultErrorRequeueAfter = 1 * time.Minute
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

// NucleiScanReconciler reconciles a NucleiScan object
type NucleiScanReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Scanner scanner.Scanner
}

// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
		// This shouldn't happen in our synchronous implementation
		// but handle it gracefully
		return r.handlePendingPhase(ctx, nucleiScan)
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
		log.Info("Handling deletion, performing cleanup")

		// Perform any cleanup here (e.g., cancel running scans)
		// In our synchronous implementation, there's nothing to clean up

		// Remove finalizer
		controllerutil.RemoveFinalizer(nucleiScan, finalizerName)
		if err := r.Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// handlePendingPhase handles the Pending phase - starts a new scan
func (r *NucleiScanReconciler) handlePendingPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Starting scan", "targets", len(nucleiScan.Spec.Targets))

	// Update status to Running
	now := metav1.Now()
	nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhaseRunning
	nucleiScan.Status.LastScanTime = &now
	nucleiScan.Status.LastError = ""
	nucleiScan.Status.ObservedGeneration = nucleiScan.Generation

	// Set condition
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeScanActive,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonScanRunning,
		Message:            "Scan is in progress",
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	// Build scan options
	options := scanner.ScanOptions{
		Templates: nucleiScan.Spec.Templates,
		Severity:  nucleiScan.Spec.Severity,
		Timeout:   30 * time.Minute, // Default timeout
	}

	// Execute the scan
	result, err := r.Scanner.Scan(ctx, nucleiScan.Spec.Targets, options)
	if err != nil {
		log.Error(err, "Scan failed")
		return r.handleScanError(ctx, nucleiScan, err)
	}

	// Update status with results
	return r.handleScanSuccess(ctx, nucleiScan, result)
}

// handleScanSuccess updates the status after a successful scan
func (r *NucleiScanReconciler) handleScanSuccess(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan, result *scanner.ScanResult) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Scan completed successfully", "findings", len(result.Findings), "duration", result.Duration)

	now := metav1.Now()
	nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhaseCompleted
	nucleiScan.Status.CompletionTime = &now
	nucleiScan.Status.Findings = result.Findings
	nucleiScan.Status.Summary = &result.Summary
	nucleiScan.Status.LastError = ""

	// Set conditions
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeScanActive,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonScanCompleted,
		Message:            "Scan completed successfully",
		LastTransitionTime: now,
	})

	message := fmt.Sprintf("Scan completed with %d findings", len(result.Findings))
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonScanCompleted,
		Message:            message,
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	// If there's a schedule, calculate next scan time
	if nucleiScan.Spec.Schedule != "" {
		return r.scheduleNextScan(ctx, nucleiScan)
	}

	return ctrl.Result{}, nil
}

// handleScanError updates the status after a failed scan
func (r *NucleiScanReconciler) handleScanError(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan, scanErr error) (ctrl.Result, error) {
	now := metav1.Now()
	nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhaseFailed
	nucleiScan.Status.CompletionTime = &now
	nucleiScan.Status.LastError = scanErr.Error()

	// Set conditions
	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeScanActive,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonScanFailed,
		Message:            scanErr.Error(),
		LastTransitionTime: now,
	})

	meta.SetStatusCondition(&nucleiScan.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonScanFailed,
		Message:            scanErr.Error(),
		LastTransitionTime: now,
	})

	if err := r.Status().Update(ctx, nucleiScan); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue with backoff for retry
	return ctrl.Result{RequeueAfter: defaultErrorRequeueAfter}, nil
}

// handleCompletedPhase handles the Completed phase - checks for scheduled rescans
func (r *NucleiScanReconciler) handleCompletedPhase(ctx context.Context, nucleiScan *nucleiv1alpha1.NucleiScan) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if spec has changed (new generation)
	if nucleiScan.Generation != nucleiScan.Status.ObservedGeneration {
		log.Info("Spec changed, triggering new scan")
		nucleiScan.Status.Phase = nucleiv1alpha1.ScanPhasePending
		if err := r.Status().Update(ctx, nucleiScan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if there's a schedule
	if nucleiScan.Spec.Schedule != "" {
		return r.checkScheduledScan(ctx, nucleiScan)
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
		Named("nucleiscan").
		Complete(r)
}
