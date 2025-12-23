/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config holds configurable values for the controller
type Config struct {
	WatcherImage           string
	WatcherServiceAccount  string
	SecretName             string
	GitHubTokenKey         string
	ArtifactoryUsernameKey string
	ArtifactoryPasswordKey string
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		WatcherImage:           getEnvOrDefault("WATCHER_IMAGE", "ghcr.io/octokode/kyverno-artifact-operator:latest"),
		WatcherServiceAccount:  getEnvOrDefault("WATCHER_SERVICE_ACCOUNT", "kyverno-artifact-operator-watcher"),
		SecretName:             getEnvOrDefault("WATCHER_SECRET_NAME", "kyverno-watcher-secret"),
		GitHubTokenKey:         getEnvOrDefault("GITHUB_TOKEN_KEY", "github-token"),
		ArtifactoryUsernameKey: getEnvOrDefault("ARTIFACTORY_USERNAME_KEY", "artifactory-username"),
		ArtifactoryPasswordKey: getEnvOrDefault("ARTIFACTORY_PASSWORD_KEY", "artifactory-password"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// KyvernoArtifactReconciler reconciles a KyvernoArtifact object
type KyvernoArtifactReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config Config
}
