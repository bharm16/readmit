package drift_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/drift"
)

// Every input that retains none of the four causes is refused by name before
// anything is compared, and no refusal echoes the path it was handed.
func TestCompareRefusesWhatRetainsNoneOfTheFourCauses(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "messages.mllp")
	if err := os.WriteFile(file, []byte("MSH|^~\\&|A|B|C|D|20260101000000||ADT^A01|1|P|2.5.1\r"), 0600); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(dir, "unknown")
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "manifest.json"), []byte(`{"schema":"readmit-backup/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(dir, "bare")
	if err := os.Mkdir(bare, 0700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"no path":               "",
		"absent directory":      filepath.Join(dir, "absent"),
		"a message file":        file,
		"an unrelated contract": unknown,
		"no manifest at all":    bare,
	} {
		report, err := drift.Compare(path, path)
		if err == nil {
			t.Fatalf("%s produced a report: %+v", name, report)
		}
		if path != "" && strings.Contains(err.Error(), path) {
			t.Fatalf("%s echoed its path: %s", name, err)
		}
		if report.Schema != "" {
			t.Fatalf("%s produced a partial report", name)
		}
	}
}

// The three renderings are one report. Terminal and Markdown carry the same
// statements, including the causes that were not compared, and the JSON is
// byte-identical every time it is written.
func TestRenderingsAgreeAndTheDocumentIsDeterministic(t *testing.T) {
	report := drift.Report{
		Schema: drift.Schema,
		Scope:  "scope",
		Left:   drift.Side{Kind: drift.JobKind, Identity: "left-identity", Input: drift.InputSide{State: drift.Declared, Identity: "case-identity", Transformations: []string{"shift_dates"}, RecordedChanges: 3}, Target: drift.TargetSide{State: drift.Declared, Fingerprint: "target-left", Revision: drift.UnknownRevision}, Environment: drift.EnvironmentSide{State: drift.Declared, Fingerprint: "pin-left", Engine: "dev", Spec: "readmit-test/v1"}, Rule: drift.RuleSide{State: drift.Declared, Fingerprint: "pin-left", Profile: "readmit-siu-v1", Resolution: drift.BundledProfile}},
		Right:  drift.Side{Kind: drift.CaseKind, Identity: "right-identity", Input: drift.InputSide{State: drift.Declared, Identity: "case-identity", Transformations: []string{}}, Target: drift.TargetSide{State: drift.Undeclared, Revision: drift.UnknownRevision}, Environment: drift.EnvironmentSide{State: drift.Undeclared}, Rule: drift.RuleSide{State: drift.Undeclared}},
		Drift: []drift.Drift{
			{Cause: drift.InputCause, Outcome: drift.Changed, Comparison: drift.Semantic, Parts: []string{"transformations"}},
			{Cause: drift.TargetCause, Outcome: drift.Undecided, Comparison: drift.NotCompared, Parts: []string{}, Reason: drift.DeclaredOnOneSide},
			{Cause: drift.EnvironmentCause, Outcome: drift.Undecided, Comparison: drift.NotCompared, Parts: []string{}, Reason: drift.DeclaredOnOneSide},
			{Cause: drift.RuleCause, Outcome: drift.Undecided, Comparison: drift.NotCompared, Parts: []string{}, Reason: drift.DeclaredOnOneSide},
		},
		Attribution: drift.Attribution{Outcome: drift.Undecided, Changed: []string{"input"}, Unresolved: []string{"target", "environment", "rule"}},
	}
	first, err := drift.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := drift.JSON(report)
	if err != nil || !bytes.Equal(first, second) || !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatalf("drift JSON is not deterministic: %v", err)
	}
	terminal, markdown := string(drift.Terminal(report)), string(drift.Markdown(report))
	for _, statement := range []string{
		"shift_dates", "readmit-siu-v1", "bundled", "unknown",
		"input: changed", "target: undecided", "environment: undecided", "rule: undecided",
		"declared_on_one_side", "single_cause",
	} {
		expected := statement != "single_cause"
		if strings.Contains(terminal, statement) != expected {
			t.Fatalf("terminal rendering disagrees about %q:\n%s", statement, terminal)
		}
		escaped := strings.ReplaceAll(strings.ReplaceAll(statement, "_", "\\_"), "-", "\\-")
		if strings.Contains(markdown, escaped) != expected {
			t.Fatalf("markdown rendering disagrees about %q:\n%s", statement, markdown)
		}
	}
	if !strings.Contains(markdown, "## Drift by cause") || strings.Contains(terminal, "## ") {
		t.Fatal("the two renderings do not differ only in markup")
	}
}
