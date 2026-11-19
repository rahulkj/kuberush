# Delivery Semantics

This document describes the current delivery behavior of KubeRush exporters and pipelines. KubeRush is in an early/scaffolding phase; semantics may evolve.

## General

- **At-least-once intent, best-effort implementation:** KubeRush attempts to deliver collected telemetry to configured sinks, but does not currently provide end-to-end exactly-once guarantees.
- **In-memory buffering:** buffered log export uses in-process queues. Data in memory can be lost on crashes, OOM kills, or abrupt termination before flush completes.
- **Shutdown behavior:** on SIGINT/SIGTERM, KubeRush stops intake components and attempts graceful shutdown (HTTP server shutdown, buffered sink flush/close, exporter close). Flush is bounded by timeouts and retry limits.

## Log pipeline (`exporters.stdout`, `otlp_logs`, `loki`, `splunk_hec`, `elasticsearch`)

- **Buffering:** each base log sink is wrapped with a buffered/batched sink.
  - Queue: bounded in-memory channel (`log_pipeline.queue_size`, default 10000)
  - Batching: flush when batch reaches `log_pipeline.batch_size` (default 200) or on `log_pipeline.flush_interval_ms` (default 2000ms)
- **Persistent spool / replay (optional):** if `log_pipeline.spool_dir` is set, batches are written as pending spool files before export attempts.
  - On successful export, spool files are removed.
  - On startup, pending spool batches are replayed and retried.
  - This provides batch-level persistence across restarts for spooled batches; records still only in the in-memory queue (not yet batched/spooled) may be lost on crash.
- **Dead-letter queue (optional):** if `log_pipeline.dlq_dir` is set, batches that fail after retries are written as JSON files (with sink/error metadata) to the DLQ directory for inspection/replay.
  - If DLQ is not configured (or DLQ write fails) and spool is enabled, failed batches may remain in pending spool for later retry/replay.
- **Backpressure / drop policy:** when the queue is full:
  - `drop_newest` (default): newly enqueued records are dropped
  - `drop_oldest`: oldest queued record is discarded to make room (best-effort)
  - Drops are logged periodically; per-sink drop counters are not yet fully exposed as metrics.
- **Retries:** flush attempts retry synchronously up to `log_pipeline.max_retries` (default 3) with linear backoff steps based on `log_pipeline.retry_backoff_ms` (default 500ms).
  - After retries are exhausted, the batch is routed to DLQ/spool handling as configured (or logged and discarded).
- **Ordering:** records are enqueued in observed order per collection path, but batching, retries, replay, and multi-sink fanout do not guarantee strict global ordering across sinks or after failures.

## Exporters (vendor/backends)

- **OTLP (logs/metrics/traces):** HTTP/protobuf to `${endpoint}/v1/{logs|metrics|traces}` with optional gzip compression and custom headers. Traces are enriched before protobuf export when possible; non-protobuf trace payloads may be forwarded raw.
- **Prometheus Remote Write:** Snappy-compressed `prompb.WriteRequest` to configured endpoint (converted from scraped Prometheus exposition, best-effort).
- **Loki:** pushes JSON streams to `/loki/api/v1/push` with core Kubernetes labels.
- **Splunk HEC:** posts HEC JSON events to configured endpoint with optional token/source/sourcetype/index.
- **Elasticsearch:** posts NDJSON bulk documents to `${endpoint}/_bulk`.
- **Datadog Logs:** posts JSON logs to Datadog HTTP intake (API key via `DD-API-KEY`).
- **New Relic:** convenience integration using OTLP exporters with New Relic endpoint and `api-key` header.
- **CloudWatch Logs:** uses AWS SDK credential/config resolution and `PutLogEvents` (best-effort sequence token handling, log group/stream initialization).
- **GCP Cloud Logging / Azure Monitor Logs:** HTTP exporters supporting configured endpoint and bearer token/custom headers (auth token acquisition/rotation is external to KubeRush unless provided).

**Timeouts/retries:** exporter HTTP clients use bounded timeouts (typically 10s). Log sink batch retries and DLQ/spool behavior are handled by the buffered pipeline; metrics/trace export paths may log and drop on failure unless otherwise stated. Read backend-specific docs for auth, quotas, and schema requirements.

## Prometheus Remote Write (metrics)

- **Format:** `prompb.WriteRequest` marshaled and Snappy-compressed (`Content-Encoding: snappy`, `X-Prometheus-Remote-Write-Version: 0.1.0`).
- **Source:** converted from scraped Prometheus exposition (best-effort mapping for counters/gauges/untyped/summary/histogram components).
- **Retries:** requests are sent per-scrape export path without a dedicated persistent retry queue beyond caller behavior; failures are logged.

## Log source semantics

- **Kubernetes API log streaming (`features.logs`):** follows container logs via `Pods/log` with `follow=true`. Gaps can occur on restarts, API disconnects, or collection lifecycle transitions; tailers reconnect with backoff but delivery is not guaranteed gap-free.
- **Node log tail mode (`features.node_log_tail`):** intended for DaemonSet/node-local collection from `/var/log/pods`; implementation maturity may vary by environment/format and should be validated in target clusters.

## Metrics source semantics

- **Annotation-based scraping:** targets are discovered from running pods with Prometheus scrape annotations; scrape failures/successes are recorded as KubeRush scrape-result metrics and internal counters.
- **Cardinality controls:** allow/drop regex filters and `max_series_per_scrape` can reduce exported volume but may intentionally drop series/labels.

## Traces ingestion semantics

- **OTLP/HTTP receiver:** accepts `POST /v1/traces` and acknowledges receipt with HTTP 200/JSON after enqueueing processing work (async). Export/enrichment failures after receipt are logged/metriced but not signaled back to the sender as per-request delivery guarantees.

## Recommendations

- Size `log_pipeline.queue_size`, batch settings, and resource limits based on workload and sink latency.
- Prefer OTLP with compression for efficient transport where supported.
- Monitor KubeRush self-metrics at `/metrics` and sink/backend health dashboards.
- Validate behavior under backpressure and restart scenarios in your environment before production rollout.
