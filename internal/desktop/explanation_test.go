package desktop_test

// The run-explanation panel's own rules, beside the parity with `readmit
// explain`: its inputs are chosen through the host's dialogs and must be
// entries of the open workspace, each entry it is handed is refused before
// anything is read unless it is one, a cancelled explanation decides nothing,
// and an explanation is refused as busy while another operation holds the slot.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// explainableWorkspace holds a copy of the retained post-fix result and a set
// its run passes.
func explainableWorkspace(t *testing.T) string {
	t.Helper()
	root := resolved(t, t.TempDir())
	copyEntry(t, filepath.Join(nativeAcceptance, "post-fix"), filepath.Join(root, "post-fix"))
	writeDocument(t, root, "accepted.json", reviewedSet)
	return root
}

const (
	outsideTheWorkspace = "an explanation reads entries of the open workspace; advanced selection cannot reach outside it"
	notARun             = "a retained run is one folder of the open workspace, or one job inside a suite execution's runs, and never a symbolic link"
)

func TestChooseExplanationInputKinds(t *testing.T) {
	root := explainableWorkspace(t)
	c := &chooser{folder: filepath.Join(root, "post-fix"), files: []string{filepath.Join(root, "accepted.json")}}
	app := newApp(t, c)

	if got := app.ChooseExplanationInput(root, "run"); got.State != desktop.Completed || got.Kind != "run" || got.Entry != "post-fix" {
		t.Fatalf("run: %+v", got)
	}
	for _, kind := range []string{"assertions", "before", "before-source", "after", "after-source"} {
		if got := app.ChooseExplanationInput(root, kind); got.State != desktop.Completed || got.Kind != kind || got.Entry != "accepted.json" {
			t.Fatalf("%s: %+v", kind, got)
		}
	}
	if got := strings.Join(c.titles, "|"); got != "Choose a retained run to explain|Choose the assertion set to re-decide|"+
		"Choose the completion record of the observation before the run|Choose the observation source the observation before the run read|"+
		"Choose the completion record of the observation after the run|Choose the observation source the observation after the run read" {
		t.Fatalf("dialog titles: %s", got)
	}
	// A job of a suite execution is chosen inside the runs folder that retains
	// it, and named as the run history names it.
	if err := os.MkdirAll(filepath.Join(root, "nightly-001", "runs", "booking-one"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.folder = filepath.Join(root, "nightly-001", "runs", "booking-one")
	if got := app.ChooseExplanationInput(root, "run"); got.State != desktop.Completed || got.Entry != "nightly-001/runs/booking-one" {
		t.Fatalf("a suite job: %+v", got)
	}
	// What was chosen is what is explained.
	if explained := app.ExplainRun(desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "accepted.json"}); explained.State != desktop.Completed || explained.Explanation.Verdict != "pass" {
		t.Fatalf("the chosen run and set were not explained: %+v", explained)
	}

	elsewhere := explainableWorkspace(t)
	for name, refused := range map[string]struct {
		kind   string
		folder string
		files  []string
		reason string
	}{
		"a folder inside a result":          {kind: "run", folder: filepath.Join(root, "post-fix", "run"), reason: notARun},
		"a set inside a result":             {kind: "assertions", files: []string{filepath.Join(root, "post-fix", "result.json")}, reason: outsideTheWorkspace},
		"a run in another folder":           {kind: "run", folder: filepath.Join(elsewhere, "post-fix"), reason: outsideTheWorkspace},
		"a set in another folder":           {kind: "assertions", files: []string{filepath.Join(elsewhere, "accepted.json")}, reason: outsideTheWorkspace},
		"the workspace itself as a run":     {kind: "run", folder: root, reason: outsideTheWorkspace},
		"a document where a run belongs":    {kind: "run", folder: filepath.Join(root, "accepted.json"), reason: notARun},
		"a folder where a document belongs": {kind: "after", files: []string{filepath.Join(root, "post-fix")}, reason: "the document is a regular file of the open workspace, never a symbolic link"},
		"two sets at once":                  {kind: "assertions", files: []string{filepath.Join(root, "accepted.json"), filepath.Join(root, "accepted.json")}, reason: "choose exactly one file"},
	} {
		c.folder, c.files = refused.folder, refused.files
		if got := app.ChooseExplanationInput(root, refused.kind); got.State != desktop.Failed || got.Reason != refused.reason || got.Entry != "" {
			t.Errorf("%s: %+v, want the refusal %q", name, got, refused.reason)
		}
	}

	c.folder, c.files = "", nil
	for _, kind := range []string{"run", "assertions"} {
		if got := app.ChooseExplanationInput(root, kind); got.State != desktop.Cancelled || got.Entry != "" {
			t.Fatalf("a dismissed %s dialog: %+v", kind, got)
		}
	}
	opened := len(c.titles)
	if got := app.ChooseExplanationInput(root, "report"); got.State != desktop.Failed || got.Reason != "that is not an input an explanation is chosen for" || len(c.titles) != opened {
		t.Fatalf("an unknown input: %+v, dialogs %d then %d", got, opened, len(c.titles))
	}
	if got := app.ChooseExplanationInput(filepath.Join(root, "accepted.json"), "run"); got.State != desktop.Failed || len(c.titles) != opened {
		t.Fatalf("a workspace that is not a folder: %+v", got)
	}
}

// Every entry is refused by the panel's own sentence before anything is read
// unless it names what it is handed as: a run as one folder of the workspace
// or a job of a suite execution's runs, and each document as one regular file
// of it.
func TestAnExplanationIsHandedOnlyEntriesOfTheWorkspace(t *testing.T) {
	root := explainableWorkspace(t)
	app := workspaceApp(t)

	const notASet = "an assertion set is one regular file of the open workspace, never a symbolic link"
	for name, refused := range map[string]struct {
		request desktop.RunExplanationRequest
		reason  string
	}{
		"a `..` escape":               {desktop.RunExplanationRequest{Run: filepath.Join("..", "post-fix"), Assertions: "accepted.json"}, notARun},
		"an absolute run":             {desktop.RunExplanationRequest{Run: filepath.Join(root, "post-fix"), Assertions: "accepted.json"}, notARun},
		"a run nobody retained":       {desktop.RunExplanationRequest{Run: "never-run", Assertions: "accepted.json"}, notARun},
		"a document named as a run":   {desktop.RunExplanationRequest{Run: "accepted.json", Assertions: "accepted.json"}, notARun},
		"a folder inside a result":    {desktop.RunExplanationRequest{Run: "post-fix/run", Assertions: "accepted.json"}, notARun},
		"an absolute set":             {desktop.RunExplanationRequest{Run: "post-fix", Assertions: filepath.Join(root, "accepted.json")}, notASet},
		"a set nobody wrote":          {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "missing.json"}, notASet},
		"a folder named as a set":     {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "post-fix"}, notASet},
		"a completion outside":        {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "accepted.json", After: filepath.Join("..", "after.json"), AfterSource: "accepted.json"}, "a completion record is one regular file of the open workspace, never a symbolic link"},
		"a source that is not a file": {desktop.RunExplanationRequest{Run: "post-fix", Assertions: "accepted.json", Before: "accepted.json", BeforeSource: "post-fix"}, "an observation source is one regular file of the open workspace, never a symbolic link"},
	} {
		request := refused.request
		request.Workspace = root
		if got := app.ExplainRun(request); got.State != desktop.Failed || got.Reason != refused.reason || got.Explanation != nil {
			t.Errorf("%s: %+v, want the refusal %q", name, got, refused.reason)
		}
	}
	for name, request := range map[string]desktop.RunExplanationRequest{
		"no run": {Workspace: root, Assertions: "accepted.json"},
		"no set": {Workspace: root, Run: "post-fix"},
	} {
		if got := app.ExplainRun(request); got.State != desktop.Empty || got.Explanation != nil {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A cancelled explanation decides nothing and retains nothing, so explaining
// again decides what it would have. While another operation holds the slot an
// explanation is refused as busy rather than racing it.
func TestACancelledExplanationDecidesNothingAndExplainingAgainDecides(t *testing.T) {
	root := explainableWorkspace(t)
	request := desktop.RunExplanationRequest{Workspace: root, Run: "post-fix", Assertions: "accepted.json"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := desktop.ExplainRunWithinForTest(ctx, request)
	if cancelled.State != desktop.Cancelled || cancelled.Explanation != nil || cancelled.Reason != "the explanation was cancelled; it retained nothing, so explaining again decides exactly what it would have" {
		t.Fatalf("a cancelled explanation: %+v", cancelled)
	}

	app := workspaceApp(t)
	release, held := desktop.HoldSlotForTest(app, "durable-run")
	if !held {
		t.Fatal("the slot was not free")
	}
	if busy := app.ExplainRun(request); busy.State != desktop.Busy || busy.Explanation != nil {
		t.Fatalf("an explanation while a run holds the slot: %+v", busy)
	}
	release()
	again := app.ExplainRun(request)
	if again.State != desktop.Completed || again.Explanation.Verdict != "pass" || again.Explanation.Passed != 3 {
		t.Fatalf("explaining again: %+v", again)
	}
}
