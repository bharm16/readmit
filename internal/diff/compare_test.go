package diff_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func sourceCase(t *testing.T, fixture string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "case")
	_, err = bundle.Write(path, []bundle.Input{{Data: data}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 17, BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture-v1", ProfileVersion: "siu-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func localTarget(t *testing.T, count int, code string) (replay.Target, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(connection, 1<<20)
		for range count {
			frame, err := reader.ReadFrame()
			if err != nil {
				t.Errorf("peer request: %v", err)
				return
			}
			doc, err := hl7.Parse(frame, hl7.Options{Format: hl7.MLLP})
			if err != nil {
				t.Errorf("peer parse: %v", err)
				return
			}
			selector, _ := hl7.ParseSelector("MSH-10")
			value, _ := doc.Select(0, selector)
			id, err := hl7.Decode(doc.Bytes(value.Span), doc.Messages[0].Delimiters)
			if err != nil {
				t.Error(err)
				return
			}
			response := []byte("MSH|^~\\&|PEER|FIXTURE|SYNTHETIC|LAB|20260101120000||ACK^S12|SECRET-ACK|P|2.5.1\rMSA|" + code + "|" + string(id) + "\r")
			if code == "malformed" {
				response = []byte("MSH|BROKEN")
			}
			if _, err := connection.Write(mllp.Frame(response)); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	finish := func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("peer did not stop")
		}
	}
	t.Cleanup(finish)
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096}, finish
}

func executeRun(t *testing.T, source string, options replay.Options, count int, code string) string {
	t.Helper()
	target, finish := localTarget(t, count, code)
	plan, err := replay.Prepare(source, target, options)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := replay.Execute(ctx, plan, path); err != nil {
		t.Fatal(err)
	}
	finish()
	if _, err := replay.Open(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func compare(t *testing.T, left, right string, options diff.Options) diff.Report {
	t.Helper()
	report, err := diff.Compare(diff.Input{Path: left}, diff.Input{Path: right}, options)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestVerifiedRunMappingSurvivesRewrittenControlIDsAndSelection(t *testing.T) {
	source := sourceCase(t, "diff-before.mllp")
	rebased := executeRun(t, source, replay.Options{Transformations: []replay.Transformation{{Name: "rebase-control-ids"}}}, 2, "AA")
	report := compare(t, source, rebased, diff.Options{})
	if report.Alignment != "source-occurrence" || report.Summary.Paired != 2 || report.Summary.Changed != 2 || report.Summary.FieldChanges != 2 || len(report.Unsupported) != 0 {
		t.Fatalf("recorded source mapping not used: %+v", report)
	}
	for i, pair := range report.Pairs {
		if pair.Left.Occurrence != fmt.Sprintf("s0001-e%06d", i+1) || pair.Right.Occurrence != fmt.Sprintf("o%06d", i+1) || len(pair.Fields) != 1 || pair.Fields[0].Selector != "MSH[1]-10[1]" {
			t.Fatal("source identity replaced with regenerated control IDs")
		}
	}
	reverse := compare(t, rebased, source, diff.Options{})
	if reverse.Alignment != "source-occurrence" || reverse.Summary.FieldChanges != 2 {
		t.Fatal("reverse source mapping lost")
	}
	subset := executeRun(t, source, replay.Options{Occurrences: []string{"s0001-e000002"}, Transformations: []replay.Transformation{{Name: "rebase-control-ids"}}}, 1, "AA")
	report = compare(t, source, subset, diff.Options{})
	if report.Summary.Paired != 1 || report.Summary.Missing != 1 || report.Missing[0].Occurrence != "s0001-e000001" || report.Pairs[0].Right.SourceOccurrence != "s0001-e000002" {
		t.Fatal("unselected source occurrence or non-first mapping lost")
	}
	unchanged := executeRun(t, source, replay.Options{}, 2, "AA")
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	report = compare(t, rebased, unchanged, diff.Options{Boundary: diff.ACKs, Fields: []string{"MSA-2"}})
	if report.Alignment != "source-occurrence" || report.Summary.Paired != 2 || report.Summary.FieldChanges != 2 || report.Left.Payloads != "actual received ACK bytes" {
		t.Fatal("offline common-source ACK comparison lost")
	}
	first, _ := diff.JSON(report)
	again, _ := diff.JSON(compare(t, rebased, unchanged, diff.Options{Boundary: diff.ACKs, Fields: []string{"MSA-2"}}))
	if !bytes.Equal(first, again) {
		t.Fatal("same evidence produced nondeterministic diff")
	}
	if strings.Contains(string(first), "SECRET") || strings.Contains(string(first), "127.0.0.1") {
		t.Fatal("run report disclosed values/target address")
	}
}

func TestClaimedSourceIdentityCannotOverrideRetainedSourceBytes(t *testing.T) {
	sourceA := sourceCase(t, "diff-before.mllp")
	sourceB := sourceCase(t, "diff-after.mllp")
	runA := executeRun(t, sourceA, replay.Options{}, 2, "AA")
	runB := executeRun(t, sourceB, replay.Options{}, 3, "AA")
	b, err := bundle.Open(sourceB)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(runA, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest replay.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.SourceBundleIdentity = b.Identity
	data, err = json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	rehashRun(t, runA)
	// The independent run remains internally consistent; validating this source
	// relationship requires the compared source's actual stored payloads.
	if _, err := replay.Open(runA); err != nil {
		t.Fatalf("forgery did not reach cross-artifact check: %v", err)
	}
	for _, right := range []string{sourceB, runB} {
		if _, err := diff.Compare(diff.Input{Path: runA}, diff.Input{Path: right}, diff.Options{Keys: []string{"MSH-10"}}); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("contradictory source claim accepted or silently matched by key")
		}
	}
}

// Independently implements the published run identity framing, so the above
// test reaches semantic mapping verification instead of only a hash failure.
func rehashRun(t *testing.T, path string) {
	t.Helper()
	var names []string
	if err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(path, name)
		if err != nil {
			return err
		}
		if rel != "identity.sha256" {
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	h := sha256.New()
	h.Write([]byte("readmit-run/v1\n"))
	var size [8]byte
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		h.Write(size[:])
		h.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		h.Write(size[:])
		h.Write(data)
	}
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(hex.EncodeToString(h.Sum(nil))+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNoSentAndMalformedReceivedBytesStayUncompared(t *testing.T) {
	source := sourceCase(t, "diff-before.mllp")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	plan, err := replay.Prepare(source, replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "100ms", MessageTimeout: "100ms", MaxACKBytes: 4096}, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "refused")
	if _, err := replay.Execute(context.Background(), plan, path); err != nil {
		t.Fatal(err)
	}
	report := compare(t, source, path, diff.Options{})
	if report.Summary.Paired != 2 || report.Summary.Uncompared != 2 || len(report.Unsupported) != 2 || report.Pairs[0].Right.PayloadState != "no_payload" {
		t.Fatal("unsent messages presented as complete/equal")
	}
	malformed := executeRun(t, source, replay.Options{Occurrences: []string{"s0001-e000001"}}, 1, "malformed")
	report = compare(t, malformed, malformed, diff.Options{Boundary: diff.ACKs})
	if report.Summary.Uncompared != 1 || len(report.Unsupported) != 2 || report.Pairs[0].Left.PayloadState != "unparsed" {
		t.Fatal("malformed ACK was silently skipped")
	}
}

func TestResultDirectoryUsesVerifiedOfflineRunAndKeepsBoundary(t *testing.T) {
	source := sourceCase(t, "diff-before.mllp")
	target, finish := localTarget(t, 1, "AA")
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.json")
	data, _ := json.Marshal(target)
	if err := os.WriteFile(targetPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	aa := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "synthetic ACK contract", Input: testrunner.Input{Case: source, Messages: []string{"s0001-e000001"}}, Target: targetPath, Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "Start the local synthetic peer."}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack-code", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &aa}}}}}
	specPath := filepath.Join(dir, "spec.json")
	data, _ = json.Marshal(spec)
	if err := os.WriteFile(specPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(dir, "result")
	result, err := testrunner.Run(context.Background(), specPath, resultPath)
	if err != nil || result.Result.Status != testrunner.Pass {
		t.Fatalf("result setup: %v %+v", err, result)
	}
	finish()
	copyPath := filepath.Join(t.TempDir(), "copied-result")
	if err := os.CopyFS(copyPath, os.DirFS(resultPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	report := compare(t, copyPath, filepath.Join(copyPath, "run"), diff.Options{Boundary: diff.ACKs})
	if report.Left.Kind != "result" || report.Left.ResultStatus != "pass" || report.Left.ResultBoundary != "ack-contract" || report.Summary.Unchanged != 1 || !strings.Contains(report.Scope, "ledger correctness") {
		t.Fatal("result boundary lost or ledger success invented")
	}
	if err := os.WriteFile(filepath.Join(copyPath, "spec.json"), []byte("SECRET-CORRUPTION"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := diff.Compare(diff.Input{Path: copyPath}, diff.Input{Path: filepath.Join(copyPath, "run")}, diff.Options{}); err == nil {
		t.Fatal("corrupt result bypassed verified result reader")
	}
}
