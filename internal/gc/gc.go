package gc

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bitfield/script"
)

var (
	// Version is set via ldflags during build
	Version = "dev"
	// scriptExecFunc can be overridden in tests
	scriptExecFunc = scriptExec
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

	// Get all policies with managed-by=kyverno-watcher label
	policies := getManagedPolicies()

	log.Printf("Found %d managed policies to check\n", len(policies))

	orphanedCount := 0
	for _, policy := range policies {
		if isOrphaned(policy) {
			log.Printf("Found orphaned policy: %s (namespace: %s, kind: %s)\n",
				policy.Name, policy.Namespace, policy.Kind)

			if err := deletePolicy(policy); err != nil {
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
func getManagedPolicies() []PolicyInfo {
	policies := make([]PolicyInfo, 0)

	// Get namespaced Policies
	namespacedPolicies, err := getPoliciesByKind("Policy")
	if err != nil {
		log.Printf("Warning: failed to list Policy resources: %v\n", err)
	} else {
		policies = append(policies, namespacedPolicies...)
	}

	// Get ClusterPolicies
	clusterPolicies, err := getPoliciesByKind("ClusterPolicy")
	if err != nil {
		log.Printf("Warning: failed to list ClusterPolicy resources: %v\n", err)
	} else {
		policies = append(policies, clusterPolicies...)
	}

	return policies
}

// getPoliciesByKind retrieves policies of a specific kind with the managed-by label
func getPoliciesByKind(kind string) ([]PolicyInfo, error) {
	// Build kubectl command to get policies with the label
	var cmd string
	if kind == "ClusterPolicy" {
		cmd = "kubectl get clusterpolicies -l managed-by=kyverno-watcher -o json"
	} else {
		cmd = "kubectl get policies --all-namespaces -l managed-by=kyverno-watcher -o json"
	}

	// Execute kubectl command
	result, err := scriptExecFunc(cmd)
	if err != nil {
		return nil, fmt.Errorf("kubectl get failed: %w", err)
	}

	// Parse JSON response
	var list struct {
		Items []struct {
			Metadata struct {
				Name      string            `json:"name"`
				Namespace string            `json:"namespace,omitempty"`
				Labels    map[string]string `json:"labels,omitempty"`
			} `json:"metadata"`
			Kind string `json:"kind"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(result), &list); err != nil {
		return nil, fmt.Errorf("failed to parse kubectl output: %w", err)
	}

	policies := make([]PolicyInfo, 0, len(list.Items))
	for _, item := range list.Items {
		policies = append(policies, PolicyInfo{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
			Kind:      kind,
			Labels:    item.Metadata.Labels,
		})
	}

	return policies, nil
}

// isOrphaned checks if a policy is orphaned (KyvernoArtifact and pod are gone)
func isOrphaned(policy PolicyInfo) bool {
	policyVersion, hasVersion := policy.Labels["policy-version"]
	if !hasVersion {
		log.Printf("Policy %s has no policy-version label, skipping\n", policy.Name)
		return false
	}

	// Check if any KyvernoArtifact exists that could own this policy
	hasActiveWatcher, err := checkForActiveWatchers()
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
	hasKyvernoArtifact, err := checkForKyvernoArtifacts()
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

// checkForActiveWatchers checks if there are any active watcher pods
func checkForActiveWatchers() (bool, error) {
	result, err := scriptExecFunc("kubectl get pods --all-namespaces -l app -o json")
	if err != nil {
		return false, fmt.Errorf("kubectl get pods failed: %w", err)
	}

	var podList struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels,omitempty"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(result), &podList); err != nil {
		return false, fmt.Errorf("failed to parse pod list: %w", err)
	}

	// Check for pods with names starting with "kyverno-artifact-manager-"
	for _, pod := range podList.Items {
		if strings.HasPrefix(pod.Metadata.Name, "kyverno-artifact-manager-") &&
			(pod.Status.Phase == "Running" || pod.Status.Phase == "Pending") {
			return true, nil
		}
	}

	return false, nil
}

// checkForKyvernoArtifacts checks if there are any KyvernoArtifact resources
func checkForKyvernoArtifacts() (bool, error) {
	result, err := scriptExecFunc("kubectl get kyvernoartifacts --all-namespaces -o json")
	if err != nil {
		return false, fmt.Errorf("kubectl get kyvernoartifacts failed: %w", err)
	}

	var artifactList struct {
		Items []interface{} `json:"items"`
	}

	if err := json.Unmarshal([]byte(result), &artifactList); err != nil {
		return false, fmt.Errorf("failed to parse artifact list: %w", err)
	}

	return len(artifactList.Items) > 0, nil
}

// deletePolicy deletes a policy resource
func deletePolicy(policy PolicyInfo) error {
	var cmd string
	if policy.Kind == "ClusterPolicy" {
		cmd = fmt.Sprintf("kubectl delete clusterpolicy %s", policy.Name)
	} else {
		if policy.Namespace != "" {
			cmd = fmt.Sprintf("kubectl delete policy %s -n %s", policy.Name, policy.Namespace)
		} else {
			cmd = fmt.Sprintf("kubectl delete policy %s", policy.Name)
		}
	}

	_, err := scriptExecFunc(cmd)
	if err != nil {
		return fmt.Errorf("kubectl delete failed: %w", err)
	}

	return nil
}

// scriptExec is a wrapper around script.Exec for easier testing
func scriptExec(cmd string) (string, error) {
	return script.Exec(cmd).String()
}
