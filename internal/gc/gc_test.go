package gc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	kindClusterPolicy = "ClusterPolicy"
	kindPolicy        = "Policy"
)

func TestGetPoliciesByKind(t *testing.T) {
	tests := []struct {
		name           string
		kind           string
		mockOutput     string
		mockError      error
		expectedLen    int
		expectedError  bool
		expectedPolicy *PolicyInfo
	}{
		{
			name: "successful clusterpolicy fetch",
			kind: kindClusterPolicy,
			mockOutput: `{
				"items": [
					{
						"metadata": {
							"name": "test-policy",
							"labels": {
								"managed-by": "kyverno-watcher",
								"policy-version": "v1.0.0"
							}
						},
						"kind": "ClusterPolicy"
					}
				]
			}`,
			mockError:   nil,
			expectedLen: 1,
			expectedPolicy: &PolicyInfo{
				Name: "test-policy",
				Kind: kindClusterPolicy,
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
		},
		{
			name: "successful namespaced policy fetch",
			kind: kindPolicy,
			mockOutput: `{
				"items": [
					{
						"metadata": {
							"name": "ns-policy",
							"namespace": "default",
							"labels": {
								"managed-by": "kyverno-watcher",
								"policy-version": "v2.0.0"
							}
						},
						"kind": "Policy"
					}
				]
			}`,
			mockError:   nil,
			expectedLen: 1,
			expectedPolicy: &PolicyInfo{
				Name:      "ns-policy",
				Namespace: "default",
				Kind:      kindPolicy,
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v2.0.0",
				},
			},
		},
		{
			name:          "kubectl command fails",
			kind:          kindClusterPolicy,
			mockOutput:    "",
			mockError:     fmt.Errorf("command failed"),
			expectedLen:   0,
			expectedError: true,
		},
		{
			name:          "empty list",
			kind:          kindClusterPolicy,
			mockOutput:    `{"items": []}`,
			mockError:     nil,
			expectedLen:   0,
			expectedError: false,
		},
		{
			name:          "invalid json",
			kind:          kindClusterPolicy,
			mockOutput:    `{invalid json}`,
			mockError:     nil,
			expectedLen:   0,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock scriptExec
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			scriptExecFunc = func(cmd string) (string, error) {
				// Verify correct command is used
				if tt.kind == kindClusterPolicy {
					if !strings.Contains(cmd, "clusterpolicies") {
						t.Errorf("Expected command to contain 'clusterpolicies', got: %s", cmd)
					}
				} else {
					if !strings.Contains(cmd, "policies") || !strings.Contains(cmd, "--all-namespaces") {
						t.Errorf("Expected command to contain 'policies --all-namespaces', got: %s", cmd)
					}
				}
				return tt.mockOutput, tt.mockError
			}

			policies, err := getPoliciesByKind(tt.kind)

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if len(policies) != tt.expectedLen {
				t.Errorf("Expected %d policies, got %d", tt.expectedLen, len(policies))
			}

			if tt.expectedPolicy != nil && len(policies) > 0 {
				p := policies[0]
				if p.Name != tt.expectedPolicy.Name {
					t.Errorf("Expected policy name %s, got %s", tt.expectedPolicy.Name, p.Name)
				}
				if p.Namespace != tt.expectedPolicy.Namespace {
					t.Errorf("Expected namespace %s, got %s", tt.expectedPolicy.Namespace, p.Namespace)
				}
				if p.Kind != tt.expectedPolicy.Kind {
					t.Errorf("Expected kind %s, got %s", tt.expectedPolicy.Kind, p.Kind)
				}
			}
		})
	}
}

func TestGetManagedPolicies(t *testing.T) {
	tests := []struct {
		name                string
		mockPolicies        string
		mockClusterPolicies string
		expectedLen         int
	}{
		{
			name: "both types present",
			mockPolicies: `{
				"items": [
					{
						"metadata": {"name": "policy1", "namespace": "default", "labels": {"managed-by": "kyverno-watcher"}},
						"kind": "Policy"
					}
				]
			}`,
			mockClusterPolicies: `{
				"items": [
					{
						"metadata": {"name": "cpolicy1", "labels": {"managed-by": "kyverno-watcher"}},
						"kind": "ClusterPolicy"
					}
				]
			}`,
			expectedLen: 2,
		},
		{
			name:         "only clusterpolicies",
			mockPolicies: `{"items": []}`,
			mockClusterPolicies: `{
				"items": [
					{
						"metadata": {"name": "cpolicy1", "labels": {"managed-by": "kyverno-watcher"}},
						"kind": "ClusterPolicy"
					}
				]
			}`,
			expectedLen: 1,
		},
		{
			name: "only namespaced policies",
			mockPolicies: `{
				"items": [
					{
						"metadata": {"name": "policy1", "namespace": "default", "labels": {"managed-by": "kyverno-watcher"}},
						"kind": "Policy"
					}
				]
			}`,
			mockClusterPolicies: `{"items": []}`,
			expectedLen:         1,
		},
		{
			name:                "no policies",
			mockPolicies:        `{"items": []}`,
			mockClusterPolicies: `{"items": []}`,
			expectedLen:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			scriptExecFunc = func(cmd string) (string, error) {
				if strings.Contains(cmd, "clusterpolicies") {
					return tt.mockClusterPolicies, nil
				}
				return tt.mockPolicies, nil
			}

			policies := getManagedPolicies()

			if len(policies) != tt.expectedLen {
				t.Errorf("Expected %d policies, got %d", tt.expectedLen, len(policies))
			}
		})
	}
}

func TestCheckForActiveWatchers(t *testing.T) {
	tests := []struct {
		name         string
		mockOutput   string
		mockError    error
		expectedBool bool
		expectError  bool
	}{
		{
			name: "active watcher running",
			mockOutput: `{
				"items": [
					{
						"metadata": {"name": "kyverno-artifact-manager-test"},
						"status": {"phase": "Running"}
					}
				]
			}`,
			expectedBool: true,
		},
		{
			name: "active watcher pending",
			mockOutput: `{
				"items": [
					{
						"metadata": {"name": "kyverno-artifact-manager-test"},
						"status": {"phase": "Pending"}
					}
				]
			}`,
			expectedBool: true,
		},
		{
			name: "watcher failed",
			mockOutput: `{
				"items": [
					{
						"metadata": {"name": "kyverno-artifact-manager-test"},
						"status": {"phase": "Failed"}
					}
				]
			}`,
			expectedBool: false,
		},
		{
			name: "no watchers",
			mockOutput: `{
				"items": []
			}`,
			expectedBool: false,
		},
		{
			name: "other pods only",
			mockOutput: `{
				"items": [
					{
						"metadata": {"name": "some-other-pod"},
						"status": {"phase": "Running"}
					}
				]
			}`,
			expectedBool: false,
		},
		{
			name:         "kubectl error",
			mockError:    fmt.Errorf("command failed"),
			expectedBool: false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			scriptExecFunc = func(cmd string) (string, error) {
				return tt.mockOutput, tt.mockError
			}

			result, err := checkForActiveWatchers()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if result != tt.expectedBool {
				t.Errorf("Expected %v, got %v", tt.expectedBool, result)
			}
		})
	}
}

func TestCheckForKyvernoArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		mockOutput   string
		mockError    error
		expectedBool bool
		expectError  bool
	}{
		{
			name: "artifacts exist",
			mockOutput: `{
				"items": [
					{"metadata": {"name": "artifact1"}},
					{"metadata": {"name": "artifact2"}}
				]
			}`,
			expectedBool: true,
		},
		{
			name: "no artifacts",
			mockOutput: `{
				"items": []
			}`,
			expectedBool: false,
		},
		{
			name:         "kubectl error",
			mockError:    fmt.Errorf("command failed"),
			expectedBool: false,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			scriptExecFunc = func(cmd string) (string, error) {
				return tt.mockOutput, tt.mockError
			}

			result, err := checkForKyvernoArtifacts()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if result != tt.expectedBool {
				t.Errorf("Expected %v, got %v", tt.expectedBool, result)
			}
		})
	}
}

func TestIsOrphaned(t *testing.T) {
	tests := []struct {
		name             string
		policy           PolicyInfo
		hasWatchers      bool
		hasArtifacts     bool
		watchersError    error
		artifactsError   error
		expectedOrphaned bool
		skipVersionCheck bool
	}{
		{
			name: "policy without version label",
			policy: PolicyInfo{
				Name:   "test-policy",
				Labels: map[string]string{"managed-by": "kyverno-watcher"},
			},
			skipVersionCheck: true,
			expectedOrphaned: false,
		},
		{
			name: "orphaned - no watchers",
			policy: PolicyInfo{
				Name: "test-policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			hasWatchers:      false,
			hasArtifacts:     true,
			expectedOrphaned: true,
		},
		{
			name: "orphaned - no artifacts",
			policy: PolicyInfo{
				Name: "test-policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			hasWatchers:      true,
			hasArtifacts:     false,
			expectedOrphaned: true,
		},
		{
			name: "not orphaned - both exist",
			policy: PolicyInfo{
				Name: "test-policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			hasWatchers:      true,
			hasArtifacts:     true,
			expectedOrphaned: false,
		},
		{
			name: "error checking watchers",
			policy: PolicyInfo{
				Name: "test-policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			watchersError:    fmt.Errorf("check failed"),
			expectedOrphaned: false,
		},
		{
			name: "error checking artifacts",
			policy: PolicyInfo{
				Name: "test-policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			hasWatchers:      true,
			artifactsError:   fmt.Errorf("check failed"),
			expectedOrphaned: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			scriptExecFunc = func(cmd string) (string, error) {
				if strings.Contains(cmd, "kyvernoartifacts") {
					if tt.artifactsError != nil {
						return "", tt.artifactsError
					}
					items := "[]"
					if tt.hasArtifacts {
						items = `[{"metadata": {"name": "artifact1"}}]`
					}
					return fmt.Sprintf(`{"items": %s}`, items), nil
				}
				if strings.Contains(cmd, "pods") {
					if tt.watchersError != nil {
						return "", tt.watchersError
					}
					items := "[]"
					if tt.hasWatchers {
						items = `[{"metadata": {"name": "kyverno-artifact-manager-test"}, "status": {"phase": "Running"}}]`
					}
					return fmt.Sprintf(`{"items": %s}`, items), nil
				}
				return "", fmt.Errorf("unexpected command: %s", cmd)
			}

			result := isOrphaned(tt.policy)

			if result != tt.expectedOrphaned {
				t.Errorf("Expected orphaned=%v, got %v", tt.expectedOrphaned, result)
			}
		})
	}
}

func TestDeletePolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      PolicyInfo
		mockError   error
		expectError bool
		expectedCmd string
	}{
		{
			name: "delete clusterpolicy",
			policy: PolicyInfo{
				Name: "test-cpolicy",
				Kind: "ClusterPolicy",
			},
			expectedCmd: "kubectl delete clusterpolicy test-cpolicy",
		},
		{
			name: "delete namespaced policy",
			policy: PolicyInfo{
				Name:      "test-policy",
				Namespace: "default",
				Kind:      "Policy",
			},
			expectedCmd: "kubectl delete policy test-policy -n default",
		},
		{
			name: "delete policy without namespace",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
			},
			expectedCmd: "kubectl delete policy test-policy",
		},
		{
			name: "delete fails",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
			},
			mockError:   fmt.Errorf("delete failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			var capturedCmd string
			scriptExecFunc = func(cmd string) (string, error) {
				capturedCmd = cmd
				return "", tt.mockError
			}

			err := deletePolicy(tt.policy)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if tt.expectedCmd != "" && capturedCmd != tt.expectedCmd {
				t.Errorf("Expected command %q, got %q", tt.expectedCmd, capturedCmd)
			}
		})
	}
}

func TestCollectGarbage(t *testing.T) {
	tests := []struct {
		name                 string
		mockPolicies         []PolicyInfo
		hasWatchers          bool
		hasArtifacts         bool
		expectedDeletedCount int
	}{
		{
			name: "delete orphaned policies",
			mockPolicies: []PolicyInfo{
				{
					Name: "orphaned-policy",
					Kind: "ClusterPolicy",
					Labels: map[string]string{
						"managed-by":     "kyverno-watcher",
						"policy-version": "v1.0.0",
					},
				},
			},
			hasWatchers:          false,
			hasArtifacts:         false,
			expectedDeletedCount: 1,
		},
		{
			name: "keep active policies",
			mockPolicies: []PolicyInfo{
				{
					Name: "active-policy",
					Kind: "ClusterPolicy",
					Labels: map[string]string{
						"managed-by":     "kyverno-watcher",
						"policy-version": "v1.0.0",
					},
				},
			},
			hasWatchers:          true,
			hasArtifacts:         true,
			expectedDeletedCount: 0,
		},
		{
			name: "mixed scenario",
			mockPolicies: []PolicyInfo{
				{
					Name: "orphaned-policy",
					Kind: "ClusterPolicy",
					Labels: map[string]string{
						"managed-by":     "kyverno-watcher",
						"policy-version": "v1.0.0",
					},
				},
				{
					Name: "policy-without-version",
					Kind: "ClusterPolicy",
					Labels: map[string]string{
						"managed-by": "kyverno-watcher",
					},
				},
			},
			hasWatchers:          false,
			hasArtifacts:         false,
			expectedDeletedCount: 1, // Only orphaned-policy should be deleted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldScriptExec := scriptExecFunc
			defer func() { scriptExecFunc = oldScriptExec }()

			deletedCount := 0
			scriptExecFunc = func(cmd string) (string, error) {
				if strings.HasPrefix(cmd, "kubectl delete") {
					deletedCount++
					return "", nil
				}
				if strings.Contains(cmd, "kyvernoartifacts") {
					items := "[]"
					if tt.hasArtifacts {
						items = `[{"metadata": {"name": "artifact1"}}]`
					}
					return fmt.Sprintf(`{"items": %s}`, items), nil
				}
				if strings.Contains(cmd, "pods") {
					items := "[]"
					if tt.hasWatchers {
						items = `[{"metadata": {"name": "kyverno-artifact-manager-test"}, "status": {"phase": "Running"}}]`
					}
					return fmt.Sprintf(`{"items": %s}`, items), nil
				}
				if strings.Contains(cmd, "policies") || strings.Contains(cmd, "clusterpolicies") {
					// Return mock policies based on the kind in the command
					var filteredPolicies []PolicyInfo
					if strings.Contains(cmd, "clusterpolicies") {
						for _, p := range tt.mockPolicies {
							if p.Kind == kindClusterPolicy {
								filteredPolicies = append(filteredPolicies, p)
							}
						}
					} else {
						for _, p := range tt.mockPolicies {
							if p.Kind == "Policy" {
								filteredPolicies = append(filteredPolicies, p)
							}
						}
					}

					items := []map[string]interface{}{}
					for _, p := range filteredPolicies {
						items = append(items, map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":      p.Name,
								"namespace": p.Namespace,
								"labels":    p.Labels,
							},
							"kind": p.Kind,
						})
					}
					listJSON, _ := json.Marshal(map[string]interface{}{"items": items})
					return string(listJSON), nil
				}
				return "", fmt.Errorf("unexpected command: %s", cmd)
			}

			collectGarbage()

			if deletedCount != tt.expectedDeletedCount {
				t.Errorf("Expected %d policies deleted, got %d", tt.expectedDeletedCount, deletedCount)
			}
		})
	}
}
