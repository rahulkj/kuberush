package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/rahulkj/kubemon/internal/config"
	"github.com/rahulkj/kubemon/internal/exporter"
	"github.com/rahulkj/kubemon/internal/selfmetrics"
)

type Scraper struct {
	client          kubernetes.Interface
	sinks           []exporter.LogSink
	metricsExporter *exporter.OTLPMetricsExporter
	kubeCfg         config.KubernetesConfig
	selfMetrics     *selfmetrics.Metrics
}

func NewScraper(client kubernetes.Interface, sinks []exporter.LogSink, metricsExporter *exporter.OTLPMetricsExporter, kubeCfg config.KubernetesConfig, sm *selfmetrics.Metrics) *Scraper {
	return &Scraper{client: client, sinks: sinks, metricsExporter: metricsExporter, kubeCfg: kubeCfg, selfMetrics: sm}
}

func (s *Scraper) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	// run once immediately
	s.scrapeOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scrapeOnce(ctx)
		}
	}
}

func (s *Scraper) scrapeOnce(ctx context.Context) {
	pods, err := s.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("[metrics] list pods error: %v\n", err)
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, pod := range pods.Items {
		if !s.kubeCfg.PodAllowed(pod.Namespace, pod.Labels) {
			continue
		}
		if pod.Status.Phase != "Running" || pod.Status.PodIP == "" {
			continue
		}
		ann := pod.Annotations
		if ann == nil {
			continue
		}
		if ann["prometheus.io/scrape"] != "true" {
			continue
		}
		port := ann["prometheus.io/port"]
		if port == "" {
			port = "9090"
		}
		path := ann["prometheus.io/path"]
		if path == "" {
			path = "/metrics"
		}
		url := fmt.Sprintf("http://%s:%s%s", pod.Status.PodIP, port, path)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		start := time.Now()
		resp, err := client.Do(req)
		duration := time.Since(start)
		if err != nil {
			fmt.Printf("[metrics] scrape %s/%s error: %v\n", pod.Namespace, pod.Name, err)
			if s.selfMetrics != nil {
				s.selfMetrics.IncScrapeAttempt("failure")
			}
			if s.metricsExporter != nil {
				mreq := exporter.NewScrapeMetricsRequest(pod.Namespace, pod.Name, url, duration, 0, false)
				_ = s.metricsExporter.Export(ctx, mreq)
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
		success := resp.StatusCode < 300
		if s.selfMetrics != nil {
			if success {
				s.selfMetrics.IncScrapeAttempt("success")
			} else {
				s.selfMetrics.IncScrapeAttempt("failure")
			}
		}
		msg := fmt.Sprintf("[metrics] scraped %s/%s url=%s bytes=%d status=%d", pod.Namespace, pod.Name, url, len(body), resp.StatusCode)
		fmt.Println(msg)
		rec := exporter.LogRecord{
			Namespace: pod.Namespace,
			Pod:       pod.Name,
			Container: "metrics-scrape",
			NodeName:  pod.Spec.NodeName,
			Message:   msg,
		}
		for _, sink := range s.sinks {
			_ = sink.WriteLogs(ctx, []exporter.LogRecord{rec})
		}
		if s.metricsExporter != nil {
			mreq := exporter.NewScrapeMetricsRequest(pod.Namespace, pod.Name, url, duration, int64(len(body)), success)
			_ = s.metricsExporter.Export(ctx, mreq)
		}
	}
}
