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
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/operationguard"
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

// Kind classifies a workspace entry by the contract the entry itself declares.
// A canonical document is located by its fixed name, but nothing is concluded
// from that name: the contract always comes from inside the entry, and an entry
// whose declared contract this release does not read is unsupported.
type Kind string

const (
	CaseArtifact      Kind = "case"
	ProjectArtifact   Kind = "project"
	RevisionsArtifact Kind = "revisions"
	ResultArtifact    Kind = "result"
	JobArtifact       Kind = "job"
	ReviewArtifact    Kind = "review"
	IndexArtifact     Kind = "index"
	TargetArtifact    Kind = "target"
	RulesArtifact     Kind = "rules"
	PlanArtifact      Kind = "plan"
	SpecArtifact      Kind = "spec"
	PackArtifact      Kind = "pack"
	ProfileArtifact   Kind = "profile"
	PackageArtifact   Kind = "package"
	AnalysisArtifact  Kind = "analysis"
	SecretArtifact    Kind = "secret"
	PolicyArtifact    Kind = "policy"
	ResetArtifact     Kind = "reset"
	// A diagnosis report directory holds report.json beside report.md; a
	// finding review directory holds review.json declaring the finding-review
	// contract; a correlation review directory holds the machine mapping
	// beside the decisions that were made over it. The three flat documents
	// are the authored configurations the diagnosis and comparison panels
	// offer pickers for.
	DiagnosisArtifact         Kind = "diagnosis"
	FindingReviewArtifact     Kind = "finding-review"
	CorrelationReviewArtifact Kind = "correlation-review"
	NormalizationArtifact     Kind = "normalization-policy"
	DiagnoseConfigArtifact    Kind = "diagnose-config"
	DecisionsArtifact         Kind = "finding-decisions"
	// A suite document — or the directory a preparation or an execution
	// retained — declares the environments, data rows and templates the
	// durable-run panels execute a whole environment of; the releases document
	// is the separate pin set that makes one an approved suite. A released test
	// version, a coverage document and a promotion approval are the suite
	// workflow's own artifacts beside them; the suite panel offers them all.
	SuiteArtifact         Kind = "suite"
	SuiteReleasesArtifact Kind = "suite-releases"
	// A sealed investigation packet is a directory whose manifest declares the
	// retained-packet contract the report panels verify and export; a portable
	// review is the sealed directory of offline renderings exported from one.
	// Both are claims the listing makes, and opening either one verifies it.
	PacketArtifact         Kind = "packet"
	PortableReviewArtifact Kind = "portable-review"
	// A synthetic demonstration packet is a directory whose manifest declares
	// the readmit-report contract `readmit report` seals: generated from the
	// committed scenario on built-in fixtures, never a person's own evidence,
	// and verified only by the synthetic packet section's own verifier.
	SyntheticPacketArtifact Kind = "synthetic-packet"
	// The privacy and protection screens' artifacts: a completed derived export
	// packet, a verified local support bundle, an encrypted transfer package,
	// a protection document registering key references readmit never holds, and
	// the sharing policy a support summary is prepared under. All five are
	// claims the listing makes, and opening the entry is still the verification
	// step.
	DerivedExportArtifact   Kind = "derived-export"
	SupportArtifact         Kind = "support"
	TransferPackageArtifact Kind = "transfer-package"
	ProtectionArtifact      Kind = "protection"
	SharingPolicyArtifact   Kind = "sharing-policy"
	UnsupportedArtifact     Kind = "unsupported"
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

func (r *WorkspaceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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

func (r *CaseResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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

func (r *ProjectResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RevisionsResult carries one state. Revisions is the editable document of a
// project exactly as it was written: the notes and drafts a person maintains,
// and the lineage of every revision derived from registered evidence. It holds
// no evidence, and nothing the shell writes through this result can reach any.
type RevisionsResult struct {
	State     State              `json:"state"`
	Reason    string             `json:"reason,omitzero"`
	Root      string             `json:"root,omitzero"`
	Revisions *project.Revisions `json:"revisions,omitzero"`
}

func (r *RevisionsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

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

func (r refusal) revisions() RevisionsResult {
	return RevisionsResult{State: r.state, Reason: r.reason}
}

// FolderChooser presents the host's native folder dialog. An empty path with a
// nil error means the person dismissed it without choosing.
type FolderChooser interface {
	ChooseFolder(title string) (string, error)
}

// FileChooser presents the host's native file dialog for one or more files.
// An empty slice with a nil error means the person dismissed it without choosing.
type FileChooser interface {
	ChooseFiles(title, filterName, filterPattern string) ([]string, error)
}

// DestinationChooser presents the host's native save dialog, in which a person
// names a new folder: a name that need not exist yet, in a location they
// choose. A folder dialog returns only a folder that already exists, and every
// writer a new destination is handed to creates it itself and refuses one that
// exists, so a new destination is named here and never picked. The dialog
// creates nothing. An empty path with a nil error means the person dismissed
// it without naming one.
type DestinationChooser interface {
	ChooseDestination(title string) (string, error)
}

// App runs exactly one operation at a time: a second request reports Busy
// rather than racing the first, and a finished operation always releases the
// slot, including after a failure or a cancellation.
type App struct {
	operationMu            sync.Mutex
	operationGuard         *operationguard.Guard
	operationPolicy        string
	operationSelectionPath string
	chooser                FolderChooser
	recentPath             string
	filtersPath            string
	sessionPath            string
	draftsPath             string

	// licenseRoot is where this computer's license lives, the folder the
	// command line reads it from too; empty when the shell was given none.
	licenseRoot string

	hubMu            sync.Mutex
	hubSelectionPath string
	hubConfigPath    string
	hubConfig        *hubclient.Config
	hubClient        *hubclient.Client
	hubSession       *hubclient.Session
	hubAuthFlow      *hubclient.AuthFlow
	// hubRestoreRefusal says why the selection an earlier session remembered
	// could not be restored, until a configuration is selected again.
	hubRestoreRefusal string

	// commercialMu guards the retained commercial destinations selection: a
	// local path the operator chose, read again for each status. It is never
	// a connection and never restored into an active request.
	commercialMu            sync.Mutex
	commercialSelectionPath string
	commercialConfigPath    string

	mu sync.Mutex
	// running, operation and runOutput are the identity of the one operation
	// that holds the slot. operation names the operation the way the panel
	// that started it does, so a cancel action can say which operation it is
	// cancelling and cannot reach a different one, and the privacy status can
	// say which disclosed activity is happening; runOutput is the folder a
	// durable run is being written into while it executes.
	running   bool
	operation string
	runOutput string
	cancel    context.CancelFunc
	// programs counts the operator-declared programs the operation holding
	// the slot is running now, as the programs report themselves; see
	// declaredProgramStarted.
	programs int

	// sessionMu serializes the working session alone. Retaining an unstored
	// note edit and the place it was typed in must not wait for the operation
	// slot, because a crash during a long operation is exactly when that work
	// has to survive; it still must not race another write of the same file.
	sessionMu sync.Mutex

	// draftsMu serializes the editor draft store alone, for the same reason:
	// an editor's unstored work is retained whether or not an operation runs,
	// and the writes of one small document must never interleave.
	draftsMu sync.Mutex

	// corpusMu guards the counts a running corpus generation or scan has
	// reached, which CorpusProgress reads without waiting for the slot the
	// operation holds. corpusProgress is nil while neither runs.
	corpusMu       sync.Mutex
	corpusProgress *CorpusProgress

	// captureMu guards the address a running collector or fixture bound,
	// which CaptureProgress reads without waiting for the slot StartCapture
	// holds. captureProgress is nil while nothing listens.
	captureMu       sync.Mutex
	captureProgress *CaptureProgress
}

// New binds the facade to a host folder dialog and to the four files that hold
// this viewer's local shell state: the workspaces they opened recently, the
// filters they saved, the working session they have not stored, and the editor
// drafts they have not stored. Each is named explicitly rather than derived
// from another, and none holds evidence.
func New(chooser FolderChooser, recentPath, filtersPath, sessionPath, draftsPath string) *App {
	return &App{operationGuard: operationguard.New(""), chooser: chooser, recentPath: recentPath, filtersPath: filtersPath, sessionPath: sessionPath, draftsPath: draftsPath}
}

// Cancel stops the operation that is running now, when that operation can be
// interrupted. Verifying a case runs to completion once the shared reader
// starts. Cancel does nothing when nothing is running, and it never retracts
// bytes an operation already wrote.
//
// operation names the operation the caller means to cancel, the same name the
// panel that started it gave it. A cancel naming a different operation does
// nothing, so one panel's cancel control can never stop another panel's work;
// an empty name cancels whatever is running and is what the window's own
// cancel command uses.
func (a *App) Cancel(operation string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel == nil {
		return
	}
	if operation != "" && operation != a.operation {
		return
	}
	a.cancel()
}

// claim reserves the single operation slot for work that runs to completion
// once it starts. operation names the work, as it does for begin; an empty
// name leaves it unnamed. The slot and the name are taken together, so the
// operation is never observable as running under no name while it has one.
// The returned release always frees the slot.
func (a *App) claim(operation string) (func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil, false
	}
	a.running, a.operation = true, operation
	return a.release, true
}

// release frees the slot and cancels whatever the operation that held it
// started, so nothing it began outlives it.
func (a *App) release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.running = false
	a.operation = ""
	a.runOutput = ""
}

// declaredProgramStarted counts one operator-declared program as running,
// until the function it returns is called. runNamed hands it to the work of
// every operation as the observer of the programs that work runs, so a program
// is counted exactly while it runs, whichever engine package starts it, and
// the privacy status reads the count under the slot's lock. The returned
// function counts the program's end once, however often it is called.
func (a *App) declaredProgramStarted() func() {
	a.mu.Lock()
	a.programs++
	a.mu.Unlock()
	var ended sync.Once
	return func() {
		ended.Do(func() {
			a.mu.Lock()
			a.programs--
			a.mu.Unlock()
		})
	}
}

// begin claims the slot for work Cancel can interrupt. operation names the
// work the way its own panel does, so a cancel action can name what it is
// cancelling; an empty name leaves the operation unnamed, cancellable only by
// the window's own cancel command. The slot, the name and the cancellation are
// taken together: an operation is never observable as running — busy to
// another caller — while a cancellation of it would still do nothing.
func (a *App) begin(operation string) (context.Context, func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.running, a.cancel, a.operation = true, cancel, operation
	return ctx, a.release, true
}

// SelectWorkspace asks the host for a folder and opens it as a workspace.
func (a *App) SelectWorkspace() WorkspaceResult {
	return run(a, true, false, func(ctx context.Context) WorkspaceResult {
		folder, declined := a.chooseFolder(ctx, "Open a readmit workspace folder")
		if folder == "" {
			return declined.workspace()
		}
		return a.openWorkspace(ctx, folder)
	})
}

// OpenWorkspace opens a folder the person already knows, such as one returned
// by RecentWorkspaces. A folder that opens is recorded so it can be reopened.
func (a *App) OpenWorkspace(path string) WorkspaceResult {
	return run(a, true, false, func(ctx context.Context) WorkspaceResult {
		return a.openWorkspace(ctx, path)
	})
}

// CreateSampleWorkspace writes the frozen synthetic SIU family into a new
// folder inside the chosen one and opens it. The evidence is generated, not
// imported: it is a fixture family, never customer data.
func (a *App) CreateSampleWorkspace() WorkspaceResult {
	return run(a, true, false, func(ctx context.Context) WorkspaceResult {
		return a.createSampleWorkspace(ctx)
	})
}

func (a *App) createSampleWorkspace(ctx context.Context) WorkspaceResult {
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
		return probeWriteFailure(parent,
			"this account cannot create a folder in the chosen folder",
			"the sample workspace could not be created in the chosen folder").workspace()
	}
	// What synth writes is a family, and a family is retained evidence nothing
	// may be written inside — including the test spec this window's authoring
	// flow saves and the evidence a practice run retains. A sample nobody can
	// work in is not a sample, so the generated cases are left exactly as they
	// were and the directory is prepared as a workspace: the family completion
	// record is dropped and the practice endpoint a test names is written.
	if err := guide.PrepareWorkspace(ctx, destination); errors.Is(err, guide.ErrCannotWrite) {
		// A folder this account cannot write is separated from the rest only
		// after the write has already failed, and the probe never touches the
		// generated evidence.
		return probeWriteFailure(parent,
			"this account cannot write into the chosen folder",
			"the sample workspace could not be completed in the chosen folder; incomplete output is retained").workspace()
	} else if err != nil {
		return WorkspaceResult{State: Failed, Reason: err.Error()}
	}
	return a.openWorkspace(ctx, destination)
}

// OpenCase verifies one listed entry through the same reader the command line
// uses. Completion, identity, payload hashes and every record are checked
// before any count below is reported. Verification is bounded by the case
// reader's own limits and runs to completion once it starts, so it holds the
// operation slot but is not interruptible.
func (a *App) OpenCase(workspace, name string) CaseResult {
	return run(a, false, false, func(context.Context) CaseResult {
		return a.openCase(workspace, name)
	})
}

func (a *App) openCase(workspace, name string) CaseResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.evidence()
	}
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return CaseResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	opened, err := operation.OpenCase(path)
	if err != nil {
		return CaseResult{State: Failed, Reason: err.Error()}
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
	return run(a, false, false, func(context.Context) ProjectResult {
		return a.openProject(path)
	})
}

func (a *App) openProject(path string) ProjectResult {
	opened, declined := openProjectFolder(path)
	if opened == nil {
		return declined.project()
	}
	if len(opened.Document.Cases) == 0 {
		return ProjectResult{State: Empty, Root: opened.Root, Project: &opened.Document}
	}
	return ProjectResult{State: Completed, Root: opened.Root, Project: &opened.Document}
}

// OpenRevisions reads the editable document of a project folder: the notes and
// drafts beside the evidence, and the recorded lineage of every revision. It
// verifies no evidence and rewrites nothing. A project that has recorded
// nothing yet is Empty rather than missing. It runs to completion once it
// starts, so it holds the operation slot but is not interruptible.
func (a *App) OpenRevisions(path string) RevisionsResult {
	return run(a, false, false, func(context.Context) RevisionsResult {
		return a.openRevisions(path)
	})
}

func (a *App) openRevisions(path string) RevisionsResult {
	opened, declined := openProjectFolder(path)
	if opened == nil {
		return declined.revisions()
	}
	revisions, declined := readRevisions(opened.Root)
	if revisions == nil {
		return declined.revisions()
	}
	if len(revisions.Notes) == 0 && len(revisions.Revisions) == 0 {
		return RevisionsResult{State: Empty, Root: opened.Root, Revisions: revisions}
	}
	return RevisionsResult{State: Completed, Root: opened.Root, Revisions: revisions}
}

// SaveNote creates or replaces one editable note of a project and returns the
// document it stored. This is the only thing the shell writes into a project,
// and a note is working text: it is held in the project's own editable
// document, beside evidence, so no edit made here reaches an import, a
// finalized run, or any other retained artifact. It takes the same typed note
// the command line stores, so the shell states what it is writing rather than
// passing interchangeable strings. It runs to completion once it starts, so it
// holds the operation slot but is not interruptible.
func (a *App) SaveNote(path string, note project.Note) RevisionsResult {
	return run(a, false, true, func(context.Context) RevisionsResult {
		return a.saveNote(path, note)
	})
}

func (a *App) saveNote(path string, note project.Note) RevisionsResult {
	root, declined := resolveFolder(path)
	if root == "" {
		return declined.revisions()
	}
	result, err := operation.SetProjectNote(root, note)
	if err != nil {
		if errors.Is(err, operation.ErrProjectOpen) {
			if errors.Is(err, project.ErrUnsupportedVersion) {
				return RevisionsResult{State: Failed, Root: result.Root, Reason: "the project document was written by a version this release cannot read"}
			}
			return probeReadFailure(result.Root).revisions()
		}
		if errors.Is(err, operation.ErrProjectRevisions) {
			if errors.Is(err, project.ErrUnsupportedVersion) {
				return RevisionsResult{State: Failed, Root: result.Root, Reason: "the editable project document was written by a version this release cannot read"}
			}
			return RevisionsResult{State: Failed, Root: result.Root, Reason: "the editable project document cannot be read"}
		}
		if errors.Is(err, operation.ErrProjectNoteInvalid) {
			return RevisionsResult{State: Failed, Root: result.Root, Reason: "the note was not stored: it needs a name and a title, its text must be bounded and printable, any subject it names must be a case or revision this project registers, and a project holds a bounded number of notes"}
		}
		if errors.Is(err, operation.ErrProjectNoteWrite) {
			return probeWriteFailure(result.Root,
				"this account cannot write to the project folder",
				"the note could not be stored; an interrupted write may be retained beside the project document").revisions()
		}
		return RevisionsResult{State: Failed, Root: result.Root, Reason: err.Error()}
	}
	return RevisionsResult{State: Completed, Root: result.Root, Revisions: &result.Revisions}
}

// openProjectFolder resolves a folder and reads the project document every
// editable document belongs to. A nil project carries the refusal to report.
func openProjectFolder(path string) (*project.Project, refusal) {
	root, declined := resolveFolder(path)
	if root == "" {
		return nil, declined
	}
	opened, err := project.Open(root)
	if errors.Is(err, project.ErrUnsupportedVersion) {
		return nil, refusal{Failed, "the project document was written by a version this release cannot read"}
	}
	if err != nil {
		return nil, probeReadFailure(root)
	}
	return opened, refusal{}
}

// readRevisions reads the editable document of an already opened project. A
// nil document carries the refusal to report; a document this release cannot
// read is reported and left exactly as written, never migrated or replaced.
func readRevisions(root string) (*project.Revisions, refusal) {
	revisions, err := project.ReadRevisions(root)
	if errors.Is(err, project.ErrUnsupportedVersion) {
		return nil, refusal{Failed, "the editable project document was written by a version this release cannot read"}
	}
	if err != nil {
		return nil, refusal{Failed, "the editable project document cannot be read"}
	}
	return &revisions, refusal{}
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

func (a *App) chooseFiles(ctx context.Context, title, filterName, filterPattern string) ([]string, refusal) {
	if ctx.Err() != nil {
		return nil, cancelledRefusal
	}
	fc, ok := a.chooser.(FileChooser)
	if !ok {
		return nil, refusal{Failed, "the file dialog is unavailable"}
	}
	files, err := fc.ChooseFiles(title, filterName, filterPattern)
	switch {
	case err != nil:
		return nil, refusal{Failed, "the file dialog is unavailable"}
	case len(files) == 0:
		return nil, refusal{Cancelled, "no file was chosen"}
	}
	return files, refusal{}
}

// chooseDestination names a new destination through the host's save dialog.
// The path it returns is handed to a writer as it is: that writer creates it,
// and refuses a path that already exists with its own reason.
func (a *App) chooseDestination(ctx context.Context, title string) (string, refusal) {
	if ctx.Err() != nil {
		return "", cancelledRefusal
	}
	dc, ok := a.chooser.(DestinationChooser)
	if !ok {
		return "", refusal{Failed, "the save dialog is unavailable"}
	}
	path, err := dc.ChooseDestination(title)
	switch {
	case err != nil:
		return "", refusal{Failed, "the save dialog is unavailable"}
	case path == "":
		return "", refusal{Cancelled, "no new folder was named"}
	}
	return path, refusal{}
}

func (a *App) openWorkspace(ctx context.Context, path string) WorkspaceResult {
	root, declined := resolveFolder(path)
	if root == "" {
		return declined.workspace()
	}
	artifacts, declined := listArtifacts(ctx, root)
	if declined.state != "" {
		return declined.workspace()
	}
	a.recordRecent(root)
	workspace := &Workspace{Root: root, Artifacts: artifacts}
	if len(artifacts) == 0 {
		return WorkspaceResult{State: Empty, Workspace: workspace}
	}
	return WorkspaceResult{State: Completed, Workspace: workspace}
}

// listArtifacts reports what each immediate entry of an already-resolved folder
// declares. It is the one listing in this package: opening a workspace and
// searching one both read exactly this and never a second, wider view of the
// folder. A folder past the entry bound is refused rather than listed in part.
func listArtifacts(ctx context.Context, root string) ([]Artifact, refusal) {
	entries, err := os.ReadDir(root)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, refusal{PermissionDenied, "this account cannot read the chosen folder"}
	case err != nil:
		return nil, refusal{Failed, "the workspace folder cannot be read"}
	case len(entries) > MaxWorkspaceEntries:
		return nil, refusal{Failed, "the folder holds more entries than this release lists"}
	}
	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, cancelledRefusal
		}
		artifacts = append(artifacts, describe(root, entry))
	}
	return artifacts, refusal{}
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

// openedCase is the pipeline every feature composes to read evidence: resolve
// the open workspace, name the case by one of its entries, and verify the
// evidence against the identity the window displays. That invariant — every
// read re-verifies and binds to the identity shown — is written here and
// nowhere else; a feature adds only what its own flow decides about the
// verified bundle it receives.
func openedCase(workspace, caseName, identity string) (string, *bundle.Bundle, refusal) {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return "", nil, declined
	}
	path, err := artifactpath.Child(root, caseName)
	if err != nil {
		return "", nil, refusal{Failed, "a case must be named by one directory entry of the open workspace"}
	}
	source, err := operation.OpenVerifiedCase(path, identity)
	if err != nil {
		return "", nil, refusal{Failed, err.Error()}
	}
	return root, source, refusal{}
}

// describe reports what one entry declares. It never verifies evidence, so an
// entry listed as a case is a claim until OpenCase accepts it.
func describe(root string, entry fs.DirEntry) Artifact {
	name := entry.Name()
	// A project directory holds its document under one canonical name, so that
	// is how it is located. Nothing is concluded from the name: the entry is
	// decoded, and what it reports is the contract the document itself
	// declares. An entry this release cannot read is unsupported, never a
	// project inferred from a file name.
	if name == project.DocumentName && entry.Type().IsRegular() {
		opened, err := project.Open(root)
		if errors.Is(err, project.ErrUnsupportedVersion) {
			return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "a project document written by a version this release cannot read"}
		}
		if err != nil {
			return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not a project document this release supports"}
		}
		return Artifact{Name: name, Kind: ProjectArtifact, Schema: opened.Document.Schema}
	}
	// The editable document beside it is located the same way and reports the
	// contract it declares, so a folder never lists a canonical readmit
	// document as something this release does not recognize.
	if name == project.RevisionsDocumentName && entry.Type().IsRegular() {
		revisions, err := project.ReadRevisions(root)
		if errors.Is(err, project.ErrUnsupportedVersion) {
			return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "an editable project document written by a version this release cannot read"}
		}
		if err != nil {
			return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: "not an editable project document this release supports"}
		}
		return Artifact{Name: name, Kind: RevisionsArtifact, Schema: revisions.Schema}
	}
	path, err := artifactpath.Child(root, name)
	if err == nil {
		manifest, err := bundle.Describe(path)
		if err == nil {
			return Artifact{Name: name, Kind: CaseArtifact, Schema: manifest.Schema, Provenance: string(manifest.Provenance.Mode)}
		}
	}
	// Beyond the cases and the two project documents, the listing names what a
	// retained artifact declares: a directory holding a fixed-name record, or
	// a regular file declaring a contract this window offers a picker for.
	// Classifying verifies nothing — opening the entry is still the
	// verification step — so an entry named here is a claim the listing makes,
	// never an admission.
	if entry.Type().IsRegular() || entry.IsDir() {
		if kind, known := classify(root, name, entry.IsDir()); known {
			return Artifact{Name: name, Kind: kind}
		}
	}
	reason := "not a case bundle this release supports"
	if entry.Type()&fs.ModeSymlink != 0 {
		reason = "symbolic links are not opened as evidence"
	} else if err != nil {
		reason = "not a case bundle directory"
	}
	return Artifact{Name: name, Kind: UnsupportedArtifact, Reason: reason}
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
// write failures, so the shell can say what to do about it. A folder's mode
// bits do not answer that portably, so it creates and immediately removes one
// temporary directory inside the folder. It runs only after a write has already
// failed, and it never touches the destination. denied and failed are the fixed
// sentences to report; neither discloses a path or a host diagnostic.
func probeWriteFailure(parent, denied, failed string) refusal {
	probe, err := os.MkdirTemp(parent, ".readmit-access-")
	if err == nil {
		os.Remove(probe)
	} else if errors.Is(err, fs.ErrPermission) {
		return refusal{PermissionDenied, denied}
	}
	return refusal{Failed, failed}
}
