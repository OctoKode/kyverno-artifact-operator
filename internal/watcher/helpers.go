package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/OctoKode/kyverno-artifact-operator/internal/k8s"
	"github.com/google/go-containerregistry/pkg/name"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"
)

// calculateSHA256 returns the SHA256 checksum of the given data as a hexadecimal string.
func calculateSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// checksumsChanged compares the checksums of freshly pulled manifests against
// the versions in the cluster. It returns true if any manifest is new or has changed,
// along with a list of files that need to be applied.
func checksumsChanged(newChecksums map[string]string, dynamicClient dynamic.Interface, mapper meta.RESTMapper) (bool, []string, error) {
	var filesToApply []string
	changed := false

	for file, newChecksum := range newChecksums {
		fileContent, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Warning: failed to read file %s for checksum comparison: %v\n", file, err)
			continue
		}
		var manifest unstructured.Unstructured
		if err := yaml.Unmarshal(fileContent, &manifest); err != nil {
			log.Printf("Warning: failed to unmarshal YAML from %s: %v\n", file, err)
			continue
		}

		gvk := manifest.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			log.Printf("Warning: failed to get REST mapping for %s: %v\n", gvk.String(), err)
			continue
		}

		var existingPolicy *unstructured.Unstructured
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			existingPolicy, err = dynamicClient.Resource(mapping.Resource).Namespace(manifest.GetNamespace()).Get(context.Background(), manifest.GetName(), metav1.GetOptions{})
		} else {
			existingPolicy, err = dynamicClient.Resource(mapping.Resource).Get(context.Background(), manifest.GetName(), metav1.GetOptions{})
		}

		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				log.Printf("Policy %s/%s not found. Adding to apply list.\n", manifest.GetNamespace(), manifest.GetName())
				filesToApply = append(filesToApply, file)
				changed = true
			} else {
				log.Printf("Warning: failed to get existing policy %s/%s: %v\n", manifest.GetNamespace(), manifest.GetName(), err)
			}
			continue
		}

		existingSpec, found, err := unstructured.NestedFieldNoCopy(existingPolicy.Object, "spec")
		if !found || err != nil {
			log.Printf("Warning: 'spec' field not found in existing policy %s/%s. %v\n", manifest.GetNamespace(), manifest.GetName(), err)
			filesToApply = append(filesToApply, file)
			changed = true
			continue
		}

		existingSpecBytes, err := json.Marshal(existingSpec)
		if err != nil {
			log.Printf("Warning: failed to marshal spec for existing policy %s/%s: %v\n", manifest.GetNamespace(), manifest.GetName(), err)
			continue
		}
		existingChecksum := calculateSHA256(existingSpecBytes)[:48]

		if newChecksum != existingChecksum {
			log.Printf("Policy %s/%s content changed (old checksum: %s, new checksum: %s). Adding to apply list.\n", manifest.GetNamespace(), manifest.GetName(), existingChecksum, newChecksum)
			filesToApply = append(filesToApply, file)
			changed = true
		} else {
			log.Printf("Policy %s/%s unchanged (checksum: %s). Skipping.\n", manifest.GetNamespace(), manifest.GetName(), newChecksum)
		}
	}

	return changed, filesToApply, nil
}

// tagChanged checks if the artifact tag has changed since the last check.
// It returns true if the tag is new, the latest tag, the previous tag, and any error.
func tagChanged(config *Config) (bool, string, string, error) {
	// If polling is disabled, we just want to process the given tag once.
	if config.PollInterval == 0 {
		ref, err := name.ParseReference(config.ImageBase)
		if err != nil {
			return false, "", "", fmt.Errorf("invalid ImageBase: %w", err)
		}
		tag := ref.Identifier()

		// If no tag is specified in the URL ("latest" or empty), fetch the actual latest one.
		if tag == "latest" || tag == "" {
			var latest string
			var err error
			if config.Provider == ProviderGitHub {
				latest, err = getLatestTagOrDigestFunc(config)
			} else {
				latest, err = getLatestArtifactoryTagFunc(config)
			}
			if err != nil {
				return false, "", "", fmt.Errorf("could not determine latest tag: %w", err)
			}
			if latest == "" {
				log.Println("No versions found for package")
				return false, "", "", nil
			}
			// We always want to process this on a single run.
			return true, latest, "", nil
		}

		// A specific tag is in the URL, use it.
		// We always want to process this on a single run.
		return true, tag, "", nil
	}

	// Polling is enabled, use existing logic to check for changes.
	var latest string
	var err error

	if config.Provider == ProviderGitHub {
		latest, err = getLatestTagOrDigestFunc(config)
		if err != nil {
			return false, "", "", fmt.Errorf("could not determine latest tag/digest: %w", err)
		}

		if latest == "" {
			log.Println("No versions found for package")
			return false, "", "", nil
		}
	} else {
		// For artifactory, check if a specific tag is provided or look for latest
		parts := strings.Split(config.ImageBase, ":")
		if len(parts) >= 2 && parts[len(parts)-1] != "latest" {
			// User specified a specific tag/version, use it as-is
			latest = parts[len(parts)-1]
		} else {
			// No specific version or "latest" tag - query Artifactory for latest version
			latest, err = getLatestArtifactoryTagFunc(config)
			if err != nil {
				return false, "", "", fmt.Errorf("could not determine latest Artifactory tag: %w", err)
			}
			if latest == "" {
				log.Println("No versions found in Artifactory")
				return false, "", "", nil
			}
		}
	}

	prev, _ := os.ReadFile(config.LastFile)
	prevTag := strings.TrimSpace(string(prev))

	return latest != prevTag, latest, prevTag, nil
}

// getKubernetesClients initializes and returns the dynamic Kubernetes client and REST mapper.
func getKubernetesClients() (dynamic.Interface, meta.RESTMapper, error) {
	kubeConfig, err := k8s.GetConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	cachedClient := memory.NewMemCacheClient(discoveryClient)
	apiGroupResources, err := restmapper.GetAPIGroupResources(cachedClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get API group resources: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(apiGroupResources)

	return dynamicClient, mapper, nil
}
