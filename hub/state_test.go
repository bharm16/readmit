package hub

import "testing"

func TestDeriveReviewsFindsThePolicyInForce(t *testing.T) {
	first := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "policy-1", Kind: "support-policy", Evidence: "digest-1", Text: "support"}
	second := first
	second.ID, second.Evidence = "policy-2", "digest-2"
	comment := ReviewCommand{Schema: "readmit-hub-review-command/v1", ID: "c1", Kind: "comment", Text: "hello"}
	events := []ReviewEvent{
		{Issuer: "issuer", Actor: "admin", Command: first},
		{Issuer: "issuer", Actor: "admin", Command: comment},
		{Issuer: "issuer", Actor: "admin", Command: second},
	}
	state := deriveReviews(events)
	if state.policy.ID != "policy-2" || state.policy.Evidence != "digest-2" {
		t.Fatalf("the last policy must be in force: %+v", state.policy)
	}
	if !state.support {
		t.Fatal("support commands went unseen")
	}
	if empty := deriveReviews(nil); empty.support || empty.policy.ID != "" {
		t.Fatalf("an empty log carries no policy: %+v", empty)
	}
	// A request under the replaced policy is history, not current.
	old := ReviewCommand{Schema: "readmit-hub-review-command/v2", ID: "req", Kind: "support-request", Parent: "policy-1", Release: "digest-1", Text: "support"}
	if state.current(old) {
		t.Fatal("a request under a replaced policy read as current")
	}
}

func TestDeriveLifecycleIsTheOneRemovedAndRetiredRule(t *testing.T) {
	events := []LifecycleEvent{
		{Issuer: "issuer", Command: LifecycleCommand{Kind: "remove-user", Subject: "alice"}},
		{Issuer: "other", Command: LifecycleCommand{Kind: "remove-user", Subject: "alice"}},
		{Issuer: "issuer", Command: LifecycleCommand{Kind: "retire", Artifact: "digest"}},
	}
	state := deriveLifecycle(events)
	if !state.isRemoved("issuer", "alice") {
		t.Fatal("a removed principal reads as present")
	}
	if state.isRemoved("other", "bob") || state.isRemoved("issuer", "bob") {
		t.Fatal("an unremoved principal reads as removed")
	}
	if !state.isRetired("digest") || state.isRetired("other") {
		t.Fatal("retirement misread")
	}
}
