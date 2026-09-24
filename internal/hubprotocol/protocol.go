// Package hubprotocol is the team hub's collaboration and administration
// protocol: the review and lifecycle commands and events, the history, query
// and audit responses, their strict decoders, the derivations both sides read
// from a project's two logs, the grammar a project and a digest are named in,
// the access-token claim rule and the fixed custody sentences.
//
// The customer hub (its own module) decodes, enforces and serves it, and
// internal/hubclient builds commands from it and reads the answers, so the
// bytes one side writes are the bytes the other side reads. It defines no
// transport, no storage and no identity: the hub supplies the authenticated
// actor and the stored artifacts, the client supplies the session.
package hubprotocol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// The contract names the protocol reads and writes. A support command (the
// sharing workflow) is the only v2 review command, and it is recorded as the
// only v2 review event.
const (
	ReviewCommandV1        = "readmit-hub-review-command/v1"
	ReviewCommandV2        = "readmit-hub-review-command/v2"
	ReviewEventV1          = "readmit-hub-review-event/v1"
	ReviewEventV2          = "readmit-hub-review-event/v2"
	ReviewHistoryV1        = "readmit-hub-review-history/v1"
	ReviewHistoryV2        = "readmit-hub-review-history/v2"
	ReviewQuerySchema      = "readmit-hub-review-query/v1"
	LifecycleCommandSchema = "readmit-hub-lifecycle-command/v1"
	LifecycleEventSchema   = "readmit-hub-lifecycle-event/v1"
	LifecycleHistorySchema = "readmit-hub-lifecycle-history/v1"
	AuditV1                = "readmit-hub-audit/v1"
	AuditV2                = "readmit-hub-audit/v2"
)

// CustodyWarning is the fixed custody sentence every lifecycle history,
// audit export and download carries: removing a grant or a user refuses new
// requests, but nothing the hub does reaches a copy already downloaded.
const CustodyWarning = "Downloaded copies remain under local custody and cannot be revoked."

// CustodyHeader is the response header a project artifact download carries
// CustodyWarning in.
const CustodyHeader = "Readmit-Custody-Warning"

const (
	// MaxReviews bounds one project's review log, and every project's together.
	MaxReviews = 1024
	// MaxLifecycle bounds one project's lifecycle log, and every project's together.
	MaxLifecycle = 1024
	// MaxCommandBytes bounds one review or lifecycle command document.
	MaxCommandBytes = 8192
	// MaxQueryBytes bounds one review query document.
	MaxQueryBytes = 2048
	// maxTips bounds a resource's unresolved revision tips.
	maxTips = 64
)

var (
	// ErrRefused reports a document the contract cannot carry, or a command
	// its actor may not make.
	ErrRefused = errors.New("access refused")
	// ErrConflict reports a command the project's recorded history refuses:
	// a stale chain, a second approval, a changed policy or a retired artifact.
	ErrConflict = errors.New("the command conflicts with the project's recorded history")
	// ErrMissing reports a command whose parent the history does not record.
	ErrMissing = errors.New("the command names a parent the project does not record")
	// ErrIntegrity reports bytes that are not what the command says they are.
	ErrIntegrity = errors.New("the named bytes are not what the command says they are")
	// ErrLimit reports a resource already holding its bound of unresolved tips.
	ErrLimit = errors.New("the resource holds its limit of unresolved revisions")
)

// ValidProject reports whether s names a project or a command: 1 to 64
// lowercase letters, digits and hyphens.
func ValidProject(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ValidDigest reports whether s is one whole SHA-256 digest: 64 lowercase
// hexadecimal characters.
func ValidDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidText reports whether s is text a command may carry: at most max bytes
// of valid UTF-8 with no NUL character.
func ValidText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// RequireExactMembers refuses a JSON object unless it has exactly the named
// members, none of them null. It is the presence half of every strict reader
// here; the typed decode that follows rejects unknown members.
func RequireExactMembers(data []byte, names ...string) error {
	var m map[string]jsontext.Value
	if json.Unmarshal(data, &m) != nil || len(m) != len(names) {
		return ErrRefused
	}
	for _, n := range names {
		if v, ok := m[n]; !ok || string(v) == "null" {
			return ErrRefused
		}
	}
	return nil
}

// ReviewCommand is the complete strict document posted to a project's
// reviews. It names no actor: identity is the authenticated session's, never
// a member of the command.
type ReviewCommand struct {
	Schema    string `json:"schema"`
	ID        string `json:"id"`
	Expected  int    `json:"expected"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Parent    string `json:"parent"`
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
	Release   string `json:"release"`
}

// ReviewEvent binds a command to the authenticated actor and the server's
// sequence. Neither the local release's approver label nor a client header
// supplies identity.
type ReviewEvent struct {
	Schema   string        `json:"schema"`
	Project  string        `json:"project"`
	Sequence int           `json:"sequence"`
	Issuer   string        `json:"issuer"`
	Actor    string        `json:"actor"`
	At       string        `json:"at"`
	Command  ReviewCommand `json:"command"`
}

// ReviewHistory is the head and events a history or notifications read answers.
type ReviewHistory struct {
	Schema string        `json:"schema"`
	Head   int           `json:"head"`
	Events []ReviewEvent `json:"events"`
}

// ReviewQuery searches history or notifications after a sequence cursor.
type ReviewQuery struct {
	Schema   string `json:"schema"`
	After    int    `json:"after"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
}

// LifecycleCommand is the complete strict document posted to a project's
// lifecycle. Parents name immutable revision event IDs, not paths.
type LifecycleCommand struct {
	Schema   string   `json:"schema"`
	ID       string   `json:"id"`
	Expected int      `json:"expected"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Artifact string   `json:"artifact"`
	Parents  []string `json:"parents"`
	Subject  string   `json:"subject"`
	Until    string   `json:"until"`
	Reason   string   `json:"reason"`
}

// LifecycleEvent is one authenticated lifecycle decision. ReviewHead is
// always present: it is the review log's head an audit-export recorded, and
// zero for every other kind.
type LifecycleEvent struct {
	Schema     string           `json:"schema"`
	Project    string           `json:"project"`
	Sequence   int              `json:"sequence"`
	Issuer     string           `json:"issuer"`
	Actor      string           `json:"actor"`
	At         string           `json:"at"`
	ReviewHead int              `json:"review_head"`
	Command    LifecycleCommand `json:"command"`
}

// LifecycleHistory carries a project's lifecycle events, its unresolved
// revision tips and the custody warning.
type LifecycleHistory struct {
	Schema  string              `json:"schema"`
	Head    int                 `json:"head"`
	Events  []LifecycleEvent    `json:"events"`
	Tips    map[string][]string `json:"tips"`
	Warning string              `json:"warning"`
}

// Removed reports whether the history records a remove-user command for this
// issuer and subject: the one removed-user rule, read the way the hub reads
// it at every admission (Lifecycle.Removed).
func (h LifecycleHistory) Removed(issuer, subject string) bool {
	return DeriveLifecycle(h.Events).Removed(issuer, subject)
}

// AuditExport is the body an audit-export lifecycle command answers: both
// committed history prefixes as they stood when it was recorded.
type AuditExport struct {
	Schema     string           `json:"schema"`
	Project    string           `json:"project"`
	Lifecycle  []LifecycleEvent `json:"lifecycle"`
	ReviewHead int              `json:"review_head"`
	Reviews    []ReviewEvent    `json:"reviews"`
	Warning    string           `json:"warning"`
}

// commandSchemas maps each review kind onto the command version that carries
// its family: the support kinds are the sharing workflow and ride v2; every
// other kind rides v1.
var commandSchemas = map[string]string{
	"comment":          ReviewCommandV1,
	"assignment":       ReviewCommandV1,
	"review-request":   ReviewCommandV1,
	"approval":         ReviewCommandV1,
	"support-policy":   ReviewCommandV2,
	"support-request":  ReviewCommandV2,
	"support-approval": ReviewCommandV2,
}

// CommandSchema is the review command version that carries kind, and whether
// the protocol has the kind at all.
func CommandSchema(kind string) (string, bool) {
	schema, ok := commandSchemas[kind]
	return schema, ok
}

// IsSupport reports whether c is a support command: the sharing workflow's
// v2 family.
func IsSupport(c ReviewCommand) bool { return c.Schema == ReviewCommandV2 }

// IsSupportRequest reports whether c asks for approval of a support summary.
func IsSupportRequest(c ReviewCommand) bool { return IsSupport(c) && c.Kind == "support-request" }

// IsSupportApproval reports whether c approves a support request.
func IsSupportApproval(c ReviewCommand) bool { return IsSupport(c) && c.Kind == "support-approval" }

// ValidEventVersion reports whether a recorded event carries the event
// version its command's family is recorded under. allowV2 admits the support
// family, which an older record cannot hold.
func ValidEventVersion(e ReviewEvent, allowV2 bool) bool {
	if IsSupport(e.Command) {
		return allowV2 && e.Schema == ReviewEventV2
	}
	return e.Command.Schema == ReviewCommandV1 && e.Schema == ReviewEventV1
}

// DecodeReviewCommand reads one review command strictly: exactly its members,
// a known version, a project-grammar id and parent, a digest for evidence, a
// head inside the log's bound, bounded text, and the members its kind needs.
func DecodeReviewCommand(data []byte) (ReviewCommand, error) {
	var c ReviewCommand
	if len(data) > MaxCommandBytes || RequireExactMembers(data, "schema", "id", "expected", "kind", "evidence", "parent", "recipient", "text", "release") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil {
		return c, ErrRefused
	}
	if (c.Schema != ReviewCommandV1 && !IsSupport(c)) || !ValidProject(c.ID) || c.Expected < 0 || c.Expected >= MaxReviews || !ValidDigest(c.Evidence) || (c.Parent != "" && !ValidProject(c.Parent)) || !ValidText(c.Recipient, 256) || !ValidText(c.Text, 2048) || strings.TrimSpace(c.Text) == "" {
		return c, ErrRefused
	}
	if IsSupport(c) {
		return c, supportShape(c)
	}
	switch c.Kind {
	case "comment":
		if c.Release != "" {
			return c, ErrRefused
		}
	case "assignment":
		if c.Recipient == "" || c.Release != "" || c.Parent != "" {
			return c, ErrRefused
		}
	case "review-request":
		if c.Recipient == "" || !ValidDigest(c.Release) || c.Parent != "" {
			return c, ErrRefused
		}
	case "approval":
		if c.Recipient != "" || !ValidDigest(c.Release) || c.Parent == "" {
			return c, ErrRefused
		}
	default:
		return c, ErrRefused
	}
	return c, nil
}

// supportShape is the members each support kind carries. A support command's
// text is the fixed word "support", so no free text rides the sharing workflow.
func supportShape(c ReviewCommand) error {
	if c.Text != "support" {
		return ErrRefused
	}
	switch c.Kind {
	case "support-policy":
		if c.Release != "" || c.Parent != "" || c.Recipient != "" {
			return ErrRefused
		}
	case "support-request":
		if !ValidDigest(c.Release) || c.Parent == "" || c.Recipient == "" {
			return ErrRefused
		}
	case "support-approval":
		if !ValidDigest(c.Release) || c.Parent == "" || c.Recipient != "" {
			return ErrRefused
		}
	default:
		return ErrRefused
	}
	return nil
}

// DecodeLifecycleCommand reads one lifecycle command strictly: exactly its
// members, a project-grammar id, resource and parents, parents in ascending
// order, a head inside the log's bound, a reason, and the members its kind
// needs.
func DecodeLifecycleCommand(data []byte) (LifecycleCommand, error) {
	var c LifecycleCommand
	if len(data) > MaxCommandBytes || RequireExactMembers(data, "schema", "id", "expected", "kind", "resource", "artifact", "parents", "subject", "until", "reason") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil {
		return c, ErrRefused
	}
	if c.Schema != LifecycleCommandSchema || !ValidProject(c.ID) || c.Expected < 0 || c.Expected >= MaxLifecycle || !ValidText(c.Reason, 2048) || strings.TrimSpace(c.Reason) == "" {
		return c, ErrRefused
	}
	switch c.Kind {
	case "revision", "resolve":
		if !ValidProject(c.Resource) || !ValidDigest(c.Artifact) || c.Subject != "" || c.Until != "" || len(c.Parents) > maxTips || (c.Kind == "revision" && len(c.Parents) > 1) || (c.Kind == "resolve" && len(c.Parents) < 2) {
			return c, ErrRefused
		}
	case "remove-user", "retention", "retire", "audit-export":
		if c.Resource != "" || len(c.Parents) != 0 {
			return c, ErrRefused
		}
		switch c.Kind {
		case "remove-user":
			if c.Subject == "" || !ValidText(c.Subject, 256) || c.Artifact != "" || c.Until != "" {
				return c, ErrRefused
			}
		case "retention":
			if !ValidDigest(c.Artifact) || c.Subject != "" {
				return c, ErrRefused
			}
			if _, e := time.Parse(time.RFC3339Nano, c.Until); e != nil {
				return c, ErrRefused
			}
		case "retire":
			if !ValidDigest(c.Artifact) || c.Subject != "" || c.Until != "" {
				return c, ErrRefused
			}
		case "audit-export":
			if c.Artifact != "" || c.Subject != "" || c.Until != "" {
				return c, ErrRefused
			}
		}
	default:
		return c, ErrRefused
	}
	previous := ""
	for _, p := range c.Parents {
		if !ValidProject(p) || p <= previous {
			return c, ErrRefused
		}
		previous = p
	}
	return c, nil
}

// DecodeReviewQuery reads one review query strictly: exactly its members,
// its version, and a query Refusal finds nothing wrong with.
func DecodeReviewQuery(data []byte) (ReviewQuery, error) {
	var q ReviewQuery
	if len(data) > MaxQueryBytes || RequireExactMembers(data, "schema", "after", "text", "evidence") != nil || json.Unmarshal(data, &q, json.RejectUnknownMembers(true)) != nil || q.Schema != ReviewQuerySchema || q.Refusal() != nil {
		return q, ErrRefused
	}
	return q, nil
}

// Refusal says what the query contract cannot carry in q: a sequence below
// zero, text beyond 256 bytes or holding a NUL, or evidence that is not one
// whole SHA-256 digest. It is nil for a query the hub would search.
func (q ReviewQuery) Refusal() error {
	switch {
	case q.After < 0:
		return errors.New("search after a sequence of 0 or more")
	case !ValidText(q.Text, 256):
		return errors.New("search for at most 256 bytes of text, with no NUL character")
	case q.Evidence != "" && !ValidDigest(q.Evidence):
		return errors.New("name the evidence by its whole SHA-256 digest: 64 lowercase hexadecimal characters")
	}
	return nil
}
