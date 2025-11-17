# kyverno-artifact-operator

Kubernetes operator that automatically syncs Kyverno policies from OCI artifacts (like GitHub Container Registry) to your cluster.

## Description

The Kyverno Artifact Operator watches OCI artifacts for changes and automatically applies Kyverno policies to your Kubernetes cluster. When you push a new version of your policy artifact, the operator detects the change, pulls the new policies, and applies them to your cluster.

This repository combines both the operator and watcher functionality into a single binary that can operate in two modes:
- **Operator mode** (default): Manages KyvernoArtifact custom resources and deploys watcher pods
- **Watcher mode** (`-watcher` flag): Continuously monitors OCI registries for policy updates

**Features:**
- Automatic policy synchronization from OCI registries
- Support for GitHub Container Registry (GHCR) and Artifactory
- Configurable polling intervals
- Secure token management via Kubernetes secrets
- Prometheus metrics for monitoring

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
  url: ghcr.io/YOUR_ORG/YOUR_POLICIES:latest
  type: oci
  provider: github
  pollingInterval: 60
```

#### For Artifactory:

```yaml
apiVersion: kyverno.octokode.io/v1alpha1
kind: KyvernoArtifact
metadata:
  name: my-artifactory-policies
spec:
  url: artifactory.example.com/docker-local/policies:latest
  type: oci
  provider: artifactory
  pollingInterval: 60
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

The operator can be customized via environment variables. This is particularly useful for Helm deployments or when using different registry credentials.

See [docs/configuration.md](docs/configuration.md) for detailed configuration options, including:
- Custom watcher images
- Custom secret names and keys
- Service account configuration
- Multi-tenant deployments

**Quick example** - Configure custom secret name:

```bash
kubectl set env deployment/kyverno-artifact-operator-controller-manager \
  -n kyverno-artifact-operator-system \
  WATCHER_SECRET_NAME=my-custom-secret
```

## Getting Started

### Binary Modes

The `manager` binary can run in two modes:

```bash
# Operator mode (default) - manages KyvernoArtifact CRDs
./bin/manager

# Watcher mode - directly monitors OCI registry for changes
./bin/manager -watcher
```

When running in watcher mode, the binary requires these environment variables:
- `IMAGE_BASE`: OCI image reference (e.g., `ghcr.io/owner/package`)
- `PROVIDER`: Registry provider (`github` or `artifactory`)
- `GITHUB_TOKEN`: GitHub token (for GitHub provider)
- `ARTIFACTORY_USERNAME` and `ARTIFACTORY_PASSWORD`: Credentials (for Artifactory provider)
- `POLL_INTERVAL`: Poll interval in seconds (default: 30)

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Running Locally

**Run the operator (automatically installs CRDs):**

```sh
make run
```

This will:
1. Generate manifests
2. Install CRDs into your cluster
3. Start the operator locally

**Note:** The operator requires a Kubernetes cluster. Make sure your `kubectl` is configured to point to a cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/kyverno-artifact-operator:tag
```

The build automatically embeds the git version into the binary. You can also set a custom version:

```sh
VERSION=v1.0.0 make docker-build IMG=<some-registry>/kyverno-artifact-operator:v1.0.0
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/kyverno-artifact-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/kyverno-artifact-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/kyverno-artifact-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.
