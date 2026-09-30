package hub

import (
	"net/http"
	"slices"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The project directory routes answer metadata only. Files lists what a
// project links — never an artifact's bytes, which stay behind the explicit
// artifact GET — to anyone who may read the project's evidence. Members lists
// the policy's grants in the project and the log's removals, to its
// administrators only. Reviewers lists the active members who may approve,
// to anyone who may ask one of them for a review. None of them changes a
// grant: membership stays the operator's policy file.

func (s *Store) directoryRequest(w http.ResponseWriter, r *http.Request, access *Access, project, route string) {
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method refused", 405)
		return
	}
	action := map[string]string{"files": "evidence.read", "members": "admin", "reviewers": "evidence.write"}[route]
	_, proj, _, ok := s.authorizeWrite(access, r, w, project, teamAdmission{action: action})
	if !ok {
		return
	}
	switch route {
	case "files":
		files, e := s.projectFiles(r.Context(), project)
		if e != nil {
			http.Error(w, "metadata unavailable", 503)
			return
		}
		out := hubprotocol.ProjectFiles{Schema: hubprotocol.ProjectFilesSchema, Project: project, Files: make([]hubprotocol.ProjectFile, 0, len(files))}
		for _, file := range files {
			out.Files = append(out.Files, hubprotocol.ProjectFile{Digest: file.Digest, Size: file.Size, Issuer: file.Issuer, Actor: file.Actor, LinkedAt: file.LinkedAt})
		}
		sendReview(w, 200, out)
	default:
		policy, e := access.policy()
		if e != nil {
			http.Error(w, "access refused", 403)
			return
		}
		sendReview(w, 200, projectMembers(policy, proj, route == "reviewers"))
	}
}

// projectMembers is the project's members as the installed policy grants
// them and its log removed them. reviewers keeps only the active members who
// may approve, the recipients a review request may name.
func projectMembers(policy AccessPolicy, proj projectView, reviewers bool) hubprotocol.ProjectMembers {
	out := hubprotocol.ProjectMembers{Schema: hubprotocol.ProjectMembersSchema, Project: proj.name, Issuer: policy.Issuer, Members: []hubprotocol.ProjectMember{}}
	seen := map[string]bool{}
	for _, grant := range policy.Grants {
		if grant.Project != proj.name {
			continue
		}
		seen[grant.Subject] = true
		status := hubprotocol.MemberActive
		if proj.lifecycle.Removed(policy.Issuer, grant.Subject) {
			status = hubprotocol.MemberRemoved
		}
		if reviewers && (status != hubprotocol.MemberActive || !roleAllows(grant.Role, "approval")) {
			continue
		}
		out.Members = append(out.Members, hubprotocol.ProjectMember{Subject: grant.Subject, Role: grant.Role, Status: status})
	}
	if !reviewers {
		for _, event := range proj.events {
			subject := event.Command.Subject
			if event.Command.Kind != "remove-user" || event.Issuer != policy.Issuer || seen[subject] {
				continue
			}
			seen[subject] = true
			out.Members = append(out.Members, hubprotocol.ProjectMember{Subject: subject, Status: hubprotocol.MemberRemoved})
		}
	}
	slices.SortFunc(out.Members, func(a, b hubprotocol.ProjectMember) int {
		switch {
		case a.Subject < b.Subject:
			return -1
		case a.Subject > b.Subject:
			return 1
		}
		return 0
	})
	return out
}
