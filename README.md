# kyverno-artifact-operator

Kubernetes operator that automatically syncs Kyverno policies from OCI artifacts (like GitHub Container Registry) to your cluster.

## Description

The Kyverno Artifact Operator watches OCI artifacts for changes and automatically applies Kyverno policies to your Kubernetes cluster. When you push a new version of your policy artifact, the operator detects the change, pulls the new policies, and applies them to your cluster.

This repository combines both the operator and watcher functionality into a single binary that can operate in three modes:
- **Operator mode** (default): Manages KyvernoArtifact custom resources and deploys watcher pods
- **Watcher mode** (`-watcher` flag): Continuously monitors OCI registries for policy updates
- **Garbage Collector mode** (`gc` or `--garbage-collect` flag): Cleans up orphaned policies

## Quick Start

### 1. Install the Operator

```bash
kubectl apply -f https://raw.githubusercontent.com/OctoKode/kyverno-artifact-operator/main/dist/install.yaml
```

### 2. Create a Secret for Your Registry

#### For GitHub Container Registry (GHCR):

```bash
kubectl create secret generic kyverno-watcher-secret \
  --from-literal=github-token=ghp_YOUR_GITHUB_TOKEN_HERE \
  --namespace=default
```

**Note:** Your GitHub token needs `read:packages` permission.

#### For Artifactory:

```bash
kubectl create secret generic kyverno-watcher-secret \
  --from-literal=artifactory-username=YOUR_USERNAME \
  --from-literal=artifactory-password=YOUR_PASSWORD \
  --namespace=default
```

### 3. Create a KyvernoArtifact Resource

#### For GitHub Container Registry:

```yaml
apiVersion: kyverno.octokode.io/v1alpha1
kind: KyvernoArtifact
metadata:
  name: my-policies
spec:
  # url can include a specific tag to start from (e.g., ghcr.io/YOUR_ORG/YOUR_POLICIES:v1.0.0).
  # If no tag is specified, 'latest' is used.
  url: ghcr.io/YOUR_ORG/YOUR_POLICIES:latest
  type: oci
  provider: github
  # pollingInterval is the interval in seconds to check for new tags.
  # Set to 0 to disable polling and only sync the tag specified in the url.
  pollingInterval: 60
  # deletePoliciesOnTermination specifies whether policies created by this artifact should be automatically deleted when the watcher pod terminates.
  # Defaults to false. Can be overridden by the WATCHER_DELETE_POLICIES_ON_TERMINATION environment variable.
  # +optional
  deletePoliciesOnTermination: true
  # reconcilePoliciesFromChecksum enables or disables policy reconciliation based on checksums.
  # When enabled, policies will be updated even if their version tag has not changed, but their content (SHA256 checksum) has.
  # Defaults to false. Can be overridden by the WATCHER_CHECKSUM_RECONCILIATION_ENABLED environment variable.
  # +optional
  reconcilePoliciesFromChecksum: false
  # pollForTagChanges enables or disables polling for new image tags.
  # If set to false, the watcher will only use the specific tag provided in the 'url' field and will not look for newer tags.
  # This is useful for pinning to a specific version while still benefiting from checksum-based reconciliation.
  # Defaults to true.
  # +optional
  pollForTagChanges: true
```

#### For Artifactory:

```yaml
apiVersion: kyverno.octokode.io/v1alpha1
kind: KyvernoArtifact
metadata:
  name: my-artifactory-policies
spec:
  # url can include a specific tag to start from (e.g., artifactory.example.com/docker-local/policies:v1.0.0).
  # If no tag is specified, 'latest' is used.
  url: artifactory.example.com/docker-local/policies:latest
  type: oci
  provider: artifactory
  # pollingInterval is the interval in seconds to check for new tags.
  # Set to 0 to disable polling and only sync the tag specified in the url.
  pollingInterval: 60
  # deletePoliciesOnTermination specifies whether policies created by this artifact should be automatically deleted when the watcher pod terminates.
  # Defaults to false. Can be overridden by the WATCHER_DELETE_POLICIES_ON_TERMINATION environment variable.
  # +optional
  deletePoliciesOnTermination: true
  # reconcilePoliciesFromChecksum enables or disables policy reconciliation based on checksums.
  # When enabled, policies will be updated even if their version tag has not changed, but their content (SHA256 checksum) has.
  # Defaults to false. Can be overridden by the WATCHER_CHECKSUM_RECONCILIATION_ENABLED environment variable.
  # +optional
  reconcilePoliciesFromChecksum: false
  # pollForTagChanges enables or disables polling for new image tags.
  # If set to false, the watcher will only use the specific tag provided in the 'url' field and will not look for newer tags.
  # This is useful for pinning to a specific version while still benefiting from checksum-based reconciliation.
  # Defaults to true.
  # +optional
  pollForTagChanges: true
```

```bash
kubectl apply -f my-artifact.yaml
```

## Troubleshooting

### "no matches for kyverno.octokode.io/v1alpha1" Error

If you see this error when running the operator:
```
unable to retrieve the complete list of server APIs: kyverno.octokode.io/v1alpha1: no matches for kyverno.octokode.io/v1alpha1
```

**Solution:** Install the CRDs first:
```bash
make install
```

Or use `make run` which automatically installs CRDs before starting the operator.

### 4. Verify

```bash
# Check the watcher pod
kubectl get pods -l app=kyverno-artifact-manager-my-policies

# View logs
kubectl logs -f kyverno-artifact-manager-my-policies

# Check applied policies
kubectl get clusterpolicies
```

For detailed troubleshooting, see [config/samples/README.md](config/samples/README.md).

## Configuration

See [docs/configuration.md](docs/configuration.md) for detailed configuration options.
**Quick example** - Configure custom secret name:

```bash
kubectl set env deployment/kyverno-artifact-operator-controller-manager \
  -n kyverno-artifact-operator-system \
  WATCHER_SECRET_NAME=my-custom-secret
```

**Quick example** - Enable checksum-based policy reconciliation:

```bash
kubectl set env deployment/kyverno-artifact-operator-controller-manager \
  -n kyverno-artifact-operator-system \
  WATCHER_CHECKSUM_RECONCILIATION_ENABLED=true
```

## Getting Started

### Binary Modes

The `manager` binary can run in three modes:

```bash
# Operator mode (default) - manages KyvernoArtifact CRDs
./bin/manager

# Watcher mode - directly monitors OCI registry for changes
./bin/manager -watcher

# Garbage Collector mode - cleans up orphaned policies
./bin/manager gc
# or
./bin/manager --garbage-collect
```

#### Watcher Mode

When running in watcher mode, the binary requires these environment variables:
- `IMAGE_BASE`: OCI image reference (e.g., `ghcr.io/owner/package`)
- `PROVIDER`: Registry provider (`github` or `artifactory`)
- `GITHUB_TOKEN`: GitHub token (for GitHub provider)
- `ARTIFACTORY_USERNAME` and `ARTIFACTORY_PASSWORD`: Credentials (for Artifactory provider)
- `POLL_INTERVAL`: Poll interval in seconds (default: 30)
- `WATCHER_DELETE_POLICIES_ON_TERMINATION`: If set to "true", policies will be deleted on watcher termination (default: false)
- `WATCHER_CHECKSUM_RECONCILIATION_ENABLED`: If set to "true", enables reconciliation of policies based on content checksums (default: false)
- `WATCHER_POLL_FOR_TAG_CHANGES_ENABLED`: If set to "false", disables polling for new tags and uses the tag specified in `IMAGE_BASE` (default: true)

#### Garbage Collector Mode

The garbage collector mode cleans up orphaned Kyverno policies that have the `managed-by: kyverno-watcher` label but no longer have a corresponding KyvernoArtifact or watcher pod. This is useful for cleaning up policies after deleting KyvernoArtifact resources.

Configuration:
- `POLL_INTERVAL`: Poll interval in seconds between garbage collection cycles (default: 30)

The garbage collector will:
1. Find all Policy and ClusterPolicy resources with `managed-by=kyverno-watcher` label
2. Check if there are any active KyvernoArtifact resources
3. Check if there are any active watcher pods
4. Delete policies that are orphaned (no KyvernoArtifact or watcher pod exists)
5. Sleep for the configured polling interval and repeat
