package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Scraper struct {
	client kubernetes.Interface
}

func NewScraper(client kubernetes.Interface) *Scraper {
	return &Scraper{client: client}
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
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[metrics] scrape %s/%s error: %v\n", pod.Namespace, pod.Name, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
		fmt.Printf("[metrics] scraped %s/%s (%d bytes)\n", pod.Namespace, pod.Name, len(body))
		// TODO: parse and export metrics via sinks/exporters
		_ = body
	}
}
