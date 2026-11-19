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

type LokiLogsExporter struct {
	endpoint string
	client   *http.Client
}

func NewLokiLogsExporter(endpoint string) *LokiLogsExporter {
	return &LokiLogsExporter{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *LokiLogsExporter) Name() string { return "loki-http" }

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func (e *LokiLogsExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}

	streams := map[string]*lokiStream{}
	order := []string{}
	for _, r := range records {
		labels := map[string]string{
			"job":       "kuberush",
			"namespace": r.Namespace,
			"pod":       r.Pod,
			"container": r.Container,
		}
		if r.NodeName != "" {
			labels["node"] = r.NodeName
		}
		key := labels["namespace"] + "|" + labels["pod"] + "|" + labels["container"] + "|" + labels["node"]
		s, ok := streams[key]
		if !ok {
			lblCopy := make(map[string]string, len(labels))
			for k, v := range labels {
				lblCopy[k] = v
			}
			s = &lokiStream{Stream: lblCopy, Values: make([][2]string, 0, 4)}
			streams[key] = s
			order = append(order, key)
		}
		ts := fmt.Sprintf("%d", time.Now().UnixNano())
		s.Values = append(s.Values, [2]string{ts, r.Message})
	}

	reqBody := lokiPushRequest{Streams: make([]lokiStream, 0, len(order))}
	for _, k := range order {
		reqBody.Streams = append(reqBody.Streams, *streams[k])
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal loki push request: %w", err)
	}

	url := e.endpoint
	if !strings.HasSuffix(url, "/loki/api/v1/push") {
		url += "/loki/api/v1/push"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post loki logs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("loki export failed: status %s", resp.Status)
	}
	return nil
}

func (e *LokiLogsExporter) Close() error { return nil }
