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

package scanner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
)

// Scanner defines the interface for executing Nuclei scans
type Scanner interface {
	// Scan executes a Nuclei scan against the given targets and returns the results
	Scan(ctx context.Context, targets []string, options ScanOptions) (*ScanResult, error)
}

// ScanOptions contains configuration options for a scan
type ScanOptions struct {
	// Templates specifies which Nuclei templates to use (paths or tags)
	Templates []string
	// Severity filters results by minimum severity level
	Severity []string
	// Timeout is the maximum duration for the scan
	Timeout time.Duration
}

// ScanResult contains the results of a completed scan
type ScanResult struct {
	// Findings contains all vulnerabilities/issues discovered
	Findings []nucleiv1alpha1.Finding
	// Summary provides aggregated statistics
	Summary nucleiv1alpha1.ScanSummary
	// Duration is how long the scan took
	Duration time.Duration
}

// NucleiScanner implements the Scanner interface using the Nuclei binary
type NucleiScanner struct {
	nucleiBinaryPath string
	templatesPath    string
}

// Config holds configuration for the NucleiScanner
type Config struct {
	// NucleiBinaryPath is the path to the nuclei binary (default: "nuclei")
	NucleiBinaryPath string
	// TemplatesPath is the path to nuclei templates (default: use nuclei's default)
	TemplatesPath string
	// DefaultTimeout is the default scan timeout (default: 30m)
	DefaultTimeout time.Duration
}

// DefaultConfig returns a Config with default values
func DefaultConfig() Config {
	return Config{
		NucleiBinaryPath: getEnvOrDefault("NUCLEI_BINARY_PATH", "nuclei"),
		TemplatesPath:    getEnvOrDefault("NUCLEI_TEMPLATES_PATH", ""),
		DefaultTimeout:   getEnvDurationOrDefault("NUCLEI_TIMEOUT", 30*time.Minute),
	}
}

// NewNucleiScanner creates a new NucleiScanner with the given configuration
func NewNucleiScanner(config Config) *NucleiScanner {
	return &NucleiScanner{
		nucleiBinaryPath: config.NucleiBinaryPath,
		templatesPath:    config.TemplatesPath,
	}
}

// NewNucleiScannerWithDefaults creates a new NucleiScanner with default configuration
func NewNucleiScannerWithDefaults() *NucleiScanner {
	return NewNucleiScanner(DefaultConfig())
}

// Scan executes a Nuclei scan against the given targets
func (s *NucleiScanner) Scan(ctx context.Context, targets []string, options ScanOptions) (*ScanResult, error) {
	logger := log.FromContext(ctx).WithName("nuclei-scanner")

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets provided for scan")
	}

	startTime := time.Now()

	// Create a temporary directory for this scan
	tmpDir, err := os.MkdirTemp("", "nuclei-scan-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write targets to a file
	targetsFile := filepath.Join(tmpDir, "targets.txt")
	targetsContent := strings.Join(targets, "\n")
	if err := os.WriteFile(targetsFile, []byte(targetsContent), 0600); err != nil {
		return nil, fmt.Errorf("failed to write targets file: %w", err)
	}

	logger.Info("Targets file created", "targetsFile", targetsFile, "targetCount", len(targets))

	// Check if nuclei binary exists and is executable
	if _, err := os.Stat(s.nucleiBinaryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("nuclei binary not found at %s", s.nucleiBinaryPath)
	}

	// Verify nuclei is executable
	if err := exec.Command(s.nucleiBinaryPath, "-version").Run(); err != nil {
		logger.Error(err, "Failed to execute nuclei -version, nuclei may not be properly installed")
	}

	// Check templates availability if templates path is set
	templatesAvailable := false
	if s.templatesPath != "" {
		if info, err := os.Stat(s.templatesPath); err != nil || !info.IsDir() {
			logger.Info("Templates path does not exist or is not a directory, nuclei will use default templates",
				"templatesPath", s.templatesPath,
				"error", err)
		} else {
			// Count template files
			entries, err := os.ReadDir(s.templatesPath)
			if err == nil {
				templateCount := 0
				for _, entry := range entries {
					if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
						templateCount++
					}
				}
				templatesAvailable = templateCount > 0
				logger.Info("Templates directory found", "templatesPath", s.templatesPath, "templateCount", templateCount)
				if templateCount == 0 {
					logger.Info("Templates directory is empty, nuclei will download templates on first run or use default location")
				}
			}
		}
	} else {
		logger.Info("No templates path configured, nuclei will use default template location (~/.nuclei/templates)")
	}

	// If no specific templates are provided and templates path is empty, warn
	if len(options.Templates) == 0 && !templatesAvailable && s.templatesPath != "" {
		logger.Info("Warning: No templates specified and templates directory appears empty. Nuclei may not run any scans.")
	}

	// Build the nuclei command arguments
	args := s.buildArgs(targetsFile, options)

	// Set timeout from options or use default
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	// Log the command being executed
	fullCommand := fmt.Sprintf("%s %s", s.nucleiBinaryPath, strings.Join(args, " "))
	logger.Info("Executing nuclei scan",
		"command", fullCommand,
		"timeout", timeout,
		"templates", len(options.Templates),
		"templatesList", options.Templates,
		"severity", options.Severity,
		"templatesPath", s.templatesPath)

	// Create context with timeout
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute nuclei
	cmd := exec.CommandContext(scanCtx, s.nucleiBinaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Info("Starting nuclei execution")
	err = cmd.Run()
	duration := time.Since(startTime)

	// Log stderr output (nuclei often outputs warnings/info to stderr)
	stderrStr := stderr.String()
	if stderrStr != "" {
		logger.Info("Nuclei stderr output", "stderr", stderrStr)
	}

	// Log stdout size for debugging
	stdoutSize := len(stdout.Bytes())
	logger.Info("Nuclei execution completed",
		"duration", duration,
		"exitCode", cmd.ProcessState.ExitCode(),
		"stdoutSize", stdoutSize,
		"stderrSize", len(stderrStr))

	// Check for context cancellation
	if scanCtx.Err() == context.DeadlineExceeded {
		logger.Error(nil, "Scan timed out", "timeout", timeout, "stderr", stderrStr)
		return nil, fmt.Errorf("scan timed out after %v", timeout)
	}
	if scanCtx.Err() == context.Canceled {
		logger.Error(nil, "Scan was cancelled", "stderr", stderrStr)
		return nil, fmt.Errorf("scan was cancelled")
	}

	// Nuclei returns exit code 0 even when it finds vulnerabilities
	// Non-zero exit codes indicate actual errors
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 can mean "no results found" which is not an error
			if exitErr.ExitCode() != 1 {
				logger.Error(err, "Nuclei execution failed",
					"exitCode", exitErr.ExitCode(),
					"stderr", stderrStr,
					"stdout", stdout.String())
				return nil, fmt.Errorf("nuclei execution failed: %w, stderr: %s", err, stderrStr)
			}
			logger.Info("Nuclei exited with code 1 (no results found)", "stderr", stderrStr)
		} else {
			logger.Error(err, "Failed to execute nuclei", "stderr", stderrStr)
			return nil, fmt.Errorf("failed to execute nuclei: %w", err)
		}
	}

	// Parse the JSONL output
	stdoutBytes := stdout.Bytes()
	logger.Info("Parsing nuclei output", "outputSize", len(stdoutBytes))
	findings, err := ParseJSONLOutput(stdoutBytes)
	if err != nil {
		logger.Error(err, "Failed to parse nuclei output",
			"stdout", string(stdoutBytes),
			"stderr", stderrStr)
		return nil, fmt.Errorf("failed to parse nuclei output: %w", err)
	}

	logger.Info("Parsed findings", "count", len(findings))

	// Calculate summary
	summary := calculateSummary(findings, len(targets), duration)

	logger.Info("Scan completed",
		"findings", len(findings),
		"duration", duration,
		"targetsScanned", len(targets))

	return &ScanResult{
		Findings: findings,
		Summary:  summary,
		Duration: duration,
	}, nil
}

// buildArgs constructs the command line arguments for nuclei
func (s *NucleiScanner) buildArgs(targetsFile string, options ScanOptions) []string {
	args := []string{
		"-l", targetsFile,
		"-jsonl",
		"-silent",
		"-no-color",
		"-rate-limit", "150", // Limit rate to avoid overwhelming targets
		"-bulk-size", "25", // Process targets in bulk
	}

	// Add specific templates if provided
	if len(options.Templates) > 0 {
		for _, t := range options.Templates {
			args = append(args, "-t", t)
		}
	} else {
		// When no templates are specified, nuclei should use all available templates
		// Only add templates path if it's configured AND contains templates
		// Otherwise, let nuclei use its default template location (~/.nuclei/templates)
		if s.templatesPath != "" {
			// Check if templates directory exists and has content
			if info, err := os.Stat(s.templatesPath); err == nil && info.IsDir() {
				entries, err := os.ReadDir(s.templatesPath)
				if err == nil {
					hasTemplates := false
					for _, entry := range entries {
						if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
							hasTemplates = true
							break
						}
					}
					if hasTemplates {
						args = append(args, "-t", s.templatesPath)
					}
					// If no templates found, don't add -t flag, let nuclei use default location
				}
			}
		}
		// If no templates path or it's empty, nuclei will use default location
		// which it will download templates to on first run if needed
	}

	// Add severity filter if provided
	if len(options.Severity) > 0 {
		args = append(args, "-severity", strings.Join(options.Severity, ","))
	}

	return args
}

// calculateSummary generates a ScanSummary from the findings
func calculateSummary(findings []nucleiv1alpha1.Finding, targetsCount int, duration time.Duration) nucleiv1alpha1.ScanSummary {
	severityCounts := make(map[string]int)

	for _, f := range findings {
		severity := strings.ToLower(f.Severity)
		severityCounts[severity]++
	}

	return nucleiv1alpha1.ScanSummary{
		TotalFindings:      len(findings),
		FindingsBySeverity: severityCounts,
		TargetsScanned:     targetsCount,
		DurationSeconds:    int64(duration.Seconds()),
	}
}

// getEnvOrDefault returns the environment variable value or a default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvDurationOrDefault returns the environment variable as a duration or a default
func getEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
