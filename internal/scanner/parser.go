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
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"time"

	nucleiv1alpha1 "github.com/mortenolsen/nuclei-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NucleiOutput represents the structure of Nuclei's JSONL output
type NucleiOutput struct {
	TemplateID   string     `json:"template-id"`
	TemplatePath string     `json:"template-path"`
	Info         NucleiInfo `json:"info"`
	Type         string     `json:"type"`
	Host         string     `json:"host"`
	MatchedAt    string     `json:"matched-at"`
	Timestamp    string     `json:"timestamp"`
	// ExtractedResults can be a string array or other types
	ExtractedResults interface{} `json:"extracted-results,omitempty"`
	// MatcherName is the name of the matcher that triggered
	MatcherName string `json:"matcher-name,omitempty"`
	// IP is the resolved IP address
	IP string `json:"ip,omitempty"`
	// CurlCommand is the curl command to reproduce the request
	CurlCommand string `json:"curl-command,omitempty"`
}

// NucleiInfo contains template metadata
type NucleiInfo struct {
	Name        string      `json:"name"`
	Author      interface{} `json:"author"` // Can be string or []string
	Tags        interface{} `json:"tags"`   // Can be string or []string
	Description string      `json:"description,omitempty"`
	Severity    string      `json:"severity"`
	Reference   interface{} `json:"reference,omitempty"` // Can be string or []string
	Metadata    interface{} `json:"metadata,omitempty"`
}

// ParseJSONLOutput parses Nuclei's JSONL output and returns a slice of Findings
func ParseJSONLOutput(output []byte) ([]nucleiv1alpha1.Finding, error) {
	var findings []nucleiv1alpha1.Finding

	scanner := bufio.NewScanner(bytes.NewReader(output))
	// Increase buffer size for potentially large JSON lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Skip non-JSON lines (nuclei sometimes outputs status messages)
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("{")) {
			continue
		}

		finding, err := parseJSONLine(line)
		if err != nil {
			// Log warning but continue parsing other lines
			// In production, you might want to use a proper logger
			continue
		}

		findings = append(findings, finding)
	}

	if err := scanner.Err(); err != nil {
		return findings, err
	}

	return findings, nil
}

// parseJSONLine parses a single JSONL line into a Finding
func parseJSONLine(line []byte) (nucleiv1alpha1.Finding, error) {
	var output NucleiOutput
	if err := json.Unmarshal(line, &output); err != nil {
		return nucleiv1alpha1.Finding{}, err
	}

	finding := nucleiv1alpha1.Finding{
		TemplateID:   output.TemplateID,
		TemplateName: output.Info.Name,
		Severity:     strings.ToLower(output.Info.Severity),
		Type:         output.Type,
		Host:         output.Host,
		MatchedAt:    output.MatchedAt,
		Description:  output.Info.Description,
		Timestamp:    parseTimestamp(output.Timestamp),
	}

	// Parse extracted results
	finding.ExtractedResults = parseStringSlice(output.ExtractedResults)

	// Parse references
	finding.Reference = parseStringSlice(output.Info.Reference)

	// Parse tags
	finding.Tags = parseStringSlice(output.Info.Tags)

	// Store additional metadata as RawExtension
	if output.Info.Metadata != nil {
		if metadataBytes, err := json.Marshal(output.Info.Metadata); err == nil {
			finding.Metadata = &runtime.RawExtension{Raw: metadataBytes}
		}
	}

	return finding, nil
}

// parseTimestamp parses a timestamp string into metav1.Time
func parseTimestamp(ts string) metav1.Time {
	if ts == "" {
		return metav1.Now()
	}

	// Try various timestamp formats that Nuclei might use
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return metav1.NewTime(t)
		}
	}

	// If parsing fails, return current time
	return metav1.Now()
}

// parseStringSlice converts various types to a string slice
// Nuclei output can have fields as either a single string or an array of strings
func parseStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		// Check if it's a comma-separated list
		if strings.Contains(val, ",") {
			parts := strings.Split(val, ",")
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result
		}
		return []string{val}
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return val
	default:
		return nil
	}
}
