package exporter

import (
	"context"
	"fmt"
	"os"
)

type StdoutLogSink struct{}

func NewStdoutLogSink() *StdoutLogSink {
	return &StdoutLogSink{}
}

func (s *StdoutLogSink) Name() string { return "stdout" }

func (s *StdoutLogSink) WriteLogs(ctx context.Context, records []LogRecord) error {
	for _, r := range records {
		_, _ = fmt.Fprintf(os.Stdout, "[log][%s/%s/%s] %s\n", r.Namespace, r.Pod, r.Container, r.Message)
	}
	return nil
}

func (s *StdoutLogSink) Close() error { return nil }
