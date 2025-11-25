package gc

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/OctoKode/kyverno-artifact-operator/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var (
	// Version is set via ldflags during build
	Version = "dev"
	// getKubeClientFunc can be overridden in tests
	getKubeClientFunc = k8s.GetClient
)

// PolicyInfo holds basic policy information
type PolicyInfo struct {
	Name      string
	Namespace string
	Kind      string
	Labels    map[string]string
}

// Run starts the garbage collector for orphaned policies
func Run(version string, pollInterval int) {
	Version = version
	log.Printf("Kyverno Policy Garbage Collector version %s\n", Version)
	log.Printf("Starting garbage collector with polling interval of %d seconds\n", pollInterval)

	for {
		collectGarbage()
		time.Sleep(time.Duration(pollInterval) * time.Second)
	}
}

// collectGarbage finds and deletes orphaned policies
func collectGarbage() {
	log.Println("Starting garbage collection cycle...")

	// Get Kubernetes clients
	clientset, dynamicClient, err := getKubeClientFunc()
	if err != nil {
		log.Printf("Error getting Kubernetes clients: %v\n", err)
		return
	}

	// Get all policies with managed-by=kyverno-watcher label
	policies := getManagedPolicies(dynamicClient)

	log.Printf("Found %d managed policies to check\n", len(policies))

	orphanedCount := 0
	for _, policy := range policies {
		if isOrphaned(policy, clientset, dynamicClient) {
			log.Printf("Found orphaned policy: %s (namespace: %s, kind: %s)\n",
				policy.Name, policy.Namespace, policy.Kind)

			if err := deletePolicy(policy, dynamicClient); err != nil {
				log.Printf("Error deleting orphaned policy %s: %v\n", policy.Name, err)
				continue
			}

			log.Printf("Successfully deleted orphaned policy: %s\n", policy.Name)
			orphanedCount++
		}
	}

	if orphanedCount > 0 {
		log.Printf("Garbage collection complete: deleted %d orphaned policies\n", orphanedCount)
	} else {
		log.Println("Garbage collection complete: no orphaned policies found")
	}
}

// getManagedPolicies returns all Policy and ClusterPolicy resources with managed-by=kyverno-watcher label
func getManagedPolicies(dynamicClient dynamic.Interface) []PolicyInfo {
	policies := make([]PolicyInfo, 0)
	ctx := context.Background()

	// Define GVRs for Kyverno policies
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

	// Get namespaced Policies
	namespacedPolicies, err := getPoliciesByKind(ctx, dynamicClient, policyGVR, "")
	if err != nil {
		log.Printf("Warning: failed to list Policy resources: %v\n", err)
	} else {
		policies = append(policies, namespacedPolicies...)
	}

	// Get ClusterPolicies
	clusterPolicies, err := getPoliciesByKind(ctx, dynamicClient, clusterPolicyGVR, "")
	if err != nil {
		log.Printf("Warning: failed to list ClusterPolicy resources: %v\n", err)
	} else {
		policies = append(policies, clusterPolicies...)
	}

	return policies
}

// getPoliciesByKind retrieves policies of a specific kind with the managed-by label
func getPoliciesByKind(ctx context.Context, dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]PolicyInfo, error) {
	labelSelector := "managed-by=kyverno-watcher"

	var list interface{}
	var err error

	if namespace == "" {
		// List across all namespaces
		list, err = dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	} else {
		list, err = dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", gvr.Resource, err)
	}

	unstructuredList, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return nil, fmt.Errorf("unexpected list type")
	}

	policies := make([]PolicyInfo, 0, len(unstructuredList.Items))
	for _, item := range unstructuredList.Items {
		kind := "Policy"
		if gvr.Resource == "clusterpolicies" {
			kind = "ClusterPolicy"
		}

		policies = append(policies, PolicyInfo{
			Name:      item.GetName(),
			Namespace: item.GetNamespace(),
			Kind:      kind,
			Labels:    item.GetLabels(),
		})
	}

	return policies, nil
}

// isOrphaned checks if a policy is orphaned (its specific KyvernoArtifact or watcher pod are gone)
func isOrphaned(policy PolicyInfo, clientset kubernetes.Interface, dynamicClient dynamic.Interface) bool {
	policyVersion, hasVersion := policy.Labels["policy-version"]
	if !hasVersion {
		log.Printf("Policy %s has no policy-version label, skipping\n", policy.Name)
		return false
	}

	// Get the artifact name that owns this policy
	artifactName, hasArtifactName := policy.Labels["artifact-name"]
	if !hasArtifactName {
		// For backward compatibility, if no artifact-name label, fall back to global check
		log.Printf("Policy %s has no artifact-name label, using legacy orphan check\n", policy.Name)
		return isOrphanedLegacy(policy, policyVersion, clientset, dynamicClient)
	}

	// Check if the specific KyvernoArtifact exists
	hasKyvernoArtifact, err := checkForSpecificKyvernoArtifact(dynamicClient, artifactName)
	if err != nil {
		log.Printf("Warning: failed to check for KyvernoArtifact %s: %v\n", artifactName, err)
		return false
	}

	if !hasKyvernoArtifact {
		log.Printf("Policy %s (version: %s) appears orphaned: KyvernoArtifact %s not found\n",
			policy.Name, policyVersion, artifactName)
		return true
	}

	// Check if the specific watcher pod exists for this artifact
	hasActiveWatcher, err := checkForSpecificWatcher(clientset, artifactName)
	if err != nil {
		log.Printf("Warning: failed to check for watcher pod for artifact %s: %v\n", artifactName, err)
		return false
	}

	if !hasActiveWatcher {
		log.Printf("Policy %s (version: %s) appears orphaned: no active watcher pod for artifact %s\n",
			policy.Name, policyVersion, artifactName)
		return true
	}

	// The specific KyvernoArtifact and watcher exist
	return false
}

// isOrphanedLegacy checks for orphaned policies without artifact-name label (backward compatibility)
func isOrphanedLegacy(policy PolicyInfo, policyVersion string, clientset kubernetes.Interface, dynamicClient dynamic.Interface) bool {
	// Check if any KyvernoArtifact exists that could own this policy
	hasActiveWatcher, err := checkForActiveWatchers(clientset)
	if err != nil {
		log.Printf("Warning: failed to check for active watchers: %v\n", err)
		return false
	}

	if !hasActiveWatcher {
		log.Printf("Policy %s (version: %s) appears orphaned: no active watchers found\n",
			policy.Name, policyVersion)
		return true
	}

	// Check if there's a KyvernoArtifact that could have created this policy
	hasKyvernoArtifact, err := checkForKyvernoArtifacts(dynamicClient)
	if err != nil {
		log.Printf("Warning: failed to check for KyvernoArtifacts: %v\n", err)
		return false
	}

	if !hasKyvernoArtifact {
		log.Printf("Policy %s (version: %s) appears orphaned: no KyvernoArtifacts found\n",
			policy.Name, policyVersion)
		return true
	}

	// If we have both watchers and artifacts, assume the policy is not orphaned
	return false
}

// checkForSpecificWatcher checks if the watcher pod for a specific artifact exists and is active
func checkForSpecificWatcher(clientset kubernetes.Interface, artifactName string) (bool, error) {
	ctx := context.Background()
	expectedPodPrefix := fmt.Sprintf("kyverno-artifact-manager-%s", artifactName)

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		// Pod names are exactly "kyverno-artifact-manager-{artifactName}"
		if pod.Name == expectedPodPrefix &&
			(pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending) {
			return true, nil
		}
	}

	return false, nil
}

// checkForSpecificKyvernoArtifact checks if a specific KyvernoArtifact exists
func checkForSpecificKyvernoArtifact(dynamicClient dynamic.Interface, artifactName string) (bool, error) {
	ctx := context.Background()

	artifactGVR := schema.GroupVersionResource{
		Group:    "kyverno.octokode.io",
		Version:  "v1alpha1",
		Resource: "kyvernoartifacts",
	}

	// Check across all namespaces for the specific artifact
	list, err := dynamicClient.Resource(artifactGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list kyvernoartifacts: %w", err)
	}

	for _, item := range list.Items {
		if item.GetName() == artifactName {
			return true, nil
		}
	}

	return false, nil
}

// checkForActiveWatchers checks if there are any active watcher pods
func checkForActiveWatchers(clientset kubernetes.Interface) (bool, error) {
	ctx := context.Background()

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app",
	})
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	// Check for pods with names starting with "kyverno-artifact-manager-"
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, "kyverno-artifact-manager-") &&
			(pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending) {
			return true, nil
		}
	}

	return false, nil
}

// checkForKyvernoArtifacts checks if there are any KyvernoArtifact resources
func checkForKyvernoArtifacts(dynamicClient dynamic.Interface) (bool, error) {
	ctx := context.Background()

	artifactGVR := schema.GroupVersionResource{
		Group:    "kyverno.octokode.io",
		Version:  "v1alpha1",
		Resource: "kyvernoartifacts",
	}

	list, err := dynamicClient.Resource(artifactGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list kyvernoartifacts: %w", err)
	}

	return len(list.Items) > 0, nil
}

// deletePolicy deletes a policy resource
func deletePolicy(policy PolicyInfo, dynamicClient dynamic.Interface) error {
	ctx := context.Background()

	var gvr schema.GroupVersionResource
	if policy.Kind == "ClusterPolicy" {
		gvr = schema.GroupVersionResource{
			Group:    "kyverno.io",
			Version:  "v1",
			Resource: "clusterpolicies",
		}
	} else {
		gvr = schema.GroupVersionResource{
			Group:    "kyverno.io",
			Version:  "v1",
			Resource: "policies",
		}
	}

	if policy.Namespace != "" {
		err := dynamicClient.Resource(gvr).Namespace(policy.Namespace).Delete(ctx, policy.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("failed to delete policy: %w", err)
		}
	} else {
		err := dynamicClient.Resource(gvr).Delete(ctx, policy.Name, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("failed to delete clusterpolicy: %w", err)
		}
	}

	return nil
}
