package metrics

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"

	"github.com/rahulkj/kubemon/internal/config"
)

type TargetInfo struct {
	Namespace string
	Pod       string
	NodeName  string
	URL       string
}

// ConvertPrometheusToOTLP converts Prometheus exposition text/format metrics into an OTLP metrics export request.
// It supports counters, gauges, untyped, summaries, and histograms on a best-effort basis.
func ConvertPrometheusToOTLP(body []byte, target TargetInfo, scrapeTime time.Time, cfg config.MetricsConfig) (*colmetricspb.ExportMetricsServiceRequest, error) {
	parser := expfmt.TextParser{}
	mfs, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse prometheus metrics: %w", err)
	}

	ts := uint64(scrapeTime.UnixNano())
	baseAttrs := targetAttributes(target)
	var out []*metricspb.Metric
	remainingSeries := cfg.MaxSeriesPerScrape
	if remainingSeries <= 0 {
		remainingSeries = int(^uint(0) >> 1)
	}

	names := make([]string, 0, len(mfs))
	for n := range mfs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		if !metricNameAllowed(name, cfg) {
			continue
		}
		mf := mfs[name]
		if mf == nil {
			continue
		}
		m, used := convertFamily(name, mf, ts, baseAttrs, cfg, remainingSeries)
		if m != nil {
			out = append(out, m)
			remainingSeries -= used
			if remainingSeries <= 0 {
				break
			}
		}
	}

	if len(out) == 0 {
		return nil, nil
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
						Scope:   &commonpb.InstrumentationScope{Name: "kuberush.metrics.scraper.prometheus"},
						Metrics: out,
					},
				},
			},
		},
	}, nil
}

func metricNameAllowed(name string, cfg config.MetricsConfig) bool {
	if len(cfg.AllowMetricRegex) > 0 {
		ok := false
		for _, p := range cfg.AllowMetricRegex {
			if p == "" {
				continue
			}
			if re, err := regexp.Compile(p); err == nil && re.MatchString(name) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, p := range cfg.DropMetricRegex {
		if p == "" {
			continue
		}
		if re, err := regexp.Compile(p); err == nil && re.MatchString(name) {
			return false
		}
	}
	return true
}

func labelAllowed(key string, cfg config.MetricsConfig) bool {
	if key == "" {
		return false
	}
	if len(cfg.AllowLabelRegex) > 0 {
		ok := false
		for _, p := range cfg.AllowLabelRegex {
			if p == "" {
				continue
			}
			if re, err := regexp.Compile(p); err == nil && re.MatchString(key) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, p := range cfg.DropLabelRegex {
		if p == "" {
			continue
		}
		if re, err := regexp.Compile(p); err == nil && re.MatchString(key) {
			return false
		}
	}
	return true
}

func targetAttributes(target TargetInfo) []*commonpb.KeyValue {
	attrs := []*commonpb.KeyValue{
		{Key: "k8s.namespace.name", Value: strValue(target.Namespace)},
		{Key: "k8s.pod.name", Value: strValue(target.Pod)},
		{Key: "target.url", Value: strValue(target.URL)},
	}
	if target.NodeName != "" {
		attrs = append(attrs, &commonpb.KeyValue{Key: "k8s.node.name", Value: strValue(target.NodeName)})
	}
	return attrs
}

func convertFamily(name string, mf *dto.MetricFamily, ts uint64, baseAttrs []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	if limit <= 0 {
		return nil, 0
	}
	switch mf.GetType() {
	case dto.MetricType_GAUGE:
		return gaugeMetric(name, mf, ts, baseAttrs, cfg, limit)
	case dto.MetricType_COUNTER:
		return counterMetric(name, mf, ts, baseAttrs, cfg, limit)
	case dto.MetricType_UNTYPED:
		return untypedAsGaugeMetric(name, mf, ts, baseAttrs, cfg, limit)
	case dto.MetricType_SUMMARY:
		return summaryMetric(name, mf, ts, baseAttrs, cfg, limit)
	case dto.MetricType_HISTOGRAM:
		return histogramMetric(name, mf, ts, baseAttrs, cfg, limit)
	default:
		return nil, 0
	}
}

func metricBase(name string, mf *dto.MetricFamily) *metricspb.Metric {
	m := &metricspb.Metric{Name: name}
	if mf.GetHelp() != "" {
		m.Description = mf.GetHelp()
	}
	if mf.GetUnit() != "" {
		m.Unit = mf.GetUnit()
	}
	return m
}

func dataPointAttrs(base []*commonpb.KeyValue, labels []*dto.LabelPair, cfg config.MetricsConfig) []*commonpb.KeyValue {
	if len(labels) == 0 {
		return base
	}
	attrs := make([]*commonpb.KeyValue, 0, len(base)+len(labels))
	attrs = append(attrs, base...)
	for _, lp := range labels {
		if lp == nil || lp.GetName() == "" {
			continue
		}
		if !labelAllowed(lp.GetName(), cfg) {
			continue
		}
		attrs = append(attrs, &commonpb.KeyValue{Key: lp.GetName(), Value: strValue(lp.GetValue())})
	}
	return attrs
}

func numberPoint(ts uint64, attrs []*commonpb.KeyValue, v float64) *metricspb.NumberDataPoint {
	dp := &metricspb.NumberDataPoint{TimeUnixNano: ts, Attributes: attrs}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		dp.Value = &metricspb.NumberDataPoint_AsDouble{AsDouble: v}
		return dp
	}
	if v == math.Trunc(v) && v >= math.MinInt64 && v <= math.MaxInt64 {
		dp.Value = &metricspb.NumberDataPoint_AsDouble{AsDouble: v}
		return dp
	}
	dp.Value = &metricspb.NumberDataPoint_AsDouble{AsDouble: v}
	return dp
}

func gaugeMetric(name string, mf *dto.MetricFamily, ts uint64, base []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	var pts []*metricspb.NumberDataPoint
	for _, m := range mf.Metric {
		if len(pts) >= limit {
			break
		}
		if m == nil || m.Gauge == nil {
			continue
		}
		pts = append(pts, numberPoint(ts, dataPointAttrs(base, m.Label, cfg), m.Gauge.GetValue()))
	}
	if len(pts) == 0 {
		return nil, 0
	}
	out := metricBase(name, mf)
	out.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: pts}}
	return out, len(pts)
}

func untypedAsGaugeMetric(name string, mf *dto.MetricFamily, ts uint64, base []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	var pts []*metricspb.NumberDataPoint
	for _, m := range mf.Metric {
		if len(pts) >= limit {
			break
		}
		if m == nil || m.Untyped == nil {
			continue
		}
		pts = append(pts, numberPoint(ts, dataPointAttrs(base, m.Label, cfg), m.Untyped.GetValue()))
	}
	if len(pts) == 0 {
		return nil, 0
	}
	out := metricBase(name, mf)
	out.Data = &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{DataPoints: pts}}
	return out, len(pts)
}

func counterMetric(name string, mf *dto.MetricFamily, ts uint64, base []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	var pts []*metricspb.NumberDataPoint
	for _, m := range mf.Metric {
		if len(pts) >= limit {
			break
		}
		if m == nil || m.Counter == nil {
			continue
		}
		pts = append(pts, numberPoint(ts, dataPointAttrs(base, m.Label, cfg), m.Counter.GetValue()))
	}
	if len(pts) == 0 {
		return nil, 0
	}
	out := metricBase(name, mf)
	out.Data = &metricspb.Metric_Sum{Sum: &metricspb.Sum{
		AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		IsMonotonic:            true,
		DataPoints:             pts,
	}}
	return out, len(pts)
}

func summaryMetric(name string, mf *dto.MetricFamily, ts uint64, base []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	var pts []*metricspb.SummaryDataPoint
	for _, m := range mf.Metric {
		if len(pts) >= limit {
			break
		}
		if m == nil || m.Summary == nil {
			continue
		}
		s := m.Summary
		var qv []*metricspb.SummaryDataPoint_ValueAtQuantile
		for _, q := range s.Quantile {
			if q == nil {
				continue
			}
			qv = append(qv, &metricspb.SummaryDataPoint_ValueAtQuantile{Quantile: q.GetQuantile(), Value: q.GetValue()})
		}
		pts = append(pts, &metricspb.SummaryDataPoint{
			TimeUnixNano:   ts,
			Attributes:     dataPointAttrs(base, m.Label, cfg),
			Count:          s.GetSampleCount(),
			Sum:            s.GetSampleSum(),
			QuantileValues: qv,
		})
	}
	if len(pts) == 0 {
		return nil, 0
	}
	out := metricBase(name, mf)
	out.Data = &metricspb.Metric_Summary{Summary: &metricspb.Summary{DataPoints: pts}}
	return out, len(pts)
}

func histogramMetric(name string, mf *dto.MetricFamily, ts uint64, base []*commonpb.KeyValue, cfg config.MetricsConfig, limit int) (*metricspb.Metric, int) {
	var pts []*metricspb.HistogramDataPoint
	for _, m := range mf.Metric {
		if len(pts) >= limit {
			break
		}
		if m == nil || m.Histogram == nil {
			continue
		}
		h := m.Histogram
		buckets := h.Bucket
		counts := make([]uint64, 0, len(buckets)+1)
		bounds := make([]float64, 0, len(buckets))
		var prev uint64
		for _, b := range buckets {
			if b == nil {
				continue
			}
			upper := b.GetUpperBound()
			c := b.GetCumulativeCount()
			if c >= prev {
				counts = append(counts, c-prev)
			} else {
				counts = append(counts, c)
			}
			prev = c
			if !math.IsInf(upper, 1) {
				bounds = append(bounds, upper)
			}
		}
		if h.GetSampleCount() >= prev {
			counts = append(counts, h.GetSampleCount()-prev)
		}
		dp := &metricspb.HistogramDataPoint{
			TimeUnixNano:   ts,
			Attributes:     dataPointAttrs(base, m.Label, cfg),
			Count:          h.GetSampleCount(),
			BucketCounts:   counts,
			ExplicitBounds: bounds,
		}
		if h.SampleSum != nil {
			dp.Sum = ptrFloat64(h.GetSampleSum())
		}
		pts = append(pts, dp)
	}
	if len(pts) == 0 {
		return nil, 0
	}
	out := metricBase(name, mf)
	out.Data = &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
		AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
		DataPoints:             pts,
	}}
	return out, len(pts)
}

func ptrFloat64(v float64) *float64 { return &v }

func strValue(v string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: strings.Clone(v)}}
}
