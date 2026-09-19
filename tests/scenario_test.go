package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScenarioPreviewsADesignedWorkflowWithoutDisclosingItsIdentifiers is the
// public workflow of R10.1: a shipped, editable, profile-bound template is
// previewed step by step, the outcome of every step is the one its author
// declared, and the preview names scenario-local subjects rather than the
// identifiers the document carries.
func TestScenarioPreviewsADesignedWorkflowWithoutDisclosingItsIdentifiers(t *testing.T) {
	stdout, stderr, err := run(t, "scenario", "preview", "../testdata/fixtures/scenario-adt.json")
	if err != nil || stderr != "" {
		t.Fatalf("scenario preview: %v; stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"Scenario: adt-visit-lifecycle (version 1)",
		"Profile: readmit-adt-lifecycle-v1",
		"Base time: 2026-01-01T12:00:00Z",
		"Steps: 14 designed; 8 accepted, 6 refused",
		"A04    visit-a                   accepted  none -> preadmit (register a patient)",
		"A40    patient-b into patient-b  refused   active unchanged; a patient identity is never merged into itself",
		"A40    patient-b into patient-a  accepted  active -> merged (merge patient identifier list)",
		"the patient identity this visit belongs to was merged away",
		"A40    patient-c into patient-b  refused   active unchanged; the surviving identity was itself merged away",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
	for _, identifier := range []string{"SYNTH-PATIENT-A", "SYNTH-PATIENT-B", "SYNTH-PATIENT-C", "SYNTH-VISIT-A", "SYNTH-VISIT-B", "READMIT"} {
		if strings.Contains(stdout+stderr, identifier) {
			t.Errorf("the preview disclosed %q", identifier)
		}
	}
	again, _, err := run(t, "scenario", "preview", "../testdata/fixtures/scenario-adt.json")
	if err != nil || again != stdout {
		t.Error("the same scenario did not preview identically twice")
	}
}

func TestScenarioPreviewsTheShippedSchedulingTemplate(t *testing.T) {
	stdout, stderr, err := run(t, "scenario", "preview", "../testdata/fixtures/scenario-siu.json")
	if err != nil || stderr != "" {
		t.Fatalf("scenario preview: %v; stderr=%s", err, stderr)
	}
	for _, want := range []string{
		"Profile: readmit-siu-lifecycle-v1",
		"Steps: 9 designed; 4 accepted, 5 refused",
		`S15    appointment-a  refused   none unchanged; S15 (appointment cancellation) is not taken from "none"`,
		`S13    appointment-a  refused   cancelled unchanged; S13 (appointment rescheduling) is not taken from "cancelled"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
}

// TestScenarioRefusesEveryWorkflowItCannotStandBehind keeps the refusals the
// documentation promises reachable from the command line: a declared outcome
// the profile contradicts, a profile this release does not implement, a member
// the contract never declared, and a document that is not one.
func TestScenarioRefusesEveryWorkflowItCannotStandBehind(t *testing.T) {
	shipped, err := os.ReadFile("../testdata/fixtures/scenario-siu.json")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for name, change := range map[string][2]string{
		"an unimplemented profile":    {"readmit-siu-lifecycle-v1", "readmit-siu-v1"},
		"an undeclared member":        {`"subjects": [`, `"seed": 0, "subjects": [`},
		"an event of another profile": {`"event": "S12"`, `"event": "A01"`},
		"a document that is not JSON": {`{`, `not json {`},
	} {
		path := filepath.Join(directory, "scenario.json")
		document := strings.Replace(string(shipped), change[0], change[1], 1)
		if document == string(shipped) {
			t.Fatalf("%s changed nothing; the test is not exercising the reader", name)
		}
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := run(t, "scenario", "preview", path)
		if err == nil {
			t.Errorf("%s was previewed:\n%s", name, stdout)
		}
		if !strings.HasPrefix(stderr, "readmit: ") {
			t.Errorf("%s: unnamed refusal %q", name, stderr)
		}
	}
	stdout, stderr, err := run(t, "scenario", "preview", "../testdata/fixtures/scenario-refused.json")
	if err == nil {
		t.Fatalf("a positive case the profile refuses was previewed:\n%s", stdout)
	}
	for _, want := range []string{"cancel-before-anything-was-booked", "is declared accepted", "readmit-siu-lifecycle-v1"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in refusal: %s", want, stderr)
		}
	}
}

func TestScenarioPreviewsOrderAndResultTemplates(t *testing.T) {
	for _, tc := range []struct{ family, counts, outcome string }{
		{"orm", "6 designed; 3 accepted, 3 refused", "ordered -> cancelled (cancel order request)"},
		{"oru", "6 designed; 4 accepted, 2 refused", "final -> corrected (corrected result report)"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			path := "../testdata/fixtures/scenario-" + tc.family + ".json"
			stdout, stderr, err := run(t, "scenario", "preview", path)
			if err != nil || stderr != "" {
				t.Fatalf("preview failed: %v %s", err, stderr)
			}
			for _, want := range []string{tc.counts, tc.outcome, "Profile: readmit-" + tc.family + "-lifecycle-v1"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("missing %q: %s", want, stdout)
				}
			}
			for _, secret := range []string{"SYNTH-PATIENT-A", "SYNTH-ORDER-A", "SYNTH-PLACER", "SYNTH-FILLER", "PLACER-001", "FILLER-001", "Synthetic observation"} {
				if strings.Contains(stdout+stderr, secret) {
					t.Errorf("disclosed %s", secret)
				}
			}
			again, _, err := run(t, "scenario", "preview", path)
			if err != nil || again != stdout {
				t.Fatal("preview changed")
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			invalid := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(invalid, []byte(strings.Replace(string(source), `"expect": "refused"`, `"expect": "accepted"`, 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err = run(t, "scenario", "preview", invalid)
			if err == nil || stdout != "" || !strings.Contains(stderr, "is declared accepted") {
				t.Fatalf("false positive preview: %v %s %s", err, stdout, stderr)
			}
		})
	}
}
