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

	// Start health/metrics HTTP server
	hSrv := health.NewServer(cfg.Health.ListenAddress)
	go func() {
		if err := hSrv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "health server error: %v\n", err)
		}
	}()

	fmt.Printf("kuberush starting (health on %s)\n", cfg.Health.ListenAddress)

	// Exporter pipeline (ADR 0001): configure sinks
	var logSinks []exporter.LogSink
	if cfg.Exporters.Stdout {
		logSinks = append(logSinks, exporter.NewStdoutLogSink())
	}
	if cfg.Exporters.OTLPLogs.Enabled {
		logSinks = append(logSinks, exporter.NewOTLPLogsExporter(cfg.Exporters.OTLPLogs.Endpoint))
	}
	if len(logSinks) == 0 {
		logSinks = append(logSinks, exporter.NewStdoutLogSink()) // default fallback
	}
	for _, s := range logSinks {
		fmt.Printf("exporter initialized: log sink=%s\n", s.Name())
	}

	// Build k8s client and start log collector + pod discovery
	_, clientset, err := kube.BuildClientset(kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kube client: %v\n", err)
	} else {
		logCollector := logs.NewCollector(clientset, logSinks)
		pw := discovery.NewPodWatcher(clientset, logCollector)
		go func() {
			if err := pw.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "pod watcher error: %v\n", err)
			}
		}()
		// Metrics scraper (minimal)
		metricsScraper := metrics.NewScraper(clientset)
		go metricsScraper.Run(ctx)
		fmt.Println("metrics scraper started (annotation-based, every 30s)")
	}

	<-ctx.Done()
	fmt.Println("kuberush shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hSrv.Shutdown(shutdownCtx)
	for _, s := range logSinks {
		_ = s.Close()
	}

	fmt.Println("kuberush stopped")
}
