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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	"github.com/mortenolsen/nuclei-operator/internal/annotations"
)

// VirtualServiceReconciler reconciles VirtualService objects and creates NucleiScan resources
type VirtualServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get
// +kubebuilder:rbac:groups=nuclei.homelab.mortenolsen.pro,resources=nucleiscans,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles VirtualService events and creates/updates corresponding NucleiScan resources
func (r *VirtualServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the VirtualService resource
	virtualService := &istionetworkingv1beta1.VirtualService{}
	if err := r.Get(ctx, req.NamespacedName, virtualService); err != nil {
		if apierrors.IsNotFound(err) {
			// VirtualService was deleted - NucleiScan will be garbage collected via ownerReference
			log.Info("VirtualService not found, likely deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get VirtualService")
		return ctrl.Result{}, err
	}

	// Parse annotations to get scan configuration
	scanConfig := annotations.ParseAnnotations(virtualService.Annotations)

	// Define the NucleiScan name based on the VirtualService name
	nucleiScanName := fmt.Sprintf("%s-scan", virtualService.Name)

	// Check if a NucleiScan already exists for this VirtualService
	existingScan := &nucleiv1alpha1.NucleiScan{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: virtualService.Namespace,
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

	// Extract target URLs from the VirtualService
	targets := extractURLsFromVirtualService(virtualService)
	if len(targets) == 0 {
		log.Info("No targets extracted from VirtualService, skipping NucleiScan creation")
		return ctrl.Result{}, nil
	}

	if apierrors.IsNotFound(err) {
		// Create a new NucleiScan
		spec := nucleiv1alpha1.NucleiScanSpec{
			SourceRef: nucleiv1alpha1.SourceReference{
				APIVersion: "networking.istio.io/v1beta1",
				Kind:       "VirtualService",
				Name:       virtualService.Name,
				Namespace:  virtualService.Namespace,
				UID:        string(virtualService.UID),
			},
			Targets: targets,
		}

		// Apply annotation configuration to the spec
		scanConfig.ApplyToNucleiScanSpec(&spec)

		nucleiScan := &nucleiv1alpha1.NucleiScan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nucleiScanName,
				Namespace: virtualService.Namespace,
			},
			Spec: spec,
		}

		// Set owner reference for garbage collection
		if err := controllerutil.SetControllerReference(virtualService, nucleiScan, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference on NucleiScan")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, nucleiScan); err != nil {
			log.Error(err, "Failed to create NucleiScan")
			return ctrl.Result{}, err
		}

		log.Info("Created NucleiScan for VirtualService", "nucleiScan", nucleiScanName, "targets", targets)
		return ctrl.Result{}, nil
	}

	// NucleiScan exists - check if it needs to be updated
	needsUpdate := false

	// Check if targets changed
	if !reflect.DeepEqual(existingScan.Spec.Targets, targets) {
		existingScan.Spec.Targets = targets
		needsUpdate = true
	}

	// Also update the SourceRef UID in case it changed (e.g., VirtualService was recreated)
	if existingScan.Spec.SourceRef.UID != string(virtualService.UID) {
		existingScan.Spec.SourceRef.UID = string(virtualService.UID)
		needsUpdate = true
	}

	// Apply annotation configuration
	scanConfig.ApplyToNucleiScanSpec(&existingScan.Spec)

	if needsUpdate {
		if err := r.Update(ctx, existingScan); err != nil {
			log.Error(err, "Failed to update NucleiScan")
			return ctrl.Result{}, err
		}

		log.Info("Updated NucleiScan for VirtualService", "nucleiScan", nucleiScanName, "targets", targets)
	}

	return ctrl.Result{}, nil
}

// extractURLsFromVirtualService extracts target URLs from a VirtualService resource
func extractURLsFromVirtualService(vs *istionetworkingv1beta1.VirtualService) []string {
	var urls []string

	// Check if VirtualService has gateways defined (indicates external traffic)
	// If no gateways or only "mesh" gateway, it's internal service-to-service
	hasExternalGateway := false
	for _, gw := range vs.Spec.Gateways {
		if gw != "mesh" {
			hasExternalGateway = true
			break
		}
	}

	// If no external gateway, skip this VirtualService
	if !hasExternalGateway && len(vs.Spec.Gateways) > 0 {
		return urls
	}

	// Extract URLs from hosts
	for _, host := range vs.Spec.Hosts {
		// Skip wildcard hosts and internal service names (no dots or starts with *)
		if strings.HasPrefix(host, "*") {
			continue
		}

		// Skip internal Kubernetes service names (typically don't contain dots or are short names)
		// External hosts typically have FQDNs like "myapp.example.com"
		if !strings.Contains(host, ".") {
			continue
		}

		// Skip Kubernetes internal service FQDNs (*.svc.cluster.local)
		if strings.Contains(host, ".svc.cluster.local") || strings.Contains(host, ".svc.") {
			continue
		}

		// Default to HTTPS for external hosts (security scanning)
		scheme := "https"

		// Extract paths from HTTP routes if defined
		pathsFound := false
		if vs.Spec.Http != nil {
			for _, httpRoute := range vs.Spec.Http {
				if httpRoute.Match != nil {
					for _, match := range httpRoute.Match {
						if match.Uri != nil {
							if match.Uri.GetPrefix() != "" {
								url := fmt.Sprintf("%s://%s%s", scheme, host, match.Uri.GetPrefix())
								urls = append(urls, url)
								pathsFound = true
							} else if match.Uri.GetExact() != "" {
								url := fmt.Sprintf("%s://%s%s", scheme, host, match.Uri.GetExact())
								urls = append(urls, url)
								pathsFound = true
							} else if match.Uri.GetRegex() != "" {
								// For regex patterns, just use the base URL
								// We can't enumerate all possible matches
								url := fmt.Sprintf("%s://%s", scheme, host)
								urls = append(urls, url)
								pathsFound = true
							}
						}
					}
				}
			}
		}

		// If no specific paths found, add base URL
		if !pathsFound {
			url := fmt.Sprintf("%s://%s", scheme, host)
			urls = append(urls, url)
		}
	}

	// Deduplicate URLs
	return deduplicateStrings(urls)
}

// SetupWithManager sets up the controller with the Manager
func (r *VirtualServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&istionetworkingv1beta1.VirtualService{}).
		Owns(&nucleiv1alpha1.NucleiScan{}).
		Named("virtualservice").
		Complete(r)
}
