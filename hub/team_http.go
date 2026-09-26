package hub

import (
	"net/http"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// TeamHandler has no legacy unscoped evidence route. mTLS admits a connection;
// every project operation additionally requires current identity and permission.
func (s *Store) TeamHandler(access *Access) http.Handler {
	return s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.teamRequest(w, r, access)
	}), 30*time.Second)
}

// identityRule declines principals a route will not serve even though the
// access policy granted the action, and carries its own refusal sentence.
type identityRule func(Principal) (bool, string)

// furtherGrant is one action a route's request must additionally hold beyond
// its own action, with the refusal sentence the route answers its absence
// with — audit-export is reached through the admin action and additionally
// requires the export grant its content stands for.
type furtherGrant struct {
	action  string
	refusal string
}

// teamAdmission declares one team route's admission decisions in one place:
// the action the request maps to, whether it writes, the identity rule its
// principals must pass beyond the access policy, and any further grants the
// same request must also hold. URL-version detection and action escalation
// are properties of the declaration; a handler keeps only the route's own
// work, and authorizeWrite applies the whole sequence to every route alike.
type teamAdmission struct {
	action  string
	writes  bool
	accept  identityRule // nil admits any authorized principal
	further []furtherGrant
	// serialized says the caller holds the store lock, so no command changes
	// the project's log while the request waits for operation admission and
	// the log is read once for it.
	serialized bool
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
	// The version is parsed once here; handlers take it as a fact they never
	// re-derive from the path again.
	v2, project := parts[0] == "v2", parts[2]
	if len(parts) == 4 && parts[3] == "lifecycle" {
		s.lifecycleRequest(w, r, access, project)
		return
	}
	if v2 {
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
		s.reviewRequest(w, r, access, project, parts[3], v2)
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
	adm := teamAdmission{action: action, writes: action == "evidence.write"}
	if action == "enrollment" {
		// Enrollment is the operator's explicit project/subject/certificate/token
		// registration. This handshake proves all four agree; it grants no new role.
		adm.accept = func(p Principal) (bool, string) {
			if p.Kind != "runner" {
				return false, "scoped runner required"
			}
			return true, ""
		}
	}
	_, proj, release, ok := s.authorizeWrite(access, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
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
		w.WriteHeader(204)
		return
	}
	if e := setTeamEnabled(ctx, s.db, true); e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	d := parts[4]
	if proj.retired(d) {
		http.Error(w, "artifact retired; recovery copy retained", 410)
		return
	}
	if r.Method == "GET" {
		var exists bool
		exists, e := s.linkedProjectArtifact(ctx, project, d)
		if e != nil {
			http.Error(w, "metadata unavailable", 503)
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == "GET" {
		w.Header().Set(hubprotocol.CustodyHeader, hubprotocol.CustodyWarning)
	}
	s.artifactRequest(w, r, ctx, d, project)
}
