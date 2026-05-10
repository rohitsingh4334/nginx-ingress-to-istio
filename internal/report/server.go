package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/k8s"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/web"
)

// ── Server ───────────────────────────────────────────────────────────────────

type Server struct {
	addr      string
	cfg       k8s.Config
	istioCfg  istio.Config
	mu        sync.Mutex
	client    *k8s.Client
	activeCtx string // kubeconfig current-context when client was last built
	ready     chan struct{} // closed once the server is accepting connections
}

func NewServer(addr string, cfg k8s.Config, istioCfg istio.Config) *Server {
	return &Server{
		addr:     addr,
		cfg:      cfg,
		istioCfg: istioCfg,
		ready:    make(chan struct{}),
	}
}

// getClient returns the current k8s client, rebuilding it when the kubeconfig
// current-context has changed since the last call.
func (s *Server) getClient() (*k8s.Client, error) {
	info, err := k8s.GetClusterInfo(s.cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("reading kubeconfig: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil && s.activeCtx == info.Context {
		return s.client, nil
	}
	client, err := k8s.NewClient(s.cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to cluster %q: %w", info.Context, err)
	}
	if s.activeCtx != "" {
		log.Printf("INFO kubeconfig context changed %q → %q, reconnected", s.activeCtx, info.Context)
	}
	s.client = client
	s.activeCtx = info.Context
	return s.client, nil
}

func (s *Server) Serve() error {
	mux := http.NewServeMux()

	// Static UI
	mux.Handle("/", http.FileServer(http.FS(web.FS)))

	// Health probes (no middleware — must always respond fast)
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	// API (wrapped with middleware)
	api := http.NewServeMux()
	api.HandleFunc("/api/report", s.handleReport)
	api.HandleFunc("/api/cluster-info", s.handleClusterInfo)
	api.HandleFunc("/api/export/excel", s.handleExportExcel)
	api.HandleFunc("/api/export/pdf", s.handleExportPDF)
	api.HandleFunc("/api/export/manifests", s.handleExportManifests)
	mux.Handle("/api/", withMiddleware(api))

	srv := &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("shutting down server…")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	// Signal readiness after the listener is up.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(s.ready)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ── Health ───────────────────────────────────────────────────────────────────

// handleHealthz is the liveness probe — always returns 200 if the process is up.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz is the readiness probe — returns 200 once the server is ready
// to accept traffic (listener is up and k8s client was successfully constructed).
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	select {
	case <-s.ready:
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	default:
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}
}

// ── API types ─────────────────────────────────────────────────────────────────

type ReportResponse struct {
	Ingresses []IngressReport `json:"ingresses"`
	Summary   Summary         `json:"summary"`
}

type IngressReport struct {
	Analysis analyzer.IngressResult `json:"analysis"`
	Istio    istio.Resources        `json:"istio"`
}

type Summary struct {
	Total  int `json:"total"`
	Low    int `json:"low"`
	Medium int `json:"medium"`
	High   int `json:"high"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Server) fetchResults(ctx context.Context) ([]analyzer.IngressResult, error) {
	client, err := s.getClient()
	if err != nil {
		return nil, err
	}
	ingresses, err := client.ListIngresses(ctx)
	if err != nil {
		return nil, err
	}
	return analyzer.Analyze(ingresses), nil
}

func (s *Server) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	info, err := k8s.GetClusterInfo(s.cfg.Kubeconfig)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, info)
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	results, err := s.fetchResults(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var items []IngressReport
	sum := Summary{Total: len(results)}
	for _, res := range results {
		items = append(items, IngressReport{Analysis: res, Istio: istio.Generate(res, s.istioCfg)})
		switch res.Complexity {
		case analyzer.Low:
			sum.Low++
		case analyzer.Medium:
			sum.Medium++
		case analyzer.High:
			sum.High++
		}
	}
	writeJSON(w, ReportResponse{Ingresses: items, Summary: sum})
}

// writeJSON marshals v into a buffer and writes it as JSON only on success.
func writeJSON(w http.ResponseWriter, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
		http.Error(w, "internal encoding error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("response write error: %v", err)
	}
}
