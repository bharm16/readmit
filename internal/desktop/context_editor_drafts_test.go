package desktop_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestContextEditorsRetainPartialAuthoredWorkAndRejectUnknownConsent(t *testing.T) {
	app, ctx := namedProject(t)
	source := writeCase(t, ctx.Project, "draft-source", framed(sampleImportHL7))
	registerCase(t, ctx.Project, "draft-source", "Draft source")
	listing := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem})
	ref := listing.Page.Items[0].Ref
	for _, kind := range []string{"interface-association", "capture-source-context"} {
		schema := "readmit-interface-association-editor/v1"
		content := map[string]any{"schema": schema, "project_id": listing.Context.ProjectID, "source": ref, "identity": source.Identity, "occurrence": "", "selector": "", "name": "Partial authored name", "documents": []any{}}
		if kind == "capture-source-context" {
			schema = "readmit-capture-source-context-editor/v1"
			content = map[string]any{"schema": schema, "project_id": listing.Context.ProjectID, "source": ref, "identity": source.Identity, "sources": []any{map[string]any{"source_id": source.Manifest.Sources[0].ID, "source": "Authored partial", "channel": "", "basis": "manual"}}}
		}
		raw, _ := json.Marshal(content)
		saved := app.SaveEditorDraft(desktop.EditorDraft{Kind: kind, Workspace: ctx.Project, Identity: source.Identity, ContentSchema: schema, Content: raw})
		if saved.State != desktop.Completed {
			t.Fatalf("partial %s refused: %+v", kind, saved)
		}
		content["consent"] = true
		bad, _ := json.Marshal(content)
		rejected := app.SaveEditorDraft(desktop.EditorDraft{Kind: kind, Workspace: ctx.Project, Identity: source.Identity, ContentSchema: schema, Content: bad})
		if rejected.State != desktop.Failed {
			t.Fatalf("unknown consent accepted: %+v", rejected)
		}
		restored := app.ReadContextEditorDraft(desktop.ContextEditorDraftRequest{Context: listing.Context, Schema: schema, Source: ref, Identity: source.Identity})
		if restored.State != desktop.Completed || restored.Retained == nil {
			t.Fatalf("partial work lost: %+v", restored)
		}
		if strings.Contains(string(restored.Retained.Content), "consent") {
			t.Fatal("refused consent replaced authored work")
		}
	}
}
