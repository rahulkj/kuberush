# ADR 0001: Initial Architecture for KubeRush (Kubernetes Telemetry Collector & Shipper)

- Status: Accepted (initial architecture); implementation in progress
- Date: 2026-11-18
- Last updated: 2026-11-18 (implementation status update)
- Owners: Platform/Observability Engineering
- Tags: kubernetes, observability, logs, metrics, traces, go
- License context: Apache-2.0 (see `LICENSE.md`)

## Context

Organizations running Kubernetes need a reliable way to collect **logs, metrics, and traces** from **all pods across all namespaces** and ship them to common observability backends with minimal operational toil. Today this is often achieved by combining multiple agents (log shippers, Prometheus scraping layers, trace collectors), which increases configuration surface area, resource overhead, and inconsistency in metadata/labeling.

**KubeRush** is introduced as a single, Go-based utility designed for:

- Cluster-wide discovery (all namespaces) with explicit RBAC boundaries.
- High-throughput, backpressure-aware telemetry pipelines.
- Unified Kubernetes metadata enrichment across signals.
- Pluggable export to widely used platforms (OTLP-first, plus vendor/native sinks).

## Decision summary

We will build **KubeRush** as a statically built Go service that:
1. Discovers pods cluster-wide via `client-go` informers/listers.
2. Collects signals via MVP paths (API log streaming, annotation-driven Prometheus scraping) and phase-2 paths (OTLP trace ingestion).
3. Normalizes/enriches events with Kubernetes metadata, batches and compresses them, and exports through isolated per-backend pipelines.
4. Exposes operational endpoints/metrics and ships with documented RBAC and deployment guidance.

## Decision

### 1) Language and runtime
- **Go** for a single static binary, strong concurrency primitives, and mature Kubernetes libraries (`client-go`).
- Use structured logging, graceful shutdown, and bounded concurrency throughout collectors and exporters.

### 2) Discovery (all namespaces)
- Watch `Pods` (cluster scope) using informers; optionally watch `Nodes` for enrichment.
- Maintain an in-memory cache of container targets and metadata: namespace, pod, container, node, labels, annotations, owner references (best effort).
- Support configuration for namespace allow/deny lists and label/annotation selectors.

### 3) Signal collection strategy
**Logs (MVP)**
- Follow container logs via Kubernetes API (`Pods/log`, `follow=true`) with a supervisor tied to pod/container lifecycle.
- Normalize records with timestamps and K8s metadata.
- Apply bounded queues, retry with exponential backoff for transient API failures, and document limitations of API-based tailing (gaps on hard restarts, API server load).

**Metrics (MVP)**
- Annotation-driven scraping using Prometheus conventions (e.g., `prometheus.io/scrape`, `prometheus.io/port`, `prometheus.io/path`).
- Scrape in-cluster HTTP endpoints, parse Prometheus exposition format, attach K8s metadata.
- Future: optional OTLP metrics ingestion.

**Traces (phase 2)**
- OTLP gRPC/HTTP receiver for application-exported traces.
- Enrich spans using informer-derived metadata (e.g., by IP/attributes) where possible.
- Export via OTLP or selected vendor exporters.

### 4) Processing pipeline
- Stages: discover → collect → normalize/enrich → batch/queue → export.
- Bounded buffers between stages; configurable worker pools.
- Batch + compression (e.g., snappy/zstd) to improve throughput and cost.
- Per-exporter queues and isolation to prevent one slow sink from starving others beyond defined limits.

### 5) Exporters (sinks)
- Pluggable interface per signal type.
- **OTLP (gRPC/HTTP)** as the universal interoperability target.
- Additional backends prioritized by user demand (examples listed in `docs/plan.md`), including Prometheus Remote Write for metrics and common log/trace platforms.

### 6) Deployment models
- **Deployment** model for MVP (API-driven log streaming and/or OTLP ingestion), with documented API server/network trade-offs.
- **DaemonSet** model reserved for future node-local collection optimizations (e.g., container runtime log files), not required for MVP.

### 7) Configuration and operations
- YAML config + env overrides + CLI flags.
- Endpoints: health, readiness, and KubeRush self-metrics (Prometheus).
- Structured JSON logs; example RBAC manifests and security guidance documented.

### 8) Security
- Least-privilege RBAC examples per deployment mode.
- TLS/mTLS for receivers/exporters where supported.
- No telemetry of secrets in logs; credentials via environment variables or mounted secrets.

## Compatibility / standards
- Prefer **OpenTelemetry (OTLP)** for export/interoperability where feasible.
- Metrics scraping follows common Prometheus pod annotation conventions for MVP.
- Kubernetes support targets recent stable releases; exact matrix documented during implementation.

## Consequences

### Positive
- Single operational component for multi-signal shipping with consistent K8s enrichment.
- Clear extension points (exporters, future collectors) without redesigning the core pipeline.
- Performance-oriented defaults (batching, compression, bounded concurrency) align with large-cluster goals.

### Negative / Trade-offs
- API-based log streaming can increase Kubernetes API server and network load at scale; mitigation includes filters, concurrency caps, and a future node-agent path.
- In-cluster scraping increases east-west traffic and requires careful concurrency/cardinality controls.
- Trace completeness depends on application instrumentation/export behavior (OTLP).

## Alternatives considered
1. **Compose existing tools only (e.g., Fluent Bit/Vector + Prometheus + OTel Collector).** Rejected as the primary goal is a single, opinionated Kubernetes-first utility with unified enrichment and simpler operations.
2. **eBPF-first collection from day one.** Rejected for MVP due to portability and implementation complexity; may be revisited for node-level optimizations.
3. **Only per-namespace agents.** Rejected because it does not meet the “all namespaces by default” operational simplicity goal.

## Follow-ups
- Finalize MVP signal/export matrix and SLOs.
- Publish benchmark methodology and initial results in `docs/BENCHMARKS.md`.
- Evaluate sharding/single-instance scaling limits and define a scaling strategy if needed.

## Implementation status update (2026-11-18)

This addendum summarizes what has been built in the repository so far against this ADR, and what remains incomplete.

### Implemented

#### Core/runtime and operations
- Single Go binary (`cmd/kuberush`) with YAML config loading, environment overrides, and CLI flags for common overrides (health address, feature toggles, exporter enable/endpoints, scrape interval).
- Graceful shutdown handling and bounded HTTP server/export timeouts in implemented paths.
- Health/readiness endpoints (`/healthz`, `/readyz`) and Prometheus-format KubeRush self-metrics endpoint (`/metrics`) exposing internal counters (log records/export errors/tailer errors, scrape attempts, OTLP ingest/forward errors).
- Kubernetes client bootstrap from in-cluster config, explicit kubeconfig, or default kubeconfig loading rules.

#### Discovery and filtering
- Cluster-wide pod discovery via `client-go` informer/lister (`internal/discovery`) watching pods and invoking handlers on add/update/delete with filter-aware behavior.
- Namespace include/exclude filtering and pod label selector filtering.
- Basic hashmod sharding strategy (`sharding.enabled/strategy/replicas/replica_index`) applied to discovery/scraping/lookup eligibility for horizontal scaling experiments.
- Pod lookup helpers (by namespace/name and by IP) used for trace enrichment.

#### Log collection and processing
- Kubernetes API log streaming collector (`Pods/log`, follow + timestamps) with per-container tailers tied to pod lifecycle and reconnect-on-error behavior.
- Basic node-log-tail mode (`features.node_log_tail`) for DaemonSet-style file collection from `/var/log/pods` (path/env configurable), including:
  - file discovery/scanning, offset tracking, checkpoint load/save (JSON, atomic write), checkpoint pruning
  - rotation/truncation handling with dev/inode identity (Unix), best-effort behavior on other platforms
  - CRI and Docker JSON log line parsing (including CRI partial-line handling)
  - best-effort per-node filtering when `NODE_NAME` and pod metadata are available
- Log normalization/enrichment with namespace/pod/container/node and pod labels (where available), exported as structured attributes in supported sinks.
- Buffered log export pipeline wrapper per sink with configurable batch size, flush interval, queue size, retries/backoff, and drop policy (`drop_newest`/`drop_oldest`).
- Batch-level persistent spool/replay support (`log_pipeline.spool_dir`) and dead-letter queue output for failed batches (`log_pipeline.dlq_dir`).

#### Metrics collection and export
- Annotation-driven Prometheus scraping (`prometheus.io/scrape`, `port`, `path`) for running pods with configurable scrape interval.
- Best-effort conversion of scraped Prometheus exposition metrics to OTLP metrics (gauge/counter/untyped/summary/histogram mappings) for OTLP export.
- Prometheus Remote Write exporter path (Snappy + `prompb.WriteRequest`) from scraped metrics (best-effort conversion).
- KubeRush scrape-result metrics exported via OTLP metrics exporter when enabled (`kuberush.scrape.up`, `kuberush.scrape.duration_ms`, `kuberush.scrape.response_bytes`) with target metadata.
- Metric filtering/cardinality controls: allow/drop metric regex, allow/drop label regex, and max series per scrape.
- OTLP/HTTP metrics ingestion endpoint (`POST /v1/metrics`) with logging/metrics and optional raw forwarding when OTLP metrics export endpoint is configured.

#### Traces pipeline
- OTLP/HTTP trace ingestion endpoint (`POST /v1/traces`) integrated into a processor pipeline.
- Protobuf trace request decode + Kubernetes metadata enrichment (resource attributes) using pod lookups by `k8s.namespace.name`/`k8s.pod.name` or IP-related attributes; adds missing `k8s.*`/pod label attributes when resolved.
- OTLP/HTTP trace exporter component and async export/forward processing; fallback raw forwarding for non-protobuf payloads when endpoint is configured.
- Trace pipeline wiring with optional enrichment based on discovery availability.

#### Exporters and integrations (implemented)
- **Logs:** stdout, OTLP/HTTP (protobuf, gzip/headers), Loki (push API), Splunk HEC, Elasticsearch bulk API, Datadog Logs (HTTP intake), CloudWatch Logs (AWS SDK v2, basic sequence-token handling), GCP Cloud Logging (HTTP entries:write with provided token/headers), Azure Monitor Logs (Logs Ingestion API with provided token/headers).
- **Metrics:** OTLP/HTTP metrics exporter (protobuf, gzip/headers), Prometheus Remote Write exporter.
- **Traces:** OTLP/HTTP trace exporter (protobuf, gzip/headers).
- **Convenience/vendor integration:** New Relic configuration path wired through OTLP exporters with API key/header injection.

#### Deployment, packaging, and docs
- Kubernetes deployment manifests and RBAC examples (`deploy/kubernetes.yaml`, CI manifests).
- Kustomize base manifests (`deploy/kustomize/base`).
- Helm chart (`deploy/helm/kuberush`) with Deployment/DaemonSet, RBAC, Service, ConfigMap, and config/feature values.
- Documentation updates: `README.md`, `config.example.yaml`, `docs/INSTALL_AND_EXAMPLES.md`, and `docs/DELIVERY_SEMANTICS.md`.

### Not completed / remaining gaps

#### Signal collection and processing completeness
- **OTLP protocol coverage:** OTLP/gRPC receivers/exporters are not implemented (OTLP/HTTP paths are implemented).
- **Metrics parity:** scraped Prometheus-to-OTLP conversion is best-effort (not full semantic parity/edge-case coverage); native OTLP metrics processing pipeline beyond receive/forward/export-of-generated-metrics is limited.
- **Traces maturity:** enrichment is resource-attribute focused and best-effort; no OTLP/gRPC, no advanced trace processors/sampling/tail-based logic, and non-protobuf trace payloads are not enriched.
- **Unified enrichment model:** metadata enrichment is improved but not fully unified/rich across all signals (e.g. owner references, workload controllers, richer node/cluster metadata, consistent label propagation policies).
- **Node-log-tail production hardening:** checkpointing/pruning/rotation handling are implemented in a basic form, but full runtime/format edge-case coverage, checkpoint compaction/retention policies, and high-scale file-tail optimizations are incomplete.

#### Exporters and vendor integrations
- **Vendor coverage gaps:** native exporters/integrations for Cloud Monitoring/Cloud Logging and Azure Monitor with built-in auth flows (ADC/OAuth2 token acquisition, Azure AAD/MSI flows) are not fully implemented (current GCP/Azure exporters support provided tokens/headers).
- **Additional ecosystems/backends:** OpenSearch/other log backends, vendor-specific metrics/traces APIs (beyond provided OTLP/HTTP convenience paths), and broader destination-specific feature parity are incomplete.
- **Prometheus Remote Write scope:** implemented for scraped metric export path; advanced remote write features (auth schemes beyond headers, batching/windowing policies, retry semantics parity) are limited.

#### Pipeline reliability and performance guarantees
- **Delivery guarantees:** no exactly-once guarantees; persistent buffering is batch-level spool (not per-record WAL). Records not yet spooled/batched can be lost on crash; replay/DLQ semantics are best-effort.
- **Backpressure/isolation:** per-sink buffered queues/retries exist, but full per-exporter isolation, configurable worker pools across all stages, end-to-end bounded memory guarantees, and comprehensive overload policies are not complete.
- **Compression/encoding coverage:** gzip supported for OTLP HTTP exporters; broader compression options and consistent compression/backpressure controls across all exporters/paths are incomplete.

#### Configuration, security, and operations
- **Security hardening:** TLS/mTLS configuration for receivers/exporters, secret management integrations, and security audit/review tasks are incomplete.
- **Observability of KubeRush:** self-metrics exist but are limited (counters only); histograms/latency, queue depth, throughput, and exporter-level detailed metrics are incomplete.
- **Config/CLI maturity:** broader schema validation, feature-flag coverage, dynamic reload, and comprehensive config docs/examples are still evolving.

#### Deployment scale, validation, and project follow-ups
- **Sharding/HA:** basic hashmod sharding is implemented; leader election, coordination, stateful HA patterns, and validated scaling strategies are incomplete.
- **Benchmarking/validation:** `docs/BENCHMARKS.md` remains a template; reproducible benchmark harnesses, baseline results, and comparison runs are not yet published.
- **Release/quality hardening:** CI/release workflows, conformance/integration tests across backends, and production hardening/performance tuning remain ongoing.