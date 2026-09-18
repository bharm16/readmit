// Package project holds the mutable metadata of an interface investigation in
// one versioned strict-JSON document beside the evidence it organizes. Evidence
// stays immutable: a project records what a case bundle already declared about
// itself — its identity, its contract version and its provenance mode — and
// adds only the metadata a person maintains, such as a title, tags, an owner, a
// status and linked incidents. Nothing here reads, verifies or writes evidence,
// so a project can never restate what a case contains; the caller verifies a
// bundle through the shared reader and passes the accepted facts in.
package project

import (
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
)

const (
	// Schema is the only project document contract this release reads. A new
	// member means a new version string and a reader for both, never an added
	// member here and never an in-place migration.
	Schema = "readmit-project/v1"

	// DocumentName is the canonical file a project directory holds.
	DocumentName = "project.json"

	// IncompleteDocumentName is the file a replacement is written to first. One
	// left behind means a previous write was interrupted; it is retained, never
	// reused, so recovery is an explicit decision outside this package.
	IncompleteDocumentName = DocumentName + incompleteSuffix

	// incompleteSuffix marks the partial file every canonical document of a
	// project directory is written to before it is renamed into place.
	incompleteSuffix = ".incomplete"

	// MaxInterfaceVersions, MaxCases, MaxTags and MaxIncidents bound one
	// document. A project past a bound is refused rather than truncated.
	MaxInterfaceVersions = 64
	MaxCases             = 256
	MaxTags              = 32
	MaxIncidents         = 32

	maxTitleBytes    = 200
	maxNameBytes     = 64
	maxDocumentBytes = 1 << 20
	identityLength   = 64
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. It is distinct from a folder holding no document
// at all, so a caller can say which one it found rather than conflating them.
// A version this release does not read is reported, never migrated in place.
var ErrUnsupportedVersion = errors.New("unsupported project document version")

// Status is the closed set of case statuses. An unknown status is refused: a
// status this release cannot interpret is never treated as any other one.
type Status string

const (
	StatusOpen          Status = "open"
	StatusInvestigating Status = "investigating"
	StatusResolved      Status = "resolved"
	StatusClosed        Status = "closed"
)

var statuses = []Status{StatusOpen, StatusInvestigating, StatusResolved, StatusClosed}

// Settings are the project-level choices every case inherits when it is
// registered without an explicit owner or interface version.
type Settings struct {
	Title                   string `json:"title"`
	DefaultOwner            string `json:"default_owner,omitzero"`
	DefaultInterfaceVersion string `json:"default_interface_version,omitzero"`
}

// Case registers one case bundle in the project.
//
// Name, Identity, Schema and Provenance are recorded from a bundle the caller
// already verified through the shared reader: they are facts about evidence and
// are never edited afterwards. Provenance is the mode the bundle's own manifest
// declares, so it can never be inferred from a file name or set by hand.
// InterfaceVersion, Title, Status, Owner, Tags and Incidents are project
// metadata a person maintains; they live here, never inside finalized evidence.
type Case struct {
	Name             string   `json:"name"`
	Identity         string   `json:"identity"`
	Schema           string   `json:"schema"`
	Provenance       string   `json:"provenance"`
	InterfaceVersion string   `json:"interface_version"`
	Title            string   `json:"title"`
	Status           Status   `json:"status"`
	Owner            string   `json:"owner,omitzero"`
	Tags             []string `json:"tags"`
	Incidents        []string `json:"incidents"`
}

// Document is the complete project document.
type Document struct {
	Schema   string   `json:"schema"`
	Settings Settings `json:"settings"`
	// InterfaceVersions are the declared versions of the interface under
	// investigation. Every case names exactly one of them, so evidence gathered
	// against different versions of the same interface stays separable.
	InterfaceVersions []string `json:"interface_versions"`
	Cases             []Case   `json:"cases"`
}

// Project is an opened project directory and the document it holds.
type Project struct {
	Root     string
	Document Document
}

// Decode reads a project document. Unknown members and unknown versions are
// errors; there is no migration and no repair.
func Decode(data []byte) (Document, error) {
	if len(data) > maxDocumentBytes {
		return Document{}, errors.New("project document exceeds its size limit")
	}
	// The declared contract version is read before the strict decode. A later
	// release bumps the version precisely because it adds members, so deciding
	// strictness first would report every such document as invalid rather than
	// as the version it plainly declares.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid project document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid project document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode writes a validated document deterministically, so the same project
// produces the same bytes on every machine.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode project document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("project document exceeds its size limit")
	}
	return data, nil
}

// Validate reports the first reason a document cannot be stored. Diagnostics
// name the member at fault and never repeat the value that failed.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := title(document.Settings.Title); err != nil {
		return errors.New("project title: " + err.Error())
	}
	if document.Settings.DefaultOwner != "" {
		if err := name(document.Settings.DefaultOwner); err != nil {
			return errors.New("project default owner: " + err.Error())
		}
	}
	if len(document.InterfaceVersions) == 0 {
		return errors.New("a project declares at least one interface version")
	}
	if len(document.InterfaceVersions) > MaxInterfaceVersions {
		return errors.New("a project declares at most " + strconv.Itoa(MaxInterfaceVersions) + " interface versions")
	}
	declared := make(map[string]bool, len(document.InterfaceVersions))
	for _, version := range document.InterfaceVersions {
		if err := name(version); err != nil {
			return errors.New("interface version: " + err.Error())
		}
		if declared[version] {
			return errors.New("an interface version is declared twice")
		}
		declared[version] = true
	}
	if version := document.Settings.DefaultInterfaceVersion; version != "" && !declared[version] {
		return errors.New("the default interface version is not declared by this project")
	}
	if len(document.Cases) > MaxCases {
		return errors.New("a project registers at most " + strconv.Itoa(MaxCases) + " cases")
	}
	names := make(map[string]bool, len(document.Cases))
	identities := make(map[string]bool, len(document.Cases))
	for _, entry := range document.Cases {
		if err := validateCase(entry, declared); err != nil {
			return err
		}
		if names[entry.Name] {
			return errors.New("a case is registered twice under the same name")
		}
		if identities[entry.Identity] {
			return errors.New("the same case identity is registered twice")
		}
		names[entry.Name] = true
		identities[entry.Identity] = true
	}
	return nil
}

func validateCase(entry Case, declared map[string]bool) error {
	if err := entryName(entry.Name); err != nil {
		return errors.New("case name: " + err.Error())
	}
	if err := identity(entry.Identity); err != nil {
		return errors.New("case identity: " + err.Error())
	}
	if err := contract(entry.Schema); err != nil {
		return errors.New("case contract version: " + err.Error())
	}
	if err := name(entry.Provenance); err != nil {
		return errors.New("case provenance mode: " + err.Error())
	}
	if err := title(entry.Title); err != nil {
		return errors.New("case title: " + err.Error())
	}
	if !slices.Contains(statuses, entry.Status) {
		return errors.New("case status: not one of open, investigating, resolved, closed")
	}
	if !declared[entry.InterfaceVersion] {
		return errors.New("case interface version: not declared by this project")
	}
	if entry.Owner != "" {
		if err := name(entry.Owner); err != nil {
			return errors.New("case owner: " + err.Error())
		}
	}
	if err := set(entry.Tags, MaxTags); err != nil {
		return errors.New("case tags: " + err.Error())
	}
	if err := set(entry.Incidents, MaxIncidents); err != nil {
		return errors.New("linked incidents: " + err.Error())
	}
	return nil
}

// set checks a bounded collection of identifiers held in sorted order with no
// duplicates, so two projects that record the same tags record the same bytes.
func set(values []string, limit int) error {
	if len(values) > limit {
		return errors.New("more entries than this release stores")
	}
	for i, value := range values {
		if err := name(value); err != nil {
			return err
		}
		if i > 0 && values[i-1] >= value {
			return errors.New("entries must be unique and sorted")
		}
	}
	return nil
}

// name is the identifier character set shared by owners, tags, incident
// references and interface version identifiers: it carries no separator, no
// whitespace and no character that changes meaning in a shell or a file name.
func name(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxNameBytes {
		return errors.New("must be at most " + strconv.Itoa(maxNameBytes) + " characters")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return errors.New("must use only letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

// contract is the character set of a declared artifact contract version, which
// carries a '/' that an identifier does not. The value is recorded exactly as
// the bundle declared it; whether this release reads that contract is settled
// by the bundle reader, not here.
func contract(value string) error {
	if value == "" || len(value) > maxNameBytes {
		return errors.New("must be between 1 and " + strconv.Itoa(maxNameBytes) + " characters")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-' || r == '/':
		default:
			return errors.New("must use only letters, digits, '.', '_', '-' and '/'")
		}
	}
	return nil
}

// entryName is one directory entry of the project, so a recorded case can never
// name a path, a parent, or an absolute location. The structural rule belongs
// to artifactpath, which enforces the same one when the entry is opened.
func entryName(value string) error {
	if err := name(value); err != nil {
		return err
	}
	if err := artifactpath.EntryName(value); err != nil {
		return errors.New("must be one directory entry of the project")
	}
	return nil
}

// title is human text. It is bounded, valid UTF-8 and free of control
// characters, so it cannot carry a line break into a rendered report.
func title(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxTitleBytes {
		return errors.New("must be at most " + strconv.Itoa(maxTitleBytes) + " bytes")
	}
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	if strings.TrimSpace(value) == "" {
		return errors.New("must not be only whitespace")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return errors.New("must not contain control characters")
		}
	}
	return nil
}

// identity is a case bundle identity exactly as the bundle reader reported it:
// the hash over relative paths and file contents that identifies that evidence.
// A project records it and never derives an identity of its own.
func identity(value string) error {
	if len(value) != identityLength {
		return errors.New("must be a " + strconv.Itoa(identityLength) + " character bundle identity")
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errors.New("must be lowercase hexadecimal")
		}
	}
	return nil
}

// AddCase registers a verified case bundle, supplying the project defaults the
// caller left unset, and returns the entry exactly as it was stored.
//
// Derived evidence is refused: it is the output of a transformation, and a
// transformation is registered with AddRevision so that its parent identity and
// operation manifest are recorded rather than lost. The editable document is
// consulted as well, so the two sides of a project stay disjoint: one name and
// one piece of evidence are held by exactly one of them. Neither document it is
// given is modified.
func AddCase(document Document, revisions Revisions, entry Case) (Document, Case, error) {
	if entry.Provenance == DerivedProvenance {
		return Document{}, Case{}, errors.New("derived evidence is a transformation; register it as a revision so its parent identity and operation are recorded")
	}
	if _, taken := registered(document, revisions, entry.Name); taken {
		return Document{}, Case{}, errors.New("that name is already registered in this project")
	}
	for _, held := range revisions.Revisions {
		if held.Identity == entry.Identity {
			return Document{}, Case{}, errors.New("that evidence is already registered as a revision of this project")
		}
	}
	if entry.InterfaceVersion == "" {
		entry.InterfaceVersion = document.Settings.DefaultInterfaceVersion
	}
	if entry.Owner == "" {
		entry.Owner = document.Settings.DefaultOwner
	}
	if entry.Status == "" {
		entry.Status = StatusOpen
	}
	entry.Tags = sorted(entry.Tags)
	entry.Incidents = sorted(entry.Incidents)
	updated := document
	updated.Cases = append(slices.Clip(slices.Clone(document.Cases)), entry)
	if err := Validate(updated); err != nil {
		return Document{}, Case{}, err
	}
	return updated, entry, nil
}

// Change is the mutable metadata one update may replace. An absent member is
// left exactly as it was, which is how an update can change a status without
// restating the tags. No member of Change can reach recorded evidence facts.
type Change struct {
	Title            *string
	Owner            *string
	Status           *Status
	InterfaceVersion *string
	Tags             *[]string
	Incidents        *[]string
}

// Empty reports whether an update would change nothing. Every member is a
// pointer, so a Change that names nothing is the zero value.
func (c Change) Empty() bool { return c == Change{} }

// UpdateCase replaces the mutable metadata of one registered case and returns
// the entry exactly as it was stored. The name, identity, contract version and
// provenance of the evidence stay as recorded. The document it is given is not
// modified.
func UpdateCase(document Document, entry string, change Change) (Document, Case, error) {
	index := slices.IndexFunc(document.Cases, func(c Case) bool { return c.Name == entry })
	if index < 0 {
		return Document{}, Case{}, errors.New("no case is registered under that name")
	}
	updated := document
	updated.Cases = slices.Clip(slices.Clone(document.Cases))
	target := updated.Cases[index]
	if change.Title != nil {
		target.Title = *change.Title
	}
	if change.Owner != nil {
		target.Owner = *change.Owner
	}
	if change.Status != nil {
		target.Status = *change.Status
	}
	if change.InterfaceVersion != nil {
		target.InterfaceVersion = *change.InterfaceVersion
	}
	if change.Tags != nil {
		target.Tags = sorted(*change.Tags)
	}
	if change.Incidents != nil {
		target.Incidents = sorted(*change.Incidents)
	}
	updated.Cases[index] = target
	if err := Validate(updated); err != nil {
		return Document{}, Case{}, err
	}
	return updated, target, nil
}

// sorted stores a set in one canonical order. Duplicates are left in place so
// validation reports them rather than silently accepting a repeated tag.
func sorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return slices.Sorted(slices.Values(values))
}

// Create writes a new project directory holding its first document. The
// destination must not exist, and artifactpath refuses one inside retained
// evidence or reached through a symbolic link.
func Create(destination string, document Document) (*Project, error) {
	data, err := Encode(document)
	if err != nil {
		return nil, err
	}
	root, err := artifactpath.Destination(destination)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, errors.New("cannot create project; destination must be new and parent writable")
	}
	if err := install(root, DocumentName, data); err != nil {
		os.Remove(root)
		return nil, err
	}
	return &Project{Root: root, Document: document}, nil
}

// Open reads the document of an existing project directory.
func Open(path string) (*Project, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return nil, errors.New("a project must be an existing directory that is not a symbolic link")
	}
	data, missing, err := readDocument(root, DocumentName)
	if err != nil {
		return nil, err
	}
	if missing {
		return nil, errors.New("directory holds no readable project document")
	}
	document, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return &Project{Root: root, Document: document}, nil
}

// readDocument reads one bounded, regular canonical document of a project
// directory. A directory that holds no such file is reported as missing rather
// than as an error, which is how a project that has recorded nothing in a
// document reads as the empty one instead of being repaired.
func readDocument(root, name string) ([]byte, bool, error) {
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, errors.New("cannot open project directory")
	}
	defer opened.Close()
	file, err := opened.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, errors.New("directory holds no readable project document")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, false, errors.New("project document must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, false, errors.New("cannot read project document")
	}
	return data, false, nil
}

// Save replaces the document atomically: it is written in full to a new file
// and renamed over the previous one, so a reader never observes a partial
// document and a failed write leaves the previous document exactly as it was.
func (p *Project) Save(document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	if err := install(p.Root, DocumentName, data); err != nil {
		return err
	}
	p.Document = document
	return nil
}

// install writes one canonical document beside the one already there and
// renames it into place. The incomplete file is the writer's own path policy
// check: it must not exist, so an interrupted write is reported rather than
// overwritten, and artifactpath refuses the whole location if the project has
// since been moved inside retained evidence.
func install(root, name string, data []byte) error {
	incomplete, err := artifactpath.Destination(filepath.Join(root, name+incompleteSuffix))
	if err != nil {
		return errors.New("cannot write the project document here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new project document; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new project document")
	}
	if err := os.Rename(incomplete, filepath.Join(filepath.Dir(incomplete), name)); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the project document")
	}
	return nil
}
