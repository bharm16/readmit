package hub

import (
	"errors"

	"github.com/bharm16/readmit/internal/sharing"
)

func supportCommand(c ReviewCommand) bool  { return c.Schema == "readmit-hub-review-command/v2" }
func supportApproval(c ReviewCommand) bool { return supportCommand(c) && c.Kind == "support-approval" }
func supportRequest(c ReviewCommand) bool  { return supportCommand(c) && c.Kind == "support-request" }
func currentSupportPolicy(events []ReviewEvent) ReviewCommand {
	var current ReviewCommand
	for _, event := range events {
		if supportCommand(event.Command) && event.Command.Kind == "support-policy" {
			current = event.Command
		}
	}
	return current
}
func validateSupportShape(c ReviewCommand) error {
	if c.Text != "support" {
		return errAccess
	}
	switch c.Kind {
	case "support-policy":
		if c.Release != "" || c.Parent != "" || c.Recipient != "" {
			return errAccess
		}
	case "support-request":
		if !validDigest(c.Release) || c.Parent == "" || c.Recipient == "" {
			return errAccess
		}
	case "support-approval":
		if !validDigest(c.Release) || c.Parent == "" || c.Recipient != "" {
			return errAccess
		}
	default:
		return errAccess
	}
	return nil
}
func validateSupport(c ReviewCommand, actor, issuer string, events []ReviewEvent, load func(string) ([]byte, error)) error {
	raw, e := load(c.Evidence)
	if e != nil {
		return e
	}
	if c.Kind == "support-policy" {
		_, e := sharing.DecodePolicy(raw)
		return e
	}
	if c.Release != currentSupportPolicy(events).Evidence {
		return ErrConflict
	}
	summary, e := sharing.Decode(raw)
	if e != nil || summary.PolicyIdentity != c.Release {
		return ErrIntegrity
	}
	policyBytes, e := load(c.Release)
	if e != nil {
		return e
	}
	policy, e := sharing.DecodePolicy(policyBytes)
	if e != nil || !policy.Allows("customer-hub-download", len(raw)) {
		return errAccess
	}
	if c.Kind == "support-request" {
		if c.Parent != currentSupportPolicy(events).ID {
			return ErrConflict
		}
		return nil
	}
	var parent *ReviewEvent
	for i := range events {
		event := &events[i]
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
	if !supportRequest(parent.Command) || parent.Command.Evidence != c.Evidence || parent.Command.Release != c.Release || parent.Command.Parent != currentSupportPolicy(events).ID || parent.Command.Recipient != actor || parent.Actor == actor || parent.Issuer != issuer {
		return errAccess
	}
	return nil
}
func approvedSupport(digest string, events []ReviewEvent, load func(string) ([]byte, error)) ([]byte, error) {
	policyID := currentSupportPolicy(events).Evidence
	if policyID == "" {
		return nil, errAccess
	}
	raw, e := load(digest)
	if e != nil {
		return nil, e
	}
	summary, e := sharing.Decode(raw)
	if e != nil || summary.PolicyIdentity != policyID {
		return nil, errAccess
	}
	policyBytes, e := load(policyID)
	if e != nil {
		return nil, e
	}
	policy, e := sharing.DecodePolicy(policyBytes)
	if e != nil || !policy.Allows("customer-hub-download", len(raw)) {
		return nil, errAccess
	}
	for _, event := range events {
		if supportApproval(event.Command) && event.Command.Evidence == digest && event.Command.Release == policyID {
			for _, request := range events {
				if request.Command.ID == event.Command.Parent && supportRequest(request.Command) && request.Command.Parent == currentSupportPolicy(events).ID {
					return raw, nil
				}
			}
		}
	}
	return nil, errors.New("exact authenticated support approval required")
}

func hasSupportEvents(events []ReviewEvent) bool {
	for _, e := range events {
		if supportCommand(e.Command) {
			return true
		}
	}
	return false
}
func reviewHistorySchema(v2 bool) string {
	if v2 {
		return "readmit-hub-review-history/v2"
	}
	return "readmit-hub-review-history/v1"
}
func auditReviewSchema(events []ReviewEvent) string {
	if hasSupportEvents(events) {
		return "readmit-hub-audit/v2"
	}
	return "readmit-hub-audit/v1"
}
func validReviewEventVersion(e ReviewEvent, allowV2 bool) bool {
	if supportCommand(e.Command) {
		return allowV2 && e.Schema == "readmit-hub-review-event/v2"
	}
	return e.Command.Schema == "readmit-hub-review-command/v1" && e.Schema == "readmit-hub-review-event/v1"
}
func supportCurrent(c ReviewCommand, events []ReviewEvent) bool {
	if c.Kind == "support-policy" {
		return true
	}
	if c.Release != currentSupportPolicy(events).Evidence {
		return false
	}
	if supportRequest(c) {
		return c.Parent == currentSupportPolicy(events).ID
	}
	for _, e := range events {
		if e.Command.ID == c.Parent && supportRequest(e.Command) {
			return e.Command.Parent == currentSupportPolicy(events).ID
		}
	}
	return false
}
