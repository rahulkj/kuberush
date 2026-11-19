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

	// Start pod discovery (all namespaces)
	pw, err := discovery.NewPodWatcher(kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create pod watcher: %v\n", err)
	} else {
		go func() {
			if err := pw.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "pod watcher error: %v\n", err)
			}
		}()
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
