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
	"reflect"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	"github.com/mortenolsen/nuclei-operator/internal/annotations"
)

// IngressReconciler reconciles Ingress objects and creates NucleiScan resources
type IngressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Ingress events and creates/updates corresponding NucleiScan resources
func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Ingress resource
	ingress := &networkingv1.Ingress{}
	if err := r.Get(ctx, req.NamespacedName, ingress); err != nil {
		if apierrors.IsNotFound(err) {
			// Ingress was deleted - NucleiScan will be garbage collected via ownerReference
			log.Info("Ingress not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Ingress")
		return ctrl.Result{}, err
	}

	// Parse annotations to get scan configuration
	scanConfig := annotations.ParseAnnotations(ingress.Annotations)

	// Define the NucleiScan name based on the Ingress name
	nucleiScanName := fmt.Sprintf("%s-scan", ingress.Name)

	// Check if a NucleiScan already exists for this Ingress
	existingScan := &nucleiv1alpha1.NucleiScan{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: ingress.Namespace,
		Name:      nucleiScanName,
	}, existingScan)

	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get existing NucleiScan")
		return ctrl.Result{}, err
	}

	// Check if scanning is disabled via annotations
	if !scanConfig.IsEnabled() {
		// Scanning disabled - delete existing NucleiScan if it exists
		if err == nil {
			log.Info("Scanning disabled via annotation, deleting existing NucleiScan", "nucleiScan", nucleiScanName)
			if err := r.Delete(ctx, existingScan); err != nil && !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to delete NucleiScan")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Extract target URLs from the Ingress
	targets := extractURLsFromIngress(ingress)
	if len(targets) == 0 {
		log.Info("No targets extracted from Ingress, skipping NucleiScan creation")
		return ctrl.Result{}, nil
	}

	if apierrors.IsNotFound(err) {
		// Create a new NucleiScan
		spec := nucleiv1alpha1.NucleiScanSpec{
			SourceRef: nucleiv1alpha1.SourceReference{
				APIVersion: "networking.k8s.io/v1",
				Kind:       "Ingress",
				Name:       ingress.Name,
				Namespace:  ingress.Namespace,
				UID:        string(ingress.UID),
			},
			Targets: targets,
		}

		// Apply annotation configuration to the spec
		scanConfig.ApplyToNucleiScanSpec(&spec)

		nucleiScan := &nucleiv1alpha1.NucleiScan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nucleiScanName,
				Namespace: ingress.Namespace,
			},
			Spec: spec,
		}

		// Set owner reference for garbage collection
		if err := controllerutil.SetControllerReference(ingress, nucleiScan, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on NucleiScan")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, nucleiScan); err != nil {
			log.Error(err, "Failed to create NucleiScan")
			return ctrl.Result{}, err
		}

		log.Info("Created NucleiScan for Ingress", "nucleiScan", nucleiScanName, "targets", targets)
		return ctrl.Result{}, nil
	}

	// NucleiScan exists - check if it needs to be updated
	needsUpdate := false

	// Check if targets changed
	if !reflect.DeepEqual(existingScan.Spec.Targets, targets) {
		existingScan.Spec.Targets = targets
		needsUpdate = true
	}

	// Also update the SourceRef UID in case it changed (e.g., Ingress was recreated)
	if existingScan.Spec.SourceRef.UID != string(ingress.UID) {
		existingScan.Spec.SourceRef.UID = string(ingress.UID)
		needsUpdate = true
	}

	// Apply annotation configuration
	scanConfig.ApplyToNucleiScanSpec(&existingScan.Spec)

	if needsUpdate {
		if err := r.Update(ctx, existingScan); err != nil {
			log.Error(err, "Failed to update NucleiScan")
			return ctrl.Result{}, err
		}

		log.Info("Updated NucleiScan for Ingress", "nucleiScan", nucleiScanName, "targets", targets)
	}

	return ctrl.Result{}, nil
}

// extractURLsFromIngress extracts target URLs from an Ingress resource
func extractURLsFromIngress(ingress *networkingv1.Ingress) []string {
	var urls []string
	tlsHosts := make(map[string]bool)

	// Build a map of TLS hosts for quick lookup
	for _, tls := range ingress.Spec.TLS {
		for _, host := range tls.Hosts {
			tlsHosts[host] = true
		}
	}

	// Extract URLs from rules
	for _, rule := range ingress.Spec.Rules {
		if rule.Host == "" {
			continue
		}

		// Determine the scheme based on TLS configuration
		scheme := "http"
		if tlsHosts[rule.Host] {
			scheme = "https"
		}

		// If there are HTTP paths defined, create URLs for each path
		if rule.HTTP != nil && len(rule.HTTP.Paths) > 0 {
			for _, path := range rule.HTTP.Paths {
				pathStr := path.Path
				if pathStr == "" {
					pathStr = "/"
				}
				url := fmt.Sprintf("%s://%s%s", scheme, rule.Host, pathStr)
				urls = append(urls, url)
			}
		} else {
			// No paths defined, just use the host
			url := fmt.Sprintf("%s://%s", scheme, rule.Host)
			urls = append(urls, url)
		}
	}

	// Deduplicate URLs
	return deduplicateStrings(urls)
}

// deduplicateStrings removes duplicate strings from a slice while preserving order
func deduplicateStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// SetupWithManager sets up the controller with the Manager
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&nucleiv1alpha1.NucleiScan{}).
		Named("ingress").
		Complete(r)
}
