package selfmetrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Metrics struct {
	mu sync.Mutex

	logRecordsTotal      map[string]uint64
	logExportErrorsTotal map[string]uint64
	logTailerErrorsTotal uint64

	scrapeAttemptsTotal map[string]uint64 // success/failure

	otlpIngestTotal  map[string]uint64
	otlpForwardError map[string]uint64
}

func New() *Metrics {
	return &Metrics{
		logRecordsTotal:      make(map[string]uint64),
		logExportErrorsTotal: make(map[string]uint64),
		scrapeAttemptsTotal:  make(map[string]uint64),
		otlpIngestTotal:      make(map[string]uint64),
		otlpForwardError:     make(map[string]uint64),
	}
}

func (m *Metrics) IncLogRecords(sink string, n uint64) {
	m.mu.Lock()
	m.logRecordsTotal[sink] += n
	m.mu.Unlock()
}

func (m *Metrics) IncLogExportErrors(sink string) {
	m.mu.Lock()
	m.logExportErrorsTotal[sink]++
	m.mu.Unlock()
}

func (m *Metrics) IncLogTailerErrors() {
	m.mu.Lock()
	m.logTailerErrorsTotal++
	m.mu.Unlock()
}

func (m *Metrics) IncScrapeAttempt(result string) {
	m.mu.Lock()
	m.scrapeAttemptsTotal[result]++
	m.mu.Unlock()
}

func (m *Metrics) IncOTLPIngest(signal string) {
	m.mu.Lock()
	m.otlpIngestTotal[signal]++
	m.mu.Unlock()
}

func (m *Metrics) IncOTLPForwardError(signal string) {
	m.mu.Lock()
	m.otlpForwardError[signal]++
	m.mu.Unlock()
}

func (m *Metrics) RenderPrometheus() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	writeCounter(&b, "kuberush_logs_records_total", "Total log records written to sinks.", "sink", m.logRecordsTotal)
	writeCounter(&b, "kuberush_log_export_errors_total", "Total log export errors by sink.", "sink", m.logExportErrorsTotal)
	writeSimpleCounter(&b, "kuberush_log_tailer_errors_total", "Total log tailer stream errors.", m.logTailerErrorsTotal)
	writeCounter(&b, "kuberush_scrape_attempts_total", "Total Prometheus scrape attempts by result.", "result", m.scrapeAttemptsTotal)
	writeCounter(&b, "kuberush_otlp_ingest_requests_total", "Total OTLP ingest requests received by signal.", "signal", m.otlpIngestTotal)
	writeCounter(&b, "kuberush_otlp_forward_errors_total", "Total OTLP forward errors by signal.", "signal", m.otlpForwardError)
	return b.String()
}

func writeSimpleCounter(b *strings.Builder, name, help string, v uint64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s counter\n", name)
	fmt.Fprintf(b, "%s %d\n", name, v)
}

func writeCounter(b *strings.Builder, name, help, label string, values map[string]uint64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s counter\n", name)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s{%s=\"%s\"} %d\n", name, label, escapeLabel(k), values[k])
	}
}

func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\n", "\\n")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	return v
}
