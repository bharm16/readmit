package desktop_test

// The review-and-transform panel's plan operations, held to what `readmit
// transform` reads. A plan the window saves is a document the command line
// previews unchanged, reopening it reads it through the same decoder, the
// window's preview of it is byte for byte the command line's JSON, and a plan
// the decoder refuses is refused by every one of them in the same sentence
// while nothing is written.

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/transform"
)

// authoredSteps renames the patient identifier the rules relate, moves every
// supported timestamp by a day and repeats the reschedule.
var authoredSteps = []transform.Step{
	{Operator: transform.RebaseIdentifiers, Rule: "patient"},
	{Operator: transform.ShiftDates, Shift: "24h"},
	{Operator: transform.DuplicateOccurrence, Entry: "t000002"},
}

// transformCommand is `readmit transform` over the workspace's case with the
// named rules and plan, as JSON, and the refusal it reported.
func transformCommand(t *testing.T, root, rules, plan string) (string, string, error) {
	t.Helper()
	return commandLine(t, "transform", filepath.Join(root, "incident"),
		"--rules", filepath.Join(root, rules), "--plan", filepath.Join(root, plan), "--format", "json")
}

// A plan authored and saved in the window is the document `readmit
// transform` previews: its rules digest is the one `readmit correlate`
// reports for the rules it names, reopening it reads the same plan through the
// same decoder, and the window's preview of it is the command line's JSON byte
// for byte. Saving wrote exactly one new entry and nothing else.
func TestAPlanSavedInTheWindowIsTheOneReadmitTransformPreviews(t *testing.T) {
	app, root, identity := transformWorkspace(t, "")
	before := bytesUnder(t, root)

	saved := app.SaveTransformPlan(desktop.TransformPlanRequest{
		Workspace: root, Case: "incident", Identity: identity, Rules: "rules.json",
		Steps: authoredSteps, Output: "authored-plan.json",
	})
	if saved.State != desktop.Completed || saved.Plan == nil {
		t.Fatalf("the window did not save the plan: %+v", saved)
	}
	written, err := os.ReadFile(filepath.Join(root, "authored-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	after := bytesUnder(t, root)
	delete(after, "authored-plan.json")
	if !maps.EqualFunc(before, after, bytes.Equal) {
		t.Fatalf("saving a plan changed something other than its own new entry: %v", entriesOf(t, root))
	}
	decoded, err := transform.DecodePlan(written)
	if err != nil {
		t.Fatalf("the command line's decoder refuses what the window wrote: %v", err)
	}
	sameDocument(t, "saved plan", saved.Plan.Plan, decoded)
	if decoded.Case != identity || !slices.Equal(decoded.Steps, authoredSteps) {
		t.Fatalf("the plan is not bound to the case and steps it was authored with: %+v", decoded)
	}

	correlated, _, err := commandLine(t, "correlate", filepath.Join(root, "incident"),
		"--rules", filepath.Join(root, "rules.json"), "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		RulesSHA256 string `json:"rules_sha256"`
	}
	if err := json.Unmarshal([]byte(correlated), &report); err != nil {
		t.Fatal(err)
	}
	if decoded.Rules != report.RulesSHA256 || saved.Plan.Digest != report.RulesSHA256 {
		t.Fatalf("the plan pins rules %s, readmit correlate reports %s", decoded.Rules, report.RulesSHA256)
	}

	opened := app.OpenTransformPlan(root, "authored-plan.json")
	if opened.State != desktop.Completed || opened.Plan == nil {
		t.Fatalf("the saved plan does not reopen: %+v", opened)
	}
	sameDocument(t, "reopened plan", opened.Plan.Plan, decoded)
	if opened.Plan.Output != "authored-plan.json" || opened.Plan.Digest != report.RulesSHA256 {
		t.Fatalf("reopening names another plan: %+v", opened.Plan)
	}

	// A plan authored beside the evidence rather than in the window — the
	// harness wrote plan.json the way the transform page shows one — reopens
	// as the decoder reads it, and reopening leaves its bytes as they were.
	authoredBeside := mustRead(t, filepath.Join(root, "plan.json"))
	besideDecoded, err := transform.DecodePlan(authoredBeside)
	if err != nil {
		t.Fatal(err)
	}
	reopened := app.OpenTransformPlan(root, "plan.json")
	if reopened.State != desktop.Empty || reopened.Plan == nil {
		t.Fatalf("a plan declaring no step reopens as empty: %+v", reopened)
	}
	sameDocument(t, "plan authored beside the evidence", reopened.Plan.Plan, besideDecoded)
	if !bytes.Equal(mustRead(t, filepath.Join(root, "plan.json")), authoredBeside) {
		t.Fatal("reopening a plan changed its bytes")
	}

	previewed := previewed(t, app, desktop.TransformRequest{
		Workspace: root, Case: "incident", Identity: identity, Rules: "rules.json", Plan: "authored-plan.json",
	})
	stdout, stderr, err := transformCommand(t, root, "rules.json", "authored-plan.json")
	if err != nil {
		t.Fatalf("readmit transform refused the window's plan: %v %s", err, stderr)
	}
	window, err := transform.JSON(previewed.Preview)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(window, []byte(stdout)) {
		t.Fatalf("the window's preview is not the command line's:\nwindow  %s\ncommand %s", window, stdout)
	}
	// The booking, the reschedule, its repetition and the acknowledgement.
	if previewed.Preview.Summary.Entries != 4 || previewed.Preview.Summary.Copies != 1 {
		t.Fatalf("the repeated reschedule is one copy in a sequence of four: %+v", previewed.Preview.Summary)
	}
}

// A plan the decoder refuses — a step carrying a member another operator uses,
// or a contract version this release does not read — is refused when the
// window reopens it, when it previews it and when the command line previews
// it, in one sentence each time. Authoring the same step is refused by the
// same decoder before anything is written.
func TestAPlanTheDecoderRefusesIsRefusedEverywhereInOneSentence(t *testing.T) {
	app, root, identity := transformWorkspace(t, "")
	valid, err := transform.DecodePlan(mustRead(t, filepath.Join(root, "plan.json")))
	if err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct {
		name, document, reason string
	}{
		{"hand-edited.json",
			`{"schema":"readmit-transform-plan/v1","case":"` + valid.Case + `","rules":"` + valid.Rules +
				`","steps":[{"operator":"shift-dates/v1","shift":"24h","entry":"t000001"}]}`,
			"a transformation step carries only the members its operator declares"},
		{"later-release.json",
			`{"schema":"readmit-transform-plan/v2","case":"` + valid.Case + `","rules":"` + valid.Rules + `","steps":[]}`,
			"transformation plan declares a contract version this release does not read"},
	} {
		writeDocument(t, root, refused.name, refused.document)
		opened := app.OpenTransformPlan(root, refused.name)
		if opened.State != desktop.Failed || opened.Reason != refused.reason || opened.Plan != nil {
			t.Fatalf("reopening %s: %+v, want %q", refused.name, opened, refused.reason)
		}
		preview := app.PreviewTransformation(desktop.TransformRequest{
			Workspace: root, Case: "incident", Identity: identity, Rules: "rules.json", Plan: refused.name,
		})
		if preview.State != desktop.Failed || preview.Reason != refused.reason || preview.Transformation != nil {
			t.Fatalf("previewing %s: %+v, want %q", refused.name, preview, refused.reason)
		}
		stdout, stderr, err := transformCommand(t, root, "rules.json", refused.name)
		if err == nil || cli.ExitCode(err) != 1 || stdout != "" || stderr != "readmit: "+refused.reason+"\n" {
			t.Fatalf("readmit transform over %s: %v, stdout %q stderr %q", refused.name, err, stdout, stderr)
		}
	}

	before := bytesUnder(t, root)
	saved := app.SaveTransformPlan(desktop.TransformPlanRequest{
		Workspace: root, Case: "incident", Identity: identity, Rules: "rules.json",
		Steps:  []transform.Step{{Operator: transform.ShiftDates, Shift: "24h", Entry: "t000001"}},
		Output: "never-written.json",
	})
	if saved.State != desktop.Failed || saved.Reason != "a transformation step carries only the members its operator declares" || saved.Plan != nil {
		t.Fatalf("authoring a step the decoder refuses: %+v", saved)
	}
	if after := bytesUnder(t, root); !maps.EqualFunc(before, after, bytes.Equal) {
		t.Fatalf("a refused save wrote something: %v", entriesOf(t, root))
	}
}
