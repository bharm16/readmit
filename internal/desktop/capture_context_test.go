package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureContextKeepsManualBasisBesideEvidenceAndRefusesInventedObservedProvenance(t *testing.T) {
	app, context := namedProject(t)
	original := writeCase(t, context.Project, "capture", framed(sampleImportHL7))
	registerCase(t, context.Project, "capture", "Original capture")
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	source := listed.Page.Items[0].Ref
	first := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: source})
	if first.State != desktop.Completed || first.Capture == nil || len(first.Capture.Sources) != 1 || first.Capture.Sources[0].Basis != "unknown" || first.Capture.Sources[0].ReceivedAt != nil {
		t.Fatalf("unknown context: %+v", first)
	}
	manual := first.Capture.Sources
	manual[0].Basis = "manual"
	manual[0].Source = "Operator supplied feed"
	manual[0].Channel = "Operator supplied channel"
	saved := app.SaveCaptureContext(desktop.CaptureContextSaveRequest{Context: context, Source: source, Identity: original.Identity, IntentID: "manual-provenance", Sources: manual})
	if saved.State != desktop.Completed || saved.Capture == nil || saved.Capture.Sources[0].Basis != "manual" {
		t.Fatalf("manual context: %+v", saved)
	}
	read := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: source})
	if read.State != desktop.Completed || read.Capture.Sources[0].Source != "Operator supplied feed" || read.Capture.Identity != original.Identity {
		t.Fatalf("retained context: %+v", read)
	}
	manual[0].Basis = "observed"
	refused := app.SaveCaptureContext(desktop.CaptureContextSaveRequest{Context: context, Source: source, Identity: original.Identity, Base: saved.Ref, IntentID: "invented-provenance", Sources: manual})
	if refused.State != desktop.Failed {
		t.Fatalf("invented observed metadata: %+v", refused)
	}
	manual[0].Basis = "manual"
	stale := app.SaveCaptureContext(desktop.CaptureContextSaveRequest{Context: context, Source: source, Identity: original.Identity, IntentID: "stale-provenance", Sources: manual})
	if stale.State != desktop.Failed {
		t.Fatalf("stale edit overwrote context: %+v", stale)
	}
	verified := app.OpenCase(context.Project, "capture")
	if verified.Case == nil || verified.Case.Identity != original.Identity {
		t.Fatalf("context changed original evidence: %+v", verified)
	}
	if private := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaptureContextItem}); private.State != desktop.Failed || private.Page != nil {
		t.Fatalf("internal context surfaced as a normal collection: %+v", private)
	}
	member := memberEntry(t, context.Project, *saved.Ref, string(desktop.CaptureContextItem))
	path := filepath.Join(context.Project, member)
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(retained, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	tampered := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: source})
	if tampered.State != desktop.Failed || tampered.Capture != nil {
		t.Fatalf("changed catalog member was accepted: %+v", tampered)
	}
}

func TestRememberedSourceChannelMappingPinsTargetRevisionAndAmbiguityStaysUnknown(t *testing.T) {
	app, context := namedProject(t)
	target := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "receiver", Draft: desktop.ItemDraft{Name: "QA receiver", Environment: environmentDraft("127.0.0.1:65530", "nonproduction", "plain")}})
	importSource := func(name string) desktop.ItemRef {
		file := sourceFile(t, name+".csv", "time,payload,source,channel\n2026-01-01T12:00:00Z,\""+sampleImportHL7+"\",sys,ch\n")
		recipe := validDesktopRecipe()
		request := desktop.ImportRequest{Context: context, Mode: "recipe", Files: []string{file}, Recipe: &recipe}
		preview := app.PreviewImport(request)
		imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: name, Source: request, PreviewToken: preview.PreviewToken, IntentID: name})
		if imported.Case == nil {
			t.Fatalf("import: %+v", imported)
		}
		return *imported.Case
	}
	first := importSource("first-mapped")
	metadata := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: first})
	if metadata.Capture == nil || metadata.Capture.Sources[0].ReceiptIdentity == "" {
		t.Fatalf("retained recipe metadata: %+v", metadata)
	}
	manual := metadata.Capture.Sources
	manual[0].Basis = "manual"
	manual[0].ReceivedAt = &target
	manual[0].Remember = true
	saved := app.SaveCaptureContext(desktop.CaptureContextSaveRequest{Context: context, Source: first, Identity: metadata.Capture.Identity, IntentID: "remember-first", Sources: manual})
	if saved.State != desktop.Completed {
		t.Fatalf("remember: %+v", saved)
	}
	second := importSource("second-mapped")
	mapped := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: second})
	if mapped.Capture == nil || mapped.Capture.Sources[0].Basis != "mapped" || mapped.Capture.Sources[0].ReceivedAt == nil || *mapped.Capture.Sources[0].ReceivedAt != target {
		t.Fatalf("mapped context: %+v", mapped)
	}
	// A target configuration at the same identity but a later revision must
	// not silently replace the exact association this mapping established.
	changed := saveEnvironment(t, app, context, desktop.SaveItemRequest{Item: target.ID, BaseRevision: target.Revision, IntentID: "changed-receiver", Draft: desktop.ItemDraft{Name: "QA receiver changed", Environment: environmentDraft("127.0.0.1:65532", "nonproduction", "plain")}})
	_ = changed
	unavailable := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: second})
	if unavailable.Capture == nil || unavailable.Capture.Sources[0].ReceivedAt != nil || unavailable.Capture.Sources[0].Basis != "unknown" {
		t.Fatalf("changed mapping rebound its target: %+v", unavailable)
	}
	other := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "other-receiver", Draft: desktop.ItemDraft{Name: "Other receiver", Environment: environmentDraft("127.0.0.1:65531", "nonproduction", "plain")}})
	corrected := mapped.Capture.Sources
	corrected[0].Basis = "manual"
	corrected[0].ReceivedAt = &other
	corrected[0].Remember = true
	if result := app.SaveCaptureContext(desktop.CaptureContextSaveRequest{Context: context, Source: second, Identity: mapped.Capture.Identity, IntentID: "remember-other", Sources: corrected}); result.State != desktop.Completed {
		t.Fatalf("other mapping: %+v", result)
	}
	third := importSource("ambiguous-mapped")
	ambiguous := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: third})
	if ambiguous.Capture == nil || ambiguous.Capture.Sources[0].Basis != "unknown" || ambiguous.Capture.Sources[0].ReceivedAt != nil {
		t.Fatalf("ambiguous provenance: %+v", ambiguous)
	}
}

func TestFinishedReceiverCaptureNamesObservedSourceWithoutInventingSendTarget(t *testing.T) {
	app, context := namedProject(t)
	receiver := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, receiver, "observed-receiver")
	deliver(t, progress.BoundAddress)
	app.FinishCapture()
	finished := awaitCapture(t, done)
	if finished.CaseRef == nil {
		t.Fatalf("finished capture: %+v", finished)
	}
	metadata := app.ReadCaptureContext(desktop.ItemRequest{Context: context, Ref: *finished.CaseRef})
	if metadata.Capture == nil || len(metadata.Capture.Sources) != 1 || metadata.Capture.Sources[0].Basis != "observed" || metadata.Capture.Sources[0].ReceivedAt == nil || metadata.Capture.Sources[0].ReceivedAt.Kind != desktop.SourceItem || *metadata.Capture.Sources[0].ReceivedAt != receiver {
		t.Fatalf("observed receiver basis: %+v", metadata.Capture)
	}
}
