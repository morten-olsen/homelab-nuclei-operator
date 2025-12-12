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

package annotations

import (
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
)

const (
	// AnnotationPrefix is the prefix for all nuclei annotations
	AnnotationPrefix = "nuclei.homelab.mortenolsen.pro/"

	// AnnotationEnabled controls whether scanning is enabled for a resource
	AnnotationEnabled = AnnotationPrefix + "enabled"

	// AnnotationTemplates specifies comma-separated template paths or tags
	AnnotationTemplates = AnnotationPrefix + "templates"

	// AnnotationSeverity specifies comma-separated severity filter
	AnnotationSeverity = AnnotationPrefix + "severity"

	// AnnotationSchedule specifies the cron schedule for periodic scans
	AnnotationSchedule = AnnotationPrefix + "schedule"

	// AnnotationTimeout specifies the scan timeout
	AnnotationTimeout = AnnotationPrefix + "timeout"

	// AnnotationScannerImage overrides the scanner image
	AnnotationScannerImage = AnnotationPrefix + "scanner-image"

	// AnnotationExcludeTemplates specifies templates to exclude
	AnnotationExcludeTemplates = AnnotationPrefix + "exclude-templates"

	// AnnotationRateLimit specifies requests per second limit
	AnnotationRateLimit = AnnotationPrefix + "rate-limit"

	// AnnotationTags specifies template tags to include
	AnnotationTags = AnnotationPrefix + "tags"

	// AnnotationExcludeTags specifies template tags to exclude
	AnnotationExcludeTags = AnnotationPrefix + "exclude-tags"
)

// ScanConfig holds parsed annotation configuration
type ScanConfig struct {
	// Enabled indicates if scanning is enabled
	Enabled bool

	// Templates to use for scanning
	Templates []string

	// Severity filter
	Severity []string

	// Schedule for periodic scans (cron format)
	Schedule string

	// Timeout for the scan
	Timeout *metav1.Duration

	// ScannerImage overrides the default scanner image
	ScannerImage string

	// ExcludeTemplates to exclude from scanning
	ExcludeTemplates []string

	// RateLimit for requests per second
	RateLimit int

	// Tags to include
	Tags []string

	// ExcludeTags to exclude
	ExcludeTags []string
}

// ParseAnnotations extracts scan configuration from resource annotations
func ParseAnnotations(annotations map[string]string) *ScanConfig {
	config := &ScanConfig{
		Enabled: true, // Default to enabled
	}

	if annotations == nil {
		return config
	}

	// Parse enabled
	if v, ok := annotations[AnnotationEnabled]; ok {
		config.Enabled = strings.ToLower(v) == "true"
	}

	// Parse templates
	if v, ok := annotations[AnnotationTemplates]; ok && v != "" {
		config.Templates = splitAndTrim(v)
	}

	// Parse severity
	if v, ok := annotations[AnnotationSeverity]; ok && v != "" {
		config.Severity = splitAndTrim(v)
	}

	// Parse schedule
	if v, ok := annotations[AnnotationSchedule]; ok {
		config.Schedule = v
	}

	// Parse timeout
	if v, ok := annotations[AnnotationTimeout]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			config.Timeout = &metav1.Duration{Duration: d}
		}
	}

	// Parse scanner image
	if v, ok := annotations[AnnotationScannerImage]; ok {
		config.ScannerImage = v
	}

	// Parse exclude templates
	if v, ok := annotations[AnnotationExcludeTemplates]; ok && v != "" {
		config.ExcludeTemplates = splitAndTrim(v)
	}

	// Parse rate limit
	if v, ok := annotations[AnnotationRateLimit]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			config.RateLimit = n
		}
	}

	// Parse tags
	if v, ok := annotations[AnnotationTags]; ok && v != "" {
		config.Tags = splitAndTrim(v)
	}

	// Parse exclude tags
	if v, ok := annotations[AnnotationExcludeTags]; ok && v != "" {
		config.ExcludeTags = splitAndTrim(v)
	}

	return config
}

// ApplyToNucleiScanSpec applies the annotation config to a NucleiScan spec
func (c *ScanConfig) ApplyToNucleiScanSpec(spec *nucleiv1alpha1.NucleiScanSpec) {
	// Apply templates if specified
	if len(c.Templates) > 0 {
		spec.Templates = c.Templates
	}

	// Apply severity if specified
	if len(c.Severity) > 0 {
		spec.Severity = c.Severity
	}

	// Apply schedule if specified
	if c.Schedule != "" {
		spec.Schedule = c.Schedule
	}

	// Apply scanner config if any scanner-specific settings are specified
	if c.ScannerImage != "" || c.Timeout != nil {
		if spec.ScannerConfig == nil {
			spec.ScannerConfig = &nucleiv1alpha1.ScannerConfig{}
		}
		if c.ScannerImage != "" {
			spec.ScannerConfig.Image = c.ScannerImage
		}
		if c.Timeout != nil {
			spec.ScannerConfig.Timeout = c.Timeout
		}
	}
}

// IsEnabled returns true if scanning is enabled
func (c *ScanConfig) IsEnabled() bool {
	return c.Enabled
}

// splitAndTrim splits a string by comma and trims whitespace from each element
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
