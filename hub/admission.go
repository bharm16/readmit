package hub

import (
	"context"
	"net/http"
	"time"
)

// admitRequest owns the pre-routing pipeline every hub request passes through:
// the concurrency bound, the security headers, the verified-client-certificate
// check, the request timeout, and the two health probes. Routing handlers
// receive an already-admitted request, so no route can weaken the pipeline.
func (s *Store) admitRequest(next http.Handler, timeout time.Duration) http.Handler {
	slots := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client identity required", http.StatusUnauthorized)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		if r.URL.Path == "/health/live" && r.Method == "GET" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/health/ready" && r.Method == "GET" {
			s.mu.Lock()
			err := s.Ready(ctx)
			s.mu.Unlock()
			if err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
