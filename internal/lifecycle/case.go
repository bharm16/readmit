package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/project"
)

// One case of a project is archived and retired the way a whole project is:
// the archive is a verified backup, of a project that registers only that
// case, so restoring it is an ordinary restore into a new project; and the
// case is deleted only against that archive while its folder is still
// exactly the bytes the archive was taken of.

// CaseRemainderPrefix names the folder a case retirement renames the case to
// inside its project before removing it: a removal that stops part way
// leaves it there, and a later retirement of the case refuses to run over it.
const CaseRemainderPrefix = ".readmit-retiring-"

// CaseRemainder is where a retirement of the case at entry leaves what it
// could not remove.
func CaseRemainder(root, entry string) string {
	return filepath.Join(root, CaseRemainderPrefix+entry)
}

// caseInventory fingerprints every file of one case folder under the backup
// file bounds, named by its path relative to the project, which is where the
// case's archive stores it.
func caseInventory(ctx context.Context, root, entry string) (map[string]fingerprint, error) {
	if _, err := artifactpath.Child(root, entry); err != nil {
		return nil, errors.New("the case is not one folder of the project")
	}
	project, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("cannot inspect the project directory")
	}
	defer project.Close()
	files := map[string]fingerprint{}
	var total int64
	err = fs.WalkDir(project.FS(), entry, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cannot enumerate the case")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type() != 0 {
			return errors.New("the case holds an entry that is a symbolic link or a device")
		}
		held, err := fingerprintOf(project, name)
		if err != nil {
			return err
		}
		if len(files) >= backup.MaxFiles || held.Size > backup.MaxFileBytes || total > backup.MaxBytes-held.Size {
			return errors.New("the case exceeds backup limits")
		}
		total += held.Size
		files[name] = held
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// PreviewCaseRetirement inventories what archiving or deleting the case at
// entry of the project would affect and derives the selection token that
// binds a later ArchiveCase or RetireCase to exactly those bytes. It changes
// nothing.
func PreviewCaseRetirement(ctx context.Context, path, entry string) (Retirement, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return Retirement{}, err
	}
	files, err := caseInventory(ctx, root, entry)
	if err != nil {
		return Retirement{}, err
	}
	var total int64
	for _, held := range files {
		total += held.Size
	}
	return Retirement{Selection: selectionOf(files), Compatible: true, Files: len(files), Bytes: total, Documents: []Compatibility{}}, nil
}

// ArchiveCase writes a new verified backup at destination holding the case
// at entry of the project as a project of its own: the project document with
// that case as its only registration, titled title, and the case's files at
// the paths they have in the project. The case is archived only while it is
// still the selection a PreviewCaseRetirement inventoried; the archive must
// verify whole and hold every file of the case, and the case must not change
// while it is taken. A staging copy the archive is taken from is removed
// whatever happens; a cancelled or failed archive leaves its incomplete
// backup, which no reader accepts. The source is never changed.
func ArchiveCase(ctx context.Context, path, entry, title, destination, selection string) (backup.Report, error) {
	var report backup.Report
	root, err := artifactpath.Directory(path)
	if err != nil {
		return report, err
	}
	opened, err := project.Open(root)
	if err != nil {
		return report, err
	}
	at := slices.IndexFunc(opened.Document.Cases, func(registered project.Case) bool { return registered.Name == entry })
	if at < 0 {
		return report, errors.New("the project does not register that case")
	}
	before, err := caseInventory(ctx, root, entry)
	if err != nil {
		return report, err
	}
	if selectionOf(before) != selection {
		return report, errors.New("the case changed since the retirement preview; nothing was archived")
	}
	document := opened.Document
	document.Cases = []project.Case{opened.Document.Cases[at]}
	document.Settings.Title = title
	if err := project.Validate(document); err != nil {
		return report, errors.New("the case cannot be archived as a project of its own: " + err.Error())
	}
	raw := make([]byte, 8)
	rand.Read(raw)
	staged := filepath.Join(filepath.Dir(destination), ".readmit-case-"+hex.EncodeToString(raw))
	defer os.RemoveAll(staged)
	if err := stageCase(ctx, root, entry, staged, document); err != nil {
		return report, err
	}
	report, err = backup.Create(ctx, staged, destination)
	if err != nil {
		return report, err
	}
	if !report.Complete() {
		return report, errors.New("archive is incomplete; the case is retained")
	}
	held, err := backup.Verify(report.Root)
	if err != nil {
		return report, err
	}
	if !held.Complete() {
		return report, errors.New("archive is incomplete; the case is retained")
	}
	after, err := caseInventory(ctx, root, entry)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(before, after) {
		return report, errors.New("the case changed during the archive; the case is retained")
	}
	return report, caseAccounts(before, held)
}

// stageCase writes the one-case project an archive of a case is taken from.
func stageCase(ctx context.Context, root, entry, staged string, document project.Document) error {
	if err := os.Mkdir(staged, 0o700); err != nil {
		return errors.New("cannot create the staging copy of the case beside the archive")
	}
	if err := project.WriteDocument(staged, document); err != nil {
		return err
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("cannot open the project directory")
	}
	defer source.Close()
	target, err := os.OpenRoot(staged)
	if err != nil {
		return errors.New("cannot open the staging copy of the case")
	}
	defer target.Close()
	files, err := caseInventory(ctx, root, entry)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := copyOne(source, target, name); err != nil {
			return err
		}
	}
	return nil
}

// caseAccounts checks that a verified case archive holds every file of the
// case an inventory fingerprinted, byte for byte, and nothing else but the
// project document it was staged under; and that it verified the case as
// evidence.
func caseAccounts(before map[string]fingerprint, held backup.Document) error {
	stored := 0
	for _, file := range held.Files {
		switch want, ok := before[file.Path]; {
		case ok && want == (fingerprint{file.Size, file.SHA256}):
			stored++
		case ok, file.Path != project.DocumentName:
			return errors.New("archive does not match the case; the case is retained")
		}
	}
	if stored != len(before) || len(held.Evidence) != 1 || held.Evidence[0].State != backup.Verified {
		return errors.New("archive does not account for the case; the case is retained")
	}
	return nil
}

// RetireCase deletes the case at entry of the project against an archive
// ArchiveCase already took, never one of its own. The case is deleted only
// while it is still exactly the selection the archive was taken under and
// the archive verifies whole and holds every file of it. Its folder is then
// renamed to its remainder and unregister takes the case off the project — a
// failure there renames it back, with nothing deleted — and the remainder is
// removed. A removal that stops part way leaves the remainder and says so; a
// remainder an earlier removal left is reported and never removed again.
//
// Deleting is unlinking; it is not secure erasure and revokes no copy.
func RetireCase(ctx context.Context, path, entry, archive, selection string, unregister func() error) (Outcome, error) {
	retained := Outcome{State: Retained}
	if err := ctx.Err(); err != nil {
		return retained, err
	}
	root, err := artifactpath.Directory(path)
	if err != nil {
		return retained, err
	}
	remainder := CaseRemainder(root, entry)
	if exists(remainder) {
		return Outcome{State: RemovalIncomplete, Remainder: remainder},
			errors.New("an earlier deletion of this case left a remainder in the project; it is not deleted again and nothing was archived again")
	}
	if selection == "" {
		return retained, errors.New("a deletion requires the selection its archive was taken under")
	}
	before, err := caseInventory(ctx, root, entry)
	if err != nil {
		return retained, err
	}
	if selectionOf(before) != selection {
		return retained, errors.New("the case changed since it was archived; nothing was deleted")
	}
	held, err := backup.Verify(archive)
	if err != nil {
		return retained, errors.New("the archive cannot be verified; the case is retained: " + err.Error())
	}
	if !held.Complete() {
		return retained, errors.New("archive is incomplete; the case is retained")
	}
	if err := caseAccounts(before, held); err != nil {
		return retained, err
	}
	if err := ctx.Err(); err != nil {
		return retained, err
	}
	folder := filepath.Join(root, entry)
	if err := os.Rename(folder, remainder); err != nil {
		return retained, errors.New("cannot retire the case; the case is retained")
	}
	if err := unregister(); err != nil {
		if os.Rename(remainder, folder) != nil {
			return Outcome{State: RemovalIncomplete, Remainder: remainder}, errors.New("the case could not be taken off the project, and its folder could not be put back; it is kept at its remainder")
		}
		return retained, err
	}
	if err := os.RemoveAll(remainder); err != nil {
		return Outcome{State: RemovalIncomplete, Remainder: remainder}, errors.New("case deletion incomplete; the remainder is retained in the project and the archive is retained")
	}
	return Outcome{State: Deleted}, nil
}
