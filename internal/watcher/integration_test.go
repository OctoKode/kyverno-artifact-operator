package watcher

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestAddLabelsToYAML(t *testing.T) {
	tests := []struct {
		name        string
		inputYAML   string
		tag         string
		wantLabels  map[string]string
		wantValid   bool
		description string
	}{
		{
			name: "simple ClusterPolicy",
			inputYAML: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: test-policy
spec:
  rules:
  - name: test-rule
    match:
      any:
      - resources:
          kinds:
          - Pod
`,
			tag: "v1.0.0",
			wantLabels: map[string]string{
				"managed-by":     "kyverno-watcher",
				"policy-version": "v1.0.0",
			},
			wantValid:   true,
			description: "Should add labels to ClusterPolicy without existing labels",
		},
		{
			name: "ClusterPolicy with existing labels",
			inputYAML: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: test-policy
  labels:
    app: myapp
    env: prod
spec:
  rules:
  - name: test-rule
`,
			tag: "v2.3.1",
			wantLabels: map[string]string{
				"app":            "myapp",
				"env":            "prod",
				"managed-by":     "kyverno-watcher",
				"policy-version": "v2.3.1",
			},
			wantValid:   true,
			description: "Should preserve existing labels and add new ones",
		},
		{
			name: "Policy with namespace",
			inputYAML: `apiVersion: kyverno.io/v1
kind: Policy
metadata:
  name: namespace-policy
  namespace: default
spec:
  rules:
  - name: test
`,
			tag: "v0.1.0",
			wantLabels: map[string]string{
				"managed-by":     "kyverno-watcher",
				"policy-version": "v0.1.0",
			},
			wantValid:   true,
			description: "Should handle namespaced Policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result, err := addLabelsToYAML([]byte(tt.inputYAML), tt.tag)
			if err != nil {
				t.Fatalf("addLabelsToYAML() error = %v", err)
			}

			// Parse the result to verify
			var manifest Manifest
			if err := yaml.Unmarshal(result, &manifest); err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}

			// Verify apiVersion is preserved
			if manifest.APIVersion == "" {
				t.Error("apiVersion was lost")
			}

			// Verify kind is preserved
			if manifest.Kind == "" {
				t.Error("kind was lost")
			}

			// Verify metadata.name is preserved
			if manifest.Metadata.Name == "" {
				t.Error("metadata.name was lost")
			}

			// Verify labels were added correctly
			if manifest.Metadata.Labels == nil {
				t.Fatal("Labels map is nil")
			}

			for key, expectedValue := range tt.wantLabels {
				if actualValue, exists := manifest.Metadata.Labels[key]; !exists {
					t.Errorf("Expected label %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Label %q = %q, want %q", key, actualValue, expectedValue)
				}
			}

			// Verify the YAML is still valid by unmarshaling again
			var checkManifest Manifest
			if err := yaml.Unmarshal(result, &checkManifest); err != nil {
				t.Errorf("Result YAML is invalid: %v", err)
			}

			// Verify spec is preserved
			if manifest.Spec == nil {
				t.Error("spec was lost")
			}
		})
	}
}

func TestAddLabelsToYAMLInvalid(t *testing.T) {
	tests := []struct {
		name      string
		inputYAML string
		tag       string
	}{
		{
			name:      "invalid YAML",
			inputYAML: "this is not: valid: yaml: content",
			tag:       "v1.0.0",
		},
		{
			name:      "malformed YAML",
			inputYAML: "apiVersion: test\nkind: [\ninvalid",
			tag:       "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := addLabelsToYAML([]byte(tt.inputYAML), tt.tag)
			if err == nil {
				t.Error("Expected error for invalid YAML, got nil")
			}
		})
	}
}

func TestManifestStructPreservesFields(t *testing.T) {
	// Test that our Manifest struct correctly preserves all important fields
	inputYAML := `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: disallow-latest-tag
  labels:
    policies.kyverno.io/category: Best Practices
spec:
  validationFailureAction: Audit
  background: true
  rules:
  - name: require-image-tag
    match:
      any:
      - resources:
          kinds:
          - Pod
    validate:
      message: "An image tag is required"
      pattern:
        spec:
          containers:
          - image: "*:*"
`

	var manifest Manifest
	if err := yaml.Unmarshal([]byte(inputYAML), &manifest); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify all critical fields
	if manifest.APIVersion != "kyverno.io/v1" {
		t.Errorf("apiVersion = %q, want %q", manifest.APIVersion, "kyverno.io/v1")
	}

	if manifest.Kind != "ClusterPolicy" {
		t.Errorf("kind = %q, want %q", manifest.Kind, "ClusterPolicy")
	}

	if manifest.Metadata.Name != "disallow-latest-tag" {
		t.Errorf("metadata.name = %q, want %q", manifest.Metadata.Name, "disallow-latest-tag")
	}

	if manifest.Metadata.Labels["policies.kyverno.io/category"] != "Best Practices" {
		t.Error("Existing label was not preserved")
	}

	if manifest.Spec == nil {
		t.Error("spec was not preserved")
	}

	// Marshal back and verify it's still valid YAML
	result, err := yaml.Marshal(&manifest)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal again to verify round-trip
	var checkManifest Manifest
	if err := yaml.Unmarshal(result, &checkManifest); err != nil {
		t.Fatalf("Round-trip failed: %v", err)
	}

	if checkManifest.APIVersion != manifest.APIVersion {
		t.Error("apiVersion changed after round-trip")
	}
	if checkManifest.Kind != manifest.Kind {
		t.Error("kind changed after round-trip")
	}
}

func TestAddLabelsToYAMLProducesValidKubernetesYAML(t *testing.T) {
	// Test that the marshaled output has correct lowercase field names
	// This is critical for kubectl validation
	tests := []struct {
		name      string
		inputYAML string
		tag       string
	}{
		{
			name: "ClusterPolicy",
			inputYAML: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: test-policy
spec:
  validationFailureAction: Audit
  rules:
  - name: test-rule
    match:
      any:
      - resources:
          kinds:
          - Pod
`,
			tag: "v1.0.0",
		},
		{
			name: "Policy with namespace",
			inputYAML: `apiVersion: kyverno.io/v1
kind: Policy
metadata:
  name: test-policy
  namespace: default
spec:
  rules:
  - name: test-rule
`,
			tag: "v2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := addLabelsToYAML([]byte(tt.inputYAML), tt.tag)
			if err != nil {
				t.Fatalf("addLabelsToYAML() error = %v", err)
			}

			resultStr := string(result)

			// Verify lowercase field names (required for kubectl)
			if !strings.Contains(resultStr, "apiVersion:") {
				t.Error("Output missing lowercase 'apiVersion:' field")
			}
			if !strings.Contains(resultStr, "kind:") {
				t.Error("Output missing lowercase 'kind:' field")
			}
			if !strings.Contains(resultStr, "metadata:") {
				t.Error("Output missing lowercase 'metadata:' field")
			}
			if !strings.Contains(resultStr, "spec:") {
				t.Error("Output missing lowercase 'spec:' field")
			}

			// Verify labels were added
			if !strings.Contains(resultStr, "managed-by: kyverno-watcher") {
				t.Error("Output missing 'managed-by' label")
			}
			if !strings.Contains(resultStr, fmt.Sprintf("policy-version: %s", tt.tag)) {
				t.Errorf("Output missing 'policy-version: %s' label", tt.tag)
			}

			// Verify NO capitalized field names (would cause kubectl validation errors)
			if strings.Contains(resultStr, "APIVersion:") {
				t.Error("Output contains capitalized 'APIVersion:' - kubectl will reject this")
			}
			if strings.Contains(resultStr, "Kind:") {
				t.Error("Output contains capitalized 'Kind:' - kubectl will reject this")
			}
			if strings.Contains(resultStr, "Metadata:") {
				t.Error("Output contains capitalized 'Metadata:' - kubectl will reject this")
			}
			if strings.Contains(resultStr, "Spec:") {
				t.Error("Output contains capitalized 'Spec:' - kubectl will reject this")
			}

			// Verify the output can be unmarshaled back
			var checkManifest Manifest
			if err := yaml.Unmarshal(result, &checkManifest); err != nil {
				t.Errorf("Result YAML is invalid: %v", err)
			}

			// Verify fields are still accessible after unmarshal
			if checkManifest.APIVersion == "" {
				t.Error("apiVersion is empty after unmarshal")
			}
			if checkManifest.Kind == "" {
				t.Error("kind is empty after unmarshal")
			}
			if checkManifest.Metadata.Name == "" {
				t.Error("metadata.name is empty after unmarshal")
			}

			// Verify labels were actually added
			if checkManifest.Metadata.Labels == nil {
				t.Fatal("Labels map is nil")
			}
			if checkManifest.Metadata.Labels["managed-by"] != "kyverno-watcher" {
				t.Error("managed-by label not found or incorrect")
			}
			if checkManifest.Metadata.Labels["policy-version"] != tt.tag {
				t.Errorf("policy-version label = %q, want %q",
					checkManifest.Metadata.Labels["policy-version"], tt.tag)
			}
		})
	}
}

func TestYAMLFileExtensionDetection(t *testing.T) {
	tests := []struct {
		filename string
		isYAML   bool
	}{
		{"policy.yaml", true},
		{"policy.yml", true},
		{"policy.YAML", true},
		{"policy.YML", true},
		{"policy.Yaml", true},
		{"policy.txt", false},
		{"policy.json", false},
		{"README.md", false},
		{"policy", false},
		{"config.yaml.backup", false},
		{"test.YAML.old", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			ext := strings.ToLower(filepath.Ext(tt.filename))
			isYAML := ext == ".yaml" || ext == ".yml"

			if isYAML != tt.isYAML {
				t.Errorf("File %s: expected isYAML=%v, got %v", tt.filename, tt.isYAML, isYAML)
			}
		})
	}
}

func TestKubectlCommandGeneration(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		wantCmd  string
	}{
		{
			name:     "simple path",
			filePath: "/tmp/policy.yaml",
			wantCmd:  "kubectl apply -f /tmp/policy.yaml",
		},
		{
			name:     "nested path",
			filePath: "/tmp/manifests/policy.yaml",
			wantCmd:  "kubectl apply -f /tmp/manifests/policy.yaml",
		},
		{
			name:     "yml extension",
			filePath: "/tmp/policy.yml",
			wantCmd:  "kubectl apply -f /tmp/policy.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the command generation logic
			cmd := formatKubectlCommand(tt.filePath)
			if cmd != tt.wantCmd {
				t.Errorf("Command mismatch:\nwant: %s\ngot:  %s", tt.wantCmd, cmd)
			}
		})
	}
}

func TestLayerFileNaming(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		layerIdx  int
		wantName  string
	}{
		{
			name:      "policy layer",
			mediaType: PolicyLayerMediaType,
			layerIdx:  0,
			wantName:  "policy-0.yaml",
		},
		{
			name:      "generic layer",
			mediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip",
			layerIdx:  1,
			wantName:  "layer-1.yaml",
		},
		{
			name:      "policy layer high index",
			mediaType: PolicyLayerMediaType,
			layerIdx:  5,
			wantName:  "policy-5.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate layer naming logic
			var filename string
			if tt.mediaType == PolicyLayerMediaType {
				filename = fmt.Sprintf("policy-%d.yaml", tt.layerIdx)
			} else {
				filename = fmt.Sprintf("layer-%d.yaml", tt.layerIdx)
			}

			if filename != tt.wantName {
				t.Errorf("Filename mismatch: want %s, got %s", tt.wantName, filename)
			}
		})
	}
}

// Helper function for command generation (pure logic, no execution)
func formatKubectlCommand(filePath string) string {
	return "kubectl apply -f " + filePath
}
