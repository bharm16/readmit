package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/project"
)

// Storage is where a person finds backups, restores work and resolves an
// actual storage problem. Backups are kept in one folder this viewer chose,
// each in a new folder the application names; the viewer remembers that
// folder and what the application itself wrote there — which project, when
// and why — in its own shell document, because a backup directory records
// none of that and a date is never read from a file's modification time.

// storageSchema is the versioned contract of the shell document that
// remembers the folder backups are kept in and every backup, archive and
// rollback copy this viewer wrote: where it is, the identity its completion
// marker sealed, the project it was taken of and when. It holds no evidence.
const storageSchema = "readmit-desktop-storage/v1"

// MaxStorageRecords bounds the backups one viewer remembers writing; the
// oldest record is forgotten first. Forgetting a record deletes nothing.
const MaxStorageRecords = 1024

const maxStorageBytes = 1 << 20

// BackupIncomplete is a backup whose writer did not finish, or whose
// evidence it could not verify: it is listed, and never offered as a
// complete one.
const BackupIncomplete Availability = "incomplete"

// BackupReason is why the application wrote one backup.
type BackupReason string

const (
	// BackupReasonBackup is a backup a person took.
	BackupReasonBackup BackupReason = "backup"
	// BackupReasonArchive is the verified archive copy a project's source
	// may later be deleted against.
	BackupReasonArchive BackupReason = "archive"
	// BackupReasonRollback is the rollback copy taken when an update was
	// prepared.
	BackupReasonRollback BackupReason = "rollback"
)

var backupReasons = []BackupReason{BackupReasonBackup, BackupReasonArchive, BackupReasonRollback}

// PreparedCandidate is the exact staged update a rollback copy was taken
// for: the folder it was staged in, the version and platform it declares,
// and the SHA-256 of the plan the check reviewed. Nothing was installed or
// run.
type PreparedCandidate struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	PlanDigest string `json:"plan_digest"`
}

type storageDocument struct {
	Schema   string          `json:"schema"`
	Location string          `json:"location,omitzero"`
	Records  []storageRecord `json:"records"`
}

// storageRecord is one backup the application wrote. Source and Selection
// are an archive's association with the project folder it copied and the
// retirement selection it was taken under, which a later Delete source is
// bound to.
type storageRecord struct {
	Path      string             `json:"path"`
	Identity  string             `json:"identity"`
	ProjectID string             `json:"project_id,omitzero"`
	Title     string             `json:"title"`
	Reason    BackupReason       `json:"reason"`
	CreatedAt string             `json:"created_at"`
	Source    string             `json:"source,omitzero"`
	Selection string             `json:"selection,omitzero"`
	Candidate *PreparedCandidate `json:"candidate,omitzero"`
}

func (a *App) readStorage() (storageDocument, error) {
	data, err := a.documents.read(storageName, maxStorageBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return storageDocument{Schema: storageSchema, Records: []storageRecord{}}, nil
	}
	if err != nil {
		return storageDocument{}, err
	}
	var document storageDocument
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil || document.Schema != storageSchema {
		return storageDocument{}, errNotADocument
	}
	if document.Location != "" && !filepath.IsAbs(document.Location) || len(document.Records) > MaxStorageRecords {
		return storageDocument{}, errNotADocument
	}
	for _, record := range document.Records {
		if !validRecord(record) {
			return storageDocument{}, errNotADocument
		}
	}
	if document.Records == nil {
		document.Records = []storageRecord{}
	}
	return document, nil
}

func validRecord(record storageRecord) bool {
	absolute := func(path string) bool { return filepath.IsAbs(path) && printable(path, maxRootBytes) }
	stamp := func(value string) bool { _, err := time.Parse(time.RFC3339, value); return err == nil }
	switch {
	case !absolute(record.Path), !digestText(record.Identity), !catalog.ValidName(record.Title),
		!slices.Contains(backupReasons, record.Reason), !stamp(record.CreatedAt),
		record.ProjectID != "" && !catalog.ValidID(record.ProjectID),
		record.Source != "" && !absolute(record.Source),
		record.Selection != "" && !digestText(record.Selection):
		return false
	}
	if candidate := record.Candidate; candidate != nil {
		return absolute(candidate.Path) && printable(candidate.Version, 128) && printable(candidate.OS, 32) &&
			printable(candidate.Arch, 32) && digestText(candidate.PlanDigest)
	}
	return true
}

func digestText(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// changeStorage reads the storage document, applies change, and writes it
// whole. A document this release cannot read is left as it is and reported.
func (a *App) changeStorage(change func(*storageDocument)) error {
	a.storageMu.Lock()
	defer a.storageMu.Unlock()
	document, err := a.readStorage()
	if err != nil {
		return err
	}
	change(&document)
	if len(document.Records) > MaxStorageRecords {
		document.Records = document.Records[:MaxStorageRecords]
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return err
	}
	return a.documents.write(storageName, append(data, '\n'))
}

// recordBackup remembers one backup the application wrote, newest first. A
// record at the same folder is replaced: a folder holds one backup.
func (a *App) recordBackup(record storageRecord) error {
	return a.changeStorage(func(document *storageDocument) {
		document.Records = slices.DeleteFunc(document.Records, func(known storageRecord) bool { return known.Path == record.Path })
		document.Records = append([]storageRecord{record}, document.Records...)
	})
}

// forgetBackup drops the record of a backup that was deleted.
func (a *App) forgetBackup(path string) {
	a.changeStorage(func(document *storageDocument) {
		document.Records = slices.DeleteFunc(document.Records, func(known storageRecord) bool { return known.Path == path })
	})
}

// unreadableStorage refuses a remembered storage document this release
// cannot read.
var unreadableStorage = refusal{Failed, "the remembered backup folder cannot be read; it is left exactly as written"}

// noBackupLocation asks for the folder backups are kept in. It is Empty, not
// Failed: nothing went wrong, and choosing the folder is the next step.
var noBackupLocation = refusal{Empty, "no folder is chosen to keep backups in; choose one first"}

// usableBackupLocation is the remembered backup folder, resolved, while it
// is still an existing folder, not a link, that this account can write in.
// A folder that was never chosen or is gone is Empty, so the window asks for
// one; the remembered value is left as it is.
func (a *App) usableBackupLocation() (string, string, refusal) {
	document, err := a.readStorage()
	if err != nil {
		return "", "", unreadableStorage
	}
	if document.Location == "" {
		return "", "", noBackupLocation
	}
	if info, err := os.Lstat(document.Location); err != nil || !info.IsDir() {
		return "", document.Location, refusal{Empty, "the folder backups are kept in is no longer there; choose one again"}
	}
	resolved, err := artifactpath.Resolve(document.Location)
	if err != nil {
		return "", document.Location, refusal{Empty, "the folder backups are kept in is no longer there; choose one again"}
	}
	probe, err := os.MkdirTemp(resolved, ".readmit-access-")
	if err != nil {
		return "", document.Location, refusal{PermissionDenied, "this account cannot create a folder in the folder backups are kept in; choose another"}
	}
	os.Remove(probe)
	return resolved, document.Location, refusal{}
}

// BackupLocation reports the remembered folder backups are kept in while a
// backup can be written there. Otherwise it is Empty with the reason, and
// the remembered value is left as it is.
func (a *App) BackupLocation() ProjectLocationResult {
	resolved, remembered, declined := a.usableBackupLocation()
	if resolved == "" {
		return ProjectLocationResult{State: declined.state, Reason: declined.reason}
	}
	return ProjectLocationResult{State: Completed, Location: remembered}
}

// ChooseBackupLocation asks the host for the folder backups, archive copies
// and rollback copies are kept in, and remembers it. Nothing is created or
// moved; backups kept in an earlier folder stay there and stay listed.
func (a *App) ChooseBackupLocation() ProjectLocationResult {
	return run(a, true, false, func(ctx context.Context) ProjectLocationResult {
		folder, declined := a.chooseFolder(ctx, "Choose where backups are kept")
		if folder == "" {
			return ProjectLocationResult{State: declined.state, Reason: declined.reason}
		}
		if err := a.changeStorage(func(document *storageDocument) { document.Location = folder }); err != nil {
			return ProjectLocationResult{State: Failed, Reason: "the backup folder could not be remembered"}
		}
		return ProjectLocationResult{State: Completed, Location: folder}
	})
}

// StorageBackup is one backup as Storage lists it. ID is opaque and stable
// while the backup stays where it is. CreatedAt is when the application
// wrote it, and null for a backup it did not record writing: a date is never
// read from a file. Project is the title recorded when it was written, or,
// for a backup the application did not record, the one the backup stores.
// Size is what its manifest records. Availability is available, or the
// actual problem: missing, unreadable, unsupported or incomplete, with
// Problem saying what was found.
type StorageBackup struct {
	ID           string             `json:"id"`
	Project      string             `json:"project"`
	ProjectID    string             `json:"project_id,omitzero"`
	CreatedAt    *string            `json:"created_at"`
	Size         int64              `json:"size"`
	Reason       BackupReason       `json:"reason,omitzero"`
	Availability Availability       `json:"availability"`
	Problem      string             `json:"problem,omitzero"`
	Folder       string             `json:"folder"`
	Candidate    *PreparedCandidate `json:"candidate,omitzero"`
}

// StorageBackupsResult lists every backup in the backup folder and every
// one recorded elsewhere, newest first, and the folder backups are kept in.
type StorageBackupsResult struct {
	State    State           `json:"state"`
	Reason   string          `json:"reason,omitzero"`
	Location string          `json:"location,omitzero"`
	Backups  []StorageBackup `json:"backups"`
}

func (r *StorageBackupsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// StorageBackupRequest names the project to back up: the one open, or the
// one a person picked by name.
type StorageBackupRequest struct {
	Context RequestContext `json:"context"`
}

// StorageBackupResult carries one backup.
type StorageBackupResult struct {
	State  State          `json:"state"`
	Reason string         `json:"reason,omitzero"`
	Backup *StorageBackup `json:"backup,omitzero"`
}

func (r *StorageBackupResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// locatedBackup is one listed backup and what reading it found.
type locatedBackup struct {
	row      StorageBackup
	path     string
	identity string
	document backup.Document
	record   *storageRecord
}

// available reports whether a backup can be restored or verified now.
func (b *locatedBackup) available() bool { return b.row.Availability == ItemAvailable }

// backupID is a backup's opaque identity: where it is and the identity its
// marker sealed. Backing up an unchanged project twice seals the same
// manifest, so the identity alone does not tell two backups apart.
func backupID(path, identity string) string {
	sum := sha256.Sum256([]byte("readmit-backup\x00" + path + "\x00" + identity))
	return hex.EncodeToString(sum[:])
}

// inspectBackup reads one backup as a row, cheaply: its marker, its seal and
// its manifest, never every stored byte.
func inspectBackup(path string, record *storageRecord) locatedBackup {
	found := locatedBackup{path: path, record: record, row: StorageBackup{Folder: path, Availability: ItemAvailable}}
	if record != nil {
		found.row.Project, found.row.ProjectID, found.row.Reason = record.Title, record.ProjectID, record.Reason
		found.row.CreatedAt, found.row.Candidate = stamped(record.CreatedAt), record.Candidate
	}
	problem := func(availability Availability, reason string) locatedBackup {
		found.row.Availability, found.row.Problem = availability, reason
		if record != nil {
			found.row.ID = backupID(path, record.Identity)
		} else {
			found.row.ID = backupID(path, found.identity)
		}
		return found
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return problem(ItemMissing, "the backup is no longer where it was written")
	case err != nil:
		return problem(ItemUnreadable, "the backup's folder cannot be read")
	case info.Mode()&fs.ModeSymlink != 0:
		return problem(ItemUnreadable, "the backup's folder is a symbolic link; it is not followed")
	case !info.IsDir():
		return problem(ItemUnreadable, "the backup's folder is not a folder")
	}
	document, identity, err := backup.Inspect(path)
	found.identity, found.document = identity, document
	switch {
	case errors.Is(err, backup.ErrIncomplete):
		return problem(BackupIncomplete, "the backup was not finished")
	case errors.Is(err, backup.ErrUnsupportedVersion):
		return problem(ItemUnsupported, "the backup was written by a newer release")
	case errors.Is(err, backup.ErrDamaged):
		return problem(ItemUnreadable, "the backup's contents do not match the backup that was written")
	case err != nil:
		return problem(ItemUnreadable, "the backup cannot be read: "+err.Error())
	}
	for _, file := range document.Files {
		found.row.Size += file.Size
	}
	if record == nil {
		found.row.Project, found.row.ProjectID = storedProject(path)
	}
	switch {
	case record != nil && record.Identity != identity:
		return problem(ItemUnreadable, "the folder holds a different backup than the one written there")
	case !document.Complete():
		return problem(BackupIncomplete, "the backup holds evidence it could not verify; nothing was put in its place")
	}
	found.row.ID = backupID(path, identity)
	return found
}

// storedProject is the title and catalog identity of the project a backup
// stores, read from its stored documents, or empty when they cannot be read.
func storedProject(path string) (string, string) {
	stored := filepath.Join(path, backup.FilesDirectory)
	opened, err := project.Open(stored)
	if err != nil {
		return "", ""
	}
	title := opened.Document.Settings.Title
	store, err := catalog.Open(opened.Root)
	if err != nil {
		return title, ""
	}
	if document, present, err := store.Read(); err == nil && present {
		return title, document.Project.ID
	}
	return title, ""
}

// looksLikeBackup reports whether a folder holds any member of a backup, so
// an unrelated folder beside the backups is not listed as a damaged one.
func looksLikeBackup(path string) bool {
	for _, member := range []string{backup.DocumentName, backup.MarkerName, backup.FilesDirectory} {
		if _, err := os.Lstat(filepath.Join(path, member)); err == nil {
			return true
		}
	}
	return false
}

// backups reads every backup Storage lists: each one recorded, wherever it
// is, each backup in the backup folder, and each one this process was shown
// through the folder dialog.
func (a *App) backups() ([]locatedBackup, string, refusal) {
	document, err := a.readStorage()
	if err != nil {
		return nil, "", unreadableStorage
	}
	seen := map[string]bool{}
	var found []locatedBackup
	for i := range document.Records {
		record := document.Records[i]
		if seen[record.Path] {
			continue
		}
		seen[record.Path] = true
		found = append(found, inspectBackup(record.Path, &record))
	}
	resolved, remembered, declined := a.usableBackupLocation()
	if resolved == "" && declined.state == PermissionDenied {
		resolved, _ = artifactpath.Resolve(remembered)
	}
	if resolved != "" {
		entries, _ := os.ReadDir(resolved)
		for _, entry := range entries {
			path := filepath.Join(resolved, entry.Name())
			if seen[path] || entry.Name()[0] == '.' {
				continue
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				if info, err := os.Stat(path); err != nil || !info.IsDir() {
					continue
				}
			} else if !entry.IsDir() {
				continue
			}
			if looksLikeBackup(path) {
				seen[path] = true
				found = append(found, inspectBackup(path, nil))
			}
		}
	}
	a.storageMu.Lock()
	chosen := slices.Collect(func(yield func(string) bool) {
		for _, path := range a.chosenBackups {
			if !yield(path) {
				return
			}
		}
	})
	a.storageMu.Unlock()
	slices.Sort(chosen)
	for _, path := range chosen {
		if !seen[path] {
			seen[path] = true
			found = append(found, inspectBackup(path, nil))
		}
	}
	slices.SortFunc(found, func(x, y locatedBackup) int {
		switch {
		case x.row.CreatedAt != nil && y.row.CreatedAt == nil:
			return -1
		case x.row.CreatedAt == nil && y.row.CreatedAt != nil:
			return 1
		case x.row.CreatedAt != nil:
			if order := cmp.Compare(*y.row.CreatedAt, *x.row.CreatedAt); order != 0 {
				return order
			}
		}
		return cmp.Compare(x.row.ID, y.row.ID)
	})
	return found, remembered, declined
}

// findBackup is the listed backup with id, read again now.
func (a *App) findBackup(id string) (*locatedBackup, refusal) {
	listed, _, declined := a.backups()
	if listed == nil && declined == unreadableStorage {
		return nil, declined
	}
	for i := range listed {
		if listed[i].row.ID == id {
			return &listed[i], refusal{}
		}
	}
	return nil, refusal{Failed, "no such backup is listed; list the backups again"}
}

// ListBackups lists every backup: those in the folder backups are kept in,
// those the application recorded writing elsewhere, and any this window was
// shown through the folder dialog. A backup that is missing, damaged,
// unfinished or reached through a link stays a row, with the actual reason.
// Reading a backup here is cheap — its marker, its seal and its manifest —
// and writes nothing.
func (a *App) ListBackups() StorageBackupsResult {
	return run(a, false, false, func(context.Context) StorageBackupsResult {
		listed, remembered, declined := a.backups()
		if declined == unreadableStorage {
			return StorageBackupsResult{State: Failed, Reason: declined.reason, Backups: []StorageBackup{}}
		}
		result := StorageBackupsResult{State: Completed, Location: remembered, Backups: make([]StorageBackup, 0, len(listed))}
		for _, found := range listed {
			result.Backups = append(result.Backups, found.row)
		}
		if len(result.Backups) == 0 {
			result.State, result.Reason = Empty, cmp.Or(declined.reason, "no backups")
		}
		return result
	})
}

// InspectBackup is the explicit recheck of one listed backup: it reads the
// backup whole, every stored byte against its manifest, and reports what it
// holds. It writes nothing.
func (a *App) InspectBackup(id string) BackupResult {
	return run(a, false, false, func(context.Context) BackupResult {
		found, declined := a.findBackup(id)
		if found == nil {
			return BackupResult{State: declined.state, Reason: declined.reason}
		}
		if !found.available() {
			return BackupResult{State: Failed, Reason: found.row.Problem}
		}
		document, err := backup.Verify(found.path)
		if err != nil {
			return classifyBackupErr(err)
		}
		view := viewFromDocument(found.path, document)
		if !document.Complete() {
			return BackupResult{State: Failed, Reason: "this backup holds evidence it could not verify; nothing was put in its place", Report: &view}
		}
		return BackupResult{State: Completed, Report: &view}
	})
}

// ChooseBackup asks the host for a backup folder kept anywhere, reads it as
// Storage lists a backup, and lists it for the rest of this process, so it
// can be verified or restored like any other. It records nothing.
func (a *App) ChooseBackup() StorageBackupResult {
	return run(a, true, false, func(ctx context.Context) StorageBackupResult {
		folder, declined := a.chooseFolder(ctx, "Open backup")
		if folder == "" {
			return StorageBackupResult{State: declined.state, Reason: declined.reason}
		}
		path, err := artifactpath.Resolve(folder)
		if err != nil {
			return StorageBackupResult{State: Failed, Reason: "the chosen folder cannot be read"}
		}
		found := inspectBackup(path, nil)
		a.storageMu.Lock()
		if a.chosenBackups == nil {
			a.chosenBackups = map[string]string{}
		}
		a.chosenBackups[found.row.ID] = path
		a.storageMu.Unlock()
		// A folder listed or recorded already is answered as that row.
		if listed, _ := a.findBackup(found.row.ID); listed != nil {
			found = *listed
		}
		state := Completed
		if !found.available() {
			state = Failed
		}
		return StorageBackupResult{State: state, Reason: found.row.Problem, Backup: &found.row}
	})
}

// RevealBackup shows one listed backup's folder in the host's file manager.
func (a *App) RevealBackup(id string) RevealResult {
	return run(a, false, false, func(context.Context) RevealResult {
		found, declined := a.findBackup(id)
		if found == nil {
			return RevealResult{State: declined.state, Reason: declined.reason}
		}
		if found.row.Availability == ItemMissing {
			return RevealResult{State: Failed, Reason: found.row.Problem}
		}
		if err := a.revealer()(found.path); err != nil {
			return RevealResult{State: Failed, Reason: "the file manager could not be opened"}
		}
		return RevealResult{State: Completed}
	})
}

// storageSource is the project a storage task is about: its folder, its
// title and the identity its catalog records, when it records one.
type storageSource struct {
	root  string
	title string
	id    string
}

// sourceOf reads the project a request names, refusing one whose catalog
// records a different identity than the one the window showed.
func sourceOf(request RequestContext) (*storageSource, refusal) {
	opened, declined := openProjectFolder(request.Project)
	if opened == nil {
		return nil, declined
	}
	source := &storageSource{root: opened.Root, title: opened.Document.Settings.Title}
	if store, err := catalog.Open(opened.Root); err == nil {
		if document, present, err := store.Read(); err == nil && present {
			source.id = document.Project.ID
		}
	}
	if request.ProjectID != "" && request.ProjectID != source.id {
		return nil, refusal{Failed, errForeignProject.Error()}
	}
	return source, refusal{}
}

// newChild names a new folder in parent for what is written there,
// refusing with a sentence about that folder rather than the projects one.
func newChild(parent, name string) (string, refusal) {
	folder, err := freeFolder(parent, name)
	if err != nil {
		return "", refusal{Failed, "no new folder can be named in the chosen folder"}
	}
	return folder, refusal{}
}

// BackupProject copies one project into a new folder the application names
// in the folder backups are kept in, then reads the backup back whole. Only a
// backup that verified whole is Completed; one that holds evidence it could
// not verify is written and recorded, and reported as incomplete. An existing
// backup is never overwritten. A backup that was stopped or failed keeps
// what it wrote, carries no completion marker, is not recorded, and lists as
// incomplete. With no backup folder chosen it is Empty and writes nothing,
// so the window asks for one.
//
// Preserving evidence that already exists is not new work: this and every
// storage task are admitted the way their `readmit backup`, `readmit
// project` and `readmit upgrade` commands are — without a term — so an
// expired license never gates them (ADR-0007, ADR-0010).
func (a *App) BackupProject(request StorageBackupRequest) StorageBackupResult {
	return run(a, true, false, func(ctx context.Context) StorageBackupResult {
		return a.backupProject(ctx, request)
	})
}

func (a *App) backupProject(ctx context.Context, request StorageBackupRequest) StorageBackupResult {
	source, declined := sourceOf(request.Context)
	if source == nil {
		return StorageBackupResult{State: declined.state, Reason: declined.reason}
	}
	location, _, declined := a.usableBackupLocation()
	if location == "" {
		return StorageBackupResult{State: declined.state, Reason: declined.reason}
	}
	made, declined := a.writeBackup(ctx, source, location, BackupReasonBackup, func(folder string) (backup.Report, error) {
		return backup.Create(ctx, source.root, folder)
	}, func(*storageRecord) {})
	if made == nil {
		return StorageBackupResult{State: declined.state, Reason: declined.reason}
	}
	if !made.available() {
		return StorageBackupResult{State: Failed, Reason: made.row.Problem, Backup: &made.row}
	}
	return StorageBackupResult{State: Completed, Backup: &made.row}
}

// writeBackup takes one backup of source into a new folder of parent through
// take, verifies it whole and records it under reason; annotate adds what the
// reason records beside it. A backup that did not complete is not recorded.
func (a *App) writeBackup(ctx context.Context, source *storageSource, parent string, reason BackupReason,
	take func(folder string) (backup.Report, error), annotate func(*storageRecord)) (*locatedBackup, refusal) {
	folder, declined := newChild(parent, source.title+" "+string(reason))
	if folder == "" {
		return nil, declined
	}
	report, err := take(folder)
	if err != nil {
		if ctx.Err() != nil {
			return nil, refusal{Cancelled, "the backup was stopped; what it wrote is kept as an incomplete backup"}
		}
		failed := classifyBackupErr(err)
		return nil, refusal{failed.State, failed.Reason}
	}
	if _, err := backup.Verify(report.Root); err != nil {
		return nil, refusal{Failed, "the backup was written but does not read back whole: " + err.Error()}
	}
	_, identity, err := backup.Inspect(report.Root)
	if err != nil {
		return nil, refusal{Failed, "the backup was written but does not read back whole: " + err.Error()}
	}
	record := storageRecord{Path: report.Root, Identity: identity, ProjectID: source.id, Title: source.title,
		Reason: reason, CreatedAt: catalog.Stamp(a.now())}
	annotate(&record)
	if err := a.recordBackup(record); err != nil {
		return nil, refusal{Failed, "the backup was written and verified, but this viewer could not record it; it is listed from the backup folder"}
	}
	found := inspectBackup(report.Root, &record)
	return &found, refusal{}
}
