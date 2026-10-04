package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"testing"
)

func TestSelectedMessageComparisonPreservesExplicitRolesAndOriginalRawDifferences(t *testing.T) {
	app, context := namedProject(t)
	original := writeCase(t, context.Project, "capture", framed(sampleImportHL7)+framed(secondImportHL7))
	registerCase(t, context.Project, "capture", "Owned pair source")
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	ref := listed.Page.Items[0].Ref
	answer := app.CompareCases(desktop.CaseComparisonRequest{Context: context, Current: ref, Other: ref, Pair: &desktop.SelectedMessagePair{Left: original.Events[1].ID, Right: original.Events[0].ID}, Reveal: true, Limit: 100})
	if answer.State != desktop.Completed || answer.Comparison == nil || answer.Comparison.Alignment != "explicit-occurrences" || answer.Comparison.Current.Occurrence != original.Events[1].ID || answer.Comparison.Other.Occurrence != original.Events[0].ID || answer.Comparison.RawEqual == nil || *answer.Comparison.RawEqual || answer.Comparison.Current.RawSHA256 == answer.Comparison.Other.RawSHA256 {
		t.Fatalf("selected pair: %+v", answer)
	}
	verified := app.OpenCase(context.Project, "capture")
	if verified.Case == nil || verified.Case.Identity != original.Identity {
		t.Fatalf("pair changed original evidence: %+v", verified)
	}
}
