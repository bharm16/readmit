package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
)

// comparisonParityWorkspace captures the diff fixtures as two entries of one
// workspace and copies in the case, and the baseline result, the native
// window retained in September (testdata/acceptance/native-109), twice over
// for the case, so a copy of historical evidence is compared with itself.
func comparisonParityWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	for name, fixture := range map[string]string{"before": "diff-before.mllp", "after": "diff-after.mllp"} {
		if _, stderr, err := run(t, "capture", "../testdata/fixtures/"+fixture, "--output", filepath.Join(workspace, name)); err != nil || stderr != "" {
			t.Fatalf("capture %s: %v %s", fixture, err, stderr)
		}
	}
	for name, retained := range map[string]string{"regression": "regression", "regression-copy": "regression", "baseline": "baseline"} {
		copyTree(t, filepath.Join("../testdata/acceptance/native-109", retained), filepath.Join(workspace, name))
	}
	return workspace
}

// A comparison paged in the window is the whole comparison `readmit diff`
// reports, window after window, and a window past its last row is empty with
// the counts still beside it. The case the native window retained in
// September pairs with a copy of itself on the identities its evidence
// carries, exactly as the command line pairs it, and neither entry point
// changes a byte of it.
func TestTheWindowPagesTheComparisonReadmitDiffReports(t *testing.T) {
	workspace := comparisonParityWorkspace(t)
	retained := treeOf(t, filepath.Join(workspace, "regression"))
	report, _ := diffJSON(t, filepath.Join(workspace, "before"), filepath.Join(workspace, "after"), "--key", "MSH-10")
	reported := reportedLines(report)
	if len(reported) < 3 {
		t.Fatalf("the fixtures no longer hold more rows than one window of two: %+v", reported)
	}

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "before")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the left collection was not verified: %+v", opened)
	}
	window := func(offset int) desktop.CompareResult {
		return app.Compare(desktop.CompareRequest{
			Workspace: workspace, Left: "before", Identity: opened.Case.Identity, Right: "after",
			Keys: []string{"MSH-10"}, Offset: offset, Limit: 2,
		})
	}
	var paged []comparisonLine
	for offset := 0; offset < len(reported); offset += 2 {
		result := window(offset)
		if result.State != desktop.Completed || result.Comparison == nil {
			t.Fatalf("the window at %d was not compared: %+v", offset, result)
		}
		if result.Comparison.Total != len(reported) || !reflect.DeepEqual(result.Comparison.Summary, report.Summary) ||
			!reflect.DeepEqual(result.Comparison.Keys, report.Keys) || result.Comparison.Alignment != report.Alignment {
			t.Fatalf("the window at %d counted %+v and the command line %+v", offset, result.Comparison, report)
		}
		for index, row := range result.Comparison.Rows {
			if row.Position != offset+index+1 {
				t.Fatalf("row %d of the window at %d is numbered %d", index, offset, row.Position)
			}
		}
		paged = append(paged, renderedLines(result.Comparison)...)
	}
	if !reflect.DeepEqual(paged, reported) {
		t.Fatalf("the window paged %+v and the command line reported %+v", paged, reported)
	}
	past := window(len(reported))
	if past.State != desktop.Empty || past.Comparison == nil || len(past.Comparison.Rows) != 0 ||
		past.Comparison.Total != len(reported) || past.Comparison.Summary != report.Summary {
		t.Fatalf("a window past the last row did not keep the counts: %+v", past)
	}

	historical, _ := diffJSON(t, filepath.Join(workspace, "regression"), filepath.Join(workspace, "regression-copy"))
	native := app.OpenCase(workspace, "regression")
	if native.State != desktop.Completed || native.Case == nil {
		t.Fatalf("the retained case was not verified: %+v", native)
	}
	compared := app.Compare(desktop.CompareRequest{
		Workspace: workspace, Left: "regression", Identity: native.Case.Identity, Right: "regression-copy", Limit: desktop.MaxComparisonRows,
	})
	if compared.State != desktop.Completed || compared.Comparison == nil {
		t.Fatalf("the retained case was not compared with its copy: %+v", compared)
	}
	if compared.Comparison.Alignment != "source-occurrence" || compared.Comparison.Alignment != historical.Alignment ||
		compared.Comparison.Summary != historical.Summary || historical.Summary.Paired != 2 || historical.Summary.Unchanged != 2 {
		t.Fatalf("the window compared the retained case %+v and the command line %+v", compared.Comparison, historical)
	}
	if !reflect.DeepEqual(renderedLines(compared.Comparison), reportedLines(historical)) ||
		compared.Comparison.LeftSummary != historical.Left || compared.Comparison.RightSummary != historical.Right {
		t.Fatalf("the window drew the retained case differently from the command line: %+v", compared.Comparison)
	}
	if !reflect.DeepEqual(treeOf(t, filepath.Join(workspace, "regression")), retained) {
		t.Fatal("comparing the retained case changed it")
	}
}

// Whatever `readmit diff` refuses between two case bundles the window refuses
// as well, and nothing is shown for it: two unrelated collections with no key
// (the window naming what to declare where the command names its option), a
// case whose stored bytes no longer match its identity (in the command line's
// own sentence), and a case a later release wrote. A run result the command
// line compares is outside the window's panel, which compares case bundles
// only, as the desktop guide says.
func TestTheWindowRefusesTheCollectionsReadmitDiffRefuses(t *testing.T) {
	workspace := comparisonParityWorkspace(t)
	copyTree(t, filepath.Join(workspace, "after"), filepath.Join(workspace, "tampered"))
	writeDocument(t, filepath.Join(workspace, "tampered", "payloads"), "s0001-e000001.bin", "MSH|^~\\&|SYNTHETIC|LAB|READMIT|FIXTURE|20260101120000||SIU^S12|CHANGED|P|2.5.1\r")
	copyTree(t, filepath.Join(workspace, "after"), filepath.Join(workspace, "newer"))
	manifest, err := os.ReadFile(filepath.Join(workspace, "newer", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, filepath.Join(workspace, "newer"), "manifest.json", strings.Replace(string(manifest), `"readmit-case/v1"`, `"readmit-case/v9"`, 1))
	before := treeOf(t, workspace)

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "before")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the left collection was not verified: %+v", opened)
	}
	compare := func(right string, keys ...string) desktop.CompareResult {
		return app.Compare(desktop.CompareRequest{
			Workspace: workspace, Left: "before", Identity: opened.Case.Identity, Right: right, Keys: keys, Limit: desktop.MaxComparisonRows,
		})
	}
	command := func(right string, keys ...string) string {
		t.Helper()
		args := []string{"diff", filepath.Join(workspace, "before"), filepath.Join(workspace, right), "--format", "json"}
		for _, key := range keys {
			args = append(args, "--key", key)
		}
		stdout, stderr, err := run(t, args...)
		if exitCode(t, err) != 1 || stdout != "" {
			t.Fatalf("the command line compared %s: %v %s", right, err, stdout)
		}
		return stderr
	}
	for _, refused := range []struct {
		right, window, command string
		keys                   []string
	}{
		{"after", "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them", diff.ErrKeysRequired.Error(), nil},
		{"tampered", "bundle is incomplete or its identity does not match contents", "bundle is incomplete or its identity does not match contents", []string{"MSH-10"}},
		{"newer", "a comparison reads two case bundles this release supports", "unsupported case bundle schema version", []string{"MSH-10"}},
	} {
		result := compare(refused.right, refused.keys...)
		if result.State != desktop.Failed || result.Comparison != nil || result.Reason != refused.window {
			t.Fatalf("the window answered %+v for %s", result, refused.right)
		}
		if stderr := command(refused.right, refused.keys...); stderr != "readmit: "+refused.command+"\n" {
			t.Fatalf("the command line refused %s with %q", refused.right, stderr)
		}
	}

	// The command line compares a case with a retained run result; the window
	// neither offers nor compares one.
	stdout, stderr, err := run(t, "diff", filepath.Join(workspace, "before"), filepath.Join(workspace, "baseline"), "--key", "MSH-10", "--format", "json")
	if err != nil || stderr != "" || stdout == "" {
		t.Fatalf("the command line no longer compares a case with a result: %v %s", err, stderr)
	}
	listed := app.OpenWorkspace(workspace)
	for _, artifact := range listed.Workspace.Artifacts {
		if artifact.Name == "baseline" && artifact.Kind == desktop.CaseArtifact || artifact.Name == "newer" && artifact.Kind == desktop.CaseArtifact {
			t.Fatalf("the window lists %s as a case it would compare", artifact.Name)
		}
	}
	if result := compare("baseline", "MSH-10"); result.State != desktop.Failed || result.Reason != "a comparison reads two case bundles this release supports" {
		t.Fatalf("the window compared a run result: %+v", result)
	}
	if !reflect.DeepEqual(treeOf(t, workspace), before) {
		t.Fatal("a refused comparison changed the workspace")
	}
}

// The policy the window opens is the policy `readmit normalize` applies: the
// same rules through the same reader, named by the digest of the exact bytes
// the preview reads it under, and opened then saved again it is read by the
// command line to the same report. Every policy the command line refuses is
// refused by the open, the preview and the save in the command line's own
// sentence, and nothing is written.
func TestTheWindowOpensThePolicyReadmitNormalizeApplies(t *testing.T) {
	workspace, declared := normalizeParityWorkspace(t)
	left, right := filepath.Join(workspace, "before"), filepath.Join(workspace, "after")
	applied, err := diff.ReadPolicy(filepath.Join(workspace, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, _ := normalizeJSON(t, left, right, "--key", "MSH-10", "--policy", filepath.Join(workspace, "policy.json"))

	app := desktopApp(t, workspace)
	opened := app.OpenNormalizationPolicy(workspace, "policy.json")
	if opened.State != desktop.Completed || opened.Policy == nil {
		t.Fatalf("the window did not open the policy: %+v", opened)
	}
	if !reflect.DeepEqual(*opened.Policy, applied) {
		t.Fatalf("the window opened %+v and the command line applies %+v", *opened.Policy, applied)
	}
	if len(opened.Policy.Rules) != len(report.Rules) {
		t.Fatalf("the window opened %d rules and the command line reported %d", len(opened.Policy.Rules), len(report.Rules))
	}
	for index, rule := range opened.Policy.Rules {
		reported := report.Rules[index]
		if rule.ID != reported.ID || rule.Operator != reported.Operator || rule.Precision != reported.Precision || rule.Tolerance != reported.Tolerance {
			t.Fatalf("rule %d opened as %+v and applied as %+v", index, rule, reported)
		}
	}
	sum := sha256.Sum256(declared)
	caseOpened := app.OpenCase(workspace, "before")
	if caseOpened.State != desktop.Completed || caseOpened.Case == nil {
		t.Fatalf("the left collection was not verified: %+v", caseOpened)
	}
	normalize := func(policy string) desktop.NormalizeResult {
		return app.NormalizeCompare(desktop.NormalizeRequest{
			Workspace: workspace, Left: "before", Identity: caseOpened.Case.Identity, Right: "after",
			Policy: policy, Keys: []string{"MSH-10"}, Limit: desktop.MaxComparisonRows,
		})
	}
	previewed := normalize("policy.json")
	if opened.SHA256 != hex.EncodeToString(sum[:]) || previewed.Normalization == nil || previewed.Normalization.PolicySHA256 != opened.SHA256 {
		t.Fatalf("the open named %s and the preview %+v for bytes hashing to %x", opened.SHA256, previewed, sum)
	}

	// Saved again from the document the window opened, the policy is a new
	// entry the command line reads to the report it read from the original.
	saved := app.SaveNormalizationPolicy(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: opened.Document, Output: "reopened.json"})
	if saved.State != desktop.Completed {
		t.Fatalf("the opened policy was not saved again: %+v", saved)
	}
	again, _ := normalizeJSON(t, left, right, "--key", "MSH-10", "--policy", filepath.Join(workspace, "reopened.json"))
	if !reflect.DeepEqual(again.Rules, report.Rules) || again.Summary != report.Summary || !reflect.DeepEqual(again.Differences, report.Differences) {
		t.Fatalf("the policy saved from the window read as %+v, the original as %+v", again, report)
	}

	refusedFixture, err := os.ReadFile("../testdata/fixtures/normalize-policy-refused.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, document := range map[string]string{
		"shipped-refused.json":  string(refusedFixture),
		"unknown-member.json":   `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore","extra":1}]}`,
		"truncated.json":        `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7"`,
		"later-contract.json":   `{"schema":"readmit-normalization-policy/v2","rules":[]}`,
		"unknown-operator.json": `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignroe"}]}`,
	} {
		writeDocument(t, workspace, name, document)
		before := treeOf(t, workspace)
		stdout, stderr, err := run(t, "normalize", left, right, "--key", "MSH-10", "--policy", filepath.Join(workspace, name), "--format", "json")
		if exitCode(t, err) != 1 || stdout != "" || !strings.HasPrefix(stderr, "readmit: ") {
			t.Fatalf("the command line applied %s: %v %s %s", name, err, stdout, stderr)
		}
		sentence := strings.TrimSuffix(strings.TrimPrefix(stderr, "readmit: "), "\n")
		if result := app.OpenNormalizationPolicy(workspace, name); result.State != desktop.Failed || result.Reason != sentence || result.Policy != nil || result.Document != "" {
			t.Fatalf("the window opened %s as %+v where the command line said %q", name, result, sentence)
		}
		if result := normalize(name); result.State != desktop.Failed || result.Reason != sentence || result.Normalization != nil {
			t.Fatalf("the window previewed %s as %+v where the command line said %q", name, result, sentence)
		}
		if result := app.SaveNormalizationPolicy(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: document, Output: "saved-" + name}); result.State != desktop.Failed || result.Reason != sentence {
			t.Fatalf("the window saved %s as %+v where the command line said %q", name, result, sentence)
		}
		if !reflect.DeepEqual(treeOf(t, workspace), before) {
			t.Fatalf("refusing %s changed the workspace", name)
		}
	}
}
