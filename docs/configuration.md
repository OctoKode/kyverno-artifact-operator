# Configuration

The Kyverno Artifact Operator can be configured via environment variables to customize its behavior for different deployment scenarios. This is particularly useful when deploying via Helm charts.

## Environment Variables

The following environment variables can be set on the controller deployment to override default values:

### Watcher Configuration

| Environment Variable | Description | Default Value |
|---------------------|-------------|---------------|
| `WATCHER_IMAGE` | Container image for the watcher pods | `ghcr.io/octokode/kyverno-artifact-operator:latest` |
| `WATCHER_SERVICE_ACCOUNT` | Service account name for watcher pods | `kyverno-artifact-operator-watcher` |

### Secret Configuration

| Environment Variable | Description | Default Value |
|---------------------|-------------|---------------|
| `WATCHER_SECRET_NAME` | Name of the Kubernetes secret containing credentials | `kyverno-watcher-secret` |
| `GITHUB_TOKEN_KEY` | Secret key for GitHub token | `github-token` |
| `ARTIFACTORY_USERNAME_KEY` | Secret key for Artifactory username | `artifactory-username` |
| `ARTIFACTORY_PASSWORD_KEY` | Secret key for Artifactory password | `artifactory-password` |

## Helm Chart Configuration

When using a Helm chart, these values can be configured in your `values.yaml`:

```yaml
controller:
  env:
    - name: WATCHER_IMAGE
      value: "ghcr.io/myorg/custom-watcher:v1.0.0"
    - name: WATCHER_SECRET_NAME
      value: "my-custom-secret"
    - name: WATCHER_SERVICE_ACCOUNT
      value: "my-watcher-sa"
    - name: GITHUB_TOKEN_KEY
      value: "gh-token"
    - name: ARTIFACTORY_USERNAME_KEY
      value: "username"
    - name: ARTIFACTORY_PASSWORD_KEY
      value: "password"
```

## Example Deployment Configuration

### Using Kustomize

Add environment variables to the controller manager deployment in your kustomization overlay:

```yaml
# config/manager/manager_config_patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: WATCHER_IMAGE
          value: "ghcr.io/octokode/kyverno-artifact-operator:v1.2.3"
        - name: WATCHER_SECRET_NAME
          value: "my-registry-secret"
```

Then reference it in your `kustomization.yaml`:

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- ../manager

patches:
- path: manager_config_patch.yaml
```

### Using kubectl directly

```bash
kubectl set env deployment/kyverno-artifact-operator-controller-manager \
  -n kyverno-artifact-operator-system \
  WATCHER_IMAGE=ghcr.io/octokode/kyverno-artifact-operator:v1.2.3 \
  WATCHER_SECRET_NAME=my-registry-secret
```

## Custom Secret Configuration

If you want to use a different secret structure, you can configure both the secret name and the keys:

```yaml
# Custom secret
apiVersion: v1
kind: Secret
metadata:
  name: my-artifact-credentials
  namespace: default
type: Opaque
data:
  gh-personal-token: <base64-encoded-token>
  artifactory-user: <base64-encoded-username>
  artifactory-pwd: <base64-encoded-password>
```

```yaml
# Controller configuration
env:
  - name: WATCHER_SECRET_NAME
    value: "my-artifact-credentials"
  - name: GITHUB_TOKEN_KEY
    value: "gh-personal-token"
  - name: ARTIFACTORY_USERNAME_KEY
    value: "artifactory-user"
  - name: ARTIFACTORY_PASSWORD_KEY
    value: "artifactory-pwd"
```

## Multi-tenant Deployments

For multi-tenant scenarios where different teams use different secrets or service accounts, you can deploy multiple instances of the operator with different configurations:

```yaml
# Team A deployment
env:
  - name: WATCHER_SECRET_NAME
    value: "team-a-secret"
  - name: WATCHER_SERVICE_ACCOUNT
    value: "team-a-watcher"

# Team B deployment
env:
  - name: WATCHER_SECRET_NAME
    value: "team-b-secret"
  - name: WATCHER_SERVICE_ACCOUNT
    value: "team-b-watcher"
```

Note: Each team's KyvernoArtifact resources will still need to be created in their respective namespaces with the corresponding secrets.
