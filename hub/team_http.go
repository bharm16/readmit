package hub

import (
	"net/http"
	"strings"
	"time"
)

// TeamHandler has no legacy unscoped evidence route. mTLS admits a connection;
// every project operation additionally requires current identity and permission.
func (s *Store) TeamHandler(access *Access) http.Handler {
	return s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.teamRequest(w, r, access)
	}), 30*time.Second)
}

func (s *Store) teamRequest(w http.ResponseWriter, r *http.Request, access *Access) {
	ctx := r.Context()
	if access == nil {
		http.Error(w, "identity required", 401)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 4 || (parts[0] != "v1" && parts[0] != "v2") || parts[1] != "projects" || !validProject(parts[2]) || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	project := parts[2]
	if len(parts) == 4 && parts[3] == "lifecycle" {
		s.lifecycleRequest(w, r, access, project)
		return
	}
	if parts[0] == "v2" {
		if len(parts) == 5 && parts[3] == "exports" && validDigest(parts[4]) && r.Method == "GET" {
			s.supportExport(w, r, access, project, parts[4])
			return
		}
		if len(parts) != 4 || (parts[3] != "reviews" && parts[3] != "history" && parts[3] != "notifications") {
			http.NotFound(w, r)
			return
		}
	}
	if len(parts) == 4 && (parts[3] == "reviews" || parts[3] == "history" || parts[3] == "notifications") {
		s.reviewRequest(w, r, access, project, parts[3])
		return
	}
	var action string
	switch parts[3] {
	case "artifacts":
		if len(parts) != 5 || !validDigest(parts[4]) {
			http.Error(w, "invalid address", 400)
			return
		}
		switch r.Method {
		case "GET":
			action = "evidence.read"
		case "PUT":
			action = "evidence.write"
		}
	case "exports":
		if len(parts) != 5 || !validDigest(parts[4]) {
			http.Error(w, "invalid address", 400)
			return
		}
		if r.Method == "GET" {
			action = "export"
		}
	case "execution":
		if len(parts) == 4 && r.Method == "POST" {
			action = "execution"
		}
	case "approvals":
		if len(parts) == 4 && r.Method == "POST" {
			action = "approval"
		}
	case "enrollment":
		if len(parts) == 4 && r.Method == "POST" {
			action = "enrollment"
		}
	default:
		http.NotFound(w, r)
		return
	}
	if action == "" {
		http.Error(w, "method refused", 405)
		return
	}
	principal, e := s.authorize(access, r, project, action)
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	if action == "evidence.write" {
		release, err := s.admitAuthor(r, principal)
		if err != nil {
			http.Error(w, "operation admission refused", 403)
			return
		}
		defer release()
	}
	if action == "export" {
		http.Error(w, "reviewed support export requires v2", 403)
		return
	}
	if action == "execution" || action == "approval" {
		http.Error(w, "operation not implemented", 501)
		return
	}
	if action == "enrollment" {
		// Enrollment is the operator's explicit project/subject/certificate/token
		// registration. This handshake proves all four agree; it grants no new role.
		if principal.Kind != "runner" {
			http.Error(w, "scoped runner required", 403)
			return
		}
		w.WriteHeader(204)
		return
	}
	if _, e := s.db.ExecContext(ctx, "UPDATE readmit_hub_schema SET team_enabled=true WHERE singleton"); e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	d := parts[4]
	if retired, e := s.retired(ctx, project, d); e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	} else if retired {
		http.Error(w, "artifact retired; recovery copy retained", 410)
		return
	}
	if r.Method == "GET" {
		var exists bool
		if e := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM readmit_hub_project_artifacts WHERE project=$1 AND digest=$2)", project, d).Scan(&exists); e != nil {
			http.Error(w, "metadata unavailable", 503)
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == "GET" {
		w.Header().Set("Readmit-Custody-Warning", "Downloaded copies remain under local custody and cannot be revoked.")
	}
	if action == "export" {
		w.Header().Set("Content-Disposition", `attachment; filename="artifact.bin"`)
	}
	s.artifactRequest(w, r, ctx, d, project)
}
