package report_test

import (
	"context"
	"encoding/json/v2"
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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/report"
)

func TestAssembleRetainsActualRunsAndHonestSingleRun(t *testing.T) {
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

func TestRetainedRefusesCaseMismatchAndIncompleteJob(t *testing.T) {
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
