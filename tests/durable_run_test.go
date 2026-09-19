package tests

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// durableSpec writes one ACK-boundary spec over a captured case, pointed at
// address with a message timeout long enough that only an explicit deadline
// can stop a run whose peer stays silent.
func durableSpec(t *testing.T, address string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stdout, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "--output", filepath.Join(dir, "case"))
	if err != nil || stderr != "" || !strings.Contains(stdout, "Messages: 1") {
		t.Fatalf("capture durable input: %v %s %s", err, stdout, stderr)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096}
	accepted := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}}
	for name, document := range map[string]any{"target.json": target, "spec.json": spec} {
		raw, err := json.Marshal(document, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "spec.json"), dir
}

// durablePeer reads one frame and answers with code, or reads and never
// answers when code is empty.
func durablePeer(t *testing.T, code string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(8 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				if code == "" {
					io.Copy(io.Discard, conn)
					return
				}
				fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", code)
			}()
		}
	}()
	return listener.Addr().String()
}

func TestRunExecutableDeadlineStopsTheRunAndReportsTheDeliveryUncertain(t *testing.T) {
	spec, dir := durableSpec(t, durablePeer(t, ""))
	job := filepath.Join(dir, "job")
	stdout, stderr, err := run(t, "run", "start", spec, "--send", "--output", job, "--deadline", "500ms", "--json")
	if processCode(t, err) != 2 || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var summary durablerun.Summary
	if err := json.Unmarshal([]byte(stdout), &summary, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s %v", stdout, err)
	}
	if summary.Schema != durablerun.Schema || summary.State != durablerun.DeliveryUncertain || summary.StopReason != durablerun.TimedOut || !summary.DeliveryUncertain {
		t.Fatalf("%+v", summary)
	}
	stdout, stderr, err = run(t, "run", "status", job, "--recovery", "--json")
	if processCode(t, err) != 2 || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var recovery durablerun.Recovery
	if err := json.Unmarshal([]byte(stdout), &recovery, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s %v", stdout, err)
	}
	if recovery.Schema != durablerun.RecoverySchema || !recovery.Terminal || recovery.Uncertain != 1 || recovery.SafeToRepeat || recovery.Lease != durablerun.LeaseReleased || recovery.Run != summary {
		t.Fatalf("%+v", recovery)
	}
	stdout, stderr, err = run(t, "run", "status", job, "--recovery")
	if processCode(t, err) != 2 || !strings.Contains(stdout, "Occurrences: 0 acknowledged, 1 uncertain, 0 not attempted\n") || !strings.Contains(stdout, "Safe to repeat: false\nResume refused: an intent was synced") || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	// The uncertain send is never repeated, and the refusal writes nothing.
	before, _ := os.ReadFile(filepath.Join(job, "journal.jsonl"))
	resumed := filepath.Join(dir, "resumed")
	stdout, stderr, err = run(t, "run", "resume", job, spec, "--send", "--output", resumed, "--json")
	if processCode(t, err) != 2 || stdout != "" || !strings.Contains(stderr, "resume refused: an intent was synced without an acknowledged outcome") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	if _, err := os.Lstat(resumed); err == nil {
		t.Fatal("a refused resume created an output")
	}
	after, _ := os.ReadFile(filepath.Join(job, "journal.jsonl"))
	if !bytes.Equal(before, after) {
		t.Fatal("a refused resume changed the job")
	}
	for _, private := range []string{dir, "LISTEN-BOOK", "SYNTH-001"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatal("run console disclosed private paths or values")
		}
	}
}

func TestRunExecutableResumeRepeatsOnlyNeverAttemptedWork(t *testing.T) {
	spec, dir := durableSpec(t, durablePeer(t, "AA"))
	job := filepath.Join(dir, "job")
	stdout, stderr, err := run(t, "run", "start", spec, "--send", "--output", job, "--deadline", "1ns", "--json")
	if processCode(t, err) != 2 || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var summary durablerun.Summary
	if err := json.Unmarshal([]byte(stdout), &summary, json.RejectUnknownMembers(true)); err != nil || summary.State != durablerun.TimedOut || summary.DeliveryUncertain {
		t.Fatalf("%s %v", stdout, err)
	}
	stdout, stderr, err = run(t, "run", "status", job, "--recovery", "--json")
	var recovery durablerun.Recovery
	if err := json.Unmarshal([]byte(stdout), &recovery, json.RejectUnknownMembers(true)); err != nil || !recovery.SafeToRepeat || recovery.NotAttempted != 1 || stderr != "" {
		t.Fatalf("%s %s %v", stdout, stderr, err)
	}
	resumed := filepath.Join(dir, "resumed")
	stdout, stderr, err = run(t, "run", "resume", job, spec, "--send", "--output", resumed, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var resumption durablerun.Resumption
	if err := json.Unmarshal([]byte(stdout), &resumption, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s %v", stdout, err)
	}
	if resumption.Schema != durablerun.ResumeSchema || resumption.ResumedFrom != durablerun.TimedOut || resumption.Repeated != 1 || resumption.Run.State != durablerun.Passed {
		t.Fatalf("%+v", resumption)
	}
	stdout, stderr, err = run(t, "run", "status", resumed)
	if err != nil || stderr != "" || !strings.HasPrefix(stdout, "Run state: passed\n") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	// Once acknowledged, the same job is not repeated again.
	stdout, stderr, err = run(t, "run", "resume", resumed, spec, "--send", "--output", filepath.Join(dir, "again"))
	if processCode(t, err) != 2 || !strings.Contains(stderr, "resume refused: a delivery was acknowledged") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	// Cleanup removes nothing from a run that released its lease, and only the
	// stale lease of one that could not.
	stdout, stderr, err = run(t, "run", "clean", resumed, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var cleanup durablerun.Cleanup
	if err := json.Unmarshal([]byte(stdout), &cleanup, json.RejectUnknownMembers(true)); err != nil || cleanup.Schema != durablerun.CleanupSchema || len(cleanup.Removed) != 0 || len(cleanup.Retained) != 7 {
		t.Fatalf("%s %v", stdout, err)
	}
	lease, _ := json.Marshal(durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: []durablerun.Resource{{Kind: durablerun.EndpointResource, Name: "127.0.0.1:1"}}})
	if err := os.WriteFile(filepath.Join(resumed, "lease.json"), lease, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(t, "run", "clean", resumed)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Removed: lease.json\nRetained: engine.json, intended, journal.jsonl, plan.json, result, result.decision.json, sent\n") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(resumed, "lease.json")); err == nil {
		t.Fatal("stale lease retained")
	}
}

func TestRunExecutableRefusesUnsafeArguments(t *testing.T) {
	spec, dir := durableSpec(t, "127.0.0.1:1")
	for name, args := range map[string][]string{
		"zero deadline":     {"run", "start", spec, "--send", "--output", filepath.Join(dir, "a"), "--deadline", "0"},
		"malformed":         {"run", "start", spec, "--send", "--output", filepath.Join(dir, "b"), "--deadline", "soon"},
		"resume no send":    {"run", "resume", filepath.Join(dir, "missing"), spec, "--output", filepath.Join(dir, "c")},
		"resume no job":     {"run", "resume", filepath.Join(dir, "missing"), spec, "--send", "--output", filepath.Join(dir, "d")},
		"clean no job":      {"run", "clean", filepath.Join(dir, "missing")},
		"clean two args":    {"run", "clean", filepath.Join(dir, "missing"), "extra"},
		"status two args":   {"run", "status", filepath.Join(dir, "missing"), "extra"},
		"resume three args": {"run", "resume", "a", "b", "c"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, args...)
			if processCode(t, err) == 0 || stdout != "" || !strings.HasPrefix(stderr, "readmit: ") {
				t.Fatalf("%v %s %s", err, stdout, stderr)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 3 {
				t.Fatalf("a refused command created an output: %v", entries)
			}
		})
	}
}

// The desktop shell and the command line are two entry points into one
// evaluator, so a run started through either must record the same engine, spec
// contract and profile, and both must refuse a run evaluated under a version
// this release does not read rather than reporting a verdict about it.
func TestDesktopAndCommandLineRunOneEngineAndRefuseVersionsItDoesNotRead(t *testing.T) {
	for _, reached := range []string{"github.com/bharm16/readmit/internal/engine", "github.com/bharm16/readmit/internal/durablerun"} {
		for _, adapter := range []string{"../cmd/readmit", "../internal/desktop"} {
			if !strings.Contains(goCommand(t, nil, "list", "-deps", adapter), reached) {
				t.Fatalf("%s no longer reaches %s", adapter, reached)
			}
		}
	}

	spec, dir := durableSpec(t, durablePeer(t, "AA"))
	job := filepath.Join(dir, "job")
	stdout, stderr, err := run(t, "run", "start", spec, "--send", "--output", job, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	var summary durablerun.Summary
	if err := json.Unmarshal([]byte(stdout), &summary, json.RejectUnknownMembers(true)); err != nil || summary.State != durablerun.Passed {
		t.Fatalf("%s %v", stdout, err)
	}

	shellSpec, shellDir := durableSpec(t, durablePeer(t, "AA"))
	shellJob := filepath.Join(shellDir, "job")
	app := desktop.New(nil, "", "", "")
	if started := app.StartDurableRun(shellSpec, shellJob); started.State != desktop.Completed || started.Run == nil || started.Run.State != durablerun.Passed {
		t.Fatalf("%+v", started)
	}

	fromCLI, err := os.ReadFile(filepath.Join(job, "engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	fromShell, err := os.ReadFile(filepath.Join(shellJob, "engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromCLI, fromShell) {
		t.Fatalf("the shell and the command line recorded different engines: %s %s", fromCLI, fromShell)
	}
	pin, err := engine.Decode(fromCLI)
	if err != nil || pin != engine.Current(testrunner.SpecSchema) {
		t.Fatalf("%+v %v", pin, err)
	}

	// The reported pin is exactly the document the job retained.
	stdout, stderr, err = run(t, "run", "status", job, "--engine", "--json")
	if err != nil || stderr != "" || stdout != string(fromCLI) {
		t.Fatalf("%v %q %q %s", err, stdout, string(fromCLI), stderr)
	}
	stdout, stderr, err = run(t, "run", "status", job, "--engine")
	if err != nil || stderr != "" || !strings.Contains(stdout, "Spec contract: "+testrunner.SpecSchema+"\n") || !strings.Contains(stdout, "Read by this build: true\n") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	if _, _, err = run(t, "run", "status", job, "--engine", "--recovery"); processCode(t, err) == 0 {
		t.Fatal("reported a pin and a recovery classification at once")
	}

	// A run evaluated under a spec contract this release does not read is
	// refused by name through both entry points, and its versions are still
	// reported so an operator reads why.
	later := engine.Pin{Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: "readmit-test/v2", Profile: pin.Profile}
	raw, err := engine.Encode(later)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(shellJob, "engine.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(t, "run", "status", shellJob, "--json")
	if processCode(t, err) != 2 || stdout != "" || !strings.Contains(stderr, "unsupported engine version") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	stdout, stderr, err = run(t, "run", "status", shellJob, "--engine", "--json")
	if processCode(t, err) != 2 || stdout != string(raw) || !strings.Contains(stderr, "does not read") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	opened := app.OpenDurableRun(shellJob)
	if opened.State != desktop.Failed || !strings.Contains(opened.Reason, "cannot read") {
		t.Fatalf("%+v", opened)
	}
	for _, private := range []string{shellDir, "LISTEN-BOOK", "SYNTH-001"} {
		if strings.Contains(stdout+stderr+opened.Reason, private) {
			t.Fatal("an engine pin disclosed private paths or values")
		}
	}
}

// The pin's build identity comes from one linker symbol, and the release
// configuration is the only thing that stamps it. A rename would ship archives
// pinning `dev` without failing anything, because the linker ignores -X for a
// symbol that is not there, so the symbol the archives are built with is read
// out of .goreleaser.yml and followed all the way into a retained pin.
func TestTheReleaseStampIsTheIdentityARunRetains(t *testing.T) {
	configuration, err := os.ReadFile("../.goreleaser.yml")
	if err != nil {
		t.Fatal(err)
	}
	stamp := regexp.MustCompile(`-X ([^\s=]+)=\{\{\.Version\}\}`).FindSubmatch(configuration)
	if stamp == nil {
		t.Fatal("the release configuration stamps no version symbol")
	}
	const probe = "0.0.0-stamp-probe"
	stamped := filepath.Join(t.TempDir(), "readmit-stamped")
	goCommand(t, []string{"CGO_ENABLED=0"}, "build", "-trimpath", "-ldflags", "-X "+string(stamp[1])+"="+probe, "-o", stamped, "../cmd/readmit")

	execute := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, stamped, args...)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil {
			t.Fatalf("%v %s %s", err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	if version := execute("--version"); !strings.Contains(version, probe) {
		t.Fatalf("the release stamp did not reach the executable: %q", version)
	}
	spec, dir := durableSpec(t, durablePeer(t, "AA"))
	job := filepath.Join(dir, "job")
	execute("run", "start", spec, "--send", "--output", job, "--json")
	pin, err := engine.Decode([]byte(execute("run", "status", job, "--engine", "--json")))
	if err != nil || pin.Engine != probe {
		t.Fatalf("the run pinned %+v rather than the stamped build: %v", pin, err)
	}
}
