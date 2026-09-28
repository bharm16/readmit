package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
)

// The review the window keeps in a project is bound to the finding the
// command line reports. A case captured into a project, reviewed under link
// rules saved there from the shipped rules document, opens with exactly the
// links `readmit correlate` reports under that document, bound to the digest
// of what it printed; every decision is one catalog revision whose retained
// history the shared reader reads back against the command line's report,
// and the command line's own report is unchanged by it.
func TestTheWindowsProjectReviewIsBoundToTheReportReadmitCorrelateProduces(t *testing.T) {
	app, context := libraryProject(t)
	project := context.Project
	evidence := "../testdata/fixtures/case-evidence.mllp"
	if _, stderr, err := run(t, "capture", evidence, evidence, "--output", filepath.Join(project, "two-sources")); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	document, err := os.ReadFile(correlateRules)
	if err != nil {
		t.Fatal(err)
	}
	rulesFile := filepath.Join(t.TempDir(), correlationRulesEntry)
	if err := os.WriteFile(rulesFile, document, 0o600); err != nil {
		t.Fatal(err)
	}
	report, printed := runJSON[correlate.Report](t, "correlate", filepath.Join(project, "two-sources"), "--rules", rulesFile, "--format", "json")
	parsed, err := correlate.ParseRules(document)
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.LinkRulesItem, IntentID: "rules",
		Draft: desktop.ItemDraft{Name: "Shipped rules", LinkRules: &parsed}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("link rules: %+v", saved)
	}
	rules := *saved.Saved
	identity := verifiedIdentity(t, app, project, "two-sources")

	sequence := app.OpenSequence(desktop.SequenceRequest{Context: context, Case: "two-sources", Identity: identity, LinkRules: &rules, Limit: desktop.MaxSequenceEvents})
	if sequence.Sequence == nil || sequence.Sequence.RulesSHA256 != report.RulesSHA256 {
		t.Fatalf("the timeline applied rules other than the command line's: %+v", sequence)
	}
	request := desktop.CorrelationReviewRequest{Context: context, Case: "two-sources", Identity: identity, LinkRules: &rules}
	opened := app.OpenCorrelationReview(request)
	if opened.State != desktop.Completed || opened.View.Machine != sha256Hex([]byte(machineBytes(printed))) || opened.View.TotalLinks != len(report.Links) {
		t.Fatalf("the project's review is not bound to the command line's finding: %+v", opened)
	}
	for index, link := range report.Links {
		if reviewed := opened.View.Links[index]; reviewed.ID != link.ID || !reflect.DeepEqual(reviewed.Occurrences, link.Occurrences) || reviewed.Status != correlate.Unreviewed {
			t.Fatalf("link %s reviewed as %+v", link.ID, reviewed)
		}
	}

	pair := [2]string{report.Collisions[0].Occurrences[0].Occurrence, report.Collisions[0].Occurrences[1].Occurrence}
	decisions := []correlate.Decision{
		{Action: correlate.AcceptDecision, Link: report.Links[0].ID, Actor: "analyst one", Reason: "the acknowledgement names this booking"},
		{Action: correlate.RejectDecision, Link: report.Links[1].ID, Actor: "analyst two", Reason: "a separate observation of that message"},
		{Action: correlate.AddDecision, From: pair[0], To: pair[1], Actor: "analyst one", Reason: "the capture context places these together"},
	}
	request.Mapping = opened.View.Mapping
	for index, decision := range decisions {
		request.Decision, request.IntentID = decision, "decision-"+string(rune('1'+index))
		answer := app.DecideCorrelation(request)
		if answer.State != desktop.Completed || answer.Saved == nil || answer.Saved.Revision != string(rune('1'+index)) {
			t.Fatalf("decision %d: %+v", index+1, answer)
		}
		request.Review, request.Mapping = answer.Saved, answer.View.Mapping
	}

	// The retained history is read by the shared reader against the command
	// line's own report, decision for decision.
	var retained *correlate.ReviewRevision
	if err := filepath.WalkDir(project, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(path, "decisions.json") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if decoded, err := correlate.DecodeReview(data); err == nil && (retained == nil || len(decoded.Decisions) > len(retained.Decisions)) {
			retained = &decoded
		}
		return nil
	}); err != nil || retained == nil {
		t.Fatalf("no retained history: %v", err)
	}
	if _, view, err := correlate.Review(openedBundle(t, filepath.Join(project, "two-sources")), report, retained, true); err != nil || view.Mapping != request.Mapping {
		t.Fatalf("the retained history is not read against the command line's report: %v", err)
	}
	for index, decision := range decisions {
		kept := retained.Decisions[index]
		if kept.At.IsZero() {
			t.Fatalf("decision %d records no time", index+1)
		}
		kept.At = decision.At
		if kept != decision {
			t.Fatalf("decision %d retained as %+v", index+1, kept)
		}
	}
	if _, again := runJSON[correlate.Report](t, "correlate", filepath.Join(project, "two-sources"), "--rules", rulesFile, "--format", "json"); again != printed {
		t.Fatal("reviewing links changed what the command line reports")
	}
}
