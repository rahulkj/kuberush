# Install and Exporter Examples

## Build

```bash
go build -o bin/kuberush ./cmd/kuberush
# or
make build
```

## Run locally

```bash
./bin/kuberush --config ./config.example.yaml
# with overrides
./bin/kuberush --config ./config.example.yaml \
  --exporter.otlp-logs --exporter.otlp-logs.endpoint http://localhost:4318 \
  --exporter.otlp-metrics --exporter.otlp-metrics.endpoint http://localhost:4318
```

## Kubernetes (plain manifests)

- `deploy/kubernetes.yaml`: Deployment + Service + RBAC + ServiceAccount
- `ci/deployment.yml`: CI-oriented manifest (templated image/env in that context)

Apply:

```bash
kubectl apply -f deploy/kubernetes.yaml
```

Mount a config file (recommended): create a ConfigMap with `config.yaml` and add `--config /etc/kuberush/config.yaml` plus volume mounts (see Helm/Kustomize base for examples).

## Kustomize

Base manifests are in `deploy/kustomize/base`:

```bash
kubectl apply -k deploy/kustomize/base
```

Customize via overlays (image, namespace, configmap values, DaemonSet mode, etc.).

## Helm

Chart is in `deploy/helm/kuberush`:

```bash
helm upgrade --install kuberush deploy/helm/kuberush \
  --set image.repository=<repo>/kuberush \
  --set image.tag=<tag>
```

Enable DaemonSet node-log-tail mode:

```bash
helm upgrade --install kuberush deploy/helm/kuberush \
  --set daemonSet.enabled=true \
  --set config.features.node_log_tail=true \
  --set config.features.logs=false
```

> In node-log-tail mode, KubeRush reads container log files from the mounted host path (default `/var/log/pods`) instead of streaming via the Kubernetes API. This is typically deployed as a DaemonSet with a hostPath mount. Set `KUBERUSH_NODE_LOG_CHECKPOINT_PATH` to persist file offsets across restarts.

## Exporter configuration examples

### OTLP (logs/metrics/traces)

```yaml
exporters:
  otlp_logs:
    enabled: true
    endpoint: "http://otel-collector:4318"
    compression: "gzip"
  otlp_metrics:
    enabled: true
    endpoint: "http://otel-collector:4318"
    compression: "gzip"
  otlp_traces:
    enabled: true
    endpoint: "http://otel-collector:4318"
    compression: "gzip"
```

### Prometheus Remote Write (metrics)

```yaml
exporters:
  prometheus_remote_write:
    enabled: true
    endpoint: "http://prometheus:9090/api/v1/write"
metrics:
  scrape_interval_seconds: 30
  allow_metric_regex: []
  drop_metric_regex: []
  allow_label_regex: []
  drop_label_regex: []
  max_series_per_scrape: 0
```

### Loki (logs)

```yaml
exporters:
  loki:
    enabled: true
    endpoint: "http://loki:3100"
```

### Splunk HEC (logs)

```yaml
exporters:
  splunk_hec:
    enabled: true
    endpoint: "https://splunk:8088/services/collector/event"
    token: "<hec-token>"
    source: "kuberush"
    sourcetype: "_json"
    index: "main"
```

### Elasticsearch (logs, bulk API)

```yaml
exporters:
  elasticsearch:
    enabled: true
    endpoint: "http://elasticsearch:9200"
    index: "kuberush-logs"
    username: ""
    password: ""
```

### Datadog Logs

```yaml
exporters:
  datadog_logs:
    enabled: true
    site: "datadoghq.com"
    api_key: "<dd-api-key>"
    service: "kuberush"
```

### New Relic (OTLP convenience)

```yaml
exporters:
  newrelic:
    enabled: true
    endpoint: "https://otlp.nr-data.net" # region-specific endpoint may apply
    api_key: "<nr-license-key>"
```

> You can also use `exporters.otlp_logs/otlp_metrics/otlp_traces` with custom `headers` for vendor OTLP ingest endpoints.

### CloudWatch Logs (AWS)

```yaml
exporters:
  cloudwatch_logs:
    enabled: true
    region: "us-east-1"
    log_group: "/kuberush/logs"
    log_stream: "kuberush"
```

> Uses standard AWS credential/config resolution (environment, shared config, IAM role, etc.).

### GCP Cloud Logging (HTTP, token-based)

```yaml
exporters:
  gcp_cloud_logging:
    enabled: true
    project_id: "my-project"
    log_name: "projects/my-project/logs/kuberush"
    endpoint: "https://logging.googleapis.com"
    access_token: "<oauth2-access-token>"
```

### Azure Monitor Logs (Logs Ingestion API, token-based)

```yaml
exporters:
  azure_monitor_logs:
    enabled: true
    endpoint: "https://<dce>.ingest.monitor.azure.com"
    dcr_immutable_id: "<dcr-immutable-id>"
    stream_name: "Custom-Stream"
    access_token: "<aad-access-token>"
```

## Pipeline, filtering, and sharding

```yaml
kubernetes:
  namespaces:
    include: ["default", "prod"]
    exclude: ["kube-system"]
  label_selector: "app.kubernetes.io/part-of=myapp"

log_pipeline:
  batch_size: 200
  flush_interval_ms: 2000
  queue_size: 10000
  max_retries: 3
  retry_backoff_ms: 500
  drop_policy: "drop_newest" # or drop_oldest
  dlq_dir: "/var/lib/kuberush/dlq"      # optional: failed batches
  spool_dir: "/var/lib/kuberush/spool"  # optional: persistent batch spooling/replay

sharding:
  enabled: true
  strategy: "hashmod"
  replicas: 2
  replica_index: 0
```

## Feature flags

```yaml
features:
  logs: true
  metrics_scrape: true
  traces_ingest: true
  node_log_tail: false
```

CLI overrides are available for common flags (see `kuberush --help`).
