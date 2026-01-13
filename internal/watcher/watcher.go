package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/OctoKode/kyverno-artifact-operator/internal/k8s"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	orasremote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
	"sigs.k8s.io/yaml"
)

const (
	PolicyLayerMediaType = "application/vnd.cncf.kyverno.policy.layer.v1+yaml"
)

var (
	// Version is set via ldflags during build
	Version = "dev"
	// orasPullFunc can be overridden in tests
	orasPullFunc = orasPull
	// applyManifestsFunc can be overridden in tests
	applyManifestsFunc = applyManifestsReal
	// pullImageToDirFunc can be overridden in tests
	pullImageToDirFunc = pullImageToDirReal
	// tagChangedFunc can be overridden in tests
	tagChangedFunc = tagChanged
	// getKubernetesClientsFunc can be overridden in tests
	getKubernetesClientsFunc = getKubernetesClients
	// checksumsChangedFunc can be overridden in tests
	checksumsChangedFunc = checksumsChanged
	// getLatestTagOrDigestFunc can be overridden in tests
	getLatestTagOrDigestFunc = getLatestTagOrDigestReal
	// getLatestArtifactoryTagFunc can be overridden in tests
	getLatestArtifactoryTagFunc = getLatestArtifactoryTagReal

	// podsGVR is the GroupVersionResource for Kubernetes Pods, used for dynamic client operations.
	podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
)

// reconcileManagedPods checks for outdated watcher pods managed by the operator
// and deletes them to ensure all watchers are running the latest version.
// This function runs once on startup of a watcher pod.
func reconcileManagedPods(config *Config) error {
	// If the WATCHER_IMAGE environment variable is not set, this watcher cannot perform
	// self-reconciliation, so skip this step. This would typically happen if the operator
	// deployment is not configured correctly or during local development/testing without full context.
	if config.WatcherImage == "" {
		log.Println("WATCHER_IMAGE not set in watcher configuration, skipping managed pod reconciliation.")
		return nil
	}
	// If the PodNamespace is not set, the watcher cannot determine its own namespace to list
	// other pods, so skip this step. This should be injected by the operator via the Downward API.
	if config.PodNamespace == "" {
		log.Println("POD_NAMESPACE not set in watcher configuration, skipping managed pod reconciliation.")
		return nil
	}

	log.Printf("Starting managed pod reconciliation for watcher image: %s in namespace: %s\n", config.WatcherImage, config.PodNamespace)

	dynamicClient, _, err := getKubernetesClientsFunc()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clients for managed pod reconciliation: %w", err)
	}

	// Define the label selector to find all watcher pods managed by this operator.
	// This relies on the standardized labels applied by the KyvernoArtifact controller.
	labelSelector := "app.kubernetes.io/managed-by=kyverno-artifact-operator,app.kubernetes.io/component=watcher"

	// List all pods matching the label selector in the current namespace.
	podList, err := dynamicClient.Resource(podsGVR).Namespace(config.PodNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list managed pods with selector %q in namespace %q: %w", labelSelector, config.PodNamespace, err)
	}

	if len(podList.Items) == 0 {
		log.Printf("No managed watcher pods found with selector %q in namespace %q.\n", labelSelector, config.PodNamespace)
		return nil
	}

	for _, pod := range podList.Items {
		// Ensure the pod has at least one container and its image can be checked.
		containers, found, err := unstructured.NestedSlice(pod.Object, "spec", "containers")
		if err != nil || !found || len(containers) == 0 {
			log.Printf("Skipping pod %s/%s: could not get containers from spec: %v, found: %t.\n", pod.GetNamespace(), pod.GetName(), err, found)
			continue
		}

		container, ok := containers[0].(map[string]interface{})
		if !ok {
			log.Printf("Skipping pod %s/%s: first container is not a map.\n", pod.GetNamespace(), pod.GetName())
			continue
		}

		image, found, err := unstructured.NestedString(container, "image")
		if err != nil || !found {
			log.Printf("Skipping pod %s/%s: could not get image from first container: %v, found: %t.\n", pod.GetNamespace(), pod.GetName(), err, found)
			continue
		}
		containerImage := image
		// Compare the image of the running pod with the expected WATCHER_IMAGE from this watcher's configuration.
		if containerImage != config.WatcherImage {
			log.Printf("Detected outdated watcher pod: %s/%s. Current image: %s, Expected image: %s. Deleting pod for recreation.\n",
				pod.GetNamespace(), pod.GetName(), containerImage, config.WatcherImage)

			// Delete the pod. Kubernetes (specifically the ReplicaSet/Deployment controller)
			// will automatically recreate it with the correct image based on its owner reference.
			err := dynamicClient.Resource(podsGVR).Namespace(pod.GetNamespace()).Delete(context.Background(), pod.GetName(), metav1.DeleteOptions{})
			if err != nil {
				// Log the error but continue processing other pods to ensure maximum cleanup.
				log.Printf("Error deleting outdated pod %s/%s: %v\n", pod.GetNamespace(), pod.GetName(), err)
			} else {
				log.Printf("Successfully deleted outdated watcher pod: %s/%s.\n", pod.GetNamespace(), pod.GetName())
			}
		} else {
			log.Printf("Watcher pod %s/%s is running the expected image: %s. No action needed.\n",
				pod.GetNamespace(), pod.GetName(), containerImage)
		}
	}

	log.Println("Managed pod reconciliation complete.")
	return nil
}

// Run starts the artifact watcher. This is the main entry point when the binary is run in watcher mode.
func Run(version string) {
	Version = version
	// Print version
	log.Printf("Kyverno Artifact Watcher version %s\n", Version)

	// Load configuration from environment variables. This includes registry credentials,
	// polling intervals, and other operational parameters.
	config := loadConfig()
	log.Printf("Using configuration %+v\n", config)

	// Perform self-reconciliation for managed watcher pods on startup.
	// This ensures that any watcher pods running an outdated image are deleted
	// and recreated by Kubernetes with the latest version.
	if err := reconcileManagedPods(config); err != nil {
		log.Printf("Warning: failed to perform managed pod reconciliation: %v\n", err)
	}

	// If deletion on termination is enabled, set up a signal handler to catch termination signals (like SIGTERM)
	// This allows the watcher to perform a graceful shutdown and clean up any policies it has created,
	// preventing orphaned resources in the cluster.
	if config.DeletePoliciesOnTermination {
		log.Printf("Deleting policies on termination is enabled.")
		// Set up signal handling for graceful shutdown
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
		go func() {
			<-c
			log.Println("Received termination signal, cleaning up policies...")
			kubeConfig, err := k8s.GetConfig()
			if err != nil {
				log.Fatalf("Error getting Kubernetes config for cleanup: %v", err)
			}
			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				log.Fatalf("Error creating dynamic client for cleanup: %v", err)
			}
			cleanupPolicies(config, dynamicClient)
			os.Exit(0)
		}()
	}

	if config.Provider == ProviderGitHub {
		log.Printf("Starting GHCR watcher for %s (owner=%s, package=%s)\n",
			config.ImageBase, config.Owner, config.Package)
	} else {
		log.Printf("Starting Artifactory watcher for %s\n", config.ImageBase)
	}

	// This is the main reconciliation loop. It will run indefinitely.
	for {
		// watchLoop contains the core logic for checking for new artifacts and applying them.
		if err := watchLoop(config); err != nil {
			log.Printf("Error in watch loop: %v\n", err)
		}
		// Wait for the configured polling interval before the next reconciliation cycle.
		time.Sleep(time.Duration(config.PollInterval) * time.Second)
	}
}

// cleanupPolicies deletes all policies and clusterpolicies associated with this watcher
func cleanupPolicies(config *Config, dynamicClient dynamic.Interface) {
	log.Println("Cleaning up policies...")

	labelSelector := fmt.Sprintf("artifact-name=%s", config.ArtifactName)

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

	// Delete namespaced Policies
	if err := deleteResourcesByLabel(dynamicClient, policyGVR, "", labelSelector); err != nil {
		log.Printf("Warning: failed to delete Policy resources: %v\n", err)
	}

	// Delete ClusterPolicies
	if err := deleteResourcesByLabel(dynamicClient, clusterPolicyGVR, "", labelSelector); err != nil {
		log.Printf("Warning: failed to delete ClusterPolicy resources: %v\n", err)
	}

	log.Println("Policy cleanup complete.")
}

// deleteResourcesByLabel deletes all resources of a specific kind matching the label selector
func deleteResourcesByLabel(dynamicClient dynamic.Interface, gvr schema.GroupVersionResource, namespace string, labelSelector string) error {
	ctx := context.Background()

	var list *unstructured.UnstructuredList
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
		return fmt.Errorf("failed to list %s: %w", gvr.Resource, err)
	}

	for _, item := range list.Items {
		log.Printf("Deleting %s %s...\n", item.GetKind(), item.GetName())
		if namespace != "" {
			err = dynamicClient.Resource(gvr).Namespace(namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
		} else {
			err = dynamicClient.Resource(gvr).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
		}
		if err != nil {
			log.Printf("Failed to delete %s %s: %v\n", item.GetKind(), item.GetName(), err)
		}
	}

	return nil
}

// watchLoop is the core reconciliation logic for the watcher.
// It checks for new artifact versions and applies policies to the cluster.
func watchLoop(config *Config) error {
	var isTagChanged bool
	var latest, prevTag string
	var err error

	// The behavior of the watcher depends on whether we are polling for new tags or are pinned to a specific tag.
	if config.PollForTagChanges {
		// If polling is enabled, check the remote registry for the latest tag.
		isTagChanged, latest, prevTag, err = tagChangedFunc(config)
		if err != nil {
			return fmt.Errorf("error checking for tag change: %w", err)
		}
	} else {
		// If polling is disabled, we operate in "pinned" mode. The watcher will only ever use the tag
		// specified in the IMAGE_BASE environment variable. This is useful for ensuring that only a specific
		// version of policies is ever applied, while still allowing for checksum-based reconciliation to fix drift.
		log.Println("Polling for tag changes is disabled.")
		var tag string
		tagFound := false
		if strings.Contains(config.ImageBase, ":") {
			parts := strings.Split(config.ImageBase, ":")
			lastPart := parts[len(parts)-1]
			// Simple heuristic to avoid matching a port number in the image base URL.
			if !strings.Contains(lastPart, "/") {
				tag = lastPart
				tagFound = true
			}
		}

		if !tagFound {
			// If polling is disabled and no tag is specified in the image URL, there is nothing to do.
			log.Println("No tag specified in IMAGE_BASE, nothing to do.")
			return nil
		}

		latest = tag
		// Read the last successfully applied tag from the state file.
		last, err := os.ReadFile(config.LastFile)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read last file: %w", err)
		}
		prevTag = string(last)

		// In pinned mode, a "change" is only when the tag in IMAGE_BASE is different from the last applied tag.
		// If lastFile doesn't exist (e.g., on a fresh start or pod restart with ephemeral storage),
		// we *do not* treat it as a "new tag" event. Instead, `isTagChanged` remains false,
		// and the reconciliation logic (if enabled) will handle the initial application
		// of policies by finding they don't exist in the cluster via checksums.
		isTagChanged = (latest != prevTag && prevTag != "")
	}

	// The most common case is that nothing has changed. If the tag is the same and checksum-based
	// reconciliation is disabled, we can exit early to avoid unnecessary work.
	if !isTagChanged && !config.ReconcilePoliciesFromChecksum {
		log.Printf("No change (latest=%s)\n", latest)
		return nil
	}

	appliedSomething := false

	dynamicClient, mapper, err := getKubernetesClientsFunc()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clients: %w", err)
	}

	// If a new tag is detected, we must re-apply all policies from the new artifact.
	// This is the primary mechanism for rolling out new policy versions.
	if isTagChanged {
		log.Printf("Detected new tag: previous='%s' new='%s'. Applying all manifests.\n", prevTag, latest)
		destDir := fmt.Sprintf("/tmp/image-%s", sanitizePath(latest))

		newChecksums, err := pullImageToDirFunc(config, latest, destDir)
		if err != nil {
			return fmt.Errorf("pull failed: %w", err)
		}

		var allFiles []string
		for filePath := range newChecksums {
			allFiles = append(allFiles, filePath)
		}

		if err := applyManifestsFunc(config, allFiles, mapper, dynamicClient); err != nil {
			return fmt.Errorf("apply manifests failed: %w", err)
		}
		appliedSomething = true

	} else if config.ReconcilePoliciesFromChecksum {
		// If the tag hasn't changed but checksum reconciliation is enabled, we perform a deeper check.
		// This logic handles cases where the policy content may have changed even though the image tag
		// is the same (e.g., a mutable tag like 'latest' was overwritten). It also helps to self-heal
		// if policies in the cluster have been manually modified or deleted.
		log.Printf("No tag change, but checksum reconciliation is enabled. Checking manifests.\n")
		destDir := fmt.Sprintf("/tmp/image-%s", sanitizePath(latest))

		// Pull the artifact to get the current "source of truth" checksums.
		newChecksums, err := pullImageToDirFunc(config, latest, destDir)
		if err != nil {
			return fmt.Errorf("pull failed: %w", err)
		}

		// Compare the checksums from the artifact with the policies currently in the cluster.
		changed, filesToApply, err := checksumsChangedFunc(newChecksums, dynamicClient, mapper)
		if err != nil {
			log.Printf("Error during checksum comparison: %v", err)
		}

		if changed {
			// If any discrepancies are found, apply only the manifests that have changed.
			if err := applyManifestsFunc(config, filesToApply, mapper, dynamicClient); err != nil {
				return fmt.Errorf("apply manifests failed: %w", err)
			}
			appliedSomething = true
		} else {
			log.Println("All policies are up to date, no manifests to apply.")
		}
	}

	// If any policies were successfully applied, update the state file with the latest tag.
	// This file acts as a bookmark, so we know which version is currently running in the cluster.
	if appliedSomething {
		if err := os.WriteFile(config.LastFile, []byte(latest), 0644); err != nil {
			return fmt.Errorf("failed to write last file: %w", err)
		}
	}

	return nil
}

// getLatestTagOrDigestReal fetches the latest tag or digest for a GitHub Container Registry (GHCR) package.
// It queries the GitHub Packages API to find the most recently updated version.
func getLatestTagOrDigestReal(config *Config) (string, error) {
	// Construct the GitHub API URL for listing package versions.
	apiURL := fmt.Sprintf("https://api.github.com/%s/%s/packages/container/%s/versions",
		config.GithubAPIOwnerType, config.Owner, config.PackageNormalized)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Authenticate the request using the provided GitHub token.
	req.Header.Set("Authorization", "token "+config.GithubToken)
	// Specify the API version to accept.
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make API request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Handle non-200 HTTP status codes, providing specific error messages for common issues.
	if resp.StatusCode != http.StatusOK {
		var errMsg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &errMsg) // Attempt to unmarshal error message even if status is not OK.

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return "", fmt.Errorf("authentication failed (401): invalid or expired GITHUB_TOKEN")
		case http.StatusForbidden:
			return "", fmt.Errorf("access forbidden (403): token may lack required permissions (read:packages). Message: %s", errMsg.Message)
		case http.StatusNotFound:
			return "", fmt.Errorf("package not found (404): owner=%s, package=%s (owner type: %s). Verify package exists and token has access",
				config.Owner, config.Package, config.GithubAPIOwnerType)
		default:
			return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, errMsg.Message)
		}
	}

	var versions []GitHubPackageVersion
	if err := json.Unmarshal(body, &versions); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %w. Response body: %s", err, string(body))
	}

	if len(versions) == 0 {
		return "", nil // No versions found for the package.
	}

	// Find the most recently updated version among all available versions.
	latest := versions[0]
	for _, v := range versions {
		if v.UpdatedAt.After(latest.UpdatedAt) {
			latest = v
		}
	}

	// Prioritize returning an actual tag name if available.
	if len(latest.Metadata.Container.Tags) > 0 {
		return latest.Metadata.Container.Tags[0], nil
	}

	// As a fallback, return the version ID if no explicit tags are found.
	return fmt.Sprintf("version-id-%d", latest.ID), nil
}

// getLatestArtifactoryTagReal fetches the latest tag for an OCI artifact stored in Artifactory.
// It uses the Artifactory Docker Registry API v2 to list available tags.
func getLatestArtifactoryTagReal(config *Config) (string, error) {
	// Extract the registry and repository path from the image base.
	// The imageBase might include a tag, so we strip it first.
	imageBase := config.ImageBase
	if strings.Contains(imageBase, ":") {
		imageBase = strings.Split(imageBase, ":")[0]
	}
	parts := strings.SplitN(imageBase, "/", 2) // Split only on the first slash to separate registry from path.
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid IMAGE_BASE format for Artifactory: %s", config.ImageBase)
	}

	registry := parts[0]
	repoPath := parts[1]

	// Construct the Artifactory Docker Registry API v2 endpoint for listing tags.
	apiURL := fmt.Sprintf("https://%s/v2/%s/tags/list", registry, repoPath)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set basic authentication credentials for Artifactory.
	req.SetBasicAuth(config.Username, config.Password)
	req.Header.Set("Accept", "application/json") // Request JSON response.

	client := &http.Client{Timeout: 30 * time.Second} // Set a timeout for the HTTP request.
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make API request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for non-200 status codes from the Artifactory API.
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artifactory API returned status %d: %s", resp.StatusCode, string(body))
	}

	var tagsResponse struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &tagsResponse); err != nil {
		return "", fmt.Errorf("failed to parse Artifactory API response: %w. Response body: %s", err, string(body))
	}

	if len(tagsResponse.Tags) == 0 {
		return "", nil // No tags found for the repository.
	}

	// Return the last tag in the list. This assumes Artifactory returns tags in a consistently ordered manner
	// where the last one is the most recent. For more robust semantic versioning, custom sorting might be needed.
	latestTag := tagsResponse.Tags[len(tagsResponse.Tags)-1]
	log.Printf("Found latest Artifactory tag: %s from %d available tags", latestTag, len(tagsResponse.Tags))

	return latestTag, nil
}

// pullImageToDirReal handles the actual pulling of the OCI artifact and extracts its contents to a local directory.
// It supports different pulling mechanisms based on the configured provider.
func pullImageToDirReal(config *Config, tag, destDir string) (map[string]string, error) {
	// Clean up any previous extraction in the destination directory to ensure a fresh pull.
	if err := os.RemoveAll(destDir); err != nil {
		log.Printf("Warning: failed to remove directory %s: %v", destDir, err)
	}
	// Create the destination directory.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	// Use ORAS for Artifactory due to specific authentication and registry API requirements.
	if config.Provider == ProviderArtifactory {
		// Construct the full image reference (e.g., registry/repo/image:tag).
		// We ensure the base image name is without a tag before concatenating, to prevent duplicate tags.
		imageBase := config.ImageBase
		if strings.Contains(imageBase, ":") {
			imageBase = strings.Split(imageBase, ":")[0]
		}
		imageRef := fmt.Sprintf("%s:%s", imageBase, tag)
		log.Printf("Pulling image %s into %s using oras...\n", imageRef, destDir)

		// Create a temporary config with the full image reference. This might be redundant now,
		// but historically ORAS pull operations might have needed this.
		configWithTag := *config
		configWithTag.ImageBase = imageRef

		if err := pullWithOras(&configWithTag, destDir); err != nil {
			return nil, fmt.Errorf("oras pull failed: %w", err)
		}
	} else {
		// For other providers (primarily GitHub Container Registry), use the go-containerregistry OCI library.
		log.Printf("Pulling image %s:%s into %s ...\n", config.ImageBase, tag, destDir)

		// Construct the full image reference, ensuring the base image name is without a tag first.
		imageBase := config.ImageBase
		if strings.Contains(imageBase, ":") {
			imageBase = strings.Split(imageBase, ":")[0]
		}
		imageRef := fmt.Sprintf("%s:%s", imageBase, tag)
		ctx := context.Background()

		if err := pullOCI(ctx, imageRef, destDir); err != nil {
			return nil, fmt.Errorf("OCI pull failed: %w", err)
		}
	}

	// After pulling, process the downloaded YAML manifests.
	// This involves adding labels (like managed-by, policy-version, artifact-name, policy-checksum)
	// and calculating checksums for reconciliation.
	files, err := findYAMLFiles(destDir)
	if err != nil {
		return nil, err
	}

	manifestChecksums := make(map[string]string)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Printf("Warning: failed to read file %s for checksum calculation: %v\n", f, err)
			continue
		}

		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(data, &obj); err != nil {
			log.Printf("Warning: could not unmarshal yaml for %s: %v", f, err)
			continue
		}

		var checksum string
		// Extract checksum from the 'spec' field if available to avoid changes in metadata
		// triggering unnecessary updates. Fallback to full content checksum if spec is not found.
		spec, found, err := unstructured.NestedFieldNoCopy(obj.Object, "spec")
		if !found || err != nil {
			log.Printf("Warning: 'spec' field not found or error in %s, falling back to full content checksum. %v\n", f, err)
			checksum = calculateSHA256(data)
		} else {
			specBytes, err := json.Marshal(spec)
			if err != nil {
				log.Printf("Warning: could not marshal spec for %s: %v", f, err)
				continue
			}
			checksum = calculateSHA256(specBytes)
		}
		manifestChecksums[f] = checksum[:48] // Store first 48 chars of SHA256

		// Add standard labels to the manifest for tracking and garbage collection.
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels["managed-by"] = "kyverno-watcher"
		labels["policy-version"] = tag
		if config.ArtifactName != "" {
			labels["artifact-name"] = config.ArtifactName
		}
		labels["policy-checksum"] = checksum[:48]
		obj.SetLabels(labels)

		// Marshal the updated manifest back to YAML and write it to disk.
		updatedData, err := yaml.Marshal(&obj)
		if err != nil {
			log.Printf("Warning: could not marshal updated yaml for %s: %v", f, err)
			continue
		}

		if err := os.WriteFile(f, updatedData, 0644); err != nil {
			log.Printf("Warning: failed to write updated manifest to %s: %v\n", f, err)
			continue
		}
	}

	return manifestChecksums, nil
}

// pullWithOras is a wrapper for orasPullFunc (used for testing).
func pullWithOras(config *Config, destDir string) error {
	return orasPullFunc(config, destDir)
}

// orasPull pulls an OCI artifact from a registry using the ORAS library.
// This is primarily used for Artifactory due to its specific authentication requirements.
func orasPull(config *Config, destDir string) error {
	log.Printf("Pulling %s to %s using ORAS library\n", config.ImageBase, destDir)

	ctx := context.Background()

	// Create a file store where the pulled artifact layers will be extracted.
	fs, err := file.New(destDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			log.Printf("Warning: failed to close file store: %v", err)
		}
	}()

	// The reference includes the registry, repository, and tag/digest.
	ref := config.ImageBase

	// Create an ORAS remote repository client.
	repo, err := orasremote.NewRepository(ref)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Set up authentication for the ORAS client using the provided username and password.
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: func(ctx context.Context, registry string) (auth.Credential, error) {
			return auth.Credential{
				Username: config.Username,
				Password: config.Password,
			}, nil
		},
	}

	// Extract the tag from the image reference.
	tag := ref
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		tag = ref[idx+1:]
	}

	// Copy the artifact from the remote repository to the local file store.
	copyOpts := oras.DefaultCopyOptions
	copyOpts.Concurrency = 1 // Process layers sequentially.

	_, err = oras.Copy(ctx, repo, tag, fs, tag, copyOpts)
	if err != nil {
		return fmt.Errorf("failed to pull artifact: %w", err)
	}

	log.Printf("Successfully pulled artifact to %s\n", destDir)

	// Log the files that were actually downloaded for debugging purposes.
	files, err := findYAMLFiles(destDir)
	if err != nil {
		log.Printf("Warning: error listing files after pull: %v", err)
	} else {
		log.Printf("Found %d YAML file(s) in %s after pull", len(files), destDir)
		for _, f := range files {
			log.Printf("  - %s", f)
		}
	}

	return nil
}

// pullOCI pulls an OCI image and extracts its layers using the go-containerregistry library.
// This is primarily used for GitHub Container Registry (GHCR).
func pullOCI(ctx context.Context, imageRef, outputDir string) error {
	// Parse the image reference string into a structured object.
	// This step validates the format of the image reference.
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("parsing image reference: %w", err)
	}

	log.Printf("Pulling files from OCI image: %s\n", ref.Name())

	// Pull the image's descriptor using the default keychain for authentication.
	// The default keychain automatically uses Docker credentials if available.
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("getting remote image: %w", err)
	}

	// Convert the image descriptor into a full image object.
	img, err := desc.Image()
	if err != nil {
		return fmt.Errorf("converting to image: %w", err)
	}

	// Retrieve all layers from the OCI image. Each layer typically contains a part of the artifact.
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("getting image layers: %w", err)
	}

	log.Printf("Found %d layers\n", len(layers))

	// Process each layer, extracting its content to the specified output directory.
	fileCount := 0
	for i, layer := range layers {
		if err := processLayer(layer, outputDir, i, &fileCount); err != nil {
			return fmt.Errorf("processing layer %d: %w", i, err)
		}
	}

	if fileCount == 0 {
		log.Println("Warning: No files were extracted from the image")
	} else {
		log.Printf("Successfully pulled %d file(s)\n", fileCount)
	}

	return nil
}

// processLayer extracts the content of a single OCI layer and saves it to a file.
// It tries to determine if the layer contains a policy and names the file accordingly.
func processLayer(layer v1.Layer, outputDir string, layerIndex int, fileCount *int) error {
	// Determine the media type of the layer, which can hint at its content (e.g., a policy layer).
	mediaType, err := layer.MediaType()
	if err != nil {
		return fmt.Errorf("getting media type: %w", err)
	}

	log.Printf("Layer %d media type: %s\n", layerIndex, mediaType)

	// Get the compressed content of the layer.
	blob, err := layer.Compressed()
	if err != nil {
		return fmt.Errorf("getting compressed layer: %w", err)
	}
	defer func() {
		if cerr := blob.Close(); cerr != nil {
			log.Printf("Warning: failed to close blob for layer %d: %v\n", layerIndex, cerr)
		}
	}()

	// Read all content from the layer's blob.
	content, err := io.ReadAll(blob)
	if err != nil {
		return fmt.Errorf("reading layer content: %w", err)
	}

	if len(content) == 0 {
		log.Printf("  Layer %d is empty, skipping\n", layerIndex)
		return nil
	}

	// Construct a filename for the extracted content.
	filename := filepath.Join(outputDir, fmt.Sprintf("layer-%d.yaml", layerIndex))

	// If the layer is identified as a policy layer, use a more descriptive filename.
	if mediaType == PolicyLayerMediaType {
		filename = filepath.Join(outputDir, fmt.Sprintf("policy-%d.yaml", layerIndex))
	}

	// Write the layer's content to the file system.
	if err := os.WriteFile(filename, content, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	log.Printf("  Saved to: %s (%d bytes)\n", filepath.Base(filename), len(content))
	// Increment the counter for successfully extracted files.
	*fileCount++

	return nil
}

// applyManifests is a wrapper for applyManifestsFunc (used for testing).
func applyManifests(config *Config, files []string, mapper meta.RESTMapper, dynamicClient dynamic.Interface) error {
	return applyManifestsFunc(config, files, mapper, dynamicClient)
}

// applyManifestsReal iterates through a list of YAML files and applies each one to the Kubernetes cluster.
// It logs the application status for each file.
func applyManifestsReal(config *Config, files []string, mapper meta.RESTMapper, dynamicClient dynamic.Interface) error {
	if len(files) == 0 {
		log.Printf("No YAML manifests found to apply\n")
		return nil
	}

	log.Printf("Applying %d manifests ...\n", len(files))

	for _, f := range files {
		log.Printf("Applying %s\n", f)
		if err := applyManifestFile(f, dynamicClient, mapper); err != nil {
			log.Printf("Failed to apply %s: %v\n", f, err)
			// Continue with other files even if one fails, to ensure as many policies as possible are applied.
			continue
		}
		log.Printf("Successfully applied %s\n", f)
	}

	return nil
}

// applyManifestFile reads a YAML file and applies its content(s) to the Kubernetes cluster.
// It supports multi-document YAML files (where documents are separated by '---').
func applyManifestFile(filePath string, dynamicClient dynamic.Interface, mapper meta.RESTMapper) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = f.Close() // Close the file when the function exits.
	}()

	// Use a YAML or JSON decoder to handle different input formats.
	decoder := k8syaml.NewYAMLOrJSONDecoder(f, 4096)
	docIndex := 0

	// Iterate through each document in the (potentially multi-document) YAML file.
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break // End of file, no more documents.
			}
			return fmt.Errorf("failed to decode YAML document %d: %w", docIndex, err)
		}

		// Skip empty documents (e.g., documents with only comments or whitespace).
		if len(obj.Object) == 0 {
			docIndex++
			continue
		}

		// Apply the current Kubernetes resource (document) to the cluster.
		if err := applyResource(obj, dynamicClient, mapper); err != nil {
			return fmt.Errorf("failed to apply document %d: %w", docIndex, err)
		}

		docIndex++
	}

	return nil
}

// applyResource applies a single unstructured Kubernetes resource (e.g., a Policy or ClusterPolicy) to the cluster.
// It handles both creation and updates, and correctly identifies whether a resource is namespaced or cluster-scoped.
func applyResource(obj *unstructured.Unstructured, dynamicClient dynamic.Interface, mapper meta.RESTMapper) error {
	// Use the Kubernetes REST mapper to get the GroupVersionResource (GVR) for the object.
	// The GVR is needed to interact with the dynamic client and correctly pluralize resource names.
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("failed to get REST mapping for %s (CRD may not be installed): %w", gvk.String(), err)
	}
	gvr := mapping.Resource

	ctx := context.Background()
	namespace := obj.GetNamespace()

	// Determine if the resource is cluster-scoped or namespaced based on its REST mapping.
	// This is important because some resources (like ClusterPolicies) might have a namespace field
	// in their YAML but are inherently cluster-scoped in Kubernetes.
	isNamespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace

	// If a resource is cluster-scoped but its YAML manifest specifies a namespace,
	// remove the namespace field to prevent validation errors in Kubernetes.
	if !isNamespaced && namespace != "" {
		log.Printf("Warning: %s/%s is cluster-scoped but has namespace '%s' - removing namespace field\n",
			gvk.Kind, obj.GetName(), namespace)
		obj.SetNamespace("")
		namespace = "" // Clear the namespace for dynamic client operations.
	}

	// Attempt to get the existing resource from the cluster.
	// If it exists, we'll update it; otherwise, we'll create it.
	var existing *unstructured.Unstructured
	if isNamespaced && namespace != "" {
		// Handle namespaced resources: scope the Get/Create/Update operation to the specified namespace.
		existing, err = dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		// Handle cluster-scoped resources: perform Get/Create/Update at the cluster level.
		existing, err = dynamicClient.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if err != nil && errors.IsNotFound(err) {
		// Resource does not exist, so create it.
		if isNamespaced && namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
	} else if err != nil {
		// An unexpected error occurred while trying to fetch the resource.
		return fmt.Errorf("failed to get existing resource: %w", err)
	} else {
		// Resource already exists, so update it.
		// It's crucial to set the ResourceVersion from the existing object to prevent conflicts.
		obj.SetResourceVersion(existing.GetResourceVersion())
		if isNamespaced && namespace != "" {
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
		} else {
			_, err = dynamicClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	}

	return nil
}

func findYAMLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".yaml" || ext == ".yml" {
				files = append(files, path)
			}
		}
		return nil
	})
	return files, err
}

func sanitizePath(s string) string {
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}
