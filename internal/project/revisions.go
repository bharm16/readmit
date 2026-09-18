package project

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
)

const (
	// RevisionsSchema is the only editable-document contract this release
	// reads. A new member means a new version string and a reader for both,
	// never an added member here and never an in-place migration.
	RevisionsSchema = "readmit-revisions/v1"

	// RevisionsDocumentName is the canonical editable document of a project
	// directory. It sits beside project.json and beside evidence, never inside
	// evidence: everything a person may edit lives here, and nothing here is
	// evidence.
	RevisionsDocumentName = "revisions.json"

	// RevisionsIncompleteDocumentName is the file a replacement is written to
	// first. One left behind means a previous write was interrupted; it is
	// retained, never reused.
	RevisionsIncompleteDocumentName = RevisionsDocumentName + incompleteSuffix

	// MaxNotes and MaxRevisions bound one editable document. A document past a
	// bound is refused rather than truncated.
	MaxNotes     = 128
	MaxRevisions = 256

	// DerivedProvenance is the provenance mode of evidence that is the output
	// of a transformation. A registered revision declares it, so lineage is
	// recorded only for evidence that says it was transformed.
	DerivedProvenance = "derived"

	maxBodyBytes = 4096
)

// Operation is the manifest of the transformation that produced one revision.
//
// Name is the derivation contract the derived evidence declares in its own
// manifest, so the operation is read from the artifact and is never typed on a
// command line or inferred from a name. Parent is the case or revision the
// transformation consumed, and ParentIdentity is the identity that parent was
// registered with, so a revision keeps naming the exact evidence it came from
// even after the parent directory is replaced, moved, or removed.
type Operation struct {
	Name           string `json:"name"`
	Parent         string `json:"parent"`
	ParentIdentity string `json:"parent_identity"`
}

// Revision is one derived case bundle registered with its lineage.
//
// Name, Identity, Schema and Provenance are recorded from a bundle the caller
// already verified through the shared reader, exactly as a Case is. Provenance
// is always the derived mode: evidence that is not the output of a
// transformation is registered as a case, never as a revision of one.
type Revision struct {
	Name       string    `json:"name"`
	Identity   string    `json:"identity"`
	Schema     string    `json:"schema"`
	Provenance string    `json:"provenance"`
	Operation  Operation `json:"operation"`
}

// Note is editable working text: an observation, a draft, a conclusion a person
// is still writing. A note with no Subject is a project draft; a note with one
// is about the registered case or revision of that name. Replacing a note
// replaces only that note, and no note can reach evidence.
type Note struct {
	Name    string `json:"name"`
	Subject string `json:"subject,omitzero"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// Revisions is the editable document of a project: the notes and drafts a
// person maintains, and the lineage of every revision derived from registered
// evidence. Evidence itself is never written here and is never written by an
// edit of this document.
type Revisions struct {
	Schema    string     `json:"schema"`
	Notes     []Note     `json:"notes"`
	Revisions []Revision `json:"revisions"`
}

// registered reports the recorded identity of the case or revision one project
// holds under name. It is what makes a parent reference and a note subject name
// evidence the project actually registers rather than any directory entry.
func registered(document Document, revisions Revisions, name string) (string, bool) {
	if i := slices.IndexFunc(document.Cases, func(entry Case) bool { return entry.Name == name }); i >= 0 {
		return document.Cases[i].Identity, true
	}
	if i := slices.IndexFunc(revisions.Revisions, func(entry Revision) bool { return entry.Name == name }); i >= 0 {
		return revisions.Revisions[i].Identity, true
	}
	return "", false
}

// DecodeRevisions reads an editable document. Unknown members and unknown
// versions are errors; there is no migration and no repair.
func DecodeRevisions(data []byte) (Revisions, error) {
	if len(data) > maxDocumentBytes {
		return Revisions{}, errors.New("project revision document exceeds its size limit")
	}
	// The declared contract version is read before the strict decode, for the
	// same reason the project document reads it first: a later release bumps
	// the version precisely because it adds members.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Revisions{}, errors.New("invalid project revision document")
	}
	if declared.Schema != RevisionsSchema {
		return Revisions{}, ErrUnsupportedVersion
	}
	var revisions Revisions
	if err := json.Unmarshal(data, &revisions, json.RejectUnknownMembers(true)); err != nil {
		return Revisions{}, errors.New("invalid project revision document")
	}
	if err := ValidateRevisions(revisions); err != nil {
		return Revisions{}, err
	}
	return revisions, nil
}

// EncodeRevisions writes a validated document deterministically, so the same
// project produces the same bytes on every machine.
func EncodeRevisions(revisions Revisions) ([]byte, error) {
	if err := ValidateRevisions(revisions); err != nil {
		return nil, err
	}
	data, err := json.Marshal(revisions, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode project revision document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("project revision document exceeds its size limit")
	}
	return data, nil
}

// ValidateRevisions reports the first reason an editable document cannot be
// stored. It checks the shape of the document alone: whether a parent or a
// subject names evidence this project registers is settled by AddRevision and
// SetNote, which are given the project document as well.
func ValidateRevisions(revisions Revisions) error {
	if revisions.Schema != RevisionsSchema {
		return ErrUnsupportedVersion
	}
	if len(revisions.Notes) > MaxNotes {
		return errors.New("a project holds at most " + strconv.Itoa(MaxNotes) + " notes")
	}
	for i, note := range revisions.Notes {
		if err := ValidateNote(note); err != nil {
			return err
		}
		if i > 0 && revisions.Notes[i-1].Name >= note.Name {
			return errors.New("notes must be uniquely named and sorted")
		}
	}
	if len(revisions.Revisions) > MaxRevisions {
		return errors.New("a project registers at most " + strconv.Itoa(MaxRevisions) + " revisions")
	}
	names := make(map[string]bool, len(revisions.Revisions))
	identities := make(map[string]bool, len(revisions.Revisions))
	for _, entry := range revisions.Revisions {
		if err := validateRevision(entry); err != nil {
			return err
		}
		if names[entry.Name] {
			return errors.New("a revision is registered twice under the same name")
		}
		if identities[entry.Identity] {
			return errors.New("the same revision identity is registered twice")
		}
		names[entry.Name] = true
		identities[entry.Identity] = true
	}
	return nil
}

// ValidateNote reports the first reason one note cannot be stored, checking the
// note by itself: whether a subject names evidence a project registers is
// settled by SetNote, which is given the project document as well.
//
// It is exported for an edit that is not stored yet. The desktop shell retains
// an unfinished note so an interruption does not lose it, and holds that draft
// to exactly this rule, so working text it keeps is working text this project
// can accept. A draft is still not a note: it names no subject this project has
// been checked against, and it is not in the project document until SetNote
// puts it there.
func ValidateNote(note Note) error {
	if err := name(note.Name); err != nil {
		return errors.New("note name: " + err.Error())
	}
	if note.Subject != "" {
		if err := entryName(note.Subject); err != nil {
			return errors.New("note subject: " + err.Error())
		}
	}
	if err := title(note.Title); err != nil {
		return errors.New("note title: " + err.Error())
	}
	if err := body(note.Body); err != nil {
		return errors.New("note body: " + err.Error())
	}
	return nil
}

func validateRevision(entry Revision) error {
	if err := entryName(entry.Name); err != nil {
		return errors.New("revision name: " + err.Error())
	}
	if err := identity(entry.Identity); err != nil {
		return errors.New("revision identity: " + err.Error())
	}
	if err := contract(entry.Schema); err != nil {
		return errors.New("revision contract version: " + err.Error())
	}
	if entry.Provenance != DerivedProvenance {
		return errors.New("revision provenance mode: a revision is derived evidence")
	}
	if err := contract(entry.Operation.Name); err != nil {
		return errors.New("operation: " + err.Error())
	}
	if err := entryName(entry.Operation.Parent); err != nil {
		return errors.New("operation parent: " + err.Error())
	}
	if err := identity(entry.Operation.ParentIdentity); err != nil {
		return errors.New("operation parent identity: " + err.Error())
	}
	if entry.Operation.Parent == entry.Name {
		return errors.New("operation parent: a revision cannot be derived from itself")
	}
	return nil
}

// body is editable working text. It is bounded, valid UTF-8, and its only
// control character is a line feed, so a note cannot carry a terminal escape or
// a carriage return into a rendered report. It may be empty while a note is
// still a title and nothing else.
func body(value string) error {
	if len(value) > maxBodyBytes {
		return errors.New("must be at most " + strconv.Itoa(maxBodyBytes) + " bytes")
	}
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	for _, r := range value {
		if r != '\n' && (r < 0x20 || r == 0x7f) {
			return errors.New("must not contain control characters other than a line feed")
		}
	}
	return nil
}

// SetNote creates or replaces one note and returns it exactly as it was stored.
// A note that names a subject must name a case or a revision this project
// registers, so working text is always attached to evidence that exists; a note
// with no subject is a draft of the project itself. Neither document it is
// given is modified, and nothing it stores can reach evidence.
func SetNote(document Document, revisions Revisions, note Note) (Revisions, Note, error) {
	if err := ValidateNote(note); err != nil {
		return Revisions{}, Note{}, err
	}
	if note.Subject != "" {
		if _, ok := registered(document, revisions, note.Subject); !ok {
			return Revisions{}, Note{}, errors.New("a note subject must be a case or revision this project registers")
		}
	}
	updated := revisions
	updated.Notes = slices.Clip(slices.Clone(revisions.Notes))
	index, found := slices.BinarySearchFunc(updated.Notes, note, func(a, b Note) int { return strings.Compare(a.Name, b.Name) })
	if found {
		updated.Notes[index] = note
	} else {
		updated.Notes = slices.Insert(updated.Notes, index, note)
	}
	if err := ValidateRevisions(updated); err != nil {
		return Revisions{}, Note{}, err
	}
	return updated, note, nil
}

// AddRevision registers one derived case bundle with its lineage and returns
// the entry exactly as it was stored.
//
// The caller supplies facts a verified bundle declared: the revision's own
// identity, contract version and provenance mode, the operation its manifest
// names, and the identity the shared reader reported for the parent. The parent
// must already be registered here, and the identity the caller verified must be
// the identity this project recorded for it, so lineage is never recorded onto
// evidence that has since been replaced. Neither document it is given is
// modified, and no byte of any bundle is read or written.
func AddRevision(document Document, revisions Revisions, entry Revision) (Revisions, Revision, error) {
	if err := validateRevision(entry); err != nil {
		return Revisions{}, Revision{}, err
	}
	if _, taken := registered(document, revisions, entry.Name); taken {
		return Revisions{}, Revision{}, errors.New("that name is already registered in this project")
	}
	for _, held := range document.Cases {
		if held.Identity == entry.Identity {
			return Revisions{}, Revision{}, errors.New("that evidence is already registered as a case of this project")
		}
	}
	recorded, ok := registered(document, revisions, entry.Operation.Parent)
	if !ok {
		return Revisions{}, Revision{}, errors.New("a revision must be derived from a case or revision this project registers")
	}
	if recorded != entry.Operation.ParentIdentity {
		return Revisions{}, Revision{}, errors.New("the parent is no longer the evidence this project recorded; its identity changed")
	}
	updated := revisions
	updated.Revisions = append(slices.Clip(slices.Clone(revisions.Revisions)), entry)
	if err := ValidateRevisions(updated); err != nil {
		return Revisions{}, Revision{}, err
	}
	return updated, entry, nil
}

// ReadRevisions reads the editable document of a project directory. A project
// that has recorded no note and no revision yet holds no such document and
// reads as the empty one, so reading never writes and never repairs. It takes a
// directory rather than an opened project because a caller that is only listing
// a folder has no reason to read the project document first.
func ReadRevisions(path string) (Revisions, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return Revisions{}, errors.New("a project must be an existing directory that is not a symbolic link")
	}
	data, missing, err := readDocument(root, RevisionsDocumentName)
	if err != nil {
		return Revisions{}, err
	}
	if missing {
		return Revisions{Schema: RevisionsSchema}, nil
	}
	return DecodeRevisions(data)
}

// WriteRevisions replaces the editable document atomically, exactly as the
// project document is replaced: written in full beside the previous one and
// renamed over it, so a reader never observes a partial document and a failed
// write leaves the previous one exactly as it was. It writes only this
// document; no evidence is opened, moved, or rewritten.
func WriteRevisions(root string, revisions Revisions) error {
	data, err := EncodeRevisions(revisions)
	if err != nil {
		return err
	}
	return install(root, RevisionsDocumentName, data)
}
