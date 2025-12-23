package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ProviderGitHub      = "github"
	ProviderArtifactory = "artifactory"
)

var (
	// logFatal can be overridden in tests
	logFatal = func(v ...interface{}) {
		log.Fatal(v...)
	}
	// getEnvFunc can be overridden in tests
	getEnvFunc = os.Getenv
	// stateDirBase can be overridden in tests to avoid creating /tmp/kyverno-watcher
	stateDirBase = "/tmp/kyverno-watcher"
)

type Manifest struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                 `yaml:"kind" json:"kind"`
	Metadata   ManifestMetadata       `yaml:"metadata" json:"metadata"`
	Spec       map[string]interface{} `yaml:"spec,omitempty" json:"spec,omitempty"`
}

type ManifestMetadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type Config struct {
	GithubToken                 string
	ImageBase                   string
	Owner                       string
	Package                     string
	PackageNormalized           string
	PollInterval                int
	GithubAPIOwnerType          string
	StateDir                    string
	LastFile                    string
	Provider                    string
	Username                    string
	Password                    string
	ArtifactName                string // Name of the KyvernoArtifact resource that owns this watcher
	DeletePoliciesOnTermination bool   // Whether to delete policies on termination
}

type GitHubPackageVersion struct {
	ID        int64     `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Metadata  struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

func loadConfig() *Config {
	provider := strings.ToLower(getEnvOrDefault("PROVIDER", ProviderGitHub))

	var githubToken, username, password string
	var owner, packageName string

	imageBase := getEnvFunc("IMAGE_BASE")
	if imageBase == "" {
		logFatal("IMAGE_BASE environment variable must be set (e.g., ghcr.io/owner/package)")
	}

	switch provider {
	case ProviderGitHub:
		githubToken = strings.TrimSpace(getEnvFunc("GITHUB_TOKEN"))
		if githubToken == "" {
			logFatal("GITHUB_TOKEN environment variable must be set")
		}

		// Validate token format - GitHub tokens should only contain alphanumeric and underscores
		// Classic tokens start with ghp_, fine-grained with github_pat_
		// Remove any non-printable characters that might cause header issues
		githubToken = strings.Map(func(r rune) rune {
			if r < 32 || r > 126 {
				return -1 // Remove non-printable ASCII
			}
			return r
		}, githubToken)

		if githubToken == "" {
			logFatal("GITHUB_TOKEN contains only invalid characters")
		}

		// Log token prefix for debugging (don't log full token)
		tokenPrefix := githubToken
		if len(tokenPrefix) > 10 {
			tokenPrefix = tokenPrefix[:10] + "..."
		}
		log.Printf("Using GitHub token: %s (length: %d)\n", tokenPrefix, len(githubToken))

		// Parse IMAGE_BASE to extract owner and package
		// Expected format: ghcr.io/owner/package or ghcr.io/owner/package:tag
		var err error
		owner, packageName, err = parseImageBase(imageBase)
		if err != nil {
			logFatal(fmt.Sprintf("Failed to parse IMAGE_BASE: %v", err))
		}
	case ProviderArtifactory:
		username = strings.TrimSpace(getEnvFunc("ARTIFACTORY_USERNAME"))
		password = strings.TrimSpace(getEnvFunc("ARTIFACTORY_PASSWORD"))
		if username == "" || password == "" {
			logFatal("ARTIFACTORY_USERNAME and ARTIFACTORY_PASSWORD environment variables must be set for artifactory provider")
		}
		log.Printf("Using Artifactory with username: %s\n", username)
	default:
		logFatal(fmt.Sprintf("Unsupported PROVIDER: %s (must be 'github' or 'artifactory')", provider))
	}

	pollInterval := getEnvAsIntOrDefault("POLL_INTERVAL", 30)
	githubAPIOwnerType := getEnvOrDefault("GITHUB_API_OWNER_TYPE", "users")
	deletePoliciesOnTermination := getEnvAsBoolOrDefault("WATCHER_DELETE_POLICIES_ON_TERMINATION", false)

	// Get artifact name from pod name (format: kyverno-artifact-manager-{artifactName})
	// This is used to link policies back to their source KyvernoArtifact for garbage collection
	artifactName := getEnvFunc("ARTIFACT_NAME")
	if artifactName == "" {
		// Try to extract from hostname/pod name as fallback
		hostname := getEnvFunc("HOSTNAME")
		if strings.HasPrefix(hostname, "kyverno-artifact-manager-") {
			artifactName = strings.TrimPrefix(hostname, "kyverno-artifact-manager-")
		}
	}

	// Normalize package name for API path
	packageNormalized := strings.ReplaceAll(packageName, "/", "%2F")

	stateDir := stateDirBase
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		logFatal(fmt.Sprintf("Failed to create state directory: %v", err))
	}
	lastFile := filepath.Join(stateDir, "last_seen")

	return &Config{
		GithubToken:                 githubToken,
		ImageBase:                   imageBase,
		Owner:                       owner,
		Package:                     packageName,
		PackageNormalized:           packageNormalized,
		PollInterval:                pollInterval,
		GithubAPIOwnerType:          githubAPIOwnerType,
		StateDir:                    stateDir,
		LastFile:                    lastFile,
		Provider:                    provider,
		Username:                    username,
		Password:                    password,
		ArtifactName:                artifactName,
		DeletePoliciesOnTermination: deletePoliciesOnTermination,
	}
}

func getEnvAsBoolOrDefault(key string, defaultValue bool) bool {
	if value := getEnvFunc(key); value != "" {
		switch strings.ToLower(value) {
		case "t", "true", "1":
			return true
		default:
			return false
		}
	}
	return defaultValue
}

func parseImageBase(imageBase string) (owner, packageName string, err error) {
	// Remove tag if present (e.g., ghcr.io/owner/package:v0.0.1 -> ghcr.io/owner/package)
	imageBase = strings.Split(imageBase, ":")[0]

	// Expected format: ghcr.io/owner/package[/subpackage/...]
	parts := strings.Split(imageBase, "/")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("IMAGE_BASE must be in format ghcr.io/owner/package, got: %s", imageBase)
	}

	// parts[0] = ghcr.io
	// parts[1] = owner
	// parts[2:] = package parts
	owner = parts[1]
	packageName = strings.Join(parts[2:], "/")

	if owner == "" || packageName == "" {
		return "", "", fmt.Errorf("could not extract owner and package from IMAGE_BASE: %s", imageBase)
	}

	return owner, packageName, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := getEnvFunc(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsIntOrDefault(key string, defaultValue int) int {
	if value := getEnvFunc(key); value != "" {
		var intVal int
		if _, err := fmt.Sscanf(value, "%d", &intVal); err == nil {
			return intVal
		}
	}
	return defaultValue
}
