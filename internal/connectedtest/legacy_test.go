package connectedtest_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestLegacyAdapterExecutesExactInputsAndReopensOffline(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "cancelled"}[cancelled], func(t *testing.T) { testLegacy(t, cancelled) })
	}
}
func testLegacy(t *testing.T, cancelled bool) {
	dir := t.TempDir()
	raw, files := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: files["book.hl7"], Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "readmit-test-fixture-v1", ProfileVersion: "readmit-siu-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: "readmit-target/v3", TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "100ms", MessageTimeout: "1s", MaxACKBytes: 4096, Name: "local-fixture", Classification: replay.Nonproduction}
	writeJSON(t, filepath.Join(dir, "target.json"), target)
	aa := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "Independent ACK fixture", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: d.Setup.Instructions}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "legacy-accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &aa}}}}}
	specPath := filepath.Join(dir, "spec.json")
	writeJSON(t, specPath, spec)
	legacy, err := testrunner.Prepare(specPath)
	if err != nil {
		t.Fatal(err)
	}
	d.Environment.Name = "local-fixture"
	d.Environment.TargetIdentity = legacy.Target().Identity()
	d.Environment.Classification = "nonproduction"
	d.Environment.AddressPolicyIdentity = connectedtest.Digest([]byte("readmit-legacy-loopback-policy/v1"))
	d.Datasets[0].Completion.Kind = "ack-responses"
	converted, err := connectedtest.ConvertLegacy(specPath, "lab", "converted", "2", files["checks.json"], connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if converted.Document().Test.Ancestry == nil || converted.Document().Test.Ancestry.SHA256 != connectedtest.Digest(legacy.PinnedInputs().Spec) {
		t.Fatal("explicit conversion lost original ancestry")
	}
	prepared := filepath.Join(dir, "prepared")
	if err := converted.Write(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if _, err := connectedtest.OpenPlan(prepared); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		wire, err := reader.ReadFrame()
		if err == nil && !bytes.Equal(wire, mllp.Frame(files["book.hl7"])) {
			err = os.ErrInvalid
		}
		if cancelled {
			time.Sleep(250 * time.Millisecond)
			done <- nil
			return
		}
		if err == nil {
			_, err = conn.Write(mllp.Frame([]byte("MSH|^~\\&|TARGET|LAB|||20260101120000||ACK^S12|ACK1|P|2.5.1\rMSA|AA|DUPLICATE\r")))
		}
		done <- err
	}()
	deadline := 5 * time.Second
	if cancelled {
		deadline = 100 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	out := filepath.Join(dir, "execution")
	result, err := connectedtest.ExecuteLegacy(ctx, p, specPath, "run-one", out)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if (!cancelled && result.Document().Verdict != "pass") || (cancelled && result.Document().Verdict == "pass") || len(result.Document().Execution.Attempts) != 1 {
		t.Fatal("fixture did not execute and decide independent checks")
	}
	_ = listener.Close()
	// Original inputs disappear; only the self-contained result remains.
	if err := os.Rename(filepath.Join(dir, "case"), filepath.Join(dir, "unavailable-case")); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectedtest.OpenResult(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Document().Verdict != result.Document().Verdict {
		t.Fatal("offline verdict changed")
	}
}
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
