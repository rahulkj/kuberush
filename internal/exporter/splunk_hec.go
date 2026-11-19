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

type SplunkHECExporter struct {
	endpoint   string
	token      string
	source     string
	sourcetype string
	index      string
	client     *http.Client
}

func NewSplunkHECExporter(endpoint, token, source, sourcetype, index string) *SplunkHECExporter {
	return &SplunkHECExporter{
		endpoint:   strings.TrimRight(endpoint, "/"),
		token:      token,
		source:     source,
		sourcetype: sourcetype,
		index:      index,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *SplunkHECExporter) Name() string { return "splunk-hec" }

func (e *SplunkHECExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range records {
		event := map[string]any{
			"event": map[string]any{
				"message":   r.Message,
				"namespace": r.Namespace,
				"pod":       r.Pod,
				"container": r.Container,
				"node":      r.NodeName,
				"labels":    r.PodLabels,
			},
			"time": float64(time.Now().UnixNano()) / 1e9,
		}
		if e.source != "" {
			event["source"] = e.source
		}
		if e.sourcetype != "" {
			event["sourcetype"] = e.sourcetype
		}
		if e.index != "" {
			event["index"] = e.index
		}
		event["host"] = r.NodeName
		if err := enc.Encode(event); err != nil {
			return fmt.Errorf("encode splunk event: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return fmt.Errorf("create splunk request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Splunk "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post splunk hec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("splunk hec export failed: status %s", resp.Status)
	}
	return nil
}

func (e *SplunkHECExporter) Close() error { return nil }
