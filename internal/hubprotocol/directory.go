package hubprotocol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"
)

// The project directory: what the hub answers about a project's files and
// members, metadata only. Neither document carries an artifact's bytes or a
// grant the reader could replay: a file list names each linked artifact by
// digest with who linked it and when, where the hub recorded that; a member
// list names each subject the access policy grants in the project, the role
// it grants and whether the project's log removed it.
const (
	ProjectFilesSchema   = "readmit-hub-project-files/v1"
	ProjectMembersSchema = "readmit-hub-project-members/v1"
)

// MaxDirectoryBytes bounds one files or members answer: 65,536 links or
// 4,096 grants at their widest.
const MaxDirectoryBytes = 512 << 20

// ProjectFile is one artifact a project links. Issuer, Actor and LinkedAt are
// all empty for a link the hub made before it recorded who made it; they are
// never filled in from anything else.
type ProjectFile struct {
	Digest   string `json:"sha256"`
	Size     int64  `json:"size"`
	Issuer   string `json:"issuer"`
	Actor    string `json:"actor"`
	LinkedAt string `json:"linked_at"`
}

// ProjectFiles is every artifact one project links.
type ProjectFiles struct {
	Schema  string        `json:"schema"`
	Project string        `json:"project"`
	Files   []ProjectFile `json:"files"`
}

// ProjectMember is one subject a project's access grants reach: its role
// under the installed policy (empty once the policy no longer grants it) and
// whether it is active or removed by the project's log.
type ProjectMember struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
	Status  string `json:"status"`
}

// ProjectMembers is the project's members as the hub reads them now, under
// the one issuer its access policy trusts.
type ProjectMembers struct {
	Schema  string          `json:"schema"`
	Project string          `json:"project"`
	Issuer  string          `json:"issuer"`
	Members []ProjectMember `json:"members"`
}

// Member statuses.
const (
	MemberActive  = "active"
	MemberRemoved = "removed"
)

var memberRoles = []string{"owner", "admin", "analyst", "reviewer", "runner", "viewer"}

// DecodeProjectFiles reads one files answer strictly.
func DecodeProjectFiles(data []byte) (ProjectFiles, error) {
	var f ProjectFiles
	if len(data) > MaxDirectoryBytes || RequireExactMembers(data, "schema", "project", "files") != nil || json.Unmarshal(data, &f, json.RejectUnknownMembers(true)) != nil || f.Schema != ProjectFilesSchema || !ValidProject(f.Project) {
		return f, ErrRefused
	}
	var raw struct {
		Files []jsontext.Value `json:"files"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return f, ErrRefused
	}
	for _, entry := range raw.Files {
		if RequireExactMembers(entry, "sha256", "size", "issuer", "actor", "linked_at") != nil {
			return f, ErrRefused
		}
	}
	previous := ""
	for _, file := range f.Files {
		if !ValidDigest(file.Digest) || file.Digest <= previous || file.Size < 0 || !ValidText(file.Issuer, 2048) || !ValidText(file.Actor, 256) {
			return f, ErrRefused
		}
		previous = file.Digest
		known := file.Issuer != "" || file.Actor != "" || file.LinkedAt != ""
		if known {
			if file.Issuer == "" || file.Actor == "" {
				return f, ErrRefused
			}
			if _, e := time.Parse(time.RFC3339Nano, file.LinkedAt); e != nil {
				return f, ErrRefused
			}
		}
	}
	return f, nil
}

// DecodeProjectMembers reads one members answer strictly.
func DecodeProjectMembers(data []byte) (ProjectMembers, error) {
	var m ProjectMembers
	if len(data) > MaxDirectoryBytes || RequireExactMembers(data, "schema", "project", "issuer", "members") != nil || json.Unmarshal(data, &m, json.RejectUnknownMembers(true)) != nil || m.Schema != ProjectMembersSchema || !ValidProject(m.Project) || !ValidText(m.Issuer, 2048) {
		return m, ErrRefused
	}
	var raw struct {
		Members []jsontext.Value `json:"members"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return m, ErrRefused
	}
	for _, entry := range raw.Members {
		if RequireExactMembers(entry, "subject", "role", "status") != nil {
			return m, ErrRefused
		}
	}
	seen := map[string]bool{}
	for _, member := range m.Members {
		roleKnown := member.Role == ""
		for _, role := range memberRoles {
			roleKnown = roleKnown || member.Role == role
		}
		if member.Subject == "" || !ValidText(member.Subject, 256) || seen[member.Subject] || !roleKnown ||
			(member.Status != MemberActive && member.Status != MemberRemoved) || (member.Status == MemberActive && member.Role == "") {
			return m, ErrRefused
		}
		seen[member.Subject] = true
	}
	return m, nil
}
