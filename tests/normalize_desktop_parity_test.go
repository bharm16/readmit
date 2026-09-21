package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
)

// normalizeParityWorkspace captures the normalization fixtures as two entries
// of one workspace and authors the committed policy beside them, which is how
// the window reaches all three: entries of the folder the person opened.
func normalizeParityWorkspace(t *testing.T) (workspace string, declared []byte) {
	t.Helper()
	workspace = t.TempDir()
	for name, fixture := range map[string]string{"before": "normalize-before.mllp", "after": "normalize-after.mllp"} {
		if _, stderr, err := run(t, "capture", "../testdata/fixtures/"+fixture, "--output", filepath.Join(workspace, name)); err != nil || stderr != "" {
			t.Fatalf("capture %s: %v %s", fixture, err, stderr)
		}
	}
	declared, err := os.ReadFile(normalizePolicy)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, workspace, "policy.json", string(declared))
	return workspace, declared
}

// The desktop shell and the command line are two entry points into one
// normalization. The window names two entries of a workspace and a declared
// policy entry; `readmit normalize --format json` reads the same evidence under
// the same policy document. Neither decides a suppression of its own, so every
// rule report, every count and every listed difference must agree — and no
// value the comparison read crosses the window's facade.
func TestDesktopNormalizationMatchesTheCommandLineOverTheSamePolicy(t *testing.T) {
	workspace, declared := normalizeParityWorkspace(t)
	left, right := filepath.Join(workspace, "before"), filepath.Join(workspace, "after")
	report, _ := normalizeJSON(t, left, right, "--key", "MSH-10", "--policy", filepath.Join(workspace, "policy.json"))
	if report.Summary.Suppressed == 0 || report.Summary.Retained == 0 || report.Summary.Undecided == 0 || report.Summary.Unaddressed == 0 {
		t.Fatalf("the fixtures no longer exercise every outcome a policy can reach: %+v", report.Summary)
	}

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "before")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the left collection was not verified: %+v", opened)
	}
	result := app.NormalizeCompare(desktop.NormalizeRequest{
		Workspace: workspace, Left: "before", Identity: opened.Case.Identity,
		Right: "after", Policy: "policy.json", Keys: []string{"MSH-10"}, Limit: desktop.MaxComparisonRows,
	})
	if result.State != desktop.Completed || result.Normalization == nil {
		t.Fatalf("the window did not normalize the comparison: %+v", result)
	}
	normalization := result.Normalization
	if normalization.Summary != report.Summary || normalization.Alignment != report.Alignment {
		t.Fatalf("the window counted %+v %q and the command line %+v %q",
			normalization.Summary, normalization.Alignment, report.Summary, report.Alignment)
	}
	if normalization.Report != report.Schema || normalization.PolicySchema != report.PolicySchema {
		t.Fatalf("the two entry points name different contracts: %+v", normalization)
	}
	if !reflect.DeepEqual(normalization.Rules, report.Rules) {
		t.Fatalf("the window reported rules %+v and the command line %+v", normalization.Rules, report.Rules)
	}
	if !reflect.DeepEqual(normalization.Differences, report.Differences) {
		t.Fatalf("the window listed %+v and the command line %+v", normalization.Differences, report.Differences)
	}
	unsupported := report.Unsupported
	if unsupported == nil {
		unsupported = []diff.Unsupported{}
	}
	if !reflect.DeepEqual(normalization.Unsupported, unsupported) {
		t.Fatalf("the window reported %+v unsupported and the command line %+v", normalization.Unsupported, report.Unsupported)
	}
	// The preview states which policy revision it ran under: the digest of the
	// exact bytes the workspace entry holds.
	sum := sha256.Sum256(declared)
	if normalization.PolicySHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the window named a policy revision the entry does not hold: %s", normalization.PolicySHA256)
	}

	// Whatever the comparison could display, none of it appears here — the same
	// line the raw comparison panel already holds.
	shown, _ := diffJSON(t, left, right, "--key", "MSH-10", "--show-values")
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	displayed := 0
	for _, pair := range shown.Pairs {
		for _, field := range pair.Fields {
			for _, value := range []diff.Value{field.Left, field.Right} {
				if value.Display == nil {
					continue
				}
				display := strings.Trim(*value.Display, `"`)
				if display == "" {
					continue
				}
				displayed++
				if strings.Contains(string(encoded), display) {
					t.Fatalf("the window disclosed a compared value: %s", encoded)
				}
			}
		}
	}
	if displayed == 0 {
		t.Fatal("the comparison displayed no value, so nothing was checked")
	}
}

// A policy neither entry point can read is refused by both for the same reason,
// and no part of it is applied anywhere.
func TestDesktopNormalizationRefusesThePolicyTheCommandLineRefuses(t *testing.T) {
	workspace, _ := normalizeParityWorkspace(t)
	left, right := filepath.Join(workspace, "before"), filepath.Join(workspace, "after")
	writeDocument(t, workspace, "refused-policy.json",
		`{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignroe"}]}`)
	stdout, stderr, err := run(t, "normalize", left, right, "--key", "MSH-10", "--policy", filepath.Join(workspace, "refused-policy.json"), "--format", "json")
	if err == nil || stdout != "" || stderr == "" {
		t.Fatalf("the command line applied part of an unreadable policy: %s", stdout)
	}
	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "before")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the left collection was not verified: %+v", opened)
	}
	result := app.NormalizeCompare(desktop.NormalizeRequest{
		Workspace: workspace, Left: "before", Identity: opened.Case.Identity,
		Right: "after", Policy: "refused-policy.json", Keys: []string{"MSH-10"}, Limit: desktop.MaxComparisonRows,
	})
	if result.State != desktop.Failed || result.Normalization != nil {
		t.Fatalf("the window applied part of an unreadable policy: %+v", result)
	}
	if result.Reason == "" || !strings.Contains(stderr, result.Reason) {
		t.Fatalf("the two entry points refuse differently: window %q, command line %q", result.Reason, stderr)
	}
}
