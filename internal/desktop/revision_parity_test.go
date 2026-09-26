package desktop_test

// A reproducer written in the window becomes a project revision through the
// operation `readmit project revise` runs. One project is copied and taken both
// ways a person can take it: in the window, where Register this revision places
// the derived case and records its lineage in one act, and on the command line,
// where the documented `cp -R …/case` step is followed by `readmit project
// revise`. Afterwards the two projects hold the same bytes, and the command line
// reads the window's project exactly as it reads its own. A registration the
// project refuses is refused in the command line's words and leaves the
// workspace as it was, and undoing a step builds exactly what the plan without
// that step builds.

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testlicense"
)

// revisionProject is a project whose one registered case is the incident, and
// the shell that reads it.
func revisionProject(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	registerCase(t, root, "incident", "Original incident")
	return app, root, incident.Identity
}

// withBooking is the reproducer the tests below write: the reschedule, and the
// booking it cannot be reproduced without, retained as its setup dependency.
var withBooking = []reproducer.Step{
	{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID},
	{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}},
}

// buildFrom writes one reproducer of the named case through the window and
// returns the entry it was written to.
func buildFrom(t *testing.T, app *desktop.App, root, name, identity, output string, steps ...reproducer.Step) string {
	t.Helper()
	request := desktop.ReproducerRequest{Workspace: root, Case: name, Identity: identity}
	for _, step := range steps {
		request.Step = step
		result := app.EditReproducer(request)
		if result.Reproducer == nil {
			t.Fatalf("the window refused a step it supports: %+v", result)
		}
		request.Plan = result.Reproducer.Plan
	}
	request.Step, request.Output = reproducer.Step{}, output
	if result := app.BuildReproducer(request); result.State != desktop.Completed {
		t.Fatalf("a reproducer this project supports was not written: %+v", result)
	}
	return output
}

// revise is `readmit project revise` under the test license, after the
// documented copy of a built reproducer's derived case into the project.
func revise(t *testing.T, root, built, name, parent string) (string, string, error) {
	t.Helper()
	copyEntry(t, filepath.Join(root, built, reproducer.CaseName), filepath.Join(root, name))
	return commandLine(t, "--operation-policy", testlicense.New(t), "project", "revise", root, name, "--parent", parent)
}

// projectShow is `readmit project show` with the project's own folder written
// as ROOT, so two copies of one project print the same text.
func projectShow(t *testing.T, root string) string {
	t.Helper()
	printed, stderr, err := commandLine(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	return strings.ReplaceAll(strings.ReplaceAll(printed, resolved(t, root), "ROOT"), root, "ROOT")
}

// Registering in the window records the revision `readmit project revise`
// records: the same editable document byte for byte, the same derived case
// beside it, and the same lineage `readmit project show` prints. The build
// folder a comparison opens is left exactly as it was.
func TestARevisionBuiltAndRegisteredInTheWindowIsTheOneProjectReviseRecords(t *testing.T) {
	app, windowRoot, identity := revisionProject(t)
	commandRoot := filepath.Join(t.TempDir(), filepath.Base(windowRoot))
	copyEntry(t, windowRoot, commandRoot)

	built := buildFrom(t, app, windowRoot, "incident", identity, "incident-reproducer", withBooking...)
	manifest, err := reproducer.Open(filepath.Join(windowRoot, built))
	if err != nil {
		t.Fatal(err)
	}
	build := workspaceState(t, filepath.Join(windowRoot, built))
	registered := app.RegisterRevision(desktop.RevisionRegistration{Workspace: windowRoot, Source: built, Name: "incident-revision", Parent: "incident"})
	if registered.State != desktop.Completed || registered.Overview == nil || len(registered.Overview.Revisions) != 1 {
		t.Fatalf("the window did not register the revision: %+v", registered)
	}

	buildFrom(t, app, commandRoot, "incident", identity, "incident-reproducer", withBooking...)
	printed, stderr, err := revise(t, commandRoot, "incident-reproducer", "incident-revision", "incident")
	if err != nil || stderr != "" {
		t.Fatalf("project revise: %v %s", err, stderr)
	}

	// Both projects now hold the same files with the same bytes: the build, the
	// placed derived case and the editable document that records its lineage.
	if window, command := workspaceState(t, windowRoot), workspaceState(t, commandRoot); !maps.Equal(window, command) {
		t.Fatalf("the window wrote\n%v\nand the command line wrote\n%v", window, command)
	}
	if window, command := projectShow(t, windowRoot), projectShow(t, commandRoot); window != command {
		t.Fatalf("project show reads the window's project as\n%s\nand the command line's as\n%s", window, command)
	}

	// What the window reports is what the command line reported registering,
	// and the derived case is the one the build wrote.
	entry := registered.Overview.Revisions[0]
	for _, want := range []string{
		"Revision registered: incident-revision\n",
		"Identity: " + entry.Identity + "\n",
		"Schema: " + entry.Schema + "\n",
		"Provenance: " + entry.Provenance + "\n",
		"Operation: " + entry.Operation + "\n",
		"Parent: " + entry.Parent + "\n",
		"Parent identity: " + identity + "\n",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("project revise printed no %q:\n%s", want, printed)
		}
	}
	if entry.Identity != manifest.Derived.Identity || entry.Operation != reproducer.Derivation ||
		entry.Parent != "incident" || entry.Evidence != operation.EvidenceVerified {
		t.Fatalf("the window registered %+v for a build of %s", entry, manifest.Derived.Identity)
	}
	if !maps.Equal(build, workspaceState(t, filepath.Join(windowRoot, built))) {
		t.Fatal("registering changed the build folder a comparison opens")
	}
}

// Every registration the project refuses is refused in the sentence the command
// line prints for it, and the copy the window placed for it is gone again, so
// the workspace is exactly as it was and the same name registers once the
// refusal is dealt with.
func TestARefusedRegistrationIsRefusedInTheCommandLinesWordsAndLeavesNothingBehind(t *testing.T) {
	app, root, identity := revisionProject(t)
	loose := writeCase(t, root, "loose", framed(repBooking)+framed(repReschedule))
	built := buildFrom(t, app, root, "incident", identity, "incident-reproducer", withBooking...)
	fromLoose := buildFrom(t, app, root, "loose", loose.Identity, "loose-reproducer",
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: "s0001-e000002"})
	if registered := app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: built, Name: "incident-revision", Parent: "incident"}); registered.State != desktop.Completed {
		t.Fatalf("the first registration was refused: %+v", registered)
	}
	// The same plan written again is the same derived evidence.
	again := buildFrom(t, app, root, "incident", identity, "incident-reproducer-again", withBooking...)

	for _, refused := range []struct {
		name         string
		registration desktop.RevisionRegistration
		reason       string
	}{
		{"a parent the project does not register",
			desktop.RevisionRegistration{Workspace: root, Source: fromLoose, Name: "loose-revision", Parent: "loose"},
			"a revision must be derived from a case or revision this project registers"},
		{"a parent that is no entry of the project",
			desktop.RevisionRegistration{Workspace: root, Source: built, Name: "orphan-revision", Parent: "absent"},
			"a case must be named by one directory entry of the project"},
		{"evidence the project already registers as a revision",
			desktop.RevisionRegistration{Workspace: root, Source: again, Name: "incident-revision-again", Parent: "incident"},
			"the same revision identity is registered twice"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			before := workspaceState(t, root)
			result := app.RegisterRevision(refused.registration)
			if result.State != desktop.Failed || result.Reason != refused.reason || result.Overview != nil {
				t.Fatalf("the window answered %+v", result)
			}
			if after := workspaceState(t, root); !maps.Equal(before, after) {
				t.Fatalf("a refused registration left the workspace changed:\n%v\n%v", before, after)
			}
			// The command line, over a copy of the same project, refuses the
			// same registration in the same words.
			copied := filepath.Join(t.TempDir(), filepath.Base(root))
			copyEntry(t, root, copied)
			registration := refused.registration
			_, stderr, err := revise(t, copied, registration.Source, registration.Name, registration.Parent)
			if err == nil || !strings.Contains(stderr, refused.reason) {
				t.Fatalf("project revise answered %v %q", err, stderr)
			}
		})
	}

	// Once the parent is registered, the name the refused attempt used is
	// free, and the same registration succeeds.
	registerCase(t, root, "loose", "Loose export")
	retried := app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: fromLoose, Name: "loose-revision", Parent: "loose"})
	if retried.State != desktop.Completed || len(retried.Overview.Revisions) != 2 {
		t.Fatalf("a registration the refused attempt left room for was refused: %+v", retried)
	}
}

// Undo removes the last step and replays the rest, so the reproducer an undone
// plan builds is byte for byte the one a plan that never had the step builds,
// and the command line registers it as the reproducer it declares.
func TestAnUndoneStepBuildsWhatThePlanWithoutItBuilds(t *testing.T) {
	app, root, identity := revisionProject(t)
	plan := reproducer.Plan{}
	var without desktop.ReproducerResult
	for _, step := range append(slices.Clone(withBooking), reproducer.Step{
		Operator: reproducer.SetField, Occurrence: repRescheduleID, Selector: "PID-3.1", Value: "MRN-REPRODUCER",
	}) {
		result := edit(t, app, root, identity, plan, step)
		if len(result.Reproducer.Plan.Steps) == len(withBooking) {
			without = result
		}
		plan = result.Reproducer.Plan
	}
	undone := app.UndoReproducer(request(root, identity, plan, reproducer.Step{}))
	if undone.State != desktop.Completed || !reflect.DeepEqual(undone.Reproducer, without.Reproducer) {
		t.Fatalf("undo resolved %+v where the plan without the step resolves %+v", undone, without)
	}

	build := request(root, identity, undone.Reproducer.Plan, reproducer.Step{})
	build.Output = "undone"
	built := app.BuildReproducer(build)
	if built.State != desktop.Completed {
		t.Fatalf("the undone plan was not written: %+v", built)
	}
	buildFrom(t, app, root, "incident", identity, "never-edited", withBooking...)
	if undoneBuild, direct := workspaceState(t, filepath.Join(root, "undone")), workspaceState(t, filepath.Join(root, "never-edited")); !maps.Equal(undoneBuild, direct) {
		t.Fatalf("the undone plan wrote\n%v\nand the plan without the step wrote\n%v", undoneBuild, direct)
	}

	printed, stderr, err := revise(t, root, "undone", "undone-revision", "incident")
	if err != nil || stderr != "" {
		t.Fatalf("project revise: %v %s", err, stderr)
	}
	for _, want := range []string{"Identity: " + built.Reproducer.Identity + "\n", "Operation: " + reproducer.Derivation + "\n"} {
		if !strings.Contains(printed, want) {
			t.Errorf("project revise printed no %q:\n%s", want, printed)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "undone", reproducer.ManifestName)); err != nil {
		t.Fatalf("the undone build has no manifest: %v", err)
	}
}
