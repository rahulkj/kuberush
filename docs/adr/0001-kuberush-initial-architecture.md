# ADR 0001: Initial Architecture for KubeRush (Kubernetes Telemetry Collector & Shipper)

- Status: Accepted (initial architecture)
- Date: 2026-11-18
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