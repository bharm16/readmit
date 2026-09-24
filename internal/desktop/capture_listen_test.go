package desktop_test

import (
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// awaitCaptureListening waits until the running capture reports the address
// it bound, and fails the test if StartCapture answers first.
func awaitCaptureListening(t *testing.T, app *desktop.App, answered chan desktop.CaptureSessionResult) desktop.CaptureProgress {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case result := <-answered:
			t.Fatalf("the capture answered before it listened: %+v", result)
		default:
		}
		if progress := app.CaptureProgress(); progress.State == desktop.Completed && progress.Progress != nil {
			return *progress.Progress
		}
	}
	t.Fatal("the capture never reported where it listens")
	return desktop.CaptureProgress{}
}

// sendFixtureMessage sends one of the frozen SIU fixtures on conn in its MLLP
// frame and returns the acknowledgement the fixture answered with.
func sendFixtureMessage(t *testing.T, conn net.Conn, frames *mllp.Reader, fixture string) string {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(mllp.Frame(payload)); err != nil {
		t.Fatal(err)
	}
	ack, err := frames.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return string(ack)
}

func dialFixture(t *testing.T, address string) (net.Conn, *mllp.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	frames, err := mllp.NewReader(conn, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return conn, frames
}

// A port of 0 is only known once the fixture has bound it, so the window reads
// the bound address while the listen runs, from the readiness point at which
// `readmit listen` prints its Listening line: the initial observation is
// already installed. The address is the declared loopback host and never a
// wider one, and nothing is reported once the listen has answered.
func TestCaptureProgressNamesTheAddressAFixtureBoundOnlyWhileItListens(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	if idle := app.CaptureProgress(); idle.State != desktop.Empty || idle.Progress != nil {
		t.Fatalf("a fresh window reports a capture listening: %+v", idle)
	}
	answered := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		answered <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "listen", Address: "127.0.0.1:0", FixtureMode: "fixed",
			OutputName: "fixture.case", ObservationName: "ledger.json", MaxMessages: 2, IdleTimeout: "10s",
		})
	}()
	listening := awaitCaptureListening(t, app, answered)
	host, port, err := net.SplitHostPort(listening.BoundAddress)
	if err != nil || listening.Kind != "listen" || host != "127.0.0.1" || port == "0" {
		t.Fatalf("the fixture reports %+v, want the declared loopback host on the port it bound", listening)
	}
	initial, err := observation.Read(filepath.Join(root, "ledger.json"))
	if err != nil || !initial.EmptyInitialState() {
		t.Fatalf("the fixture was reported listening before its initial ledger was installed: %+v %v", initial, err)
	}

	conn, frames := dialFixture(t, listening.BoundAddress)
	for _, message := range []struct{ fixture, control string }{{"listen-s12.hl7", "LISTEN-BOOK"}, {"listen-s13.hl7", "LISTEN-MOVE"}} {
		if ack := sendFixtureMessage(t, conn, frames, message.fixture); !strings.Contains(ack, "\rMSA|AA|"+message.control+"\r") {
			t.Fatalf("%s was answered %q", message.fixture, ack)
		}
	}
	result := <-answered
	if result.State != desktop.Completed || result.Phase != desktop.CaptureStopped || result.BoundAddress != listening.BoundAddress {
		t.Fatalf("a completed listen answered %+v", result)
	}
	if result.Case == nil || result.Case.Schema != "readmit-case/v2" || result.Case.Messages != 2 || result.Case.Acknowledgements != 2 || result.Received != 2 {
		t.Fatalf("the sealed case: %+v %+v", result.Case, result)
	}
	want := desktop.FixtureLedger{Schema: observation.Schema, Profile: observation.Profile, Mode: "fixed", Processed: 2, Records: 1, Consistent: true}
	if result.Ledger == nil || *result.Ledger != want {
		t.Fatalf("the exported ledger reads %+v, want %+v", result.Ledger, want)
	}
	if after := app.CaptureProgress(); after.State != desktop.Empty {
		t.Fatalf("a listen that answered still reports it listens: %+v", after)
	}
}

// Cancelling a fixture mid-run is its orderly exit: the case is sealed with the
// bytes it received and the ledger it committed, and the window says it was
// cancelled rather than failed. A cancellation naming another operation never
// reaches it.
func TestCancellingAFixtureListenMidRunSealsWhatItReceived(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	answered := make(chan desktop.CaptureSessionResult, 1)
	go func() {
		answered <- app.StartCapture(desktop.CaptureRequest{
			Workspace: root, Kind: "listen", Address: "127.0.0.1:0", FixtureMode: "defective",
			OutputName: "fixture.case", ObservationName: "ledger.json", IdleTimeout: "1m",
		})
	}()
	listening := awaitCaptureListening(t, app, answered)
	conn, frames := dialFixture(t, listening.BoundAddress)
	if ack := sendFixtureMessage(t, conn, frames, "listen-s12.hl7"); !strings.Contains(ack, "\rMSA|AA|LISTEN-BOOK\r") {
		t.Fatalf("the booking was answered %q", ack)
	}
	app.Cancel("collect")
	select {
	case result := <-answered:
		t.Fatalf("a cancellation naming source collection stopped the fixture: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}
	app.Cancel("capture")
	var result desktop.CaptureSessionResult
	select {
	case result = <-answered:
	case <-time.After(15 * time.Second):
		t.Fatal("cancelling the fixture by its own name did not stop it")
	}
	if result.State != desktop.Cancelled || result.Phase != desktop.CaptureStopped || result.Reason == "" {
		t.Fatalf("a cancelled listen answered %+v", result)
	}
	if result.Case == nil || result.Case.Messages != 1 || result.Case.Acknowledgements != 1 {
		t.Fatalf("the cancelled listen sealed %+v", result.Case)
	}
	want := desktop.FixtureLedger{Schema: observation.Schema, Profile: observation.Profile, Mode: "defective", Processed: 1, Records: 1, Consistent: true}
	if result.Ledger == nil || *result.Ledger != want {
		t.Fatalf("the cancelled listen's ledger reads %+v, want %+v", result.Ledger, want)
	}
	retained, err := observation.Read(filepath.Join(root, "ledger.json"))
	if err != nil || len(retained.Records) != 1 || len(retained.Processed) != 1 || !retained.Consistent {
		t.Fatalf("the exported ledger after cancellation: %+v %v", retained, err)
	}
	if after := app.CaptureProgress(); after.State != desktop.Empty {
		t.Fatalf("a cancelled listen still reports it listens: %+v", after)
	}
}

// The window binds exactly what was declared. A nonloopback address without
// the explicit approval, and a name, are refused in the command line's words
// before anything binds; an address something else holds is refused at the
// bind. None of them ever reports listening or writes a case or a ledger.
func TestAFixtureListenNeverBindsWiderThanDeclared(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	for _, refused := range []struct{ address, reason string }{
		{"0.0.0.0:0", sendpolicy.ErrNonloopbackBind.Error()},
		{"[::]:0", sendpolicy.ErrNonloopbackBind.Error()},
		{"localhost:0", sendpolicy.ErrNonloopbackBind.Error()},
		{":0", sendpolicy.ErrNonloopbackBind.Error()},
		{"127.0.0.1", sendpolicy.ErrBindAddress.Error()},
		{held.Addr().String(), "cannot bind receiver address"},
	} {
		request := desktop.CaptureRequest{
			Workspace: root, Kind: "listen", Address: refused.address, FixtureMode: "fixed",
			OutputName: "fixture.case", ObservationName: "ledger.json", MaxMessages: 1, IdleTimeout: "1s",
		}
		if refused.reason != "cannot bind receiver address" {
			if preview := app.PreviewCapture(request); preview.State != desktop.Failed || preview.Reason != refused.reason || preview.Preview != nil {
				t.Errorf("preview of %q: %+v", refused.address, preview)
			}
		}
		result := app.StartCapture(request)
		if result.State != desktop.Failed || result.Reason != refused.reason || result.BoundAddress != "" || result.Case != nil {
			t.Errorf("start at %q: %+v, want refused with %q", refused.address, result, refused.reason)
		}
		if progress := app.CaptureProgress(); progress.State != desktop.Empty {
			t.Errorf("a refused bind at %q reports listening: %+v", refused.address, progress)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatalf("a refused bind at %q wrote %v", refused.address, entries)
		}
	}
}

// A responder policy reopens as the document the command line reads: every
// version, every declared member, and a copy the window saves from it reads
// back the same. What the command line's reader refuses, the window refuses in
// the same words, and a name outside the workspace is never read.
func TestReopeningAResponderPolicyReadsWhatTheCommandLineReads(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	for name, document := range map[string]string{
		"v1.json": facadeAnyPolicy,
		"v2.json": `{
  "schema": "readmit-receiver-policy/v2",
  "name": "enhanced-sink",
  "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AE"},
  "accepted_message_types": {"operator": "message-type-in", "values": ["SIU^S12", "SIU^S13"]},
  "enhanced_acknowledgement": {"operator": "enhanced-mode-fixed-codes", "accept_code": "CA", "application_code": "AA", "application_delivery": "same-connection", "application_endpoint": "", "approved_transport": false}
}`,
		"v3.json": `{
  "schema": "readmit-receiver-policy/v3",
  "name": "faulting-sink",
  "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
  "accepted_message_types": {"operator": "any-message-type", "values": []},
  "enhanced_acknowledgement": {"operator": "unsupported", "accept_code": "", "application_code": "", "application_delivery": "", "application_endpoint": "", "approved_transport": false},
  "faults": {"environment_class": "nonproduction", "approved_test_endpoints": ["127.0.0.1:2575"],
    "steps": [{"message": 1, "stage": "application", "action": "delay", "delay_ms": 25}, {"message": 3, "stage": "application", "action": "reject", "delay_ms": 0}]}
}`,
	} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		declared, err := operation.ReceiverPolicyRead(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		opened := app.ReadReceiverPolicy(root, name)
		if opened.State != desktop.Completed || opened.PolicyFile != name || opened.Policy == nil || !reflect.DeepEqual(*opened.Policy, declared) {
			t.Fatalf("%s reopened as %+v, want %+v", name, opened, declared)
		}
		copied := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: root, PolicyFile: "copy-" + name, Policy: *opened.Policy})
		reopened := app.ReadReceiverPolicy(root, "copy-"+name)
		if copied.State != desktop.Completed || reopened.State != desktop.Completed || !reflect.DeepEqual(*reopened.Policy, declared) {
			t.Fatalf("%s saved and reopened as %+v %+v", name, copied, reopened)
		}
	}
	for name, document := range map[string]string{
		"unknown-member.json": strings.Replace(facadeAnyPolicy, `"name"`, `"extra":true,"name"`, 1),
		"v1-enhanced.json":    strings.Replace(facadeAnyPolicy, `"name"`, `"enhanced_acknowledgement":null,"name"`, 1),
		"later.json":          strings.Replace(facadeAnyPolicy, "/v1", "/v9", 1),
		"oversized.json":      facadeAnyPolicy + strings.Repeat(" ", collection.MaxPolicyBytes),
	} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := operation.ReceiverPolicyRead(path)
		if err == nil {
			t.Fatalf("%s was not refused by the command line's reader", name)
		}
		if opened := app.ReadReceiverPolicy(root, name); opened.State != desktop.Failed || opened.Reason != err.Error() || opened.Policy != nil {
			t.Errorf("%s reopened as %+v, want refused with %q", name, opened, err.Error())
		}
	}
	for _, missing := range []struct{ name, reason string }{
		{"absent.json", "input must be a readable regular file"},
		{".", "input must be a readable regular file"},
		{"../outside.json", "file path must remain within the workspace"},
		{"", "file path must not be empty"},
	} {
		if opened := app.ReadReceiverPolicy(root, missing.name); opened.State != desktop.Failed || opened.Reason != missing.reason {
			t.Errorf("reopening %q: %+v, want %q", missing.name, opened, missing.reason)
		}
	}
}

// A source registration reopens as the document the command line reads, with
// every member of its kind, including the ones the window's form does not
// show; a copy the window saves from it reads back the same. What the reader
// refuses, the window refuses in the same words.
func TestReopeningASourceRegistrationReadsWhatTheCommandLineReads(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	for name, document := range map[string]string{
		"directory.json": `{
  "schema": "readmit-source/v1", "name": "scheduling-exports", "kind": "directory", "scope": "appointments",
  "root": "exports",
  "quota": {"max_entries": 16, "max_entry_bytes": 65536, "max_total_bytes": 1048576},
  "retry": {"attempts": 2, "backoff": "250ms"}
}`,
		"transfer.json": `{
  "schema": "readmit-source/v1", "name": "scheduling-sftp", "kind": "transfer", "scope": "appointments",
  "address": "192.0.2.10:22", "classification": "nonproduction",
  "command": "/usr/local/libexec/readmit-sftp", "arguments": ["--path", "/exports/appointments"],
  "secrets_file": "secrets.json", "credential": "lab-sftp",
  "quota": {"max_entries": 64, "max_entry_bytes": 4194304, "max_total_bytes": 33554432},
  "retry": {"attempts": 1, "backoff": "1s"}
}`,
	} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		declared, err := evidencesource.ReadSource(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		opened := app.ReadSourceRegistration(root, name)
		if opened.State != desktop.Completed || opened.SourceFile != name || opened.Source == nil || !reflect.DeepEqual(*opened.Source, declared) {
			t.Fatalf("%s reopened as %+v, want %+v", name, opened, declared)
		}
		copied := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "copy-" + name, Source: *opened.Source})
		reopened := app.ReadSourceRegistration(root, "copy-"+name)
		if copied.State != desktop.Completed || reopened.State != desktop.Completed || !reflect.DeepEqual(*reopened.Source, declared) {
			t.Fatalf("%s saved and reopened as %+v %+v", name, copied, reopened)
		}
	}
	valid := map[string]any{}
	if err := json.Unmarshal([]byte(`{"schema":"readmit-source/v1","name":"exports","kind":"directory","scope":"appointments","root":"exports","quota":{"max_entries":1,"max_entry_bytes":1,"max_total_bytes":1},"retry":{"attempts":1,"backoff":"1ms"}}`), &valid); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(map[string]any){
		"unknown-member.json": func(d map[string]any) { d["extra"] = true },
		"foreign-member.json": func(d map[string]any) { d["address"] = "192.0.2.10:22" },
		"later.json":          func(d map[string]any) { d["schema"] = "readmit-source/v9" },
		"no-quota.json":       func(d map[string]any) { delete(d, "quota") },
	} {
		document := map[string]any{}
		for key, value := range valid {
			document[key] = value
		}
		change(document)
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, err = evidencesource.ReadSource(path)
		if err == nil {
			t.Fatalf("%s was not refused by the command line's reader", name)
		}
		if opened := app.ReadSourceRegistration(root, name); opened.State != desktop.Failed || opened.Reason != err.Error() || opened.Source != nil {
			t.Errorf("%s reopened as %+v, want refused with %q", name, opened, err.Error())
		}
	}
	if opened := app.ReadSourceRegistration(root, "../outside.json"); opened.State != desktop.Failed || opened.Reason != "file path must remain within the workspace" {
		t.Errorf("a registration outside the workspace: %+v", opened)
	}
}
