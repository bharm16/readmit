package desktop_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

func TestConfirmedMessageFindingStartsOnlyItsEvidenceWithoutInventedExpectations(t *testing.T) {
	app, project := namedProject(t)
	// Only the second message lacks a required patient identifier.
	booking := strings.ReplaceAll(fixture(t, "diagnose-booking.hl7"), "\n", "\r")
	bad := strings.ReplaceAll(strings.Replace(fixture(t, "diagnose-reschedule.hl7"), "PATIENT-001^^^READMIT", "", 1), "\n", "\r")
	writeCase(t, project.Project, "incident", framed(booking)+framed(bad))
	item := caseAt(t, app, project, "incident")
	identity := caseIdentityOf(t, app, project.Project, "incident")
	analysis := analyzed(t, app, project, item, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "analysis")
	var finding string
	for _, row := range analysis.Analysis.Findings {
		if row.RuleID == diagnose.RequiredField {
			finding = row.ID
		}
	}
	if finding == "" {
		t.Fatal("the independently malformed message must have a required-field finding")
	}
	review := reviewSaved(t, app, project, "", "", "confirm", desktop.FindingReviewDraft{
		Analysis: analysis.Analysis.Ref, ReportSHA256: analysis.Analysis.ReportSHA256,
		Decisions: []findingreview.Decision{{Finding: finding, Verdict: findingreview.Confirmed, Rationale: "Missing patient identifier"}},
	})
	origin := desktop.TestOrigin{Case: item.Ref, Title: "Reject missing patient identifier", Source: &desktop.TestSource{
		Kind: desktop.SourceFinding, Finding: finding, Review: review.Saved.ID, ReportSHA256: analysis.Analysis.ReportSHA256,
	}}
	open := func() desktop.ItemDraftResult {
		return app.OpenItemDraft(desktop.ItemRequest{Context: project, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &origin})
	}
	got := open()
	if got.State != desktop.Completed || got.Draft == nil || got.Draft.Test == nil || !slices.Equal(got.Draft.Test.Messages, []string{"s0001-e000002"}) {
		t.Fatalf("only the finding's original message becomes input: %+v", got)
	}
	if len(got.Draft.Test.Expectations) != 0 || len(got.Test.Proposals) != 0 || got.Draft.TestLinks == nil || *got.Draft.TestLinks.Source != *origin.Source {
		t.Fatalf("a finding supplies provenance, not an expected passing value: %+v", got)
	}
	// A caller cannot broaden the evidence selection by supplying unrelated inputs.
	origin.Messages = []string{"s0001-e000001"}
	if got := open(); got.State != desktop.Completed || !slices.Equal(got.Draft.Test.Messages, []string{"s0001-e000002"}) {
		t.Fatalf("caller selection replaced the finding: %+v", got)
	}
	original := *origin.Source
	for _, altered := range []desktop.TestSource{
		{Kind: desktop.SourceFinding, Finding: "missing", Review: original.Review, ReportSHA256: original.ReportSHA256},
		{Kind: desktop.SourceFinding, Finding: finding, Review: "missing", ReportSHA256: original.ReportSHA256},
		{Kind: desktop.SourceFinding, Finding: finding, Review: original.Review, ReportSHA256: strings.Repeat("0", 64)},
	} {
		origin.Source = &altered
		if got := open(); got.State != desktop.Failed {
			t.Fatalf("unavailable finding provenance created a test: %+v", got)
		}
	}
	origin.Source = &original
	// A stale confirmation cannot keep creating a confirmed-finding draft.
	reviewSaved(t, app, project, review.Saved.ID, review.Saved.Revision, "undo", desktop.FindingReviewDraft{
		Analysis: analysis.Analysis.Ref, ReportSHA256: analysis.Analysis.ReportSHA256, Decisions: []findingreview.Decision{},
	})
	if got := open(); got.State != desktop.Failed {
		t.Fatalf("withdrawn confirmation created a test: %+v", got)
	}
}

func TestConfirmedACKFindingStillProposesItsLinkedResponseWithoutAcceptingIt(t *testing.T) {
	app, project, item, identity := findingsProject(t)
	analysis := analyzed(t, app, project, item, identity, desktop.AnalysisProfileRef{Builtin: "siu"}, "analysis")
	var finding string
	for _, row := range analysis.Analysis.Findings {
		if row.RuleID == diagnose.ACKOutcome {
			finding = row.ID
		}
	}
	if finding == "" {
		t.Fatal("the fixture's AE acknowledgement must be diagnosed")
	}
	review := reviewSaved(t, app, project, "", "", "confirm", desktop.FindingReviewDraft{
		Analysis: analysis.Analysis.Ref, ReportSHA256: analysis.Analysis.ReportSHA256,
		Decisions: []findingreview.Decision{{Finding: finding, Verdict: findingreview.Confirmed, Rationale: "Observed rejection"}},
	})
	got := app.OpenItemDraft(desktop.ItemRequest{Context: project, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{
		Case: item.Ref, Source: &desktop.TestSource{Kind: desktop.SourceFinding, Finding: finding, Review: review.Saved.ID, ReportSHA256: analysis.Analysis.ReportSHA256},
	}})
	if got.State != desktop.Completed || got.Draft == nil || got.Draft.Test == nil || !slices.Equal(got.Draft.Test.Messages, []string{"s0001-e000001"}) {
		t.Fatalf("ACK finding must select its linked input: %+v", got)
	}
	if len(got.Draft.Test.Expectations) != 0 || len(got.Test.Proposals) != 1 || got.Test.Proposals[0].Check.Field == nil || got.Test.Proposals[0].Check.Field.Text == nil || *got.Test.Proposals[0].Check.Field.Text != "AE" {
		t.Fatalf("observed AE must stay an undecided proposal: %+v", got)
	}
}
