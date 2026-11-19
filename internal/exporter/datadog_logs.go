package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DatadogLogsExporter struct {
	endpoint string
	apiKey   string
	service  string
	client   *http.Client
}

func NewDatadogLogsExporter(endpoint, site, apiKey, service string) *DatadogLogsExporter {
	ep := strings.TrimRight(endpoint, "/")
	if ep == "" {
		s := strings.TrimSpace(site)
		if s == "" {
			s = "datadoghq.com"
		}
		ep = "https://http-intake.logs." + s + "/api/v2/logs"
	}
	return &DatadogLogsExporter{
		endpoint: ep,
		apiKey:   apiKey,
		service:  service,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *DatadogLogsExporter) Name() string { return "datadog-logs" }

func (e *DatadogLogsExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	payload := make([]map[string]any, 0, len(records))
	for _, r := range records {
		item := map[string]any{
			"ddsource": "kuberush",
			"service":  e.service,
			"hostname": r.NodeName,
			"message":  r.Message,
			"kubernetes": map[string]any{
				"namespace_name": r.Namespace,
				"pod_name":       r.Pod,
				"container_name": r.Container,
				"node_name":      r.NodeName,
				"pod_labels":     r.PodLabels,
			},
		}
		if e.service == "" {
			item["service"] = "kuberush"
		}
		payload = append(payload, item)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal datadog logs: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create datadog request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("DD-API-KEY", e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post datadog logs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("datadog logs export failed: status %s", resp.Status)
	}
	return nil
}

func (e *DatadogLogsExporter) Close() error { return nil }
