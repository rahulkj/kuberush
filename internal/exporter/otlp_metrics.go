package exporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

type OTLPMetricsExporter struct {
	endpoint    string
	compression string
	headers     map[string]string
	client      *http.Client
}

func NewOTLPMetricsExporter(endpoint, compression string, headers map[string]string) *OTLPMetricsExporter {
	cp := map[string]string{}
	for k, v := range headers {
		cp[k] = v
	}
	return &OTLPMetricsExporter{
		endpoint:    endpoint,
		compression: compression,
		headers:     cp,
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (e *OTLPMetricsExporter) Name() string { return "otlp-http-metrics-proto" }

func (e *OTLPMetricsExporter) Export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) error {
	if req == nil {
		return nil
	}
	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal otlp metrics proto: %w", err)
	}
	url := strings.TrimRight(e.endpoint, "/") + "/v1/metrics"
	body, encoding, err := maybeGzip(data, e.compression)
	if err != nil {
		return fmt.Errorf("compress otlp metrics: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	if encoding != "" {
		httpReq.Header.Set("Content-Encoding", encoding)
	}
	for k, v := range e.headers {
		if k != "" {
			httpReq.Header.Set(k, v)
		}
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("post otlp metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("otlp metrics export failed: status %s", resp.Status)
	}
	return nil
}

func (e *OTLPMetricsExporter) Close() error { return nil }

// NewScrapeMetricsRequest builds a minimal OTLP metrics payload for a single Prometheus scrape attempt.
func NewScrapeMetricsRequest(namespace, pod, nodeName, targetURL string, scrapeDuration time.Duration, responseBytes int64, success bool) *colmetricspb.ExportMetricsServiceRequest {
	now := uint64(time.Now().UnixNano())
	start := uint64(time.Now().Add(-scrapeDuration).UnixNano())
	attrs := []*commonpb.KeyValue{
		{Key: "k8s.namespace.name", Value: strValue(namespace)},
		{Key: "k8s.pod.name", Value: strValue(pod)},
		{Key: "target.url", Value: strValue(targetURL)},
	}
	if nodeName != "" {
		attrs = append(attrs, &commonpb.KeyValue{Key: "k8s.node.name", Value: strValue(nodeName)})
	}

	successVal := 0.0
	if success {
		successVal = 1.0
	}

	metrics := []*metricspb.Metric{
		{
			Name:        "kuberush.scrape.duration_ms",
			Description: "Duration of a Prometheus scrape attempt in milliseconds",
			Unit:        "ms",
			Data: &metricspb.Metric_Gauge{
				Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{
						{
							StartTimeUnixNano: start,
							TimeUnixNano:      now,
							Attributes:        attrs,
							Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: float64(scrapeDuration.Milliseconds())},
						},
					},
				},
			},
		},
		{
			Name:        "kuberush.scrape.response_bytes",
			Description: "Bytes returned by a Prometheus scrape endpoint",
			Unit:        "By",
			Data: &metricspb.Metric_Gauge{
				Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{
						{
							StartTimeUnixNano: start,
							TimeUnixNano:      now,
							Attributes:        attrs,
							Value:             &metricspb.NumberDataPoint_AsInt{AsInt: responseBytes},
						},
					},
				},
			},
		},
		{
			Name:        "kuberush.scrape.up",
			Description: "Whether a Prometheus scrape attempt succeeded (1) or failed (0)",
			Unit:        "1",
			Data: &metricspb.Metric_Gauge{
				Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{
						{
							StartTimeUnixNano: start,
							TimeUnixNano:      now,
							Attributes:        attrs,
							Value:             &metricspb.NumberDataPoint_AsDouble{AsDouble: successVal},
						},
					},
				},
			},
		},
	}

	return &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: strValue("kuberush")},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope:   &commonpb.InstrumentationScope{Name: "kuberush.metrics.scraper"},
						Metrics: metrics,
					},
				},
			},
		},
	}
}

func strValue(v string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}
}
