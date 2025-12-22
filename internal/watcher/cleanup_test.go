package watcher

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
)

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
		name      string
		existing  []runtime.Object
		expectErr bool
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
			expectErr: false,
		},
		{
			name:      "no matching policies",
			existing:  []runtime.Object{},
			expectErr: false,
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

			if tt.name == "cleanup matching policies" {
				if deleteActions != 2 {
					t.Errorf("expected 2 delete actions, got %d", deleteActions)
				}
			}

			if tt.name == "no matching policies" {
				if deleteActions != 0 {
					t.Errorf("expected 0 delete actions, got %d", deleteActions)
				}
			}
		})
	}
}
