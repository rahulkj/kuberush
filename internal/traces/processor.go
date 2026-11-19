package traces

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/rahulkj/kubemon/internal/exporter"
	"github.com/rahulkj/kubemon/internal/selfmetrics"
)

type Processor struct {
	enricher *Enricher
	exporter *exporter.OTLPTracesExporter
	endpoint string
	metrics  *selfmetrics.Metrics
	client   *http.Client
}

func NewProcessor(enricher *Enricher, exp *exporter.OTLPTracesExporter, endpoint string, sm *selfmetrics.Metrics) *Processor {
	return &Processor{
		enricher: enricher,
		exporter: exp,
		endpoint: endpoint,
		metrics:  sm,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Handle processes an incoming OTLP traces payload. It attempts protobuf decode+enrich+export;
// if protobuf decoding fails, it falls back to raw forwarding when an endpoint is configured.
func (p *Processor) Handle(ctx context.Context, body []byte, contentType string) error {
	if len(body) == 0 {
		return nil
	}
	if p == nil {
		return nil
	}

	req := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(body, req); err == nil {
		if p.enricher != nil {
			p.enricher.Enrich(req)
		}
		if p.exporter != nil {
			return p.exporter.Export(ctx, req)
		}
		if p.endpoint != "" {
			return p.forwardRaw(ctx, body, "application/x-protobuf")
		}
		return nil
	}

	// Fallback for non-protobuf payloads (e.g. JSON): forward raw without enrichment if configured.
	if p.endpoint != "" {
		ct := contentType
		if ct == "" {
			ct = "application/x-protobuf"
		}
		return p.forwardRaw(ctx, body, ct)
	}
	return fmt.Errorf("unable to decode otlp traces payload and no forward endpoint configured")
}

func (p *Processor) forwardRaw(ctx context.Context, body []byte, contentType string) error {
	url := strings.TrimRight(p.endpoint, "/") + "/v1/traces"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		if p.metrics != nil {
			p.metrics.IncOTLPForwardError("traces")
		}
		return fmt.Errorf("create traces forward request: %w", err)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if p.metrics != nil {
			p.metrics.IncOTLPForwardError("traces")
		}
		return fmt.Errorf("forward traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		if p.metrics != nil {
			p.metrics.IncOTLPForwardError("traces")
		}
		return fmt.Errorf("forward traces failed: status %s", resp.Status)
	}
	return nil
}

func (p *Processor) Close() error {
	if p != nil && p.exporter != nil {
		return p.exporter.Close()
	}
	return nil
}
