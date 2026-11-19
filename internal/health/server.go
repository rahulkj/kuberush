package health

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	addr           string
	tracesEnabled  bool
	tracesEndpoint string
	srv            *http.Server
}

func NewServer(addr string, tracesEnabled bool, tracesEndpoint string) *Server {
	return &Server{addr: addr, tracesEnabled: tracesEnabled, tracesEndpoint: tracesEndpoint}
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
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("# KubeRush metrics placeholder\n"))
	})
	// OTLP/HTTP trace receiver + optional forward
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
		if s.tracesEnabled && s.tracesEndpoint != "" {
			go func(data []byte, contentType string) {
				url := strings.TrimRight(s.tracesEndpoint, "/") + "/v1/traces"
				req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
				if err != nil {
					fmt.Printf("[traces] forward create request error: %v\n", err)
					return
				}
				if contentType != "" {
					req.Header.Set("Content-Type", contentType)
				}
				client := &http.Client{Timeout: 10 * time.Second}
				resp, err := client.Do(req)
				if err != nil {
					fmt.Printf("[traces] forward error: %v\n", err)
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode >= 300 {
					fmt.Printf("[traces] forward failed status: %s\n", resp.Status)
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
