package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The demo is a named project of the frozen synthetic sample, kept in the
// application's own folder beside the shell's documents: the sample family,
// the practice endpoint and the sample index, with a project document of its
// own. It is created once and opened from then on; nothing is ever written
// over it or over a project a person made.

// demoFolder is the folder, below the shell documents' folder, the demo
// project is created in.
const demoFolder = "demo"

// DemoTitle is the demo project's name.
const DemoTitle = "Demo"

// DemoStepID names one step of the demo task.
type DemoStepID string

// The steps of the demo task, in the order they are walked.
const (
	DemoOpenMessages    DemoStepID = "open-messages"
	DemoCreateTest      DemoStepID = "create-test"
	DemoRunDefective    DemoStepID = "run-defective"
	DemoViewFailedCheck DemoStepID = "view-failed-check"
	DemoRunFixed        DemoStepID = "run-fixed"
	DemoCompare         DemoStepID = "compare"
)

// Where a demo step's completion is read from. A project step is read back
// from the evidence the project holds, as the guided sample always was. A
// session step completes on a read that writes nothing — opening the
// messages, a failed check, a comparison — so this facade never reports it
// done: the window marks it done only from the successful result of the call
// the step names, never from a click.
type DemoEvidence string

const (
	DemoProjectEvidence DemoEvidence = "project"
	DemoSessionEvidence DemoEvidence = "session"
)

// DemoStep is one step of the demo task. Entry and Status are what the
// project holds for a project step that is done: the saved test, or the run
// and its verdict; Ref is that test or run as the project's object. Output is
// the new entry a run step not done yet writes its practice run into.
type DemoStep struct {
	ID       DemoStepID   `json:"id"`
	Title    string       `json:"title"`
	Evidence DemoEvidence `json:"evidence"`
	Done     bool         `json:"done"`
	Entry    string       `json:"entry,omitzero"`
	Status   string       `json:"status,omitzero"`
	Ref      *ItemRef     `json:"ref,omitzero"`
	Output   string       `json:"output,omitzero"`
}

// DemoProgress is the demo task over one project: the sample case, its
// identity and its object, the practice endpoint a test names, the saved
// test once there is one — the entry of the project RunPractice executes —
// the test the demo supplies for the ordinary New test editor to open with,
// and the steps.
type DemoProgress struct {
	Case     string           `json:"case"`
	Identity string           `json:"identity"`
	CaseRef  *ItemRef         `json:"case_ref,omitzero"`
	Target   string           `json:"target"`
	Spec     string           `json:"spec,omitzero"`
	Test     testauthor.Draft `json:"test"`
	Steps    []DemoStep       `json:"steps"`
}

// DemoProgressResult is Completed over the demo project, and Empty over a
// project that is not a demo.
type DemoProgressResult struct {
	State  State         `json:"state"`
	Reason string        `json:"reason,omitzero"`
	Demo   *DemoProgress `json:"demo,omitzero"`
}

func (r *DemoProgressResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenDemoProject opens the demo project, creating it first when the
// application's folder does not hold it yet. It is the frozen synthetic
// sample: generated, never imported, finite, and bound to the loopback
// practice receiver, so like CreateSampleWorkspace and the sample capture it
// needs no activation. Recording the catalog of the demo, which writes that
// project's own catalog document and nothing else, is admitted under the
// same exemption, and only while its sample case is the frozen one.
func (a *App) OpenDemoProject() ProjectOpenResult {
	return run(a, false, false, func(ctx context.Context) ProjectOpenResult {
		root := a.demoPath()
		if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
			if declined := a.createDemo(ctx, root); declined.state != "" {
				return declined.namedProject()
			}
		} else if err != nil {
			return ProjectOpenResult{State: refusalState(err), Reason: "the demo project cannot be inspected"}
		}
		if _, err := project.Open(root); err != nil {
			return ProjectOpenResult{State: Failed, Reason: "the demo project's folder is incomplete or was changed; it is left exactly as it is"}
		}
		return a.openNamed(ctx, root, frozenDemo(root) && a.needsRecording(root))
	})
}

// demoPath is where the demo project is kept, below the shell documents'
// folder.
func (a *App) demoPath() string { return filepath.Join(a.documents.Folder, demoFolder, SampleName) }

// demoRoot reports whether a resolved project folder is the demo project.
func (a *App) demoRoot(root string) bool {
	demo, _ := resolveFolder(a.demoPath())
	return demo != "" && demo == root
}

// createDemo writes the demo project into a new folder: the frozen family,
// prepared as the sample workspace is, and then its project document. A
// failure removes what this call created, since nothing else is there yet.
func (a *App) createDemo(ctx context.Context, root string) refusal {
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return refusal{refusalState(err), "the folder the demo project is kept in could not be created"}
	}
	if ctx.Err() != nil {
		return cancelledRefusal
	}
	if _, err := synth.Write(root, sampleInputs); err != nil {
		return probeWriteFailure(parent, "this account cannot create the demo project", "the demo project could not be created")
	}
	err := guide.PrepareWorkspace(ctx, root)
	if err == nil {
		err = project.WriteDocument(root, project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: DemoTitle}})
	}
	if err != nil {
		os.RemoveAll(root)
		return probeWriteFailure(parent, "this account cannot create the demo project", "the demo project could not be completed")
	}
	return refusal{}
}

// frozenDemo reports whether the project in root holds the frozen sample
// case, unmodified.
func frozenDemo(root string) bool {
	source, err := bundle.Open(artifactpath.JoinReference(root, guide.CaseName))
	return err == nil && source.Identity == guide.CaseIdentity
}

// DemoProgress reads the demo task over one project, back from what the
// project holds, the tests its catalog saved included. The window
// keeps no tutorial state and nothing here writes, so a project reopened in
// a new window reports exactly what was really done. The three session steps
// are never done here; see DemoSessionEvidence.
func (a *App) DemoProgress(request RequestContext) DemoProgressResult {
	return run(a, false, false, func(ctx context.Context) DemoProgressResult {
		loaded, declined := a.loadCatalog(ctx, request, false)
		if loaded == nil {
			return DemoProgressResult{State: declined.state, Reason: declined.reason}
		}
		// A catalog saves each revision as a new entry of the project, so the
		// guide finds a saved test among the entries; the current revision
		// of each saved test is preferred over an earlier one.
		var current []string
		for _, item := range loaded.document.Items {
			if item.Kind != string(TestItem) || item.RemovedAt != "" || item.Current() == nil {
				continue
			}
			if paths, availability, _ := loaded.backing(item); availability == ItemAvailable {
				current = append(current, filepath.Base(paths[primaryRole(TestItem)]))
			}
		}
		progress, err := guide.ReadWith(loaded.root, current)
		if err != nil {
			return DemoProgressResult{State: Failed, Reason: err.Error()}
		}
		if !progress.Steps[0].Done {
			return DemoProgressResult{State: Empty, Reason: "this project is not the demo"}
		}
		// The project's object of each entry a step names.
		refOf := func(kind ItemKind, entry string) *ItemRef {
			for _, item := range loaded.document.Items {
				if item.Kind != string(kind) || item.RemovedAt != "" {
					continue
				}
				if item.Entry == entry {
					return &ItemRef{Kind: kind, ID: item.ID}
				}
				if paths, availability, _ := loaded.backing(item); availability == ItemAvailable && item.Current() != nil &&
					filepath.Base(paths[primaryRole(kind)]) == entry {
					return &ItemRef{Kind: kind, ID: item.ID, Revision: item.RevisionLabel()}
				}
			}
			return nil
		}
		step := func(id DemoStepID, title string, from *guide.Step, kind ItemKind, output string) DemoStep {
			if from == nil {
				return DemoStep{ID: id, Title: title, Evidence: DemoSessionEvidence}
			}
			shown := DemoStep{ID: id, Title: title, Evidence: DemoProjectEvidence, Done: from.Done, Entry: from.Entry, Status: from.Status}
			if from.Done && from.Entry != "" {
				shown.Ref = refOf(kind, from.Entry)
			} else if output != "" {
				shown.Output = freshEntry(loaded.root, output)
			}
			return shown
		}
		supplied, err := suppliedTest(progress.Identity)
		if err != nil {
			return DemoProgressResult{State: Failed, Reason: err.Error()}
		}
		return DemoProgressResult{State: Completed, Demo: &DemoProgress{
			Case: progress.Case, Identity: progress.Identity, CaseRef: refOf(CaseItem, progress.Case), Target: guide.TargetName, Spec: progress.Spec, Test: supplied,
			Steps: []DemoStep{
				step(DemoOpenMessages, "Open sample messages", nil, "", ""),
				step(DemoCreateTest, "Create supplied test", &progress.Steps[1], TestItem, ""),
				step(DemoRunDefective, "Run defective receiver", &progress.Steps[2], RunItem, "defective-run"),
				step(DemoViewFailedCheck, "View failed check", nil, "", ""),
				step(DemoRunFixed, "Run fixed receiver", &progress.Steps[3], RunItem, "fixed-run"),
				step(DemoCompare, "Compare results", nil, "", ""),
			},
		}}
	})
}

// suppliedTest is the test the demo supplies over its sample case: the
// reschedule and its update sent to the practice receiver, expecting one
// appointment in its ledger. The defective receiver keeps two.
func suppliedTest(identity string) (testauthor.Draft, error) {
	draft, err := testauthor.NewDraft(guide.CaseName, identity)
	if err != nil {
		return testauthor.Draft{}, err
	}
	appointments := 1
	draft.Name, draft.Messages, draft.Target = "Rescheduling updates the original appointment", []string{"s0001-e000001", "s0001-e000002"}, guide.TargetName
	draft.Boundary, draft.Observation = testrunner.LedgerBoundary, "practice-observation.json"
	draft.Reset = "Start a fresh practice receiver with an empty ledger before each run."
	draft.Expectations = []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &appointments}}
	return draft, nil
}

// freshEntry is name, or name with the first number after it that makes it
// an entry the project does not hold yet.
func freshEntry(root, name string) string {
	candidate := name
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(root, candidate)); errors.Is(err, fs.ErrNotExist) {
			return candidate
		}
		candidate = name + "-" + strconv.Itoa(n)
	}
}
