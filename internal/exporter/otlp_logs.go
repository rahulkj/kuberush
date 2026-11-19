package exporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

type OTLPLogsExporter struct {
	endpoint string
	client   *http.Client
}

func NewOTLPLogsExporter(endpoint string) *OTLPLogsExporter {
	return &OTLPLogsExporter{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *OTLPLogsExporter) Name() string { return "otlp-http-logs-proto" }

func (e *OTLPLogsExporter) WriteLogs(ctx context.Context, records []LogRecord) error {
	if len(records) == 0 {
		return nil
	}
	// Build a simple OTLP logs payload
	scopeLogs := &logspb.ScopeLogs{
		Scope:      &commonpb.InstrumentationScope{Name: "kuberush"},
		LogRecords: []*logspb.LogRecord{},
	}
	for _, r := range records {
		attrs := []*commonpb.KeyValue{
			{Key: "k8s.namespace.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Namespace}}},
			{Key: "k8s.pod.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Pod}}},
			{Key: "k8s.container.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Container}}},
		}
		if r.NodeName != "" {
			attrs = append(attrs, &commonpb.KeyValue{Key: "k8s.node.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.NodeName}}})
		}
		lr := &logspb.LogRecord{
			TimeUnixNano: uint64(time.Now().UnixNano()),
			Body: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: r.Message},
			},
			Attributes: attrs,
		}
		scopeLogs.LogRecords = append(scopeLogs.LogRecords, lr)
	}
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "kuberush"}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{scopeLogs},
			},
		},
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal otlp logs proto: %w", err)
	}
	url := e.endpoint + "/v1/logs"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post otlp logs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp logs export failed: status %s", resp.Status)
	}
	return nil
}

func (e *OTLPLogsExporter) Close() error { return nil }
