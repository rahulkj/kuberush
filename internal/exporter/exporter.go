package exporter

import "context"

// LogRecord is a minimal placeholder for a normalized log event.
type LogRecord struct {
	Namespace string
	Pod       string
	Container string
	Message   string
}

type LogSink interface {
	Name() string
	WriteLogs(ctx context.Context, records []LogRecord) error
	Close() error
}
