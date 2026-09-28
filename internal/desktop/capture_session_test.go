package desktop_test

// Capture from a saved source (#552): a listener and its responder are one
// saved revision; Stop finishes the capture — journal finalized — and
// publishes one registered case; Cancel keeps what arrived and registers
// nothing; a failed finalization is retried without serving again; and the
// history is read, never resumed.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/project"
)

// listenerSource saves a loopback MLLP listener source and returns its
// reference.
func listenerSource(t *testing.T, app *desktop.App, context desktop.RequestContext) desktop.ItemRef {
	t.Helper()
	listener := desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 0, Transport: desktop.PlainTransport, ConnectionLimit: 1, IdleTimeout: "5s", AckCode: "AA"}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "save-listener",
		Draft: desktop.ItemDraft{Name: "Scheduling QA", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener}}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save the listener: %+v", saved)
	}
	return *saved.Saved
}

// startedCapture starts a capture from source and waits until it listens.
func startedCapture(t *testing.T, app *desktop.App, context desktop.RequestContext, source desktop.ItemRef, intent string) (<-chan desktop.CaptureSessionResult, desktop.CaptureProgress) {
	t.Helper()
	done := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		done <- app.StartCapture(desktop.CaptureRequest{Context: context, Source: &source, Name: "Morning capture", IntentID: intent})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if progress := app.CaptureProgress(); progress.Progress != nil && progress.Progress.BoundAddress != "" {
			return done, *progress.Progress
		}
		select {
		case result := <-done:
			t.Fatalf("the capture ended before it listened: %+v", result)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("the capture never listened")
	return nil, desktop.CaptureProgress{}
}

// deliver sends one message to address and reads its acknowledgement.
func deliver(t *testing.T, address string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write(mllp.Frame([]byte(sampleImportHL7))); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(connection).ReadBytes(0x1c); err != nil {
		t.Fatalf("no acknowledgement: %v", err)
	}
}

func awaitCapture(t *testing.T, done <-chan desktop.CaptureSessionResult) desktop.CaptureSessionResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("the capture did not end")
	}
	return desktop.CaptureSessionResult{}
}

func sessionFolder(root, session string) string {
	return filepath.Join(root, ".readmit", "captures", session)
}

func TestASourceAndResponderPublishAsOneRevision(t *testing.T) {
	app, context := namedProject(t)
	fresh := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.SourceItem}})
	if fresh.State != desktop.Completed || !fresh.New || fresh.Draft.Source == nil || fresh.Draft.Source.Listener == nil || fresh.Draft.Source.Listener.BindAddress != "127.0.0.1" {
		t.Fatalf("a new source: %+v", fresh)
	}
	listener := desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575, Transport: desktop.PlainTransport, IdleTimeout: "30s", AckCode: "AE"}
	responder, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	responder.Acknowledgement.Code = "AE"
	draft := desktop.ItemDraft{Name: "Scheduling QA", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener, Responder: &responder}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Draft: draft, IntentID: "source-1"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved.Revision != "1" {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.Draft == nil || opened.Draft.Source.Listener.Port != 2575 || opened.Draft.Source.Responder == nil || opened.Draft.Source.Responder.Name != "downstream-sink" {
		t.Fatalf("the saved listener and responder: %+v", opened)
	}
	// Changing the responder alone is a new revision of the one source.
	responder.SourceLabel = "scheduling-qa"
	again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Item: saved.Saved.ID, BaseRevision: "1", Draft: draft, IntentID: "source-2"})
	if again.Outcome != desktop.SavedOutcome || again.Saved.Revision != "2" || again.Saved.ID != saved.Saved.ID {
		t.Fatalf("a responder change: %+v", again)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.SourceItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 || listed.Page.Items[0].Summary.Source == nil || listed.Page.Items[0].Summary.Source.Address != "127.0.0.1:2575" {
		t.Fatalf("listed: %+v", listed)
	}

	for _, refused := range []struct {
		field  string
		source desktop.CaptureSourceDraft
	}{
		{"source.listener.allow_remote", desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource,
			Listener: &desktop.ListenerSettings{BindAddress: "0.0.0.0", Port: 2575, Transport: desktop.PlainTransport, AckCode: "AA"}}},
		{"source.responder.acknowledgement", desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource,
			Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575, Transport: desktop.PlainTransport, AckCode: "AA"}, Responder: &responder}},
		{"source.listener.tls_certificate", desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource,
			Listener: &desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 2575, Transport: desktop.TLSTransport, AckCode: "AA"}}},
		{"source.type", desktop.CaptureSourceDraft{Type: desktop.APISource}},
	} {
		source := refused.source
		result := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Name: "Refused", Source: &source}, IntentID: "refused-" + refused.field})
		if result.Outcome != desktop.InvalidOutcome || len(result.Problems) == 0 || result.Problems[0].Field != refused.field {
			t.Errorf("%s: %+v", refused.field, result)
		}
	}
	// Allowing remote connections beside the address permits the bind.
	remote := desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource,
		Listener: &desktop.ListenerSettings{BindAddress: "0.0.0.0", Port: 2575, Transport: desktop.PlainTransport, AckCode: "AA", AllowRemote: true}}
	if result := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.SourceItem, Draft: desktop.ItemDraft{Source: &remote}}); len(result.Problems) != 0 {
		t.Fatalf("an allowed remote bind: %+v", result)
	}
}

func TestFinishingACaptureFinalizesAndRegistersOneCase(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "capture-1")
	if progress.Session == "" || progress.Name != "Morning capture" || progress.SourceName != "Scheduling QA" || progress.StartedAt == nil || progress.Received != 0 {
		t.Fatalf("progress: %+v", progress)
	}
	deliver(t, progress.BoundAddress)
	if now := app.CaptureProgress(); now.Progress == nil || now.Progress.Received != 1 || len(now.Progress.Messages) != 1 || now.Progress.Messages[0].Type != "ADT^A01" || now.Progress.Messages[0].Connection != 1 {
		t.Fatalf("progress after a message: %+v", now)
	}
	if finish := app.FinishCapture(); finish.State != desktop.Completed || !finish.Progress.Finishing {
		t.Fatalf("finish: %+v", finish)
	}
	result := awaitCapture(t, done)
	if result.State != desktop.Completed || result.Outcome != desktop.CaptureFinished || result.CaseRef == nil || result.Session != progress.Session {
		t.Fatalf("finished: %+v", result)
	}
	journal, err := capturejournal.Open(filepath.Join(sessionFolder(root, result.Session), "journal"))
	if err != nil || journal.State != capturejournal.Finalized || journal.Received != 1 {
		t.Fatalf("the journal of a finished capture: %+v %v", journal, err)
	}
	cases := registeredCases(t, root)
	if len(cases) != 1 || cases[0].Title != "Morning capture" {
		t.Fatalf("registered: %+v", cases)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 || listed.Page.Items[0].Ref.ID != result.CaseRef.ID {
		t.Fatalf("listed: %+v", listed)
	}
	// The same click again answers the capture it made; nothing listens.
	again := app.StartCapture(desktop.CaptureRequest{Context: context, Source: &source, Name: "Morning capture", IntentID: "capture-1"})
	if !again.Replayed || again.CaseRef == nil || again.CaseRef.ID != result.CaseRef.ID || len(registeredCases(t, root)) != 1 {
		t.Fatalf("a repeated start: %+v", again)
	}
	if finish := app.FinishCapture(); finish.State != desktop.Empty {
		t.Fatalf("finish with nothing running: %+v", finish)
	}
}

func TestCancellingACaptureRegistersNothing(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	source := listenerSource(t, app, context)
	before := entries(t, root)
	done, progress := startedCapture(t, app, context, source, "capture-cancel")
	deliver(t, progress.BoundAddress)
	app.Cancel("capture")
	result := awaitCapture(t, done)
	if result.State != desktop.Cancelled || result.Outcome != desktop.CaptureCancelled || result.CaseRef != nil {
		t.Fatalf("cancelled: %+v", result)
	}
	if len(registeredCases(t, root)) != 0 {
		t.Fatal("a cancelled capture registered a case")
	}
	if now := entries(t, root); len(now) != len(before) {
		t.Fatalf("a cancelled capture added project entries: %v, was %v", now, before)
	}
	// What arrived is kept under the session, never published.
	if _, err := os.Stat(filepath.Join(sessionFolder(root, result.Session), "case", "manifest.json")); err != nil {
		t.Fatalf("the cancelled capture's data: %v", err)
	}
	if retried := app.RetryCaptureFinalization(desktop.CaptureSessionRequest{Context: context, Session: result.Session}); retried.State != desktop.Failed || retried.Case != nil {
		t.Fatalf("a cancelled capture was finalized: %+v", retried)
	}
}

func TestRetryingFinalizationNeverServesAgain(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "capture-retry")
	deliver(t, progress.BoundAddress)
	// The project's documents cannot be registered into while it finishes.
	writeDocument(t, root, project.RevisionsDocumentName, "{")
	app.FinishCapture()
	result := awaitCapture(t, done)
	if result.State != desktop.Failed || result.Outcome != desktop.CaptureFinalizeFailed || result.Session == "" || result.CaseRef != nil {
		t.Fatalf("a failed finalization: %+v", result)
	}
	if len(registeredCases(t, root)) != 0 {
		t.Fatal("a failed finalization registered a case")
	}
	if err := os.Remove(filepath.Join(root, project.RevisionsDocumentName)); err != nil {
		t.Fatal(err)
	}
	journalBefore := digestTree(t, filepath.Join(sessionFolder(root, result.Session), "journal"))
	retried := app.RetryCaptureFinalization(desktop.CaptureSessionRequest{Context: context, Session: result.Session})
	if retried.State != desktop.Completed || retried.Case == nil {
		t.Fatalf("retry: %+v", retried)
	}
	// Nothing listened and nothing was received again.
	if connection, err := net.DialTimeout("tcp", progress.BoundAddress, 200*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("retrying finalization listened again")
	}
	if progress := app.CaptureProgress(); progress.State != desktop.Empty {
		t.Fatalf("retrying finalization started a capture: %+v", progress)
	}
	if after := digestTree(t, filepath.Join(sessionFolder(root, result.Session), "journal")); after != journalBefore {
		t.Fatal("retrying finalization changed the capture's journal")
	}
	if cases := registeredCases(t, root); len(cases) != 1 || cases[0].Title != "Morning capture" {
		t.Fatalf("registered: %+v", cases)
	}
	// A finished session is not finalized twice.
	if twice := app.RetryCaptureFinalization(desktop.CaptureSessionRequest{Context: context, Session: result.Session}); twice.State != desktop.Failed {
		t.Fatalf("a second retry: %+v", twice)
	}
}

func TestCaptureSessionsListReadOnly(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	source := listenerSource(t, app, context)
	done, progress := startedCapture(t, app, context, source, "history-finished")
	deliver(t, progress.BoundAddress)
	app.FinishCapture()
	finished := awaitCapture(t, done)
	done, _ = startedCapture(t, app, context, source, "history-cancelled")
	app.Cancel("capture")
	cancelled := awaitCapture(t, done)

	// A session whose process ended while it ran reads as interrupted.
	orphan := sessionFolder(root, "0123456789abcdef01234567")
	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, orphan, "session.json", `{"schema":"readmit-capture-session/v1","id":"0123456789abcdef01234567","intent":"x","name":"Overnight",`+
		`"source":{"kind":"source","id":"`+source.ID+`","revision":"1"},"source_name":"Scheduling QA","source_type":"mllp-listener",`+
		`"started_at":"2026-01-01T00:00:00Z","state":"running","received":0}`)

	before := digestTree(t, filepath.Join(root, ".readmit", "captures"))
	listed := app.ListCaptureSessions(context)
	if listed.State != desktop.Completed || len(listed.Sessions) != 3 {
		t.Fatalf("history: %+v", listed)
	}
	states := map[string]desktop.CaptureSessionRow{}
	for _, row := range listed.Sessions {
		states[row.ID] = row
	}
	if row := states[finished.Session]; row.State != desktop.CaptureFinished || row.Case == nil || row.Received != 1 || row.Name != "Morning capture" {
		t.Fatalf("the finished session: %+v", row)
	}
	if row := states[cancelled.Session]; row.State != desktop.CaptureCancelled || row.Case != nil {
		t.Fatalf("the cancelled session: %+v", row)
	}
	if row := states["0123456789abcdef01234567"]; row.State != desktop.CaptureInterrupted || row.Reason == "" {
		t.Fatalf("the interrupted session: %+v", row)
	}
	if after := digestTree(t, filepath.Join(root, ".readmit", "captures")); after != before {
		t.Fatal("listing the history wrote")
	}
	if progress := app.CaptureProgress(); progress.State != desktop.Empty {
		t.Fatalf("listing the history resumed a capture: %+v", progress)
	}
}

// digestTree is a digest of every file below folder, by name and bytes.
func digestTree(t *testing.T, folder string) string {
	t.Helper()
	digest := sha256.New()
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		digest.Write([]byte(path + "\x00"))
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// A local folder source is collected and imported inside the one capture, and
// the case it finishes into is registered with its name; there is no separate
// finalize step.
func TestAFolderSourceCaptureCollectsAndRegistersInOneOperation(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	export := t.TempDir()
	writeDocument(t, export, "one.hl7", sampleImportHL7)
	plan := validDesktopPlan()
	evidence := evidencesource.Source{Name: "exports", Kind: evidencesource.Directory, Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20}, Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, IntentID: "save-folder",
		Draft: desktop.ItemDraft{Name: "Exports", Source: &desktop.CaptureSourceDraft{Type: desktop.LocalFolderSource, Evidence: &evidence, Plan: &plan}}})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	result := app.StartCapture(desktop.CaptureRequest{Context: context, Source: saved.Saved, Name: "Exported appointments", IntentID: "folder-1"})
	if result.State != desktop.Completed || result.Outcome != desktop.CaptureFinished || result.CaseRef == nil || result.Received != 1 {
		t.Fatalf("a folder capture: %+v", result)
	}
	if cases := registeredCases(t, root); len(cases) != 1 || cases[0].Title != "Exported appointments" {
		t.Fatalf("registered: %+v", cases)
	}
	// A folder capture finishes on its own; there is nothing to ask.
	if finish := app.FinishCapture(); finish.State != desktop.Empty {
		t.Fatalf("finish: %+v", finish)
	}
}
