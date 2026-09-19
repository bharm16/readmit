package tests

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/reproducer"
)

// chosenFolder stands in for the host's native folder dialog.
type chosenFolder string

func (c chosenFolder) ChooseFolder(string) (string, error) { return string(c), nil }

func desktopApp(t *testing.T, folder string) *desktop.App {
	t.Helper()
	return desktop.New(chosenFolder(folder), filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"))
}

// The desktop shell and the command line are two entry points into one engine.
// Neither reimplements the generator, so the sample workspace the desktop
// writes must be byte-identical to the family `readmit synth` writes from the
// same declared inputs, down to every manifest, record, payload and identity.
func TestDesktopSampleWorkspaceIsByteIdenticalToTheCommandLineFamily(t *testing.T) {
	command := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(command))

	parent := t.TempDir()
	result := desktopApp(t, parent).CreateSampleWorkspace()
	if result.State != desktop.Completed || result.Workspace == nil {
		t.Fatalf("sample workspace: %+v", result)
	}

	shell := synthTree(t, result.Workspace.Root)
	reference := synthTree(t, command)
	if len(shell) != len(reference) {
		t.Fatalf("the two entry points wrote different files: %d and %d", len(shell), len(reference))
	}
	for name, want := range reference {
		got, present := shell[name]
		if !present {
			t.Fatalf("the desktop sample workspace is missing %s", name)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs between the desktop shell and the command line", name)
		}
	}
}

// Opening a case in the shell must be the same verification `timeline`
// performs, reported as typed values instead of prose.
func TestDesktopCaseVerificationAgreesWithTheCommandLine(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed || created.Workspace == nil {
		t.Fatalf("sample workspace: %+v", created)
	}
	root := created.Workspace.Root

	for _, name := range []string{"regression", "cancellation", "invalid"} {
		opened := app.OpenCase(root, name)
		if opened.State != desktop.Completed || opened.Case == nil {
			t.Fatalf("desktop open %s: %+v", name, opened)
		}
		stdout, stderr, err := run(t, "timeline", filepath.Join(root, name))
		if err != nil || stderr != "" {
			t.Fatalf("timeline %s: %v %s", name, err, stderr)
		}
		for _, want := range []string{
			"Bundle: " + opened.Case.Identity,
			"Schema: " + opened.Case.Schema,
			"Provenance: " + opened.Case.Provenance,
			fmt.Sprintf("Sources: %d", opened.Case.Sources),
			fmt.Sprintf("Occurrences: %d", opened.Case.Occurrences),
			fmt.Sprintf("Messages: %d", opened.Case.Messages),
			fmt.Sprintf("ACKs: %d", opened.Case.Acknowledgements),
			fmt.Sprintf("Unparsed: %d", opened.Case.Unparsed),
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("timeline %s disagrees with the desktop facade about %q:\n%s", name, want, stdout)
			}
		}
	}

	// Damaged evidence is refused identically by both entry points.
	if err := os.WriteFile(filepath.Join(root, "invalid", "identity.sha256"), []byte("0000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if opened := app.OpenCase(root, "invalid"); opened.State != desktop.Failed || opened.Case != nil {
		t.Fatalf("the desktop facade accepted damaged evidence: %+v", opened)
	}
	if _, _, err := run(t, "timeline", filepath.Join(root, "invalid")); err == nil {
		t.Fatal("the command line accepted damaged evidence")
	}
}

// The desktop shell is a separate module with a webview and cgo. The released
// command line must stay a static build that never reaches it.
func TestCommandLineReleaseNeverReachesTheDesktopShell(t *testing.T) {
	packages := goCommand(t, nil, "list", "-deps", "../cmd/readmit")
	for _, forbidden := range []string{"wails", "github.com/bharm16/readmit/internal/desktop"} {
		if strings.Contains(packages, forbidden) {
			t.Fatalf("the released command line depends on %s", forbidden)
		}
	}
	binary := filepath.Join(t.TempDir(), "readmit-static")
	goCommand(t, []string{"CGO_ENABLED=0"}, "build", "-o", binary, "../cmd/readmit")
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("the command line no longer builds without cgo: %v", err)
	}
}

// The desktop module keeps its dependency graph out of the released module, so
// adding a webview dependency cannot change what the command line resolves.
func TestDesktopDependenciesStayOutOfTheReleasedModule(t *testing.T) {
	released, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(released), "wails") {
		t.Fatal("the released module requires the desktop webview dependency")
	}
	shell, err := os.ReadFile("../desktop/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"module github.com/bharm16/readmit/desktop", "github.com/wailsapp/wails/v2", "replace github.com/bharm16/readmit => ../"} {
		if !strings.Contains(string(shell), want) {
			t.Fatalf("the desktop module no longer declares %q", want)
		}
	}
}

// The recent workspace list is local shell state. It records folders the person
// opened and nothing read out of them.
func TestRecentWorkspacesRecordFoldersAndNoEvidence(t *testing.T) {
	store := filepath.Join(t.TempDir(), "recent.json")
	parent := t.TempDir()
	app := desktop.New(chosenFolder(parent), store, filepath.Join(filepath.Dir(store), "filters.json"), filepath.Join(filepath.Dir(store), "session.json"))
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed {
		t.Fatalf("sample workspace: %+v", created)
	}
	recent := app.RecentWorkspaces()
	if recent.State != desktop.Completed || !reflect.DeepEqual(recent.Roots, []string{created.Workspace.Root}) {
		t.Fatalf("the sample workspace was not recorded for reopening: %+v", recent)
	}
	stored, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MSH", "SCH", "SYNTH-", "regression", "identity"} {
		if strings.Contains(string(stored), leaked) {
			t.Fatalf("the recent workspace list recorded %q: %s", leaked, stored)
		}
	}
	// Reopening from the recorded folder returns the same workspace.
	reopened := desktop.New(chosenFolder(""), store, filepath.Join(filepath.Dir(store), "filters.json"), filepath.Join(filepath.Dir(store), "session.json")).OpenWorkspace(recent.Roots[0])
	if reopened.State != desktop.Completed || reopened.Workspace == nil || reopened.Workspace.Root != created.Workspace.Root {
		t.Fatalf("a recorded workspace could not be reopened: %+v", reopened)
	}
	if len(reopened.Workspace.Artifacts) != len(created.Workspace.Artifacts) {
		t.Fatalf("reopening changed the listing: %+v", reopened.Workspace.Artifacts)
	}
}

func goCommand(t *testing.T, environment []string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", args...)
	command.Env = append(os.Environ(), environment...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go %s: %v %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

// The command line and the desktop shell are two entry points into one engine,
// and the index is the one search path over a case. So the occurrences a
// filtered message grid renders must be exactly the ones `readmit index search`
// reports for the same question over the same index, and the grid must say how
// many of the case it left out.
func TestDesktopGridRendersExactlyWhatTheCommandLineIndexSearchFinds(t *testing.T) {
	const occurrences = 120
	const wanted = "MRN-0042^^^READMIT^MR"

	workspace := t.TempDir()
	evidence := filepath.Join(workspace, "incident")
	if _, stderr, err := run(t, "capture", indexCorpus(t, occurrences), "--output", evidence); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	written := filepath.Join(workspace, "incident.index.json")
	if _, stderr, err := run(t, "index", "build", evidence, "--output", written,
		"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "index", "search", evidence, written, "--field", "PID-3", "--equals", wanted)
	if err != nil || stderr != "" {
		t.Fatalf("index search: %v %s", err, stderr)
	}
	reported := reportedOccurrences(stdout)
	if len(reported) != 1 {
		t.Fatalf("the command line found %v, want exactly one occurrence:\n%s", reported, stdout)
	}

	app := desktopApp(t, workspace)
	saved := app.SaveFilter(grid.Filter{Name: "one patient", Fields: []grid.FieldPredicate{
		{Selector: "PID[1]-3[1]", Match: index.Equals, Term: wanted},
	}})
	if saved.State != desktop.Completed || saved.Selected != "one patient" {
		t.Fatalf("the filter was not saved: %+v", saved)
	}
	result := app.OpenGrid(workspace, "incident", "incident.index.json", 0, 50)
	if result.State != desktop.Completed || result.Grid == nil {
		t.Fatalf("the grid did not open: %+v", result)
	}
	rendered := make([]string, 0, len(result.Grid.Rows))
	for _, row := range result.Grid.Rows {
		rendered = append(rendered, row.ID)
	}
	if !reflect.DeepEqual(rendered, reported) {
		t.Fatalf("the grid rendered %v and the command line found %v", rendered, reported)
	}

	// The case holds one occurrence this release cannot decode, and the grid
	// separates that from the records the filter simply removed.
	if result.Grid.Total != occurrences+1 || result.Grid.Matched != 1 {
		t.Fatalf("the grid did not describe the whole case: %+v", result.Grid)
	}
	if result.Grid.Excluded != occurrences || result.Grid.Undecodable != 1 {
		t.Fatalf("the grid did not name what it excluded: %+v", result.Grid)
	}
	if encoded, err := json.Marshal(result); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(encoded), "MRN-") || strings.Contains(string(encoded), "corpus.mllp") {
		t.Fatalf("the grid disclosed evidence: %s", encoded)
	}
}

// reportedOccurrences reads the occurrence IDs out of an `index search` report.
// Each match is one indented line beginning with the occurrence it names.
func reportedOccurrences(stdout string) []string {
	found := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, "  s") {
			continue
		}
		found = append(found, strings.Fields(line)[0])
	}
	return found
}

// The whole reproducer delivery, across both entry points. The window extracts
// a reproducer out of a captured incident — the reschedule a person selected,
// the booking its declared identity requires, and one edited field — and the
// command line then verifies that derived case and registers it as a revision
// of the evidence it came from. Neither side reimplements the other, and the
// original is byte-identical afterwards.
func TestDesktopReproducerIsVerifiedAndRegisteredByTheCommandLine(t *testing.T) {
	workspace := t.TempDir()
	evidence := filepath.Join(workspace, "incident")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/synth-v1-regression.mllp", "--output", evidence); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	before := caseFiles(t, evidence)

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "incident")
	if opened.State != desktop.Completed {
		t.Fatalf("the case did not verify: %+v", opened)
	}
	build := desktop.ReproducerRequest{Workspace: workspace, Case: "incident", Identity: opened.Case.Identity}
	for _, step := range []reproducer.Step{
		{Operator: reproducer.SelectOccurrence, Occurrence: "s0001-e000002"},
		{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}},
		{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
	} {
		build.Step = step
		result := app.EditReproducer(build)
		if result.State != desktop.Completed {
			t.Fatalf("the window refused a step it supports: %+v", result)
		}
		build.Plan = result.Reproducer.Plan
	}
	build.Step, build.Output = reproducer.Step{}, "incident-reproducer"
	built := app.BuildReproducer(build)
	if built.State != desktop.Completed || len(built.Reproducer.Resolution.Occurrences) != 2 {
		t.Fatalf("the reproducer was not written: %+v", built)
	}
	if !reflect.DeepEqual(before, caseFiles(t, evidence)) {
		t.Fatal("building a reproducer changed the evidence it was derived from")
	}

	// The command line verifies what the window wrote, through the same reader
	// every other case goes through.
	derived := filepath.Join(workspace, "incident-reproducer", reproducer.CaseName)
	stdout, stderr, err := run(t, "timeline", derived)
	if err != nil || stderr != "" {
		t.Fatalf("timeline: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, built.Reproducer.Identity) || !strings.Contains(stdout, "Occurrences: 2") {
		t.Fatalf("timeline did not report the reproducer the window wrote:\n%s", stdout)
	}

	// And a project records where it came from, reading the operation out of the
	// derivation the derived case declares rather than from anything typed here.
	project := filepath.Join(workspace, "investigation")
	if _, stderr, err := run(t, "project", "init", "--output", project, "--title", "Scheduling", "--interface-version", "v1"); err != nil || stderr != "" {
		t.Fatalf("project init: %v %s", err, stderr)
	}
	copyTree(t, evidence, filepath.Join(project, "incident"))
	copyTree(t, derived, filepath.Join(project, "incident-reproducer"))
	added, stderr, err := run(t, "project", "add", project, "incident", "--title", "Original incident")
	if err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	stdout, stderr, err = run(t, "project", "revise", project, "incident-reproducer", "--parent", "incident")
	if err != nil || stderr != "" {
		t.Fatalf("project revise: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Schema: readmit-case/v3",
		"Provenance: derived",
		"Operation: " + reproducer.Derivation,
		"Parent identity: " + field(t, added, "Identity: "),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project revise omitted %q:\n%s", want, stdout)
		}
	}
}

// caseFiles reads every file of a case directory, so a test can assert that
// deriving from it changed nothing.
func caseFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(name)
		relative, _ := filepath.Rel(root, name)
		files[relative] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// copyTree copies one artifact directory, which is what an operator does before
// registering derived evidence as a revision of a project.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		t.Fatal(err)
	}
}
