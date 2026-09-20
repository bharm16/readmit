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
	return s.admitRequest(http.HandlerFunc(s.operatorRequest), 30*time.Second)
}

func (s *Store) operatorRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !strings.HasPrefix(r.URL.Path, "/v1/artifacts/") {
		http.NotFound(w, r)
		return
	}
	d := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
	if !validDigest(d) || r.URL.RawQuery != "" {
		http.Error(w, "invalid artifact address", http.StatusBadRequest)
		return
	}
	var team bool
	if err := s.db.QueryRowContext(ctx, "SELECT team_enabled FROM readmit_hub_schema WHERE singleton").Scan(&team); err != nil || team {
		http.Error(w, "team authorization required", 403)
		return
	}
	if r.Method == "PUT" {
		release, err := s.admitOperator(r)
		if err != nil {
			http.Error(w, "authenticated author required", 403)
			return
		}
		defer release()
	}
	s.artifactRequest(w, r, ctx, d, "")
}

// Serve never starts an HTTP listener without mutual TLS. Shutdown cancels new
// connections and gives bounded in-flight transfers time to finish.
func (s *Store) Serve(ctx context.Context) error {
	return s.serve(ctx, s.Handler())
}

func (s *Store) ServeTeam(ctx context.Context, access *Access) error {
	return s.ServeRunners(ctx, access, "")
}

// ServeRunners enables explicit runner admission alongside team access.
func (s *Store) ServeRunners(ctx context.Context, access *Access, runnerPolicy string) error {
	handler, _, err := s.runnerService(ctx, access, runnerPolicy)
	if err != nil {
		return err
	}
	return s.serve(ctx, handler)
}

// runnerService returns the same instant enforced by the handler, so the
// scheduler cannot consume an occurrence during predecessor-lease recovery.
func (s *Store) runnerService(ctx context.Context, access *Access, runnerPolicy string) (http.Handler, time.Time, error) {
	if access == nil {
		return nil, time.Time{}, errAccess
	}
	if err := s.Ready(ctx); err != nil {
		return nil, time.Time{}, err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE readmit_hub_schema SET team_enabled=true WHERE singleton"); err != nil {
		return nil, time.Time{}, errAccess
	}
	ready := time.Now().Add(10 * time.Second)
	if runnerPolicy != "" {
		if _, err := readRunnerPolicy(runnerPolicy); err != nil {
			return nil, time.Time{}, err
		}
		return s.runnerHandler(access, runnerPolicy, ready), ready, nil
	}
	return s.TeamHandler(access), time.Now(), nil
}

func (s *Store) serve(ctx context.Context, handler http.Handler) error {
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
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
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

func (s *Store) artifactRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, d, project string) {
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
		if project != "" {
			if err := s.linkProject(ctx, project, d); err != nil {
				http.Error(w, "metadata unavailable", http.StatusServiceUnavailable)
				return
			}
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
}

// linkProject bounds the catalogue that backup/v2 promises to restore. The
// store owns the database exclusively, and this mutex serializes HTTP writers.
func (s *Store) linkProject(ctx context.Context, project, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM readmit_hub_project_artifacts WHERE project=$1 AND digest=$2)", project, digest).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM readmit_hub_project_artifacts").Scan(&count); err != nil {
		return err
	}
	if count >= 65536 {
		return ErrLimit
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO readmit_hub_project_artifacts(project,digest) VALUES($1,$2)", project, digest)
	return err
}
