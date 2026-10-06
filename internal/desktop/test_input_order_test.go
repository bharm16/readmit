package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/grid"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestTestDraftRefusesAReplacedCaptureAfterMessageSelection(t *testing.T) {
	app, context := namedProject(t)
	source := writeCase(t, context.Project, "capture", framed(sampleImportHL7))
	registerCase(t, context.Project, "capture", "Pinned capture")
	item := caseAt(t, app, context, "capture")
	request := desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{
		Case: item.Ref, Identity: source.Identity, Messages: []string{"s0001-e000001"},
	}}
	if got := app.OpenItemDraft(request); got.State != desktop.Completed || got.Draft.Test.Case.Identity != source.Identity {
		t.Fatalf("unchanged selected evidence: %+v", got)
	}
	writeCase(t, context.Project, "replacement", framed(secondImportHL7))
	if err := os.Rename(filepath.Join(context.Project, "capture"), filepath.Join(context.Project, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(context.Project, "replacement"), filepath.Join(context.Project, "capture")); err != nil {
		t.Fatal(err)
	}
	if got := app.OpenItemDraft(request); got.State != desktop.Failed || got.Draft != nil {
		t.Fatalf("same occurrence ID from replacement evidence created a draft: %+v", got)
	}
}

func TestIncompleteTestDraftKeepsExplicitInputOrderBeforeAnyExecutionSetup(t *testing.T) {
	app, context := namedProject(t)
	source := writeCase(t, context.Project, "capture", framed(sampleImportHL7)+framed(secondImportHL7))
	registerCase(t, context.Project, "capture", "Ordered capture")
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	requested := []string{source.Events[1].ID, source.Events[0].ID}
	answer := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: listed.Page.Items[0].Ref, Messages: requested, Title: "Reversed input draft"}})
	if answer.State != desktop.Completed || answer.Draft == nil || answer.Draft.Test == nil || !slices.Equal(answer.Draft.Test.Messages, requested) || answer.Draft.Test.Target != "" {
		t.Fatalf("ordered incomplete draft: %+v", answer.Draft)
	}
}

func TestFHIRDraftRequestsKeepExplicitRetainedInputOrder(t *testing.T) {
	app, context := namedProject(t)
	file := sourceFile(t, "owned-r4.json", `{"resourceType":"Bundle","type":"collection","entry":[{"resource":{"resourceType":"Patient","active":true}},{"resource":{"resourceType":"Observation","status":"final","code":{"text":"Owned synthetic marker"}}}]}`)
	source := desktop.ImportRequest{Context: context, Mode: "fhir-r4", Files: []string{file}, FHIR: &fhirevidence.Declaration{SourceKind: "bundle", Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"}}}
	preview := app.PreviewImport(source)
	imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Owned R4", Source: source, PreviewToken: preview.PreviewToken, IntentID: "ordered-fhir"})
	if imported.Case == nil {
		t.Fatalf("imported evidence: %+v", imported)
	}
	opened := app.OpenItem(desktop.ItemRequest{Context: context, Ref: *imported.Case})
	if opened.Item == nil || opened.Item.Summary.Case == nil {
		t.Fatalf("evidence item: %+v", opened)
	}
	verified := app.OpenCase(context.Project, opened.Item.Summary.Case.Entry)
	if verified.Case == nil {
		t.Fatalf("verified evidence: %+v", verified)
	}
	rows := app.ReadMessages(desktop.MessagesRequest{Workspace: context.Project, Case: verified.Case.Name, Identity: verified.Case.Identity, Query: grid.Query{}})
	if len(rows.Rows) != 3 {
		t.Fatalf("retained resources: %+v", rows)
	}
	requested := []string{rows.Rows[2].ID, rows.Rows[1].ID}
	request := desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: *imported.Case, Identity: verified.Case.Identity, Messages: requested}}
	answer := app.OpenItemDraft(request)
	if answer.Draft == nil || answer.Draft.ConnectedTest == nil || len(answer.Draft.ConnectedTest.Steps) != 2 {
		t.Fatalf("typed draft: %+v", answer)
	}
	steps := answer.Draft.ConnectedTest.Steps
	if steps[0].Source.Occurrence != requested[0] || steps[1].Source.Occurrence != requested[1] || !slices.Equal(steps[1].After, []string{steps[0].ID}) {
		t.Fatalf("authored request order: %+v", steps)
	}
	request.From.Identity = "changed"
	if got := app.OpenItemDraft(request); got.State != desktop.Failed {
		t.Fatalf("different R4 source identity created a draft: %+v", got)
	}
}
