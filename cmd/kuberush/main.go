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
	flag.StringVar(&configPath, "config", "", "Path to config YAML file (optional)")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (optional; defaults to in-cluster or KUBECONFIG/default)")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
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
		baseSinks = append(baseSinks, exporter.NewOTLPLogsExporter(cfg.Exporters.OTLPLogs.Endpoint))
	}
	if cfg.Exporters.Loki.Enabled {
		baseSinks = append(baseSinks, exporter.NewLokiLogsExporter(cfg.Exporters.Loki.Endpoint))
	}
	if len(baseSinks) == 0 {
		baseSinks = append(baseSinks, exporter.NewStdoutLogSink()) // default fallback
	}
	var logSinks []exporter.LogSink
	flushInterval := time.Duration(cfg.LogPipeline.FlushIntervalMS) * time.Millisecond
	for _, s := range baseSinks {
		bs := exporter.NewBufferedLogSink(s, cfg.LogPipeline.BatchSize, flushInterval, cfg.LogPipeline.QueueSize)
		logSinks = append(logSinks, bs)
		fmt.Printf("exporter initialized: log sink=%s (buffered batch=%d flush=%s queue=%d)\n", s.Name(), cfg.LogPipeline.BatchSize, flushInterval, cfg.LogPipeline.QueueSize)
	}

	var metricsExporter *exporter.OTLPMetricsExporter
	if cfg.Exporters.OTLPMetrics.Enabled {
		metricsExporter = exporter.NewOTLPMetricsExporter(cfg.Exporters.OTLPMetrics.Endpoint)
		fmt.Printf("exporter initialized: metrics sink=%s\n", metricsExporter.Name())
	}

	// Build k8s client and optional components (discovery/logs/metrics scrape/trace enrichment)
	var podWatcher *discovery.PodWatcher
	_, clientset, err := kube.BuildClientset(kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kube client: %v\n", err)
	} else {
		logCollector := logs.NewCollector(clientset, logSinks, sm)
		podWatcher = discovery.NewPodWatcher(clientset, logCollector, cfg.Kubernetes)
	}

	var traceProcessor *traces.Processor
	if cfg.Exporters.OTLPTraces.Enabled || cfg.Exporters.OTLPTraces.Endpoint != "" {
		var enricher *traces.Enricher
		if podWatcher != nil {
			enricher = traces.NewEnricher(podWatcher)
		}
		var traceExporter *exporter.OTLPTracesExporter
		if cfg.Exporters.OTLPTraces.Enabled {
			traceExporter = exporter.NewOTLPTracesExporter(cfg.Exporters.OTLPTraces.Endpoint)
			fmt.Printf("exporter initialized: traces sink=%s\n", traceExporter.Name())
		}
		traceProcessor = traces.NewProcessor(enricher, traceExporter, cfg.Exporters.OTLPTraces.Endpoint, sm)
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
		// Metrics scraper (minimal)
		metricsScraper := metrics.NewScraper(clientset, logSinks, metricsExporter, cfg.Kubernetes, sm)
		go metricsScraper.Run(ctx)
		if metricsExporter != nil {
			fmt.Println("metrics scraper started (annotation-based, every 30s, exporting scrape metrics via OTLP and summaries to log sinks)")
		} else {
			fmt.Println("metrics scraper started (annotation-based, every 30s, shipping summaries to log sinks)")
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

	fmt.Println("kuberush stopped")
}
