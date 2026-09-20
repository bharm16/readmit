package durablerun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// fullDisk stands in for a volume with room for only limit more bytes of one
// file: the write that crosses it is short and fails, exactly as a full disk
// reports it, and the bytes before it stay on disk.
type fullDisk struct {
	evidenceFile
	limit   int
	written int
	observe func([]byte)
}

func (f *fullDisk) Write(b []byte) (int, error) {
	room := max(0, f.limit-f.written)
	if room < len(b) {
		n, _ := f.evidenceFile.Write(b[:room])
		f.written += n
		return n, syscall.ENOSPC
	}
	n, err := f.evidenceFile.Write(b)
	f.written += n
	if f.observe != nil {
		f.observe(b)
	}
	return n, err
}

func limitFile(t *testing.T, name string, limit int, observe func([]byte)) {
	t.Helper()
	previous := openEvidence
	openEvidence = func(root *os.Root, n string) (evidenceFile, error) {
		f, err := root.OpenFile(n, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil || n != name {
			return f, err
		}
		return &fullDisk{evidenceFile: f, limit: limit, observe: observe}, nil
	}
	t.Cleanup(func() { openEvidence = previous })
}

func fixture(t *testing.T, address string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "200ms", MessageTimeout: "2s", MaxACKBytes: 4096}
	val := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &val}}}}}
	for name, v := range map[string]any{"target.json": target, "spec.json": spec} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return filepath.Join(dir, "spec.json"), filepath.Join(dir, "job")
}

// countingPeer acknowledges every frame it reads and reports how many it read.
func countingPeer(t *testing.T) (string, <-chan int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	count := make(chan int, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			count <- 0
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r, _ := mllp.NewReader(conn, 1<<20)
		n := 0
		for {
			if _, e = r.ReadFrame(); e != nil {
				break
			}
			n++
			fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
		}
		count <- n
	}()
	return listener.Addr().String(), count
}

func frames(t *testing.T, count <-chan int) int {
	t.Helper()
	select {
	case n := <-count:
		return n
	case <-time.After(6 * time.Second):
		t.Fatal("peer connection never closed")
		return 0
	}
}

func refusesRepeatAndCleanup(t *testing.T, spec, out string, wantClean string) {
	t.Helper()
	before, err := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(context.Background(), out, spec, filepath.Join(filepath.Dir(out), "resumed")); err == nil {
		t.Fatal("resume repeated a run with an uncertain or unrecorded send")
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(out), "resumed")); err == nil {
		t.Fatal("a refused resume created an output")
	}
	cleanup, err := Clean(out)
	if wantClean != "" && (err == nil || err.Error() != wantClean) {
		t.Fatalf("cleanup %+v %v, want refusal %q", cleanup, err, wantClean)
	}
	if wantClean == "" && (err != nil || len(cleanup.Removed) != 0) {
		t.Fatalf("cleanup %+v %v", cleanup, err)
	}
	after, _ := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if !bytes.Equal(before, after) {
		t.Fatal("recovery, resume or cleanup changed the journal")
	}
}

func TestDiskFullDuringPayloadWriteHaltsSendsAndRecordsHowTheRunStopped(t *testing.T) {
	address, count := countingPeer(t)
	spec, out := fixture(t, address)
	limitFile(t, "sent/o000001.bin", 8, nil)
	got, err := Start(context.Background(), spec, out)
	if err == nil || err.Error() != "cannot sync durable evidence; partial evidence retained" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.State != DeliveryUncertain || got.StopReason != ExecutionError || !got.DeliveryUncertain || got.JournalIncomplete || got.Recorded != 0 {
		t.Fatalf("%+v", got)
	}
	if n := frames(t, count); n != 1 {
		t.Fatalf("peer read %d frames, want the one in flight", n)
	}
	partial, err := os.ReadFile(filepath.Join(out, "sent", "o000001.bin"))
	if err != nil || len(partial) != 8 {
		t.Fatalf("short sent prefix not retained: %d %v", len(partial), err)
	}
	recovery, err := Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Terminal || recovery.Run.State != DeliveryUncertain || recovery.Run.StopReason != ExecutionError || recovery.Uncertain != 1 || recovery.Acknowledged != 0 || recovery.SafeToRepeat || recovery.Lease != LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
	if recovery.ResumeRefusal != "an intent was synced without an acknowledged outcome; that send is never repeated" {
		t.Fatalf("%q", recovery.ResumeRefusal)
	}
	refusesRepeatAndCleanup(t, spec, out, "")
	if _, err := os.ReadFile(filepath.Join(out, "sent", "o000001.bin")); err != nil {
		t.Fatal("cleanup removed a partial sent prefix")
	}
}

func TestDiskFullDuringJournalWriteStopsBeforeTheSend(t *testing.T) {
	address, count := countingPeer(t)
	spec, out := fixture(t, address)
	// Room for ready and running; the intent record is torn.
	limitFile(t, "journal.jsonl", 320, nil)
	got, err := Start(context.Background(), spec, out)
	if err == nil || err.Error() != "cannot sync durable journal; execution stopped" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.State != DeliveryUncertain || got.StopReason != ExecutionError || !got.JournalIncomplete {
		t.Fatalf("%+v", got)
	}
	if n := frames(t, count); n != 0 {
		t.Fatalf("peer read %d frames after a failed intent", n)
	}
	journal, _ := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if len(journal) != 320 || bytes.Count(journal, []byte{'\n'}) != 2 || !bytes.Contains(journal, []byte(`"kind":"running"`)) {
		t.Fatalf("readable prefix not retained: %d bytes", len(journal))
	}
	recovery, err := Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Terminal || recovery.Run.State != DeliveryUncertain || recovery.Run.StopReason != Interrupted || !recovery.Run.JournalIncomplete || recovery.Uncertain != 1 || recovery.SafeToRepeat || recovery.Lease != LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
	refusesRepeatAndCleanup(t, spec, out, "completion was not recorded; the writer may still hold its lease and nothing was removed")
}

func TestJournalLimitRefusesTheIntentBeforeAnyByte(t *testing.T) {
	address, count := countingPeer(t)
	spec, out := fixture(t, address)
	previous := journalLimit
	journalLimit = 350
	t.Cleanup(func() { journalLimit = previous })
	got, err := Start(context.Background(), spec, out)
	if !errors.Is(err, errJournalLimit) {
		t.Fatalf("%+v %v", got, err)
	}
	if got.State != ExecutionError || got.DeliveryUncertain || !got.JournalIncomplete {
		t.Fatalf("%+v", got)
	}
	if n := frames(t, count); n != 0 {
		t.Fatalf("peer read %d frames after the journal limit", n)
	}
	recovery, err := Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Terminal || recovery.Run.State != Interrupted || recovery.Run.JournalIncomplete || recovery.NotAttempted != 1 || recovery.SafeToRepeat || recovery.Lease != LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
	refusesRepeatAndCleanup(t, spec, out, "completion was not recorded; the writer may still hold its lease and nothing was removed")
}

func TestCancellationBetweenIntentAndSendIsRecordedUncertain(t *testing.T) {
	address, count := countingPeer(t)
	spec, out := fixture(t, address)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limitFile(t, "journal.jsonl", 1<<20, func(b []byte) {
		if bytes.Contains(b, []byte(`"kind":"intent"`)) {
			cancel()
		}
	})
	got, err := Start(ctx, spec, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != DeliveryUncertain || got.StopReason != Cancelled || !got.DeliveryUncertain || got.JournalIncomplete {
		t.Fatalf("%+v", got)
	}
	if n := frames(t, count); n != 0 {
		t.Fatalf("peer read %d frames after cancellation", n)
	}
	recovery, err := Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Terminal || recovery.Run.StopReason != Cancelled || recovery.Uncertain != 1 || recovery.Occurrences[0].Delivery != Uncertain || recovery.SafeToRepeat || recovery.Lease != LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
	refusesRepeatAndCleanup(t, spec, out, "")
}

// The state vocabulary is the one classification every consumer reads. A
// change to what terminal or decided means must be a change here, not a
// drift between recovery, the queue, comparisons and reports.
func TestStateVocabularyIsTheOneClassification(t *testing.T) {
	terminal := map[State]bool{
		Ready: false, Running: false, Interrupted: false, DeliveryUncertain: false,
		Passed: true, AssertionFailed: true, ExecutionError: true, Cancelled: true, TimedOut: true,
	}
	decided := map[State]bool{
		Ready: false, Running: false, Interrupted: false, DeliveryUncertain: false,
		Cancelled: false, TimedOut: false,
		Passed: true, AssertionFailed: true, ExecutionError: true,
	}
	for state, want := range terminal {
		if got := state.Terminal(); got != want {
			t.Fatalf("Terminal(%q)=%t", state, got)
		}
	}
	for state, want := range decided {
		if got := state.Decided(); got != want {
			t.Fatalf("Decided(%q)=%t", state, got)
		}
	}
}

func TestSummaryUsabilityClassifiesInConsumerOrder(t *testing.T) {
	usable := Summary{State: Passed, ResultIdentity: "result"}
	classifications := []struct {
		name     string
		summary  Summary
		expected Usability
	}{
		{"certain result", usable, UsableResult},
		{"no finalized result", Summary{State: Passed}, UsabilityNoResult},
		{"journal incomplete wins over the state it records", Summary{State: Passed, ResultIdentity: "result", JournalIncomplete: true}, UsabilityJournalIncomplete},
		{"delivery uncertain", Summary{State: Passed, ResultIdentity: "result", DeliveryUncertain: true}, UsabilityDeliveryUncertain},
		{"no verdict", Summary{State: Cancelled, ResultIdentity: "result"}, UsabilityUndecided},
		{"uncertain state carries no verdict", Summary{State: DeliveryUncertain, ResultIdentity: "result"}, UsabilityUndecided},
	}
	for _, classification := range classifications {
		if got := classification.summary.Usability(); got != classification.expected {
			t.Fatalf("%s: got %d want %d", classification.name, got, classification.expected)
		}
	}
}
