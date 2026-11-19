package exporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

type OTLPTracesExporter struct {
	endpoint    string
	compression string
	client      *http.Client
}

func NewOTLPTracesExporter(endpoint, compression string) *OTLPTracesExporter {
	return &OTLPTracesExporter{
		endpoint:    endpoint,
		compression: compression,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *OTLPTracesExporter) Name() string { return "otlp-http-traces-proto" }

func (e *OTLPTracesExporter) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	if req == nil {
		return nil
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal otlp traces proto: %w", err)
	}
	url := strings.TrimRight(e.endpoint, "/") + "/v1/traces"
	body, encoding, err := maybeGzip(data, e.compression)
	if err != nil {
		return fmt.Errorf("compress otlp traces: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		httpReq.Header.Set("Content-Encoding", encoding)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post otlp traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp traces export failed: status %s", resp.Status)
	}
	return nil
}

func (e *OTLPTracesExporter) Close() error { return nil }
