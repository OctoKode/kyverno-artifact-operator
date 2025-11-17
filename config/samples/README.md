# Sample Configurations

This directory contains example configurations for the Kyverno Artifact Operator.

## Quick Start Samples

- `kyverno_v1alpha1_kyvernoartifact.yaml` - Basic GitHub Container Registry example
- `kyverno_v1alpha1_kyvernoartifact_artifactory.yaml` - Artifactory example

## Configuration Samples

- `helm-values-example.yaml` - Example Helm values for customizing the operator
- `manager_config_patch.yaml` - Kustomize patch for environment variable configuration

## Configurable Environment Variables

The following environment variables can be set on the controller deployment:

| Variable | Default | Description |
|----------|---------|-------------|
| `WATCHER_IMAGE` | `ghcr.io/octokode/kyverno-artifact-operator:latest` | Watcher container image (same as operator) |
| `WATCHER_SERVICE_ACCOUNT` | `kyverno-artifact-operator-watcher` | Service account for watcher pods |
| `WATCHER_SECRET_NAME` | `kyverno-watcher-secret` | Secret containing credentials |
| `GITHUB_TOKEN_KEY` | `github-token` | Secret key for GitHub token |
| `ARTIFACTORY_USERNAME_KEY` | `artifactory-username` | Secret key for Artifactory username |
| `ARTIFACTORY_PASSWORD_KEY` | `artifactory-password` | Secret key for Artifactory password |

For detailed configuration documentation, see [../../docs/configuration.md](../../docs/configuration.md).
