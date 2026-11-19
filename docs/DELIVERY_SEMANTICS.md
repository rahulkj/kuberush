# Delivery Semantics

This document describes the current delivery behavior of KubeRush exporters and pipelines. KubeRush is in an early/scaffolding phase; semantics may evolve.

## General

- **At-least-once intent, best-effort implementation:** KubeRush attempts to deliver collected telemetry to configured sinks, but does not currently provide end-to-end exactly-once guarantees.
- **In-memory buffering:** buffered log export uses in-process queues. Data in memory can be lost on crashes, OOM kills, or abrupt termination before flush completes.
- **Shutdown behavior:** on SIGINT/SIGTERM, KubeRush stops intake components and attempts graceful shutdown (HTTP server shutdown, buffered sink flush/close, exporter close). Flush is bounded by timeouts and retry limits.

## Log pipeline (`exporters.stdout`, `otlp_logs`, `loki`, `splunk_hec`, `elasticsearch`)

- **Buffering:** each base log sink is wrapped with a buffered/batched sink.
  - Queue: bounded channel (`log_pipeline.queue_size`, default 10000)
  - Batching: flush when batch reaches `log_pipeline.batch_size` (default 200) or on `log_pipeline.flush_interval_ms` (default 2000ms)
- **Backpressure / drop policy:** when the queue is full:
  - `drop_newest` (default): newly enqueued records are dropped
  - `drop_oldest`: oldest queued record is discarded to make room (best-effort)
  - Drops are logged periodically; per-sink drop counters are not yet fully exposed as metrics.
- **Retries:** flush attempts retry synchronously up to `log_pipeline.max_retries` (default 3) with linear backoff steps based on `log_pipeline.retry_backoff_ms` (default 500ms).
  - After retries are exhausted, the batch is logged as failed and discarded (no persistent dead-letter queue).
- **Ordering:** records are enqueued in observed order per collection path, but batching, retries, and multi-sink fanout do not guarantee strict global ordering across sinks or after failures.

## OTLP exporters

- **Logs:** exported as OTLP/HTTP protobuf to `${endpoint}/v1/logs`.
- **Metrics:** exported as OTLP/HTTP protobuf to `${endpoint}/v1/metrics` (scrape-result metrics and converted scraped Prometheus metrics when enabled).
- **Traces:** exported as OTLP/HTTP protobuf to `${endpoint}/v1/traces` after enrichment for protobuf payloads; non-protobuf trace payloads may be forwarded raw without enrichment when configured.
- **Compression:** OTLP HTTP exporters support `compression: gzip|none` (request body `Content-Encoding: gzip` when enabled).
- **Timeouts:** HTTP client/export operations use bounded timeouts (currently 10s in exporter paths).
- **Partial failures:** a failed export attempt for a batch/request is retried according to the active pipeline/export path behavior; persistent failures result in dropped data for that batch/request.

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
