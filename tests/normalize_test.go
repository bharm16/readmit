package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diff"
)

const normalizeBefore = "../testdata/fixtures/normalize-before.mllp"
const normalizeAfter = "../testdata/fixtures/normalize-after.mllp"
const normalizePolicy = "../testdata/fixtures/normalize-policy.json"

func normalizeJSON(t *testing.T, args ...string) (diff.NormalizationReport, string) {
	t.Helper()
	args = append(append([]string{"normalize"}, args...), "--format", "json")
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("normalize: %v %s", err, stderr)
	}
	var report diff.NormalizationReport
	if err := json.Unmarshal([]byte(stdout), &report, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	return report, stdout
}

func writePolicy(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNormalizeAcceptanceFixtureShowsEveryRuleAndEverySuppression(t *testing.T) {
	report, stdout := normalizeJSON(t, normalizeBefore, normalizeAfter, "--policy", normalizePolicy)
	var expected struct {
		Summary    diff.NormalizationSummary `json:"summary"`
		Suppressed []struct {
			Selector string `json:"selector"`
			Rule     string `json:"rule"`
		} `json:"suppressed"`
		Retained      string `json:"retained_selector"`
		Undecided     string `json:"undecided_selector"`
		Reason        string `json:"undecided_reason"`
		Unaddressed   string `json:"unaddressed_selector"`
		UnappliedRule string `json:"unapplied_rule"`
	}
	data, err := os.ReadFile("../testdata/fixtures/normalize-expected.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &expected, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if report.Schema != "readmit-normalization/v1" || report.PolicySchema != "readmit-normalization-policy/v1" {
		t.Fatalf("wrong contract names: %+v", report)
	}
	if report.Summary != expected.Summary || report.Alignment != "single-message" {
		t.Fatalf("incorrect fixture summary: %+v", report.Summary)
	}
	byOutcome := map[string][]diff.Difference{}
	for _, difference := range report.Differences {
		byOutcome[difference.Outcome] = append(byOutcome[difference.Outcome], difference)
	}
	if len(byOutcome["suppressed"]) != len(expected.Suppressed) {
		t.Fatalf("suppressed differences were not all listed: %+v", byOutcome["suppressed"])
	}
	for i, want := range expected.Suppressed {
		got := byOutcome["suppressed"][i]
		if got.Selector != want.Selector || got.Rule != want.Rule || got.Reason != "" {
			t.Fatalf("a suppressed difference is not attributable to its rule: %+v", got)
		}
	}
	if byOutcome["retained"][0].Selector != expected.Retained {
		t.Fatalf("a difference outside every tolerance was not retained: %+v", byOutcome["retained"])
	}
	undecided := byOutcome["undecided"][0]
	if undecided.Selector != expected.Undecided || undecided.Reason != expected.Reason {
		t.Fatalf("a rule that could not read its values did not say so: %+v", undecided)
	}
	unaddressed := byOutcome["unaddressed"][0]
	if unaddressed.Selector != expected.Unaddressed || unaddressed.Rule != "" {
		t.Fatalf("a difference no rule addressed was attributed to one: %+v", unaddressed)
	}
	// A rule that applied to nothing is still a rule the reader was told about.
	unapplied := false
	for _, rule := range report.Rules {
		if rule.ID == expected.UnappliedRule {
			unapplied = rule.Compared == 0 && rule.Suppressed == 0 && rule.Retained == 0 && rule.Undecided == 0
		}
	}
	if len(report.Rules) != 6 || !unapplied {
		t.Fatalf("every applied rule is not listed with its counts: %+v", report.Rules)
	}
	if strings.Contains(stdout, "SECRET") || strings.Contains(stdout, "normalize-before") {
		t.Fatal("the report disclosed values or paths")
	}
	// Every rendering carries every suppression; none may quietly drop one.
	for _, format := range []string{"terminal", "markdown"} {
		stdout, stderr, err := run(t, "normalize", normalizeBefore, normalizeAfter, "--policy", normalizePolicy, "--format", format)
		if err != nil || stderr != "" {
			t.Fatalf("normalize %s: %v %s", format, err, stderr)
		}
		for _, want := range []string{"suppressed=3", "uncompared fields=0", "undecided=1", "unaddressed=1", "value_not_numeric", "volatile-run-identifier"} {
			if !strings.Contains(stdout, escaped(want, format)) {
				t.Fatalf("%s rendering is missing %q:\n%s", format, want, stdout)
			}
		}
	}
}

// escaped repeats the renderer's own Markdown escaping, so a marker is looked
// for in the form that rendering actually writes it in.
func escaped(text, format string) string {
	if format != "markdown" {
		return text
	}
	var out strings.Builder
	for _, c := range text {
		if strings.ContainsRune("\\`*_{}[]<>()#+-.!|&", c) {
			out.WriteByte('\\')
		}
		out.WriteRune(c)
	}
	return out.String()
}

// #53 decided deliberately what crosses a comparison boundary and tested it.
// This report holds the same line from the other side: whatever the comparison
// could display, none of it appears here, in any rendering.
func TestNormalizeNeverDisplaysAValueTheComparisonRead(t *testing.T) {
	shown, _ := diffJSON(t, normalizeBefore, normalizeAfter, "--show-values")
	var displays []string
	for _, pair := range shown.Pairs {
		for _, field := range pair.Fields {
			for _, value := range []diff.Value{field.Left, field.Right} {
				if value.Display == nil {
					t.Fatal("the comparison under test displayed nothing to look for")
				}
				displays = append(displays, strings.Trim(*value.Display, `"`))
			}
		}
	}
	if len(displays) < 6 {
		t.Fatalf("too few displayed values to be a meaningful check: %v", displays)
	}
	for _, format := range []string{"json", "terminal", "markdown"} {
		stdout, _, err := run(t, "normalize", normalizeBefore, normalizeAfter, "--policy", normalizePolicy, "--format", format)
		if err != nil {
			t.Fatalf("normalize %s: %v", format, err)
		}
		for _, display := range displays {
			if strings.Contains(stdout, display) {
				t.Fatalf("%s rendering disclosed a compared value", format)
			}
		}
	}
	// There is no option that would turn them on, either.
	if _, stderr, err := run(t, "normalize", normalizeBefore, normalizeAfter, "--policy", normalizePolicy, "--show-values"); err == nil || stderr == "" {
		t.Fatal("normalize accepted a request to display values")
	}
}

func TestNormalizeRefusesAPolicyItCannotReadRatherThanApplyingPartOfIt(t *testing.T) {
	for _, c := range []struct{ name, policy string }{
		{"a tolerance that is a direction", "../testdata/fixtures/normalize-policy-refused.json"},
		{"an unknown member", writePolicy(t, `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignroe"}]}`)},
		{"a document that is not this contract", writePolicy(t, `{"schema":"readmit-redact-policy/v1","fields":[]}`)},
		{"a directory", t.TempDir()},
		{"a path that does not exist", filepath.Join(t.TempDir(), "absent.json")},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := run(t, "normalize", normalizeBefore, normalizeAfter, "--policy", c.policy, "--format", "json")
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("an unreadable policy produced a report: %s", stdout)
			}
		})
	}
	stdout, stderr, err := run(t, "normalize", normalizeBefore, normalizeAfter, "--format", "json")
	if err == nil || stdout != "" || stderr == "" {
		t.Fatal("a comparison ran with no policy at all")
	}
}

// A policy scopes field differences. It cannot make an occurrence one side does
// not have look like one both sides agree about.
func TestNormalizeCannotSuppressEvidenceNoRuleCanAddress(t *testing.T) {
	left, right := diffCases(t)
	policy := writePolicy(t, `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"volatile-message-time","selector":"MSH-7","operator":"ignore"},{"id":"volatile-patient-alternate","selector":"PID-3[2]","operator":"ignore"}]}`)
	identities := [2][]byte{}
	for i, path := range []string{left, right} {
		data, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
		if err != nil {
			t.Fatal(err)
		}
		identities[i] = data
	}
	report, stdout := normalizeJSON(t, left, right, "--key", "MSH-10", "--policy", policy)
	if report.Summary.Inserted != 1 || report.Summary.Paired != 2 {
		t.Fatalf("an inserted occurrence was concealed: %+v", report.Summary)
	}
	if report.Summary.Suppressed != 2 || report.Summary.Unaddressed != 2 {
		t.Fatalf("suppression was not confined to the two declared rules: %+v", report.Summary)
	}
	if report.Alignment != "declared-keys" || len(report.Keys) != 1 {
		t.Fatalf("the report did not restate how records were paired: %+v", report)
	}
	if strings.Contains(stdout, "SECRET") {
		t.Fatal("the report disclosed values")
	}
	for i, path := range []string{left, right} {
		data, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
		if err != nil || string(data) != string(identities[i]) {
			t.Fatal("producing a normalization report changed the evidence it read")
		}
	}
}
