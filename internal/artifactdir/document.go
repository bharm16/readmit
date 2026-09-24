package artifactdir

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// Document is one kind of single-file document, declared once by the package
// that owns it, and the store every such document is created, replaced and
// read through: the bound a reader holds it to, whether a reader follows a
// link at its name, where a replacement is staged, whether the bytes a
// replacement replaces are kept, how durably it is written, and the sentences
// its failures are reported in.
//
// A write is durable unless the document says Scratch: the file is synced
// before its handle closes, and once it is in place the folder naming it is
// synced before the write answers, so a document reported written survives a
// power loss. A replacement is written in full under its staging name and
// renamed onto the document, so a reader sees either the previous document or
// the new one, and a failed replacement leaves the previous one exactly as it
// was.
type Document struct {
	// MaxBytes bounds what a read accepts, and the current bytes a replacement
	// keeping Previous copies reads.
	MaxBytes int
	// Links is whether a read follows a symbolic link at the document's name.
	Links Links
	// OwnerOnly refuses to read a file whose permissions let anyone but its
	// owner read or write it, as it refuses any other file that is not the
	// regular file the document must be. It is the caller's to set: a caller
	// that treats Windows access control lists as the administrator's
	// boundary sets it only where mode bits describe access.
	OwnerOnly bool
	// Staging is where a replacement is written before it is renamed into
	// place. The zero Staging is the document's name followed by
	// ".incomplete".
	Staging Staging
	// Previous, when set, keeps the exact bytes each replacement replaces
	// beside the document, under the name PreviousName gives them, before the
	// replacement is renamed into place. A copy is never overwritten.
	Previous *Previous
	// RetainFailed leaves what a failed write wrote where it is — a new
	// document's partial bytes, or a replacement's staged file — so the next
	// write is refused until someone looks at it, rather than removing it. It
	// is for a writer whose partial output must stop the next one, such as an
	// operation guard's clock. A Create by link never leaves its staged file.
	RetainFailed bool
	// CreateByLink makes Create write a new document under its staging name
	// and link it into place, so the name never holds a partial document. The
	// folder must support hard links.
	CreateByLink bool
	// Durability is Durable unless the document lives in a throwaway
	// workspace its owner removes before answering; a Scratch document syncs
	// neither itself nor its folder.
	Durability Durability
	// Flush, when set, is the sync a Durable write makes of the file it
	// writes, in place of the shared one, for a writer whose flushes are a
	// seam of its own, such as a receiver's ledger.
	Flush func(*os.File) error
	// Errors are the sentences a failed write is reported in.
	Errors DocumentErrors
	// Refusals are the sentences a refused read is reported in.
	Refusals DocumentRefusals
}

// Links is whether a document read follows a symbolic link at the document's
// own name. A link in a folder above it is always followed, as the
// filesystem does.
type Links uint8

const (
	// RefuseLinks refuses a symbolic link at the name as it refuses any other
	// entry that is not a regular file.
	RefuseLinks Links = iota
	// FollowLinks reads the regular file a link at the name leads to, for a
	// reader of a document a person names, which has always done so.
	FollowLinks
)

// Staging is where a replacement is written in full before it is renamed
// onto its document. It is one of a closed set, chosen through its
// constructors.
//
// The zero Staging stages a replacement at the document's name followed by
// ".incomplete". That name must not already exist: a replacement a crash
// interrupted is retained there and reported, and every later replacement is
// refused until someone looks at it, rather than overwritten.
type Staging struct {
	name    string
	pattern string
}

// StagingName stages every replacement at name, in the document's folder,
// with the same exclusivity as the zero Staging.
func StagingName(name string) Staging { return Staging{name: name} }

// StagingTemp stages each replacement at a new name in the document's folder,
// made from pattern with its last "*" replaced by a random number, as
// os.CreateTemp names one. A replacement never finds another's staged file, so
// one a crash left behind refuses nothing.
func StagingTemp(pattern string) Staging { return Staging{pattern: pattern} }

// DocumentErrors are the sentences a document's failed writes are reported
// in. Each is returned as declared; where the filesystem refused to create a
// name — the new document, a staged replacement, or the link a Create by link
// makes — its error is kept behind the sentence, so errors.Is can tell a
// folder this account may not write (fs.ErrPermission), or a name already
// taken (fs.ErrExist), from any other failure. A sentence never names a path.
type DocumentErrors struct {
	// Destination is a path artifactpath refuses. When nil, the refusal is
	// reported as artifactpath words it.
	Destination error
	// Create is a new document, or a replacement's staged file, that cannot be
	// created: the name is taken, an interrupted replacement is retained there,
	// or the folder cannot be written.
	Create error
	// Write is bytes that cannot be written and synced in full. Nothing a
	// reader would take for the document is left.
	Write error
	// Install is a complete staged document that cannot be renamed onto its
	// name, or linked there by a Create that stages. When nil, Write.
	Install error
	// Sync is a folder that cannot be synced once the document is in place: it
	// was written in full, but a power loss could still lose it.
	Sync error
}

func (e DocumentErrors) destination(refusal error) error {
	if e.Destination != nil {
		return e.Destination
	}
	return refusal
}

func (e DocumentErrors) create(cause error) error {
	return report(e.Create, "cannot create the document; its name is taken or its folder cannot be written", cause, true)
}

func (e DocumentErrors) write(cause error) error {
	return report(e.Write, "cannot write the document", cause, false)
}

// install reports a staged document that could not be put in place: renamed
// onto its name, or linked there, which creates the name, so a link keeps the
// filesystem's cause behind the sentence.
func (e DocumentErrors) install(cause error, linked bool) error {
	if e.Install == nil {
		return report(e.Write, "cannot write the document", cause, linked)
	}
	return report(e.Install, "", cause, linked)
}

func (e DocumentErrors) sync(cause error) error {
	return report(e.Sync, "cannot sync the document's folder; the document was written in full but a power loss could still lose it", cause, false)
}

// DocumentRefusals are the sentences a document's refused reads are reported
// in, returned as DocumentErrors are: the filesystem's error is kept behind an
// Inspect or an Open refusal, so errors.Is can tell a document that is not
// there (fs.ErrNotExist) or that this account may not read.
type DocumentRefusals struct {
	// Inspect is a name that cannot be inspected: nothing is there
	// (fs.ErrNotExist), or its folder cannot be read. When nil, Irregular.
	Inspect error
	// Irregular is a name that holds something other than a regular file: a
	// folder, device, pipe or socket, or a symbolic link unless the document
	// follows links.
	Irregular error
	// Open is a regular file that cannot be opened. When nil, Read.
	Open error
	// Changed is an opened file that is not a regular file, or, after a few
	// attempts, not the file inspected a moment before. When nil, Irregular.
	Changed error
	// Read is a file that cannot be read.
	Read error
	// Size is contents past MaxBytes. When nil, Read.
	Size error
}

func (r DocumentRefusals) irregular() error {
	return report(r.Irregular, "the document must be a regular file", nil, false)
}

func (r DocumentRefusals) inspect(cause error) error {
	if r.Inspect == nil {
		return report(r.Irregular, "the document must be a regular file", cause, true)
	}
	return report(r.Inspect, "", cause, true)
}

func (r DocumentRefusals) read(cause error) error {
	return report(r.Read, "cannot read the document", cause, false)
}

func (r DocumentRefusals) open(cause error) error {
	if r.Open == nil {
		return report(r.Read, "cannot read the document", cause, true)
	}
	return report(r.Open, "", cause, true)
}

func (r DocumentRefusals) changed() error {
	if r.Changed == nil {
		return r.irregular()
	}
	return report(r.Changed, "", nil, false)
}

func (r DocumentRefusals) size() error {
	if r.Size == nil {
		return r.read(nil)
	}
	return report(r.Size, "", nil, false)
}

// FilesystemReport, declared as a step's sentence, reports that step's
// failure as the filesystem worded it, for a caller that has always shown the
// filesystem's own error. That error can name a path, which no new sentence
// does; a step with no filesystem error reports the store's own sentence.
var FilesystemReport = errors.New("the failure as the filesystem reported it")

// report is the declared sentence, or fallback when it is nil, keeping the
// filesystem's cause behind it when attach says to. A step declared as
// FilesystemReport is the cause itself.
func report(sentence error, fallback string, cause error, attach bool) error {
	if sentence == FilesystemReport {
		if cause != nil {
			return cause
		}
		sentence = nil
	}
	if sentence == nil {
		sentence = errors.New(fallback)
	}
	if attach && cause != nil {
		return causedError{sentence, cause}
	}
	return sentence
}

// causedError is a declared sentence with the filesystem's own error behind
// it. It reads as the sentence alone, and errors.Is matches either.
type causedError struct {
	sentence error
	cause    error
}

func (e causedError) Error() string { return e.sentence.Error() }

func (e causedError) Unwrap() []error { return []error{e.sentence, e.cause} }

// Read reads the document at path whole, within MaxBytes. It inspects the
// name before opening it, so a pipe is refused before an open could block on
// it, and it refuses an opened file that is not a regular file. An opened file
// that is not the one inspected a moment before — the name was replaced
// underneath, as a live document is by every rename onto it — is inspected and
// opened again, a few times, before it is refused. A file that says it is
// longer than MaxBytes is refused before its bytes are read, and what is read
// is checked against the bound too, because a file can say less than it
// holds.
func (d Document) Read(path string) ([]byte, error) {
	stat := os.Lstat
	if d.Links == FollowLinks {
		stat = os.Stat
	}
	return d.read(func() (fs.FileInfo, error) { return stat(path) }, func() (*os.File, error) { return os.Open(path) })
}

// ReadIn is Read of the document name below root. A link, when the document
// follows links, is followed only within root.
func (d Document) ReadIn(root *os.Root, name string) ([]byte, error) {
	stat := root.Lstat
	if d.Links == FollowLinks {
		stat = root.Stat
	}
	return d.read(func() (fs.FileInfo, error) { return stat(name) }, func() (*os.File, error) { return root.Open(name) })
}

// readAttempts bounds how often a read inspects and opens a name that keeps
// being replaced underneath it.
const readAttempts = 8

// read inspects and opens one name, checking what it read against the bound.
func (d Document) read(inspect func() (fs.FileInfo, error), open func() (*os.File, error)) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		info, err := inspect()
		if err != nil {
			return nil, d.Refusals.inspect(err)
		}
		if !info.Mode().IsRegular() || d.OwnerOnly && info.Mode().Perm()&0077 != 0 {
			return nil, d.Refusals.irregular()
		}
		file, err := open()
		if err != nil {
			return nil, d.Refusals.open(err)
		}
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() {
			file.Close()
			return nil, d.Refusals.changed()
		}
		if !os.SameFile(info, opened) {
			file.Close()
			if attempt < readAttempts {
				continue
			}
			return nil, d.Refusals.changed()
		}
		limit := max(d.MaxBytes, 0)
		if opened.Size() > int64(limit) {
			file.Close()
			return nil, d.Refusals.size()
		}
		data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, d.Refusals.read(errors.Join(readErr, closeErr))
		}
		if len(data) > limit {
			return nil, d.Refusals.size()
		}
		return data, nil
	}
}

// reserve resolves path as a destination outside retained evidence and opens
// the folder holding it, answering that folder and the document's name in it.
func (d Document) reserve(path string) (*os.Root, string, error) {
	resolved, err := artifactpath.Destination(path)
	if err != nil {
		return nil, "", d.Errors.destination(err)
	}
	root, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return nil, "", d.Errors.create(err)
	}
	return root, filepath.Base(resolved), nil
}

// Create writes data as a new document at path, resolved as a destination
// outside retained evidence. It never replaces anything at the name, and a
// write that fails leaves nothing there unless the document retains failed
// writes.
func (d Document) Create(path string, data []byte) error {
	root, name, err := d.reserve(path)
	if err != nil {
		return err
	}
	defer root.Close()
	return d.CreateIn(root, name, data)
}

// CreateIn is Create of the document name below root.
func (d Document) CreateIn(root *os.Root, name string, data []byte) error {
	if d.CreateByLink {
		r, err := d.beginIn(root, name, false)
		if err != nil {
			return err
		}
		err = r.Write(data)
		if err == nil {
			if linkErr := root.Link(r.staging, name); linkErr != nil {
				err = d.Errors.install(linkErr, true)
			}
		}
		r.Abandon()
		if err != nil {
			return err
		}
		return d.syncFolder(root, name)
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return d.Errors.create(err)
	}
	writeErr := WriteFileSync(d.durable(file), data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		if !d.RetainFailed {
			root.Remove(name)
		}
		return d.Errors.write(errors.Join(writeErr, closeErr))
	}
	return d.syncFolder(root, name)
}

// Replace installs data as the document at path, resolved as a destination
// outside retained evidence, in place of whatever entry is at the name. It
// never opens that entry to write it: a symbolic link, a hard link or a pipe
// at the name is replaced, never written through, and a folder there is
// refused. A replacement that fails leaves the previous document as it was
// and removes its staged file, unless the document retains failed writes.
func (d Document) Replace(path string, data []byte) error {
	root, name, err := d.reserve(path)
	if err != nil {
		return err
	}
	defer root.Close()
	return d.ReplaceIn(root, name, data)
}

// ReplaceIn is Replace of the document name below root.
func (d Document) ReplaceIn(root *os.Root, name string, data []byte) error {
	if d.Previous != nil {
		if err := d.keepPrevious(root, name, data); err != nil {
			return err
		}
	}
	r, err := d.beginIn(root, name, false)
	if err != nil {
		return err
	}
	err = r.Write(data)
	if err == nil {
		err = r.Commit()
	}
	if err != nil && d.RetainFailed {
		r.Close()
	} else {
		r.Abandon()
	}
	return err
}

// Replacement is one replacement of a document in progress, for a writer that
// decides what the document will hold, or whether to replace it at all, only
// once its staged file is held: an update that reads the current document
// under that exclusive name, or a replacement installed only after another
// step succeeds. It is not safe for concurrent use.
type Replacement struct {
	document Document
	root     *os.Root
	// owned is a root Begin opened, which Close closes.
	owned   bool
	name    string
	staging string
	file    *os.File
	// staged is a staging file this replacement created that is still there.
	staged  bool
	written bool
}

// Begin starts a replacement of the document at path, resolved as a
// destination outside retained evidence, by creating its staged file. While it
// is held no other replacement staged at the same name can begin. The caller
// ends it with Commit, and with Abandon, which removes the staged file, or
// Close, which leaves whatever was staged for someone to look at. A document
// that keeps Previous copies is replaced whole, through Replace.
func (d Document) Begin(path string) (*Replacement, error) {
	if d.Previous != nil {
		return nil, errors.New("a document that keeps previous copies is replaced whole")
	}
	root, name, err := d.reserve(path)
	if err != nil {
		return nil, err
	}
	r, err := d.beginIn(root, name, true)
	if err != nil {
		root.Close()
		return nil, err
	}
	return r, nil
}

// beginIn creates the staged file of a replacement of name below root.
func (d Document) beginIn(root *os.Root, name string, owned bool) (*Replacement, error) {
	folder, base := filepath.Split(name)
	var staging string
	var file *os.File
	var err error
	switch {
	case d.Staging.pattern != "":
		prefix, suffix := d.Staging.pattern, ""
		if at := strings.LastIndex(prefix, "*"); at >= 0 {
			prefix, suffix = prefix[:at], prefix[at+1:]
		}
		for range 10000 {
			staging = folder + prefix + strconv.FormatUint(uint64(rand.Uint32()), 10) + suffix
			file, err = root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if !errors.Is(err, fs.ErrExist) {
				break
			}
		}
	case d.Staging.name != "":
		staging = folder + d.Staging.name
		file, err = root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	default:
		staging = folder + base + ".incomplete"
		file, err = root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return nil, d.Errors.create(err)
	}
	return &Replacement{document: d, root: root, owned: owned, name: name, staging: staging, file: file, staged: true}, nil
}

// Write writes data in full to the staged file and syncs and closes it. A
// failed write leaves the staged file for Abandon or Close to settle.
func (r *Replacement) Write(data []byte) error {
	if r.file == nil || r.written {
		return r.document.Errors.write(nil)
	}
	writeErr := WriteFileSync(r.document.durable(r.file), data)
	closeErr := r.file.Close()
	r.file = nil
	if writeErr != nil || closeErr != nil {
		return r.document.Errors.write(errors.Join(writeErr, closeErr))
	}
	r.written = true
	return nil
}

// Commit renames the written staged file onto the document and then syncs the
// folder naming it, answering only once both are done.
func (r *Replacement) Commit() error {
	d := r.document
	if !r.written || !r.staged {
		return d.Errors.install(nil, false)
	}
	if err := r.root.Rename(r.staging, r.name); err != nil {
		return d.Errors.install(err, false)
	}
	r.staged = false
	return d.syncFolder(r.root, r.name)
}

// Abandon removes the staged file, unless Commit renamed it into place, and
// releases the replacement.
func (r *Replacement) Abandon() {
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
	if r.staged {
		r.root.Remove(r.staging)
		r.staged = false
	}
	r.Close()
}

// Close releases the replacement and leaves whatever is staged in place.
func (r *Replacement) Close() {
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
	if r.owned && r.root != nil {
		r.root.Close()
	}
	r.root = nil
}

// durable is file as a durable write syncs it.
func (d Document) durable(file *os.File) DurableFile {
	if d.Durability == Durable && d.Flush != nil {
		return flushedFile{file, d.Flush}
	}
	return seamedFile{file, d.Durability}
}

// flushedFile is a file synced by its writer's own flush.
type flushedFile struct {
	*os.File
	flush func(*os.File) error
}

func (f flushedFile) Sync() error { return f.flush(f.File) }

// syncFolder syncs the folder naming name below root, unless the document is
// Scratch.
func (d Document) syncFolder(root *os.Root, name string) error {
	if d.Durability == Scratch {
		return nil
	}
	folder := filepath.Dir(name)
	if err := syncDirectory(root, folder); err != nil {
		return d.Errors.sync(err)
	}
	return nil
}

// Previous are the sentences a document keeping previous copies reports a
// copy it cannot keep in. Each refuses the replacement, which leaves the
// current document as it was.
type Previous struct {
	// Irregular is a copy's name that holds something other than a regular
	// file.
	Irregular error
	// Damaged is a copy's name that holds other bytes than those its name
	// records. A copy is never overwritten, even a damaged one.
	Damaged error
	// Create is a copy that cannot be created.
	Create error
	// Write is a copy that cannot be written in full. What was written is
	// left, so every later replacement reports it Damaged rather than keeping
	// the bytes nowhere.
	Write error
}

// previousInfix separates a copy's document name from the digest of the
// bytes it keeps.
const previousInfix = ".recovery-"

// PreviousName is the name the store keeps a document's previous bytes under
// beside it: the document's name, ".recovery-" and the lowercase hexadecimal
// SHA-256 of those bytes.
func PreviousName(document, digest string) string { return document + previousInfix + digest }

// ParsePreviousName reports whether name is one PreviousName gives, and the
// document and digest it records. A name whose digest is not a lowercase
// SHA-256 is not a previous copy.
func ParsePreviousName(name string) (document, digest string, ok bool) {
	at := strings.LastIndex(name, previousInfix)
	if at <= 0 {
		return "", "", false
	}
	document, digest = name[:at], name[at+len(previousInfix):]
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest {
		return "", "", false
	}
	return document, digest, true
}

// PreviousCopy is one previous copy a folder holds, as its name records it.
type PreviousCopy struct {
	Document string
	Digest   string
}

// ListPrevious lists every previous copy directory holds, in name order, as
// the names record them. It opens none of them; a reader of a copy checks its
// bytes against its digest.
func ListPrevious(directory string) ([]PreviousCopy, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, causedError{errors.New("cannot list the folder holding the previous copies"), err}
	}
	copies := []PreviousCopy{}
	for _, entry := range entries {
		if document, digest, ok := ParsePreviousName(entry.Name()); ok {
			copies = append(copies, PreviousCopy{document, digest})
		}
	}
	return copies, nil
}

// keepPrevious keeps the current bytes of name, when there are any and they
// differ from data, under the name PreviousName gives them. A copy already
// there is checked rather than written again.
func (d Document) keepPrevious(root *os.Root, name string, data []byte) error {
	current, err := d.ReadIn(root, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(current, data) {
		return nil
	}
	sum := sha256.Sum256(current)
	copyName := filepath.Join(filepath.Dir(name), PreviousName(filepath.Base(name), hex.EncodeToString(sum[:])))
	previous := d.Previous
	if info, err := root.Lstat(copyName); err == nil && !info.Mode().IsRegular() {
		return report(previous.Irregular, "the previous copy must be a regular file", nil, false)
	}
	kept, err := d.ReadIn(root, copyName)
	if err == nil {
		if !bytes.Equal(kept, current) {
			return report(previous.Damaged, "the previous copy is damaged; the document was not changed", nil, false)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := root.OpenFile(copyName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report(previous.Create, "cannot keep the previous copy", err, true)
	}
	writeErr := WriteFileSync(d.durable(file), current)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return report(previous.Write, "the previous copy is incomplete; the document was not changed", errors.Join(writeErr, closeErr), false)
	}
	return nil
}
