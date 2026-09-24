package artifactdir

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// Family is one kind of sealed directory, declared once by the package that
// owns it: the layout its writer may make, which a reader of the family may
// read it with, the rule its completion record is made by, the sentences a
// failed write is reported in, and what a write that stops before its
// completion record leaves behind.
type Family struct {
	Layout     Layout
	Seal       Seal
	Errors     Errors
	Incomplete Incomplete
}

// Errors are the sentences a family's failed writes are reported in. Each is
// returned exactly as declared, so a sentinel a caller separates keeps its
// identity. A member the family reports in a sentence of its own, such as a
// journal it appends to, maps that one failure itself.
type Errors struct {
	// Destination, when set, is reported for a destination artifactpath
	// refuses; otherwise that refusal is reported as artifactpath words it.
	Destination error
	// Reserve is reported when the new directory cannot be made or the folder
	// holding it cannot be opened. Nothing has been written.
	Reserve error
	// Open is reported when the new directory, once made, cannot be opened.
	// When nil, Reserve is.
	Open error
	// Directory is reported when a member directory cannot be made. When nil,
	// Create is.
	Directory error
	// Create is reported when a member cannot be created. When nil, Write is.
	Create error
	// Write is reported when a member cannot be written and synced; what was
	// written is retained incomplete, without a completion record.
	Write error
	// Replace is reported when a member written in full under its staging
	// name cannot be renamed into place. When nil, Write is.
	Replace error
	// Verify is reported when a streamed directory sealed by its directory
	// hash cannot be read back through its layout to state that identity.
	// When nil, the reader's own refusal is.
	Verify error
	// Complete is reported when the completion record cannot be written. When
	// nil, the record is reported as any member is.
	Complete error
	// Sync is reported when a directory sync fails. Once the completion record
	// is written, every file is synced, so it says the artifact was written in
	// full but a power loss could still lose it.
	Sync error
	// Cancelled is reported when a whole-map write is stopped by its context.
	// When nil, Write is.
	Cancelled error
}

func (e Errors) write() error {
	if e.Write != nil {
		return e.Write
	}
	return errors.New("cannot write artifact; incomplete artifact retained")
}

func (e Errors) create() error {
	if e.Create != nil {
		return e.Create
	}
	return e.write()
}

func (e Errors) directory() error {
	if e.Directory != nil {
		return e.Directory
	}
	return e.create()
}

func (e Errors) reserve() error {
	if e.Reserve != nil {
		return e.Reserve
	}
	return errors.New("cannot create artifact; destination must be new and parent readable and writable")
}

func (e Errors) open() error {
	if e.Open != nil {
		return e.Open
	}
	return e.reserve()
}

func (e Errors) replace() error {
	if e.Replace != nil {
		return e.Replace
	}
	return e.write()
}

func (e Errors) sync() error {
	if e.Sync != nil {
		return e.Sync
	}
	return errors.New("cannot sync artifact directory; the artifact was written in full but a power loss could still lose it")
}

func (e Errors) cancelled() error {
	if e.Cancelled != nil {
		return e.Cancelled
	}
	return e.write()
}

// Incomplete is what a write that stops before its completion record leaves.
type Incomplete uint8

const (
	// RetainIncomplete leaves what was written, without a completion record,
	// so every reader refuses it and a retry names a new destination.
	RetainIncomplete Incomplete = iota
	// RemoveIncomplete removes the directory, for a family whose partial
	// output must not be left for anyone to find.
	RemoveIncomplete
)

// Seal is the rule a family's completion record is made by. It is one of a
// closed set, chosen through its constructors; the zero Seal writes no
// completion record, for a workspace or for a directory whose completion is
// the last record of a log it appends to, such as a durable job's journal.
type Seal struct {
	rule     sealRule
	domain   string
	manifest string
	marker   string
	record   string
	staging  string
}

type sealRule uint8

const (
	unsealed sealRule = iota
	directoryHash
	manifestHash
	completionRecord
)

// DirectoryHash seals with the ADR-0002 identity of every other file of the
// directory under domain, written to identity.sha256 once every other file is.
func DirectoryHash(domain string) Seal {
	return Seal{rule: directoryHash, domain: domain, marker: "identity.sha256"}
}

// ManifestHash seals with the SHA-256 of the manifest member, which indexes
// every other file, written to marker once the manifest is. A domain, when
// not empty, is hashed first as its own line.
func ManifestHash(domain, manifest, marker string) Seal {
	return Seal{rule: manifestHash, domain: domain, manifest: manifest, marker: marker}
}

// CompletionRecord seals with a record the family makes itself, written last
// as the member record. When staging is not empty the record is written there
// first and renamed onto record, so record never holds a partial write.
func CompletionRecord(record, staging string) Seal {
	return Seal{rule: completionRecord, record: record, staging: staging}
}

// Writer writes one sealed directory member by member, for a family that
// streams it: a run recording each send, a job journaling its intents, a
// packet read back while it is assembled. It is not safe for concurrent use.
// Close releases it, and removes an incomplete directory when its family says
// to.
type Writer struct {
	family     Family
	durability Durability
	path       string
	// parent is the folder holding the directory, opened before the directory
	// is made in it and synced last.
	parent *os.Root
	root   *os.Root
	made   []string
	// files is every member a whole-map write wrote, which its identity is
	// made from; a stream is read back through its family's layout instead.
	files    map[string][]byte
	manifest []byte
	complete bool
}

// Create reserves a new directory at path for family and opens it. path is
// resolved as a destination outside retained evidence and outside every
// protected source directory. The folder holding it is opened first, because
// it is synced last: a folder the writer cannot open is refused before
// anything is created, and the directory is made inside that opened folder,
// so the folder synced is the one naming it.
func Create(path string, family Family, durability Durability, protected ...os.FileInfo) (*Writer, error) {
	resolved, err := artifactpath.Destination(path, protected...)
	if err != nil {
		if family.Errors.Destination != nil {
			return nil, family.Errors.Destination
		}
		return nil, err
	}
	parent, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return nil, family.Errors.reserve()
	}
	if err := parent.Mkdir(filepath.Base(resolved), 0700); err != nil {
		parent.Close()
		return nil, family.Errors.reserve()
	}
	return open(parent, filepath.Base(resolved), family, durability)
}

// CreateTemp is Create for a workspace whose name only has to be new: a
// directory in folder named pattern followed by a random number.
func CreateTemp(folder, pattern string, family Family, durability Durability) (*Writer, error) {
	resolved, err := artifactpath.Destination(filepath.Join(folder, pattern))
	if err != nil {
		if family.Errors.Destination != nil {
			return nil, family.Errors.Destination
		}
		return nil, err
	}
	parent, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return nil, family.Errors.reserve()
	}
	for range 10000 {
		name := pattern + strconv.FormatUint(uint64(rand.Uint32()), 10)
		err := parent.Mkdir(name, 0700)
		if err == nil {
			return open(parent, name, family, durability)
		}
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	parent.Close()
	return nil, family.Errors.reserve()
}

// open opens the directory name just made in parent for family's writer. A
// directory it cannot open is removed when the family removes incomplete
// output.
func open(parent *os.Root, name string, family Family, durability Durability) (*Writer, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		if family.Incomplete == RemoveIncomplete {
			parent.Remove(name)
		}
		parent.Close()
		return nil, family.Errors.open()
	}
	return &Writer{family: family, durability: durability, path: filepath.Join(parent.Name(), name), parent: parent, root: root}, nil
}

// Path is the resolved path of the directory being written.
func (w *Writer) Path() string { return w.path }

// Close releases the directory. One its writer did not complete is removed
// when its family says RemoveIncomplete, and otherwise left as written.
func (w *Writer) Close() {
	if w.root == nil {
		return
	}
	w.root.Close()
	w.parent.Close()
	w.root, w.parent = nil, nil
	if !w.complete && w.family.Incomplete == RemoveIncomplete {
		os.RemoveAll(w.path)
	}
}

// member reports whether name is a member this writer may still create: a
// relative name the family's layout admits as a file, and never the name its
// completion record is written to.
func (w *Writer) member(name string) bool {
	seal := w.family.Seal
	return !w.complete && fs.ValidPath(name) && name != "." && name != seal.marker && name != seal.record && w.family.Layout.admitsFile(name)
}

// Mkdir makes the member directory name and any missing directory above it.
// Every directory a writer makes is synced when it syncs. A directory that
// already exists and was not made by this writer is refused: another writer
// owns what is below it.
func (w *Writer) Mkdir(name string) error {
	if w.complete || !fs.ValidPath(name) || name == "." {
		return w.family.Errors.directory()
	}
	var missing []string
	for directory := name; directory != "."; directory = path.Dir(directory) {
		if slices.Contains(w.made, directory) {
			break
		}
		missing = append(missing, directory)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if !w.family.Layout.admitsDirectory(missing[i]) || w.root.Mkdir(missing[i], 0700) != nil {
			return w.family.Errors.directory()
		}
		w.made = append(w.made, missing[i])
	}
	return nil
}

// WriteFile creates one new member, making any missing directory above it,
// and writes and syncs data in it. It never replaces an existing member.
func (w *Writer) WriteFile(name string, data []byte) error {
	if !w.member(name) {
		return w.family.Errors.create()
	}
	if err := w.writeMember(name, data, w.family.Errors.create(), w.family.Errors.write()); err != nil {
		return err
	}
	w.wrote(name, data)
	return nil
}

// wrote keeps the manifest a ManifestHash family's completion record digests.
func (w *Writer) wrote(name string, data []byte) {
	if w.family.Seal.rule == manifestHash && name == w.family.Seal.manifest {
		w.manifest = bytes.Clone(data)
	}
}

// writeMember writes one member, reporting a failure to create it as created
// and any other failure as written.
func (w *Writer) writeMember(name string, data []byte, created, written error) error {
	if directory := path.Dir(name); directory != "." {
		if err := w.Mkdir(directory); err != nil {
			return err
		}
	}
	err := w.durability.writeFile(w.root, name, data)
	if errors.Is(err, ErrCreateFile) {
		return created
	}
	if err != nil {
		return written
	}
	return nil
}

// Replace writes data as the new member staging and renames it onto name, so
// name never holds a partial write, whatever it held before. staging is never
// part of a complete directory, so the layout need not admit it.
func (w *Writer) Replace(name, staging string, data []byte) error {
	if !w.member(name) || !fs.ValidPath(staging) || path.Dir(staging) != path.Dir(name) {
		return w.family.Errors.create()
	}
	if err := w.writeMember(staging, data, w.family.Errors.create(), w.family.Errors.write()); err != nil {
		return err
	}
	if w.root.Rename(staging, name) != nil {
		return w.family.Errors.replace()
	}
	w.wrote(name, data)
	return nil
}

// Member is one file of a sealed directory written as a stream, such as an
// append-only log or a copy too large to hold in memory. Its writer frames
// what it writes and syncs it at its own boundaries; Sync keeps the writer's
// durability.
type Member struct {
	file       *os.File
	durability Durability
}

// Open creates the new member name as a stream, making any missing directory
// above it.
func (w *Writer) Open(name string) (*Member, error) {
	if !w.member(name) {
		return nil, w.family.Errors.create()
	}
	if directory := path.Dir(name); directory != "." {
		if err := w.Mkdir(directory); err != nil {
			return nil, err
		}
	}
	file, err := w.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, w.family.Errors.create()
	}
	return &Member{file: file, durability: w.durability}, nil
}

func (m *Member) Write(p []byte) (int, error) { return m.file.Write(p) }

// Sync flushes what was written unless the writer is Scratch.
func (m *Member) Sync() error { return m.durability.Sync(m.file) }

func (m *Member) Close() error { return m.file.Close() }

// Remove removes a member that is not part of the complete directory, such as
// a lease a job holds only while it runs. Removing it is not synced.
func (w *Writer) Remove(name string) error {
	if !fs.ValidPath(name) || name == "." {
		return errors.New("invalid artifact member name")
	}
	return w.root.Remove(name)
}

// SyncDirectories syncs member directories another writer made inside this
// one, such as a nested packet's, so this writer does not depend on that one
// to have synced them before it goes on.
func (w *Writer) SyncDirectories(names ...string) error {
	if w.durability == Scratch {
		return nil
	}
	for _, name := range names {
		if !fs.ValidPath(name) || syncDirectory(w.root, name) != nil {
			return w.family.Errors.sync()
		}
	}
	return nil
}

// Sync syncs every directory this writer made, the directory itself and the
// folder holding it, so every name it made survives a power loss. After
// Complete that is the whole artifact; before it, it is a checkpoint, such as
// a job's before its first send.
func (w *Writer) Sync() error {
	if w.durability == Scratch {
		return nil
	}
	made := slices.Sorted(slices.Values(w.made))
	for _, name := range made {
		if syncDirectory(w.root, name) != nil {
			return w.family.Errors.sync()
		}
	}
	if syncDirectory(w.root, ".") != nil || syncDirectory(w.parent, ".") != nil {
		return w.family.Errors.sync()
	}
	return nil
}

// Complete writes the completion record, after which no member is written;
// Sync then makes the directory survive a power loss. record is the record a
// CompletionRecord family makes and is nil for any other. It answers the
// identity the record states, which is empty for a CompletionRecord family. A
// directory whose record is its ADR-0002 identity is read back through its
// family's layout, so a nested packet another writer made is part of it.
func (w *Writer) Complete(record []byte) (string, error) {
	seal, errs := w.family.Seal, w.family.Errors
	created, written := errs.create(), errs.write()
	if errs.Complete != nil {
		created, written = errs.Complete, errs.Complete
	}
	if w.complete || (seal.rule == completionRecord) != (record != nil) {
		return "", errors.New("invalid artifact completion record")
	}
	identity := ""
	switch seal.rule {
	case directoryHash:
		files := w.files
		if files == nil {
			layout := w.family.Layout
			layout.RequiredFiles = slices.DeleteFunc(slices.Clone(layout.RequiredFiles), func(name string) bool { return name == seal.marker })
			read, err := Read(w.path, layout)
			if err != nil {
				if errs.Verify != nil {
					return "", errs.Verify
				}
				return "", err
			}
			files = read
		}
		identity = Identity(seal.domain, files)
	case manifestHash:
		if w.manifest == nil {
			return "", errors.New("artifact manifest must be written before its completion record")
		}
		sum := sha256.New()
		if seal.domain != "" {
			sum.Write([]byte(seal.domain + "\n"))
		}
		sum.Write(w.manifest)
		identity = hex.EncodeToString(sum.Sum(nil))
	case completionRecord:
		name := seal.record
		if seal.staging != "" {
			name = seal.staging
		}
		if err := w.writeMember(name, record, created, written); err != nil {
			return "", err
		}
		if seal.staging != "" && w.root.Rename(seal.staging, seal.record) != nil {
			if errs.Complete != nil {
				return "", errs.Complete
			}
			return "", errs.replace()
		}
		w.complete = true
		return "", nil
	default:
		return "", errors.New("artifact family writes no completion record")
	}
	if err := w.writeMember(seal.marker, []byte(identity+"\n"), created, written); err != nil {
		return "", err
	}
	w.complete = true
	return identity, nil
}

// Seal completes the directory and then syncs every entry it is found
// through, answering only once both are done.
func (w *Writer) Seal(record []byte) (string, error) {
	identity, err := w.Complete(record)
	if err != nil {
		return "", err
	}
	if err := w.Sync(); err != nil {
		return "", err
	}
	return identity, nil
}

// Write creates one sealed directory from a whole map of files, for a family
// that holds every byte before it writes. Every directory its layout lists is
// made first, even one no file is written in. Files are written in bytewise
// path order, a manifest just before its completion record, which is written
// last; a CompletionRecord family's record is files[record]. It answers only once
// every directory naming one of them, the directory itself and the folder
// holding it are synced. A cancellation is observed between files: one
// arriving before the destination exists creates nothing, and one arriving
// later stops the write with what it wrote treated as any interrupted write.
func Write(ctx context.Context, path string, family Family, durability Durability, files map[string][]byte) (string, error) {
	seal := family.Seal
	members := maps.Clone(files)
	var record []byte
	switch seal.rule {
	case completionRecord:
		record = members[seal.record]
		if record == nil {
			return "", errors.New("artifact files must supply their completion record")
		}
		delete(members, seal.record)
	case unsealed:
		return "", errors.New("artifact family writes no completion record")
	default:
		if _, exists := members[seal.marker]; exists {
			return "", errors.New("artifact files cannot supply their own completion marker")
		}
	}
	names := slices.Sorted(maps.Keys(members))
	if seal.rule == manifestHash {
		if _, exists := members[seal.manifest]; !exists {
			return "", errors.New("artifact files must supply their manifest")
		}
		names = append(slices.DeleteFunc(names, func(name string) bool { return name == seal.manifest }), seal.manifest)
	}
	if ctx.Err() != nil {
		return "", family.Errors.cancelled()
	}
	w, err := Create(path, family, durability)
	if err != nil {
		return "", err
	}
	defer w.Close()
	w.files = members
	for _, directory := range slices.Sorted(slices.Values(family.Layout.AllowedDirectories)) {
		if err := w.Mkdir(directory); err != nil {
			return "", err
		}
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return "", family.Errors.cancelled()
		}
		if err := w.WriteFile(name, members[name]); err != nil {
			return "", err
		}
	}
	return w.Seal(record)
}
