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

type AzureMonitorLogsExporter struct {
	endpoint    string
	ruleID      string
	streamName  string
	accessToken string
	headers     map[string]string
	client      *http.Client
}

func NewAzureMonitorLogsExporter(endpoint, ruleID, streamName, accessToken string, headers map[string]string) *AzureMonitorLogsExporter {
	h := map[string]string{}
	for k, v := range headers {
		h[k] = v
	}
	return &AzureMonitorLogsExporter{
		endpoint:    strings.TrimRight(endpoint, "/"),
		ruleID:      ruleID,
		streamName:  streamName,
		accessToken: accessToken,
		headers:     h,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *AzureMonitorLogsExporter) Name() string { return "azure-monitor-logs" }

func (e *AzureMonitorLogsExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	if e.endpoint == "" || e.ruleID == "" || e.streamName == "" {
		return fmt.Errorf("azure monitor logs requires endpoint, dcr_immutable_id, and stream_name")
	}

	payload := make([]map[string]any, 0, len(records))
	for _, r := range records {
		payload = append(payload, map[string]any{
			"TimeGenerated":    time.Now().UTC().Format(time.RFC3339Nano),
			"Message":          strings.TrimRight(r.Message, "\n"),
			"Namespace":        r.Namespace,
			"Pod":              r.Pod,
			"Container":        r.Container,
			"NodeName":         r.NodeName,
			"PodLabels":        r.PodLabels,
			"K8sNamespaceName": r.Namespace,
			"K8sPodName":       r.Pod,
			"K8sContainerName": r.Container,
			"K8sNodeName":      r.NodeName,
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal azure monitor logs: %w", err)
	}
	url := fmt.Sprintf("%s/dataCollectionRules/%s/streams/%s?api-version=2023-01-01", e.endpoint, e.ruleID, e.streamName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create azure monitor request: %w", err)
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
		return fmt.Errorf("post azure monitor logs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("azure monitor logs export failed: status %s", resp.Status)
	}
	return nil
}

func (e *AzureMonitorLogsExporter) Close() error { return nil }
