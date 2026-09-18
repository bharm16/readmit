package tests

import (
	"bytes"
	"context"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The sample walkthrough is the scripted evaluation the site and
// samples/synthetic-walkthrough/README.md describe. It is executed here exactly
// as an evaluator runs it, against the built executable, so the published
// procedure cannot rot away from the product.

const walkthroughWorkspace = "readmit-walkthrough"

func walkthroughCommand(t *testing.T, ctx context.Context, parent, fixtures string) *exec.Cmd {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("the sample walkthrough is a POSIX shell script; no sh on this machine")
	}
	script, err := filepath.Abs("../samples/synthetic-walkthrough/walkthrough.sh")
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err = filepath.Abs(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, sh, script, walkthroughWorkspace)
	cmd.Dir = parent
	cmd.Env = append(os.Environ(), "READMIT="+binary, "FIXTURES="+fixtures)
	return cmd
}

func runWalkthrough(t *testing.T, parent, fixtures string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := walkthroughCommand(t, ctx, parent, fixtures)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("walkthrough timed out")
	}
	return stdout.String(), stderr.String(), err
}

// publishedTranscript returns every line of the walkthrough transcript quoted
// on the samples page, so the page can only quote what the product printed.
func publishedTranscript(t *testing.T) []string {
	t.Helper()
	page, err := os.ReadFile("../site/samples.html")
	if err != nil {
		t.Fatal(err)
	}
	_, after, found := strings.Cut(string(page), "<pre><code>== 2. synth")
	block, _, _ := strings.Cut(after, "</code></pre>")
	if !found || block == "" {
		t.Fatal("samples.html no longer quotes the walkthrough transcript")
	}
	var lines []string
	for _, line := range strings.Split("== 2. synth"+html.UnescapeString(block), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestSyntheticWalkthroughCompletesEveryStepAgainstTheBuiltExecutable(t *testing.T) {
	parent := t.TempDir()
	stdout, stderr, err := runWalkthrough(t, parent, "../testdata/fixtures")
	if err != nil || stderr != "" {
		t.Fatalf("walkthrough: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, stdout)
	}
	for _, want := range append([]string{
		"== 1. inspect", "Format: mllp (declared)", "Messages: 2",
		// The frozen v1 identities of docs/synth-v1-vector.md, never refreshed from output.
		"regression: 7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
		"cancellation: 96077b34226faa19325f01f3fbb0d728de88644c22ba8428659c883703d3f438",
		"invalid: ab6d014aa0fc9e2ed9cba7160e73bba17b8753f6a7ca5c3a8618f3ec8ba095a9",
		"Provenance: generated", "Provenance: imported",
		"Retention: states", "Matches: 2",
		"Diagnosis complete: 0 findings", "Diagnosis complete: 1 findings",
		"Local validation: no connection opened, no verdict or result artifact produced",
		"Baseline: assertion_failure; post-fix: pass",
		"Packet verified:",
		"Walkthrough complete: " + walkthroughWorkspace,
	}, publishedTranscript(t)...) {
		if !strings.Contains(stdout, want) {
			t.Errorf("walkthrough output missing %q", want)
		}
	}
	workspace := filepath.Join(parent, walkthroughWorkspace)
	written := synthTree(t, workspace)
	for _, artifact := range []string{
		"family/family.json", "family/regression/manifest.json", "family/invalid/manifest.json",
		"test-case/manifest.json", "regression.index.json",
		"regression-diagnosis/report.json", "regression-diagnosis/report.md",
		"invalid-diagnosis/report.json", "invalid-diagnosis/report.md",
		"regression-vs-invalid.md", "packet/identity.sha256", "packet/SUMMARY.md", "packet/RERUN.md",
	} {
		if len(written[artifact]) == 0 {
			t.Errorf("walkthrough did not write %s", artifact)
		}
	}
	if !bytes.Contains(written["regression-diagnosis/report.json"], []byte(`"findings":[]`)) || !bytes.Contains(written["invalid-diagnosis/report.json"], []byte(`"rule_id":"siu.booking-not-observed"`)) {
		t.Error("the regression case is not clean, or the invalid case's finding is not the documented hypothesis")
	}
	if diff := written["regression-vs-invalid.md"]; !bytes.Contains(diff, []byte("Paired=2; changed=1; unchanged=1")) || !bytes.Contains(diff, []byte("Filler Appointment ID: changed")) {
		t.Errorf("diff did not isolate the one changed filler identifier:\n%s", diff)
	}
	// Nothing the walkthrough writes records where it ran, and the console
	// names no loopback address or synthetic identifier.
	for name, data := range written {
		if bytes.Contains(data, []byte(parent)) {
			t.Errorf("%s records the workspace location", name)
		}
	}
	for _, private := range []string{parent, "127.0.0.1", "SYNTH-0000000000000000"} {
		if strings.Contains(stdout, private) {
			t.Errorf("walkthrough console disclosed %q", private)
		}
	}
}

func TestSyntheticWalkthroughNeverOverwritesAndItsPacketRefusesTampering(t *testing.T) {
	parent := t.TempDir()
	if stdout, stderr, err := runWalkthrough(t, parent, "../testdata/fixtures"); err != nil {
		t.Fatalf("first walkthrough: %v %s %s", err, stdout, stderr)
	}
	workspace := filepath.Join(parent, walkthroughWorkspace)
	before := synthTree(t, workspace)
	stdout, _, err := runWalkthrough(t, parent, "../testdata/fixtures")
	if err == nil || strings.Contains(stdout, "Synthetic SIU family") {
		t.Fatal("a second walkthrough ran into the existing workspace")
	}
	if !reflect.DeepEqual(before, synthTree(t, workspace)) {
		t.Fatal("a refused second walkthrough changed the first one's artifacts")
	}

	packet := filepath.Join(workspace, "packet")
	summary := filepath.Join(packet, "SUMMARY.md")
	original := before["packet/SUMMARY.md"]
	if err := os.WriteFile(summary, append(bytes.Clone(original), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "report", "verify", packet)
	if err == nil || strings.Contains(stdout, "Packet verified") {
		t.Fatalf("an altered packet verified: %v %s", err, stdout)
	}
	if strings.Contains(stdout+stderr, parent) {
		t.Fatal("verification diagnostic disclosed the packet location")
	}
	if err := os.WriteFile(summary, original, 0600); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, err := run(t, "report", "verify", packet); err != nil || !strings.Contains(stdout, "Packet verified") {
		t.Fatalf("restored packet did not verify: %v %s %s", err, stdout, stderr)
	}
}

func TestSyntheticWalkthroughCreatesNothingWhenItsInputsAreMissing(t *testing.T) {
	parent := t.TempDir()
	stdout, _, err := runWalkthrough(t, parent, filepath.Join(parent, "no-such-fixtures"))
	if err == nil || strings.Contains(stdout, "== 1.") {
		t.Fatal("walkthrough started without its fixtures")
	}
	if _, err := os.Stat(filepath.Join(parent, walkthroughWorkspace)); !os.IsNotExist(err) {
		t.Fatal("walkthrough created a workspace it could not fill")
	}
}
