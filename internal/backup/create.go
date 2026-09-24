package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
)

// Verification is one registered artifact of a report. State is what opening the
// artifact found; Recorded is the state a backup had already written down for
// it, and a backup being created has none, because the state it found is the
// state it is recording. A restore reports both, so evidence that was already
// missing when the backup ran reads as that rather than as something the
// restore lost.
type Verification struct {
	Name     string
	Kind     Kind
	Identity string
	Recorded State
	State    State
}

// IndexState is what one operation did about one derived index. A backup
// records the declarations an index was built under; a restore builds it again
// from them. Only `recorded` and `rebuilt` are a pass.
type IndexState string

const (
	// IndexRecorded is an index whose declarations a backup recorded, so a
	// restore can build it again from the canonical case. A backup never
	// copies an index, so this is the most a backup can report.
	IndexRecorded IndexState = "recorded"
	// IndexRebuilt is an index a restore built again from the restored case.
	IndexRebuilt IndexState = "rebuilt"
	// IndexCaseUnavailable is an index whose case did not come back verified,
	// so there is no evidence to build it from. Nothing is written.
	IndexCaseUnavailable IndexState = "case-unavailable"
	// IndexUndeclared is an index whose declarations could not be read.
	IndexUndeclared IndexState = "undeclared"
	// IndexUnregistered is an index describing evidence the project does not
	// register.
	IndexUnregistered IndexState = "case-unregistered"
	// IndexRefused is an index the builder or the path policy refused. It is
	// reported; no partial index is left behind.
	IndexRefused IndexState = "refused"
)

// Complete reports whether an index came through an operation intact: a backup
// recorded declarations a restore can build it from, or a restore built it.
// Nothing else is a pass, and this is the only place that says so.
func (s IndexState) Complete() bool { return s == IndexRecorded || s == IndexRebuilt }

// IndexOutcome is one index of a report. Path is the exact path the index
// writer returned and is empty unless one was built.
type IndexOutcome struct {
	Name  string
	Case  string
	State IndexState
	Path  string
}

// Report is the account of one backup or restore: the artifact it wrote, how
// much it stored, and for every registered case, revision and index what
// verifying it found. Nothing in it is inferred from a name.
type Report struct {
	Root     string
	Files    int
	Bytes    int64
	Evidence []Verification
	Indexes  []IndexOutcome
}

// Complete reports whether every registered artifact verified and every index
// came through intact. A report that is not complete names what it could not
// account for; it never becomes complete by leaving that part out.
func (r Report) Complete() bool {
	for _, entry := range r.Evidence {
		if entry.State != Verified {
			return false
		}
	}
	for _, entry := range r.Indexes {
		if !entry.State.Complete() {
			return false
		}
	}
	return true
}

// Create copies one project directory into a new backup directory and reports
// what it found there.
//
// The project's own documents are read first: a backup records what a project
// registers, so one whose documents cannot be read is refused before anything
// is copied rather than stored as an unexamined pile of files.
//
// What the manifest records is then read back from the copy, not from the
// source it was taken from. Every registered case and revision is opened from
// the backup's own stored files, through the same reader `timeline` uses, and
// its identity compared with the one the stored project document recorded. A
// backup therefore says what it holds rather than what it saw, and a project
// that changed while it was being copied cannot leave a manifest vouching for
// bytes the backup does not contain. Evidence that is missing, unreadable or
// changed is recorded as such and never reconstructed.
//
// A cancelled or failed write leaves the incomplete directory in place without
// a completion marker, so it is detected as interrupted and refused by every
// reader rather than mistaken for a backup.
func Create(ctx context.Context, projectPath, destination string) (Report, error) {
	root, err := artifactpath.Directory(projectPath)
	if err != nil {
		return Report{}, errors.New("a project must be an existing directory that is not a symbolic link")
	}
	// Both documents are read here only so that a project nobody can read
	// costs nobody a copy. What the manifest records is read again below, from
	// the files the backup actually stored.
	if _, err := project.Open(root); err != nil {
		return Report{}, err
	}
	if _, err := project.ReadRevisions(root); err != nil {
		return Report{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Report{}, errors.New("cannot inspect the project directory")
	}
	// The destination is reserved before anything is read, and the project is
	// passed as a protected source so a backup can never be written inside the
	// directory it is copying.
	target, err := artifactpath.Destination(destination, info)
	if err != nil {
		return Report{}, err
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		return Report{}, errors.New("cannot open the project directory")
	}
	defer source.Close()
	names, candidates, err := scan(source)
	if err != nil {
		return Report{}, err
	}
	held, err := artifactdir.Create(target, family, artifactdir.Durable)
	if err != nil {
		return Report{}, err
	}
	defer held.Close()
	if held.Mkdir(FilesDirectory) != nil {
		return Report{}, errors.New("cannot create backup file directory; an incomplete backup is retained")
	}
	files, copied, err := copyAll(ctx, source, held, names)
	if err != nil {
		return Report{}, err
	}
	evidence, registered, err := inspectCopy(artifactpath.JoinReference(target, FilesDirectory))
	if err != nil {
		return Report{}, err
	}
	document := Document{Schema: Schema, Files: files, Evidence: evidence, Indexes: recipesFor(candidates, registered)}
	if ctx.Err() != nil {
		return Report{}, errors.New("backup cancelled; an incomplete backup is retained")
	}
	if err := seal(held, document); err != nil {
		return Report{}, err
	}
	report := Report{Root: target, Files: len(files), Bytes: copied, Indexes: make([]IndexOutcome, 0, len(document.Indexes))}
	for _, entry := range evidence {
		report.Evidence = append(report.Evidence, Verification{Name: entry.Name, Kind: entry.Kind, Identity: entry.Identity, State: entry.State})
	}
	for _, entry := range document.Indexes {
		report.Indexes = append(report.Indexes, IndexOutcome{Name: entry.Name, Case: entry.Case, State: recorded(entry.Recipe)})
	}
	return report, nil
}

// recorded reports what a backup did about one index. A backup records what an
// index declared and rebuilds nothing, so a recipe it read is `recorded`; one
// it could not read, or that names evidence the project does not register, is
// reported at the moment it is found rather than only when a restore runs.
func recorded(recipe Recipe) IndexState {
	switch recipe {
	case Declared:
		return IndexRecorded
	case Unregistered:
		return IndexUnregistered
	default:
		return IndexUndeclared
	}
}

// candidate is one top-level file of the project that declares the index
// contract. A backup records what an index was built under and never copies it,
// so the retained patient data an index holds exists in exactly one place.
type candidate struct {
	name     string
	document index.Document
	readable bool
}

// scan lists the regular files a backup stores and the indexes it records
// instead. An entry that is not a regular file — a symbolic link, a device, a
// socket — is refused rather than followed or silently skipped: following one
// would copy bytes from outside the project, and skipping it would produce a
// backup that is quietly missing part of what it was pointed at.
func scan(source *os.Root) ([]string, []candidate, error) {
	var names []string
	var candidates []candidate
	total := int64(0)
	err := fs.WalkDir(source.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read the project directory")
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type() != 0 {
			return errors.New("a backup stores regular files only; the project holds an entry that is a symbolic link or a device")
		}
		if err := storedPath(name); err != nil {
			return errors.New("the project holds an entry a backup cannot name: " + err.Error())
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("cannot inspect a file of the project")
		}
		if len(names)+len(candidates) >= MaxFiles {
			return errors.New("the project holds more files than one backup stores")
		}
		// An index sits beside the project, never inside a case bundle, so
		// only a top-level file is ever considered one. Probe up to the
		// backup file bound, not the smaller readable-index bound: an
		// oversized index is still derived data, never an ordinary file.
		if !strings.Contains(name, "/") && info.Size() <= MaxFileBytes {
			if found, ok := declaredIndex(source, name); ok {
				if len(candidates) >= MaxIndexes {
					return errors.New("the project holds more indexes than one backup records")
				}
				candidates = append(candidates, found)
				return nil
			}
		}
		if info.Size() > MaxFileBytes || total > MaxBytes-info.Size() {
			return errors.New("the project holds more evidence than one backup stores")
		}
		total += info.Size()
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	slices.Sort(names)
	return names, candidates, nil
}

// declaredIndex reports whether one file declares the index contract, and the
// index it holds when this release can read it. A file that declares the
// contract and cannot be read is still an index: recording it as an ordinary
// file would copy a damaged derived document into the backup and restore it as
// though it were evidence.
func declaredIndex(source *os.Root, name string) (candidate, bool) {
	opened, err := source.Open(name)
	if err != nil {
		return candidate{}, false
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, MaxFileBytes+1))
	if err != nil || int64(len(data)) > MaxFileBytes {
		// The copy step opens the same file and reports the failure by itself,
		// with one diagnostic rather than two.
		return candidate{}, false
	}
	if !index.DeclaresSchema(data) {
		return candidate{}, false
	}
	document, err := index.Decode(data)
	if err != nil {
		return candidate{name: name}, true
	}
	return candidate{name: name, document: document, readable: true}, true
}

// inspect verifies every case and revision the project registers, in the order
// the project holds them. Each one is opened through the shared reader and its
// identity compared with the identity the project recorded; what the reader
// reported is what the backup records.
//
// It also returns every identity by which each artifact can be recognised: the
// one the project recorded, and the one the reader observed where the two
// differ. An index names the identity it was actually built from, so an index
// of evidence that changed after it was registered is still matched to the
// entry it belongs to.
func inspect(root string, document project.Document, revisions project.Revisions) ([]Artifact, map[string]string) {
	entries := make([]Artifact, 0, len(document.Cases)+len(revisions.Revisions))
	known := make(map[string]string, len(document.Cases)+len(revisions.Revisions))
	// The first artifact to claim an identity keeps it, which is the precedence
	// the project itself uses when it resolves a name. A project written by
	// this release never registers one identity twice — every writer refuses
	// it, across both documents — so this only decides what a hand-edited pair
	// means, and it decides it the same way every time.
	claim := func(identity, name string) {
		if identity == "" {
			return
		}
		if _, taken := known[identity]; !taken {
			known[identity] = name
		}
	}
	record := func(name string, kind Kind, identity string) {
		state, observed := verify(root, name, identity)
		entries = append(entries, Artifact{Name: name, Kind: kind, Identity: identity, State: state})
		claim(identity, name)
		claim(observed, name)
	}
	for _, entry := range document.Cases {
		record(entry.Name, CaseKind, entry.Identity)
	}
	for _, entry := range revisions.Revisions {
		record(entry.Name, RevisionKind, entry.Identity)
	}
	return entries, known
}

// verify opens one registered artifact and reports what it found, with the
// identity the reader observed when it could open one. A directory that is not
// there is missing; one the reader refused is unreadable; one that opened under
// a different identity is changed and is never re-identified.
func verify(root, name, identity string) (State, string) {
	if artifactpath.EntryName(name) != nil {
		return Unreadable, ""
	}
	if _, err := os.Lstat(artifactpath.JoinReference(root, name)); errors.Is(err, fs.ErrNotExist) {
		return Missing, ""
	}
	evidence, err := artifactpath.Child(root, name)
	if err != nil {
		return Unreadable, ""
	}
	opened, err := bundle.Open(evidence)
	if err != nil {
		return Unreadable, ""
	}
	if opened.Identity != identity {
		return Changed, opened.Identity
	}
	return Verified, opened.Identity
}

// recipesFor records what each index was built under and which registered
// artifact it describes. An index is matched by the case identity it names,
// against the identities inspect recognised for each entry, so an index built
// after the evidence changed is still matched to the entry it belongs to
// instead of being reported as unregistered.
func recipesFor(candidates []candidate, registered map[string]string) []Index {
	entries := make([]Index, 0, len(candidates))
	for _, found := range candidates {
		entry := Index{Name: found.name, Recipe: Undeclared, Fields: []string{}}
		if !found.readable {
			entries = append(entries, entry)
			continue
		}
		name, known := registered[found.document.Case.Identity]
		if !known {
			entry.Recipe = Unregistered
			entries = append(entries, entry)
			continue
		}
		entry.Recipe, entry.Case = Declared, name
		entry.Fields = slices.Clone(found.document.Policy.Fields)
		entry.Retention = string(found.document.Policy.Retention)
		if until := found.document.Policy.RetainUntil; until != nil {
			instant := *until
			entry.RetainUntil = &instant
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b Index) int { return strings.Compare(a.Name, b.Name) })
	return entries
}

// copyAll stores every file of the project in a backup directory that has
// already been created, and records what it wrote.
func copyAll(ctx context.Context, source *os.Root, target *artifactdir.Writer, names []string) ([]File, int64, error) {
	files := make([]File, 0, len(names))
	total := int64(0)
	for i, name := range names {
		if ctx.Err() != nil {
			return nil, 0, errors.New("backup cancelled at file " + position(i) + "; an incomplete backup is retained and carries no completion marker")
		}
		copied, err := copyInto(ctx, source, target, name)
		if err != nil {
			return nil, 0, err
		}
		total += copied.Size
		if total > MaxBytes {
			return nil, 0, errors.New("the project holds more evidence than one backup stores; an incomplete backup is retained")
		}
		files = append(files, copied)
	}
	return files, total, nil
}

// inspectCopy opens the project a backup has just stored and verifies every
// case and revision it registers from those stored files. Reading the record
// back out of the copy is what makes a manifest stand behind the bytes the
// backup holds: a project that was edited while it was being copied is reported
// as the copy reads, never as the source read before the copy began.
func inspectCopy(stored string) ([]Artifact, map[string]string, error) {
	opened, err := project.Open(stored)
	if err != nil {
		return nil, nil, errors.New("the project document the backup stored cannot be read; an incomplete backup is retained")
	}
	revisions, err := project.ReadRevisions(opened.Root)
	if err != nil {
		return nil, nil, errors.New("the revision document the backup stored cannot be read; an incomplete backup is retained")
	}
	entries, registered := inspect(opened.Root, opened.Document, revisions)
	return entries, registered, nil
}

// family is a backup: the stored copy of the project under files/, the
// manifest naming every stored file, and the completion marker written last,
// the digest of the manifest under the backup schema. Every file and every
// directory naming one is synced before a backup is reported, so a retirement
// that deletes the source after it never outlives the only copy.
var family = artifactdir.Family{
	Layout: artifactdir.Layout{
		Nested:    []string{FilesDirectory},
		AllowFile: func(name string) bool { return name == DocumentName || name == MarkerName },
	},
	Seal: artifactdir.ManifestHash(Schema, DocumentName, MarkerName),
	Errors: artifactdir.Errors{
		Reserve:   errors.New("cannot create backup; destination must be new and parent writable"),
		Open:      errors.New("cannot open new backup directory"),
		Directory: errors.New("cannot create a directory of the destination"),
		Create:    errors.New("cannot create a file of the destination"),
		Write:     errors.New("cannot write a file of the destination"),
		Complete:  errors.New("cannot complete the backup; an incomplete backup is retained"),
		Sync:      errors.New("cannot sync the backup directory; the backup was written in full but a power loss could still lose it"),
	},
}

// seal writes the manifest and then the completion marker, after every stored
// file, so every way a backup can stop leaves a directory that carries no
// marker and is refused rather than restored.
func seal(target *artifactdir.Writer, document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	if target.WriteFile(DocumentName, data) != nil {
		return errors.New("cannot write the backup document; an incomplete backup is retained")
	}
	_, err = target.Seal(nil)
	return err
}

// copyInto stores one file of the project and records what was written.
func copyInto(ctx context.Context, source *os.Root, target *artifactdir.Writer, name string) (File, error) {
	size, digest, err := copyFile(ctx, source, func(name string) (copied, error) { return target.Open(name) }, name, FilesDirectory+"/"+name)
	if err != nil {
		return File{}, errors.New(err.Error() + "; an incomplete backup is retained")
	}
	return File{Path: name, Size: size, SHA256: digest}, nil
}

// copied is the new file one copy writes: a stored file of a backup, or a file
// of a restored project.
type copied interface {
	io.Writer
	Sync() error
	Close() error
}

// copyFile streams one file from one artifact directory into another and
// returns the length and digest of what it wrote, hashing the bytes it wrote
// rather than the ones it read. Taking a backup and restoring one are the same
// copy in opposite directions, so they share it: the caller creates the new
// file, reporting a directory or file it cannot create in the words both use,
// decides whether the answer is a record to keep or a claim to check, and adds
// which of the two incomplete artifacts its failure leaves behind.
func copyFile(ctx context.Context, source *os.Root, create func(string) (copied, error), from, to string) (int64, string, error) {
	in, err := source.Open(from)
	if err != nil {
		return 0, "", errors.New("cannot read a file of the source")
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return 0, "", errors.New("only a regular file is copied")
	}
	out, err := create(to)
	if err != nil {
		return 0, "", err
	}
	sum := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, sum), io.LimitReader(contextReader{ctx, in}, MaxFileBytes+1))
	if copyErr == nil {
		copyErr = ctx.Err()
	}
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		return 0, "", errors.New("cannot write a file of the destination")
	}
	if written > MaxFileBytes {
		return 0, "", errors.New("a file exceeds the size one backup stores")
	}
	return written, hex.EncodeToString(sum.Sum(nil)), nil
}

// contextReader checks between bounded copy chunks, including the final read.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
