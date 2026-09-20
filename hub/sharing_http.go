package hub

import (
	"context"
	"log"
	"net/http"
)

func (s *Store) supportArtifact(ctx context.Context, project, digest string) ([]byte, error) {
	if !s.linked(ctx, project, digest) {
		return nil, ErrMissing
	}
	retired, e := s.retired(ctx, project, digest)
	if e != nil || retired {
		return nil, ErrMissing
	}
	return s.Get(ctx, digest)
}
func (s *Store) supportExport(w http.ResponseWriter, r *http.Request, a *Access, project, digest string) {
	// The log vocabulary is fixed, never derived from a request, identity, path,
	// artifact, credential or backend error. HTTP response loss never retries.
	published := false
	defer func() {
		if published {
			log.Print(`{"schema":"readmit-sharing-security-event/v1","action":"team-exported"}`)
		} else {
			log.Print(`{"schema":"readmit-sharing-security-event/v1","action":"team-export-refused"}`)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.authorize(a, r, project, "export"); e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	events, e := s.reviewEvents(r.Context(), project)
	if e != nil {
		http.Error(w, "sharing unavailable", 503)
		return
	}
	data, e := deriveReviews(events).approved(digest, func(d string) ([]byte, error) { return s.supportArtifact(r.Context(), project, d) })
	if e != nil {
		http.Error(w, "exact reviewed support unavailable", 403)
		return
	}
	if r.Context().Err() != nil {
		http.Error(w, "sharing cancelled", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="support.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, e = w.Write(data); e == nil {
		published = true
	}
}
