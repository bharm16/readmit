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

// RecentResult lists workspace folders in most-recently-opened order.
type RecentResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Roots  []string `json:"roots"`
}

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

	mu     sync.Mutex
	cancel context.CancelFunc
}

// New binds the facade to a host folder dialog and to the file that holds
// recently opened workspaces.
func New(chooser FolderChooser, recentPath string) *App {
	return &App{chooser: chooser, recentPath: recentPath}
}

// Cancel stops the operation that is running now. It does nothing when none is
// running, and it cannot retract bytes an operation already wrote.
func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// begin claims the single operation slot. The returned release always frees it.
func (a *App) begin() (context.Context, func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	return ctx, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		cancel()
		a.cancel = nil
	}, true
}

// SelectWorkspace asks the host for a folder and opens it as a workspace.
func (a *App) SelectWorkspace() WorkspaceResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return WorkspaceResult{State: Busy, Reason: busyReason}
	}
	defer release()
	folder, refusal := a.chooseFolder(ctx, "Open a readmit workspace folder")
	if folder == "" {
		return refusal
	}
	return a.openWorkspace(ctx, folder)
}

// OpenWorkspace opens a folder the person already knows, such as one returned
// by RecentWorkspaces. A folder that opens is recorded so it can be reopened.
func (a *App) OpenWorkspace(path string) WorkspaceResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return WorkspaceResult{State: Busy, Reason: busyReason}
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
		return WorkspaceResult{State: Busy, Reason: busyReason}
	}
	defer release()
	parent, refusal := a.chooseFolder(ctx, "Choose a folder for the readmit sample workspace")
	if parent == "" {
		return refusal
	}
	if ctx.Err() != nil {
		return WorkspaceResult{State: Cancelled, Reason: cancelledReason}
	}
	destination := filepath.Join(parent, SampleName)
	if _, err := os.Lstat(destination); err == nil {
		return WorkspaceResult{State: Failed, Reason: "a sample workspace already exists in the chosen folder"}
	}
	if _, err := synth.Write(destination, sampleInputs); err != nil {
		return sampleFailure(parent)
	}
	return a.openWorkspace(ctx, destination)
}

// OpenCase verifies one listed entry through the same reader the command line
// uses. Completion, identity, payload hashes and every record are checked
// before any count below is reported.
func (a *App) OpenCase(workspace, name string) CaseResult {
	_, release, claimed := a.begin()
	if !claimed {
		return CaseResult{State: Busy, Reason: busyReason}
	}
	defer release()
	if name == "." || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return CaseResult{State: Failed, Reason: "a case must be named by one entry of the open workspace"}
	}
	root, refusal := resolveFolder(workspace)
	if root == "" {
		return CaseResult{State: refusal.State, Reason: refusal.Reason}
	}
	path := filepath.Join(root, name)
	if info, err := os.Lstat(path); err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return CaseResult{State: Failed, Reason: "that entry is not a case bundle directory in this workspace"}
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return CaseResult{State: Failed, Reason: "the case could not be verified as complete, unmodified evidence"}
	}
	evidence := &Case{
		Name:        name,
		Identity:    opened.Identity,
		Schema:      opened.Manifest.Schema,
		Provenance:  string(opened.Manifest.Provenance.Mode),
		Sources:     len(opened.Manifest.Sources),
		Occurrences: len(opened.Events),
	}
	for _, event := range opened.Events {
		switch event.Kind {
		case bundle.Message:
			evidence.Messages++
		case bundle.Acknowledgement:
			evidence.Acknowledgements++
		case bundle.Unparsed:
			evidence.Unparsed++
		}
	}
	return CaseResult{State: Completed, Case: evidence}
}

// RecentWorkspaces lists previously opened workspace folders, most recent
// first. A list this release cannot read is reported, never replaced.
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

const (
	busyReason      = "another operation is already running"
	cancelledReason = "the operation was cancelled"
)

func (a *App) chooseFolder(ctx context.Context, title string) (string, WorkspaceResult) {
	if ctx.Err() != nil {
		return "", WorkspaceResult{State: Cancelled, Reason: cancelledReason}
	}
	folder, err := a.chooser.ChooseFolder(title)
	switch {
	case err != nil:
		return "", WorkspaceResult{State: Failed, Reason: "the folder dialog is unavailable"}
	case folder == "":
		return "", WorkspaceResult{State: Cancelled, Reason: "no folder was chosen"}
	}
	return folder, WorkspaceResult{}
}

func (a *App) openWorkspace(ctx context.Context, path string) WorkspaceResult {
	root, refusal := resolveFolder(path)
	if root == "" {
		return refusal
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
			return WorkspaceResult{State: Cancelled, Reason: cancelledReason}
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
func resolveFolder(path string) (string, WorkspaceResult) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrPermission) {
		return "", WorkspaceResult{State: PermissionDenied, Reason: "this account cannot open the chosen folder"}
	}
	root, err := artifactpath.Directory(path)
	if err != nil {
		return "", WorkspaceResult{State: Failed, Reason: "a workspace must be an existing folder that is not a symbolic link"}
	}
	return root, WorkspaceResult{}
}

// describe reports what one entry declares. It never verifies evidence, so an
// entry listed as a case is a claim until OpenCase accepts it.
func describe(root string, entry fs.DirEntry) Artifact {
	name := entry.Name()
	switch {
	case entry.Type()&fs.ModeSymlink != 0:
		return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "symbolic links are not opened as evidence"}
	case !entry.IsDir():
		return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not a case bundle directory"}
	}
	manifest, err := bundle.Describe(filepath.Join(root, name))
	if err != nil {
		return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not a case bundle this release supports"}
	}
	return Artifact{Name: name, Kind: CaseArtifact, Schema: manifest.Schema, Provenance: string(manifest.Provenance.Mode)}
}

// sampleFailure separates a folder this account cannot write from other write
// failures, so the shell can say to choose a different folder.
func sampleFailure(parent string) WorkspaceResult {
	probe, err := os.MkdirTemp(parent, ".readmit-access-")
	if err == nil {
		os.Remove(probe)
	} else if errors.Is(err, fs.ErrPermission) {
		return WorkspaceResult{State: PermissionDenied, Reason: "this account cannot create a folder in the chosen folder"}
	}
	return WorkspaceResult{State: Failed, Reason: "the sample workspace could not be created in the chosen folder"}
}
