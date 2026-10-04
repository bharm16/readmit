package desktop_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedMessagesExportWritesExactReviewedBytesOnceAndRetainsSourceHistory(t *testing.T) {
	parent := t.TempDir()
	dialogs := &chooser{folder: parent}
	app := activatedApp(t, dialogs, t.TempDir())
	root, _ := createdProject(t, app, parent)
	first := "MSH|^~\\&|OWNED|SYNTH||||20260101120000||SIU^S12|FIRST|T|2.5.1\rPID|1||FIRST^^^OWNED\r"
	second := "MSH|^~\\&|OWNED|SYNTH||||20260101120100||SIU^S13|SECOND|T|2.5.1\rPID|1||SECOND^^^OWNED\r"
	source := writeCase(t, root, "capture", framed(first)+framed(second))
	registerCase(t, root, "capture", "Owned capture")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("project: %+v", opened)
	}
	capture := caseAt(t, app, opened.Context, "capture")
	request := desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportSelectedMessagesAction, Items: []desktop.ItemRef{capture.Ref}, SelectedExport: &desktop.SelectedExportOptions{Messages: []string{source.Events[1].ID, source.Events[0].ID}, Identity: source.Identity, Transformation: "original", Reveal: true}}
	prepared := app.PrepareAction(request)
	if prepared.Review == nil || prepared.Review.SelectedExport == nil {
		t.Fatalf("preview: %+v", prepared)
	}
	shown := prepared.Review.SelectedExport
	expected := []byte(framed(second) + framed(first))
	if !bytes.Equal(sharedBytes(t, shown.Output.Files[0]), expected) || !shown.SourceValues || shown.Redacted || shown.Class != "selected-original-message-bytes" {
		t.Fatalf("dishonest output: %+v", shown)
	}
	output := filepath.Join(t.TempDir(), "selected.hl7")
	dialogs.destination = output
	chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: opened.Context, Name: shown.Output.Name})
	request.SelectedExport.Destination = chosen.Destination
	prepared = app.PrepareAction(request)
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("review: %+v", prepared)
	}
	final := desktop.ExecuteActionRequest{Context: opened.Context, Token: prepared.Review.Token, IntentID: "selected-1"}
	exported := app.ExecuteReviewedAction(final)
	if exported.State != desktop.Completed || exported.SelectedExport == nil || exported.SelectedExport.History == nil {
		t.Fatalf("export: %+v", exported)
	}
	raw, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(raw, expected) {
		t.Fatal("written output differs from reviewed original bytes")
	}
	again := app.ExecuteReviewedAction(final)
	if again.State != desktop.Completed || !again.Replayed {
		t.Fatalf("duplicate was not idempotent: %+v", again)
	}
	history := app.ListSourceExports(desktop.ItemRequest{Context: opened.Context, Ref: capture.Ref})
	if history.State != desktop.Completed || len(history.Entries) != 1 || history.Entries[0].Source.Identity != source.Identity || len(history.Entries[0].Source.Occurrences) != 2 {
		t.Fatalf("source history: %+v", history)
	}
	// Reads return detached receipt scopes; callers cannot rewrite retained history.
	history.Entries[0].Source.Occurrences[0].Occurrence = "other"
	if app.ListSourceExports(desktop.ItemRequest{Context: opened.Context, Ref: capture.Ref}).Entries[0].Source.Occurrences[0].Occurrence != source.Events[1].ID {
		t.Fatal("history aliases returned scope")
	}

	dialogs.files = []string{output}
	preview := app.OpenSourceExport(desktop.ItemRequest{Context: opened.Context, Ref: history.Entries[0].Ref})
	if preview.State != desktop.Completed || preview.Output == nil || !bytes.Equal(sharedBytes(t, preview.Output.Files[0]), expected) {
		t.Fatalf("real output readback: %+v", preview)
	}
	if err := os.WriteFile(output, []byte("changed output"), 0600); err != nil {
		t.Fatal(err)
	}
	rejected := app.OpenSourceExport(desktop.ItemRequest{Context: opened.Context, Ref: history.Entries[0].Ref})
	if rejected.State != desktop.Failed || rejected.Output != nil {
		t.Fatalf("changed output inherited the receipt: %+v", rejected)
	}
}

func TestSelectedExportRefusesChangedEvidenceDestinationAndUnsupportedTransformations(t *testing.T) {
	for _, change := range []string{"evidence", "destination", "parent"} {
		t.Run(change, func(t *testing.T) {
			parent := t.TempDir()
			dialogs := &chooser{folder: parent}
			app := activatedApp(t, dialogs, t.TempDir())
			root, _ := createdProject(t, app, parent)
			source := writeCase(t, root, "capture", framed("MSH|^~\\&|OWNED||||20260101120000||SIU^S12|FIRST|T|2.5.1\r"))
			registerCase(t, root, "capture", "Source")
			opened := app.OpenNamedProject(root)
			capture := caseAt(t, app, opened.Context, "capture")
			outputDir := filepath.Join(t.TempDir(), "output")
			if err := os.Mkdir(outputDir, 0700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(outputDir, "selected.hl7")
			dialogs.destination = output
			chosen := app.ChooseShareDestination(desktop.ShareDestinationRequest{Context: opened.Context, Name: "Selected messages.hl7"})
			request := desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportSelectedMessagesAction, Items: []desktop.ItemRef{capture.Ref}, SelectedExport: &desktop.SelectedExportOptions{Messages: []string{source.Events[0].ID}, Identity: source.Identity, Transformation: "original", Destination: chosen.Destination, Reveal: true}}
			reviewed := app.PrepareAction(request)
			if reviewed.Review == nil || !reviewed.Review.Ready {
				t.Fatalf("review: %+v", reviewed)
			}
			switch change {
			case "evidence":
				payload := filepath.Join(root, "capture", source.Events[0].Payload.Path)
				raw, err := os.ReadFile(payload)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(payload, append(raw, '\r'), 0600); err != nil {
					t.Fatal(err)
				}
			case "destination":
				if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			case "parent":
				if err := os.Rename(outputDir, outputDir+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(outputDir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: opened.Context, Token: reviewed.Review.Token, IntentID: "changed"})
			if refused.Outcome != desktop.ActionStale {
				t.Fatalf("changed %s did not invalidate review: %+v", change, refused)
			}
			if change == "destination" {
				raw, _ := os.ReadFile(output)
				if string(raw) != "existing" {
					t.Fatal("changed destination overwritten")
				}
			} else if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("stale review wrote output")
			}
		})
	}
	parent := t.TempDir()
	dialogs := &chooser{folder: parent}
	app := activatedApp(t, dialogs, t.TempDir())
	root, _ := createdProject(t, app, parent)
	source := writeCase(t, root, "capture", framed("MSH|^~\\&|OWNED||||20260101120000||SIU^S12|FIRST|T|2.5.1\r"))
	registerCase(t, root, "capture", "Source")
	opened := app.OpenNamedProject(root)
	capture := caseAt(t, app, opened.Context, "capture")
	refused := app.PrepareAction(desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportSelectedMessagesAction, Items: []desktop.ItemRef{capture.Ref}, SelectedExport: &desktop.SelectedExportOptions{Messages: []string{source.Events[0].ID}, Identity: source.Identity, Transformation: "redacted"}})
	if refused.State != desktop.Failed || refused.Review != nil {
		t.Fatalf("unsupported redaction claimed: %+v", refused)
	}
}
