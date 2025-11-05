# Metrics

The Kyverno Artifact Operator exposes Prometheus metrics on the `/metrics` endpoint.

## Available Metrics

### `kyverno_artifacts_total`

**Type:** Gauge

**Description:** Total number of KyvernoArtifact resources being managed by the operator.

**Example:**
```
kyverno_artifacts_total 5
```

### `kyverno_artifacts_by_phase`

**Type:** Gauge

**Description:** Number of KyvernoArtifact resources grouped by the phase of their associated pods.

**Labels:**
- `phase`: The current phase of the pod (Running, Pending, Failed, Succeeded, Unknown)

**Example:**
```
kyverno_artifacts_by_phase{phase="Running"} 3
kyverno_artifacts_by_phase{phase="Pending"} 1
kyverno_artifacts_by_phase{phase="Failed"} 1
```

## Accessing Metrics

The metrics are exposed on port 8443 (HTTPS) by default via the `controller-manager-metrics-service` service.

### In-cluster access

```bash
kubectl port-forward -n kyverno-artifact-operator-system \
  service/kyverno-artifact-operator-controller-manager-metrics-service 8443:8443
```

Then access metrics at: `https://localhost:8443/metrics`

### Prometheus scraping

Add the following ServiceMonitor to have Prometheus scrape the metrics:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kyverno-artifact-operator-metrics
  namespace: kyverno-artifact-operator-system
spec:
  endpoints:
  - interval: 30s
    port: https
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
  selector:
    matchLabels:
      control-plane: controller-manager
```

## Default Metrics

In addition to custom metrics, the operator also exposes standard controller-runtime metrics:

- `controller_runtime_reconcile_total` - Total number of reconciliations per controller
- `controller_runtime_reconcile_errors_total` - Total number of reconciliation errors per controller
- `controller_runtime_reconcile_time_seconds` - Length of time per reconciliation per controller
- `workqueue_*` - Work queue metrics
- `rest_client_*` - REST client metrics
- Go runtime metrics (memory, goroutines, etc.)
