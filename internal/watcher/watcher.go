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
)

// Run starts the artifact watcher
func Run(version string) {
	Version = version
	// Print version
	log.Printf("Kyverno Artifact Watcher version %s\n", Version)

	config := loadConfig()
	log.Printf("Using configuration %+v\n", config)

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

	for {
		if err := watchLoop(config); err != nil {
			log.Printf("Error in watch loop: %v\n", err)
		}
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

func watchLoop(config *Config) error {
	isTagChanged, latest, prevTag, err := tagChangedFunc(config)
	if err != nil {
		return fmt.Errorf("error checking for tag change: %w", err)
	}

	// Exit early if nothing has changed and checksum reconciliation is disabled
	if !isTagChanged && !config.ReconcilePoliciesFromChecksum {
		log.Printf("No change (latest=%s)\n", latest)
		return nil
	}

	appliedSomething := false

	dynamicClient, mapper, err := getKubernetesClientsFunc()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes clients: %w", err)
	}

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
		log.Printf("No tag change, but checksum reconciliation is enabled. Checking manifests.\n")
		destDir := fmt.Sprintf("/tmp/image-%s", sanitizePath(latest))

		newChecksums, err := pullImageToDirFunc(config, latest, destDir)
		if err != nil {
			return fmt.Errorf("pull failed: %w", err)
		}

		changed, filesToApply, err := checksumsChangedFunc(newChecksums, dynamicClient, mapper)
		if err != nil {
			log.Printf("Error during checksum comparison: %v", err)
		}

		if changed {
			if err := applyManifestsFunc(config, filesToApply, mapper, dynamicClient); err != nil {
				return fmt.Errorf("apply manifests failed: %w", err)
			}
			appliedSomething = true
		} else {
			log.Println("All policies are up to date, no manifests to apply.")
		}
	}

	if appliedSomething {
		if err := os.WriteFile(config.LastFile, []byte(latest), 0644); err != nil {
			return fmt.Errorf("failed to write last file: %w", err)
		}
	}

	return nil
}

func getLatestTagOrDigest(config *Config) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/%s/%s/packages/container/%s/versions",
		config.GithubAPIOwnerType, config.Owner, config.PackageNormalized)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+config.GithubToken)
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

	// Check for non-200 status codes
	if resp.StatusCode != http.StatusOK {
		var errMsg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &errMsg)

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
		return "", nil
	}

	// Find the most recently updated version
	latest := versions[0]
	for _, v := range versions {
		if v.UpdatedAt.After(latest.UpdatedAt) {
			latest = v
		}
	}

	// Prefer tag names if present
	if len(latest.Metadata.Container.Tags) > 0 {
		return latest.Metadata.Container.Tags[0], nil
	}

	// Fallback to version ID
	return fmt.Sprintf("version-id-%d", latest.ID), nil
}

func getLatestArtifactoryTag(config *Config) (string, error) {
	// Parse the image base to extract registry, repository path
	// Expected format: registry.example.com/repo/path or registry.example.com/repo/path:tag
	imageBase := strings.Split(config.ImageBase, ":")[0]
	parts := strings.SplitN(imageBase, "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid IMAGE_BASE format for Artifactory: %s", config.ImageBase)
	}

	registry := parts[0]
	repoPath := parts[1]

	// Artifactory Docker Registry API v2 endpoint to list tags
	apiURL := fmt.Sprintf("https://%s/v2/%s/tags/list", registry, repoPath)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(config.Username, config.Password)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
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
		return "", nil
	}

	// Return the last tag in the list (typically the most recent)
	// For semantic versioning, you might want to add sorting logic here
	latestTag := tagsResponse.Tags[len(tagsResponse.Tags)-1]
	log.Printf("Found latest Artifactory tag: %s from %d available tags", latestTag, len(tagsResponse.Tags))

	return latestTag, nil
}

//nolint:unused // Used via pullImageToDirFunc for testing
func pullImageToDir(config *Config, tag, destDir string) (map[string]string, error) {
	return pullImageToDirFunc(config, tag, destDir)
}

func pullImageToDirReal(config *Config, tag, destDir string) (map[string]string, error) {
	if err := os.RemoveAll(destDir); err != nil {
		log.Printf("Warning: failed to remove directory %s: %v", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	if config.Provider == ProviderArtifactory {
		// Construct full image reference with tag
		imageBase := strings.Split(config.ImageBase, ":")[0]
		imageRef := fmt.Sprintf("%s:%s", imageBase, tag)
		log.Printf("Pulling image %s into %s using oras...\n", imageRef, destDir)

		// Create a temporary config with the full image reference
		configWithTag := *config
		configWithTag.ImageBase = imageRef

		if err := pullWithOras(&configWithTag, destDir); err != nil {
			return nil, fmt.Errorf("oras pull failed: %w", err)
		}
	} else {
		log.Printf("Pulling image %s:%s into %s ...\n", config.ImageBase, tag, destDir)

		// Pull using OCI library
		imageRef := fmt.Sprintf("%s:%s", config.ImageBase, tag)
		ctx := context.Background()

		if err := pullOCI(ctx, imageRef, destDir); err != nil {
			return nil, fmt.Errorf("OCI pull failed: %w", err)
		}
	}

	// Add labels to manifests and calculate checksums
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
		manifestChecksums[f] = checksum[:48]

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

func pullWithOras(config *Config, destDir string) error {
	return orasPullFunc(config, destDir)
}

func orasPull(config *Config, destDir string) error {
	log.Printf("Pulling %s to %s using ORAS library\n", config.ImageBase, destDir)

	ctx := context.Background()

	// Create file store for the destination
	fs, err := file.New(destDir)
	if err != nil {
		return fmt.Errorf("failed to create file store: %w", err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			log.Printf("Warning: failed to close file store: %v", err)
		}
	}()

	// Parse the image reference to get tag
	ref := config.ImageBase

	// Create repository
	repo, err := orasremote.NewRepository(ref)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Set up authentication with static credentials
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

	// Get the tag from the reference
	tag := ref
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		tag = ref[idx+1:]
	}

	// Copy from repository to file store
	copyOpts := oras.DefaultCopyOptions
	copyOpts.Concurrency = 1

	_, err = oras.Copy(ctx, repo, tag, fs, tag, copyOpts)
	if err != nil {
		return fmt.Errorf("failed to pull artifact: %w", err)
	}

	log.Printf("Successfully pulled artifact to %s\n", destDir)

	// List what was actually downloaded for debugging
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

func pullOCI(ctx context.Context, imageRef, outputDir string) error {
	// Parse the image reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("parsing image reference: %w", err)
	}

	log.Printf("Pulling files from OCI image: %s\n", ref.Name())

	// Pull the image using default keychain (uses Docker credentials if available)
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("getting remote image: %w", err)
	}

	img, err := desc.Image()
	if err != nil {
		return fmt.Errorf("converting to image: %w", err)
	}

	// Get image layers
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("getting image layers: %w", err)
	}

	log.Printf("Found %d layers\n", len(layers))

	// Process each layer
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

func processLayer(layer v1.Layer, outputDir string, layerIndex int, fileCount *int) error {
	// Get layer media type
	mediaType, err := layer.MediaType()
	if err != nil {
		return fmt.Errorf("getting media type: %w", err)
	}

	log.Printf("Layer %d media type: %s\n", layerIndex, mediaType)

	// Get layer content
	blob, err := layer.Compressed()
	if err != nil {
		return fmt.Errorf("getting compressed layer: %w", err)
	}
	defer func() {
		if cerr := blob.Close(); cerr != nil {
			log.Printf("Warning: failed to close blob for layer %d: %v\n", layerIndex, cerr)
		}
	}()

	// Read the layer content
	content, err := io.ReadAll(blob)
	if err != nil {
		return fmt.Errorf("reading layer content: %w", err)
	}

	if len(content) == 0 {
		log.Printf("  Layer %d is empty, skipping\n", layerIndex)
		return nil
	}

	// Save layer content to file
	filename := filepath.Join(outputDir, fmt.Sprintf("layer-%d.yaml", layerIndex))

	// If it's a policy layer, try to give it a better name
	if mediaType == PolicyLayerMediaType {
		filename = filepath.Join(outputDir, fmt.Sprintf("policy-%d.yaml", layerIndex))
	}

	if err := os.WriteFile(filename, content, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	log.Printf("  Saved to: %s (%d bytes)\n", filepath.Base(filename), len(content))
	*fileCount++

	return nil
}

//nolint:unused // Used via applyManifestsFunc for testing
func applyManifests(config *Config, files []string, mapper meta.RESTMapper, dynamicClient dynamic.Interface) error {
	return applyManifestsFunc(config, files, mapper, dynamicClient)
}

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
			// Continue with other files even if one fails
			continue
		}
		log.Printf("Successfully applied %s\n", f)
	}

	return nil
}

// applyManifestFile reads a YAML file and applies it to the cluster.
// It supports multi-document YAML files (documents separated by ---).
func applyManifestFile(filePath string, dynamicClient dynamic.Interface, mapper meta.RESTMapper) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	decoder := k8syaml.NewYAMLOrJSONDecoder(f, 4096)
	docIndex := 0

	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode YAML document %d: %w", docIndex, err)
		}

		// Skip empty documents (e.g., documents with only comments or whitespace)
		if len(obj.Object) == 0 {
			docIndex++
			continue
		}

		if err := applyResource(obj, dynamicClient, mapper); err != nil {
			return fmt.Errorf("failed to apply document %d: %w", docIndex, err)
		}

		docIndex++
	}

	return nil
}

// applyResource applies a single unstructured resource to the cluster
func applyResource(obj *unstructured.Unstructured, dynamicClient dynamic.Interface, mapper meta.RESTMapper) error {
	// Get GVR from the object using the REST mapper for proper pluralization
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("failed to get REST mapping for %s (CRD may not be installed): %w", gvk.String(), err)
	}
	gvr := mapping.Resource

	ctx := context.Background()
	namespace := obj.GetNamespace()

	// Determine if resource is cluster-scoped or namespaced based on the REST mapping
	// Some resources like ClusterPolicy have namespace in their YAML but are actually cluster-scoped
	isNamespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace

	// If resource is cluster-scoped, remove namespace field if present
	if !isNamespaced && namespace != "" {
		log.Printf("Warning: %s/%s is cluster-scoped but has namespace '%s' - removing namespace field\n",
			gvk.Kind, obj.GetName(), namespace)
		obj.SetNamespace("")
		namespace = ""
	}

	// Try to create or update the resource
	if isNamespaced && namespace != "" {
		// Namespaced resource
		existing, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			// Resource doesn't exist, create it
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create resource: %w", err)
			}
		} else {
			// Resource exists, update it
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, err = dynamicClient.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update resource: %w", err)
			}
		}
	} else {
		// Cluster-scoped resource
		existing, err := dynamicClient.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			// Resource doesn't exist, create it
			_, err = dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create resource: %w", err)
			}
		} else {
			// Resource exists, update it
			obj.SetResourceVersion(existing.GetResourceVersion())
			_, err = dynamicClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update resource: %w", err)
			}
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
