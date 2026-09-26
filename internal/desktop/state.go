package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/operationguard"
)

// This file holds what the shell's seven local documents share: saved
// filters, working session, editor drafts, remembered
// projects, and operation, commercial and hub selections. One store owns
// the folder they live in, the file names they are kept under, the one rule
// they are read by, and the one way any of them is replaced. Each document supplies only its contract
// version and its decoder. replaceDocument, beneath the store, is also how a
// workspace save writes over an entry it may overwrite.

// ShellDocuments is the store of the shell's seven local documents. They are
// separate owner-only files that live beside each other in one folder, each
// named by the store and none derived from another, and none of them is
// evidence: no case, run, result, review or report ever holds one.
type ShellDocuments struct {
	// Folder is the owner-only directory every document lives in.
	Folder string
}

// The seven file names. The store owns them: nothing the shell is wired up
// with names a document, only the folder they all live in. An earlier
// release's recent-folder list, recent.json, may still be there; nothing
// reads or writes it.
const (
	filtersName             = "filters.json"
	sessionName             = "session.json"
	draftsName              = "drafts.json"
	operationSelectionName  = "operations.json"
	commercialSelectionName = "commercial.json"
	hubSelectionName        = "hub.json"
	projectsName            = "projects.json"
)

// DefaultShellDocuments is the store over this account's own configuration
// folder, where the shell keeps its documents beside each other.
func DefaultShellDocuments() (ShellDocuments, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return ShellDocuments{}, errors.New("cannot resolve the user configuration directory")
	}
	return ShellDocuments{Folder: filepath.Join(directory, "readmit")}, nil
}

// path is one document of the store, by its fixed name.
func (s ShellDocuments) path(name string) string { return filepath.Join(s.Folder, name) }

// errNotADocument reports an entry at a document's name that is not a bounded
// regular file: a symbolic link, a folder, or contents past the document's
// bound. It is distinct from a document that decodes and refuses, so a caller
// can say the file itself is not one this release reads.
var errNotADocument = errors.New("the document must be a bounded regular file")

// read reads the named document whole. This is the one reading rule every
// shell document is held to: the name must hold a regular file within the
// document's bound, and a symbolic link at the name is refused rather than
// followed, so nothing beside the shell's own files is read into them. A
// document that is not there is reported as the filesystem reports it, so a
// caller can treat its absence as an empty one.
func (s ShellDocuments) read(name string, maxBytes int) ([]byte, error) {
	document := artifactdir.Document{
		MaxBytes: maxBytes,
		Refusals: artifactdir.DocumentRefusals{Irregular: errNotADocument, Size: errNotADocument},
	}
	return document.Read(s.path(name))
}

// write installs a complete document or leaves the previous one in place,
// creating the folder that holds the shell's documents if it is not there
// yet, through replaceDocument: the shared document store. A reader never
// observes a partial document, and a failed write keeps exactly what was
// there before.
//
// The diagnostics it returns are never shown: every caller reports its own
// fixed sentence for the document it was writing, and reads only the error
// class to separate a file this account cannot write from any other failure.
func (s ShellDocuments) write(name string, data []byte) error {
	if err := os.MkdirAll(s.Folder, 0700); err != nil {
		return err
	}
	return replaceDocument(s.path(name), data)
}

// replaceDocument installs a complete document or leaves the previous one in
// place, through the shared document store, so a reader never observes a
// partial one and a failed write keeps exactly what was there before.
// ShellDocuments.write and every workspace save that may overwrite its entry
// write through it.
//
// It never opens the entry it replaces. The bytes are written in full to a new
// owner-only file beside it and renamed onto the name, and a rename replaces
// the entry itself, so a symbolic link at the name is replaced rather than
// written through to whatever it points at, and so are a hard link and a FIFO.
// A folder at the name is refused. The store refuses a write inside retained
// case, run, result, review or report evidence. The incomplete file must not
// already exist, so an interrupted write is reported rather than overwritten,
// and a link planted at its name is refused rather than followed. The
// document survives a power loss only once the folder naming it is synced
// too; every caller reports a document as retained only when this returns, so
// the store syncs it before then.
//
// A failure to create the incomplete file is returned as the filesystem
// reported it, so a caller can tell a file this account cannot write, or one
// already there, from any other failure. Every other diagnostic is a fixed
// sentence naming no path.
func replaceDocument(path string, data []byte) error {
	return shellDocument.Replace(path, data)
}

// shellDocument is how the shell's local documents, and a workspace entry a
// save may overwrite, are replaced.
var shellDocument = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the document here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write the document"),
		Install:     errors.New("cannot replace the document"),
		Sync:        errors.New("cannot confirm the document was retained durably"),
	},
}

// maxSelectionBytes bounds a remembered selection, the same bound the
// operation guard reads its own control documents within.
const maxSelectionBytes = operationguard.MaxDocumentBytes

// rememberedSelections are the three shell documents that remember what a
// person selected: the operation policy, the commercial destinations and the
// customer hub configuration. They are one document, rememberedSelection,
// three times over, differing only in file name, contract version and the
// member the selected path is written under. A shell either remembers all
// three or none: the constructor that restores them wires them, and a window
// wired without them keeps every selection in memory alone.
type rememberedSelections struct {
	operation  rememberedSelection
	commercial rememberedSelection
	hub        rememberedSelection
}

// selections is the shell's three remembered selections over this store.
func (s ShellDocuments) selections() *rememberedSelections {
	return &rememberedSelections{
		operation:  rememberedSelection{documents: s, name: operationSelectionName, schema: operationSelectionSchema, member: "policy"},
		commercial: rememberedSelection{documents: s, name: commercialSelectionName, schema: commercialSelectionSchema, member: "config"},
		hub:        rememberedSelection{documents: s, name: hubSelectionName, schema: hubSelectionSchema, member: "config"},
	}
}

// rememberedSelection is the shell document that remembers one path a person
// selected: the declared contract version and that one path, and nothing
// else. Reading is the store's bounded read, and a document that is there but
// is not a selection this release reads is reported to the person, never
// replaced: the next selection is the person's own act.
type rememberedSelection struct {
	documents ShellDocuments
	name      string
	schema    string
	// member is the JSON member the selected path is written under, after
	// schema. The bytes each selection writes are the two members and a
	// newline, in that order, and nothing else.
	member string
}

// recall answers the path a previous window selected: the empty path when
// there is no document, and the path when the document is a selection this
// release reads. Every other refusal — a document that is not a bounded
// regular file, or not this contract — is returned for the caller to report.
func (s rememberedSelection) recall() (string, error) {
	data, err := s.documents.read(s.name, maxSelectionBytes)
	if err != nil {
		return "", err
	}
	return s.decode(data)
}

// Remember keeps path for the next window, replacing the document whole.
func (s rememberedSelection) Remember(path string) error {
	data, err := s.encode(path)
	if err != nil {
		return err
	}
	return s.documents.write(s.name, data)
}

// encode writes the document: its two members and a newline. The member names
// are fixed words of the contract and the path is quoted as JSON quotes a
// string, so the bytes are exactly the two members in contract order.
func (s rememberedSelection) encode(path string) ([]byte, error) {
	data := make([]byte, 0, len(s.schema)+len(s.member)+len(path)+32)
	data = append(data, `{"schema":`...)
	quoted, err := jsontext.AppendQuote(data, s.schema)
	if err != nil {
		return nil, err
	}
	if quoted, err = jsontext.AppendQuote(append(quoted, ','), s.member); err != nil {
		return nil, err
	}
	if quoted, err = jsontext.AppendQuote(append(quoted, ':'), path); err != nil {
		return nil, err
	}
	return append(quoted, '}', '\n'), nil
}

// decode reads one remembered selection: the declared contract version and
// the absolute path its member names, and nothing else. Unknown members,
// another version, or a path that is not absolute are all one refusal; there
// is no migration and no repair, so a selection this release cannot read
// stays exactly as it was written.
func (s rememberedSelection) decode(data []byte) (string, error) {
	var members map[string]string
	if err := json.Unmarshal(data, &members); err != nil {
		return "", errInvalidSelection
	}
	if len(members) != 2 || members["schema"] != s.schema {
		return "", errInvalidSelection
	}
	path, held := members[s.member]
	if !held || !filepath.IsAbs(path) {
		return "", errInvalidSelection
	}
	return path, nil
}

// Recall is recall with the absence of the document answered as nothing
// remembered. It is how the hub connection keeps its selection in the store.
func (s rememberedSelection) Recall() (string, error) {
	path, err := s.recall()
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return path, err
}

// errInvalidSelection reports a document at a selection's name that is not a
// selection this release reads.
var errInvalidSelection = errors.New("the document is not a selection this release reads")

// printable accepts bounded text a person typed or a path they opened. It is
// the rule internal/grid holds a saved filter's text to, for the same reason:
// control characters are refused rather than escaped, so nothing stored here
// can rewrite a terminal or a rendered line when it is displayed back.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
