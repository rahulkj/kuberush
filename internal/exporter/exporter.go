package exporter

import "context"

// LogRecord is a normalized log event with Kubernetes metadata.
type LogRecord struct {
	Namespace string
	Pod       string
	Container string
	NodeName  string
	PodLabels map[string]string
	Message   string
}

type LogSink interface {
	Name() string
	WriteLogs(ctx context.Context, records []LogRecord) error
	Close() error
}
