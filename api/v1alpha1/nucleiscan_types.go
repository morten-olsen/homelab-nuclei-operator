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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SourceReference identifies the Ingress or VirtualService that triggered this scan
type SourceReference struct {
	// APIVersion of the source resource
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion"`

	// Kind of the source resource - Ingress or VirtualService
	// +kubebuilder:validation:Enum=Ingress;VirtualService
	Kind string `json:"kind"`

	// Name of the source resource
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the source resource
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// UID of the source resource for owner reference
	// +kubebuilder:validation:Required
	UID string `json:"uid"`
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
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

// NucleiScanSpec defines the desired state of NucleiScan
type NucleiScanSpec struct {
	// SourceRef references the Ingress or VirtualService being scanned
	// +kubebuilder:validation:Required
	SourceRef SourceReference `json:"sourceRef"`

	// Targets is the list of URLs to scan, extracted from the source resource
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Targets []string `json:"targets"`

	// Templates specifies which Nuclei templates to use
	// If empty, uses default templates
	// +optional
	Templates []string `json:"templates,omitempty"`

	// Severity filters scan results by severity level
	// +kubebuilder:validation:Enum=info;low;medium;high;critical
	// +optional
	Severity []string `json:"severity,omitempty"`

	// Schedule for periodic rescanning in cron format
	// If empty, scan runs once
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Suspend prevents scheduled scans from running
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// ScannerConfig allows overriding scanner settings for this scan
	// +optional
	ScannerConfig *ScannerConfig `json:"scannerConfig,omitempty"`
}

// ScanPhase represents the current phase of the scan
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type ScanPhase string

const (
	ScanPhasePending   ScanPhase = "Pending"
	ScanPhaseRunning   ScanPhase = "Running"
	ScanPhaseCompleted ScanPhase = "Completed"
	ScanPhaseFailed    ScanPhase = "Failed"
)

// Finding represents a single Nuclei scan finding
type Finding struct {
	// TemplateID is the Nuclei template identifier
	TemplateID string `json:"templateId"`

	// TemplateName is the human-readable template name
	// +optional
	TemplateName string `json:"templateName,omitempty"`

	// Severity of the finding
	Severity string `json:"severity"`

	// Type of the finding - http, dns, ssl, etc.
	// +optional
	Type string `json:"type,omitempty"`

	// Host that was scanned
	Host string `json:"host"`

	// MatchedAt is the specific URL or endpoint where the issue was found
	// +optional
	MatchedAt string `json:"matchedAt,omitempty"`

	// ExtractedResults contains any data extracted by the template
	// +optional
	ExtractedResults []string `json:"extractedResults,omitempty"`

	// Description provides details about the finding
	// +optional
	Description string `json:"description,omitempty"`

	// Reference contains URLs to additional information about the finding
	// +optional
	Reference []string `json:"reference,omitempty"`

	// Tags associated with the finding
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Timestamp when the finding was discovered
	Timestamp metav1.Time `json:"timestamp"`

	// Metadata contains additional template metadata
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Metadata *runtime.RawExtension `json:"metadata,omitempty"`
}

// ScanSummary provides aggregated statistics about the scan
type ScanSummary struct {
	// TotalFindings is the total number of findings
	TotalFindings int `json:"totalFindings"`

	// FindingsBySeverity breaks down findings by severity level
	// +optional
	FindingsBySeverity map[string]int `json:"findingsBySeverity,omitempty"`

	// TargetsScanned is the number of targets that were scanned
	TargetsScanned int `json:"targetsScanned"`

	// DurationSeconds is the duration of the scan in seconds
	// +optional
	DurationSeconds int64 `json:"durationSeconds,omitempty"`
}

// NucleiScanStatus defines the observed state of NucleiScan
type NucleiScanStatus struct {
	// Phase represents the current scan phase
	// +optional
	Phase ScanPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastScanTime is when the last scan was initiated
	// +optional
	LastScanTime *metav1.Time `json:"lastScanTime,omitempty"`

	// CompletionTime is when the last scan completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// NextScheduledTime is when the next scheduled scan will run
	// +optional
	NextScheduledTime *metav1.Time `json:"nextScheduledTime,omitempty"`

	// Summary provides aggregated scan statistics
	// +optional
	Summary *ScanSummary `json:"summary,omitempty"`

	// Findings contains the array of scan results from Nuclei JSONL output
	// Each element is a parsed JSON object from Nuclei output
	// +optional
	Findings []Finding `json:"findings,omitempty"`

	// LastError contains the error message if the scan failed
	// +optional
	LastError string `json:"lastError,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// RetryCount tracks the number of consecutive availability check retries
	// Used for exponential backoff when waiting for targets
	// +optional
	RetryCount int `json:"retryCount,omitempty"`

	// LastRetryTime is when the last availability check retry occurred
	// +optional
	LastRetryTime *metav1.Time `json:"lastRetryTime,omitempty"`

	// JobRef references the current or last scanner job
	// +optional
	JobRef *JobReference `json:"jobRef,omitempty"`

	// ScanStartTime is when the scanner pod actually started scanning
	// +optional
	ScanStartTime *metav1.Time `json:"scanStartTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ns;nscan
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Findings",type=integer,JSONPath=`.status.summary.totalFindings`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.kind`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NucleiScan is the Schema for the nucleiscans API
type NucleiScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NucleiScanSpec   `json:"spec,omitempty"`
	Status NucleiScanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NucleiScanList contains a list of NucleiScan
type NucleiScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NucleiScan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NucleiScan{}, &NucleiScanList{})
}
