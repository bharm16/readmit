package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/transform"
)

// schedulingCase captures the two committed SIU fixtures as two sources: one
// booking and its reschedule, declaring one patient under one configured
// assigning authority. It is the smallest evidence a relationship-preserving
// rename can be demonstrated on.
func schedulingCase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	if _, stderr, err := run(t, "capture",
		"../testdata/fixtures/listen-s12.hl7", "../testdata/fixtures/listen-s13.hl7", "--output", path); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return path
}

// authoredPlan writes one plan bound to the case and the declarations it is
// about to be applied to. Both identities come out of commands an operator
// already runs: `timeline` verifies the case, and a JSON correlation report
// names the SHA-256 of the exact rules it ran under.
func authoredPlan(t *testing.T, path, steps string) string {
	t.Helper()
	reported, stderr, err := run(t, "correlate", path, "--rules", correlateRules, "--format", "json")
	if err != nil || stderr != "" {
		t.Fatalf("correlate: %v %s", err, stderr)
	}
	var report correlate.Report
	if err := json.Unmarshal([]byte(reported), &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	plan := `{"schema":"` + transform.PlanSchema + `","case":"` + report.CaseIdentity +
		`","rules":"` + report.RulesSHA256 + `","steps":[` + steps + `]}`
	file := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(file, []byte(plan), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestTransformExecutablePreviewsASequenceWithoutTouchingTheCase(t *testing.T) {
	path := schedulingCase(t)
	before, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	plan := authoredPlan(t, path, `{"operator":"rebase-identifiers/v1","rule":"patient"},`+
		`{"operator":"shift-dates/v1","shift":"24h"},`+
		`{"operator":"duplicate-occurrence/v1","entry":"t000001"},`+
		`{"operator":"reorder-occurrence/v1","entry":"t000002","position":1}`)

	stdout, stderr, err := run(t, "transform", path, "--rules", correlateRules, "--plan", plan)
	if err != nil || stderr != "" {
		t.Fatalf("transform: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Contract: readmit-transform-preview/v1",
		"Pinned profile pack: none",
		"rebase-identifiers/v1 PID[1]-3[1].1",
		"shift-dates/v1 SCH[1]-11[1].4",
		"copy",
		"preserved=true",
		"2.5.1 SIU entries=3 parse=unknown",
		"A preview states what this plan would do",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in\n%s", want, stdout)
		}
	}
	for _, secret := range []string{"SYNTH-001", "LISTEN-BOOK", "READMIT000001", "20260102100000"} {
		if strings.Contains(stdout, secret) {
			t.Fatalf("the preview printed the value %q:\n%s", secret, stdout)
		}
	}

	rendered, stderr, err := run(t, "transform", path, "--rules", correlateRules, "--plan", plan, "--format", "json")
	if err != nil || stderr != "" {
		t.Fatalf("transform json: %v %s", err, stderr)
	}
	var preview transform.Preview
	if err := json.Unmarshal([]byte(rendered), &preview, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the preview is not one strict %s document: %v", transform.PreviewSchema, err)
	}
	if preview.Summary.Copies != 1 || preview.Summary.Entries != preview.Summary.Occurrences+1 {
		t.Fatalf("the duplicated entry was not previewed: %+v", preview.Summary)
	}
	if preview.Sequence[0].ID != "t000002" || preview.Sequence[0].Source != "s0002" {
		t.Fatalf("the reordered entry is not first, or lost its source mapping: %+v", preview.Sequence[0])
	}
	// One patient declared in two sources under one configured authority is one
	// relation, so both occurrences and the copy of one of them are renamed to
	// the same value.
	relation := 0
	for _, change := range preview.Changes {
		if change.Operator != transform.RebaseIdentifiers {
			continue
		}
		if change.Group == 0 {
			t.Fatalf("a rename was recorded without the relation it preserves: %+v", change)
		}
		if relation != 0 && change.Group != relation {
			t.Fatalf("one patient under one authority was renamed into two: %+v", preview.Changes)
		}
		relation = change.Group
	}
	if relation == 0 {
		t.Fatal("no identifier was renamed")
	}
	if preview.Summary.Preserved != preview.Summary.Relations || preview.Summary.Relations == 0 {
		t.Fatalf("a transformation that dropped nothing did not preserve every relation: %+v", preview.Summary)
	}

	after, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a preview changed the evidence it read")
	}
}

// A control ID this case ties to an acknowledgement it could not resolve cannot
// be renamed without stranding that acknowledgement, so the command refuses
// rather than reporting a transformation that breaks a relation. Equal
// identifier bytes under no configured authority are not decided either: the
// position is left exactly as the evidence has it and reported.
func TestTransformExecutableRefusesWhatItCannotStandBehind(t *testing.T) {
	path := twoSourceCase(t)
	stranding := authoredPlan(t, path, `{"operator":"rebase-identifiers/v1","rule":"same-message"}`)
	if _, stderr, err := run(t, "transform", path, "--rules", correlateRules, "--plan", stranding); err == nil {
		t.Fatalf("a rename that would strand an acknowledgement was previewed anyway: %s", stderr)
	}

	undecided := authoredPlan(t, path, `{"operator":"rebase-identifiers/v1","rule":"patient"}`)
	stdout, stderr, err := run(t, "transform", path, "--rules", correlateRules, "--plan", undecided)
	if err != nil || stderr != "" {
		t.Fatalf("transform: %v %s", err, stderr)
	}
	for _, want := range []string{"unqualified-identifier", "undecodable-occurrence", "Changes: 0"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in\n%s", want, stdout)
		}
	}
}

func TestTransformExecutableRefusesInvalidInvocations(t *testing.T) {
	path := schedulingCase(t)
	valid := authoredPlan(t, path, `{"operator":"shift-dates/v1","shift":"24h"}`)
	for name, args := range map[string][]string{
		"no declared rules":    {"transform", path, "--plan", valid},
		"no declared plan":     {"transform", path, "--rules", correlateRules},
		"an unknown format":    {"transform", path, "--rules", correlateRules, "--plan", valid, "--format", "yaml"},
		"a pack nobody pinned": {"transform", path, "--rules", correlateRules, "--plan", valid, "--profile", "../testdata/fixtures/profile-pack.json"},
		"two case arguments":   {"transform", path, path, "--rules", correlateRules, "--plan", valid},
		"a missing plan file":  {"transform", path, "--rules", correlateRules, "--plan", filepath.Join(t.TempDir(), "absent.json")},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	// A plan names the evidence it was authored against, so it is refused
	// elsewhere rather than applied to whatever happens to be there.
	if _, _, err := run(t, "transform", twoSourceCase(t), "--rules", correlateRules, "--plan", valid); err == nil {
		t.Fatal("a plan authored against other evidence was applied")
	}
}

// There is nothing to cancel and nothing to recover: one run is one bounded
// read of one verified case. What it can leave behind is the one file
// `--output` writes, and that file is new or it is not written at all.
func TestTransformExecutableWritesOneNewFileAndNeverOverwrites(t *testing.T) {
	path := schedulingCase(t)
	plan := authoredPlan(t, path, `{"operator":"shift-dates/v1","shift":"24h"}`)
	destination := filepath.Join(t.TempDir(), "preview.json")

	if _, stderr, err := run(t, "transform", path, "--rules", correlateRules, "--plan", plan,
		"--format", "json", "--output", destination); err != nil {
		t.Fatalf("transform --output: %v %s", err, stderr)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the preview was written as %v", info.Mode().Perm())
	}
	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}

	// A destination that already exists is refused, and the bytes that were
	// there are exactly what they were.
	if _, _, err := run(t, "transform", path, "--rules", correlateRules, "--plan", plan,
		"--format", "json", "--output", destination); err == nil {
		t.Fatal("an existing destination was overwritten")
	}
	again, err := os.ReadFile(destination)
	if err != nil || string(again) != string(written) {
		t.Fatal("a refused write changed the file that was already there")
	}

	// A destination inside the case it was read from is refused by the same
	// output policy every other command uses.
	inside := filepath.Join(path, "preview.json")
	if _, _, err := run(t, "transform", path, "--rules", correlateRules, "--plan", plan, "--output", inside); err == nil {
		t.Fatal("a preview was written inside the evidence it read")
	}
	if _, err := os.Lstat(inside); !os.IsNotExist(err) {
		t.Fatal("a refused write left a file inside the case")
	}
}
