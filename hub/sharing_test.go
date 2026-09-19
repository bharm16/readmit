package hub

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/sharing"
)

func TestSupportPolicyAndExactAuthenticatedReview(t *testing.T) {
	policy := []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	p := sharing.Digest(policy)
	summary := sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet", SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64), SpecIdentity: strings.Repeat("3", 64), PolicyIdentity: p, Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope}
	raw, _ := json.Marshal(summary, json.Deterministic(true))
	d := sharing.Digest(raw)
	files := map[string][]byte{p: policy, d: raw}
	load := func(d string) ([]byte, error) {
		v, ok := files[d]
		if !ok {
			return nil, ErrMissing
		}
		return v, nil
	}
	policyCommand := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "policy", Kind: "support-policy", Evidence: p, Text: "support"}
	events := []ReviewEvent{{Schema: "readmit-hub-review-event/v2", Issuer: "issuer", Actor: "admin", Command: policyCommand}}
	request := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "request", Parent: "policy", Kind: "support-request", Evidence: d, Release: p, Recipient: "reviewer", Text: "support"}
	if e := validateSupport(request, "analyst", "issuer", events, load); e != nil {
		t.Fatal(e)
	}
	events = append(events, ReviewEvent{Schema: "readmit-hub-review-event/v2", Issuer: "issuer", Actor: "analyst", Command: request})
	approval := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "approve", Kind: "support-approval", Evidence: d, Release: p, Parent: "request", Text: "support"}
	if _, e := approvedSupport(d, events, load); e == nil {
		t.Fatal("unapproved download")
	}
	for _, actor := range []string{"analyst", "owner", "runner"} {
		if e := validateSupport(approval, actor, "issuer", events, load); e == nil {
			t.Fatal("wrong actor approved")
		}
	}
	if e := validateSupport(approval, "reviewer", "wrong-issuer", events, load); e == nil {
		t.Fatal("wrong issuer approved")
	}
	if e := validateSupport(approval, "reviewer", "issuer", events, load); e != nil {
		t.Fatal(e)
	}
	events = append(events, ReviewEvent{Schema: "readmit-hub-review-event/v2", Issuer: "issuer", Actor: "reviewer", Command: approval})
	if got, e := approvedSupport(d, events, load); e != nil || string(got) != string(raw) {
		t.Fatal("approved summary unavailable")
	}
	changed := append(policy, ' ')
	changedID := sharing.Digest(changed)
	files[changedID] = changed
	updated := policyCommand
	updated.Evidence = changedID
	updated.ID = "policy-next"
	events = append(events, ReviewEvent{Schema: "readmit-hub-review-event/v2", Command: updated})
	if _, e := approvedSupport(d, events, load); e == nil {
		t.Fatal("old approval survived policy version change")
	}
}

func TestSupportVersionedCommandsRejectOldSchemasAndInjectedIdentity(t *testing.T) {
	c := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "policy", Kind: "support-policy", Evidence: strings.Repeat("a", 64), Text: "support"}
	raw, _ := json.Marshal(c)
	if _, e := decodeReviewCommand(raw); e != nil {
		t.Fatal(e)
	}
	for _, mutated := range []string{
		strings.Replace(string(raw), "readmit-hub-review-command/v2", "readmit-hub-review-command/v1", 1),
		strings.TrimSuffix(string(raw), "}") + `,"actor":"owner"}`,
		strings.Replace(string(raw), `"support"`, `"PLANTED-PRIVATE-TEXT"`, 1),
		strings.Replace(string(raw), `"expected":0,`, "", 1),
	} {
		if _, e := decodeReviewCommand([]byte(mutated)); e == nil {
			t.Fatal("invalid support command accepted")
		}
	}
	event := ReviewEvent{Schema: "readmit-hub-review-event/v2", Command: c}
	if validReviewEventVersion(event, false) || !validReviewEventVersion(event, true) {
		t.Fatal("frozen event boundary widened")
	}
	event.Schema = "readmit-hub-review-event/v1"
	if validReviewEventVersion(event, true) {
		t.Fatal("new command under old event")
	}
}
