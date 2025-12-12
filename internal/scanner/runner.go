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

package scanner

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
)

// RunnerConfig holds configuration for the scanner runner
type RunnerConfig struct {
	// ScanName is the name of the NucleiScan to execute
	ScanName string

	// ScanNamespace is the namespace of the NucleiScan
	ScanNamespace string

	// NucleiBinaryPath is the path to the nuclei binary
	NucleiBinaryPath string

	// TemplatesPath is the path to nuclei templates
	TemplatesPath string
}

// Runner executes a single scan and updates the NucleiScan status
type Runner struct {
	config  RunnerConfig
	client  client.Client
	scanner Scanner
}

// NewRunner creates a new scanner runner
func NewRunner(config RunnerConfig) (*Runner, error) {
	// Set up logging
	log.SetLogger(zap.New(zap.UseDevMode(false)))
	logger := log.Log.WithName("scanner-runner")

	// Create scheme
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(nucleiv1alpha1.AddToScheme(scheme))

	// Get in-cluster config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	// Create client
	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Create scanner with configuration
	scannerConfig := Config{
		NucleiBinaryPath: config.NucleiBinaryPath,
		TemplatesPath:    config.TemplatesPath,
	}
	// Use defaults if not specified
	if scannerConfig.NucleiBinaryPath == "" {
		scannerConfig.NucleiBinaryPath = "nuclei"
	}
	nucleiScanner := NewNucleiScanner(scannerConfig)

	logger.Info("Scanner runner initialized",
		"scanName", config.ScanName,
		"scanNamespace", config.ScanNamespace)

	return &Runner{
		config:  config,
		client:  k8sClient,
		scanner: nucleiScanner,
	}, nil
}

// Run executes the scan and updates the NucleiScan status
func (r *Runner) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("scanner-runner")

	// Fetch the NucleiScan
	scan := &nucleiv1alpha1.NucleiScan{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      r.config.ScanName,
		Namespace: r.config.ScanNamespace,
	}, scan)
	if err != nil {
		return fmt.Errorf("failed to get NucleiScan: %w", err)
	}

	logger.Info("Starting scan",
		"targets", len(scan.Spec.Targets),
		"templates", scan.Spec.Templates,
		"severity", scan.Spec.Severity)

	// Update status to indicate scan has started
	startTime := metav1.Now()
	scan.Status.ScanStartTime = &startTime
	if err := r.client.Status().Update(ctx, scan); err != nil {
		logger.Error(err, "Failed to update scan start time")
		// Continue anyway - this is not critical
	}

	// Build scan options
	options := ScanOptions{
		Templates: scan.Spec.Templates,
		Severity:  scan.Spec.Severity,
	}

	// Execute the scan
	scanStartTime := time.Now()
	result, err := r.scanner.Scan(ctx, scan.Spec.Targets, options)
	scanDuration := time.Since(scanStartTime)

	// Re-fetch the scan to avoid conflicts
	if fetchErr := r.client.Get(ctx, types.NamespacedName{
		Name:      r.config.ScanName,
		Namespace: r.config.ScanNamespace,
	}, scan); fetchErr != nil {
		return fmt.Errorf("failed to re-fetch NucleiScan: %w", fetchErr)
	}

	// Update status based on result
	completionTime := metav1.Now()
	scan.Status.CompletionTime = &completionTime

	if err != nil {
		logger.Error(err, "Scan failed")
		scan.Status.Phase = nucleiv1alpha1.ScanPhaseFailed
		scan.Status.LastError = err.Error()
	} else {
		logger.Info("Scan completed successfully",
			"findings", len(result.Findings),
			"duration", scanDuration)

		scan.Status.Phase = nucleiv1alpha1.ScanPhaseCompleted
		scan.Status.Findings = result.Findings
		scan.Status.Summary = &nucleiv1alpha1.ScanSummary{
			TotalFindings:      len(result.Findings),
			FindingsBySeverity: countFindingsBySeverity(result.Findings),
			TargetsScanned:     len(scan.Spec.Targets),
			DurationSeconds:    int64(scanDuration.Seconds()),
		}
		scan.Status.LastError = ""
	}

	// Update the status
	if err := r.client.Status().Update(ctx, scan); err != nil {
		return fmt.Errorf("failed to update NucleiScan status: %w", err)
	}

	logger.Info("Scan status updated",
		"phase", scan.Status.Phase,
		"findings", len(scan.Status.Findings))

	return nil
}

// countFindingsBySeverity counts findings by severity level
func countFindingsBySeverity(findings []nucleiv1alpha1.Finding) map[string]int {
	counts := make(map[string]int)
	for _, f := range findings {
		counts[f.Severity]++
	}
	return counts
}

// RunScannerMode is the entry point for scanner mode
func RunScannerMode(scanName, scanNamespace string) error {
	// Get configuration from environment
	config := RunnerConfig{
		ScanName:         scanName,
		ScanNamespace:    scanNamespace,
		NucleiBinaryPath: os.Getenv("NUCLEI_BINARY_PATH"),
		TemplatesPath:    os.Getenv("NUCLEI_TEMPLATES_PATH"),
	}

	if config.ScanName == "" || config.ScanNamespace == "" {
		return fmt.Errorf("scan name and namespace are required")
	}

	runner, err := NewRunner(config)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	ctx := context.Background()
	return runner.Run(ctx)
}
