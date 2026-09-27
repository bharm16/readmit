package desktop

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/upgrade"
)

// Storage's tasks that write or delete are reviewed actions: the review
// names the exact backup, project and folder, the final click executes that
// binding once, and anything it bound that changed is a stale review. Each
// task writes somewhere new or deletes exactly what it names; none of them
// overwrites a project, resumes traffic, installs or runs anything.

// The reviewed storage actions.
const (
	StorageRestoreBackupAction ActionID = "storage.restore-backup"
	StorageDeleteBackupAction  ActionID = "storage.delete-backup"
	StorageArchiveCopyAction   ActionID = "storage.archive-copy"
	StorageDeleteSourceAction  ActionID = "storage.delete-source"
	StorageMoveProjectAction   ActionID = "storage.move-project"
	StorageRestoreCopyAction   ActionID = "storage.restore-copy"
	StoragePrepareUpdateAction ActionID = "storage.prepare-update"
)

const (
	// RestoreConsent: the displayed backup or recovery copy is restored as
	// displayed.
	RestoreConsent Consent = "restore"
	// DeleteConsent: exactly the displayed backup or source is deleted.
	DeleteConsent Consent = "delete"
	// CopyConsent: the displayed project is copied into a new verified
	// folder; its source is kept.
	CopyConsent Consent = "copy"
	// PrepareConsent: a rollback copy is taken and the displayed candidate is
	// recorded as prepared; nothing is installed or run.
	PrepareConsent Consent = "prepare"
)

// The consequences a storage review states, in the words Storage shows.
const (
	restoreConsequence      = "Creates a separate project; the current project stays unchanged."
	deleteBackupConsequence = "Deletes this backup; the source project remains."
	archiveConsequence      = "Copies the selected project into a new verified archive; the source is kept."
	archiveCaseConsequence  = "Copies the selected case into a new verified archive; the case is kept."
	deleteSourceConsequence = "Archives the selected source, then deletes it from this computer; this is not secure erasure."
	moveConsequence         = "Copies the project to the chosen folder, verifies it and opens it there; the project in its current folder is kept until you delete it."
	restoreCopyConsequence  = "Creates a separate project with this earlier document; the current project stays unchanged. Nothing is sent or resumed."
	prepareConsequence      = "Takes a verified rollback copy of this project and records this staged candidate as prepared. Nothing is installed or run."
)

// StorageActionOptions are what a storage review is prepared for, beyond the
// project in its context: the listed backup it is about, the name a restored
// project gets, the folder a native dialog chose for its output, the
// recovery copy to restore, or the staged candidate to prepare. An empty
// Name is the backup's title plus "restored"; an empty Location is the
// folder the task's output is kept in by default.
type StorageActionOptions struct {
	Backup    string `json:"backup,omitzero"`
	Name      string `json:"name,omitzero"`
	Location  string `json:"location,omitzero"`
	Document  string `json:"document,omitzero"`
	Digest    string `json:"digest,omitzero"`
	Candidate string `json:"candidate,omitzero"`
}

// StorageReview is what one storage task will do: the project and backup it
// is about, what the backup holds, the name and folder its output gets, the
// scope it copies — files, bytes and the documents in it — the recovery copy
// or staged update it concerns, the retention its source declares and the
// related work that refers to it, and its consequence in Storage's words. A
// task about one case of the project names that case as its review's item.
type StorageReview struct {
	Project     string                    `json:"project,omitzero"`
	ProjectID   string                    `json:"project_id,omitzero"`
	Backup      *StorageBackup            `json:"backup,omitzero"`
	Contents    *BackupReportView         `json:"contents,omitzero"`
	Name        string                    `json:"name,omitzero"`
	Location    string                    `json:"location,omitzero"`
	Files       int                       `json:"files,omitzero"`
	Bytes       int64                     `json:"bytes,omitzero"`
	Documents   []lifecycle.Compatibility `json:"documents,omitzero"`
	Copy        *ProjectRecoveryCopy      `json:"copy,omitzero"`
	Upgrade     *UpgradePlanView          `json:"upgrade,omitzero"`
	Retention   []RetentionHold           `json:"retention,omitzero"`
	Related     []RelatedWork             `json:"related,omitzero"`
	Consequence string                    `json:"consequence"`
}

// StorageOutcome is what one storage task did: the project it opened (a
// restored or moved project), the backup it wrote (an archive or rollback
// copy), what happened to a deleted source and the remainder a removal that
// stopped left, the incomplete staging folder a failed restore or move kept,
// and the candidate a preparation recorded.
type StorageOutcome struct {
	Project    *ProjectOpenResult    `json:"project,omitzero"`
	Backup     *StorageBackup        `json:"backup,omitzero"`
	Source     lifecycle.RetireState `json:"source,omitzero"`
	Remainder  string                `json:"remainder,omitzero"`
	Incomplete string                `json:"incomplete,omitzero"`
	Candidate  *PreparedCandidate    `json:"candidate,omitzero"`
}

// storagePlan is everything a storage binding resolved, which its execution
// acts on.
type storagePlan struct {
	source    *storageSource
	item      *storageCase
	backup    *locatedBackup
	archive   *storageRecord
	name      string
	location  string
	selection string
	document  string
	digest    string
	candidate *PreparedCandidate
}

func init() {
	interruptible := slot{interruptible: true}
	maps.Copy(actionPolicies, map[ActionID]actionPolicy{
		StorageRestoreBackupAction: {consent: RestoreConsent, perform: interruptible, bind: bindRestoreBackup, execute: executeRestoreBackup},
		StorageDeleteBackupAction:  {consent: DeleteConsent, bind: bindDeleteBackup, execute: executeDeleteBackup},
		StorageArchiveCopyAction:   {consent: CopyConsent, perform: interruptible, bind: bindArchiveCopy, execute: executeArchiveCopy},
		StorageDeleteSourceAction:  {consent: DeleteConsent, perform: interruptible, bind: bindDeleteSource, execute: executeDeleteSource},
		StorageMoveProjectAction:   {consent: CopyConsent, perform: interruptible, bind: bindMoveProject, execute: executeMoveProject},
		StorageRestoreCopyAction:   {consent: RestoreConsent, perform: interruptible, bind: bindRestoreCopy, execute: executeRestoreCopy},
		StoragePrepareUpdateAction: {consent: PrepareConsent, perform: interruptible, bind: bindPrepareUpdate, execute: executePrepareUpdate},
	})
}

func storageOptions(request PrepareActionRequest) StorageActionOptions {
	if request.Storage == nil {
		return StorageActionOptions{}
	}
	return *request.Storage
}

// writableFolder is a folder a native dialog chose, while it is an existing
// folder, not a link, this account can create a folder in.
func writableFolder(path string) (string, refusal) {
	root, declined := resolveFolder(path)
	if root == "" {
		return "", declined
	}
	probe, err := os.MkdirTemp(root, ".readmit-access-")
	if err != nil {
		return "", refusal{PermissionDenied, "this account cannot create a folder in the chosen folder; choose another"}
	}
	os.Remove(probe)
	return root, refusal{}
}

// outputFolder is where a task's output goes: the folder chosen for it, or
// the backup folder.
func (a *App) outputFolder(chosen string) (string, refusal) {
	if strings.TrimSpace(chosen) != "" {
		return writableFolder(chosen)
	}
	location, _, declined := a.usableBackupLocation()
	return location, declined
}

// staging names a new hidden folder in parent for a copy that is not yet a
// project. A listing never offers it, and a copy that stops part way stays
// there as it is, recoverable and never accepted.
func staging(parent, purpose string) string {
	raw := make([]byte, 8)
	rand.Read(raw)
	return filepath.Join(parent, ".readmit-"+purpose+"-"+hex.EncodeToString(raw))
}

// restoredName is the default name of a restored project: its title and
// "restored", the title shortened, a character at a time, until the whole
// name keeps the title rule of the document it is written into.
func restoredName(title string, check func(string) error) string {
	const suffix = " restored"
	runes := []rune(strings.TrimSpace(title))
	for len(runes) > 0 {
		if name := strings.TrimSpace(string(runes)) + suffix; validName(name, check) {
			return name
		}
		runes = runes[:len(runes)-1]
	}
	return "Restored project"
}

// storageBinding is the binding every storage review shares: the action, the
// reviewer, the operation policy and the admission it grants, and the
// action's own parts.
func (a *App) storageBinding(ctx context.Context, action ActionID, held bool, parts ...string) string {
	return binding(append([]string{string(action), a.reviewer(), a.policyBinding(ctx, false, held)}, parts...)...)
}

// reviewedBackup reads the listed backup a review names and verifies it whole
// now: selecting a backup for a task already verifies what the task needs.
func (a *App) reviewedBackup(options StorageActionOptions) (*locatedBackup, *BackupReportView, string, refusal) {
	if options.Backup == "" {
		return nil, nil, "", refusal{Failed, "a backup task names one listed backup"}
	}
	found, declined := a.findBackup(options.Backup)
	if found == nil {
		return nil, nil, "", declined
	}
	if !found.available() {
		return found, nil, found.row.Problem, refusal{}
	}
	document, err := backup.Verify(found.path)
	if err != nil {
		return found, nil, "the backup does not read back whole: " + err.Error(), refusal{}
	}
	view := viewFromDocument(found.path, document)
	if !document.Complete() {
		return found, &view, "the backup holds evidence it could not verify; restoring it would not be a whole project", refusal{}
	}
	return found, &view, "", refusal{}
}

func bindRestoreBackup(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	options := storageOptions(request)
	found, contents, problem, declined := a.reviewedBackup(options)
	if found == nil {
		return nil, declined
	}
	schema := project.SchemaV2
	if stored, err := project.Open(filepath.Join(found.path, backup.FilesDirectory)); err == nil {
		schema = stored.Document.Schema
	}
	check := func(name string) error { return project.CheckTitle(schema, name) }
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = restoredName(found.row.Project, check)
	}
	if !validName(name, check) {
		return nil, refusal{Failed, nameRule}
	}
	// A restored project is a project: it goes where new projects are
	// created, or beside the backups when no projects folder is chosen.
	location, declined := a.newProjectFolder(options.Location)
	if location == "" {
		return nil, declined
	}
	review := StorageReview{Project: found.row.Project, ProjectID: found.row.ProjectID, Backup: &found.row, Contents: contents,
		Name: name, Location: location, Consequence: restoreConsequence}
	return &boundAction{action: StorageRestoreBackupAction, origin: request,
		storage: storagePlan{backup: found, name: name, location: location},
		binding: a.storageBinding(ctx, StorageRestoreBackupAction, held, found.path, found.identity, name, location),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Storage: &review, Destination: ReviewDestination{Output: location}}}, noRefusal
}

// executeRestoreBackup restores the bound backup into a hidden folder beside
// where it will be, gives it the reviewed name and an identity of its own —
// so the project it was taken of keeps its own and its place among the
// projects this viewer opened — verifies it, and only then names it and opens
// it. A restore that stops or fails part way leaves the hidden folder as it
// is: recoverable, never an accepted project, and never the original.
func executeRestoreBackup(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	result := ReviewedActionResult{Outcome: ActionCompleted, State: Completed, Storage: &StorageOutcome{}}
	fail := func(state State, reason, incomplete string) ReviewedActionResult {
		result.State, result.Reason, result.Storage.Incomplete = state, reason, incomplete
		result.Outcome = ActionRefused
		if state == Cancelled {
			result.Outcome = ActionCancelled
		}
		return result
	}
	hidden := staging(plan.location, "restoring")
	report, err := backup.Restore(ctx, plan.backup.path, hidden, a.now().UTC())
	kept := ""
	if _, statErr := os.Lstat(hidden); statErr == nil {
		kept = hidden
	}
	switch {
	case err != nil && ctx.Err() != nil:
		return fail(Cancelled, "the restore was stopped; what it wrote is kept as an incomplete restore and is not a project", kept)
	case err != nil:
		failed := classifyBackupErr(err)
		return fail(failed.State, failed.Reason, kept)
	case !report.Complete():
		return fail(Failed, "the backup did not come back whole; the incomplete restore is kept and is not a project", kept)
	}
	if reason := a.giveIdentity(hidden, plan.name); reason != "" {
		return fail(Failed, reason, kept)
	}
	folder, declined := newChild(plan.location, plan.name)
	if folder == "" {
		return fail(declined.state, declined.reason, kept)
	}
	if err := os.Rename(hidden, folder); err != nil {
		return fail(Failed, "the restored project could not be named; it is kept as an incomplete restore", kept)
	}
	opened := a.openNamed(ctx, folder, false)
	result.Storage.Project = &opened
	if opened.State != Completed {
		result.State, result.Reason = opened.State, opened.Reason
	}
	return result
}

// giveIdentity names a restored copy and records a new catalog identity for
// it, then reads both back.
func (a *App) giveIdentity(root, name string) string {
	opened, err := project.Open(root)
	if err != nil {
		return "the restored project document cannot be read; the incomplete restore is kept"
	}
	if opened.Document.Settings.Title != name {
		document := opened.Document
		document.Settings.Title = name
		if err := opened.Save(document); err != nil {
			return "the restored project could not be named; the incomplete restore is kept"
		}
	}
	store, err := catalog.Open(root)
	if err != nil {
		return "the restored project's catalog cannot be opened; the incomplete restore is kept"
	}
	document, present, err := store.Read()
	if err != nil {
		return "the restored project's catalog cannot be read; the incomplete restore is kept"
	}
	fresh, err := catalog.Fresh(a.now())
	if err != nil {
		return "no new identity could be made for the restored project"
	}
	if present {
		document.Project = fresh.Project
	} else {
		document = fresh
	}
	if err := store.Write(document); err != nil {
		return "the restored project's identity could not be recorded; the incomplete restore is kept"
	}
	reread, err := project.Open(root)
	written, present, readErr := store.Read()
	if err != nil || readErr != nil || !present || reread.Document.Settings.Title != name || written.Project.ID != fresh.Project.ID {
		return "the restored project does not read back as it was written; the incomplete restore is kept"
	}
	return ""
}

func bindDeleteBackup(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	options := storageOptions(request)
	if options.Backup == "" {
		return nil, refusal{Failed, "a backup task names one listed backup"}
	}
	found, declined := a.findBackup(options.Backup)
	if found == nil {
		return nil, declined
	}
	// A backup is deleted by what its sealed manifest names, so one without a
	// readable seal cannot be; one whose evidence was incomplete can.
	problem := ""
	if found.identity == "" || found.row.Availability != ItemAvailable && found.row.Availability != BackupIncomplete {
		problem = cmp.Or(found.row.Problem, "this backup cannot be deleted here")
	}
	review := StorageReview{Project: found.row.Project, ProjectID: found.row.ProjectID, Backup: &found.row, Location: filepath.Dir(found.path),
		Consequence: deleteBackupConsequence}
	return &boundAction{action: StorageDeleteBackupAction, origin: request, storage: storagePlan{backup: found},
		binding: a.storageBinding(ctx, StorageDeleteBackupAction, held, found.path, found.identity),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Storage: &review}}, noRefusal
}

func executeDeleteBackup(a *App, _ context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	found := bound.storage.backup
	if err := backup.Remove(found.path, found.identity); err != nil {
		result := ReviewedActionResult{Outcome: ActionRefused}
		result.refuse(Failed, err.Error())
		if _, statErr := os.Lstat(filepath.Join(found.path, backup.MarkerName)); errors.Is(statErr, os.ErrNotExist) {
			// The marker went first: what is left reads as incomplete.
			result.Outcome = ActionCompleted
		}
		return result
	}
	a.forgetBackup(found.path)
	return ReviewedActionResult{State: Completed, Outcome: ActionCompleted, Storage: &StorageOutcome{Backup: &found.row}}
}

// retirementOf previews what copying or deleting a project affects.
func retirementOf(ctx context.Context, root string) (lifecycle.Retirement, refusal) {
	retirement, err := lifecycle.PreviewRetirement(ctx, root)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return retirement, cancelledRefusal
		}
		return retirement, refusal{Failed, err.Error()}
	}
	return retirement, refusal{}
}

// storageCase is the one case of the project an archive or a deletion is
// about: its catalog object, the entry its folder is, the title its archive
// is named by, and the identity the project recorded for it.
type storageCase struct {
	item     CatalogItem
	entry    string
	title    string
	identity string
}

// retiring is the source of an archive or a deletion as its review states it:
// the project, and the one case when the request names one, with what it
// copies, the retention it declares and the related work that refers to it.
type retiring struct {
	source     *storageSource
	item       *storageCase
	retirement lifecycle.Retirement
	holds      []RetentionHold
	related    []RelatedWork
}

// retiringOf reads the project a request names and, when its items name one
// of its cases, that case; then what archiving or deleting it affects.
func (a *App) retiringOf(ctx context.Context, request PrepareActionRequest) (*retiring, refusal) {
	source, declined := sourceOf(request.Context)
	if source == nil {
		return nil, declined
	}
	found := &retiring{source: source}
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		return nil, declined
	}
	identity := ""
	switch {
	case len(request.Items) == 0:
	case len(request.Items) == 1 && request.Items[0].Kind == CaseItem:
		_, items, records, declined := a.scoped(ctx, request.Context, request.Items)
		if items == nil {
			return nil, declined
		}
		registered := loaded.registeredCase(items[0].Ref.ID)
		if registered == nil {
			return nil, refusal{Failed, "only a case the project registers is archived or deleted on its own"}
		}
		found.item = &storageCase{item: items[0], entry: records[0].Entry, title: cmp.Or(records[0].Name, registered.Title, registered.Name),
			identity: registered.Identity}
		identity = registered.Identity
	default:
		return nil, refusal{Failed, "an archive or a deletion names the project or one of its cases"}
	}
	if found.item == nil {
		found.retirement, declined = retirementOf(ctx, source.root)
	} else {
		found.retirement, declined = caseRetirementOf(ctx, source.root, found.item.entry)
	}
	if declined.state != "" {
		return nil, declined
	}
	entry, id := "", ""
	if found.item != nil {
		entry, id = found.item.entry, found.item.item.Ref.ID
	}
	holds, err := retentionOf(source.root, entry, identity, a.now())
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	found.holds, found.related = holds, relatedOf(loaded, id)
	return found, refusal{}
}

// caseRetirementOf previews what archiving or deleting one case affects.
func caseRetirementOf(ctx context.Context, root, entry string) (lifecycle.Retirement, refusal) {
	retirement, err := lifecycle.PreviewCaseRetirement(ctx, root, entry)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return retirement, cancelledRefusal
		}
		return retirement, refusal{Failed, err.Error()}
	}
	return retirement, refusal{}
}

// parts are what a binding of the source covers: the project, the case and
// the retention declared for it.
func (r *retiring) parts() []string {
	parts := []string{r.source.root, r.source.id}
	if r.item != nil {
		parts = append(parts, r.item.item.Ref.ID, r.item.entry, r.item.identity)
	}
	return append(parts, holdParts(r.holds)...)
}

// review is the review of an archive or a deletion of the source.
func (r *retiring) review(consequence string) StorageReview {
	return StorageReview{Project: r.source.title, ProjectID: r.source.id, Files: r.retirement.Files, Bytes: r.retirement.Bytes,
		Documents: r.retirement.Documents, Retention: r.holds, Related: r.related, Consequence: consequence}
}

// items are the review's items: the case, when the source is one.
func (r *retiring) items() []CatalogItem {
	if r.item == nil {
		return []CatalogItem{}
	}
	return []CatalogItem{r.item.item}
}

func bindArchiveCopy(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	found, declined := a.retiringOf(ctx, request)
	if found == nil {
		return nil, declined
	}
	location, declined := a.outputFolder(storageOptions(request).Location)
	if location == "" {
		return nil, declined
	}
	problem := ""
	if !found.retirement.Compatible {
		problem = "the project holds documents this release cannot read; no archive is taken"
	}
	consequence := archiveConsequence
	if found.item != nil {
		consequence = archiveCaseConsequence
	}
	review := found.review(consequence)
	review.Location = location
	return &boundAction{action: StorageArchiveCopyAction, origin: request,
		storage: storagePlan{source: found.source, item: found.item, location: location, selection: found.retirement.Selection},
		binding: a.storageBinding(ctx, StorageArchiveCopyAction, held, append(found.parts(), found.retirement.Selection, location)...),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Items: found.items(), Storage: &review, Destination: ReviewDestination{Output: location}}}, noRefusal
}

// archived writes the verified archive of a bound source, as a retirement
// takes it, into a new folder of the plan's location, and records it.
func (a *App) archived(ctx context.Context, plan storagePlan, reason BackupReason, annotate func(*storageRecord)) (*locatedBackup, refusal) {
	if item := plan.item; item != nil {
		// A case's archive is named and recorded by the case's title.
		named := &storageSource{root: plan.source.root, title: item.title, id: plan.source.id}
		return a.writeBackup(ctx, named, plan.location, reason, func(folder string) (backup.Report, error) {
			return lifecycle.ArchiveCase(ctx, plan.source.root, item.entry, item.title, folder, plan.selection)
		}, func(record *storageRecord) {
			record.Case = item.item.Ref.ID
			annotate(record)
		})
	}
	return a.writeBackup(ctx, plan.source, plan.location, reason, func(folder string) (backup.Report, error) {
		return lifecycle.Archive(ctx, plan.source.root, folder, plan.selection, false)
	}, annotate)
}

func executeArchiveCopy(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	made, declined := a.archived(ctx, plan, BackupReasonArchive, func(record *storageRecord) {
		record.Source, record.Selection = plan.source.root, plan.selection
	})
	return storageWritten(made, declined)
}

// storageWritten answers a task that wrote one backup.
func storageWritten(made *locatedBackup, declined refusal) ReviewedActionResult {
	if made == nil {
		result := ReviewedActionResult{Outcome: ActionRefused}
		if declined.state == Cancelled {
			result.Outcome = ActionCancelled
		}
		result.refuse(declined.state, declined.reason)
		return result
	}
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted, Storage: &StorageOutcome{Backup: &made.row, Candidate: made.row.Candidate}}
	if !made.available() {
		result.State, result.Reason = Failed, made.row.Problem
	}
	return result
}

// archiveOf is the recorded archive association a source may be deleted
// against: the one the review names, or the newest archive recorded of it —
// of the project whole, or of the one case with id.
func (a *App) archiveOf(source *storageSource, chosen, id string) (*storageRecord, refusal) {
	document, err := a.readStorage()
	if err != nil {
		return nil, unreadableStorage
	}
	for i := range document.Records {
		record := document.Records[i]
		ofSource := record.Source == source.root || source.id != "" && record.ProjectID == source.id
		if record.Reason != BackupReasonArchive || !ofSource || record.Case != id {
			continue
		}
		if chosen == "" || backupID(record.Path, record.Identity) == chosen {
			return &record, refusal{}
		}
	}
	return nil, refusal{}
}

func bindDeleteSource(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	found, declined := a.retiringOf(ctx, request)
	if found == nil {
		return nil, declined
	}
	source := found.source
	review := found.review(deleteSourceConsequence)
	bound := &boundAction{action: StorageDeleteSourceAction, origin: request, storage: storagePlan{source: source, item: found.item},
		review: ActionReview{Items: found.items(), Storage: &review}}
	refuse := func(reason string, parts ...string) (*boundAction, refusal) {
		bound.review.Refusal = reason
		bound.binding = a.storageBinding(ctx, StorageDeleteSourceAction, held, append(append(found.parts(), reason), parts...)...)
		return bound, noRefusal
	}
	id := ""
	if item := found.item; item != nil {
		id = item.item.Ref.ID
		if remainder := lifecycle.CaseRemainder(source.root, item.entry); exists(remainder) {
			return refuse("an earlier deletion of this case left a remainder at " + remainder + "; it is not deleted again")
		}
		opened, err := project.Open(source.root)
		revisions, readErr := project.ReadRevisions(source.root)
		if err != nil || readErr != nil {
			return refuse("the project's documents cannot be read; nothing is deleted")
		}
		if _, err := project.RemoveCase(opened.Document, revisions, item.entry); errors.Is(err, project.ErrCaseNamed) {
			return refuse("a note or a variant of this project names this case; remove the note or the variant first. Nothing is deleted")
		}
	} else if exists(source.root + ".retiring") {
		return refuse("an earlier deletion of this project left a remainder at " + source.root + ".retiring; it is not deleted again")
	}
	if held := blockingHold(found.holds); held != "" {
		return refuse(held)
	}
	record, declined := a.archiveOf(source, storageOptions(request).Backup, id)
	if declined.state != "" {
		return nil, declined
	}
	if record == nil {
		what := "this project has no archive copy yet"
		if found.item != nil {
			what = "this case has no archive copy yet"
		}
		return refuse(what + "; archive it first, and it is deleted only against that verified copy")
	}
	archive := inspectBackup(record.Path, record)
	review.Backup, review.Location = &archive.row, filepath.Dir(record.Path)
	if !archive.available() {
		return refuse(archive.row.Problem, record.Path, archive.identity)
	}
	document, err := backup.Verify(record.Path)
	if err != nil || !document.Complete() {
		return refuse("the archive copy does not read back whole; the source is not deleted against it", record.Path, archive.identity)
	}
	contents := viewFromDocument(record.Path, document)
	review.Contents = &contents
	if found.retirement.Selection != record.Selection {
		changed := "the project changed since it was archived; archive it again before deleting it"
		if found.item != nil {
			changed = "the case changed since it was archived; archive it again before deleting it"
		}
		return refuse(changed, record.Path, archive.identity, found.retirement.Selection)
	}
	bound.storage.archive, bound.storage.backup, bound.storage.selection = record, &archive, found.retirement.Selection
	bound.review.Ready = true
	bound.binding = a.storageBinding(ctx, StorageDeleteSourceAction, held, append(found.parts(), record.Path, archive.identity, found.retirement.Selection)...)
	return bound, noRefusal
}

// executeDeleteSource deletes the bound source against its recorded archive
// and nothing else: it never takes a second archive. A deletion that stopped
// part way says so, names what remains, and keeps the archive. A case is
// taken off the project, as removing it from the project does, in the same
// step that deletes its folder.
func executeDeleteSource(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	var outcome lifecycle.Outcome
	var err error
	if item := plan.item; item != nil {
		outcome, err = lifecycle.RetireCase(ctx, plan.source.root, item.entry, plan.archive.Path, plan.selection, func() error {
			return a.takeCaseOff(plan.source.root, item.item.Ref.ID, item.entry)
		})
	} else {
		outcome, err = lifecycle.Retire(ctx, plan.source.root, plan.archive.Path, plan.selection)
	}
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted,
		Storage: &StorageOutcome{Backup: &plan.backup.row, Source: outcome.State, Remainder: outcome.Remainder}}
	switch {
	case outcome.State == lifecycle.RemovalIncomplete:
		result.State, result.Reason = Failed, "the archive copy is kept and the source was only partly deleted; what remains is at "+outcome.Remainder
	case err != nil && ctx.Err() != nil:
		result.Outcome = ActionCancelled
		result.State, result.Reason = Cancelled, "the deletion was stopped before the source was removed; the source is kept"
	case err != nil:
		result.Outcome = ActionRefused
		result.State, result.Reason = Failed, err.Error()
	}
	return result
}

// takeCaseOff records in the project's catalog that the case with id was
// removed and then unregisters it, as removing a case from the project does.
func (a *App) takeCaseOff(root, id, entry string) error {
	store, err := catalog.Open(root)
	if err != nil {
		return errors.New("the project's catalog cannot be opened; the case is kept")
	}
	if _, err := store.Update(a.now(), func(document *catalog.Document) (bool, error) {
		at := document.Find(id)
		if at < 0 {
			// A case the catalog has discovered but never recorded is
			// recorded now, under the identity every read derived for it, so
			// that it is recorded as removed.
			document.Items = append(document.Items, catalog.Item{Kind: string(CaseItem), ID: id, Entry: entry, Revisions: []catalog.Revision{}})
			at = len(document.Items) - 1
		}
		document.Items[at].RemovedAt = catalog.Stamp(a.now())
		return true, nil
	}); err != nil {
		return errors.New("the project's catalog could not record that the case was removed; the case is kept")
	}
	if err := operation.UnregisterCase(root, entry); err != nil {
		return errors.New("the case could not be taken off the project; it is kept: " + err.Error())
	}
	return nil
}

// exists reports whether anything is at path.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func bindMoveProject(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	source, declined := sourceOf(request.Context)
	if source == nil {
		return nil, declined
	}
	if source.id == "" {
		return nil, refusal{Failed, "open the project first, so its identity is recorded before it moves"}
	}
	chosen := storageOptions(request).Location
	if strings.TrimSpace(chosen) == "" {
		return nil, refusal{Failed, "a move names the folder the project goes to"}
	}
	location, declined := writableFolder(chosen)
	if location == "" {
		return nil, declined
	}
	retirement, declined := retirementOf(ctx, source.root)
	if declined.state != "" {
		return nil, declined
	}
	problem := ""
	switch {
	case location == filepath.Dir(source.root):
		problem = "the project is already in that folder"
	case location == source.root || strings.HasPrefix(location, source.root+string(filepath.Separator)):
		problem = "a project cannot be moved into itself"
	}
	review := StorageReview{Project: source.title, ProjectID: source.id, Location: location, Files: retirement.Files, Bytes: retirement.Bytes,
		Documents: retirement.Documents, Consequence: moveConsequence}
	return &boundAction{action: StorageMoveProjectAction, origin: request,
		storage: storagePlan{source: source, location: location, selection: retirement.Selection},
		binding: a.storageBinding(ctx, StorageMoveProjectAction, held, source.root, source.id, retirement.Selection, location),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Storage: &review, Destination: ReviewDestination{Output: location}}}, noRefusal
}

// executeMoveProject copies the bound project whole into a hidden folder of
// the chosen one, verifies every file of the copy against the source, which
// must still be the reviewed bytes, and only then names it and switches the
// project this viewer opened to it. The project keeps its identity; the copy
// in its previous folder is kept, and deleting it is a separate task.
func executeMoveProject(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted, Storage: &StorageOutcome{}}
	hidden := staging(plan.location, "moving")
	fail := func(state State, reason string) ReviewedActionResult {
		result.State, result.Reason, result.Outcome = state, reason, ActionRefused
		if state == Cancelled {
			result.Outcome = ActionCancelled
		}
		if _, err := os.Lstat(hidden); err == nil {
			result.Storage.Incomplete = hidden
		}
		return result
	}
	if err := lifecycle.Copy(ctx, plan.source.root, hidden); err != nil {
		if ctx.Err() != nil {
			return fail(Cancelled, "the move was stopped; the project stays where it was")
		}
		return fail(Failed, err.Error()+"; the project stays where it was")
	}
	if err := lifecycle.VerifyCopy(ctx, plan.source.root, hidden); err != nil {
		return fail(Failed, err.Error()+"; the project stays where it was")
	}
	if retirement, declined := retirementOf(ctx, plan.source.root); declined.state != "" || retirement.Selection != plan.selection {
		return fail(Failed, "the project changed while it was copied; it stays where it was")
	}
	if copied, err := sourceOf(RequestContext{Project: hidden}); err.state != "" || copied.id != plan.source.id {
		return fail(Failed, "the copy does not keep the project's identity; the project stays where it was")
	}
	folder, declined := newChild(plan.location, plan.source.title)
	if folder == "" {
		return fail(declined.state, declined.reason)
	}
	if err := os.Rename(hidden, folder); err != nil {
		return fail(Failed, "the copy could not be named; the project stays where it was")
	}
	opened := a.openNamed(ctx, folder, false)
	result.Storage.Project = &opened
	if opened.State != Completed {
		result.State, result.Reason = opened.State, opened.Reason
	}
	return result
}

func bindRestoreCopy(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	source, declined := sourceOf(request.Context)
	if source == nil {
		return nil, declined
	}
	options := storageOptions(request)
	copies, err := project.RecoveryCopies(source.root)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	at := slices.IndexFunc(copies, func(copy project.RecoveryCopy) bool {
		return copy.Document == options.Document && copy.Digest == options.Digest
	})
	if at < 0 {
		return nil, refusal{Failed, "the project holds no such recovery copy; list the copies again"}
	}
	retained := copies[at]
	problem := ""
	switch {
	case retained.State != project.RecoveryReadable:
		problem = "this recovery copy is " + string(retained.State) + "; it cannot be restored"
	case retained.Current:
		problem = "this copy is the document as it stands; there is nothing to restore"
	}
	// The restored project is named, by default, after the title it will
	// hold: the copy's own, when the copy is the project document.
	title, schema := source.title, project.SchemaV2
	if opened, err := project.Open(source.root); err == nil {
		schema = opened.Document.Schema
	}
	if recovered, err := project.ReadRecoveryCopy(source.root, retained.Document, retained.Digest); err == nil && recovered.Project != nil {
		title, schema = recovered.Project.Settings.Title, recovered.Project.Schema
	}
	check := func(name string) error { return project.CheckTitle(schema, name) }
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = restoredName(title, check)
	}
	if !validName(name, check) {
		return nil, refusal{Failed, nameRule}
	}
	location, declined := a.newProjectFolder(options.Location)
	if location == "" {
		return nil, declined
	}
	retirement, declined := retirementOf(ctx, source.root)
	if declined.state != "" {
		return nil, declined
	}
	view := recoveryCopyView(retained)
	review := StorageReview{Project: source.title, ProjectID: source.id, Copy: &view, Name: name, Location: location,
		Files: retirement.Files, Bytes: retirement.Bytes, Consequence: restoreCopyConsequence}
	return &boundAction{action: StorageRestoreCopyAction, origin: request,
		storage: storagePlan{source: source, document: retained.Document, digest: retained.Digest, name: name, location: location, selection: retirement.Selection},
		binding: a.storageBinding(ctx, StorageRestoreCopyAction, held, source.root, source.id, retained.Document, retained.Digest, retirement.Selection, name, location),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Storage: &review, Destination: ReviewDestination{Output: location}}}, noRefusal
}

// newProjectFolder is where a task that makes a new project writes it: the
// folder a native dialog chose, or else where new projects are created, or
// beside the backups when no projects folder is chosen.
func (a *App) newProjectFolder(chosen string) (string, refusal) {
	if strings.TrimSpace(chosen) != "" {
		return writableFolder(chosen)
	}
	location, declined := a.usableLocation()
	if location == "" {
		location, _, declined = a.usableBackupLocation()
	}
	return location, declined
}

// executeRestoreCopy copies the bound project whole into a hidden folder of
// the chosen one, verifies the copy against the source, which must still be
// the reviewed bytes, and recovers the earlier document inside the copy
// through the operation `readmit project recover` runs. The copy then gets
// the reviewed name and an identity of its own, is named and opened as a
// separate project. The project the copy was kept in is never changed, and
// nothing is sent or resumed. A restore that stops or fails part way leaves
// the hidden folder as it is: recoverable, never an accepted project.
func executeRestoreCopy(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted, Storage: &StorageOutcome{}}
	hidden := staging(plan.location, "restoring")
	fail := func(state State, reason string) ReviewedActionResult {
		result.State, result.Reason, result.Outcome = state, reason, ActionRefused
		if state == Cancelled {
			result.Outcome = ActionCancelled
		}
		if _, err := os.Lstat(hidden); err == nil {
			result.Storage.Incomplete = hidden
		}
		return result
	}
	if err := lifecycle.Copy(ctx, plan.source.root, hidden); err != nil {
		if ctx.Err() != nil {
			return fail(Cancelled, "the restore was stopped; what it wrote is kept as an incomplete restore and is not a project")
		}
		return fail(Failed, err.Error()+"; the current project is unchanged")
	}
	if err := lifecycle.VerifyCopy(ctx, plan.source.root, hidden); err != nil {
		if ctx.Err() != nil {
			return fail(Cancelled, "the restore was stopped; what it wrote is kept as an incomplete restore and is not a project")
		}
		return fail(Failed, err.Error()+"; the current project is unchanged")
	}
	if retirement, declined := retirementOf(ctx, plan.source.root); declined.state != "" || retirement.Selection != plan.selection {
		return fail(Failed, "the project changed while it was copied; the incomplete restore is kept and is not a project")
	}
	if err := project.Recover(hidden, plan.document, plan.digest); err != nil {
		return fail(Failed, err.Error()+"; the incomplete restore is kept and is not a project")
	}
	if reason := a.giveIdentity(hidden, plan.name); reason != "" {
		return fail(Failed, reason)
	}
	folder, declined := newChild(plan.location, plan.name)
	if folder == "" {
		return fail(declined.state, declined.reason)
	}
	if err := os.Rename(hidden, folder); err != nil {
		return fail(Failed, "the restored project could not be named; it is kept as an incomplete restore")
	}
	opened := a.openNamed(ctx, folder, false)
	result.Storage.Project = &opened
	if opened.State != Completed {
		result.State, result.Reason = opened.State, opened.Reason
	}
	return result
}

func bindPrepareUpdate(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	source, declined := sourceOf(request.Context)
	if source == nil {
		return nil, declined
	}
	options := storageOptions(request)
	if strings.TrimSpace(options.Candidate) == "" {
		return nil, refusal{Failed, "preparing an update names the folder its candidate was staged in"}
	}
	candidate, declined := resolveFolder(options.Candidate)
	if candidate == "" {
		return nil, declined
	}
	location, declined := a.outputFolder(options.Location)
	if location == "" {
		return nil, declined
	}
	plan, err := upgrade.Check(ctx, candidate, []string{source.root}, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, cancelledRefusal
		}
		return nil, refusal{Failed, err.Error()}
	}
	encoded, err := upgrade.Encode(plan)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	sum := sha256.Sum256(encoded)
	prepared := &PreparedCandidate{Path: candidate, Version: plan.Candidate, OS: plan.OS, Arch: plan.Arch, PlanDigest: hex.EncodeToString(sum[:])}
	retirement, declined := retirementOf(ctx, source.root)
	if declined.state != "" {
		return nil, declined
	}
	problem := ""
	switch {
	case !plan.StagedIntact():
		problem = upgrade.ErrNotStaged.Error()
	case !plan.RetainedReadable():
		problem = upgrade.ErrNotReadable.Error()
	case !retirement.Compatible:
		problem = "the project holds documents this release cannot read; no rollback copy is taken"
	}
	view := upgradeView(plan)
	review := StorageReview{Project: source.title, ProjectID: source.id, Location: location, Files: retirement.Files, Bytes: retirement.Bytes,
		Documents: retirement.Documents, Upgrade: view, Consequence: prepareConsequence}
	return &boundAction{action: StoragePrepareUpdateAction, origin: request,
		storage: storagePlan{source: source, location: location, selection: retirement.Selection, candidate: prepared},
		binding: a.storageBinding(ctx, StoragePrepareUpdateAction, held, source.root, source.id, candidate, prepared.PlanDigest, retirement.Selection, location),
		review:  ActionReview{Ready: problem == "", Refusal: problem, Storage: &review, Destination: ReviewDestination{Output: location}}}, noRefusal
}

// executePrepareUpdate takes the verified rollback copy of the bound project
// and records the exact reviewed candidate beside it. It never installs or
// runs the candidate; a failed preparation records nothing.
func executePrepareUpdate(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	plan := bound.storage
	made, declined := a.archived(ctx, plan, BackupReasonRollback, func(record *storageRecord) {
		candidate := *plan.candidate
		record.Candidate = &candidate
	})
	return storageWritten(made, declined)
}
