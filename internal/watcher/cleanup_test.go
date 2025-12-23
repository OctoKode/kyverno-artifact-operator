package watcher

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
)

func TestGetEnvAsBoolOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		setEnv       bool
		want         bool
	}{
		{
			name:         "true values",
			key:          "TEST_BOOL_1",
			defaultValue: false,
			envValue:     "true",
			setEnv:       true,
			want:         true,
		},
		{
			name:         "T value",
			key:          "TEST_BOOL_2",
			defaultValue: false,
			envValue:     "T",
			setEnv:       true,
			want:         true,
		},
		{
			name:         "1 value",
			key:          "TEST_BOOL_3",
			defaultValue: false,
			envValue:     "1",
			setEnv:       true,
			want:         true,
		},
		{
			name:         "false value",
			key:          "TEST_BOOL_4",
			defaultValue: true,
			envValue:     "false",
			setEnv:       true,
			want:         false,
		},
		{
			name:         "default value",
			key:          "TEST_BOOL_5",
			defaultValue: true,
			envValue:     "",
			setEnv:       false,
			want:         true,
		},
		{
			name:         "random string",
			key:          "TEST_BOOL_6",
			defaultValue: true,
			envValue:     "random",
			setEnv:       true,
			want:         false,
		},
	}

	originalGetEnvFunc := getEnvFunc
	defer func() {
		getEnvFunc = originalGetEnvFunc
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				getEnvFunc = func(key string) string {
					if key == tt.key {
						return tt.envValue
					}
					return ""
				}
			} else {
				getEnvFunc = func(key string) string {
					return ""
				}
			}

			got := getEnvAsBoolOrDefault(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvAsBoolOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanupPolicies(t *testing.T) {
	artifactName := "my-artifact"

	policyGVR := schema.GroupVersionResource{
		Group:    "kyverno.io",
		Version:  "v1",
		Resource: "policies",
	}
	clusterPolicyGVR := schema.GroupVersionResource{
		Group:    "kyverno.io",
		Version:  "v1",
		Resource: "clusterpolicies",
	}

	tests := []struct {
		name            string
		existing        []runtime.Object
		expectedDeletes int
	}{
		{
			name: "cleanup matching policies",
			existing: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.io/v1",
						"kind":       "Policy",
						"metadata": map[string]interface{}{
							"name":      "test-policy",
							"namespace": "default",
							"labels": map[string]interface{}{
								"artifact-name": artifactName,
							},
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.io/v1",
						"kind":       "ClusterPolicy",
						"metadata": map[string]interface{}{
							"name": "test-clusterpolicy",
							"labels": map[string]interface{}{
								"artifact-name": artifactName,
							},
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.io/v1",
						"kind":       "Policy",
						"metadata": map[string]interface{}{
							"name":      "another-policy",
							"namespace": "default",
							"labels": map[string]interface{}{
								"artifact-name": "another-artifact",
							},
						},
					},
				},
			},
			expectedDeletes: 2,
		},
		{
			name:            "no matching policies",
			existing:        []runtime.Object{},
			expectedDeletes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			scheme.AddKnownTypeWithName(policyGVR.GroupVersion().WithKind("Policy"), &unstructured.Unstructured{})
			scheme.AddKnownTypeWithName(clusterPolicyGVR.GroupVersion().WithKind("ClusterPolicy"), &unstructured.Unstructured{})
			scheme.AddKnownTypeWithName(policyGVR.GroupVersion().WithKind("PolicyList"), &unstructured.UnstructuredList{})
			scheme.AddKnownTypeWithName(clusterPolicyGVR.GroupVersion().WithKind("ClusterPolicyList"), &unstructured.UnstructuredList{})

			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, tt.existing...)

			config := &Config{
				ArtifactName: artifactName,
			}

			cleanupPolicies(config, dynamicClient)

			// Verify deletions
			actions := dynamicClient.Actions()
			deleteActions := 0
			for _, action := range actions {
				if action.GetVerb() == "delete" {
					deleteActions++
				}
			}

			if deleteActions != tt.expectedDeletes {
				t.Errorf("expected %d delete actions, got %d", tt.expectedDeletes, deleteActions)
			}
		})
	}
}
