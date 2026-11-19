package metrics

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/prometheus/prompb"

	"github.com/rahulkj/kubemon/internal/config"
)

// ConvertPrometheusToRemoteWrite converts Prometheus exposition metrics into a Remote Write request.
// It emits sample series for counters/gauges/untyped and histogram/summary components on a best-effort basis.
func ConvertPrometheusToRemoteWrite(body []byte, target TargetInfo, scrapeTime time.Time, cfg config.MetricsConfig) (*prompb.WriteRequest, error) {
	parser := expfmt.TextParser{}
	mfs, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse prometheus metrics: %w", err)
	}

	ts := scrapeTime.UnixMilli()
	base := []prompb.Label{
		{Name: "k8s_namespace_name", Value: target.Namespace},
		{Name: "k8s_pod_name", Value: target.Pod},
		{Name: "target_url", Value: target.URL},
	}
	if target.NodeName != "" {
		base = append(base, prompb.Label{Name: "k8s_node_name", Value: target.NodeName})
	}

	remaining := cfg.MaxSeriesPerScrape
	if remaining <= 0 {
		remaining = int(^uint(0) >> 1)
	}

	names := make([]string, 0, len(mfs))
	for n := range mfs {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []prompb.TimeSeries
	for _, name := range names {
		if !metricNameAllowed(name, cfg) {
			continue
		}
		mf := mfs[name]
		if mf == nil {
			continue
		}
		series, used := familyToRemoteWrite(name, mf, ts, base, cfg, remaining)
		if len(series) > 0 {
			out = append(out, series...)
			remaining -= used
			if remaining <= 0 {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &prompb.WriteRequest{Timeseries: out}, nil
}

func familyToRemoteWrite(name string, mf *dto.MetricFamily, ts int64, base []prompb.Label, cfg config.MetricsConfig, limit int) ([]prompb.TimeSeries, int) {
	if limit <= 0 {
		return nil, 0
	}
	var series []prompb.TimeSeries
	add := func(lbls []prompb.Label, v float64) bool {
		if len(series) >= limit {
			return false
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			// Prometheus remote write samples can carry float values including NaN/Inf in some cases, but keep as-is
		}
		series = append(series, prompb.TimeSeries{
			Labels:  lbls,
			Samples: []prompb.Sample{{Timestamp: ts, Value: v}},
		})
		return true
	}

	for _, m := range mf.Metric {
		if len(series) >= limit {
			break
		}
		if m == nil {
			continue
		}
		lblsBase := remoteWriteLabels(name, base, m.Label, cfg)
		switch mf.GetType() {
		case dto.MetricType_GAUGE:
			if m.Gauge != nil {
				add(lblsBase, m.Gauge.GetValue())
			}
		case dto.MetricType_COUNTER:
			if m.Counter != nil {
				add(lblsBase, m.Counter.GetValue())
			}
		case dto.MetricType_UNTYPED:
			if m.Untyped != nil {
				add(lblsBase, m.Untyped.GetValue())
			}
		case dto.MetricType_SUMMARY:
			if m.Summary != nil {
				s := m.Summary
				if !add(withName(lblsBase, name+"_sum"), s.GetSampleSum()) {
					break
				}
				if len(series) >= limit || !add(withName(lblsBase, name+"_count"), float64(s.GetSampleCount())) {
					break
				}
				for _, q := range s.Quantile {
					if q == nil || len(series) >= limit {
						break
					}
					ql := withLabel(withName(lblsBase, name), "quantile", fmt.Sprintf("%g", q.GetQuantile()))
					add(ql, q.GetValue())
				}
			}
		case dto.MetricType_HISTOGRAM:
			if m.Histogram != nil {
				h := m.Histogram
				if !add(withName(lblsBase, name+"_sum"), h.GetSampleSum()) {
					break
				}
				if len(series) >= limit || !add(withName(lblsBase, name+"_count"), float64(h.GetSampleCount())) {
					break
				}
				for _, b := range h.Bucket {
					if b == nil || len(series) >= limit {
						break
					}
					le := "+Inf"
					if !math.IsInf(b.GetUpperBound(), 1) {
						le = fmt.Sprintf("%g", b.GetUpperBound())
					}
					bl := withLabel(withName(lblsBase, name+"_bucket"), "le", le)
					add(bl, float64(b.GetCumulativeCount()))
				}
			}
		}
	}
	return series, len(series)
}

func remoteWriteLabels(metricName string, base []prompb.Label, labels []*dto.LabelPair, cfg config.MetricsConfig) []prompb.Label {
	out := make([]prompb.Label, 0, len(base)+len(labels)+1)
	out = append(out, prompb.Label{Name: "__name__", Value: metricName})
	out = append(out, base...)
	for _, lp := range labels {
		if lp == nil || lp.GetName() == "" {
			continue
		}
		if !labelAllowed(lp.GetName(), cfg) {
			continue
		}
		out = append(out, prompb.Label{Name: lp.GetName(), Value: lp.GetValue()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func withName(labels []prompb.Label, name string) []prompb.Label {
	out := make([]prompb.Label, 0, len(labels))
	for _, l := range labels {
		if l.Name == "__name__" {
			out = append(out, prompb.Label{Name: "__name__", Value: name})
		} else {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func withLabel(labels []prompb.Label, k, v string) []prompb.Label {
	out := append(append([]prompb.Label(nil), labels...), prompb.Label{Name: k, Value: v})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ensure regexp import used in some builds? not needed directly but kept simple
var _ = regexp.MustCompile
