# KubeRush — Project Plan

## 1) Project summary
**KubeRush** is a **Go-based, highly performant** Kubernetes telemetry utility that:

- Discovers workloads across **all namespaces** (cluster-wide) using Kubernetes APIs.
- **Pulls/collects**:
  - **Logs** from pods/containers (streaming)
  - **Metrics** from pods/services via discovery + scraping (Prometheus-compatible) and/or OTLP ingestion (roadmap)
  - **Traces** via **OTLP** receiver (roadmap / phase 2)
- **Ships** telemetry to **common monitoring platforms** through a pluggable exporter pipeline (batching, compression, backpressure).

**Working name:** `KubeRush` (binary: `kuberush`). Tagline: *“Rush telemetry out of Kubernetes—fast, unified, boring to operate.”*

---

## 2) Goals and non-goals

### Goals
- Single static Go binary with clear RBAC and Helm/Kustomize install examples.
- Cluster-wide discovery (all namespaces) with allow/deny and label/annotation filters.
- High-throughput pipelines: bounded queues, worker pools, batching, compression.
- Unified Kubernetes metadata enrichment on all signals (namespace, pod, container, node, labels, owner refs where available).
- Reliable delivery semantics per exporter (at-least-once where practical; document exactly-once limitations).
- Observability of KubeRush itself (Prometheus metrics, health endpoints).

### Non-goals (initial)
- Full replacement of all eBPF-based node agents on day one.
- Building a full UI (dashboards) — focus on collection + shipping.
- Exactly-once delivery guarantees across all backends.
- Collecting host-level OS metrics except what comes via existing exporters/integrations (future work).

---

## 3) Supported platforms (shipping targets)

KubeRush will use an **exporter abstraction**. Initial and planned supported destinations:

### Universal / open standards
- **OTLP/gRPC** and **OTLP/HTTP** (logs/metrics/traces) — recommended default interop path.
- **Prometheus Remote Write** (metrics) — for Prometheus-compatible backends.

### Logs-focused backends (examples)
- **Grafana Loki** (push/API)
- **Elasticsearch** / **OpenSearch** (bulk ingest API)
- **Splunk HTTP Event Collector (HEC)**
- **AWS CloudWatch Logs**
- **Google Cloud Logging**
- **Azure Monitor Logs** (via supported ingest paths)

### Metrics-focused backends (examples)
- **Prometheus / Mimir / Cortex / Thanos** (via Remote Write)
- **Datadog** (metrics/logs APIs as applicable)
- **New Relic** (OTLP or platform APIs)
- **Amazon CloudWatch Metrics**
- **Google Cloud Monitoring**
- **Azure Monitor Metrics**

### Traces-focused backends (examples)
- **Grafana Tempo**, **Jaeger** (OTLP/Thrift/gRPC depending on endpoint), **Honeycomb**, **Datadog APM**, **New Relic**, **Splunk Observability** — primarily via **OTLP** or vendor exporters where needed.

> **MVP suggestion:** OTLP export + Prometheus Remote Write + Loki (or one log backend) + one hosted backend (e.g., Datadog or New Relic) to validate plumbing; expand via community/priority.

---

## 4) High-level architecture

```
                    +---------------------------+
                    |  Kube API (informers)     |
                    |  Pods/Nodes (all ns)      |
                    +------------+--------------+
                                 |
        +------------------------+------------------------+
        |                        |                        |
  +-----v-----+            +-----v-----+            +-----v-----+
  | Log tail  |            | Metrics   |            | OTLP      |
  | manager   |            | scraper   |            | receiver  |
  | (API)     |            | (annot.)  |            | (traces/  |
  +-----+-----+            +-----+-----+            |  metrics) |
        |                        |                  +-----+-----+
        +------------+-----------+------------------------+
                     |
              +------v------+
              | Normalize + |
              | Enrich +    |
              | Batch/Queue |
              +------+------+
                     |
        +------------+------------+
        | Exporter pipeline       |
        | (workers, retries, TLS) |
        +--+---------+---------+--+
           |         |         |
        Loki/ES   Prom RW   OTLP/vendor
```

Key behaviors:
- Backpressure: if exporters are slow, buffers fill → configurable drop/slowdown policies (documented).
- Concurrency limits: max concurrent log streams, max scrape workers, max in-flight exports.
- Metadata enrichment from informer cache (low-latency, consistent).

---

## 5) Performance goals and benchmarking

### Performance goals (targets to validate)
- **High throughput logs:** sustain tens of MB/s per instance in tuned setups (workload-dependent) with bounded CPU; linear scaling via sharding in future.
- **Efficient metrics scraping:** scrape thousands of targets with controlled parallelism and memory bounds.
- **Low overhead:** modest CPU/memory footprint compared to running multiple legacy agents (to be measured).
- **Fast shipping:** configurable batch windows (e.g., 1–5s) and compression to minimize time-to-backend while maximizing throughput.

### Benchmarking plan
**Methodology**
- Reproducible Kubernetes test clusters (kind/k3d for small, managed cluster for scale).
- Synthetic workloads:
  - Log generators (configurable line rate/size)
  - Prometheus-exporter pods (configurable metric cardinality/series)
  - OTLP trace generators (phase 2)
- Instrument KubeRush with internal metrics: bytes in/out, records processed, queue depths, export latency histograms, errors/retries.

**Scenarios**
1. **Log flood:** N pods × M lines/sec → measure sustained ingestion rate, CPU, RSS, end-to-end delivery latency to mock sink and to a real backend (Loki/OTLP).
2. **Metrics scale:** S scrape targets, R series each → measure scrape duration, memory, export throughput to Prometheus Remote Write / OTLP.
3. **Mixed workload:** logs + metrics simultaneously → measure interference and resource ceilings.
4. **Backpressure:** slow sink → verify queue bounds, drop/alert behavior, recovery.

**Metrics to report**
- Throughput (events/sec, MiB/s) by signal
- p50/p95/p99 export/pipeline latency
- CPU cores and memory (RSS) at given load
- Loss behavior under overload (policy-dependent)
- Comparison runs vs baseline stack (e.g., Fluent Bit + Prometheus scraper) on the same cluster (document versions/configs)

**Deliverables**
- `benchmarks/` scripts/manifests and a `BENCHMARKS.md` with dated results, hardware/node specs, and reproduction steps.

---

## 6) Competitive landscape (other products in this space)

Examples of adjacent tools (not exhaustive):

- **OpenTelemetry Collector (Contrib)** — very flexible pipelines, supports receivers/exporters for many signals; can be configured for k8s (logs via filelog/k8s attrs, receivers, etc.). Powerful but configuration can be complex; not a single “opinionated all-in-one puller” focused on *all pods log streaming via K8s API* out of the box in the same way.
- **Fluent Bit / Fluentd** — strong log collection/forwarding; often paired with Prom tailing or Prometheus setups; metrics/traces usually need other components.
- **Prometheus + node_exporter/kube-state-metrics + adapters** — metrics-focused ecosystem; logs/traces separate.
- **Grafana Agent / Grafana Alloy** — unified agent direction (logs/metrics/traces), capable but broader product surface; resource and config trade-offs vary.
- **Vector (Datadog)** — high-performance observability data pipeline; flexible but requires assembling sources/sinks for full k8s coverage.
- **Datadog Agent / New Relic infrastructure agents** — commercial, feature-rich, vendor lock-in tendencies for full value.
- **Elastic Agent / Filebeat** — log-centric with add-ons.

---

## 7) Where KubeRush shines (differentiators)

- **Single Go binary, opinionated for Kubernetes:** one tool to discover **all pods in all namespaces** and ship logs + metrics (+ traces roadmap) with Kubernetes-native enrichment.
- **Performance-first design:** concurrent, bounded pipelines, batching, compression, and explicit backpressure policies tuned for cluster-scale telemetry.
- **Kubernetes API-aware log streaming (MVP path):** direct `Pods/log` follow management with lifecycle awareness (complementary to node-log tailers in some environments).
- **Unified metadata model:** consistent labels/tags across signals for easier correlation in backends.
- **Pluggable exporters:** start with OTLP + popular backends; add vendors without changing collectors.
- **Operational clarity:** focused docs for RBAC, resource sizing, and benchmark-backed tuning guidance (goal).
- **Simple deployment story:** aim for minimal moving parts vs assembling multiple stacks.

> Positioning: “**KubeRush** is the fast, unified on-ramp to ship K8s pod telemetry to the platforms you already use—without running three different agents first.”

---

## 8) Milestones / phases

### Phase 0 — Foundations (1–2 weeks)
- Repo scaffolding (Go modules, CI, lint, basic release workflow).
- Config loading, logging, health endpoints, internal metrics.
- Client-go informer for Pods (all namespaces), basic event handling.

### Phase 1 — MVP: Logs + Metrics + core exports (3–6 weeks)
- Log streaming manager + normalization + batching.
- Annotation-based metrics scraping + normalization.
- Exporters: **OTLP (logs/metrics)** and **Prometheus Remote Write** (metrics); plus **one logs backend** (e.g., Loki or Elasticsearch) — pick based on priority.
- RBAC examples, Helm chart skeleton.
- Initial benchmarks (logs flood, metrics scale) and `BENCHMARKS.md` draft.

### Phase 2 — Traces + broader platforms (3–5 weeks)
- OTLP receiver (gRPC/HTTP), enrichment, trace exporters (OTLP + key vendors).
- Add 2–3 more exporters (e.g., Splunk HEC, CloudWatch, Datadog/New Relic paths).
- Improved sharding/ha notes (even if single-instance MVP).

### Phase 3 — Hardening & scale (ongoing)
- Performance tuning, memory optimizations, fuzzing/chaos tests for reconnects.
- Optional node-agent mode exploration (CRI/log file tailing) for cost/scale.
- Security audit checklist, mTLS, secrets handling review.

---

## 9) Risks and mitigations
- **Kubernetes API server load (log streaming):** mitigate with concurrency caps, filtering, future node-local option.
- **Cardinality explosion (metrics):** document label allowlists, relabeling, and scrape scope controls.
- **Backend rate limits:** retries + backoff + buffering policies + exporter-specific tuning.
- **Scope creep across vendors:** prioritize OTLP first, then top-requested platforms.

---

## 10) Immediate next actions
1. Confirm MVP exporter priority (which 2–3 backends first).
2. Implement `kuberush` CLI skeleton + config schema.
3. Land informer + log streamer prototype behind a feature flag.
4. Stand up benchmark harness and publish first baseline numbers.