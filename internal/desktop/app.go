// Package desktop is the typed application facade behind the native desktop
// shell. Every operation returns a typed result carrying exactly one explicit
// state, so a caller never reads prose, parses command output, or guesses what
// happened. Evidence decisions belong to the same internal packages the command
// line calls: this package identifies, verifies and counts nothing itself.
package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/synth"
)

// SampleName is the fixed folder the sample workspace is created in, inside the
// folder the person chooses.
const SampleName = "readmit-sample"

// MaxWorkspaceEntries bounds one listing. A larger folder is refused rather
// than listed in part, so the shell never implies it showed everything.
const MaxWorkspaceEntries = 1024

// sampleInputs are the frozen readmit-synth-v1 reference vector recorded in
// docs/synth-v1-vector.md. The sample workspace is a pure function of them, so
// every installation produces byte-identical sample evidence and `readmit synth`
// with the same declared inputs produces the same bundles.
var sampleInputs = bundle.GeneratorInputs{
	Seed:             0,
	BaseTime:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	GeneratorVersion: "readmit-synth-v1",
	ProfileVersion:   "readmit-siu-v1",
}

// State is the single explicit outcome of one operation. Unknown, unsupported
// and incomplete artifacts are never reported as Completed.
type State string

const (
	Empty            State = "empty"
	Busy             State = "busy"
	Cancelled        State = "cancelled"
	Failed           State = "failed"
	PermissionDenied State = "permission_denied"
	Completed        State = "completed"
)

// Kind classifies a workspace entry by the contract it declares, never by its
// name or extension.
type Kind string

const (
	CaseArtifact        Kind = "case"
	ProjectArtifact     Kind = "project"
	UnsupportedArtifact Kind = "unsupported"
)

// Artifact is one entry of a workspace folder. Schema and Provenance are what
// the entry declares; listing a folder verifies nothing.
type Artifact struct {
	Name       string `json:"name"`
	Kind       Kind   `json:"kind"`
	Schema     string `json:"schema,omitzero"`
	Provenance string `json:"provenance,omitzero"`
	Reason     string `json:"reason,omitzero"`
}

// Workspace is an opened folder and what it declares it holds.
type Workspace struct {
	Root      string     `json:"root"`
	Artifacts []Artifact `json:"artifacts"`
}

// WorkspaceResult carries one state. Workspace is present only when the folder
// was opened; Reason is a fixed sentence that never repeats a path or a value.
type WorkspaceResult struct {
	State     State      `json:"state"`
	Reason    string     `json:"reason,omitzero"`
	Workspace *Workspace `json:"workspace,omitzero"`
}

// Case is verified evidence. Every count below was derived after the shared
// reader checked completion, identity, payload hashes and every record. It
// carries no message bytes, field values, or original source paths.
type Case struct {
	Name             string `json:"name"`
	Identity         string `json:"identity"`
	Schema           string `json:"schema"`
	Provenance       string `json:"provenance"`
	Sources          int    `json:"sources"`
	Occurrences      int    `json:"occurrences"`
	Messages         int    `json:"messages"`
	Acknowledgements int    `json:"acknowledgements"`
	Unparsed         int    `json:"unparsed"`
}

// CaseResult carries one state. Case is present only when the shared reader
// accepted the evidence.
type CaseResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Case   *Case  `json:"case,omitzero"`
}

// ProjectResult carries one state. Project is the document exactly as it was
// written: the facade never rewrites a recorded case identity, and it verifies
// no evidence here. Whether a registered case is still the evidence the project
// recorded is what `readmit project show` reports.
type ProjectResult struct {
	State   State             `json:"state"`
	Reason  string            `json:"reason,omitzero"`
	Root    string            `json:"root,omitzero"`
	Project *project.Document `json:"project,omitzero"`
}

// RecentResult lists workspace folders in most-recently-opened order.
type RecentResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Roots  []string `json:"roots"`
}

// refusal is the state and fixed reason of an operation that did not run. It is
// shared plumbing: each public result keeps its own flat shape for the shell.
type refusal struct {
	state  State
	reason string
}

var (
	busyRefusal      = refusal{Busy, "another operation is already running"}
	cancelledRefusal = refusal{Cancelled, "the operation was cancelled"}
)

func (r refusal) workspace() WorkspaceResult {
	return WorkspaceResult{State: r.state, Reason: r.reason}
}

func (r refusal) evidence() CaseResult { return CaseResult{State: r.state, Reason: r.reason} }

func (r refusal) project() ProjectResult { return ProjectResult{State: r.state, Reason: r.reason} }

// FolderChooser presents the host's native folder dialog. An empty path with a
// nil error means the person dismissed it without choosing.
type FolderChooser interface {
	ChooseFolder(title string) (string, error)
}

// App runs exactly one operation at a time: a second request reports Busy
// rather than racing the first, and a finished operation always releases the
// slot, including after a failure or a cancellation.
type App struct {
	chooser    FolderChooser
	recentPath string

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// New binds the facade to a host folder dialog and to the file that holds
// recently opened workspaces.
func New(chooser FolderChooser, recentPath string) *App {
	return &App{chooser: chooser, recentPath: recentPath}
}

// Cancel stops the operation that is running now, when that operation can be
// interrupted. Verifying a case runs to completion once the shared reader
// starts. Cancel does nothing when nothing is running, and it never retracts
// bytes an operation already wrote.
func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// claim reserves the single operation slot for work that runs to completion
// once it starts. The returned release always frees the slot.
func (a *App) claim() (func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil, false
	}
	a.running = true
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.cancel != nil {
			a.cancel()
			a.cancel = nil
		}
		a.running = false
	}, true
}

// begin claims the slot for work Cancel can interrupt.
func (a *App) begin() (context.Context, func(), bool) {
	release, claimed := a.claim()
	if !claimed {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	return ctx, release, true
}

// SelectWorkspace asks the host for a folder and opens it as a workspace.
func (a *App) SelectWorkspace() WorkspaceResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return busyRefusal.workspace()
	}
	defer release()
	folder, declined := a.chooseFolder(ctx, "Open a readmit workspace folder")
	if folder == "" {
		return declined.workspace()
	}
	return a.openWorkspace(ctx, folder)
}

// OpenWorkspace opens a folder the person already knows, such as one returned
// by RecentWorkspaces. A folder that opens is recorded so it can be reopened.
func (a *App) OpenWorkspace(path string) WorkspaceResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return busyRefusal.workspace()
	}
	defer release()
	return a.openWorkspace(ctx, path)
}

// CreateSampleWorkspace writes the frozen synthetic SIU family into a new
// folder inside the chosen one and opens it. The evidence is generated, not
// imported: it is a fixture family, never customer data.
func (a *App) CreateSampleWorkspace() WorkspaceResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return busyRefusal.workspace()
	}
	defer release()
	parent, declined := a.chooseFolder(ctx, "Choose a folder for the readmit sample workspace")
	if parent == "" {
		return declined.workspace()
	}
	if ctx.Err() != nil {
		return cancelledRefusal.workspace()
	}
	// synth.Write resolves and protects the destination itself. This only
	// separates the one failure worth an actionable sentence: the chosen folder
	// already holds a readmit-sample folder, which may be complete evidence or
	// output an interrupted attempt retained. Neither is reused or overwritten.
	destination := filepath.Join(parent, SampleName)
	if _, err := os.Lstat(destination); err == nil {
		return WorkspaceResult{State: Failed, Reason: "the chosen folder already holds a readmit-sample folder; choose a different folder"}
	}
	if _, err := synth.Write(destination, sampleInputs); err != nil {
		return probeWriteFailure(parent).workspace()
	}
	return a.openWorkspace(ctx, destination)
}

// OpenCase verifies one listed entry through the same reader the command line
// uses. Completion, identity, payload hashes and every record are checked
// before any count below is reported. Verification is bounded by the case
// reader's own limits and runs to completion once it starts, so it holds the
// operation slot but is not interruptible.
func (a *App) OpenCase(workspace, name string) CaseResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.evidence()
	}
	defer release()
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.evidence()
	}
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return CaseResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return CaseResult{State: Failed, Reason: "the case could not be verified as complete, unmodified evidence"}
	}
	counts := opened.Counts()
	return CaseResult{State: Completed, Case: &Case{
		Name:             name,
		Identity:         opened.Identity,
		Schema:           opened.Manifest.Schema,
		Provenance:       string(opened.Manifest.Provenance.Mode),
		Sources:          len(opened.Manifest.Sources),
		Occurrences:      len(opened.Events),
		Messages:         counts[bundle.Message],
		Acknowledgements: counts[bundle.Acknowledgement],
		Unparsed:         counts[bundle.Unparsed],
	}}
}

// OpenProject reads the project document of a folder. The document is the
// canonical record the command line writes, so the shell shows the same case
// identities the command line and exported artifacts name. Reading it verifies
// no evidence and rewrites nothing: a project this release cannot read is
// reported and left exactly as written. It runs to completion once it starts,
// so it holds the operation slot but is not interruptible.
func (a *App) OpenProject(path string) ProjectResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.project()
	}
	defer release()
	root, declined := resolveFolder(path)
	if root == "" {
		return declined.project()
	}
	opened, err := project.Open(root)
	if err != nil {
		return probeReadFailure(root).project()
	}
	if len(opened.Document.Cases) == 0 {
		return ProjectResult{State: Empty, Root: opened.Root, Project: &opened.Document}
	}
	return ProjectResult{State: Completed, Root: opened.Root, Project: &opened.Document}
}

// RecentWorkspaces lists previously opened workspace folders, most recent
// first. It reads one small local file and deliberately does not claim the
// operation slot, so the shell can still offer the list while an operation
// runs. A list this release cannot read is reported, never replaced.
func (a *App) RecentWorkspaces() RecentResult {
	roots, err := readRecent(a.recentPath)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return RecentResult{State: PermissionDenied, Reason: "this account cannot read the recent workspace list", Roots: []string{}}
	case err != nil:
		return RecentResult{State: Failed, Reason: "the recent workspace list was written by a version this release cannot read", Roots: []string{}}
	case len(roots) == 0:
		return RecentResult{State: Empty, Roots: []string{}}
	}
	return RecentResult{State: Completed, Roots: roots}
}

func (a *App) chooseFolder(ctx context.Context, title string) (string, refusal) {
	if ctx.Err() != nil {
		return "", cancelledRefusal
	}
	folder, err := a.chooser.ChooseFolder(title)
	switch {
	case err != nil:
		return "", refusal{Failed, "the folder dialog is unavailable"}
	case folder == "":
		return "", refusal{Cancelled, "no folder was chosen"}
	}
	return folder, refusal{}
}

func (a *App) openWorkspace(ctx context.Context, path string) WorkspaceResult {
	root, declined := resolveFolder(path)
	if root == "" {
		return declined.workspace()
	}
	entries, err := os.ReadDir(root)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return WorkspaceResult{State: PermissionDenied, Reason: "this account cannot read the chosen folder"}
	case err != nil:
		return WorkspaceResult{State: Failed, Reason: "the workspace folder cannot be read"}
	case len(entries) > MaxWorkspaceEntries:
		return WorkspaceResult{State: Failed, Reason: "the folder holds more entries than this release lists"}
	}
	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return cancelledRefusal.workspace()
		}
		artifacts = append(artifacts, describe(root, entry))
	}
	a.recordRecent(root)
	workspace := &Workspace{Root: root, Artifacts: artifacts}
	if len(artifacts) == 0 {
		return WorkspaceResult{State: Empty, Workspace: workspace}
	}
	return WorkspaceResult{State: Completed, Workspace: workspace}
}

// resolveFolder leaves path policy to artifactpath and only separates a folder
// this account cannot reach from one that is not an openable workspace.
func resolveFolder(path string) (string, refusal) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrPermission) {
		return "", refusal{PermissionDenied, "this account cannot open the chosen folder"}
	}
	root, err := artifactpath.Directory(path)
	if err != nil {
		return "", refusal{Failed, "a workspace must be an existing folder that is not a symbolic link"}
	}
	return root, refusal{}
}

// describe reports what one entry declares. It never verifies evidence, so an
// entry listed as a case is a claim until OpenCase accepts it.
func describe(root string, entry fs.DirEntry) Artifact {
	name := entry.Name()
	// The canonical document is found by its fixed name, exactly as a case
	// bundle's manifest is. Nothing is concluded from that name: the entry is
	// decoded, and what it reports is the contract the document itself
	// declares. An entry this release cannot read is unsupported, never a
	// project inferred from a file name.
	if name == project.DocumentName && entry.Type().IsRegular() {
		opened, err := project.Open(root)
		if err != nil {
			return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not a project document this release supports"}
		}
		return Artifact{Name: name, Kind: ProjectArtifact, Schema: opened.Document.Schema}
	}
	path, err := artifactpath.Child(root, name)
	if err != nil {
		reason := "not a case bundle directory"
		if entry.Type()&fs.ModeSymlink != 0 {
			reason = "symbolic links are not opened as evidence"
		}
		return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: reason}
	}
	manifest, err := bundle.Describe(path)
	if err != nil {
		return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not a case bundle this release supports"}
	}
	return Artifact{Name: name, Kind: CaseArtifact, Schema: manifest.Schema, Provenance: string(manifest.Provenance.Mode)}
}

// probeReadFailure separates a folder or document this account cannot read from
// one that holds no project document this release reads. The project reader
// returns fixed sentences that disclose no path and no host diagnostic, so the
// distinction is made here, on the already-resolved root, and only after a read
// has already failed. It inspects the error class and reads no content.
func probeReadFailure(root string) refusal {
	for _, path := range []string{root, filepath.Join(root, project.DocumentName)} {
		opened, err := os.Open(path)
		if err == nil {
			opened.Close()
		} else if errors.Is(err, fs.ErrPermission) {
			return refusal{PermissionDenied, "this account cannot open the chosen folder"}
		}
	}
	return refusal{Failed, "the folder holds no project document this release reads"}
}

// probeWriteFailure separates a folder this account cannot write from other
// write failures, so the shell can say to choose a different folder. A folder's
// mode bits do not answer that portably, so it creates and immediately removes
// one temporary directory inside the chosen folder. It runs only after a write
// has already failed, and it never touches the destination.
func probeWriteFailure(parent string) refusal {
	probe, err := os.MkdirTemp(parent, ".readmit-access-")
	if err == nil {
		os.Remove(probe)
	} else if errors.Is(err, fs.ErrPermission) {
		return refusal{PermissionDenied, "this account cannot create a folder in the chosen folder"}
	}
	return refusal{Failed, "the sample workspace could not be created in the chosen folder"}
}
