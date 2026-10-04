package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/grid"
	"slices"
	"testing"
)

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
	answer := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: *imported.Case, Messages: requested}})
	if answer.Draft == nil || answer.Draft.ConnectedTest == nil || len(answer.Draft.ConnectedTest.Steps) != 2 {
		t.Fatalf("typed draft: %+v", answer)
	}
	steps := answer.Draft.ConnectedTest.Steps
	if steps[0].Source.Occurrence != requested[0] || steps[1].Source.Occurrence != requested[1] || !slices.Equal(steps[1].After, []string{steps[0].ID}) {
		t.Fatalf("authored request order: %+v", steps)
	}
}
