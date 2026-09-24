package tests

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

// fixtureExchange sends frozen SIU fixtures, one after another on one
// connection, to a listening fixture and returns what each acknowledgement
// said: its MSA segment and its receipt, with the random session named SESSION.
func fixtureExchange(t *testing.T, address string, fixtures ...string) []string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	frames, err := mllp.NewReader(conn, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var answered []string
	for _, fixture := range fixtures {
		payload, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", fixture))
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
		var said []string
		for segment := range strings.SplitSeq(strings.TrimRight(string(ack), "\r"), "\r") {
			switch {
			case strings.HasPrefix(segment, "MSA|"):
				said = append(said, segment)
			case strings.HasPrefix(segment, "ZRT|"):
				fields := strings.Split(segment, "|")
				if len(fields) != 4 {
					t.Fatalf("receipt %q", segment)
				}
				fields[2] = "SESSION"
				said = append(said, strings.Join(fields, "|"))
			}
		}
		answered = append(answered, strings.Join(said, "\r"))
	}
	return answered
}

// windowListening waits until the window's running capture reports the
// address it bound, as the capture screen reads it.
func windowListening(t *testing.T, app *desktop.App, answered chan desktop.CaptureSessionResult) string {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case result := <-answered:
			t.Fatalf("the window's capture answered before it listened: %+v", result)
		default:
		}
		if progress := app.CaptureProgress(); progress.State == desktop.Completed && progress.Progress != nil {
			return progress.Progress.BoundAddress
		}
	}
	t.Fatal("the window's capture never reported where it listens")
	return ""
}

// caseSummary is the summary the command line prints of a case, up to the
// timeline `readmit timeline` goes on to list, without the line naming the
// case's identity, which a recorded case's random session makes differ from
// any other recording of the same exchange.
func caseSummary(output string) string {
	summary, _, _ := strings.Cut(output, "Timeline (")
	var kept []string
	for line := range strings.SplitSeq(summary, "\n") {
		if !strings.HasPrefix(line, "Bundle: ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// The capture screen's SIU fixture is `readmit listen`: the same exchange, sent
// to each on the port it chose, is acknowledged the same, exports the same
// appointment ledger (the hand-authored expectation for its mode), and seals a
// case the command line reads exactly as it reads its own, with the counts the
// window reported. Only the random session and the instants differ.
func TestTheWindowsFixtureListenIsTheCommandLinesListen(t *testing.T) {
	for _, mode := range []string{"fixed", "defective"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			command := startReceiver(t, 30*time.Second, "listen", "--address", "127.0.0.1:0", "--mode", mode,
				"--output", filepath.Join(dir, "command.case"), "--observation", filepath.Join(dir, "command-ledger.json"),
				"--max-messages", "2", "--idle-timeout", "10s")
			if startup := command.startup(t, 2); startup != "Profile: readmit-siu-v1\nMode: "+mode+"\n" {
				t.Fatalf("listen startup: %q", startup)
			}
			commandAcks := fixtureExchange(t, command.address, "listen-s12.hl7", "listen-s13.hl7")
			printed := command.wait(t)

			workspace := t.TempDir()
			app := desktopApp(t, workspace)
			answered := make(chan desktop.CaptureSessionResult, 1)
			go func() {
				answered <- app.StartCapture(desktop.CaptureRequest{
					Workspace: workspace, Kind: "listen", Address: "127.0.0.1:0", FixtureMode: mode,
					OutputName: "window.case", ObservationName: "window-ledger.json", MaxMessages: 2, IdleTimeout: "10s",
				})
			}()
			// The window reports where it listens at the point `readmit listen`
			// prints its Listening line: the declared host, on the port it chose.
			bound := windowListening(t, app, answered)
			windowHost, windowPort, err := net.SplitHostPort(bound)
			commandHost, _, _ := net.SplitHostPort(command.address)
			if err != nil || windowHost != commandHost || windowHost != "127.0.0.1" || windowPort == "0" {
				t.Fatalf("the window bound %q and the command line %q for a declared 127.0.0.1:0", bound, command.address)
			}
			if _, err := observation.Read(filepath.Join(workspace, "window-ledger.json")); err != nil {
				t.Fatalf("the window reported listening before its initial ledger was installed: %v", err)
			}
			windowAcks := fixtureExchange(t, bound, "listen-s12.hl7", "listen-s13.hl7")
			result := <-answered
			if result.State != desktop.Completed || result.Case == nil || result.Ledger == nil {
				t.Fatalf("the window's listen: %+v", result)
			}
			if !reflect.DeepEqual(windowAcks, commandAcks) || len(commandAcks) != 2 {
				t.Fatalf("acknowledgements differ:\nwindow  %q\ncommand %q", windowAcks, commandAcks)
			}

			// The exported ledgers are the same appointments, processed in the
			// same order, and both are the hand-authored expectation.
			expected := readStrictDocument[[]observation.Record](t, filepath.Join("..", "testdata", "fixtures", "listen-"+mode+".json"))
			commandLedger, err := observation.Read(filepath.Join(dir, "command-ledger.json"))
			if err != nil {
				t.Fatal(err)
			}
			windowLedger, err := observation.Read(filepath.Join(workspace, "window-ledger.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(windowLedger.Records, expected) || !reflect.DeepEqual(commandLedger.Records, expected) {
				t.Fatalf("exported ledgers:\nwindow  %+v\ncommand %+v\nwant    %+v", windowLedger.Records, commandLedger.Records, expected)
			}
			if !reflect.DeepEqual(windowLedger.Processed, commandLedger.Processed) || windowLedger.Mode != commandLedger.Mode ||
				windowLedger.Profile != commandLedger.Profile || !windowLedger.Consistent || !commandLedger.Consistent {
				t.Fatalf("exported ledgers differ:\nwindow  %+v\ncommand %+v", windowLedger, commandLedger)
			}

			// The command line reads the window's case exactly as it printed its
			// own, and that is what the window reported.
			timeline, stderr, err := run(t, "timeline", filepath.Join(workspace, "window.case"))
			if err != nil || stderr != "" {
				t.Fatalf("timeline of the window's case: %v %s", err, stderr)
			}
			if caseSummary(timeline) != caseSummary(printed) {
				t.Fatalf("the window's case reads\n%s\nthe command printed\n%s", timeline, printed)
			}
			for _, line := range []string{
				"Schema: " + result.Case.Schema,
				"Provenance: " + result.Case.Provenance,
				fmt.Sprintf("Sources: %d", result.Case.Sources),
				fmt.Sprintf("Messages: %d", result.Case.Messages),
				fmt.Sprintf("ACKs: %d", result.Case.Acknowledgements),
				"Observation: " + result.Ledger.Schema,
				"Observation profile: " + result.Ledger.Profile,
				"Receiver mode: " + result.Ledger.Mode,
				fmt.Sprintf("Processed occurrences: %d", result.Ledger.Processed),
				fmt.Sprintf("Ledger records: %d", result.Ledger.Records),
				fmt.Sprintf("Consistent: %t", result.Ledger.Consistent),
			} {
				if !strings.Contains(printed, line+"\n") {
					t.Errorf("the window reported %q, which `readmit listen` did not print", line)
				}
			}

			// The sealed cases hold the same received bytes and the same ledger.
			windowCase, err := bundle.Open(filepath.Join(workspace, "window.case"))
			if err != nil {
				t.Fatal(err)
			}
			commandCase, err := bundle.Open(filepath.Join(dir, "command.case"))
			if err != nil {
				t.Fatal(err)
			}
			if windowCase.Identity != result.Case.Identity || !reflect.DeepEqual(windowCase.Observation.Records, commandCase.Observation.Records) {
				t.Fatal("the sealed ledgers differ")
			}
			for i, event := range commandCase.Events {
				if event.Kind != bundle.Message {
					continue
				}
				commandBytes, _ := commandCase.Raw(event.ID)
				windowBytes, _ := windowCase.Raw(windowCase.Events[i].ID)
				if windowCase.Events[i].Kind != bundle.Message || string(windowBytes) != string(commandBytes) {
					t.Fatalf("received message %d differs", i)
				}
			}
		})
	}
}

// What `readmit listen` refuses before it serves, the capture screen refuses
// in the same words, and neither writes a case or a ledger: a wider bind
// without approval, a name, an address something else holds, and a
// destination that already exists.
func TestTheWindowRefusesAFixtureListenInTheCommandLinesWords(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	for _, refused := range []struct{ name, address, existing string }{
		{"nonloopback", "0.0.0.0:0", ""},
		{"name", "localhost:0", ""},
		{"held", held.Addr().String(), ""},
		{"case exists", "127.0.0.1:0", "fixture.case"},
		{"ledger exists", "127.0.0.1:0", "ledger.json"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			dir, workspace := t.TempDir(), t.TempDir()
			if refused.existing != "" {
				for _, folder := range []string{dir, workspace} {
					if err := os.Mkdir(filepath.Join(folder, refused.existing), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, stderr, err := run(t, "listen", "--address", refused.address, "--mode", "fixed",
				"--output", filepath.Join(dir, "fixture.case"), "--observation", filepath.Join(dir, "ledger.json"), "--max-messages", "1")
			if err == nil || stderr == "" {
				t.Fatalf("`readmit listen` served at %s", refused.address)
			}
			app := desktopApp(t, workspace)
			result := app.StartCapture(desktop.CaptureRequest{
				Workspace: workspace, Kind: "listen", Address: refused.address, FixtureMode: "fixed",
				OutputName: "fixture.case", ObservationName: "ledger.json", MaxMessages: 1, IdleTimeout: "1s",
			})
			if result.State != desktop.Failed || result.Reason != refusedAs(stderr) || result.BoundAddress != "" {
				t.Fatalf("the window answered %+v, the command line refused with %q", result, refusedAs(stderr))
			}
			kept := 0
			if refused.existing != "" {
				kept = 1
			}
			for _, folder := range []string{dir, workspace} {
				if entries, _ := os.ReadDir(folder); len(entries) != kept {
					t.Fatalf("a refused listen wrote into %s: %v", folder, entries)
				}
			}
		})
	}
}

// enhancedAsPrinted is how `readmit collect` names a policy's enhanced
// acknowledgement on its startup lines.
func enhancedAsPrinted(p collection.Policy) string {
	if !p.SupportsEnhanced() {
		return collection.EnhancedUnsupported
	}
	return fmt.Sprintf("%s %s %s %s", p.Enhanced.Operator, p.Enhanced.AcceptCode, p.Enhanced.ApplicationCode, p.Enhanced.ApplicationDelivery)
}

// A responder policy hand-authored for `readmit collect`, and the copy the
// window saves once it has reopened it, are the policy the command line
// serves: it states the same declarations on its startup lines as the window
// reopened. What the command line refuses to serve under, the window refuses
// to reopen in the same words, and a fault policy is held to the endpoints it
// approves before anything binds in both.
func TestAReopenedResponderPolicyIsTheOneTheCommandLineServes(t *testing.T) {
	workspace := t.TempDir()
	app := desktopApp(t, workspace)
	approved := freeLoopbackAddress(t, "tcp")
	documents := map[string]string{
		"v1": `{"schema": "readmit-receiver-policy/v1", "name": "downstream-sink", "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AR"},
  "accepted_message_types": {"operator": "message-type-in", "values": ["SIU^S12", "ADT"]}}`,
		"v2": `{"schema": "readmit-receiver-policy/v2", "name": "enhanced-sink", "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
  "accepted_message_types": {"operator": "any-message-type", "values": []},
  "enhanced_acknowledgement": {"operator": "enhanced-mode-fixed-codes", "accept_code": "CA", "application_code": "AE",
    "application_delivery": "same-connection", "application_endpoint": "", "approved_transport": false}}`,
		"v3": `{"schema": "readmit-receiver-policy/v3", "name": "faulting-sink", "source_label": "downstream-test-endpoint",
  "acknowledgement": {"operator": "original-mode-fixed-code", "code": "AA"},
  "accepted_message_types": {"operator": "any-message-type", "values": []},
  "enhanced_acknowledgement": {"operator": "unsupported", "accept_code": "", "application_code": "",
    "application_delivery": "", "application_endpoint": "", "approved_transport": false},
  "faults": {"environment_class": "nonproduction", "approved_test_endpoints": ["` + approved + `"],
    "steps": [{"message": 1, "stage": "application", "action": "delay", "delay_ms": 25},
              {"message": 2, "stage": "application", "action": "malformed-ack", "delay_ms": 0}]}}`,
	}
	for version, document := range documents {
		authored := writeDocument(t, workspace, version+".json", document)
		opened := app.ReadReceiverPolicy(workspace, version+".json")
		if opened.State != desktop.Completed || opened.Policy == nil {
			t.Fatalf("%s reopened as %+v", version, opened)
		}
		if saved := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: workspace, PolicyFile: "window-" + version + ".json", Policy: *opened.Policy}); saved.State != desktop.Completed {
			t.Fatalf("%s saved as %+v", version, saved)
		}
		for _, file := range []string{authored, filepath.Join(workspace, "window-"+version+".json")} {
			reopened := app.ReadReceiverPolicy(workspace, file)
			if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Policy, opened.Policy) {
				t.Fatalf("%s reopened as %+v, want %+v", file, reopened.Policy, opened.Policy)
			}
			p := *reopened.Policy
			address := "127.0.0.1:0"
			if p.Faults != nil {
				address = approved
			}
			collector := startReceiver(t, 20*time.Second, "collect", "--address", address, "--policy", file,
				"--output", filepath.Join(t.TempDir(), "case"), "--max-messages", "1")
			want := fmt.Sprintf("Policy: %s\nSource label: %s\nAcknowledgement: %s %s\nEnhanced acknowledgement: %s\n",
				p.Name, p.SourceLabel, p.Acknowledgement.Operator, p.Acknowledgement.Code, enhancedAsPrinted(p))
			if startup := collector.startup(t, 4); startup != want {
				t.Fatalf("`readmit collect --policy %s` serves\n%s\nthe window reopened\n%s", filepath.Base(file), startup, want)
			}
			collector.kill(t)
		}
	}

	// A fault policy at an address it does not approve: both refuse before
	// binding, in the same words.
	elsewhere := filepath.Join(workspace, "v3.json")
	_, stderr, err := run(t, "collect", "--address", "127.0.0.1:0", "--policy", elsewhere, "--output", filepath.Join(t.TempDir(), "case"))
	if err == nil {
		t.Fatal("`readmit collect` served a fault policy where it approves nothing")
	}
	preview := app.PreviewCapture(desktop.CaptureRequest{Workspace: workspace, Kind: "collect", Address: "127.0.0.1:0", PolicyFile: "v3.json", OutputName: "case"})
	if preview.State != desktop.Failed || preview.Reason != refusedAs(stderr) {
		t.Fatalf("the window previewed %+v, the command line refused with %q", preview, refusedAs(stderr))
	}

	// What the command line will not serve under, the window will not reopen.
	anyTypes := `{"schema":"readmit-receiver-policy/v1","name":"sink","source_label":"label","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`
	for name, document := range map[string]string{
		"unknown-member.json": strings.Replace(anyTypes, `"name"`, `"extra":1,"name"`, 1),
		"v1-enhanced.json":    strings.Replace(anyTypes, `"name"`, `"enhanced_acknowledgement":null,"name"`, 1),
		"v3-no-faults.json":   strings.Replace(documents["v2"], "/v2", "/v3", 1),
		"later.json":          strings.Replace(anyTypes, "/v1", "/v4", 1),
		"code.json":           strings.Replace(anyTypes, `"AA"`, `"CA"`, 1),
		"oversized.json":      anyTypes + strings.Repeat(" ", collection.MaxPolicyBytes),
	} {
		path := writeDocument(t, workspace, name, document)
		_, stderr, err := run(t, "collect", "--address", "127.0.0.1:0", "--policy", path, "--output", filepath.Join(t.TempDir(), "case"))
		if err == nil || stderr == "" {
			t.Fatalf("`readmit collect` served under %s", name)
		}
		if opened := app.ReadReceiverPolicy(workspace, name); opened.State != desktop.Failed || opened.Reason != refusedAs(stderr) {
			t.Errorf("%s reopened as %+v, the command line refused with %q", name, opened, refusedAs(stderr))
		}
	}
}

// A source registration hand-authored for `readmit source`, and the copy the
// window saves once it has reopened it, are the source the command line
// diagnoses: its whole diagnosis of each is the same, and names what the window
// reopened. What the command line refuses to read, the window refuses to
// reopen in the same words.
func TestAReopenedSourceRegistrationIsTheOneTheCommandLineDiagnoses(t *testing.T) {
	workspace, authored := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage, "b.hl7": sourceMessage})
	app := desktopApp(t, workspace)
	opened := app.ReadSourceRegistration(workspace, "source.json")
	if opened.State != desktop.Completed || opened.Source == nil {
		t.Fatalf("reopened as %+v", opened)
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: workspace, SourceFile: "window-source.json", Source: *opened.Source}); saved.State != desktop.Completed {
		t.Fatalf("saved as %+v", saved)
	}
	copied := filepath.Join(workspace, "window-source.json")
	if reopened := app.ReadSourceRegistration(workspace, copied); reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Source, opened.Source) {
		t.Fatalf("the window's copy reopened as %+v, want %+v", reopened.Source, opened.Source)
	}
	diagnosis := func(file string) string {
		t.Helper()
		stdout, stderr, err := run(t, append([]string{"source", "diagnose", file}, sourcePlanFlags()...)...)
		if err != nil || stderr != "" {
			t.Fatalf("source diagnose %s: %v %s", filepath.Base(file), err, stderr)
		}
		return stdout
	}
	fromAuthored, fromWindow := diagnosis(authored), diagnosis(copied)
	if fromAuthored != fromWindow {
		t.Fatalf("the command line diagnoses the window's copy as\n%s\nand the authored registration as\n%s", fromWindow, fromAuthored)
	}
	s := opened.Source
	for _, line := range []string{
		"Source: " + s.Name, "Kind: " + string(s.Kind), "Scope: " + s.Scope,
		fmt.Sprintf("Quota: %d entries, %d bytes per entry, %d bytes in total", s.Quota.MaxEntries, s.Quota.MaxEntryBytes, s.Quota.MaxTotalBytes),
	} {
		if !strings.Contains(fromAuthored, line+"\n") {
			t.Errorf("the window reopened %q, which the command line's diagnosis does not name", line)
		}
	}

	// An api source is declarable and read, and only refused when reached; the
	// others are refused by the reader itself.
	for _, declared := range []struct {
		name     string
		read     bool
		document func(string) string
	}{
		{"unknown-member.json", false, func(d string) string { return strings.Replace(d, `"name"`, `"extra": 1, "name"`, 1) }},
		{"foreign-member.json", false, func(d string) string { return strings.Replace(d, `"root"`, `"address": "192.0.2.10:22", "root"`, 1) }},
		{"later.json", false, func(d string) string { return strings.Replace(d, "/v1", "/v2", 1) }},
		{"no-scope.json", false, func(d string) string { return strings.Replace(d, `"scope": "appointments",`, "", 1) }},
		{"api.json", true, func(string) string { return sourceAPIDocument }},
	} {
		original, err := os.ReadFile(authored)
		if err != nil {
			t.Fatal(err)
		}
		path := writeDocument(t, workspace, declared.name, declared.document(string(original)))
		stdout, stderr, err := run(t, append([]string{"source", "diagnose", path}, sourcePlanFlags()...)...)
		if err == nil {
			t.Fatalf("`readmit source diagnose` reached %s", declared.name)
		}
		opened := app.ReadSourceRegistration(workspace, declared.name)
		if declared.read {
			if opened.State != desktop.Completed || !strings.Contains(stdout, "Source: "+opened.Source.Name+"\nKind: "+string(opened.Source.Kind)+"\n") {
				t.Errorf("%s: the command line read %q, the window reopened %+v", declared.name, stdout, opened)
			}
			continue
		}
		if stdout != "" || opened.State != desktop.Failed || opened.Reason != refusedAs(stderr) {
			t.Errorf("%s reopened as %+v, the command line refused with %q", declared.name, opened, refusedAs(stderr))
		}
	}
}
