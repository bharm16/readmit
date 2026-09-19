package runcompare_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The fixture runs real durable jobs against one synthetic loopback peer. Only
// ACK content changes, independently of the unchanged retained input and target.
func fixture(t *testing.T, codes ...string) (string, string) {
	t.Helper()
	return fixtureWithFailure(t, false, codes...)
}
func fixtureWithFailure(t *testing.T, alwaysFail bool, codes ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan error, 1)
	go func() {
		for _, code := range codes {
			conn, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			reader, _ := mllp.NewReader(conn, 1<<20)
			_, err = reader.ReadFrame()
			if err == nil {
				_, err = fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", code)
			}
			conn.Close()
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(root, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "2s", MaxACKBytes: 4096}
	text := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "synthetic", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "reset synthetic peer"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &text}}}}}
	if alwaysFail {
		text := "AR"
		second := spec.Assertions[0]
		second.ID = "persistent"
		second.Expected = testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &text}}
		spec.Assertions = append(spec.Assertions, second)
	}
	writeJSON(t, filepath.Join(root, "target.json"), target)
	specPath := filepath.Join(root, "spec.json")
	writeJSON(t, specPath, spec)
	for i := range codes {
		if _, err := durablerun.Start(context.Background(), specPath, filepath.Join(root, fmt.Sprintf("job-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return root, specPath
}
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func input(root string) runcompare.Input {
	return runcompare.Input{Baseline: filepath.Join(root, "job-0"), Current: filepath.Join(root, "job-1")}
}

func TestRetainedHistoryApprovalAndCancellation(t *testing.T) {
	root, specPath := fixture(t, "AE", "AA", "AE")
	in := input(root)
	in.Repeats = []string{filepath.Join(root, "job-2")}
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	review, err := baseline.Review(data, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := baseline.Approve(data, nil, review.Identity, "reviewer", "synthetic fixture")
	if err != nil {
		t.Fatal(err)
	}
	in.Approval = filepath.Join(root, "baseline.json")
	if err := baseline.Save(in.Approval, approved); err != nil {
		t.Fatal(err)
	}
	got, err := runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Approval != "matches_baseline_specification" || got.Stability.State != "possible_flakiness" || got.Stability.Failures != 2 || got.Stability.Passes != 1 || got.Baseline.Unobserved != 0 {
		t.Fatalf("%+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "127.0.0.1") || strings.Contains(string(raw), "LISTEN-BOOK") || strings.Contains(string(raw), root) {
		t.Fatalf("private source crossed comparison boundary: %s", raw)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runcompare.Compare(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := runcompare.Compare(context.Background(), in); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	in.Repeats = append(in.Repeats, in.Repeats[0])
	if _, err := runcompare.Compare(context.Background(), in); err == nil {
		t.Fatal("duplicate repeat counted")
	}
	in.Repeats = make([]string, 15)
	if _, err := runcompare.Compare(context.Background(), in); err == nil {
		t.Fatal("unbounded history")
	}
	// An explicit approval of different expectations never approves this run.
	text := "AR"
	approved.Spec.Assertions[0].Expected.Field.Text = &text
	changedData, _ := json.Marshal(approved.Spec)
	changedReview, _ := baseline.Review(changedData, nil, false)
	different, _ := baseline.Approve(changedData, nil, changedReview.Identity, "reviewer", "changed")
	in.Repeats = nil
	in.Approval = filepath.Join(root, "different.json")
	if err := baseline.Save(in.Approval, different); err != nil {
		t.Fatal(err)
	}
	got, err = runcompare.Compare(context.Background(), in)
	if err != nil || got.Approval != "different_specification" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestUnfinishedJournalNeverBecomesStablePass(t *testing.T) {
	root, _ := fixture(t, "AA", "AA")
	in := input(root)
	// A torn append preserves the finalized result, but completion is uncertain.
	f, err := os.OpenFile(filepath.Join(in.Current, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"partial\":"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stability.Incomplete != 1 || got.Current.RunState != "delivery_uncertain" || got.Stability.State == "no_observed_flakiness" || len(got.Current.Gaps) == 0 {
		t.Fatalf("unfinished completion became a pass: %+v", got)
	}
}

func TestMissingResultAndChangedDefinitionsStayUncompared(t *testing.T) {
	root, specPath := fixture(t, "AA", "AA")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := filepath.Join(root, "cancelled")
	if _, err := durablerun.Start(ctx, specPath, stopped); err != nil {
		t.Fatal(err)
	}
	in := input(root)
	in.Current = stopped
	got, err := runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current.RunState != "cancelled" || got.Current.Unobserved != 1 || got.Current.Unevaluated != 1 || got.Assertions[0].Behavior != "not_compared" {
		t.Fatalf("cancelled evidence inferred: %+v", got)
	}
	// A preparation failure retains an error, never an invented empty pass.
	bad := filepath.Join(root, "bad.json")
	if err := os.WriteFile(bad, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "configuration-error")
	if _, err := testrunner.Run(context.Background(), bad, out); err != nil {
		t.Fatal(err)
	}
	in.Current = out
	got, err = runcompare.Compare(context.Background(), in)
	if err != nil || got.Current.Status != "execution_error" || got.Specification != "unknown" || got.Assertions[0].Current != "unknown" {
		t.Fatalf("%+v %v", got, err)
	}
	in.Baseline, in.Current = in.Current, in.Baseline
	got, err = runcompare.Compare(context.Background(), in)
	if err != nil || got.Assertions[0].Baseline != "unknown" || got.Assertions[0].Definition != "unknown" {
		t.Fatalf("missing spec became exclusion: %+v %v", got, err)
	}
}

func TestChangedAndExcludedAssertionsAreNotBehaviorEquivalence(t *testing.T) {
	root, specPath := fixture(t, "AA", "AA")
	spec, err := testrunner.ReadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	text := "AE"
	spec.Assertions[0].Expected.Field.Text = &text
	second := spec.Assertions[0]
	second.ID = "added"
	spec.Assertions = append(spec.Assertions, second)
	writeJSON(t, specPath, spec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(root, "changed")
	if _, err := durablerun.Start(ctx, specPath, out); err != nil {
		t.Fatal(err)
	}
	in := input(root)
	in.Current = out
	got, err := runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Specification != "changed" || len(got.Assertions) != 2 || got.Assertions[0].Definition != "changed" || got.Assertions[0].Behavior != "not_compared" || got.Assertions[1].Baseline != "excluded" {
		t.Fatalf("%+v", got)
	}
	in.Baseline, in.Current = in.Current, in.Baseline
	got, err = runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assertions[1].Definition != "removed" || got.Assertions[1].Current != "excluded" || got.Assertions[1].Behavior != "not_compared" {
		t.Fatalf("removed assertion passed: %+v", got.Assertions)
	}
}

func TestAssertionFlakinessRemainsVisibleBesidePersistentFailure(t *testing.T) {
	root, _ := fixtureWithFailure(t, true, "AE", "AA")
	got, err := runcompare.Compare(context.Background(), input(root))
	if err != nil {
		t.Fatal(err)
	}
	if got.Stability.State != "possible_flakiness" || got.Stability.Passes != 0 || got.Stability.Failures != 2 || len(got.Stability.FlakyAssertions) != 1 || got.Stability.FlakyAssertions[0] != "accepted" {
		t.Fatalf("assertion flakiness lost: %+v", got.Stability)
	}
}

func TestDuplicateResultDoesNotHideUnfinishedJournal(t *testing.T) {
	root, _ := fixture(t, "AA", "AA")
	copyPath := filepath.Join(root, "copied-job")
	if err := os.CopyFS(copyPath, os.DirFS(filepath.Join(root, "job-0"))); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(copyPath, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	in := input(root)
	in.Current = copyPath
	in.Repeats = []string{filepath.Join(root, "job-1")}
	got, err := runcompare.Compare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stability.Incomplete != 1 || got.Stability.State != "unresolved" || got.Stability.Runs != 2 {
		t.Fatalf("duplicate result masked journal: %+v", got.Stability)
	}
}
