// Package backup copies one project directory into a verified backup and
// restores it somewhere else. A backup is a plain directory, exactly as the
// evidence it holds is: the stored files under one subdirectory, a versioned
// `readmit-backup/v1` manifest naming every one of them with its size and
// digest, and a completion marker written last, so a backup interrupted at any
// point is detected as incomplete rather than restored as a usable one.
//
// Nothing here repairs, substitutes or regenerates evidence. A case the project
// registers but no longer holds is recorded as missing and restored as missing;
// a case whose bytes no longer match what the project recorded is recorded as
// changed and stays changed. An incomplete account of the evidence is the
// correct answer, and a restore that quietly filled a gap would be worse than
// one that reports it.
//
// A derived index is the one thing a backup deliberately does not copy.
// [ADR-0008] makes the index a pure function of the canonical case and the
// retention an operator declared, so this package records those declarations
// and a restore builds the index again from the restored evidence. A damaged
// index is therefore a rebuild rather than a loss, and the retained patient
// data an index holds is never duplicated into a second artifact.
//
// [ADR-0008]: https://github.com/bharm16/readmit/blob/main/docs/adr/0008-the-case-index-is-a-derived-disposable-readmit-owned-file.md
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
)

const (
	// Schema is the only backup manifest contract this release reads. A new
	// member means a new version string and a reader for both, never an added
	// member here and never an in-place migration.
	Schema = "readmit-backup/v1"

	// DocumentName is the manifest of a backup directory, FilesDirectory holds
	// the stored copy of the project, and MarkerName is the completion marker
	// written after everything else. A directory without the marker was
	// interrupted and is never restored.
	DocumentName   = "backup.json"
	FilesDirectory = "files"
	MarkerName     = "identity.sha256"

	// MaxFiles, MaxFileBytes and MaxBytes bound one backup, and MaxIndexes
	// bounds how many index recipes it records. Past a bound the backup is
	// refused rather than written without the part that did not fit.
	MaxFiles     = 65536
	MaxFileBytes = bundle.MaxEvidenceBytes
	MaxBytes     = 1 << 30
	MaxIndexes   = 512

	maxDocumentBytes = 16 << 20
	maxPathBytes     = 512
	digestLength     = 64
)

// ErrUnsupportedVersion reports a manifest written under a contract version
// this release does not read. It is distinct from a directory holding no
// manifest at all, so a caller can say which one it found.
var ErrUnsupportedVersion = errors.New("unsupported backup document version")

// ErrIncomplete reports a backup that was never finished. The marker is written
// last, so its absence means the writer stopped partway; the bytes that are
// there are not a backup and are never presented as one.
var ErrIncomplete = errors.New("backup is incomplete: it carries no completion marker, so the write that produced it did not finish")

// ErrDamaged reports a backup whose contents no longer match the manifest that
// was sealed over them.
var ErrDamaged = errors.New("backup contents do not match the backup that was written")

// State is what verifying one registered artifact found. Only `verified` means
// the shared reader accepted the evidence and its identity is still the
// identity the project recorded; nothing else is treated as one.
type State string

const (
	// Verified is evidence the shared reader accepted whose identity is the
	// identity the project recorded for it.
	Verified State = "verified"
	// Changed is evidence the reader accepted that is no longer the evidence
	// the project recorded. It is never re-identified.
	Changed State = "changed"
	// Unreadable is a directory the shared reader refused: incomplete,
	// modified, or holding fewer files than it records.
	Unreadable State = "unreadable"
	// Missing is a registered artifact the project directory no longer holds.
	Missing State = "missing"
)

var states = []State{Verified, Changed, Unreadable, Missing}

// Kind separates the two things a project registers. A case is evidence in its
// own right; a revision is the output of a transformation and carries lineage.
type Kind string

const (
	CaseKind     Kind = "case"
	RevisionKind Kind = "revision"
)

var kinds = []Kind{CaseKind, RevisionKind}

// Recipe is what a backup recorded about one derived index beside the project.
type Recipe string

const (
	// Declared is an index that named a case this project registers and whose
	// retention declarations this release reads. It is the only recipe a
	// restore can build again.
	Declared Recipe = "declared"
	// Undeclared is a file declaring the index contract that this release
	// could not read. The index is derived, so there is nothing to recover:
	// what is lost is only the knowledge of which fields to build again.
	Undeclared Recipe = "undeclared"
	// Unregistered is an index describing evidence this project does not
	// register, so a restore has no canonical case to build it from.
	Unregistered Recipe = "unregistered"
)

var recipes = []Recipe{Declared, Undeclared, Unregistered}

// File is one file the backup stored, named by its path relative to the project
// root. The backup holds it under FilesDirectory at exactly that path, so a
// restore writes the same relative layout somewhere else and the bundle
// identities it contains — a hash over relative paths and contents only, never
// absolute paths — come out unchanged.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Artifact is one case or revision the project registers, and what verifying it
// found when the backup was taken. Identity is the identity the project
// recorded; a backup restates it and never derives one of its own.
type Artifact struct {
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Identity string `json:"identity"`
	State    State  `json:"state"`
}

// Index is one derived index the backup found beside the project, recorded as
// the declarations it was built under rather than copied.
//
// Fields, Retention and RetainUntil are this contract's own members, not the
// index contract's: `readmit-backup/v1` keeps its shape whatever a later index
// version declares, and a recipe this release cannot turn into a policy is
// refused rather than passed through. They are stated only for Declared;
// otherwise the field list is empty, the form is empty, and the end is null.
type Index struct {
	Name        string     `json:"name"`
	Case        string     `json:"case,omitzero"`
	Recipe      Recipe     `json:"recipe"`
	Fields      []string   `json:"fields"`
	Retention   string     `json:"retention"`
	RetainUntil *time.Time `json:"retain_until"`
}

// UnmarshalJSON requires every declaration explicitly, so an omitted one cannot
// decode into a permissive zero value, and then re-decodes rejecting unknown
// members so a misspelled declaration is an error rather than one that quietly
// did nothing. Both passes are needed: a custom unmarshaler does not inherit
// the caller's strictness. The retention end matters most: a valid record
// states it as an explicit null whenever no end was declared, and a typed
// pointer cannot tell that from an absent member, so an index that declared an
// end would otherwise be rebuilt as one retained until somebody deletes it.
func (i *Index) UnmarshalJSON(data []byte) error {
	invalid := errors.New("invalid recorded index")
	var members struct {
		Name        jsontext.Value `json:"name"`
		Case        jsontext.Value `json:"case"`
		Recipe      jsontext.Value `json:"recipe"`
		Fields      jsontext.Value `json:"fields"`
		Retention   jsontext.Value `json:"retention"`
		RetainUntil jsontext.Value `json:"retain_until"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	if len(members.Name) == 0 || len(members.Recipe) == 0 || len(members.Fields) == 0 ||
		len(members.Retention) == 0 || len(members.RetainUntil) == 0 {
		return errors.New("a recorded index states its name, its recipe, and the three declarations an index is built under")
	}
	type plainIndex Index
	var value plainIndex
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*i = Index(value)
	return nil
}

// Document is the complete manifest of one backup.
type Document struct {
	Schema   string     `json:"schema"`
	Files    []File     `json:"files"`
	Evidence []Artifact `json:"evidence"`
	Indexes  []Index    `json:"indexes"`
}

// Complete reports whether every registered artifact was verified and every
// index recorded a recipe a restore can build again. A backup of a project that
// has lost evidence is still written — it is the honest copy of what is there —
// but it never reads as a complete one.
func (d Document) Complete() bool {
	for _, entry := range d.Evidence {
		if entry.State != Verified {
			return false
		}
	}
	for _, entry := range d.Indexes {
		if !recorded(entry.Recipe).Complete() {
			return false
		}
	}
	return true
}

// Decode reads a backup manifest. Unknown members and unknown versions are
// errors; there is no migration and no repair.
func Decode(data []byte) (Document, error) {
	if len(data) > maxDocumentBytes {
		return Document{}, errors.New("backup document exceeds its size limit")
	}
	// The declared contract version is read before the strict decode. A later
	// release bumps the version precisely because it adds members, so deciding
	// strictness first would report every such document as invalid rather than
	// as the version it plainly declares.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid backup document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid backup document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode writes a validated manifest deterministically. Nothing in it is read
// from the clock or from an absolute path, so backing up the same project twice
// produces the same bytes.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode backup document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("backup document exceeds its size limit; the project holds more files than one backup records")
	}
	return data, nil
}

// Validate reports the first reason a manifest cannot be stored or restored.
// Diagnostics name the member at fault and never repeat the path that failed.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if len(document.Files) > MaxFiles {
		return errors.New("a backup stores at most " + strconv.Itoa(MaxFiles) + " files")
	}
	total := int64(0)
	for i, file := range document.Files {
		if err := storedPath(file.Path); err != nil {
			return errors.New("stored file path: " + err.Error())
		}
		if i > 0 && document.Files[i-1].Path >= file.Path {
			return errors.New("stored files must be uniquely named and sorted")
		}
		if file.Size < 0 || file.Size > MaxFileBytes {
			return errors.New("a stored file records a size within the evidence limit")
		}
		if len(file.SHA256) != digestLength || !hexadecimal(file.SHA256) {
			return errors.New("a stored file records the digest of its contents")
		}
		total += file.Size
		if total > MaxBytes {
			return errors.New("a backup stores at most " + strconv.Itoa(MaxBytes>>20) + " MiB")
		}
	}
	if err := validateEvidence(document.Evidence); err != nil {
		return err
	}
	return validateIndexes(document.Indexes)
}

func validateEvidence(entries []Artifact) error {
	if len(entries) > project.MaxCases+project.MaxRevisions {
		return errors.New("a backup records at most " + strconv.Itoa(project.MaxCases+project.MaxRevisions) + " registered artifacts")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := artifactpath.EntryName(entry.Name); err != nil {
			return errors.New("registered artifact name: must be one directory entry of the project")
		}
		if !slices.Contains(kinds, entry.Kind) {
			return errors.New("registered artifact kind: not one of case, revision")
		}
		if len(entry.Identity) != digestLength || !hexadecimal(entry.Identity) {
			return errors.New("registered artifact identity: must be the identity the project recorded")
		}
		if !slices.Contains(states, entry.State) {
			return errors.New("registered artifact state: not one of verified, changed, unreadable, missing")
		}
		if seen[entry.Name] {
			return errors.New("a registered artifact is recorded twice under the same name")
		}
		seen[entry.Name] = true
	}
	return nil
}

func validateIndexes(entries []Index) error {
	if len(entries) > MaxIndexes {
		return errors.New("a backup records at most " + strconv.Itoa(MaxIndexes) + " indexes")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := artifactpath.EntryName(entry.Name); err != nil {
			return errors.New("index name: must be one directory entry of the project")
		}
		if !slices.Contains(recipes, entry.Recipe) {
			return errors.New("index recipe: not one of declared, undeclared, unregistered")
		}
		if seen[entry.Name] {
			return errors.New("an index is recorded twice under the same name")
		}
		seen[entry.Name] = true
		if entry.Recipe != Declared {
			// An index nothing can be rebuilt from carries no declarations at
			// all, so a restore can never read a partial recipe as a whole one.
			if entry.Case != "" || len(entry.Fields) != 0 || entry.Retention != "" || entry.RetainUntil != nil {
				return errors.New("only an index recorded as declared carries the declarations it was built under")
			}
			continue
		}
		if err := artifactpath.EntryName(entry.Case); err != nil {
			return errors.New("index case: must be one directory entry of the project")
		}
		if _, err := entry.Policy(); err != nil {
			return err
		}
	}
	return nil
}

// Policy turns the declarations a backup recorded back into the retention
// policy the index was built under. A recipe this release cannot read is an
// error rather than a narrower policy silently taking its place.
func (i Index) Policy() (index.Policy, error) {
	policy := index.Policy{Fields: slices.Clone(i.Fields), Retention: index.Retention(i.Retention)}
	if i.RetainUntil != nil {
		instant := *i.RetainUntil
		policy.RetainUntil = &instant
	}
	if err := index.ValidatePolicy(policy); err != nil {
		return index.Policy{}, err
	}
	return policy, nil
}

// storedPath checks one path a backup records: relative to the project root,
// slash-separated, and every element one entry name, so a stored file can never
// name an absolute location, a parent directory, or a reserved device name.
func storedPath(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxPathBytes {
		return errors.New("must be at most " + strconv.Itoa(maxPathBytes) + " bytes")
	}
	if value == "." || !fs.ValidPath(value) {
		return errors.New("must be a relative path inside the project")
	}
	for element := range strings.SplitSeq(value, "/") {
		if err := artifactpath.EntryName(element); err != nil {
			return errors.New("must be a relative path inside the project")
		}
	}
	return nil
}

// identityFor seals one backup. The manifest already carries the size and
// digest of every stored file, so a digest over the manifest bytes covers the
// whole backup, and the marker holding it is written after every other file.
// It detects damage and interruption, not forgery: anyone who can rewrite the
// files can recompute it, exactly as ADR-0004 says a hash is not source
// authentication.
func identityFor(document []byte) string {
	sum := sha256.New()
	sum.Write([]byte(Schema + "\n"))
	sum.Write(document)
	return hex.EncodeToString(sum.Sum(nil))
}

// position names one entry of a list by its place in it. Diagnostics carry a
// position rather than a path, so a failure can be located without a message
// repeating the name of a file somebody kept evidence in.
func position(i int) string { return strconv.Itoa(i + 1) }

// hexadecimal accepts the lowercase form every readmit digest is written in, so
// one digest cannot be recorded under two different spellings.
func hexadecimal(value string) bool {
	for i := range len(value) {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
