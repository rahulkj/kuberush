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

type GCPCloudLoggingExporter struct {
	endpoint    string
	projectID   string
	logName     string
	accessToken string
	headers     map[string]string
	client      *http.Client
}

func NewGCPCloudLoggingExporter(endpoint, projectID, logName, accessToken string, headers map[string]string) *GCPCloudLoggingExporter {
	ep := strings.TrimRight(endpoint, "/")
	if ep == "" {
		ep = "https://logging.googleapis.com"
	}
	h := map[string]string{}
	for k, v := range headers {
		h[k] = v
	}
	return &GCPCloudLoggingExporter{
		endpoint:    ep,
		projectID:   projectID,
		logName:     logName,
		accessToken: accessToken,
		headers:     h,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *GCPCloudLoggingExporter) Name() string { return "gcp-cloud-logging" }

func (e *GCPCloudLoggingExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	logName := e.logName
	if logName == "" && e.projectID != "" {
		logName = fmt.Sprintf("projects/%s/logs/kuberush", e.projectID)
	}
	if logName == "" {
		return fmt.Errorf("gcp cloud logging requires log_name or project_id")
	}

	entries := make([]map[string]any, 0, len(records))
	for _, r := range records {
		entries = append(entries, map[string]any{
			"logName":     logName,
			"severity":    "DEFAULT",
			"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
			"textPayload": strings.TrimRight(r.Message, "\n"),
			"labels": map[string]string{
				"k8s_namespace": r.Namespace,
				"k8s_pod":       r.Pod,
				"k8s_container": r.Container,
				"k8s_node":      r.NodeName,
			},
			"jsonPayload": map[string]any{
				"message":   r.Message,
				"podLabels": r.PodLabels,
			},
		})
	}
	body := map[string]any{
		"entries": entries,
	}
	if e.projectID != "" {
		body["logName"] = logName
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal gcp logging request: %w", err)
	}
	url := e.endpoint + "/v2/entries:write"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create gcp logging request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.accessToken)
	}
	for k, v := range e.headers {
		if k != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post gcp cloud logging: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gcp cloud logging export failed: status %s", resp.Status)
	}
	return nil
}

func (e *GCPCloudLoggingExporter) Close() error { return nil }
