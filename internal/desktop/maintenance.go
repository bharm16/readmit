package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/upgrade"
)

// Path class labels for one backup inventory row. The evidence label is this
// view's name for what a backup manifest records; the classes of a project
// directory's own files are the project package's.
const BackupClassEvidence = "canonical-evidence"

// MaintenancePathResult is one native folder choice for backup, restore or upgrade.
type MaintenancePathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Kind   string `json:"kind,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *MaintenancePathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// BackupInventoryEntry classifies one file or exclusion the maintenance screen shows.
type BackupInventoryEntry struct {
	Path        string `json:"path"`
	Class       string `json:"class"`
	Size        int64  `json:"size,omitzero"`
	SHA256      string `json:"sha256,omitzero"`
	Kind        string `json:"kind,omitzero"`
	Identity    string `json:"identity,omitzero"`
	State       string `json:"state,omitzero"`
	Recorded    string `json:"recorded,omitzero"`
	Case        string `json:"case,omitzero"`
	Retention   string `json:"retention,omitzero"`
	Explanation string `json:"explanation,omitzero"`
}

// BackupReportView is the typed account of create, verify or restore for the window.
type BackupReportView struct {
	Root        string                 `json:"root"`
	Complete    bool                   `json:"complete"`
	Files       int                    `json:"files"`
	Bytes       int64                  `json:"bytes"`
	Evidence    []BackupInventoryEntry `json:"evidence"`
	Mutable     []BackupInventoryEntry `json:"mutable"`
	Exclusions  []BackupInventoryEntry `json:"exclusions"`
	Credentials []BackupInventoryEntry `json:"credentials"`
	Protection  []BackupInventoryEntry `json:"protection"`
	Other       []BackupInventoryEntry `json:"other"`
}

// BackupResult carries one backup create, verify or restore outcome.
type BackupResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	Report *BackupReportView `json:"report,omitzero"`
}

func (r *BackupResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// BackupCreateRequest names the project and the new destination directory.
type BackupCreateRequest struct {
	Project     string `json:"project"`
	Destination string `json:"destination"`
}

// BackupRestoreRequest names the backup and the new project destination.
type BackupRestoreRequest struct {
	Backup      string `json:"backup"`
	Destination string `json:"destination"`
}

// ProjectQuotaView is the retained-file quota declaration and measured usage.
type ProjectQuotaView struct {
	Declared  bool   `json:"declared"`
	MaxBytes  int64  `json:"max_bytes,omitzero"`
	MaxFiles  int    `json:"max_files,omitzero"`
	UsedBytes int64  `json:"used_bytes"`
	UsedFiles int    `json:"used_files"`
	Within    bool   `json:"within"`
	Explain   string `json:"explain"`
}

// ProjectQuotaResult carries quota inspection or an updated declaration.
type ProjectQuotaResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	Quota  *ProjectQuotaView `json:"quota,omitzero"`
}

func (r *ProjectQuotaResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProjectQuotaChange sets both positive limits together.
type ProjectQuotaChange struct {
	Project  string `json:"project"`
	MaxBytes int64  `json:"max_bytes"`
	MaxFiles int    `json:"max_files"`
}

// MigrationPreviewResult carries the schema migration preview without writing.
type MigrationPreviewResult struct {
	State    State           `json:"state"`
	Reason   string          `json:"reason,omitzero"`
	Plan     *lifecycle.Plan `json:"plan,omitzero"`
	Guidance string          `json:"guidance,omitzero"`
}

func (r *MigrationPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// RetirementPreview is the concrete affected-artifact preview before archive or delete.
type RetirementPreview struct {
	Selection  string                    `json:"selection"`
	Project    string                    `json:"project"`
	Compatible bool                      `json:"compatible"`
	Files      int                       `json:"files"`
	Bytes      int64                     `json:"bytes"`
	Documents  []lifecycle.Compatibility `json:"documents"`
	Explain    string                    `json:"explain"`
	NotErasure string                    `json:"not_erasure"`
}

// RetirementPreviewResult carries one retirement preview.
type RetirementPreviewResult struct {
	State   State              `json:"state"`
	Reason  string             `json:"reason,omitzero"`
	Preview *RetirementPreview `json:"preview,omitzero"`
}

func (r *RetirementPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ProjectArchiveRequest archives or deletes after an explicit preview selection.
type ProjectArchiveRequest struct {
	Project     string `json:"project"`
	Destination string `json:"destination"`
	Selection   string `json:"selection"`
	Delete      bool   `json:"delete,omitzero"`
	Confirm     bool   `json:"confirm,omitzero"`
}

// ProjectRecoveryCopy is one retained earlier version of a project document:
// the document it was retained for, the SHA-256 its name records, its length,
// what reading it found (readable, damaged or unreadable) and whether it holds
// the document as it stands.
type ProjectRecoveryCopy struct {
	Document string `json:"document"`
	Digest   string `json:"digest"`
	Size     int64  `json:"size"`
	State    string `json:"state"`
	Current  bool   `json:"current,omitzero"`
}

// ProjectRecoveryCopiesResult lists a project's recovery copies.
type ProjectRecoveryCopiesResult struct {
	State  State                 `json:"state"`
	Reason string                `json:"reason,omitzero"`
	Copies []ProjectRecoveryCopy `json:"copies,omitzero"`
}

func (r *ProjectRecoveryCopiesResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ProjectRecoverRequest restores one retained recovery copy by digest.
type ProjectRecoverRequest struct {
	Project  string `json:"project"`
	Document string `json:"document"`
	Digest   string `json:"digest"`
}

// ProjectRecoverResult reports a successful document recovery.
type ProjectRecoverResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Root   string `json:"root,omitzero"`
}

func (r *ProjectRecoverResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// UpgradeCheckRequest names a staged candidate and the retained artifacts to review.
type UpgradeCheckRequest struct {
	Candidate string   `json:"candidate"`
	Projects  []string `json:"projects,omitzero"`
	Runs      []string `json:"runs,omitzero"`
}

// UpgradePrepareRequest takes the rollback archive after administrator approval.
type UpgradePrepareRequest struct {
	Project     string `json:"project"`
	Candidate   string `json:"candidate"`
	Destination string `json:"destination"`
	Approve     bool   `json:"approve"`
}

// UpgradePlanView is the upgrade plan plus the installer handoff the window states.
type UpgradePlanView struct {
	Plan             *upgrade.Plan `json:"plan"`
	InstallerHandoff string        `json:"installer_handoff"`
	Offline          string        `json:"offline"`
	SigningDeferred  string        `json:"signing_deferred"`
}

// UpgradeResult carries check or prepare outcomes.
type UpgradeResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	View   *UpgradePlanView  `json:"view,omitzero"`
	Report *BackupReportView `json:"report,omitzero"`
}

func (r *UpgradeResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

const (
	installerHandoffText  = "Application and service changes use the platform's own installer run by an administrator (apt-get, installer -pkg, msiexec). readmit never elevates, downloads, or interrupts a service when Settings or this screen opens."
	upgradeOfflineText    = "This check is offline. It opens no network connection and contacts no update service. Stage packages yourself, then point here at that folder."
	upgradeSigningText    = "Published signature and clean-machine release gates stay outside this screen. A development preview that is not signed for distribution is reported honestly and never auto-installed."
	retirementNotErasure  = "Unlinking a project is not forensic secure erasure and does not revoke remote copies. The recovery archive is retained."
	quotaExplainText      = "These are retained-file limits on this project directory, including recovery copies and indexes. They are not free-space reservations. Indexes are disposable and rebuilt from canonical evidence; they are never the only copy of work."
	migrationGuidanceText = "Supported documents stay unchanged. Indexes rebuild on restore. There is no in-place converter for an unknown schema and no silent rewrite of retained artifacts or historical verdicts. New semantics require a new compatible version."
)

// ChooseMaintenancePath presents the host's save dialog to name the new folder
// a backup, a restored project or a recovery or rollback archive is written
// into, and its folder picker for an existing backup source or staged upgrade
// candidate.
func (a *App) ChooseMaintenancePath(kind string) MaintenancePathResult {
	return run(a, true, false, func(ctx context.Context) MaintenancePathResult {
		var choose func(context.Context, string) (string, refusal)
		var title string
		switch kind {
		case "backup-destination":
			choose, title = a.chooseDestination, "Choose a new folder for the backup"
		case "restore-destination":
			choose, title = a.chooseDestination, "Choose a new folder for the restored project"
		case "backup-source":
			choose, title = a.chooseFolder, "Choose the backup folder to verify or restore"
		case "upgrade-candidate":
			choose, title = a.chooseFolder, "Choose the staged upgrade package folder"
		case "archive-destination":
			choose, title = a.chooseDestination, "Choose a new folder for the recovery archive"
		default:
			return MaintenancePathResult{State: Failed, Reason: "unknown maintenance path kind"}
		}
		folder, declined := choose(ctx, title)
		if folder == "" {
			return MaintenancePathResult{State: declined.state, Reason: declined.reason, Kind: kind}
		}
		return MaintenancePathResult{State: Completed, Kind: kind, Path: folder}
	})
}

// CreateProjectBackup copies a project into a new verified backup directory.
//
// Preserving evidence that already exists is not new work. This operation,
// restoring, recovering a document, archiving and taking an upgrade's
// rollback point are admitted the way their `readmit backup`, `readmit
// project` and `readmit upgrade` commands are — without a term — so an
// expired license never gates them (ADR-0007, ADR-0010).
func (a *App) CreateProjectBackup(request BackupCreateRequest) BackupResult {
	return run(a, true, false, func(ctx context.Context) BackupResult {
		root, declined := resolveProjectPath(request.Project)
		if root == "" {
			return BackupResult{State: declined.state, Reason: declined.reason}
		}
		if strings.TrimSpace(request.Destination) == "" {
			return BackupResult{State: Failed, Reason: "backup create requires a new destination folder"}
		}
		report, err := backup.Create(ctx, root, request.Destination)
		if err != nil {
			return classifyBackupErr(err)
		}
		view := viewFromReport(report)
		classifyProjectFiles(root, &view)
		state := Completed
		reason := ""
		if !report.Complete() {
			state = Failed
			reason = "this project holds evidence the backup could not verify; the backup records what it found and puts nothing in its place"
		}
		return BackupResult{State: state, Reason: reason, Report: &view}
	})
}

// VerifyProjectBackup reads a backup whole and reports what it holds.
func (a *App) VerifyProjectBackup(path string) BackupResult {
	return run(a, false, false, func(context.Context) BackupResult {
		document, err := backup.Verify(path)
		if err != nil {
			return classifyBackupErr(err)
		}
		view := viewFromDocument(path, document)
		state := Completed
		reason := ""
		if !document.Complete() {
			state = Failed
			reason = "this backup holds evidence it could not verify; nothing was put in its place"
		}
		return BackupResult{State: state, Reason: reason, Report: &view}
	})
}

// RestoreProjectBackup writes a backup into a new project directory and rebuilds indexes.
func (a *App) RestoreProjectBackup(request BackupRestoreRequest) BackupResult {
	return run(a, true, false, func(ctx context.Context) BackupResult {
		if strings.TrimSpace(request.Backup) == "" || strings.TrimSpace(request.Destination) == "" {
			return BackupResult{State: Failed, Reason: "restore requires a backup folder and a new destination"}
		}
		report, err := backup.Restore(ctx, request.Backup, request.Destination, time.Now().UTC())
		if err != nil {
			return classifyBackupErr(err)
		}
		view := viewFromReport(report)
		if report.Root != "" {
			classifyProjectFiles(report.Root, &view)
		}
		state := Completed
		reason := ""
		if !report.Complete() {
			state = Failed
			reason = "this backup holds evidence the restore could not account for; it is reported exactly as it was found and nothing was put in its place"
		}
		return BackupResult{State: state, Reason: reason, Report: &view}
	})
}

// InspectProjectQuota reports retained-file usage and any declared limits.
func (a *App) InspectProjectQuota(path string) ProjectQuotaResult {
	return run(a, false, false, func(context.Context) ProjectQuotaResult {
		root, declined := resolveProjectPath(path)
		if root == "" {
			return ProjectQuotaResult{State: declined.state, Reason: declined.reason}
		}
		view, err := readQuotaView(root)
		if err != nil {
			return ProjectQuotaResult{State: Failed, Reason: err.Error()}
		}
		return ProjectQuotaResult{State: Completed, Quota: &view}
	})
}

// SetProjectQuota declares both positive retained-file limits together.
func (a *App) SetProjectQuota(change ProjectQuotaChange) ProjectQuotaResult {
	return run(a, false, true, func(context.Context) ProjectQuotaResult {
		root, declined := resolveProjectPath(change.Project)
		if root == "" {
			return ProjectQuotaResult{State: declined.state, Reason: declined.reason}
		}
		if err := project.SetQuota(root, project.Quota{Schema: project.QuotaSchema, MaxBytes: change.MaxBytes, MaxFiles: change.MaxFiles}); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ProjectQuotaResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ProjectQuotaResult{State: Failed, Reason: err.Error()}
		}
		view, err := readQuotaView(root)
		if err != nil {
			return ProjectQuotaResult{State: Failed, Reason: err.Error()}
		}
		return ProjectQuotaResult{State: Completed, Quota: &view}
	})
}

// PreviewProjectMigration reports supported schemas without changing files.
func (a *App) PreviewProjectMigration(path string) MigrationPreviewResult {
	return run(a, true, false, func(ctx context.Context) MigrationPreviewResult {
		root, declined := resolveProjectPath(path)
		if root == "" {
			return MigrationPreviewResult{State: declined.state, Reason: declined.reason}
		}
		plan, err := lifecycle.Preview(ctx, root)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return MigrationPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return MigrationPreviewResult{State: Failed, Reason: err.Error()}
		}
		state := Completed
		reason := ""
		if !plan.Compatible {
			state = Failed
			reason = "unsupported or damaged documents; no migration available"
		}
		return MigrationPreviewResult{State: state, Reason: reason, Plan: &plan, Guidance: migrationGuidanceText}
	})
}

// PreviewProjectRetirement inventories what archive or delete would affect,
// through lifecycle's own retirement preview: the compatibility plan, the
// source as the backup limits measure it, and the selection token a later
// archive or delete must present.
func (a *App) PreviewProjectRetirement(path string) RetirementPreviewResult {
	return run(a, true, false, func(ctx context.Context) RetirementPreviewResult {
		root, declined := resolveProjectPath(path)
		if root == "" {
			return RetirementPreviewResult{State: declined.state, Reason: declined.reason}
		}
		retirement, err := lifecycle.PreviewRetirement(ctx, root)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return RetirementPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return RetirementPreviewResult{State: Failed, Reason: err.Error()}
		}
		return RetirementPreviewResult{
			State: Completed,
			Preview: &RetirementPreview{
				Selection:  retirement.Selection,
				Project:    root,
				Compatible: retirement.Compatible,
				Files:      retirement.Files,
				Bytes:      retirement.Bytes,
				Documents:  retirement.Documents,
				Explain:    "Archive writes a verified recovery backup and keeps the source. Delete does the same, then unlinks the source only when this selection still matches.",
				NotErasure: retirementNotErasure,
			},
		}
	})
}

// ArchiveOrDeleteProject creates a verified recovery archive, optionally deleting the source.
func (a *App) ArchiveOrDeleteProject(request ProjectArchiveRequest) BackupResult {
	return run(a, true, false, func(ctx context.Context) BackupResult {
		root, declined := resolveProjectPath(request.Project)
		if root == "" {
			return BackupResult{State: declined.state, Reason: declined.reason}
		}
		if strings.TrimSpace(request.Destination) == "" {
			return BackupResult{State: Failed, Reason: "archive and delete require a new recovery directory"}
		}
		if strings.TrimSpace(request.Selection) == "" {
			return BackupResult{State: Failed, Reason: "archive and delete require a current retirement preview selection"}
		}
		if request.Delete && !request.Confirm {
			return BackupResult{State: Failed, Reason: "project delete requires explicit confirmation; the recovery archive will be retained"}
		}
		// The selection the person previewed is what lifecycle binds the
		// operation to: a project changed after the preview is refused before
		// anything is written.
		report, operationErr := lifecycle.Archive(ctx, root, request.Destination, request.Selection, request.Delete)
		view := BackupReportView{}
		if report.Root != "" {
			view = viewFromReport(report)
		}
		if operationErr != nil {
			if errors.Is(operationErr, context.Canceled) {
				return BackupResult{State: Cancelled, Reason: cancelledRefusal.reason, Report: nonemptyReport(view)}
			}
			result := classifyBackupErr(operationErr)
			if report.Root != "" {
				result.Report = &view
			}
			return result
		}
		state := Completed
		reason := ""
		if !report.Complete() {
			state = Failed
			reason = "archive is incomplete; source retained"
		} else if request.Delete {
			reason = "Project unlinked; recovery archive retained. This is not secure erasure."
		}
		return BackupResult{State: state, Reason: reason, Report: &view}
	})
}

// RecoverProjectDocument restores one selected recovery copy and retains the current bytes.
func (a *App) RecoverProjectDocument(request ProjectRecoverRequest) ProjectRecoverResult {
	return run(a, false, false, func(context.Context) ProjectRecoverResult {
		root, declined := resolveProjectPath(request.Project)
		if root == "" {
			return ProjectRecoverResult{State: declined.state, Reason: declined.reason}
		}
		if err := project.Recover(root, request.Document, request.Digest); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ProjectRecoverResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ProjectRecoverResult{State: Failed, Reason: err.Error()}
		}
		return ProjectRecoverResult{State: Completed, Root: root}
	})
}

// ListProjectRecoveryCopies lists the recovery copies of the project's
// documents through the reader RecoverProjectDocument restores them with, so
// a person selects a copy by what it is rather than by a file name. It writes
// nothing; a project nothing replaced yet is empty.
func (a *App) ListProjectRecoveryCopies(path string) ProjectRecoveryCopiesResult {
	return run(a, false, false, func(context.Context) ProjectRecoveryCopiesResult {
		root, declined := resolveProjectPath(path)
		if root == "" {
			return ProjectRecoveryCopiesResult{State: declined.state, Reason: declined.reason}
		}
		copies, err := project.RecoveryCopies(root)
		if err != nil {
			return ProjectRecoveryCopiesResult{State: Failed, Reason: err.Error()}
		}
		result := ProjectRecoveryCopiesResult{State: Completed, Copies: make([]ProjectRecoveryCopy, 0, len(copies))}
		if len(copies) == 0 {
			result.State = Empty
		}
		for _, retained := range copies {
			result.Copies = append(result.Copies, ProjectRecoveryCopy{
				Document: retained.Document,
				Digest:   retained.Digest,
				Size:     retained.Size,
				State:    string(retained.State),
				Current:  retained.Current,
			})
		}
		return result
	})
}

// CheckStagedUpgrade reviews a staged candidate against named retained artifacts.
func (a *App) CheckStagedUpgrade(request UpgradeCheckRequest) UpgradeResult {
	return run(a, true, false, func(ctx context.Context) UpgradeResult {
		if strings.TrimSpace(request.Candidate) == "" {
			return UpgradeResult{State: Failed, Reason: "upgrade check requires the directory an administrator staged"}
		}
		if len(request.Projects)+len(request.Runs) == 0 {
			return UpgradeResult{State: Failed, Reason: "upgrade check requires at least one project or run: a check that reviewed nothing is not a compatibility review"}
		}
		plan, err := upgrade.Check(ctx, request.Candidate, request.Projects, request.Runs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return UpgradeResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return UpgradeResult{State: Failed, Reason: err.Error()}
		}
		view := &UpgradePlanView{
			Plan:             &plan,
			InstallerHandoff: installerHandoffText,
			Offline:          upgradeOfflineText,
			SigningDeferred:  upgradeSigningText,
		}
		state := Completed
		reason := ""
		if refused := plan.Refusal(); refused != nil {
			state = Failed
			reason = refused.Error()
		}
		return UpgradeResult{State: state, Reason: reason, View: view}
	})
}

// PrepareStagedUpgrade takes the verified recovery archive an upgrade rolls back to.
func (a *App) PrepareStagedUpgrade(request UpgradePrepareRequest) UpgradeResult {
	return run(a, true, false, func(ctx context.Context) UpgradeResult {
		root, declined := resolveProjectPath(request.Project)
		if root == "" {
			return UpgradeResult{State: declined.state, Reason: declined.reason}
		}
		if strings.TrimSpace(request.Candidate) == "" || strings.TrimSpace(request.Destination) == "" {
			return UpgradeResult{State: Failed, Reason: "upgrade prepare requires a staged candidate and a new recovery archive"}
		}
		if !request.Approve {
			return UpgradeResult{State: Failed, Reason: "upgrade prepare requires administrator approval before anything is written"}
		}
		plan, err := upgrade.Check(ctx, request.Candidate, []string{root}, nil)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return UpgradeResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return UpgradeResult{State: Failed, Reason: err.Error()}
		}
		view := &UpgradePlanView{
			Plan:             &plan,
			InstallerHandoff: installerHandoffText,
			Offline:          upgradeOfflineText,
			SigningDeferred:  upgradeSigningText,
		}
		if !plan.StagedIntact() {
			return UpgradeResult{State: Failed, Reason: upgrade.ErrNotStaged.Error(), View: view}
		}
		if !plan.RetainedReadable() {
			return UpgradeResult{State: Failed, Reason: upgrade.ErrNotReadable.Error(), View: view}
		}
		retirement, err := lifecycle.PreviewRetirement(ctx, root)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return UpgradeResult{State: Cancelled, Reason: cancelledRefusal.reason, View: view}
			}
			return UpgradeResult{State: Failed, Reason: err.Error(), View: view}
		}
		report, operationErr := lifecycle.Archive(ctx, root, request.Destination, retirement.Selection, false)
		var reportView *BackupReportView
		if report.Root != "" {
			v := viewFromReport(report)
			reportView = &v
		}
		if operationErr != nil {
			if errors.Is(operationErr, context.Canceled) {
				return UpgradeResult{State: Cancelled, Reason: cancelledRefusal.reason, View: view, Report: reportView}
			}
			return UpgradeResult{State: Failed, Reason: operationErr.Error(), View: view, Report: reportView}
		}
		state := Completed
		reason := ""
		if !report.Complete() {
			state = Failed
			reason = "rollback archive is incomplete"
		} else if refused := plan.Refusal(); refused != nil {
			reason = "Rollback point taken. Installing this candidate is still refused: " + refused.Error()
		}
		return UpgradeResult{State: state, Reason: reason, View: view, Report: reportView}
	})
}

func resolveProjectPath(path string) (string, refusal) {
	root, declined := resolveFolder(path)
	if root == "" {
		return "", declined
	}
	if _, err := project.Open(root); err != nil {
		return "", probeReadFailure(root)
	}
	return root, refusal{}
}

func readQuotaView(root string) (ProjectQuotaView, error) {
	q, present, err := project.ReadQuota(root)
	if err != nil {
		return ProjectQuotaView{}, err
	}
	usage, checkErr := project.CheckQuota(root)
	view := ProjectQuotaView{
		Declared:  present,
		UsedBytes: usage.Bytes,
		UsedFiles: usage.Files,
		Within:    checkErr == nil,
		Explain:   quotaExplainText,
	}
	if present {
		view.MaxBytes = q.MaxBytes
		view.MaxFiles = q.MaxFiles
	}
	return view, nil
}

func viewFromReport(report backup.Report) BackupReportView {
	view := BackupReportView{
		Root:        report.Root,
		Complete:    report.Complete(),
		Files:       report.Files,
		Bytes:       report.Bytes,
		Evidence:    []BackupInventoryEntry{},
		Mutable:     []BackupInventoryEntry{},
		Exclusions:  []BackupInventoryEntry{},
		Credentials: []BackupInventoryEntry{},
		Protection:  []BackupInventoryEntry{},
		Other:       []BackupInventoryEntry{},
	}
	for _, entry := range report.Evidence {
		view.Evidence = append(view.Evidence, BackupInventoryEntry{
			Path:        entry.Name,
			Class:       BackupClassEvidence,
			Kind:        string(entry.Kind),
			Identity:    entry.Identity,
			State:       string(entry.State),
			Recorded:    string(entry.Recorded),
			Explanation: "Canonical registered evidence. Incomplete accounts stay incomplete; nothing is substituted.",
		})
	}
	for _, entry := range report.Indexes {
		view.Exclusions = append(view.Exclusions, BackupInventoryEntry{
			Path:        entry.Name,
			Class:       string(project.ClassDeclaredExclusion),
			Case:        entry.Case,
			State:       string(entry.State),
			Explanation: "Derived index declarations only. Disposable; rebuilt from canonical evidence. Never the only copy of work.",
		})
	}
	return view
}

func viewFromDocument(root string, document backup.Document) BackupReportView {
	view := BackupReportView{
		Root:        root,
		Complete:    document.Complete(),
		Files:       len(document.Files),
		Evidence:    []BackupInventoryEntry{},
		Mutable:     []BackupInventoryEntry{},
		Exclusions:  []BackupInventoryEntry{},
		Credentials: []BackupInventoryEntry{},
		Protection:  []BackupInventoryEntry{},
		Other:       []BackupInventoryEntry{},
	}
	var bytes int64
	for _, file := range document.Files {
		bytes += file.Size
		entry := BackupInventoryEntry{Path: file.Path, Size: file.Size, SHA256: file.SHA256}
		switch class := project.ClassifyFile(file.Path, schemaOfStoredFile(root, file.Path)); class {
		case project.ClassMutableDocument:
			entry.Class = string(class)
			entry.Explanation = "Mutable project document. Edited separately from immutable evidence."
			view.Mutable = append(view.Mutable, entry)
		case project.ClassCredentialReference:
			entry.Class = string(class)
			entry.Explanation = "Credential reference document. References only; secret values are never exported."
			view.Credentials = append(view.Credentials, entry)
		case project.ClassProtectionKeyReference:
			entry.Class = string(class)
			entry.Explanation = "Protection control with an external key reference. Key material is never stored or rendered."
			view.Protection = append(view.Protection, entry)
		default:
			entry.Class = string(class)
			view.Other = append(view.Other, entry)
		}
	}
	view.Bytes = bytes
	for _, entry := range document.Evidence {
		view.Evidence = append(view.Evidence, BackupInventoryEntry{
			Path:        entry.Name,
			Class:       BackupClassEvidence,
			Kind:        string(entry.Kind),
			Identity:    entry.Identity,
			State:       string(entry.State),
			Explanation: "Canonical registered evidence recorded in the backup manifest.",
		})
	}
	for _, entry := range document.Indexes {
		view.Exclusions = append(view.Exclusions, BackupInventoryEntry{
			Path:        entry.Name,
			Class:       string(project.ClassDeclaredExclusion),
			Case:        entry.Case,
			Retention:   string(entry.Retention),
			State:       string(recordedIndex(entry)),
			Explanation: "Index recipe only. Values were not copied; restore rebuilds from evidence.",
		})
	}
	return view
}

func recordedIndex(entry backup.Index) backup.IndexState {
	if entry.Recipe == backup.Declared {
		return backup.IndexRecorded
	}
	if entry.Recipe == backup.Unregistered {
		return backup.IndexUnregistered
	}
	return backup.IndexUndeclared
}

func classifyProjectFiles(root string, view *BackupReportView) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		schema := peekSchema(filepath.Join(root, name))
		class := project.ClassifyFile(name, schema)
		row := BackupInventoryEntry{Path: name, Class: string(class), Size: info.Size()}
		switch class {
		case project.ClassMutableDocument:
			row.Explanation = "Mutable project document."
			if !containsPath(view.Mutable, name) {
				view.Mutable = append(view.Mutable, row)
			}
		case project.ClassCredentialReference:
			row.Explanation = "Credential reference document. Values stay in the external store."
			if !containsPath(view.Credentials, name) {
				view.Credentials = append(view.Credentials, row)
			}
		case project.ClassProtectionKeyReference:
			row.Explanation = "Protection key reference. Key material is never exported."
			if !containsPath(view.Protection, name) {
				view.Protection = append(view.Protection, row)
			}
		}
	}
}

func peekSchema(path string) string {
	data := mustReadPrefix(path)
	var probe struct {
		Schema string `json:"schema"`
	}
	_ = json.Unmarshal(data, &probe)
	return probe.Schema
}

func schemaOfStoredFile(backupRoot, relative string) string {
	return peekSchema(filepath.Join(backupRoot, backup.FilesDirectory, filepath.FromSlash(relative)))
}

func mustReadPrefix(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 4096))
	return data
}

func containsPath(entries []BackupInventoryEntry, name string) bool {
	for _, entry := range entries {
		if entry.Path == name {
			return true
		}
	}
	return false
}

func nonemptyReport(view BackupReportView) *BackupReportView {
	if view.Root == "" {
		return nil
	}
	return &view
}

func classifyBackupErr(err error) BackupResult {
	if err == nil {
		return BackupResult{State: Failed, Reason: "backup operation failed"}
	}
	if errors.Is(err, context.Canceled) {
		return BackupResult{State: Cancelled, Reason: cancelledRefusal.reason}
	}
	msg := err.Error()
	if errors.Is(err, fs.ErrPermission) || strings.Contains(msg, "permission") {
		return BackupResult{State: PermissionDenied, Reason: "this account cannot write into the chosen folder"}
	}
	if strings.Contains(msg, "no space") || strings.Contains(msg, "disk full") {
		return BackupResult{State: Failed, Reason: "the destination volume has no space for this backup"}
	}
	return BackupResult{State: Failed, Reason: msg}
}
