package report

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"time"
)

// statusRecorder wraps ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// withMiddleware chains request-ID stamping, access logging, and panic recovery.
func withMiddleware(h http.Handler) http.Handler {
	return withRecovery(withAccessLog(withRequestID(h)))
}

func withRequestID(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		h.ServeHTTP(w, r)
	})
}

func withAccessLog(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		h.ServeHTTP(rec, r)
		log.Printf("method=%s path=%s status=%d duration=%s request_id=%s",
			r.Method, r.URL.Path, rec.status,
			time.Since(start).Round(time.Millisecond),
			w.Header().Get("X-Request-ID"))
	})
}

func withRecovery(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered request_id=%s: %v", w.Header().Get("X-Request-ID"), err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
