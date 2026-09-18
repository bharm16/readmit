package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
)

func TestCaptureAndTimelineExposeGapsWithoutDisclosingEvidence(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "case")
	stdout, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", destination)
	if err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	for _, want := range []string{"Schema: readmit-case/v1", "Occurrences: 8", "Messages: 4", "ACKs: 3", "Unparsed: 1", "Matched ACKs: 1", "Unmatched ACKs: 1", "Ambiguous ACKs: 1", "Unacknowledged messages: 3", "Unknown observed times: 8"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing summary %q: %s", want, stdout)
		}
	}
	for _, secret := range []string{"SYNTH", "DUP", "ORPHAN", "case-evidence.mllp", destination} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("capture disclosed %q", secret)
		}
	}
	stdout, stderr, err = run(t, "timeline", destination)
	if err != nil || stderr != "" {
		t.Fatalf("timeline: %v %s", err, stderr)
	}
	for _, want := range []string{"observed=unknown", `declared="20260102120200"`, "declared=unknown (empty)", "declared=unknown (unparsed)", "imported=", "ambiguous_ack", "unmatched_ack", "unacknowledged_message"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing timeline evidence %q: %s", want, stdout)
		}
	}
	for _, secret := range []string{"SYNTH", "DUP", "ORPHAN", "case-evidence.mllp", "raw="} {
		if strings.Contains(stdout, secret) {
			t.Errorf("timeline disclosed %q", secret)
		}
	}
	stdout, stderr, err = run(t, "timeline", destination, "--show-values")
	if err != nil || stderr != "" || !strings.Contains(stdout, "SYNTH-CASE") || !strings.Contains(stdout, `\x00\xffSYNTH-INVALID`) {
		t.Fatalf("explicit bytes were not escaped losslessly: %v %s %s", err, stderr, stdout)
	}
	b, err := bundle.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	var actual []byte
	for _, event := range b.Events {
		raw, _ := b.Raw(event.ID)
		actual = append(actual, raw...)
	}
	want, err := os.ReadFile("../testdata/fixtures/case-evidence.mllp")
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("CLI import lost source bytes")
	}
	stdout, stderr, err = run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", destination)
	if err == nil || stdout != "" || !strings.Contains(stderr, "destination must be new") {
		t.Fatal("capture overwrote existing bundle")
	}
}

func TestCaptureExplicitObservationsKeepThreeTimesIndependent(t *testing.T) {
	dir := t.TempDir()
	metadata := filepath.Join(dir, "observations.json")
	if err := os.WriteFile(metadata, []byte(`{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":4,"direction":"outbound","observed_at":"2026-01-02T12:05:00Z"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "case")
	_, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", destination, "--metadata", metadata)
	if err != nil || stderr != "" {
		t.Fatalf("capture metadata: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "timeline", destination)
	if err != nil || stderr != "" {
		t.Fatalf("timeline: %v %s", err, stderr)
	}
	for _, expected := range []string{"Unknown observed times: 7", "direction=outbound", `observed=2026-01-02T12:05:00Z declared="20260102120200" imported=`} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("independent observation missing: %q", expected)
		}
	}
}

func TestCaptureErrorsArePrivateAndCreateNoPartialArtifact(t *testing.T) {
	dir := t.TempDir()
	for i, metadata := range []string{
		`{"schema":"readmit-capture/v1","secret":"SECRET-PATIENT"}`,
		`{"schema":"readmit-capture/v1","observations":[{"source":2,"sequence":1}]}`,
		`{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":99}]}`,
		`{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":1,"direction":"SECRET"}]}`,
		`{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":1,"observed_at":"SECRET"}]}`,
		`{"schema":"readmit-capture/v1","observations":[{"source":1,"sequence":1},{"source":1,"sequence":1}]}`,
	} {
		path := filepath.Join(dir, "SECRET-METADATA.json")
		if err := os.WriteFile(path, []byte(metadata), 0600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(t.TempDir(), "case")
		stdout, stderr, err := run(t, "capture", "../testdata/fixtures/adt-cr.hl7", "--output", output, "--metadata", path)
		if err == nil || stdout != "" || len(stderr) == 0 || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, dir) {
			t.Errorf("unsafe metadata error %d: %v %q %q", i, err, stdout, stderr)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid input created a partial artifact")
		}
	}
	for _, args := range [][]string{
		{"capture"}, {"capture", "SECRET-MISSING", "--output", filepath.Join(dir, "new")},
		{"capture", "../testdata/fixtures/adt-cr.hl7"},
		{"capture", dir, "--output", filepath.Join(dir, "new")},
		{"capture", "../testdata/fixtures/adt-cr.hl7", "--format", "SECRET", "--output", filepath.Join(dir, "new")},
		{"timeline", "SECRET-MISSING"}, {"timeline"}, {"timeline", dir},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil || stdout != "" || len(stderr) == 0 || len(stderr) > 300 || strings.Contains(stderr, "SECRET") || strings.Contains(stderr, dir) {
			t.Errorf("unsafe command error: %v %q %q", err, stdout, stderr)
		}
	}
}

func TestTimelineHidesArbitraryMSH7AndRejectsTamperedEvidence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "SECRET-PATH.hl7")
	if err := os.WriteFile(file, []byte("MSH|^~\\&|||||SECRET-PATIENT||ADT^A08|SECRET-ID|P|2.5.1\r"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "case")
	if _, stderr, err := run(t, "capture", file, "--output", output); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "timeline", output)
	if err != nil || strings.Contains(stdout+stderr, "SECRET") || !strings.Contains(stdout, "declared=uninterpreted") {
		t.Fatal("default timeline disclosed non-timestamp MSH-7")
	}
	payload := filepath.Join(output, "payloads", "s0001-e000001.bin")
	if err := os.WriteFile(payload, []byte("SECRET-TAMPERED"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(t, "timeline", output, "--show-values")
	if err == nil || stdout != "" || strings.Contains(stderr, "SECRET") {
		t.Fatal("timeline rendered unverified evidence")
	}
}
