package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
)

// chosenFolder stands in for the host's native folder dialog.
type chosenFolder string

func (c chosenFolder) ChooseFolder(string) (string, error) { return string(c), nil }

func desktopApp(t *testing.T, folder string) *desktop.App {
	t.Helper()
	return desktop.New(chosenFolder(folder), filepath.Join(t.TempDir(), "recent.json"))
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
	app := desktop.New(chosenFolder(parent), store)
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
	reopened := desktop.New(chosenFolder(""), store).OpenWorkspace(recent.Roots[0])
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
