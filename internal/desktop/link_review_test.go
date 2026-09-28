package desktop_test

import (
	jsonv1 "encoding/json"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
)

// decideAsTheWindowSends decides through the request exactly as the window's
// bindings send it and Wails decodes it: encoding/json over the members the
// window writes, with a decision that carries no time of its own.
func decideAsTheWindowSends(t *testing.T, app *desktop.App, context desktop.RequestContext, identity string, rules *desktop.ItemRef, review *desktop.ItemRef, mapping, intent string, decision map[string]string) desktop.CorrelationReviewResult {
	t.Helper()
	sent := map[string]any{
		"context": context, "rules_sha256": "", "offset": 0, "workspace": "", "case": "incident", "identity": identity, "rules": "",
		"previous": "", "mapping": mapping, "show_values": false, "output": "", "intent_id": intent,
		"decision": map[string]string{"action": decision["action"], "link": decision["link"], "from": decision["from"], "to": decision["to"], "actor": decision["actor"], "reason": decision["reason"]},
	}
	if rules != nil {
		sent["link_rules"] = rules
	}
	if review != nil {
		sent["review"] = review
	}
	data, err := jsonv1.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	var request desktop.CorrelationReviewRequest
	if err := jsonv1.Unmarshal(data, &request); err != nil {
		t.Fatalf("the window's decision does not decode: %v\n%s", err, data)
	}
	return app.DecideCorrelation(request)
}

// relationOf is the relation of one id the timeline answers, or nil.
func relationOf(sequence *desktop.Sequence, id string) *desktop.Relation {
	for i := range sequence.Relations {
		if sequence.Relations[i].ID == id {
			return &sequence.Relations[i]
		}
	}
	return nil
}

// A decision decodes exactly as the window sends it — no time of its own,
// which the facade stamps — for a review, an added link and an undo.
func TestAWindowDecisionDecodesAsWailsSendsItAndIsRecorded(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	opened := app.OpenCorrelationReview(linkReviewRequest(context, identity, rules))
	link := opened.View.Links[0].ID
	reviewed := decideAsTheWindowSends(t, app, context, identity, &rules, nil, opened.View.Mapping, "review",
		map[string]string{"action": "reject", "link": link, "actor": "Dana", "reason": "separate booking"})
	if reviewed.State != desktop.Completed || reviewed.Saved == nil {
		t.Fatalf("review: %+v", reviewed)
	}
	added := decideAsTheWindowSends(t, app, context, identity, &rules, reviewed.Saved, reviewed.View.Mapping, "add",
		map[string]string{"action": "add", "from": "s0001-e000003", "to": "s0002-e000001", "actor": "Dana", "reason": "same booking"})
	if added.State != desktop.Completed || added.Saved == nil {
		t.Fatalf("add link: %+v", added)
	}
	undone := decideAsTheWindowSends(t, app, context, identity, &rules, added.Saved, added.View.Mapping, "undo",
		map[string]string{"action": "withdraw", "link": link, "actor": "Dana", "reason": "Undo decision"})
	if undone.State != desktop.Completed || undone.Saved == nil || undone.View.TotalDecisions != 3 {
		t.Fatalf("undo: %+v", undone)
	}
}

// With no link rules chosen, the case's recorded links are reviewed: their
// own history of the case, bound to no rules, which the timeline overlays
// under Recorded links exactly as a named review overlays its rules' links.
// Review, Add link, History and Undo all work there, a withdrawn or rejected
// link says so on its status, and neither history is applied to the other.
func TestRecordedLinksAreReviewedAddedAndUndoneWithNoLinkRules(t *testing.T) {
	app, context, identity, _ := timelineProject(t)
	read := func() *desktop.Sequence {
		t.Helper()
		return laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, Limit: desktop.MaxSequenceEvents})
	}
	before := read()
	var recorded *desktop.Relation
	for i := range before.Relations {
		if before.Relations[i].Basis == desktop.RecordedRelation && !before.Relations[i].Ambiguous {
			recorded = &before.Relations[i]
		}
	}
	if recorded == nil || recorded.Status != correlate.Unreviewed || before.Review != nil || before.Mapping == "" {
		t.Fatalf("recorded links under no rules are not reviewable: %+v", before.Relations)
	}
	link := recorded.ID

	rejected := decideAsTheWindowSends(t, app, context, identity, nil, nil, before.Mapping, "reject",
		map[string]string{"action": "reject", "link": link, "actor": "Dana", "reason": "the ACK belongs to another booking"})
	if rejected.State != desktop.Completed || rejected.Saved == nil || rejected.Saved.Kind != desktop.LinkReviewItem {
		t.Fatalf("reviewing a recorded link: %+v", rejected)
	}
	after := read()
	if after.Review == nil || *after.Review != *rejected.Saved || relationOf(after, link).Status != correlate.Rejected {
		t.Fatalf("the timeline did not overlay the recorded review: %+v %+v", after.Review, relationOf(after, link))
	}

	added := decideAsTheWindowSends(t, app, context, identity, nil, after.Review, after.Mapping, "add",
		map[string]string{"action": "add", "from": "s0001-e000001", "to": "s0002-e000001", "actor": "Dana", "reason": "same booking"})
	if added.State != desktop.Completed {
		t.Fatalf("adding a link under recorded links: %+v", added)
	}
	withAdded := read()
	var manual *desktop.Relation
	for i := range withAdded.Relations {
		if withAdded.Relations[i].Basis == desktop.ReviewedRelation {
			manual = &withAdded.Relations[i]
		}
	}
	if manual == nil || manual.Status != correlate.Accepted || manual.BasisName != "Reviewed" {
		t.Fatalf("the added link is not a reviewed relation: %+v", withAdded.Relations)
	}

	withdrawn := decideAsTheWindowSends(t, app, context, identity, nil, withAdded.Review, withAdded.Mapping, "withdraw-added",
		map[string]string{"action": "withdraw", "link": manual.ID, "actor": "Dana", "reason": "Undo decision"})
	undone := decideAsTheWindowSends(t, app, context, identity, nil, withdrawn.Saved, withdrawn.View.Mapping, "undo",
		map[string]string{"action": "withdraw", "link": link, "actor": "Dana", "reason": "Undo decision"})
	if withdrawn.State != desktop.Completed || undone.State != desktop.Completed {
		t.Fatalf("undo under recorded links: %+v %+v", withdrawn, undone)
	}
	final := read()
	if relationOf(final, link).Status != correlate.Unreviewed || relationOf(final, manual.ID).Status != correlate.Withdrawn {
		t.Fatalf("undone links: %+v %+v", relationOf(final, link), relationOf(final, manual.ID))
	}

	history := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{Context: context, Case: "incident", Identity: identity, Link: link, ShowValues: true})
	if history.State != desktop.Completed || len(history.View.History) != 2 || history.View.History[0].Actor != "Dana" ||
		history.View.History[0].Reason != "the ACK belongs to another booking" || history.View.History[1].Action != correlate.WithdrawDecision {
		t.Fatalf("the recorded link's history: %+v", history)
	}

	// The recorded review is never applied under link rules, nor a rules'
	// review under recorded links.
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, seqRules))
	underRules := laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	if underRules.Review != nil || relationOf(underRules, link).Status != "" {
		t.Fatalf("the recorded review was applied under link rules: %+v %+v", underRules.Review, relationOf(underRules, link))
	}
	if got := app.OpenSequence(desktop.SequenceRequest{Context: context, Case: "incident", Identity: identity, Review: final.Review, LinkRules: &rules, Limit: desktop.MaxSequenceEvents}); got.State != desktop.Failed {
		t.Fatalf("the recorded review was applied to rule links on request: %+v", got)
	}
}

// A collision of a rule, and an acknowledgement matching more than one
// message, are relationships but never links: each is marked ambiguous,
// carries no review status, and a decision about one is refused in words.
func TestAnAmbiguousRelationshipIsNeverReviewable(t *testing.T) {
	app, context, _, _ := timelineProject(t)
	written := writeInputs(t, context.Project, "duplicates", []bundle.Input{{
		Path: "a", Data: []byte(framed(seqBooking) + framed(seqBooking)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
		Observations: map[int]bundle.Observation{1: {Direction: bundle.Outbound, ObservedAt: seqTime(1)}, 2: {Direction: bundle.Outbound, ObservedAt: seqTime(2)}},
	}})
	rules := saveLinkRules(t, app, context, "", "", "rules", rulesOf(t, `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"same","operator":"control-id","scope":"source"}]}`))
	sequence := laidOut(t, app, desktop.SequenceRequest{Context: context, Case: "duplicates", Identity: written.Identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	collision := relationOf(sequence, "collision-1")
	if collision == nil || !collision.Ambiguous || collision.Status != "" || collision.Basis != desktop.RuleRelation {
		t.Fatalf("a collision is not marked unreviewable: %+v", sequence.Relations)
	}
	request := desktop.CorrelationReviewRequest{Context: context, Case: "duplicates", Identity: written.Identity, LinkRules: &rules, Mapping: sequence.Mapping, IntentID: "collision",
		Decision: correlate.Decision{Action: correlate.RejectDecision, Link: collision.ID, Actor: "Dana", Reason: "not one booking"}}
	refused := app.DecideCorrelation(request)
	if refused.State != desktop.Failed || refused.Saved != nil || !strings.Contains(refused.Reason, "ambiguous relationship") {
		t.Fatalf("a decision about a collision: %+v", refused)
	}
}
