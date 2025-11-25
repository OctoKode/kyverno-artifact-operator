package gc

import (
	"context"
	"testing"

	"github.com/OctoKode/kyverno-artifact-operator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

const (
	testPolicyName         = "test-policy"
	policyKind             = "Policy"
	clusterPolicyKind      = "ClusterPolicy"
	managedByLabel         = "managed-by"
	managedByValue         = "kyverno-watcher"
	policyVersionLabel     = "policy-version"
	kyvernoAPIGroup        = "kyverno.io"
	kyvernoAPIVersion      = "v1"
	kyvernoArtifactGroup   = "kyverno.octokode.io"
	kyvernoArtifactVersion = "v1alpha1"
)

func TestPolicyInfo(t *testing.T) {
	policy := PolicyInfo{
		Name:      testPolicyName,
		Namespace: "default",
		Kind:      policyKind,
		Labels: map[string]string{
			managedByLabel:     managedByValue,
			policyVersionLabel: "v1.0.0",
		},
	}

	if policy.Name != testPolicyName {
		t.Errorf("Expected name %q, got '%s'", testPolicyName, policy.Name)
	}
	if policy.Kind != policyKind {
		t.Errorf("Expected kind %q, got '%s'", policyKind, policy.Kind)
	}
	if policy.Labels[managedByLabel] != managedByValue {
		t.Errorf("Expected managed-by label %q, got '%s'", managedByValue, policy.Labels[managedByLabel])
	}
}

func TestVersionVariable(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}

	// Test setting version
	oldVersion := Version
	Version = "test-1.0.0"
	if Version != "test-1.0.0" {
		t.Errorf("Version = %q, want %q", Version, "test-1.0.0")
	}
	Version = oldVersion
}

// TestGetKubeClient verifies the function exists and has correct signature
func TestGetKubeClient(t *testing.T) {
	// This will attempt to get real config, which may fail in test environment
	_, _, err := k8s.GetClient()
	// It's OK if this fails - we're running in a test environment without a cluster
	if err == nil {
		t.Log("Successfully got kube client (test environment has cluster access)")
	} else {
		t.Logf("Expected error in test environment without cluster: %v", err)
	}
}

func TestGetManagedPolicies(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create a ClusterPolicy
	clusterPolicy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": kyvernoAPIGroup + "/" + kyvernoAPIVersion,
			"kind":       clusterPolicyKind,
			"metadata": map[string]interface{}{
				"name": "test-cluster-policy",
				"labels": map[string]interface{}{
					managedByLabel:     managedByValue,
					policyVersionLabel: "v1.0.0",
				},
			},
		},
	}

	// Create a Policy
	policy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": kyvernoAPIGroup + "/" + kyvernoAPIVersion,
			"kind":       policyKind,
			"metadata": map[string]interface{}{
				"name":      testPolicyName,
				"namespace": "default",
				"labels": map[string]interface{}{
					managedByLabel:     managedByValue,
					policyVersionLabel: "v2.0.0",
				},
			},
		},
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, clusterPolicy, policy)

	policies := getManagedPolicies(dynamicClient)

	if len(policies) != 2 {
		t.Errorf("Expected 2 policies, got %d", len(policies))
	}

	// Verify ClusterPolicy
	foundClusterPolicy := false
	foundPolicy := false
	for _, p := range policies {
		if p.Name == "test-cluster-policy" && p.Kind == clusterPolicyKind {
			foundClusterPolicy = true
			if p.Labels[policyVersionLabel] != "v1.0.0" {
				t.Errorf("Expected policy-version v1.0.0, got %s", p.Labels[policyVersionLabel])
			}
		}
		if p.Name == testPolicyName && p.Kind == policyKind {
			foundPolicy = true
			if p.Namespace != "default" {
				t.Errorf("Expected namespace 'default', got '%s'", p.Namespace)
			}
		}
	}

	if !foundClusterPolicy {
		t.Error("ClusterPolicy not found in results")
	}
	if !foundPolicy {
		t.Error("Policy not found in results")
	}
}

func TestGetPoliciesByKind(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	policy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": kyvernoAPIGroup + "/" + kyvernoAPIVersion,
			"kind":       policyKind,
			"metadata": map[string]interface{}{
				"name":      testPolicyName,
				"namespace": "default",
				"labels": map[string]interface{}{
					managedByLabel: managedByValue,
				},
			},
		},
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, policy)

	gvr := schema.GroupVersionResource{
		Group:    kyvernoAPIGroup,
		Version:  kyvernoAPIVersion,
		Resource: "policies",
	}

	ctx := context.Background()
	policies, err := getPoliciesByKind(ctx, dynamicClient, gvr, "")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(policies) != 1 {
		t.Errorf("Expected 1 policy, got %d", len(policies))
	}

	if policies[0].Name != testPolicyName {
		t.Errorf("Expected policy name %q, got '%s'", testPolicyName, policies[0].Name)
	}
}

func TestIsOrphaned(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name             string
		policy           PolicyInfo
		pods             []runtime.Object
		artifacts        []runtime.Object
		expectedOrphaned bool
	}{
		{
			name: "policy without version label",
			policy: PolicyInfo{
				Name:   "test-policy",
				Kind:   "Policy",
				Labels: map[string]string{"managed-by": "kyverno-watcher"},
			},
			expectedOrphaned: false,
		},
		{
			name: "legacy policy with no active watchers",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			pods:             []runtime.Object{},
			expectedOrphaned: true,
		},
		{
			name: "legacy policy with active watcher but no artifacts",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-abc123",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			artifacts:        []runtime.Object{},
			expectedOrphaned: true,
		},
		{
			name: "legacy policy with active watcher and artifacts",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-abc123",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "test-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedOrphaned: false,
		},
		// New test cases for specific artifact tracking
		{
			name: "policy with artifact-name label and matching artifact exists",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
					"artifact-name":  "my-artifact",
				},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-my-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-my-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "my-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedOrphaned: false,
		},
		{
			name: "policy with artifact-name label but artifact deleted",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
					"artifact-name":  "deleted-artifact",
				},
			},
			pods: []runtime.Object{
				// Other watcher pods exist but not for this artifact
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-other-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-other-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			artifacts: []runtime.Object{
				// Other artifacts exist but not the one we're looking for
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "other-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedOrphaned: true, // Should be orphaned because its specific artifact is gone
		},
		{
			name: "policy with artifact-name label but watcher pod is missing",
			policy: PolicyInfo{
				Name: "test-policy",
				Kind: "Policy",
				Labels: map[string]string{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
					"artifact-name":  "my-artifact",
				},
			},
			pods: []runtime.Object{}, // No pods at all
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "my-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedOrphaned: true, // Artifact exists but watcher pod is gone
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fakeclientset.NewSimpleClientset(tt.pods...)

			// Register KyvernoArtifact list kind
			gvr := schema.GroupVersionResource{
				Group:    "kyverno.octokode.io",
				Version:  "v1alpha1",
				Resource: "kyvernoartifacts",
			}
			listKind := schema.GroupVersionKind{
				Group:   "kyverno.octokode.io",
				Version: "v1alpha1",
				Kind:    "KyvernoArtifactList",
			}
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
				scheme,
				map[schema.GroupVersionResource]string{gvr: listKind.Kind},
				tt.artifacts...,
			)

			orphaned := isOrphaned(tt.policy, clientset, dynamicClient)
			if orphaned != tt.expectedOrphaned {
				t.Errorf("Expected orphaned=%v, got %v", tt.expectedOrphaned, orphaned)
			}
		})
	}
}

func TestCheckForActiveWatchers(t *testing.T) {
	tests := []struct {
		name           string
		pods           []runtime.Object
		expectedActive bool
		expectError    bool
	}{
		{
			name:           "no pods",
			pods:           []runtime.Object{},
			expectedActive: false,
			expectError:    false,
		},
		{
			name: "running watcher pod",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-abc123",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedActive: true,
			expectError:    false,
		},
		{
			name: "pending watcher pod",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-xyz789",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			expectedActive: true,
			expectError:    false,
		},
		{
			name: "failed watcher pod",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-failed",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodFailed},
				},
			},
			expectedActive: false,
			expectError:    false,
		},
		{
			name: "wrong name prefix",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "other"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedActive: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fakeclientset.NewSimpleClientset(tt.pods...)

			hasActive, err := checkForActiveWatchers(clientset)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if hasActive != tt.expectedActive {
				t.Errorf("Expected active=%v, got %v", tt.expectedActive, hasActive)
			}
		})
	}
}

func TestCheckForKyvernoArtifacts(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Register KyvernoArtifact list kind
	gvr := schema.GroupVersionResource{
		Group:    "kyverno.octokode.io",
		Version:  "v1alpha1",
		Resource: "kyvernoartifacts",
	}
	listKind := schema.GroupVersionKind{
		Group:   "kyverno.octokode.io",
		Version: "v1alpha1",
		Kind:    "KyvernoArtifactList",
	}

	tests := []struct {
		name                 string
		artifacts            []runtime.Object
		expectedHasArtifacts bool
	}{
		{
			name:                 "no artifacts",
			artifacts:            []runtime.Object{},
			expectedHasArtifacts: false,
		},
		{
			name: "one artifact",
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "test-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedHasArtifacts: true,
		},
		{
			name: "multiple artifacts",
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "artifact-1",
							"namespace": "default",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "artifact-2",
							"namespace": "kube-system",
						},
					},
				},
			},
			expectedHasArtifacts: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
				scheme,
				map[schema.GroupVersionResource]string{gvr: listKind.Kind},
				tt.artifacts...,
			)

			hasArtifacts, err := checkForKyvernoArtifacts(dynamicClient)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if hasArtifacts != tt.expectedHasArtifacts {
				t.Errorf("Expected hasArtifacts=%v, got %v", tt.expectedHasArtifacts, hasArtifacts)
			}
		})
	}
}

func TestCheckForSpecificWatcher(t *testing.T) {
	tests := []struct {
		name           string
		artifactName   string
		pods           []runtime.Object
		expectedActive bool
		expectError    bool
	}{
		{
			name:           "no pods",
			artifactName:   "my-artifact",
			pods:           []runtime.Object{},
			expectedActive: false,
			expectError:    false,
		},
		{
			name:         "matching running watcher pod",
			artifactName: "my-artifact",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-my-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-my-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedActive: true,
			expectError:    false,
		},
		{
			name:         "matching pending watcher pod",
			artifactName: "my-artifact",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-my-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-my-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			expectedActive: true,
			expectError:    false,
		},
		{
			name:         "matching failed watcher pod",
			artifactName: "my-artifact",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-my-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-my-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodFailed},
				},
			},
			expectedActive: false, // Failed pods are not active
			expectError:    false,
		},
		{
			name:         "other watcher pod exists but not the one we're looking for",
			artifactName: "my-artifact",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "kyverno-artifact-manager-other-artifact",
						Namespace: "default",
						Labels:    map[string]string{"app": "kyverno-artifact-manager-other-artifact"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedActive: false, // Wrong artifact name
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fakeclientset.NewSimpleClientset(tt.pods...)

			hasActive, err := checkForSpecificWatcher(clientset, tt.artifactName)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}

			if hasActive != tt.expectedActive {
				t.Errorf("Expected active=%v, got %v", tt.expectedActive, hasActive)
			}
		})
	}
}

func TestCheckForSpecificKyvernoArtifact(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Register KyvernoArtifact list kind
	gvr := schema.GroupVersionResource{
		Group:    "kyverno.octokode.io",
		Version:  "v1alpha1",
		Resource: "kyvernoartifacts",
	}
	listKind := schema.GroupVersionKind{
		Group:   "kyverno.octokode.io",
		Version: "v1alpha1",
		Kind:    "KyvernoArtifactList",
	}

	tests := []struct {
		name         string
		artifactName string
		artifacts    []runtime.Object
		expectedHas  bool
	}{
		{
			name:         "no artifacts",
			artifactName: "my-artifact",
			artifacts:    []runtime.Object{},
			expectedHas:  false,
		},
		{
			name:         "matching artifact exists",
			artifactName: "my-artifact",
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "my-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedHas: true,
		},
		{
			name:         "other artifacts exist but not the one we're looking for",
			artifactName: "my-artifact",
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "other-artifact",
							"namespace": "default",
						},
					},
				},
			},
			expectedHas: false,
		},
		{
			name:         "multiple artifacts including the one we're looking for",
			artifactName: "my-artifact",
			artifacts: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "other-artifact",
							"namespace": "default",
						},
					},
				},
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.octokode.io/v1alpha1",
						"kind":       "KyvernoArtifact",
						"metadata": map[string]interface{}{
							"name":      "my-artifact",
							"namespace": "kube-system",
						},
					},
				},
			},
			expectedHas: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
				scheme,
				map[schema.GroupVersionResource]string{gvr: listKind.Kind},
				tt.artifacts...,
			)

			hasArtifact, err := checkForSpecificKyvernoArtifact(dynamicClient, tt.artifactName)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			if hasArtifact != tt.expectedHas {
				t.Errorf("Expected hasArtifact=%v, got %v", tt.expectedHas, hasArtifact)
			}
		})
	}
}

func TestDeletePolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name      string
		policy    PolicyInfo
		setupObjs []runtime.Object
	}{
		{
			name: "delete namespaced policy",
			policy: PolicyInfo{
				Name:      "test-policy",
				Namespace: "default",
				Kind:      "Policy",
			},
			setupObjs: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.io/v1",
						"kind":       "Policy",
						"metadata": map[string]interface{}{
							"name":      "test-policy",
							"namespace": "default",
						},
					},
				},
			},
		},
		{
			name: "delete cluster policy",
			policy: PolicyInfo{
				Name: "test-cluster-policy",
				Kind: "ClusterPolicy",
			},
			setupObjs: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "kyverno.io/v1",
						"kind":       "ClusterPolicy",
						"metadata": map[string]interface{}{
							"name": "test-cluster-policy",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, tt.setupObjs...)

			err := deletePolicy(tt.policy, dynamicClient)
			if err != nil {
				t.Errorf("Expected no error, got %v", err)
			}

			// Verify the policy was deleted
			ctx := context.Background()
			var gvr schema.GroupVersionResource
			if tt.policy.Kind == clusterPolicyKind {
				gvr = schema.GroupVersionResource{
					Group:    kyvernoAPIGroup,
					Version:  kyvernoAPIVersion,
					Resource: "clusterpolicies",
				}
				_, err = dynamicClient.Resource(gvr).Get(ctx, tt.policy.Name, metav1.GetOptions{})
			} else {
				gvr = schema.GroupVersionResource{
					Group:    kyvernoAPIGroup,
					Version:  kyvernoAPIVersion,
					Resource: "policies",
				}
				_, err = dynamicClient.Resource(gvr).Namespace(tt.policy.Namespace).Get(ctx, tt.policy.Name, metav1.GetOptions{})
			}

			if err == nil {
				t.Error("Expected policy to be deleted, but it still exists")
			}
		})
	}
}

func TestCollectGarbage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Mock getKubeClientFunc
	oldFunc := getKubeClientFunc
	defer func() { getKubeClientFunc = oldFunc }()

	policy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kyverno.io/v1",
			"kind":       "Policy",
			"metadata": map[string]interface{}{
				"name":      "orphaned-policy",
				"namespace": "default",
				"labels": map[string]interface{}{
					"managed-by":     "kyverno-watcher",
					"policy-version": "v1.0.0",
				},
			},
		},
	}

	// Register list kinds for all resources we'll query
	listKinds := map[schema.GroupVersionResource]string{
		{Group: "kyverno.io", Version: "v1", Resource: "policies"}:                        "PolicyList",
		{Group: "kyverno.io", Version: "v1", Resource: "clusterpolicies"}:                 "ClusterPolicyList",
		{Group: "kyverno.octokode.io", Version: "v1alpha1", Resource: "kyvernoartifacts"}: "KyvernoArtifactList",
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, policy)
	clientset := fakeclientset.NewSimpleClientset()

	getKubeClientFunc = func() (kubernetes.Interface, dynamic.Interface, error) {
		return clientset, dynamicClient, nil
	}

	// This should run without panicking
	collectGarbage()
}

// Integration tests would require:
// 1. A running Kubernetes cluster
// 2. Kyverno CRDs installed
// 3. Test fixtures for policies and artifacts
// These should be run in a separate integration test suite with proper setup/teardown
