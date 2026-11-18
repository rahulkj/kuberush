# KubeRush Benchmarks (Template)

This document tracks reproducible performance results for **KubeRush**. Update it with dated runs, environment details, and configs.

## How to run (planned harness)
- Clusters: `kind` / `k3d` (small) and a managed cluster (scale) — document which was used.
- Workload generators:
  - Logs: configurable pods × lines/sec × line size
  - Metrics: pods exposing Prometheus metrics with configurable series/cardinality
  - Traces (later): OTLP generators
- Instrumentation: scrape KubeRush self-metrics (`/metrics`) and collect container resource usage (cAdvisor/`kubectl top`/Prometheus).
- Sinks: start with mock/localhost receivers, then validate against real backends (document versions).

## Report template (copy per run)

### Run: <short name>
- **Date:**
- **Git commit / tag:**
- **KubeRush version/config summary:**
- **Cluster:** (provider, node type/count, k8s version)
- **KubeRush deployment:** (replicas, CPU/mem requests+limits, env)
- **Workload:**
  - Logs: pods=…, rate=…, line bytes=…, duration=…
  - Metrics: targets=…, series/target=…, scrape interval=…
- **Exporters enabled:** (e.g., OTLP + Loki)

**Results (fill in):**
- Log throughput: ____ events/sec, ____ MiB/s
- Metrics throughput: ____ scrapes ok/fail, ____ samples/sec exported
- Pipeline/export latency: p50=… p95=… p99=…
- Resource usage: CPU (avg/max cores)=… , RSS (avg/max)=…
- Errors/retries/drops: …

**Notes / observations:**
- …

## Baseline comparisons (optional)
Document comparisons vs other stacks (e.g., Fluent Bit + Prometheus remote write path) with:
- Component versions and full config links
- Same workload and cluster
- Same “shipped to backend” definition

## Historical results
| Date | Commit | Scenario | Throughput (logs MiB/s) | Notes |
|------|--------|----------|-------------------------|-------|
| TBD  | —      | —        | —                       | Template row |
