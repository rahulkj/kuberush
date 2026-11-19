package exporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

type PrometheusRemoteWriteExporter struct {
	endpoint string
	client   *http.Client
}

func NewPrometheusRemoteWriteExporter(endpoint string) *PrometheusRemoteWriteExporter {
	return &PrometheusRemoteWriteExporter{
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *PrometheusRemoteWriteExporter) Name() string { return "prometheus-remote-write" }

func (e *PrometheusRemoteWriteExporter) Export(ctx context.Context, req *prompb.WriteRequest) error {
	if req == nil || len(req.Timeseries) == 0 {
		return nil
	}
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("marshal remote write request: %w", err)
	}
	compressed := snappy.Encode(nil, data)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("create remote write request: %w", err)
	}
	httpReq.Header.Set("Content-Encoding", "snappy")
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post remote write: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("prometheus remote write failed: status %s", resp.Status)
	}
	return nil
}

func (e *PrometheusRemoteWriteExporter) Close() error { return nil }
