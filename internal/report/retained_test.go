package report_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/report"
)

func TestAssembleRetainsActualRunsAndHonestSingleRun(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, source)
	for _, baseline := range []bool{false, true} {
		input := report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix")}
		if baseline {
			input.Baseline = filepath.Join(source, "baseline")
		}
		out := filepath.Join(t.TempDir(), "packet")
		p, err := report.Assemble(context.Background(), input, out)
		if err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(t.TempDir(), "relocated")
		if err := os.Rename(out, moved); err != nil {
			t.Fatal(err)
		}
		got, err := report.OpenRetained(context.Background(), moved)
		if err != nil || got.Identity != p.Identity {
			t.Fatalf("relocation: %v", err)
		}
		if got.Manifest.Current.Status != "pass" || (got.Manifest.Baseline != nil) != baseline {
			t.Fatalf("incorrect outcomes: %+v", got.Manifest)
		}
		if !baseline && !strings.Contains(string(read(t, filepath.Join(moved, "SUMMARY.md"))), "No observed baseline") {
			t.Fatal("missing single-run limitation")
		}
		if !reflect.DeepEqual(snapshot(t, filepath.Join(source, "post-fix")), snapshot(t, filepath.Join(moved, "current"))) {
			t.Fatal("retained execution bytes changed")
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, source)) {
		t.Fatal("original modified")
	}
}

func TestRetainedPacketRefusesTamperingMismatchesAndCancellation(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	input := report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix")}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(context.Background(), input, packet); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing", "extra", "claim", "summary", "incomplete", "spec", "payload", "unknown", "null", "omitted"} {
		t.Run(kind, func(t *testing.T) {
			dir := clone(t, packet)
			switch kind {
			case "missing":
				os.Remove(filepath.Join(dir, "current", "spec.json"))
			case "extra":
				write(t, filepath.Join(dir, "extra"), []byte("private"))
			case "claim":
				write(t, filepath.Join(dir, "manifest.json"), []byte(strings.Replace(string(read(t, filepath.Join(dir, "manifest.json"))), `"case_provenance":"generated"`, `"case_provenance":"imported"`, 1)))
			case "summary":
				write(t, filepath.Join(dir, "SUMMARY.md"), []byte("Manufactured passing baseline"))
			case "incomplete":
				os.Remove(filepath.Join(dir, "identity.sha256"))
			case "spec":
				write(t, filepath.Join(dir, "spec.json"), []byte("{}"))
			case "payload":
				write(t, filepath.Join(dir, "current", "result.json"), []byte("{}"))
			case "unknown":
				write(t, filepath.Join(dir, "manifest.json"), []byte(strings.Replace(string(read(t, filepath.Join(dir, "manifest.json"))), `"state":"complete"`, `"state":"complete","extra":true`, 1)))
			case "null":
				write(t, filepath.Join(dir, "manifest.json"), []byte(strings.Replace(string(read(t, filepath.Join(dir, "manifest.json"))), `"contains_source_values":true`, `"contains_source_values":null`, 1)))
			case "omitted":
				write(t, filepath.Join(dir, "manifest.json"), []byte(strings.Replace(string(read(t, filepath.Join(dir, "manifest.json"))), `"baseline":null,`, "", 1)))
			}
			if kind == "claim" || kind == "unknown" || kind == "null" || kind == "omitted" {
				write(t, filepath.Join(dir, "identity.sha256"), []byte(sha(read(t, filepath.Join(dir, "manifest.json")))+"\n"))
			}
			if _, err := report.OpenRetained(context.Background(), dir); err == nil {
				t.Fatal("accepted damaged packet")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "cancelled")
	if _, err := report.Assemble(ctx, input, out); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("cancelled assembly created output")
	}
	if _, err := report.Assemble(context.Background(), input, packet); err == nil {
		t.Fatal("overwrote packet")
	}
	if _, err := report.Assemble(context.Background(), input, filepath.Join(packet, "nested")); err == nil {
		t.Fatal("wrote within evidence")
	}
	input.Baseline = input.Current
	if _, err := report.Assemble(context.Background(), input, filepath.Join(t.TempDir(), "duplicate")); err == nil {
		t.Fatal("manufactured baseline from same execution")
	}
	input.Baseline = ""
	input.Spec = filepath.Join(t.TempDir(), "spec.json")
	write(t, input.Spec, append(read(t, filepath.Join(source, "spec.json")), '\n'))
	if _, err := report.Assemble(context.Background(), input, filepath.Join(t.TempDir(), "mismatch")); err == nil {
		t.Fatal("accepted nonidentical spec")
	}
}

func TestRetainedImportedDurableRunAndExecutionFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	raw := []byte("MSH|^~\\&|TEST|LOCAL|||20260101000000||ADT^A01|CONTROL|P|2.5.1\rPID|1||PRIVATE-ID\r")
	c, err := bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Path: "private-source.hl7", Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(conn, 4096)
		_, e = reader.ReadFrame()
		if e == nil {
			_, e = conn.Write([]byte("\x0bMSH|^~\\&|TEST|LOCAL|||20260101000000||ACK|ACK1|P|2.5.1\rMSA|AA|CONTROL\r\x1c\r"))
		}
		done <- e
	}()
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096}
	write(t, filepath.Join(dir, "target.json"), marshal(t, target))
	aa := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "Imported ACK evidence", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "Operator authorized test endpoint and reset"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &aa}}}}}
	specPath := filepath.Join(dir, "spec.json")
	write(t, specPath, marshal(t, spec))
	job := filepath.Join(dir, "job")
	state, err := durablerun.Start(context.Background(), specPath, job)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if state.State != durablerun.Passed {
		t.Fatalf("run: %+v", state)
	}
	in := report.RetainedInput{Case: filepath.Join(dir, "case"), Spec: specPath, Current: job}
	packet, err := report.Assemble(context.Background(), in, filepath.Join(dir, "packet"))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Manifest.Current.CaseIdentity != c.Identity || packet.Manifest.Current.CaseProvenance != "imported" || packet.Manifest.Current.RunState != "passed" {
		t.Fatalf("lost provenance: %+v", packet)
	}
	listener.Close()
	failed := filepath.Join(dir, "failed")
	result, err := testrunner.Run(context.Background(), specPath, failed)
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.Status != testrunner.ExecutionError {
		t.Fatal("expected connection failure")
	}
	in.Current = failed
	packet, err = report.Assemble(context.Background(), in, filepath.Join(dir, "failed-packet"))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Manifest.Current.Status != "execution_error" {
		t.Fatal("failure upgraded")
	}
	failedJob := filepath.Join(dir, "failed-job")
	state, err = durablerun.Start(context.Background(), specPath, failedJob)
	if err != nil || state.State != durablerun.ExecutionError {
		t.Fatalf("failed durable run: %+v %v", state, err)
	}
	in.Current = failedJob
	failedPacket := filepath.Join(dir, "failed-job-packet")
	packet, err = report.Assemble(context.Background(), in, failedPacket)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := report.OpenRetained(context.Background(), failedPacket)
	if err != nil || reopened.Identity != packet.Identity || reopened.Manifest.Current.Status != "execution_error" || reopened.Manifest.Current.RunState != "execution_error" {
		t.Fatalf("lost durable failure: %+v %v", reopened, err)
	}

}

func TestRetainedRejectsResealedFalseSummary(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	in := report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix")}
	out := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(context.Background(), in, out); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(out, "SUMMARY.md"), []byte("Baseline failed and current proves fix"))
	var manifest report.RetainedManifest
	if err := json.Unmarshal(read(t, filepath.Join(out, "manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	files := snapshot(t, out)
	names := []string{}
	for name := range files {
		if name != "manifest.json" && name != "identity.sha256" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	manifest.Files = []bundle.Payload{}
	for _, name := range names {
		manifest.Files = append(manifest.Files, bundle.Payload{Path: name, Size: len(files[name]), SHA256: sha(files[name])})
	}
	raw := marshal(t, manifest)
	write(t, filepath.Join(out, "manifest.json"), raw)
	write(t, filepath.Join(out, "identity.sha256"), []byte(sha(raw)+"\n"))
	if _, err := report.OpenRetained(context.Background(), out); err == nil {
		t.Fatal("accepted resealed false summary")
	}
}

// The preview and the commit agree at the package's interface: the sections
// the preview names are the sections the sealed manifest indexes, a preview
// with no problems is exactly the input assembly seals, and every problem a
// preview reports names an input assembly refuses.
func TestPreviewRetainedAgreesWithAssemble(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	wrongCase := filepath.Join(t.TempDir(), "wrong")
	if _, err := bundle.Write(wrongCase, []bundle.Input{{Path: "other.hl7", Data: []byte("MSH|^~\\&|OTHER|SITE|||20260101000000||ADT^A01|OTHER|P|2.5.1\r"), Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); err != nil {
		t.Fatal(err)
	}
	input := report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix"), Baseline: filepath.Join(source, "baseline")}

	preview, err := report.PreviewRetained(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Problems) != 0 || !preview.BaselineSupplied {
		t.Fatalf("the complete input previewed with problems: %+v", preview)
	}
	if preview.Case == nil || !preview.Case.Found || !preview.Case.CaseMatch || len(preview.Case.Problems) != 0 {
		t.Fatalf("the case did not read as verified: %+v", preview.Case)
	}
	if preview.Spec == nil || !preview.Spec.Found || !preview.Spec.SpecMatch || len(preview.Spec.Problems) != 0 {
		t.Fatalf("the specification did not read as verified: %+v", preview.Spec)
	}
	if preview.Current == nil || !preview.Current.Found || preview.Current.Status == "" || len(preview.Current.Problems) != 0 {
		t.Fatalf("the current execution did not read as verified: %+v", preview.Current)
	}
	if preview.Baseline == nil || !preview.Baseline.Found || preview.Baseline.Status == "" || len(preview.Baseline.Problems) != 0 {
		t.Fatalf("the baseline did not read as verified: %+v", preview.Baseline)
	}
	if preview.BaselineCase == nil || !preview.BaselineCase.Found || !preview.BaselineCase.CaseMatch || len(preview.BaselineCase.Problems) != 0 {
		t.Fatalf("the baseline's own case did not read as verified: %+v", preview.BaselineCase)
	}
	if len(preview.Limitations) == 0 {
		t.Fatal("a supplied baseline states what it establishes")
	}
	named := strings.Join(preview.Inventory, "\n")
	for _, section := range []string{"case/", "spec.json", "current/", "baseline/", "baseline-case/", "SUMMARY.md"} {
		if !strings.Contains(named, section) {
			t.Fatalf("the inventory did not name %s: %q", section, preview.Inventory)
		}
	}
	out := filepath.Join(t.TempDir(), "packet")
	packet, err := report.Assemble(context.Background(), input, out)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Manifest.ExportPolicy != preview.ExportPolicy || packet.Manifest.ContainsSourceValues != preview.ContainsSourceValues {
		t.Fatalf("the preview and the manifest disagree about the packet's own statements: %+v vs %+v", preview, packet.Manifest)
	}
	for _, section := range []string{"case/", "spec.json", "current/", "baseline/", "baseline-case/"} {
		present := false
		for _, file := range packet.Manifest.Files {
			present = present || strings.HasPrefix(file.Path, section)
		}
		if !present {
			t.Fatalf("the sealed packet does not hold %s, which the preview named: %q", section, preview.Inventory)
		}
	}

	// Each problem a preview reports is an input assembly refuses, and the
	// refusal agrees across the two interfaces.
	for _, test := range []struct {
		what                     string
		change                   func(*report.RetainedInput)
		problemIn                func(*report.RetainedPreview) []string
		says, orSays             string
		completeWithoutSelection bool
	}{
		{
			what:      "an unrelated case",
			change:    func(in *report.RetainedInput) { in.Case = wrongCase },
			problemIn: func(p *report.RetainedPreview) []string { return p.Case.Problems },
			says:      "not the case the current result retained",
		},
		{
			what:      "a missing case",
			change:    func(in *report.RetainedInput) { in.Case = filepath.Join(source, "absent") },
			problemIn: func(p *report.RetainedPreview) []string { return p.Case.Problems },
			says:      "not a case bundle this release verifies",
		},
		{
			what:      "a rewritten historical specification",
			change:    func(in *report.RetainedInput) { in.Spec = writeSpecCopy(t, source, "\n") },
			problemIn: func(p *report.RetainedPreview) []string { return p.Spec.Problems },
			says:      "never substitutes",
		},
		{
			what:      "a specification this release does not assemble",
			change:    func(in *report.RetainedInput) { in.Spec = writeRawSpec(t, "{}") },
			problemIn: func(p *report.RetainedPreview) []string { return p.Spec.Problems },
			says:      "not one this release assembles",
		},
		{
			what:      "a missing baseline",
			change:    func(in *report.RetainedInput) { in.Baseline = filepath.Join(source, "absent-run") },
			problemIn: func(p *report.RetainedPreview) []string { return p.Baseline.Problems },
			says:      "not a retained execution this release verifies",
		},
		{
			what:      "the current result relabelled as the baseline",
			change:    func(in *report.RetainedInput) { in.Baseline = in.Current },
			problemIn: func(p *report.RetainedPreview) []string { return p.Problems },
			says:      "distinct retained execution",
		},
		{
			what:      "an incomplete current execution",
			change:    func(in *report.RetainedInput) { in.Current = incompleteCopy(t, source) },
			problemIn: func(p *report.RetainedPreview) []string { return p.Current.Problems },
		},
	} {
		t.Run(test.what, func(t *testing.T) {
			changed := input
			test.change(&changed)
			preview, err := report.PreviewRetained(context.Background(), changed)
			if err != nil {
				t.Fatal(err)
			}
			problems := test.problemIn(preview)
			if len(problems) == 0 {
				t.Fatalf("the preview reported no problem: %+v", preview)
			}
			if test.says != "" && !strings.Contains(strings.Join(problems, " "), test.says) {
				t.Fatalf("the preview did not name the problem: %+v", preview)
			}
			if len(preview.Problems) == 0 {
				t.Fatal("the input's own problem did not surface in the preview's one problem list")
			}
			if _, err := report.Assemble(context.Background(), changed, filepath.Join(t.TempDir(), "refused")); err == nil {
				t.Fatal("assembly accepted an input the preview refused")
			}
		})
	}
}

// Preview and assembly admit a request through one check, so the packet's
// aggregate bounds refuse at preview what they refuse at assembly. Every input
// here verifies on its own — a case, the exact specification and a finalized
// execution of that case — but together they hold more files than one packet
// does, which only the aggregate bound sees.
func TestPreviewRetainedAppliesAssemblysAggregateBounds(t *testing.T) {
	t.Parallel()
	input := refusedConnectionRun(t, 250, 1)

	preview, err := report.PreviewRetained(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for what, view := range map[string]*report.RetainedInputView{"case": preview.Case, "spec": preview.Spec, "current": preview.Current} {
		if view == nil || !view.Found || len(view.Problems) != 0 {
			t.Fatalf("the %s did not verify on its own: %+v", what, view)
		}
	}
	_, assembleErr := report.Assemble(context.Background(), input, filepath.Join(t.TempDir(), "refused"))
	if assembleErr == nil {
		t.Fatal("assembly sealed a packet past its file bound")
	}
	if !slices.Contains(preview.Problems, assembleErr.Error()) {
		t.Fatalf("the preview did not report the refusal assembly makes (%q): %q", assembleErr, preview.Problems)
	}
}

// A retained execution that does not bind its case's original bytes is refused
// by the preview in the sentence assembly refuses it with. The execution here
// is a resealed copy of a real one whose two messages' retained bytes trade
// places: every file, digest and identity of it verifies on its own, and it
// names the case and specification it was run from, but the bytes it retained
// for each case occurrence are the other occurrence's.
func TestPreviewRetainedAppliesAssemblysOriginalPayloadCheck(t *testing.T) {
	t.Parallel()
	input := refusedConnectionRun(t, 2, 2)
	input.Current = swappedSourcesCopy(t, input.Current)

	preview, err := report.PreviewRetained(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for what, view := range map[string]*report.RetainedInputView{"case": preview.Case, "spec": preview.Spec, "current": preview.Current} {
		if view == nil || !view.Found || len(view.Problems) != 0 {
			t.Fatalf("the %s did not verify on its own: %+v", what, view)
		}
	}
	if !preview.Case.CaseMatch || !preview.Spec.SpecMatch {
		t.Fatalf("the execution does not name the selected case and specification: %+v", preview)
	}
	_, assembleErr := report.Assemble(context.Background(), input, filepath.Join(t.TempDir(), "refused"))
	if assembleErr == nil {
		t.Fatal("assembly sealed an execution that does not bind its case's original bytes")
	}
	if !slices.Contains(preview.Problems, assembleErr.Error()) {
		t.Fatalf("the preview did not report the refusal assembly makes (%q): %q", assembleErr, preview.Problems)
	}
}

// refusedConnectionRun retains a real execution of a case of the given number
// of messages, the first selected of them sent to a closed loopback port: a
// finalized execution error that verifies, with its case and its exact
// specification.
func refusedConnectionRun(t *testing.T, messages, selected int) report.RetainedInput {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	var raw []byte
	for n := range messages {
		raw = fmt.Appendf(raw, "\x0bMSH|^~\\&|TEST|LOCAL|||20260101000000||ADT^A01|C%03d|P|2.5.1\rPID|1||ID%03d\r\x1c\r", n, n)
	}
	c, err := bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Path: "messages.hl7", Data: raw, Options: hl7.Options{Format: hl7.MLLP}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Events) != messages {
		t.Fatalf("the case holds %d events, want %d", len(c.Events), messages)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096}
	write(t, filepath.Join(dir, "target.json"), marshal(t, target))
	ids := []string{}
	for _, event := range c.Events[:selected] {
		ids = append(ids, event.ID)
	}
	aa := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "Refused connection", Input: testrunner.Input{Case: "case", Messages: ids}, Target: "target.json", Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "Operator authorized test endpoint and reset"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: ids[0], Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &aa}}}}}
	specPath := filepath.Join(dir, "spec.json")
	write(t, specPath, marshal(t, spec))
	result, err := testrunner.Run(context.Background(), specPath, filepath.Join(dir, "result"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.Status != testrunner.ExecutionError {
		t.Fatalf("expected a finalized connection failure: %+v", result.Result)
	}
	return report.RetainedInput{Case: filepath.Join(dir, "case"), Spec: specPath, Current: filepath.Join(dir, "result")}
}

// swappedSourcesCopy copies a retained two-message execution with the source
// and intended bytes of its two messages exchanged — each event keeps its case
// occurrence, its outbound occurrence and its outcome — and reseals the run and
// the result, so only the comparison with the case's original bytes can tell.
func swappedSourcesCopy(t *testing.T, result string) string {
	t.Helper()
	dir := clone(t, result)
	run := filepath.Join(dir, "run")
	var manifest map[string]any
	decode(t, read(t, filepath.Join(run, "manifest.json")), &manifest)
	mappings := manifest["mappings"].([]any)
	if len(mappings) != 2 {
		t.Fatalf("the fixture execution retained %d messages, want 2", len(mappings))
	}
	first, second := mappings[0].(map[string]any), mappings[1].(map[string]any)
	first["source_sha256"], second["source_sha256"] = second["source_sha256"], first["source_sha256"]
	raw := marshal(t, manifest)
	write(t, filepath.Join(run, "manifest.json"), raw[:len(raw)-1])
	lines := strings.Split(strings.TrimSuffix(string(read(t, filepath.Join(run, "events.jsonl"))), "\n"), "\n")
	events := make([]map[string]any, len(lines))
	for i, line := range lines {
		decode(t, []byte(line), &events[i])
	}
	for _, member := range []string{"source", "intended"} {
		one, two := events[0][member].(map[string]any), events[1][member].(map[string]any)
		for _, field := range []string{"size", "sha256"} {
			one[field], two[field] = two[field], one[field]
		}
		first, second := filepath.Join(run, one["path"].(string)), filepath.Join(run, two["path"].(string))
		a, b := read(t, first), read(t, second)
		write(t, first, b)
		write(t, second, a)
	}
	events[0]["control_id_base64"], events[1]["control_id_base64"] = events[1]["control_id_base64"], events[0]["control_id_base64"]
	var rewritten []byte
	for _, event := range events {
		rewritten = append(rewritten, marshal(t, event)...)
	}
	write(t, filepath.Join(run, "events.jsonl"), rewritten)
	oldRun := strings.TrimSpace(string(read(t, filepath.Join(run, "identity.sha256"))))
	newRun := artifactdir.Identity(replay.Schema, snapshot(t, run))
	write(t, filepath.Join(run, "identity.sha256"), []byte(newRun+"\n"))
	write(t, filepath.Join(dir, "result.json"), []byte(strings.Replace(string(read(t, filepath.Join(dir, "result.json"))), oldRun, newRun, 1)))
	write(t, filepath.Join(dir, "identity.sha256"), []byte(artifactdir.Identity(testrunner.Schema, snapshot(t, dir))+"\n"))
	return dir
}

// writeSpecCopy copies the source specification with its bytes changed but
// still decodable, so its identity differs from the one the run retained.
func writeSpecCopy(t *testing.T, source, suffix string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.json")
	write(t, path, append(read(t, filepath.Join(source, "spec.json")), []byte(suffix)...))
	return path
}

func writeRawSpec(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.json")
	write(t, path, []byte(raw))
	return path
}

func incompleteCopy(t *testing.T, source string) string {
	t.Helper()
	copied := clone(t, filepath.Join(source, "post-fix"))
	os.Remove(filepath.Join(copied, "identity.sha256"))
	return copied
}

func TestRetainedRefusesCaseMismatchAndIncompleteJob(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(context.Background(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	in := report.RetainedInput{Case: filepath.Join(source, "reproducer"), Spec: filepath.Join(source, "spec.json"), Current: filepath.Join(source, "post-fix")}
	now := time.Now().UTC()
	wrong := filepath.Join(t.TempDir(), "wrong")
	if _, err := bundle.Write(wrong, []bundle.Input{{Path: "other.hl7", Data: []byte("MSH|^~\\&|OTHER|SITE|||20260101000000||ADT^A01|OTHER|P|2.5.1\r"), Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); err != nil {
		t.Fatal(err)
	}
	in.Case = wrong
	out := filepath.Join(t.TempDir(), "refused")
	if _, err := report.Assemble(context.Background(), in, out); err == nil {
		t.Fatal("accepted unrelated case")
	}
	if _, err := report.OpenRetained(context.Background(), out); err == nil {
		t.Fatal("partial output verified")
	}
	in.Case = filepath.Join(source, "reproducer")
	in.Current = clone(t, filepath.Join(source, "post-fix"))
	os.Remove(filepath.Join(in.Current, "identity.sha256"))
	if _, err := report.Assemble(context.Background(), in, filepath.Join(t.TempDir(), "incomplete")); err == nil {
		t.Fatal("accepted incomplete result")
	}
	in.Current = filepath.Join(source, "post-fix")
	if _, err := report.Assemble(context.Background(), in, filepath.Join(t.TempDir(), "recovery")); err != nil {
		t.Fatalf("new-destination recovery failed: %v", err)
	}
}
