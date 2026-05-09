package report

import (
	"encoding/json"
	"net/http"

	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/analyzer"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/internal/istio"
	"github.com/rohitsingh4334/nginx-ingress-to-istio/web"
)

type Server struct {
	addr    string
	results []analyzer.IngressResult
}

func NewServer(addr string, results []analyzer.IngressResult) *Server {
	return &Server{addr: addr, results: results}
}

func (s *Server) Serve() error {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web.FS)))
	mux.HandleFunc("/api/report", s.handleReport)
	mux.HandleFunc("/api/export/excel", s.handleExportExcel)
	mux.HandleFunc("/api/export/pdf", s.handleExportPDF)
	return http.ListenAndServe(s.addr, mux)
}

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

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var items []IngressReport
	sum := Summary{Total: len(s.results)}
	for _, res := range s.results {
		items = append(items, IngressReport{Analysis: res, Istio: istio.Generate(res)})
		switch res.Complexity {
		case analyzer.Low:    sum.Low++
		case analyzer.Medium: sum.Medium++
		case analyzer.High:   sum.High++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ReportResponse{Ingresses: items, Summary: sum})
}
