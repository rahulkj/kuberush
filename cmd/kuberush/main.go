package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rahulkj/kubemon/internal/config"
	"github.com/rahulkj/kubemon/internal/discovery"
	"github.com/rahulkj/kubemon/internal/exporter"
	"github.com/rahulkj/kubemon/internal/health"
	"github.com/rahulkj/kubemon/internal/kube"
	"github.com/rahulkj/kubemon/internal/logs"
	"github.com/rahulkj/kubemon/internal/metrics"
	"github.com/rahulkj/kubemon/internal/selfmetrics"
	"github.com/rahulkj/kubemon/internal/traces"
)

func main() {
	var configPath string
	var kubeconfig string
	var healthAddr string
	var enableOTLPLogs bool
	var otlpLogsEndpoint string
	var enableOTLPMetrics bool
	var otlpMetricsEndpoint string
	var enableOTLPTraces bool
	var otlpTracesEndpoint string
	var enableLoki bool
	var lokiEndpoint string
	var enableSplunk bool
	var splunkEndpoint string
	var enableES bool
	var esEndpoint string
	var enableRemoteWrite bool
	var remoteWriteEndpoint string
	var disableLogs bool
	var disableMetricsScrape bool
	var disableTracesIngest bool
	var scrapeInterval int

	flag.StringVar(&configPath, "config", "", "Path to config YAML file (optional)")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (optional; defaults to in-cluster or KUBECONFIG/default)")
	flag.StringVar(&healthAddr, "health.listen-address", "", "Health/metrics server listen address (overrides config)")
	flag.BoolVar(&enableOTLPLogs, "exporter.otlp-logs", false, "Enable OTLP logs exporter")
	flag.StringVar(&otlpLogsEndpoint, "exporter.otlp-logs.endpoint", "", "OTLP logs endpoint (e.g. http://localhost:4318)")
	flag.BoolVar(&enableOTLPMetrics, "exporter.otlp-metrics", false, "Enable OTLP metrics exporter")
	flag.StringVar(&otlpMetricsEndpoint, "exporter.otlp-metrics.endpoint", "", "OTLP metrics endpoint")
	flag.BoolVar(&enableOTLPTraces, "exporter.otlp-traces", false, "Enable OTLP traces exporter/forwarding")
	flag.StringVar(&otlpTracesEndpoint, "exporter.otlp-traces.endpoint", "", "OTLP traces endpoint")
	flag.BoolVar(&enableLoki, "exporter.loki", false, "Enable Loki log exporter")
	flag.StringVar(&lokiEndpoint, "exporter.loki.endpoint", "", "Loki endpoint (e.g. http://localhost:3100)")
	flag.BoolVar(&enableSplunk, "exporter.splunk-hec", false, "Enable Splunk HEC log exporter")
	flag.StringVar(&splunkEndpoint, "exporter.splunk-hec.endpoint", "", "Splunk HEC endpoint")
	flag.BoolVar(&enableES, "exporter.elasticsearch", false, "Enable Elasticsearch log exporter")
	flag.StringVar(&esEndpoint, "exporter.elasticsearch.endpoint", "", "Elasticsearch endpoint")
	flag.BoolVar(&enableRemoteWrite, "exporter.prom-remote-write", false, "Enable Prometheus Remote Write exporter")
	flag.StringVar(&remoteWriteEndpoint, "exporter.prom-remote-write.endpoint", "", "Prometheus Remote Write endpoint")
	flag.BoolVar(&disableLogs, "disable.logs", false, "Disable Kubernetes log collection")
	flag.BoolVar(&disableMetricsScrape, "disable.metrics-scrape", false, "Disable annotation-based metrics scraping")
	flag.BoolVar(&disableTracesIngest, "disable.traces-ingest", false, "Disable OTLP traces ingestion pipeline")
	flag.IntVar(&scrapeInterval, "metrics.scrape-interval-seconds", 0, "Metrics scrape interval in seconds (overrides config)")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if healthAddr != "" {
		cfg.Health.ListenAddress = healthAddr
	}
	if enableOTLPLogs {
		cfg.Exporters.OTLPLogs.Enabled = true
	}
	if otlpLogsEndpoint != "" {
		cfg.Exporters.OTLPLogs.Endpoint = otlpLogsEndpoint
	}
	if enableOTLPMetrics {
		cfg.Exporters.OTLPMetrics.Enabled = true
	}
	if otlpMetricsEndpoint != "" {
		cfg.Exporters.OTLPMetrics.Endpoint = otlpMetricsEndpoint
	}
	if enableOTLPTraces {
		cfg.Exporters.OTLPTraces.Enabled = true
	}
	if otlpTracesEndpoint != "" {
		cfg.Exporters.OTLPTraces.Endpoint = otlpTracesEndpoint
	}
	if enableLoki {
		cfg.Exporters.Loki.Enabled = true
	}
	if lokiEndpoint != "" {
		cfg.Exporters.Loki.Endpoint = lokiEndpoint
	}
	if enableSplunk {
		cfg.Exporters.SplunkHEC.Enabled = true
	}
	if splunkEndpoint != "" {
		cfg.Exporters.SplunkHEC.Endpoint = splunkEndpoint
	}
	if enableES {
		cfg.Exporters.Elasticsearch.Enabled = true
	}
	if esEndpoint != "" {
		cfg.Exporters.Elasticsearch.Endpoint = esEndpoint
	}
	if enableRemoteWrite {
		cfg.Exporters.PrometheusRemoteWrite.Enabled = true
	}
	if remoteWriteEndpoint != "" {
		cfg.Exporters.PrometheusRemoteWrite.Endpoint = remoteWriteEndpoint
	}
	if disableLogs {
		cfg.Features.Logs = false
	}
	if disableMetricsScrape {
		cfg.Features.MetricsScrape = false
	}
	if disableTracesIngest {
		cfg.Features.TracesIngest = false
	}
	if scrapeInterval > 0 {
		cfg.Metrics.ScrapeIntervalSeconds = scrapeInterval
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sm := selfmetrics.New()

	// Exporter pipeline (ADR 0001): configure sinks
	var baseSinks []exporter.LogSink
	if cfg.Exporters.Stdout {
		baseSinks = append(baseSinks, exporter.NewStdoutLogSink())
	}
	if cfg.Exporters.OTLPLogs.Enabled {
		baseSinks = append(baseSinks, exporter.NewOTLPLogsExporter(cfg.Exporters.OTLPLogs.Endpoint, cfg.Exporters.OTLPLogs.Compression, cfg.Exporters.OTLPLogs.Headers))
	}
	if cfg.Exporters.NewRelic.Enabled && cfg.Exporters.NewRelic.Endpoint != "" {
		h := map[string]string{}
		for k, v := range cfg.Exporters.NewRelic.Headers {
			h[k] = v
		}
		if cfg.Exporters.NewRelic.APIKey != "" {
			if _, ok := h["api-key"]; !ok {
				h["api-key"] = cfg.Exporters.NewRelic.APIKey
			}
		}
		baseSinks = append(baseSinks, exporter.NewOTLPLogsExporter(cfg.Exporters.NewRelic.Endpoint, "gzip", h))
	}
	if cfg.Exporters.DatadogLogs.Enabled {
		baseSinks = append(baseSinks, exporter.NewDatadogLogsExporter(cfg.Exporters.DatadogLogs.Endpoint, cfg.Exporters.DatadogLogs.Site, cfg.Exporters.DatadogLogs.APIKey, cfg.Exporters.DatadogLogs.Service))
	}
	if cfg.Exporters.CloudWatchLogs.Enabled {
		cw, err := exporter.NewCloudWatchLogsExporter(context.Background(), cfg.Exporters.CloudWatchLogs.Region, cfg.Exporters.CloudWatchLogs.Endpoint, cfg.Exporters.CloudWatchLogs.LogGroup, cfg.Exporters.CloudWatchLogs.LogStream)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize cloudwatch logs exporter: %v\n", err)
		} else {
			baseSinks = append(baseSinks, cw)
		}
	}
	if cfg.Exporters.GCPCloudLogging.Enabled {
		baseSinks = append(baseSinks, exporter.NewGCPCloudLoggingExporter(
			cfg.Exporters.GCPCloudLogging.Endpoint,
			cfg.Exporters.GCPCloudLogging.ProjectID,
			cfg.Exporters.GCPCloudLogging.LogName,
			cfg.Exporters.GCPCloudLogging.AccessToken,
			cfg.Exporters.GCPCloudLogging.Headers,
		))
	}
	if cfg.Exporters.AzureMonitorLogs.Enabled {
		baseSinks = append(baseSinks, exporter.NewAzureMonitorLogsExporter(
			cfg.Exporters.AzureMonitorLogs.Endpoint,
			cfg.Exporters.AzureMonitorLogs.RuleID,
			cfg.Exporters.AzureMonitorLogs.StreamName,
			cfg.Exporters.AzureMonitorLogs.AccessToken,
			cfg.Exporters.AzureMonitorLogs.Headers,
		))
	}
	if cfg.Exporters.Loki.Enabled {
		baseSinks = append(baseSinks, exporter.NewLokiLogsExporter(cfg.Exporters.Loki.Endpoint))
	}
	if cfg.Exporters.SplunkHEC.Enabled {
		baseSinks = append(baseSinks, exporter.NewSplunkHECExporter(
			cfg.Exporters.SplunkHEC.Endpoint,
			cfg.Exporters.SplunkHEC.Token,
			cfg.Exporters.SplunkHEC.Source,
			cfg.Exporters.SplunkHEC.Sourcetype,
			cfg.Exporters.SplunkHEC.Index,
		))
	}
	if cfg.Exporters.Elasticsearch.Enabled {
		baseSinks = append(baseSinks, exporter.NewElasticsearchExporter(
			cfg.Exporters.Elasticsearch.Endpoint,
			cfg.Exporters.Elasticsearch.Index,
			cfg.Exporters.Elasticsearch.Username,
			cfg.Exporters.Elasticsearch.Password,
		))
	}
	if len(baseSinks) == 0 {
		baseSinks = append(baseSinks, exporter.NewStdoutLogSink()) // default fallback
	}
	var logSinks []exporter.LogSink
	flushInterval := time.Duration(cfg.LogPipeline.FlushIntervalMS) * time.Millisecond
	retryBackoff := time.Duration(cfg.LogPipeline.RetryBackoffMS) * time.Millisecond
	for _, s := range baseSinks {
		bs := exporter.NewBufferedLogSink(s, cfg.LogPipeline.BatchSize, flushInterval, cfg.LogPipeline.QueueSize, cfg.LogPipeline.MaxRetries, retryBackoff, cfg.LogPipeline.DropPolicy, cfg.LogPipeline.DLQDir, cfg.LogPipeline.SpoolDir)
		logSinks = append(logSinks, bs)
		fmt.Printf("exporter initialized: log sink=%s (buffered batch=%d flush=%s queue=%d retries=%d drop=%s dlq=%t spool=%t)\n", s.Name(), cfg.LogPipeline.BatchSize, flushInterval, cfg.LogPipeline.QueueSize, cfg.LogPipeline.MaxRetries, cfg.LogPipeline.DropPolicy, cfg.LogPipeline.DLQDir != "", cfg.LogPipeline.SpoolDir != "")
	}

	var metricsExporter *exporter.OTLPMetricsExporter
	if cfg.Exporters.OTLPMetrics.Enabled {
		metricsExporter = exporter.NewOTLPMetricsExporter(cfg.Exporters.OTLPMetrics.Endpoint, cfg.Exporters.OTLPMetrics.Compression, cfg.Exporters.OTLPMetrics.Headers)
		fmt.Printf("exporter initialized: metrics sink=%s\n", metricsExporter.Name())
	} else if cfg.Exporters.NewRelic.Enabled && cfg.Exporters.NewRelic.Endpoint != "" {
		h := map[string]string{}
		for k, v := range cfg.Exporters.NewRelic.Headers {
			h[k] = v
		}
		if cfg.Exporters.NewRelic.APIKey != "" {
			if _, ok := h["api-key"]; !ok {
				h["api-key"] = cfg.Exporters.NewRelic.APIKey
			}
		}
		metricsExporter = exporter.NewOTLPMetricsExporter(cfg.Exporters.NewRelic.Endpoint, "gzip", h)
		fmt.Printf("exporter initialized: metrics sink=%s (newrelic)\n", metricsExporter.Name())
	}
	var remoteWriteExporter *exporter.PrometheusRemoteWriteExporter
	if cfg.Exporters.PrometheusRemoteWrite.Enabled {
		remoteWriteExporter = exporter.NewPrometheusRemoteWriteExporter(cfg.Exporters.PrometheusRemoteWrite.Endpoint)
		fmt.Printf("exporter initialized: metrics sink=%s\n", remoteWriteExporter.Name())
	}

	// Build k8s client and optional components (discovery/logs/metrics scrape/trace enrichment)
	var podWatcher *discovery.PodWatcher
	var logCollector *logs.Collector
	_, clientset, err := kube.BuildClientset(kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kube client: %v\n", err)
	} else {
		if cfg.Features.Logs && !cfg.Features.NodeLogTail {
			logCollector = logs.NewCollector(clientset, logSinks, sm)
		}
		podWatcher = discovery.NewPodWatcher(clientset, logCollector, cfg.Kubernetes, cfg.Sharding)
	}

	var traceProcessor *traces.Processor
	if cfg.Features.TracesIngest && (cfg.Exporters.OTLPTraces.Enabled || cfg.Exporters.OTLPTraces.Endpoint != "" || (cfg.Exporters.NewRelic.Enabled && cfg.Exporters.NewRelic.Endpoint != "")) {
		var enricher *traces.Enricher
		if podWatcher != nil {
			enricher = traces.NewEnricher(podWatcher)
		}
		var traceExporter *exporter.OTLPTracesExporter
		traceEndpoint := cfg.Exporters.OTLPTraces.Endpoint
		if cfg.Exporters.OTLPTraces.Enabled {
			traceExporter = exporter.NewOTLPTracesExporter(cfg.Exporters.OTLPTraces.Endpoint, cfg.Exporters.OTLPTraces.Compression, cfg.Exporters.OTLPTraces.Headers)
			fmt.Printf("exporter initialized: traces sink=%s\n", traceExporter.Name())
		} else if cfg.Exporters.NewRelic.Enabled && cfg.Exporters.NewRelic.Endpoint != "" {
			h := map[string]string{}
			for k, v := range cfg.Exporters.NewRelic.Headers {
				h[k] = v
			}
			if cfg.Exporters.NewRelic.APIKey != "" {
				if _, ok := h["api-key"]; !ok {
					h["api-key"] = cfg.Exporters.NewRelic.APIKey
				}
			}
			traceEndpoint = cfg.Exporters.NewRelic.Endpoint
			traceExporter = exporter.NewOTLPTracesExporter(traceEndpoint, "gzip", h)
			fmt.Printf("exporter initialized: traces sink=%s (newrelic)\n", traceExporter.Name())
		}
		traceProcessor = traces.NewProcessor(enricher, traceExporter, traceEndpoint, sm)
		if enricher != nil {
			fmt.Println("traces pipeline enabled (OTLP HTTP ingest -> k8s enrichment -> export)")
		} else {
			fmt.Println("traces pipeline enabled (OTLP HTTP ingest -> export/forward)")
		}
	}

	// Start health/metrics HTTP server (also OTLP traces/metrics receiver)
	hSrv := health.NewServer(
		cfg.Health.ListenAddress,
		traceProcessor,
		cfg.Exporters.OTLPMetrics.Enabled,
		cfg.Exporters.OTLPMetrics.Endpoint,
		sm,
	)
	go func() {
		if err := hSrv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "health server error: %v\n", err)
		}
	}()

	fmt.Printf("kuberush starting (health on %s)\n", cfg.Health.ListenAddress)

	if clientset != nil && podWatcher != nil {
		go func() {
			if err := podWatcher.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "pod watcher error: %v\n", err)
			}
		}()
		if !cfg.Features.Logs {
			fmt.Println("log collection feature disabled (discovery running for enrichment/scrape)")
		}
		if cfg.Features.NodeLogTail {
			root := os.Getenv("KUBERUSH_NODE_LOG_PATH")
			if root == "" {
				root = "/var/log/pods"
			}
			checkpointPath := os.Getenv("KUBERUSH_NODE_LOG_CHECKPOINT_PATH")
			nodeName := os.Getenv("NODE_NAME")
			nodeTailer := logs.NewNodeTailer(root, checkpointPath, nodeName, logSinks, sm, cfg.Kubernetes, cfg.Sharding, podWatcher)
			go func() {
				_ = nodeTailer.Run(ctx)
			}()
			if checkpointPath != "" {
				fmt.Printf("node log tail mode enabled (root=%s checkpoint=%s node=%s)\n", root, checkpointPath, nodeName)
			} else {
				fmt.Printf("node log tail mode enabled (root=%s node=%s)\n", root, nodeName)
			}
		}
		if cfg.Features.MetricsScrape {
			metricsScraper := metrics.NewScraper(clientset, logSinks, metricsExporter, remoteWriteExporter, cfg.Kubernetes, cfg.Sharding, sm, cfg.Metrics)
			go metricsScraper.Run(ctx)
			fmt.Printf("metrics scraper started (annotation-based, every %ds)\n", cfg.Metrics.ScrapeIntervalSeconds)
		}
	}

	<-ctx.Done()
	fmt.Println("kuberush shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hSrv.Shutdown(shutdownCtx)
	if traceProcessor != nil {
		_ = traceProcessor.Close()
	}
	for _, s := range logSinks {
		_ = s.Close()
	}
	if metricsExporter != nil {
		_ = metricsExporter.Close()
	}
	if remoteWriteExporter != nil {
		_ = remoteWriteExporter.Close()
	}

	fmt.Println("kuberush stopped")
}
