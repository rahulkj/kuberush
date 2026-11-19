# KubeRush

**KubeRush** (`kuberush`) is a Go-based, high-performance Kubernetes telemetry utility that discovers workloads across **all namespaces** and ships **logs, metrics, and (roadmap) traces** to common observability platforms.

> Tagline: *“Rush telemetry out of Kubernetes—fast, unified, boring to operate.”*

## What it does (target)
- Cluster-wide pod/container discovery via Kubernetes API (informers), all namespaces (with filters).
- **Logs:** stream container logs (Kubernetes API follow) with K8s metadata enrichment.
- **Metrics:** annotation-driven Prometheus endpoint scraping with enrichment and OTLP metrics ingestion/export support (including conversion of scraped Prometheus metrics to OTLP).
- **Traces:** OTLP/HTTP receiver with Kubernetes metadata enrichment (pod/node/labels via informer lookups) and OTLP trace export/forwarding pipeline.
- **Exporters:** stdout, OTLP (logs/metrics/traces), Loki, Splunk HEC, Elasticsearch, Prometheus Remote Write, Datadog Logs, CloudWatch Logs, GCP Cloud Logging, Azure Monitor Logs, and New Relic (OTLP convenience) sinks/exporters with buffered batching/retries, optional disk spool/DLQ, compression, and configurable pipeline controls.
- **Ops:** single static Go binary, health endpoint, Prometheus-format KubeRush self-metrics (`/metrics`), namespace/label filtering, sharding, CLI/config feature flags, Helm/Kustomize manifests, and RBAC examples.

## Documentation
- ADR: [`docs/adr/0001-kuberush-initial-architecture.md`](docs/adr/0001-kuberush-initial-architecture.md)
- Plan: [`docs/plan.md`](docs/plan.md)
- Install & exporter examples: [`docs/INSTALL_AND_EXAMPLES.md`](docs/INSTALL_AND_EXAMPLES.md)
- Delivery semantics: [`docs/DELIVERY_SEMANTICS.md`](docs/DELIVERY_SEMANTICS.md)
- Benchmarks (template): [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md)

## Status
🚧 **Early design / scaffolding phase.** APIs, config, and exporters are subject to change. See the plan for phases and milestones.

## Quickstart (placeholder)
1. Build (once code exists): `go build ./cmd/kuberush`
2. Provide kubeconfig/RBAC with pod list/watch (and log permissions as needed).
3. Run with a config file: `kuberush --config ./config.yaml`

> Detailed install (Helm/Kustomize), RBAC manifests, and exporter examples will be added as implementation lands.

## License
Apache-2.0. See [LICENSE.md](LICENSE.md).