package hub

import (
	"errors"
	"log"
	"net/http"
)

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
	_, proj, e := s.authorizeProject(a, r, project, "export")
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	data, e := s.projects().approvedSupport(r.Context(), proj, digest)
	if errors.Is(e, errLogUnavailable) {
		http.Error(w, "sharing unavailable", 503)
		return
	}
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
