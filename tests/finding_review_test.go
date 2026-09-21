package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/findingreview"
)

// reviewable captures the acknowledged booking fixture and diagnoses it,
// reporting the case, the diagnosis and the digest a decision names.
func reviewable(t *testing.T) (casePath, diagnosisPath, reportIdentity string) {
	t.Helper()
	root := t.TempDir()
	casePath = filepath.Join(root, "acked-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/diagnose-acknowledged.mllp", "--output", casePath); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	diagnosisPath = filepath.Join(root, "diagnosis")
	if _, stderr, err := run(t, "diagnose", casePath, "--output", diagnosisPath); err != nil {
		t.Fatalf("diagnose: %v %s", err, stderr)
	}
	data, err := os.ReadFile(filepath.Join(diagnosisPath, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return casePath, diagnosisPath, hex.EncodeToString(sum[:])
}

func writeDecisions(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decisions.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const confirmAndSuppress = `{"schema":"readmit-finding-decisions/v1","report_sha256":"%s",
"decisions":[
 {"finding":"f000001","verdict":"confirmed","rationale":"The scheduler must keep rejecting this booking."},
 {"finding":"f000002","verdict":"suppressed","scope":"case","rationale":"The error detail is the vendor's wording."}]}`

func TestDiagnoseReviewPromotesOnlyWhatAPersonConfirmed(t *testing.T) {
	casePath, diagnosisPath, identity := reviewable(t)
	before, err := os.ReadFile(filepath.Join(casePath, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	decisions := writeDecisions(t, fmt.Sprintf(confirmAndSuppress, identity))
	output := filepath.Join(t.TempDir(), "review")
	stdout, stderr, err := run(t, "diagnose", "review", diagnosisPath, "--case", casePath, "--decisions", decisions, "--output", output)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Review complete:") {
		t.Fatalf("diagnose review: %v %s %s", err, stdout, stderr)
	}
	data, err := os.ReadFile(filepath.Join(output, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	record := readStrictOutput[findingreview.Record](t, string(data))
	if record.Schema != findingreview.Schema || record.Diagnosis.Report != identity || record.Statement == "" {
		t.Fatalf("the review does not name the diagnosis it was made over: %+v", record)
	}
	confirmed, suppressed := 0, 0
	for _, status := range record.Findings {
		switch status.Verdict {
		case findingreview.Confirmed:
			confirmed++
			if status.Promotion == nil || len(status.Promotion.Expectations) == 0 {
				t.Fatalf("a confirmed acknowledgement finding promoted nothing: %+v", status)
			}
		case findingreview.Suppressed:
			suppressed++
			if status.Promotion != nil {
				t.Fatalf("a suppressed finding was promoted: %+v", status)
			}
		}
		if status.NextEvidence == "" {
			t.Fatalf("a finding was reported without the evidence that would settle it: %+v", status)
		}
	}
	if confirmed != 1 || suppressed != 1 {
		t.Fatalf("expected one confirmation and one suppression, got %d and %d", confirmed, suppressed)
	}
	markdown, err := os.ReadFile(filepath.Join(output, "review.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Both formats render the same record, and neither carries evidence the
	// diagnosis itself refuses to copy.
	for _, status := range record.Findings {
		if !strings.Contains(string(markdown), status.Finding) || !strings.Contains(string(markdown), status.RuleID) {
			t.Fatal("the review formats disagree")
		}
	}
	rendered := stdout + stderr + string(data) + string(markdown)
	if strings.Contains(rendered, "SECRET-") || strings.Contains(rendered, "DIAGNOSE-ACKED") || strings.Contains(rendered, casePath) {
		t.Fatal("the review disclosed free text, a raw identifier or a path")
	}
	after, err := os.ReadFile(filepath.Join(casePath, "identity.sha256"))
	if err != nil || string(before) != string(after) {
		t.Fatal("the reviewed case did not survive the review unchanged")
	}
}

func TestDiagnoseReviewRefusesWhatWouldMisattributeAJudgment(t *testing.T) {
	casePath, diagnosisPath, identity := reviewable(t)
	valid := writeDecisions(t, fmt.Sprintf(confirmAndSuppress, identity))
	other := writeDecisions(t, fmt.Sprintf(confirmAndSuppress, strings.Repeat("b", 64)))
	unrelated := filepath.Join(t.TempDir(), "other-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/diagnose-booking.hl7", "--output", unrelated); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	for name, args := range map[string][]string{
		"decisions read against another report": {diagnosisPath, "--case", casePath, "--decisions", other},
		"a case the diagnosis was not run over": {diagnosisPath, "--case", unrelated, "--decisions", valid},
		"a decisions document this release refuses": {diagnosisPath, "--case", casePath,
			"--decisions", "../testdata/fixtures/finding-decisions-refused.json"},
		"no decisions at all": {diagnosisPath, "--case", casePath},
	} {
		output := filepath.Join(t.TempDir(), "review")
		_, _, err := run(t, append(append([]string{"diagnose", "review"}, args...), "--output", output)...)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if _, statErr := os.Stat(output); statErr == nil {
			t.Fatalf("%s wrote a review directory anyway", name)
		}
	}
	// A review inside the verified case would change the evidence it reviews.
	inside := filepath.Join(casePath, "review")
	if _, _, err := run(t, "diagnose", "review", diagnosisPath, "--case", casePath, "--decisions", valid, "--output", inside); err == nil {
		t.Fatal("a review was written inside the case it was made over")
	}
	if _, err := os.Stat(inside); err == nil {
		t.Fatal("a refused review left a directory inside the case")
	}
}

func TestShippedDecisionsFixturesReadExactlyAsDocumented(t *testing.T) {
	accepted, err := os.ReadFile("../testdata/fixtures/finding-decisions.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findingreview.ParseDecisions(accepted); err != nil {
		t.Fatalf("the shipped decisions fixture was refused: %v", err)
	}
	refused, err := os.ReadFile("../testdata/fixtures/finding-decisions-refused.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findingreview.ParseDecisions(refused); err == nil {
		t.Fatal("the shipped refused fixture was accepted")
	}
}
