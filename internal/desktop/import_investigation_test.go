package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"testing"
)

func TestLooseInvestigationRetainsExactSelectionAndRefusesChangedEvidence(t *testing.T) {
	app, context := namedProject(t)
	file := sourceFile(t, "two.hl7", "\x0b"+sampleImportHL7+"\x1c\r\x0b"+secondImportHL7+"\x1c\r")
	original := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
	probe := app.ProbeImport(desktop.ImportProbeRequest{Context: context, Files: []string{file}})
	source := desktop.ImportRequest{Context: context, Files: []string{file}, Mode: "plan", Plan: probe.Formats[*probe.Selected].Plan}
	preview := app.PreviewImport(source)
	selected := 1
	investigation := &desktop.ImportInvestigation{File: file, Identity: original.SHA256, Format: "auto", Terminator: "auto", Messages: []int{1, 0}, Selected: &selected, Path: "MSH-9.2"}
	imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Retained investigation", Source: source, PreviewToken: preview.PreviewToken, IntentID: "retain-investigation", Investigation: investigation})
	if imported.State != desktop.Completed || imported.Selection == nil || imported.Selection.Identity == "" || len(imported.Selection.Messages) != 2 || imported.Selection.Selected != imported.Selection.Messages[0] || imported.Selection.Path != "MSH-9.2" {
		t.Fatalf("retained navigation: %+v", imported)
	}
	replay := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Retained investigation", Source: source, PreviewToken: preview.PreviewToken, IntentID: "retain-investigation", Investigation: investigation})
	if replay.State != desktop.Completed || replay.Case.ID != imported.Case.ID || replay.Selection.Selected != imported.Selection.Selected {
		t.Fatalf("replayed import: %+v", replay)
	}
	changed := *investigation
	changed.Identity = "a" + investigation.Identity[1:]
	refused := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Changed", Source: source, PreviewToken: preview.PreviewToken, IntentID: "changed-investigation", Investigation: &changed})
	if refused.State != desktop.Failed || !refused.Stale || refused.Case != nil {
		t.Fatalf("changed source: %+v", refused)
	}
}
