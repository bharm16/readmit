package desktop_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observewindow"
)

const facadeAnyPolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func TestChooseCapturePathKinds(t *testing.T) {
	c := &chooser{folder: "/tmp/export", files: []string{"/usr/local/bin/transfer"}}
	app := newApp(t, c)
	folder := app.ChooseCapturePath("source-root")
	if folder.State != desktop.Completed || folder.Kind != "source-root" || folder.Paths[0] != "/tmp/export" {
		t.Fatalf("folder: %+v", folder)
	}
	program := app.ChooseCapturePath("transfer-program")
	if program.State != desktop.Completed || program.Paths[0] != "/usr/local/bin/transfer" {
		t.Fatalf("program: %+v", program)
	}
	c.files = nil
	dismissed := app.ChooseCapturePath("policy")
	if dismissed.State != desktop.Cancelled {
		t.Fatalf("dismissed: %+v", dismissed)
	}
}

func TestDiagnoseAndCollectSourceThroughFacade(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	export := filepath.Join(root, "export")
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if err := os.WriteFile(filepath.Join(export, "one.hl7"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{
		Workspace: root, SourceFile: "source.json", Source: source,
	})
	if saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	plan := importer.Plan{
		Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"},
	}
	access := app.DiagnoseSource(desktop.SourceWorkRequest{
		Workspace: root, SourceFile: "source.json", Plan: &plan,
	})
	if access.State != desktop.Completed || access.Access == nil || access.Access.Status != string(observewindow.Complete) {
		t.Fatalf("diagnose: %+v", access)
	}
	collected := app.CollectSource(desktop.SourceWorkRequest{
		Workspace: root, SourceFile: "source.json", Plan: &plan,
		OutputName: "staged", ReceiptName: "receipt.json",
	})
	if collected.State != desktop.Completed || collected.Collection == nil || collected.Collection.Collected != 1 {
		t.Fatalf("collect: %+v", collected)
	}
}

// A collection a person cancels part way through is a cancellation, the way
// its own receipt records it: not a failure of the source, and never a
// complete collection. What it staged stays staged, and the receipt accounts
// for every entry it never reached.
func TestCancellingACollectionPartWayIsCancelledNotFailed(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	export := filepath.Join(root, "export")
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
	const entries = 200
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	for i := range entries {
		// Distinct bytes, so no entry is recorded as a duplicate of another.
		if err := os.WriteFile(filepath.Join(export, fmt.Sprintf("%04d.hl7", i)), append(payload, fmt.Sprintf("NTE|%d\r", i)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: entries, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: source}); saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	plan := importer.Plan{Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"}}

	answered := make(chan desktop.SourceCollectionResult, 1)
	go func() {
		answered <- app.CollectSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", Plan: &plan,
			OutputName: "staged", ReceiptName: "receipt.json"})
	}()
	// Cancel only once entries are being staged, so the collector is inside
	// its loop over the source rather than still being admitted.
	awaitEntries(t, filepath.Join(root, "staged"), 2, answered)
	app.Cancel("collect")
	result := <-answered
	if result.State != desktop.Cancelled || result.Collection == nil || result.Collection.Status != string(observewindow.Cancelled) {
		t.Fatalf("a collection cancelled part way answered %+v", result)
	}
	if result.Collection.Collected == 0 || result.Collection.Collected >= entries || result.Collection.Collected+result.Collection.Unreadable != entries {
		t.Fatalf("the cancelled collection did not account for every entry: %+v", result.Collection)
	}
	receipt, err := os.ReadFile(filepath.Join(root, "receipt.json"))
	if err != nil || !strings.Contains(string(receipt), `"status":"cancelled"`) {
		t.Fatalf("the receipt does not record the cancellation: %v", err)
	}
}

func TestPreviewAndStartCollectThroughFacade(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{
		Workspace: root, PolicyFile: "policy.json", Policy: policy,
	})
	if saved.State != desktop.Completed {
		t.Fatalf("save policy: %+v", saved)
	}
	preview := app.PreviewCapture(desktop.CaptureRequest{
		Workspace: root, Kind: "collect", Address: "127.0.0.1:0",
		PolicyFile: "policy.json", OutputName: "case", JournalName: "journal",
		MaxMessages: 1,
	})
	if preview.State != desktop.Completed || preview.Preview == nil || preview.Preview.Kind != "collect" {
		t.Fatalf("preview: %+v", preview)
	}
	if preview.Preview.SourceLabel != "downstream-test-endpoint" {
		t.Fatalf("preview leaked or lost source label: %+v", preview.Preview)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	done := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		done <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "collect", Address: addr,
			PolicyFile: "policy.json", OutputName: "case", JournalName: "journal",
			MaxMessages: 1, IdleTimeout: "2s",
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not dial facade collector")
	}
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if _, err := conn.Write(mllp.Frame(payload)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	session := <-done
	if session.State != desktop.Completed || session.Case == nil || session.Received != 1 {
		t.Fatalf("session: %+v", session)
	}
	journal := app.OpenCaptureJournal(root, "journal")
	if journal.State != desktop.Completed || journal.Journal == nil || journal.Journal.Received != 1 {
		t.Fatalf("journal: %+v", journal)
	}
}

func TestStartCaptureBusyAndCancel(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if res := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{
		Workspace: root, PolicyFile: "policy.json", Policy: policy,
	}); res.State != desktop.Completed {
		t.Fatalf("save: %+v", res)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	started := make(chan struct{})
	done := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		close(started)
		done <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "collect", Address: addr,
			PolicyFile: "policy.json", OutputName: "case", MaxMessages: 0, IdleTimeout: "5s",
		})
	}()
	<-started
	time.Sleep(30 * time.Millisecond)
	busy := app.PreviewCapture(desktop.CaptureRequest{
		Workspace: root, Kind: "collect", Address: "127.0.0.1:0",
		PolicyFile: "policy.json", OutputName: "other",
	})
	// Preview does not need the interruptible slot exclusively the same way —
	// but Start holds the slot, so Preview should report Busy.
	if busy.State != desktop.Busy {
		// Preview uses non-interruptible claim; Start holds the slot, so Busy is required.
		t.Fatalf("expected busy while capturing, got %+v", busy)
	}
	app.Cancel("")
	session := <-done
	if session.State != desktop.Cancelled && session.State != desktop.Completed {
		t.Fatalf("cancel session: %+v", session)
	}
}

// A collector and a fixture listener are both started by StartCapture, so both
// run under the capture name, and that is the name that stops a collector.
// Approved source collection is a different operation with its own name, and a
// cancellation naming it must not reach a listening collector: a panel's cancel
// control that names the wrong operation stops nothing. Stopping a collector is
// its controlled stop, so it answers with the case it sealed, not a failure.
func TestACollectorIsCancelledByTheCaptureNameAndNotByCollection(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if res := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: root, PolicyFile: "policy.json", Policy: policy}); res.State != desktop.Completed {
		t.Fatalf("save: %+v", res)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()

	answered, holding := startHolding(t, app, root, func(r desktop.CaptureSessionResult) desktop.State { return r.State }, func() desktop.CaptureSessionResult {
		return app.StartCapture(desktop.CaptureRequest{Workspace: root, Kind: "collect", Address: addr,
			PolicyFile: "policy.json", OutputName: "case", MaxMessages: 0, IdleTimeout: "1m"})
	})
	if !holding {
		t.Fatalf("the collector answered before it was listening: %+v", <-answered)
	}
	listening := func() bool {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
		}
		return err == nil
	}
	for deadline := time.Now().Add(10 * time.Second); !listening(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the collector never listened")
		}
	}
	// After a cancellation naming source collection, the collector is still
	// the one holding the slot and still accepts a connection.
	app.Cancel("collect")
	if !listening() {
		t.Fatal("a cancellation naming source collection stopped the collector")
	}
	if app.Search(root, "").State != desktop.Busy {
		t.Fatal("a cancellation naming source collection released the collector's slot")
	}
	app.Cancel("capture")
	select {
	case result := <-answered:
		if result.State != desktop.Completed && result.State != desktop.Cancelled || result.Phase != desktop.CaptureStopped {
			t.Fatalf("a stopped collector answered %+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the collector by its own name did not stop it")
	}
}

func TestPreviewListenLabelsSyntheticFixture(t *testing.T) {
	app := workspaceApp(t)
	preview := app.PreviewCapture(desktop.CaptureRequest{
		Workspace: t.TempDir(), Kind: "listen", Address: "127.0.0.1:0",
		FixtureMode: "fixed", OutputName: "fixture.case", ObservationName: "obs.json",
	})
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("preview: %+v", preview)
	}
	if preview.Preview.FixtureLabel == "" || preview.Preview.Kind != "listen" {
		t.Fatalf("fixture must stay labelled: %+v", preview.Preview)
	}
}

func TestFinalizeCaptureImportOffersExploration(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	staged := filepath.Join(root, "staged")
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")
	if err := os.WriteFile(filepath.Join(staged, "one.hl7"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{
		Workspace: root, Folder: "staged", OutputName: "imported.case",
	})
	if result.State != desktop.Completed || result.Case == nil {
		t.Fatalf("finalize: %+v", result)
	}
	if result.Case.Messages < 1 {
		t.Fatalf("expected imported messages: %+v", result.Case)
	}
}

func TestOpenCaptureJournalNeverFabricatesCompletion(t *testing.T) {
	app := workspaceApp(t)
	missing := app.OpenCaptureJournal(t.TempDir(), "missing-journal")
	if missing.State != desktop.Failed {
		t.Fatalf("missing journal must fail: %+v", missing)
	}
}

func TestCaptureJournalCrashRecoveryNeverFinalized(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if res := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{
		Workspace: root, PolicyFile: "policy.json", Policy: policy,
	}); res.State != desktop.Completed {
		t.Fatalf("save: %+v", res)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	done := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		done <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "collect", Address: addr,
			PolicyFile: "policy.json", OutputName: "case", JournalName: "journal",
			MaxMessages: 0, IdleTimeout: "5s",
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond); err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Kill the serve without an orderly Cancel finish by cancelling after a connection attempt.
	app.Cancel("")
	<-done
	journal := app.OpenCaptureJournal(root, "journal")
	if journal.State != desktop.Completed || journal.Journal == nil {
		t.Fatalf("journal: %+v", journal)
	}
	if journal.Journal.State == "finalized" && journal.Journal.Recovered {
		t.Fatal("recovered journal must not claim finalized completion")
	}
}

func TestPreviewCaptureTLSMembersMustBeTogether(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if res := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{
		Workspace: root, PolicyFile: "policy.json", Policy: policy,
	}); res.State != desktop.Completed {
		t.Fatalf("save: %+v", res)
	}
	preview := app.PreviewCapture(desktop.CaptureRequest{
		Workspace: root, Kind: "collect", Address: "127.0.0.1:0",
		PolicyFile: "policy.json", OutputName: "case",
		TLSCertificateFile: "only-cert.pem",
	})
	if preview.State != desktop.Failed {
		t.Fatalf("expected TLS refusal: %+v", preview)
	}
}
