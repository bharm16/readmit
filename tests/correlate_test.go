package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
)

const correlateRules = "../testdata/fixtures/correlate-rules.json"

// twoSourceCase captures the acceptance fixture twice. Importing one file
// twice is two independent sources, which is what a rule crossing a declared
// source boundary needs to be demonstrated on.
func twoSourceCase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	evidence := "../testdata/fixtures/case-evidence.mllp"
	if _, stderr, err := run(t, "capture", evidence, evidence, "--output", path); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return path
}

func TestCorrelateExecutableLinksDeclaredScopesAndRefusesToMergeCollisions(t *testing.T) {
	path := twoSourceCase(t)
	before, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "correlate", path, "--rules", correlateRules)
	if err != nil || stderr != "" {
		t.Fatalf("correlate: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Contract: readmit-correlation/v1",
		"Links: 7 (2 observed, 5 inferred)",
		"Collisions: 4",
		"reason=ambiguous_acknowledgement",
		"reason=duplicate_control_id",
		"reason=unqualified_identifier",
		"unparsed_occurrence",
		"unknown_assigning_authority",
		"Colliding identifiers were never merged.",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in\n%s", want, stdout)
		}
	}

	jsonOut, stderr, err := run(t, "correlate", path, "--rules", correlateRules, "--format", "json")
	if err != nil || stderr != "" {
		t.Fatalf("correlate json: %v %s", err, stderr)
	}
	report := readStrictOutput[correlate.Report](t, jsonOut)
	if len(report.Links) != 7 || len(report.Collisions) != 4 || len(report.Unsupported) != 4 {
		t.Fatalf("report disagrees with the rendered summary: %+v", report.Summary)
	}
	// An acknowledgement's own declaration is observed linkage; equal keys are
	// inferred. The two are never reported as the same thing.
	for _, link := range report.Links {
		switch link.Rule {
		case "acknowledgements":
			if link.Linkage != correlate.Observed {
				t.Fatalf("a declared acknowledgement was not observed linkage: %+v", link)
			}
		case "same-message":
			if link.Linkage != correlate.Inferred {
				t.Fatalf("equal control IDs claimed observed linkage: %+v", link)
			}
		}
	}
	for _, rule := range report.Rules {
		if !rule.Applied {
			t.Fatalf("a rule over a source the case has did not run: %+v", rule)
		}
	}

	// No message content, no source path and no filename reaches any output.
	for _, secret := range []string{"SYNTH-CASE", "SYNTH-INVALID", "ACK-ORPHAN", "case-evidence", path, correlateRules} {
		if strings.Contains(stdout+jsonOut+stderr, secret) {
			t.Fatalf("correlation disclosed %q", secret)
		}
	}
	after, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil || string(after) != string(before) {
		t.Fatal("correlation changed the case it read")
	}
}

func TestCorrelateWritesOneNewPrivateFileAndNeverIntoTheCase(t *testing.T) {
	path := twoSourceCase(t)
	output := filepath.Join(t.TempDir(), "correlation.json")
	if _, stderr, err := run(t, "correlate", path, "--rules", correlateRules, "--format", "json", "--output", output); err != nil {
		t.Fatalf("correlate --output: %v %s", err, stderr)
	}
	// The written report must itself be a strict readmit-correlation/v1 document.
	readStrictDocument[correlate.Report](t, output)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(output)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("correlation output permissions are not private")
		}
	}
	if stdout, stderr, err := run(t, "correlate", path, "--rules", correlateRules, "--output", output); err == nil || stdout != "" || !strings.Contains(stderr, "must be new") {
		t.Fatalf("correlation overwrote an existing destination: %v %s %s", err, stdout, stderr)
	}
	inside := filepath.Join(path, "correlation.json")
	if stdout, _, err := run(t, "correlate", path, "--rules", correlateRules, "--output", inside); err == nil || stdout != "" {
		t.Fatal("correlation wrote into the case it read")
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatal("a refused destination inside the case was created anyway")
	}
}

func TestCorrelateRefusesDeclarationsAndEvidenceItCannotStandBehind(t *testing.T) {
	path := twoSourceCase(t)
	directory := t.TempDir()
	unknownMember := filepath.Join(directory, "SECRET-RULES.json")
	if err := os.WriteFile(unknownMember, []byte(`{"schema":"readmit-correlation-rules/v1","script":"SECRET-PATIENT","rules":[{"id":"a","operator":"control-id","scope":"source"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no case":                 {"correlate", "--rules", correlateRules},
		"two cases":               {"correlate", path, path, "--rules", correlateRules},
		"no rules":                {"correlate", path},
		"empty rules flag":        {"correlate", path, "--rules", ""},
		"unreadable rules":        {"correlate", path, "--rules", filepath.Join(directory, "SECRET-ABSENT.json")},
		"unknown rules member":    {"correlate", path, "--rules", unknownMember},
		"another report format":   {"correlate", path, "--rules", correlateRules, "--format", "markdown"},
		"a case that is not one":  {"correlate", directory, "--rules", correlateRules},
		"empty output":            {"correlate", path, "--rules", correlateRules, "--output", ""},
		"rules that are a case":   {"correlate", path, "--rules", path},
		"case that is a document": {"correlate", correlateRules, "--rules", correlateRules},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, args...)
			if err == nil || stdout != "" {
				t.Fatalf("accepted %v: %s", args, stdout)
			}
			if strings.Contains(stderr, "SECRET-") || strings.Contains(stderr, path) || strings.Contains(stderr, directory) {
				t.Fatalf("diagnostic disclosed evidence or a path: %s", stderr)
			}
		})
	}
}

// A rule naming a source this case does not declare is a declaration that does
// not fit the evidence: it is reported, and it never quietly widens to
// whatever sources happen to be there.
func TestCorrelateReportsDeclaredSourcesTheCaseDoesNotHave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", path); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "correlate", path, "--rules", correlateRules, "--format", "json")
	if err != nil || stderr != "" {
		t.Fatalf("correlate: %v %s", err, stderr)
	}
	report := readStrictOutput[correlate.Report](t, stdout)
	unknown := 0
	for _, item := range report.Unsupported {
		if item.Code == "unknown_source" {
			unknown++
		}
	}
	if unknown != 2 {
		t.Fatalf("a rule naming an absent source did not say so: %+v", report.Unsupported)
	}
	for _, link := range report.Links {
		if link.Rule == "same-message" {
			t.Fatalf("a single-source case produced a cross-source link: %+v", link)
		}
	}
}
