package tests

import (
	"context"
	"encoding/json/v2"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observesource"
)

// The window and source below are authored here rather than produced by the
// engine, so the collector is exercised against text an operator could have
// written. Both name the same downstream system and scope: a collector handed a
// window over some other source reports unsupported rather than observing
// something else.
const captureWindowDocument = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "integration-sink", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "3s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}
}`

const captureSourceDocument = `{
  "schema": "readmit-observation-source/v2",
  "source": {"kind": "downstream-capture", "identity": "integration-sink", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "1h"},
  "extraction": null,
  "file": null,
  "http": null,
  "capture": {"path": "downstream.case", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}
}`

// producedBooking is what the run sends downstream. SCH-1.1 carries the key the
// observation is declared to read back out of whatever the downstream system
// was actually sent.
const producedBooking = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120000||SIU^S12|OBSERVE-001|P|2.5.1\rSCH|APPT-7710|FILLER-7710\r"

// captureDocuments writes one window and one source beside the capture the
// source names, and returns the directory they share.
func captureDocuments(t *testing.T) (directory, window, source string) {
	t.Helper()
	directory = t.TempDir()
	window = filepath.Join(directory, "window.json")
	source = filepath.Join(directory, "source.json")
	for path, document := range map[string]string{window: captureWindowDocument, source: captureSourceDocument} {
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory, window, source
}

// sealDownstreamCapture runs the generic collector as the downstream sink of a
// real exchange: something sends it HL7 the way it always would, and readmit
// retains what it was sent. Nothing about this asks the sender to produce a
// readmit-specific receipt.
func sealDownstreamCapture(t *testing.T, directory string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	capture := startCapture(t, ctx, "--address", "127.0.0.1:0",
		"--policy", policyFile(t, directory, collectAnyPolicy),
		"--output", filepath.Join(directory, "downstream.case"),
		"--max-messages", "1", "--idle-timeout", "3s")
	connection, err := net.DialTimeout("tcp", capture.address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(mllp.Frame([]byte(producedBooking))); err != nil {
		t.Fatal(err)
	}
	reader, _ := mllp.NewReader(connection, 1<<20)
	if _, err := reader.ReadFrame(); err != nil {
		t.Fatalf("the downstream sink did not answer: %v", err)
	}
	_, _ = io.ReadAll(capture.stdout)
	if err := capture.command.Wait(); err != nil {
		t.Fatalf("collect: %v %s", err, capture.diagnostic.String())
	}
}

// TestObserveCollectBindsDownstreamMessageEvidenceToWhatTheRunProduced is the
// whole point of this collector: a regression assertion inspects the system
// that was actually under test, through the evidence readmit captured of what
// that system was sent, instead of a readmit-only ledger the fixture receiver
// exports.
func TestObserveCollectBindsDownstreamMessageEvidenceToWhatTheRunProduced(t *testing.T) {
	directory, window, source := captureDocuments(t)
	sealDownstreamCapture(t, directory)
	record := filepath.Join(directory, "completion.json")
	snapshot := filepath.Join(directory, "snapshot")
	stdout, stderr, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", snapshot, "--produced", "APPT-7710", "--produced", "APPT-0000")
	if err != nil {
		t.Fatalf("a completed collection was refused: %v %s", err, stderr)
	}
	for _, expected := range []string{"Status: complete", "Boundary: observation-window",
		"kind downstream-capture", "Records observed: 1", "Correlations: 2 recorded, 1 matched",
		"Absence assertion: supported by this completion"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("collection did not report %q:\n%s", expected, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("a completed collection wrote diagnostics: %q", stderr)
	}
	// The snapshot names the retained evidence the observation read rather than
	// copying it: the capture is already the original, and readmit keeps one.
	retained, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(directory, "downstream.case", "identity.sha256"))
	if err != nil || string(retained) != string(marker) {
		t.Fatalf("the retained material does not name the capture that was read: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "body")); err == nil {
		t.Fatalf("the snapshot holds a second copy of the captured messages: %d bytes", len(body))
	}
	// A capture sealed by a live collector states the bytes of evidence the
	// read covered, from that capture's own manifest. Zero is reserved for a
	// capture that retained nothing, and this one retained a message.
	data, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var read observesource.Evidence
	if err := json.Unmarshal(data, &read, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("a retained read is not one strict %s record: %v", observesource.EvidenceSchema, err)
	}
	if read.Kind != observesource.DownstreamCapture || read.Bytes <= 0 || read.Records != 1 {
		t.Fatalf("the retained read does not state what it covered: %+v", read)
	}
	// The capture still verifies afterwards, because observing it read it.
	if _, stderr, err := run(t, "timeline", filepath.Join(directory, "downstream.case")); err != nil {
		t.Fatalf("the observed capture no longer verifies: %v %s", err, stderr)
	}
	// The retained record re-decides to the same verdict against its window.
	if _, _, err := run(t, "observe", "explain", record, "--window", window); err != nil {
		t.Fatalf("the retained completion did not re-decide to its own verdict: %v", err)
	}
}

// A capture that was never sealed observed nothing, and nothing is not an
// observation that the downstream system received nothing.
func TestObserveCollectNeverReadsAnUnsealedCaptureAsAbsence(t *testing.T) {
	directory, window, source := captureDocuments(t)
	record := filepath.Join(directory, "completion.json")
	stdout, _, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", filepath.Join(directory, "snapshot"))
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an unsealed capture exited %d, want the observation error status 2", code)
	}
	for _, expected := range []string{"Status: missing", "Absence assertion: not supported",
		"Records observed: not settled", "Durable run state: execution_error"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("an unsealed capture did not report %q:\n%s", expected, stdout)
		}
	}
}
