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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	"github.com/mortenolsen/nuclei-operator/internal/controller"
	"github.com/mortenolsen/nuclei-operator/internal/jobmanager"
	"github.com/mortenolsen/nuclei-operator/internal/scanner"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(nucleiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
	utilruntime.Must(istionetworkingv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)

	// Scanner mode flags
	var mode string
	var scanName string
	var scanNamespace string

	flag.StringVar(&mode, "mode", "controller", "Run mode: 'controller' or 'scanner'")
	flag.StringVar(&scanName, "scan-name", "", "Name of the NucleiScan to execute (scanner mode only)")
	flag.StringVar(&scanNamespace, "scan-namespace", "", "Namespace of the NucleiScan (scanner mode only)")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Check if running in scanner mode
	if mode == "scanner" {
		if err := scanner.RunScannerMode(scanName, scanNamespace); err != nil {
			setupLog.Error(err, "Scanner mode failed")
			os.Exit(1)
		}
		os.Exit(0)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "501467ce.homelab.mortenolsen.pro",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Parse environment variables for JobManager configuration
	scannerImage := os.Getenv("SCANNER_IMAGE")
	if scannerImage == "" {
		scannerImage = jobmanager.DefaultScannerImage
	}

	scannerTimeout := 30 * time.Minute
	if v := os.Getenv("SCANNER_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			scannerTimeout = d
		}
	}

	maxConcurrentScans := 5
	if v := os.Getenv("MAX_CONCURRENT_SCANS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxConcurrentScans = n
		}
	}

	ttlAfterFinished := int32(3600)
	if v := os.Getenv("JOB_TTL_AFTER_FINISHED"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ttlAfterFinished = int32(n)
		}
	}

	scannerServiceAccount := os.Getenv("SCANNER_SERVICE_ACCOUNT")
	if scannerServiceAccount == "" {
		scannerServiceAccount = "nuclei-scanner"
	}

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		// Try to read from the downward API file
		if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			operatorNamespace = string(data)
		} else {
			operatorNamespace = "nuclei-operator-system"
		}
	}

	defaultTemplates := []string{}
	if v := os.Getenv("DEFAULT_TEMPLATES"); v != "" {
		defaultTemplates = strings.Split(v, ",")
	}

	defaultSeverity := []string{}
	if v := os.Getenv("DEFAULT_SEVERITY"); v != "" {
		defaultSeverity = strings.Split(v, ",")
	}

	// Create the JobManager configuration
	jobManagerConfig := jobmanager.Config{
		ScannerImage:       scannerImage,
		DefaultTimeout:     scannerTimeout,
		TTLAfterFinished:   ttlAfterFinished,
		BackoffLimit:       2,
		MaxConcurrent:      maxConcurrentScans,
		ServiceAccountName: scannerServiceAccount,
		OperatorNamespace:  operatorNamespace,
		DefaultResources:   jobmanager.DefaultConfig().DefaultResources,
		DefaultTemplates:   defaultTemplates,
		DefaultSeverity:    defaultSeverity,
	}

	// Create the JobManager for scanner job management
	jobMgr := jobmanager.NewJobManager(
		mgr.GetClient(),
		mgr.GetScheme(),
		jobManagerConfig,
	)

	// Run startup recovery to handle orphaned scans from previous operator instance
	setupLog.Info("Running startup recovery")
	if err := runStartupRecovery(mgr.GetClient(), jobMgr); err != nil {
		setupLog.Error(err, "Startup recovery failed")
		// Don't exit - continue with normal operation
	}

	// Create a context that will be cancelled when the manager stops
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start periodic cleanup goroutine
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := jobMgr.CleanupOrphanedJobs(ctx); err != nil {
					setupLog.Error(err, "Periodic cleanup failed")
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Create the NucleiScan reconciler with JobManager
	if err := controller.NewNucleiScanReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		mgr.GetEventRecorderFor("nucleiscan-controller"),
		jobMgr,
		controller.DefaultReconcilerConfig(),
	).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NucleiScan")
		os.Exit(1)
	}
	if err := (&controller.IngressReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Ingress")
		os.Exit(1)
	}
	if err := (&controller.VirtualServiceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VirtualService")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// runStartupRecovery handles orphaned scans and jobs from previous operator instance
func runStartupRecovery(c client.Client, jobMgr *jobmanager.JobManager) error {
	ctx := context.Background()

	// List all NucleiScans in Running phase
	scanList := &nucleiv1alpha1.NucleiScanList{}
	if err := c.List(ctx, scanList); err != nil {
		return fmt.Errorf("failed to list NucleiScans: %w", err)
	}

	for _, scan := range scanList.Items {
		if scan.Status.Phase != nucleiv1alpha1.ScanPhaseRunning {
			continue
		}

		// Check if the referenced job still exists
		if scan.Status.JobRef != nil {
			job, err := jobMgr.GetJob(ctx, scan.Status.JobRef.Name, scan.Namespace)
			if err != nil {
				if apierrors.IsNotFound(err) {
					// Job is gone - reset scan to Pending
					scan.Status.Phase = nucleiv1alpha1.ScanPhasePending
					scan.Status.LastError = "Recovered from operator restart - job not found"
					scan.Status.JobRef = nil
					if updateErr := c.Status().Update(ctx, &scan); updateErr != nil {
						return fmt.Errorf("failed to update scan %s: %w", scan.Name, updateErr)
					}
					continue
				}
				return fmt.Errorf("failed to get job for scan %s: %w", scan.Name, err)
			}

			// Job exists - check if it's completed but status wasn't updated
			if jobMgr.IsJobComplete(job) {
				// The scanner pod should have updated the status
				// If it didn't, mark as failed
				if scan.Status.Phase == nucleiv1alpha1.ScanPhaseRunning {
					if jobMgr.IsJobFailed(job) {
						scan.Status.Phase = nucleiv1alpha1.ScanPhaseFailed
						scan.Status.LastError = "Job completed during operator downtime: " + jobMgr.GetJobFailureReason(job)
					} else {
						// Job succeeded but status wasn't updated - this shouldn't happen
						// but handle it gracefully
						scan.Status.Phase = nucleiv1alpha1.ScanPhaseFailed
						scan.Status.LastError = "Job completed during operator downtime but status was not updated"
					}
					if updateErr := c.Status().Update(ctx, &scan); updateErr != nil {
						return fmt.Errorf("failed to update scan %s: %w", scan.Name, updateErr)
					}
				}
			}
		} else {
			// No job reference but Running - invalid state
			scan.Status.Phase = nucleiv1alpha1.ScanPhasePending
			scan.Status.LastError = "Recovered from invalid state - no job reference"
			if updateErr := c.Status().Update(ctx, &scan); updateErr != nil {
				return fmt.Errorf("failed to update scan %s: %w", scan.Name, updateErr)
			}
		}
	}

	// Clean up orphaned jobs
	if err := jobMgr.CleanupOrphanedJobs(ctx); err != nil {
		return fmt.Errorf("failed to cleanup orphaned jobs: %w", err)
	}

	return nil
}
