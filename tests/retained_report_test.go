package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedReportCLIAssemblesAndVerifiesOffline(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source-private-path")
	output := filepath.Join(dir, "packet-private-path")
	if out, diag, err := run(t, "report", "--scenario", "siu-reschedule-v1", "--output", source); err != nil {
		t.Fatalf("fixture: %s %s %v", out, diag, err)
	}
	out, diag, err := run(t, "report", "assemble", "--case", filepath.Join(source, "reproducer"), "--spec", filepath.Join(source, "spec.json"), "--current", filepath.Join(source, "post-fix"), "--output", output)
	if err != nil || diag != "" || !strings.Contains(out, "Baseline present: false") {
		t.Fatalf("assemble: %s %s %v", out, diag, err)
	}
	if strings.Contains(out+diag, "private-path") || strings.Contains(out+diag, "SYNTH-") {
		t.Fatal("console leaked values or paths")
	}
	if out, diag, err := run(t, "report", "verify-retained", output); err != nil || !strings.Contains(out, "verified") || diag != "" {
		t.Fatalf("verify: %s %s %v", out, diag, err)
	}
	if out, _, err := run(t, "report", "assemble", "--case", filepath.Join(source, "reproducer"), "--output", output); err == nil || out != "" {
		t.Fatal("accepted missing result/spec")
	}
}
