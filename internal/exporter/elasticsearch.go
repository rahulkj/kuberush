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

type ElasticsearchExporter struct {
	endpoint string
	index    string
	username string
	password string
	client   *http.Client
}

func NewElasticsearchExporter(endpoint, index, username, password string) *ElasticsearchExporter {
	return &ElasticsearchExporter{
		endpoint: strings.TrimRight(endpoint, "/"),
		index:    index,
		username: username,
		password: password,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *ElasticsearchExporter) Name() string { return "elasticsearch-bulk" }

func (e *ElasticsearchExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	index := e.index
	if index == "" {
		index = "kuberush-logs"
	}
	var buf bytes.Buffer
	for _, r := range records {
		meta := map[string]any{"index": map[string]any{"_index": index}}
		doc := map[string]any{
			"@timestamp":         time.Now().UTC().Format(time.RFC3339Nano),
			"message":            r.Message,
			"k8s.namespace.name": r.Namespace,
			"k8s.pod.name":       r.Pod,
			"k8s.container.name": r.Container,
			"k8s.node.name":      r.NodeName,
			"k8s.pod.labels":     r.PodLabels,
		}
		mb, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal es bulk meta: %w", err)
		}
		db, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("marshal es document: %w", err)
		}
		buf.Write(mb)
		buf.WriteByte('\n')
		buf.Write(db)
		buf.WriteByte('\n')
	}

	url := e.endpoint + "/_bulk"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return fmt.Errorf("create elasticsearch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if e.username != "" {
		req.SetBasicAuth(e.username, e.password)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post elasticsearch bulk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("elasticsearch export failed: status %s", resp.Status)
	}
	return nil
}

func (e *ElasticsearchExporter) Close() error { return nil }
