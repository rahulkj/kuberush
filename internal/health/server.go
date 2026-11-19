package health

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rahulkj/kubemon/internal/selfmetrics"
	"github.com/rahulkj/kubemon/internal/traces"
)

type Server struct {
	addr            string
	traceProcessor  *traces.Processor
	metricsEnabled  bool
	metricsEndpoint string
	metrics         *selfmetrics.Metrics
	srv             *http.Server
}

func NewServer(addr string, traceProcessor *traces.Processor, metricsEnabled bool, metricsEndpoint string, metrics *selfmetrics.Metrics) *Server {
	return &Server{
		addr:            addr,
		traceProcessor:  traceProcessor,
		metricsEnabled:  metricsEnabled,
		metricsEndpoint: metricsEndpoint,
		metrics:         metrics,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if s.metrics != nil {
			_, _ = w.Write([]byte(s.metrics.RenderPrometheus()))
			return
		}
		_, _ = w.Write([]byte("# KubeRush metrics\n"))
	})
	// OTLP/HTTP trace receiver + enrichment/export pipeline
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Printf("[traces] received OTLP traces request content-type=%s bytes=%d\n", ct, len(body))
		if s.metrics != nil {
			s.metrics.IncOTLPIngest("traces")
		}
		if s.traceProcessor != nil {
			go func(data []byte, contentType string) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := s.traceProcessor.Handle(ctx, data, contentType); err != nil {
					fmt.Printf("[traces] process/export error: %v\n", err)
				}
			}(body, ct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	// OTLP/HTTP metrics receiver + optional forward
	mux.HandleFunc("/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Printf("[metrics] received OTLP metrics request content-type=%s bytes=%d\n", ct, len(body))
		if s.metrics != nil {
			s.metrics.IncOTLPIngest("metrics")
		}
		if s.metricsEnabled && s.metricsEndpoint != "" {
			go func(data []byte, contentType string) {
				url := strings.TrimRight(s.metricsEndpoint, "/") + "/v1/metrics"
				req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
				if err != nil {
					fmt.Printf("[metrics] forward create request error: %v\n", err)
					if s.metrics != nil {
						s.metrics.IncOTLPForwardError("metrics")
					}
					return
				}
				if contentType != "" {
					req.Header.Set("Content-Type", contentType)
				}
				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(req)
				if err != nil {
					fmt.Printf("[metrics] forward error: %v\n", err)
					if s.metrics != nil {
						s.metrics.IncOTLPForwardError("metrics")
					}
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode >= 300 {
					fmt.Printf("[metrics] forward failed status: %s\n", resp.Status)
					if s.metrics != nil {
						s.metrics.IncOTLPForwardError("metrics")
					}
				}
			}(body, ct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
