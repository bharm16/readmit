package hub

import (
	"errors"

	"github.com/bharm16/readmit/internal/sharing"
)

// This file is the one derivation of a project's two event logs. Every
// handler used to re-fold the logs itself — the support policy was scanned
// out of the review log inside a nested loop on the export path, and the
// removed-user rule was spelled once for admission and once for recipients —
// so one request could answer two ways about what the same log says. The
// derivations here run once per request, and the rules read them as fields.

// projectReviews is what one read of a project's review log establishes: the
// committed events, the support policy in force, and whether any support
// command is recorded.
type projectReviews struct {
	events []ReviewEvent
	// policy is the support policy in force: the last support-policy command
	// the log records. A log with none carries the zero command, which no
	// release, request or approval can match.
	policy ReviewCommand
	// support says whether any support command is recorded, which decides
	// which audit and history schemas the project's history carries.
	support bool
}

func deriveReviews(events []ReviewEvent) projectReviews {
	state := projectReviews{events: events}
	for _, event := range events {
		if !supportCommand(event.Command) {
			continue
		}
		state.support = true
		if event.Command.Kind == "support-policy" {
			state.policy = event.Command
		}
	}
	return state
}

// historySchema is the history document version this log reads back as: a
// project that never carried a support command reads as v1.
func (s projectReviews) historySchema(v2 bool) string {
	if v2 {
		return "readmit-hub-review-history/v2"
	}
	return "readmit-hub-review-history/v1"
}

// auditSchema is the audit export version this log reads back as.
func (s projectReviews) auditSchema() string {
	if s.support {
		return "readmit-hub-audit/v2"
	}
	return "readmit-hub-audit/v1"
}

// current reports whether a support command stands under the policy in force.
// A command the log recorded under a since-replaced policy is history, not a
// live statement, and v2 history refuses it.
func (s projectReviews) current(c ReviewCommand) bool {
	if c.Kind == "support-policy" {
		return true
	}
	if c.Release != s.policy.Evidence {
		return false
	}
	if supportRequest(c) {
		return c.Parent == s.policy.ID
	}
	for i := range s.events {
		if s.events[i].Command.ID == c.Parent && supportRequest(s.events[i].Command) {
			return s.events[i].Command.Parent == s.policy.ID
		}
	}
	return false
}

// validate decides a support command against the log. One approval counts
// only against one request, one request only against the policy in force,
// and a release only under the policy its summary names.
func (s projectReviews) validate(c ReviewCommand, actor, issuer string, load func(string) ([]byte, error)) error {
	raw, e := load(c.Evidence)
	if e != nil {
		return e
	}
	if c.Kind == "support-policy" {
		_, e := sharing.DecodePolicy(raw)
		return e
	}
	if c.Release != s.policy.Evidence {
		return ErrConflict
	}
	if e := s.underPolicy(raw, c.Release, ErrIntegrity, load); e != nil {
		return e
	}
	if c.Kind == "support-request" {
		if c.Parent != s.policy.ID {
			return ErrConflict
		}
		return nil
	}
	var parent *ReviewEvent
	for i := range s.events {
		event := &s.events[i]
		if event.Command.ID == c.Parent {
			parent = event
		}
		if supportApproval(event.Command) && event.Command.Parent == c.Parent {
			return ErrConflict
		}
	}
	if parent == nil {
		return ErrMissing
	}
	if !supportRequest(parent.Command) || parent.Command.Evidence != c.Evidence || parent.Command.Release != c.Release || parent.Command.Parent != s.policy.ID || parent.Command.Recipient != actor || parent.Actor == actor || parent.Issuer != issuer {
		return errAccess
	}
	return nil
}

// approved decides one export: the digest is served only when an approval
// under the policy in force names a request that itself stands under that
// policy. The checks the admission rule makes are the same checks, read from
// the same derivation, so admission and export cannot disagree.
func (s projectReviews) approved(digest string, load func(string) ([]byte, error)) ([]byte, error) {
	policyID := s.policy.Evidence
	if policyID == "" {
		return nil, errAccess
	}
	raw, e := load(digest)
	if e != nil {
		return nil, e
	}
	if e := s.underPolicy(raw, policyID, errAccess, load); e != nil {
		return nil, e
	}
	for _, event := range s.events {
		if !supportApproval(event.Command) || event.Command.Evidence != digest || event.Command.Release != policyID {
			continue
		}
		for _, request := range s.events {
			if request.Command.ID == event.Command.Parent && supportRequest(request.Command) && request.Command.Parent == s.policy.ID {
				return raw, nil
			}
		}
	}
	return nil, errors.New("exact authenticated support approval required")
}

// underPolicy is the one policy-integrity check behind admission and export
// alike: the bytes decode as a summary naming the policy in force, the policy
// itself loads and decodes, and it allows this download of exactly these
// bytes. A summary naming another policy earns the caller's mismatch error.
func (s projectReviews) underPolicy(raw []byte, policyID string, mismatch error, load func(string) ([]byte, error)) error {
	summary, e := sharing.Decode(raw)
	if e != nil || summary.PolicyIdentity != policyID {
		return mismatch
	}
	policyBytes, e := load(policyID)
	if e != nil {
		return e
	}
	policy, e := sharing.DecodePolicy(policyBytes)
	if e != nil || !policy.Allows("customer-hub-download", len(raw)) {
		return errAccess
	}
	return nil
}

// projectLifecycle is what one read of a project's lifecycle log establishes.
type projectLifecycle struct {
	events []LifecycleEvent
	// removed holds every "issuer\x00subject" a remove-user command retired.
	removed map[string]bool
	// retired holds every artifact digest a retire command named.
	retired map[string]bool
}

func deriveLifecycle(events []LifecycleEvent) projectLifecycle {
	state := projectLifecycle{events: events, removed: map[string]bool{}, retired: map[string]bool{}}
	for _, event := range events {
		switch event.Command.Kind {
		case "remove-user":
			state.removed[event.Issuer+"\x00"+event.Command.Subject] = true
		case "retire":
			state.retired[event.Command.Artifact] = true
		}
	}
	return state
}

// isRemoved reports whether a principal's issuer and subject were removed
// from the project. It is the one removed-user rule: policy reinstallation
// cannot accidentally resurrect a removed principal, and neither can a
// recipient check.
func (s projectLifecycle) isRemoved(issuer, subject string) bool {
	return s.removed[issuer+"\x00"+subject]
}

// isRetired reports whether an artifact digest was retired.
func (s projectLifecycle) isRetired(digest string) bool {
	return s.retired[digest]
}
