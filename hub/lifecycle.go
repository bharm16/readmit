package hub

import (
	"errors"
	"io"
	"net/http"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// lifecycleAdmission is the lifecycle route's one declaration of how a
// request maps into action vocabulary: a read reports history, a revision or
// resolve writes, every other command is administrative, and an audit-export
// additionally holds the export grant its content stands for. An audit-export
// is an export of record rather than new authored work, so it never admits
// an operation.
func lifecycleAdmission(method, kind string) teamAdmission {
	adm := teamAdmission{action: "evidence.read"}
	if method != "POST" {
		return adm
	}
	adm.writes = kind != "audit-export"
	if kind == "revision" || kind == "resolve" {
		adm.action = "evidence.write"
	} else {
		adm.action = "admin"
	}
	if kind == "audit-export" {
		adm.further = []furtherGrant{{action: "export", refusal: "export refused"}}
	}
	return adm
}

// lifecycleIdentity is the identity rule the lifecycle route declares: a
// command is issued by a human the policy identifies, whose issuer the
// backup bound can retain.
func lifecycleIdentity(method string) identityRule {
	return func(p Principal) (bool, string) {
		if method == "POST" && (p.Kind != "oidc" || p.Role == "runner" || !reviewText(p.Issuer, 2048)) {
			return false, "access refused"
		}
		return true, ""
	}
}

func (s *Store) lifecycleRequest(w http.ResponseWriter, r *http.Request, a *Access, project string) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method refused", 405)
		return
	}
	var c LifecycleCommand
	if r.Method == "POST" {
		data, e := io.ReadAll(io.LimitReader(r.Body, hubprotocol.MaxCommandBytes+1))
		if e != nil {
			http.Error(w, "incomplete command", 400)
			return
		}
		c, e = hubprotocol.DecodeLifecycleCommand(data)
		if e != nil {
			http.Error(w, "invalid command", 400)
			return
		}
	}
	adm := lifecycleAdmission(r.Method, c.Kind)
	adm.accept = lifecycleIdentity(r.Method)
	adm.serialized = true
	s.mu.Lock()
	defer s.mu.Unlock()
	p, proj, release, ok := s.authorizeWrite(a, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	if r.Method == "GET" {
		sendReview(w, 200, proj.history())
		return
	}
	projects := s.projects()
	event, events, replayed, e := projects.recordLifecycle(r.Context(), proj, p, c, a.roles(project))
	switch {
	case errors.Is(e, errLogIDConflict):
		http.Error(w, "command id conflict", 409)
		return
	case errors.Is(e, errLogHead):
		http.Error(w, "head conflict or limit", 409)
		return
	case errors.Is(e, errRemoval):
		http.Error(w, "removal refused", 403)
		return
	case errors.Is(e, errLifecycleConflict):
		http.Error(w, "lifecycle conflict", 409)
		return
	case errors.Is(e, errAuditUnavailable):
		http.Error(w, "audit unavailable", 503)
		return
	case errors.Is(e, errCommitUnavailable):
		http.Error(w, "commit unavailable; retry same id", 503)
		return
	case e != nil:
		http.Error(w, "metadata unavailable", 503)
		return
	}
	status := 201
	if replayed {
		status = 200
	}
	if event.Command.Kind != "audit-export" {
		sendReview(w, status, event)
		return
	}
	export, e := projects.auditExport(r.Context(), event, events)
	if errors.Is(e, errAuditRetry) {
		http.Error(w, "audit unavailable; retry same id", 503)
		return
	}
	if e != nil {
		http.Error(w, "audit unavailable", 503)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="audit.json"`)
	sendReview(w, status, export)
}

// authorizeProject authorizes a request against the access policy and only
// then reads the project's lifecycle log, once, for the request's decisions.
func (s *Store) authorizeProject(a *Access, r *http.Request, project, action string) (Principal, projectView, error) {
	p, e := a.Authorize(r, project, action)
	if e != nil {
		return p, projectView{}, e
	}
	proj, e := s.projects().open(r.Context(), project)
	if e != nil || proj.removed(p) {
		return Principal{}, proj, errAccess
	}
	return p, proj, nil
}

// authorize combines the reloaded access policy with durable project removals,
// read once for the request: policy reinstallation cannot resurrect a removed
// principal.
func (s *Store) authorize(a *Access, r *http.Request, proj projectView, action string) (Principal, error) {
	p, e := a.Authorize(r, proj.name, action)
	if e != nil {
		return p, e
	}
	if proj.removed(p) {
		return Principal{}, errAccess
	}
	return p, nil
}
