package hubprotocol

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/sharing"
)

// This file is the one derivation of a project's two event logs. The hub
// decides every command and export against it and hubclient builds commands
// from it, so the service and the application cannot read the same log two
// ways: which support policy is in force, which request an approval answers,
// who was removed, what was retired and which revisions are unresolved.
//
// A load function reads the bytes a digest names within the project; its
// errors are returned unchanged.

// Reviews is what one read of a project's review log establishes: the
// committed events, the support policy in force, and whether any support
// command is recorded.
type Reviews struct {
	events []ReviewEvent
	// policy is the last support-policy command the log records. A log with
	// none carries the zero command, which no release, request or approval
	// can match.
	policy ReviewCommand
	// support says whether any support command is recorded, which decides
	// which audit and history versions the project's history reads back as.
	support bool
}

// DeriveReviews reads a project's review log once.
func DeriveReviews(events []ReviewEvent) Reviews {
	state := Reviews{events: events}
	for _, event := range events {
		if !IsSupport(event.Command) {
			continue
		}
		state.support = true
		if event.Command.Kind == "support-policy" {
			state.policy = event.Command
		}
	}
	return state
}

// PolicyInForce is the support policy in force: the last support-policy
// command the log records, and whether there is one. Its evidence is the
// digest of the policy's bytes.
func (s Reviews) PolicyInForce() (ReviewCommand, bool) {
	return s.policy, s.policy.Kind != ""
}

// HasSupport reports whether any support command is recorded.
func (s Reviews) HasSupport() bool { return s.support }

// HistorySchema is the history version a read on the v1 or v2 route answers with.
func (s Reviews) HistorySchema(v2 bool) string {
	if v2 {
		return ReviewHistoryV2
	}
	return ReviewHistoryV1
}

// AuditSchema is the audit export version this log reads back as: a project
// that never carried a support command reads as v1.
func (s Reviews) AuditSchema() string {
	if s.support {
		return AuditV2
	}
	return AuditV1
}

// ReleaseRequest is the review request an approval of this exact release
// content answers: the last review-request naming the release digest.
func (s Reviews) ReleaseRequest(release string) (ReviewEvent, bool) {
	return s.last(func(c ReviewCommand) bool {
		return !IsSupport(c) && c.Kind == "review-request" && c.Release == release
	})
}

// SupportRequest is the support request an approval of this exact summary
// answers: the last support-request naming the summary's digest.
func (s Reviews) SupportRequest(summary string) (ReviewEvent, bool) {
	return s.last(func(c ReviewCommand) bool { return IsSupportRequest(c) && c.Evidence == summary })
}

func (s Reviews) last(match func(ReviewCommand) bool) (ReviewEvent, bool) {
	for i := len(s.events) - 1; i >= 0; i-- {
		if match(s.events[i].Command) {
			return s.events[i], true
		}
	}
	return ReviewEvent{}, false
}

// Current reports whether a support command stands under the policy in force.
// A command the log recorded under a since-replaced policy is history, not a
// live statement, and v2 history refuses it.
func (s Reviews) Current(c ReviewCommand) bool {
	if c.Kind == "support-policy" {
		return true
	}
	if c.Release != s.policy.Evidence {
		return false
	}
	if IsSupportRequest(c) {
		return c.Parent == s.policy.ID
	}
	for i := range s.events {
		if s.events[i].Command.ID == c.Parent && IsSupportRequest(s.events[i].Command) {
			return s.events[i].Command.Parent == s.policy.ID
		}
	}
	return false
}

// Validate decides a command an actor of issuer makes against the log: its
// evidence loads, its parent is recorded against the same evidence, and an
// approval answers a request addressed to that actor by someone else, once,
// continuing the release chain the earlier approvals of the same expectation
// recorded. A support command is decided by the support rules.
func (s Reviews) Validate(c ReviewCommand, actor, issuer string, load func(string) ([]byte, error)) error {
	if IsSupport(c) {
		return s.validateSupport(c, actor, issuer, load)
	}
	events := s.events
	if _, e := load(c.Evidence); e != nil {
		return e
	}
	var parent *ReviewEvent
	for i := range events {
		if events[i].Command.ID == c.Parent {
			parent = &events[i]
		}
	}
	if c.Parent != "" && (parent == nil || parent.Command.Evidence != c.Evidence) {
		return ErrMissing
	}
	if c.Kind != "review-request" && c.Kind != "approval" {
		return nil
	}
	release, e := loadRelease(load, c.Release)
	if e != nil {
		return e
	}
	if c.Kind == "review-request" {
		return nil
	}
	if parent.Command.Kind != "review-request" || parent.Command.Release != c.Release || parent.Command.Recipient != actor || parent.Issuer != issuer || parent.Actor == actor {
		return ErrRefused
	}
	var previous *expectation.Release
	for _, event := range events {
		if event.Command.Kind != "approval" {
			continue
		}
		if event.Command.Parent == c.Parent {
			return ErrConflict
		}
		prior, e := loadRelease(load, event.Command.Release)
		if e != nil {
			return e
		}
		if prior.ID == release.ID {
			previous = &prior
		}
	}
	if previous == nil {
		if release.Parent != "" || release.Baseline.Revision != 1 {
			return ErrConflict
		}
	} else {
		if release.Parent != previous.Identity() || release.Baseline.Revision != previous.Baseline.Revision+1 {
			return ErrConflict
		}
		// Recompute both predecessor commitments and all profile continuity rules.
		spec, e := json.Marshal(release.Baseline.Spec)
		if e != nil {
			return e
		}
		review, e := expectation.Review(release.ID, spec, release.Profiles, previous, false)
		if e != nil || review.Identity != release.Review {
			return ErrConflict
		}
	}
	return nil
}

func loadRelease(load func(string) ([]byte, error), digest string) (expectation.Release, error) {
	data, e := load(digest)
	if e != nil {
		return expectation.Release{}, e
	}
	return expectation.Decode(data)
}

// validateSupport decides a support command against the log. One approval
// counts only against one request, one request only against the policy in
// force, and a summary only under the policy it names.
func (s Reviews) validateSupport(c ReviewCommand, actor, issuer string, load func(string) ([]byte, error)) error {
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
		if IsSupportApproval(event.Command) && event.Command.Parent == c.Parent {
			return ErrConflict
		}
	}
	if parent == nil {
		return ErrMissing
	}
	if !IsSupportRequest(parent.Command) || parent.Command.Evidence != c.Evidence || parent.Command.Release != c.Release || parent.Command.Parent != s.policy.ID || parent.Command.Recipient != actor || parent.Actor == actor || parent.Issuer != issuer {
		return ErrRefused
	}
	return nil
}

// Approved decides one support export: the digest is served only when an
// approval under the policy in force names a request that itself stands
// under that policy. Admission and export read the same derivation, so they
// cannot disagree.
func (s Reviews) Approved(digest string, load func(string) ([]byte, error)) ([]byte, error) {
	policyID := s.policy.Evidence
	if policyID == "" {
		return nil, ErrRefused
	}
	raw, e := load(digest)
	if e != nil {
		return nil, e
	}
	if e := s.underPolicy(raw, policyID, ErrRefused, load); e != nil {
		return nil, e
	}
	for _, event := range s.events {
		if !IsSupportApproval(event.Command) || event.Command.Evidence != digest || event.Command.Release != policyID {
			continue
		}
		for _, request := range s.events {
			if request.Command.ID == event.Command.Parent && IsSupportRequest(request.Command) && request.Command.Parent == s.policy.ID {
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
func (s Reviews) underPolicy(raw []byte, policyID string, mismatch error, load func(string) ([]byte, error)) error {
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
		return ErrRefused
	}
	return nil
}

// Lifecycle is what one read of a project's lifecycle log establishes.
type Lifecycle struct {
	events []LifecycleEvent
	// removed holds every "issuer\x00subject" a remove-user command retired.
	removed map[string]bool
	// retired holds every artifact digest a retire command named.
	retired map[string]bool
}

// DeriveLifecycle reads a project's lifecycle log once.
func DeriveLifecycle(events []LifecycleEvent) Lifecycle {
	state := Lifecycle{events: events, removed: map[string]bool{}, retired: map[string]bool{}}
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

// Removed reports whether a principal's issuer and subject were removed from
// the project. It is the one removed-user rule: policy reinstallation cannot
// resurrect a removed principal, and neither can a recipient check.
func (s Lifecycle) Removed(issuer, subject string) bool {
	return s.removed[issuer+"\x00"+subject]
}

// Retired reports whether an artifact digest was retired.
func (s Lifecycle) Retired(digest string) bool {
	return s.retired[digest]
}

// Tips are each resource's unresolved revisions: the revision and resolve
// IDs no later revision or resolve names as a parent, in ascending order.
func (s Lifecycle) Tips() map[string][]string {
	tips := map[string][]string{}
	for _, e := range s.events {
		c := e.Command
		if c.Kind != "revision" && c.Kind != "resolve" {
			continue
		}
		p := tips[c.Resource]
		p = slices.DeleteFunc(p, func(id string) bool { return slices.Contains(c.Parents, id) })
		p = append(p, c.ID)
		slices.Sort(p)
		tips[c.Resource] = p
	}
	return tips
}

// Validate decides a lifecycle command against the log at an instant: a
// retired artifact takes no further command, a retention only extends, a
// retirement waits for its retention to lapse, a revision continues one tip
// of its resource, and a resolve names every tip.
func (s Lifecycle) Validate(c LifecycleCommand, load func(string) ([]byte, error), at time.Time) error {
	events := s.events
	if c.Artifact != "" {
		for _, event := range events {
			if event.Command.Kind == "retire" && event.Command.Artifact == c.Artifact {
				return ErrConflict
			}
		}
		if _, e := load(c.Artifact); e != nil {
			return e
		}
	}
	switch c.Kind {
	case "remove-user", "audit-export":
		return nil
	case "retention", "retire":
		var until time.Time
		for _, e := range events {
			if e.Command.Artifact == c.Artifact {
				if e.Command.Kind == "retire" {
					return ErrConflict
				}
				if e.Command.Kind == "retention" {
					until, _ = time.Parse(time.RFC3339Nano, e.Command.Until)
				}
			}
		}
		if c.Kind == "retire" {
			if until.IsZero() || at.Before(until) {
				return ErrConflict
			}
		} else {
			next, _ := time.Parse(time.RFC3339Nano, c.Until)
			if next.Before(until) {
				return ErrConflict
			}
		}
		return nil
	}
	tips := s.Tips()[c.Resource]
	if c.Kind == "revision" && len(tips) >= maxTips && (len(c.Parents) == 0 || !slices.Contains(tips, c.Parents[0])) {
		return ErrLimit
	}
	if c.Kind == "resolve" && !slices.Equal(c.Parents, tips) {
		return ErrConflict
	}
	if len(c.Parents) == 0 && len(tips) > 0 {
		return ErrConflict
	}
	for _, parent := range c.Parents {
		found := false
		for _, event := range events {
			p := event.Command
			if p.ID == parent && p.Resource == c.Resource && (p.Kind == "revision" || p.Kind == "resolve") {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}
