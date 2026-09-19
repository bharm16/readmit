package hub

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Handler exposes opaque immutable objects, not case interpretation or user roles.
// Every request, including health probes, requires a verified client certificate.
func (s *Store) Handler() http.Handler {
	slots := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client identity required", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
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
		if !strings.HasPrefix(r.URL.Path, "/v1/artifacts/") {
			http.NotFound(w, r)
			return
		}
		d := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
		if !validDigest(d) || r.URL.RawQuery != "" {
			http.Error(w, "invalid artifact address", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case "PUT":
			err := s.Put(ctx, d, http.MaxBytesReader(w, r.Body, MaxArtifactBytes+1))
			if err != nil {
				code := http.StatusServiceUnavailable
				if errors.Is(err, ErrIntegrity) {
					code = http.StatusUnprocessableEntity
				}
				if errors.Is(err, ErrLimit) {
					code = http.StatusRequestEntityTooLarge
				}
				http.Error(w, http.StatusText(code), code)
				return
			}
			w.Header().Set("ETag", `"`+d+`"`)
			w.WriteHeader(http.StatusCreated)
		case "GET":
			data, err := s.Get(ctx, d)
			if err != nil {
				code := http.StatusServiceUnavailable
				if errors.Is(err, ErrMissing) {
					code = http.StatusNotFound
				}
				http.Error(w, http.StatusText(code), code)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("ETag", `"`+d+`"`)
			w.Write(data)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method refused", http.StatusMethodNotAllowed)
		}
	})
}

// Serve never starts an HTTP listener without mutual TLS. Shutdown cancels new
// connections and gives bounded in-flight transfers time to finish.
func (s *Store) Serve(ctx context.Context) error {
	tc, err := s.config.TLS()
	if err != nil {
		return err
	}
	if err = s.Ready(ctx); err != nil {
		return err
	}
	listener, err := tls.Listen("tcp", s.config.Listen, tc)
	if err != nil {
		return errors.New("TLS listener unavailable")
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			server.Shutdown(c)
		case <-done:
		}
	}()
	err = server.Serve(listener)
	close(done)
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("hub listener failed")
}
