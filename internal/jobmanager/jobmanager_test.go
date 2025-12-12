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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
)

func TestBuildJob(t *testing.T) {
	config := DefaultConfig()
	manager := &JobManager{
		Config: config,
	}

	scan := &nucleiv1alpha1.NucleiScan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-scan",
			Namespace: "default",
		},
		Spec: nucleiv1alpha1.NucleiScanSpec{
			Targets: []string{"https://example.com"},
		},
	}

	job := manager.buildJob(scan)

	// Verify job name prefix
	if len(job.Name) == 0 {
		t.Error("Job name should not be empty")
	}

	// Verify namespace
	if job.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", job.Namespace)
	}

	// Verify labels
	if job.Labels[LabelManagedBy] != "nuclei-operator" {
		t.Error("Job should have managed-by label")
	}

	if job.Labels[LabelComponent] != "scanner" {
		t.Error("Job should have component label")
	}

	// Verify container
	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Error("Job should have exactly one container")
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != config.ScannerImage {
		t.Errorf("Expected image '%s', got '%s'", config.ScannerImage, container.Image)
	}

	// Verify security context
	if job.Spec.Template.Spec.SecurityContext.RunAsNonRoot == nil || !*job.Spec.Template.Spec.SecurityContext.RunAsNonRoot {
		t.Error("Pod should run as non-root")
	}
}

func TestBuildJobWithCustomConfig(t *testing.T) {
	config := DefaultConfig()
	manager := &JobManager{
		Config: config,
	}

	customImage := "custom/scanner:v1"
	customTimeout := metav1.Duration{Duration: 45 * time.Minute}

	scan := &nucleiv1alpha1.NucleiScan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-scan",
			Namespace: "default",
		},
		Spec: nucleiv1alpha1.NucleiScanSpec{
			Targets: []string{"https://example.com"},
			ScannerConfig: &nucleiv1alpha1.ScannerConfig{
				Image:   customImage,
				Timeout: &customTimeout,
			},
		},
	}

	job := manager.buildJob(scan)

	// Verify custom image
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != customImage {
		t.Errorf("Expected custom image '%s', got '%s'", customImage, container.Image)
	}

	// Verify custom timeout
	expectedDeadline := int64(45 * 60) // 45 minutes in seconds
	if *job.Spec.ActiveDeadlineSeconds != expectedDeadline {
		t.Errorf("Expected deadline %d, got %d", expectedDeadline, *job.Spec.ActiveDeadlineSeconds)
	}
}
