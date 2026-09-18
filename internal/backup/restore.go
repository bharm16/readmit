package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
)

// Verify reads one backup directory whole and returns its manifest.
//
// The completion marker is read first: it is written after everything else, so
// a backup that was interrupted at any point does not carry one and is refused
// here rather than restored as a usable copy. The marker is then checked
// against the manifest, and the manifest against every file the backup stores —
// each one present, the recorded length, and the recorded digest — so a restore
// never writes a byte that has not already been accounted for. A backup holding
// a file its manifest does not record is refused as well: an unrecorded file is
// exactly how a gap would be filled with something nothing stands behind.
func Verify(backupPath string) (Document, error) {
	root, err := artifactpath.Directory(backupPath)
	if err != nil {
		return Document{}, errors.New("a backup must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return Document{}, errors.New("cannot open the backup directory")
	}
	defer opened.Close()
	// One byte past the marker's own length, so a marker with anything
	// appended to it is refused rather than truncated into a match.
	marker, err := bounded(opened, MarkerName, digestLength+2)
	if errors.Is(err, fs.ErrNotExist) {
		return Document{}, ErrIncomplete
	}
	if err != nil {
		return Document{}, errors.New("cannot read the backup completion marker")
	}
	data, err := bounded(opened, DocumentName, maxDocumentBytes+1)
	if errors.Is(err, fs.ErrNotExist) {
		return Document{}, ErrIncomplete
	}
	if err != nil {
		return Document{}, errors.New("cannot read the backup document")
	}
	// The seal is checked before the manifest is decoded, so a backup whose
	// bytes were altered reports that rather than whichever member the
	// alteration happened to break.
	if string(marker) != identityFor(data)+"\n" {
		return Document{}, ErrDamaged
	}
	document, err := Decode(data)
	if err != nil {
		return Document{}, err
	}
	if err := held(opened, document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// held checks that the backup holds exactly the files its manifest records,
// each with the recorded length and digest.
func held(root *os.Root, document Document) error {
	listed := make(map[string]bool, len(document.Files))
	for i, file := range document.Files {
		if err := stored(root, file); err != nil {
			return errors.New(err.Error() + " at entry " + position(i))
		}
		listed[file.Path] = true
	}
	err := fs.WalkDir(root.FS(), FilesDirectory, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read the files a backup stores")
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type() != 0 {
			return errors.New("a backup stores regular files only")
		}
		if !listed[strings.TrimPrefix(name, FilesDirectory+"/")] {
			return errors.New("the backup holds a file its document does not record")
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return ErrIncomplete
	}
	return err
}

// stored reads one file the backup holds and compares it with what the manifest
// records for it.
func stored(root *os.Root, file File) error {
	opened, err := root.Open(FilesDirectory + "/" + file.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("the backup does not hold a file its document records")
	}
	if err != nil {
		return errors.New("cannot read a file the backup stores")
	}
	defer opened.Close()
	info, err := opened.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("a backup stores regular files only")
	}
	sum := sha256.New()
	read, err := io.Copy(sum, io.LimitReader(opened, MaxFileBytes+1))
	if err != nil {
		return errors.New("cannot read a file the backup stores")
	}
	if read != file.Size || hex.EncodeToString(sum.Sum(nil)) != file.SHA256 {
		return errors.New("a file the backup stores is not the file its document records")
	}
	return nil
}

// Restore writes the project a backup holds into a new directory and reports
// what came back.
//
// The backup is verified whole before the destination is created, so a damaged
// or interrupted one produces no directory at all rather than a partial project
// somebody has to judge. Every file is written at the relative path the backup
// recorded for it — a bundle identity covers relative paths and contents only,
// never absolute paths, so evidence restored beside a different machine's
// folders keeps the identity the project registered, and this reports whether
// it did.
//
// Evidence that was already missing, unreadable or changed when the backup ran
// comes back exactly that way, and is reported beside the state the backup
// recorded. Nothing is reconstructed to close a gap.
//
// Every index the backup recorded is built again from the restored canonical
// case under the declarations the index was built with. An index whose case did
// not come back verified is reported as not rebuilt; no empty or partial index
// is ever written in its place.
func Restore(ctx context.Context, backupPath, destination string, at time.Time) (Report, error) {
	root, err := artifactpath.Directory(backupPath)
	if err != nil {
		return Report{}, errors.New("a backup must be an existing directory that is not a symbolic link")
	}
	document, err := Verify(root)
	if err != nil {
		return Report{}, err
	}
	target, err := artifactpath.Destination(destination)
	if err != nil {
		return Report{}, err
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		return Report{}, errors.New("cannot open the backup directory")
	}
	defer source.Close()
	if err := os.Mkdir(target, 0700); err != nil {
		return Report{}, errors.New("cannot create the restored project; destination must be new and parent writable")
	}
	written, err := os.OpenRoot(target)
	if err != nil {
		return Report{}, errors.New("cannot open the restored project directory")
	}
	defer written.Close()
	total := int64(0)
	for i, file := range document.Files {
		if ctx.Err() != nil {
			return Report{}, errors.New("restore cancelled at file " + position(i) + "; an incomplete restore is retained")
		}
		if err := restore(ctx, source, written, file); err != nil {
			return Report{}, errors.New(err.Error() + " at entry " + position(i))
		}
		total += file.Size
	}
	report := Report{
		Root:     target,
		Files:    len(document.Files),
		Bytes:    total,
		Evidence: make([]Verification, 0, len(document.Evidence)),
		Indexes:  make([]IndexOutcome, 0, len(document.Indexes)),
	}
	found := make(map[string]State, len(document.Evidence))
	for _, entry := range document.Evidence {
		state, _ := verify(target, entry.Name, entry.Identity)
		found[entry.Name] = state
		report.Evidence = append(report.Evidence, Verification{
			Name: entry.Name, Kind: entry.Kind, Identity: entry.Identity, Recorded: entry.State, State: state,
		})
	}
	// A build the context stops writes nothing and is reported as refused with
	// everything else the builder refuses, so a cancelled restore still leaves
	// no index behind and still reads as incomplete.
	for _, entry := range document.Indexes {
		report.Indexes = append(report.Indexes, rebuild(ctx, target, entry, found, at))
	}
	if ctx.Err() != nil {
		return Report{}, errors.New("restore cancelled; an incomplete restore is retained")
	}
	return report, nil
}

// restore writes one stored file at the relative path the backup recorded and
// checks what landed against what the manifest records for it, so the bytes in
// the restored project are the bytes the manifest stands behind.
func restore(ctx context.Context, source, target *os.Root, file File) error {
	size, digest, err := copyFile(ctx, source, target, FilesDirectory+"/"+file.Path, file.Path)
	if err != nil {
		return errors.New(err.Error() + "; an incomplete restore is retained")
	}
	if size != file.Size || digest != file.SHA256 {
		return errors.New("a file the backup stores changed while it was being restored; an incomplete restore is retained")
	}
	return nil
}

// rebuild builds one index again from the canonical case that was restored. An
// index is derived and disposable, so the only thing a backup keeps of it is
// the declarations it was built under; the values come from the evidence every
// time. Where the evidence is not there, nothing is written and the reason is
// reported.
func rebuild(ctx context.Context, root string, entry Index, found map[string]State, at time.Time) IndexOutcome {
	outcome := IndexOutcome{Name: entry.Name, Case: entry.Case}
	// A recipe that could not be read, or that names evidence the project does
	// not register, was already settled when the backup recorded it.
	if state := recorded(entry.Recipe); state != IndexRecorded {
		outcome.State = state
		return outcome
	}
	if found[entry.Case] != Verified {
		outcome.State = IndexCaseUnavailable
		return outcome
	}
	policy, err := entry.Policy()
	if err != nil {
		outcome.State = IndexRefused
		return outcome
	}
	evidence, err := artifactpath.Child(root, entry.Case)
	if err != nil {
		outcome.State = IndexCaseUnavailable
		return outcome
	}
	opened, err := bundle.Open(evidence)
	if err != nil {
		outcome.State = IndexCaseUnavailable
		return outcome
	}
	document, err := index.Build(ctx, opened, policy, at)
	if err != nil {
		outcome.State = IndexRefused
		return outcome
	}
	// index.Write reserves the destination itself and returns the path it
	// actually wrote; that path is what the report names.
	built, err := index.Write(artifactpath.JoinReference(root, entry.Name), document)
	if err != nil {
		outcome.State = IndexRefused
		return outcome
	}
	outcome.State, outcome.Path = IndexRebuilt, built
	return outcome
}

// bounded reads one regular file of an artifact directory up to a limit. A file
// that is not there is reported as such, so a caller can tell an interrupted
// write from an unreadable one.
func bounded(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("artifact member must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("cannot read artifact member")
	}
	return data, nil
}
