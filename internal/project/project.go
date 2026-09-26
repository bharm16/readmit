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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const (
	// Schema is the first project document contract. A v1 project declares
	// at least one interface version and every case names one of them. It is
	// read and written exactly as it always was; a new member means a new
	// version string and a reader for both, never an added member here and
	// never a conversion a reader makes. The one conversion is Migrate, which
	// only a person's explicit request runs (ADR-0003, 2026-09-26).
	Schema = "readmit-project/v1"

	// SchemaV2 is the project a person creates by naming it: the same members
	// as v1, except that it may declare no interface version yet and a case
	// may leave its interface version unassigned. No version is invented for
	// either. A v1 document becomes v2 only through Migrate, an explicit act;
	// reading one never converts it.
	SchemaV2 = "readmit-project/v2"

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

	maxTitleBytes     = 200
	maxTitleRunes     = 200
	maxTitleRuneBytes = 800
	maxOwnerRunes     = 100
	maxLabelRunes     = 64
	maxNameBytes      = 64
	maxDocumentBytes  = 1 << 20
	identityLength    = 64
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. It is distinct from a folder holding no document
// at all, so a caller can say which one it found rather than conflating them.
// A version this release does not read is reported, never migrated in place.
var ErrUnsupportedVersion = errors.New("unsupported project document version")

// ErrAlreadyCurrent reports a migration of a document that already declares
// the current contract. Nothing is rewritten.
var ErrAlreadyCurrent = errors.New("the project document already declares the current contract")

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
	// Tags are the project's own tags. Only a readmit-project/v2 document
	// holds them.
	Tags []string `json:"tags,omitzero"`
}

// VersionName is the name a person gave one declared interface version. The
// version itself stays the identifier every case records; renaming it changes
// only this name. Only a readmit-project/v2 document holds names, and a
// version without one is named by its identifier.
type VersionName struct {
	Version string `json:"version"`
	Name    string `json:"name"`
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
	// InterfaceVersionNames name declared versions, sorted by version. v2
	// only.
	InterfaceVersionNames []VersionName `json:"interface_version_names,omitzero"`
	Cases                 []Case        `json:"cases"`
}

// Project is an opened project directory and the document it holds.
type Project struct {
	Root     string
	Document Document
}

// projectDocuments are the strict readings of a project document, one per
// contract this release reads, through strictdoc: the declared contract
// version is read before the strict decode, so a later version reads as the
// version it declares, never as invalid.
var projectDocuments = []strictdoc.Document{
	{
		MaxBytes:    maxDocumentBytes,
		Schema:      Schema,
		Invalid:     "invalid project document",
		TooLarge:    "project document exceeds its size limit",
		MustDeclare: "a project document declares its contract version",
		Unsupported: ErrUnsupportedVersion,
	},
	{
		MaxBytes:    maxDocumentBytes,
		Schema:      SchemaV2,
		Invalid:     "invalid project document",
		TooLarge:    "project document exceeds its size limit",
		MustDeclare: "a project document declares its contract version",
		Unsupported: ErrUnsupportedVersion,
	},
}

// Decode reads a project document of either contract this release reads,
// as the version it declares. Unknown members and unknown versions are
// errors; nothing is migrated or repaired while reading.
func Decode(data []byte) (Document, error) {
	var decoded Document
	var err error
	for _, reading := range projectDocuments {
		decoded = Document{}
		if err = reading.Decode(data, &decoded); !errors.Is(err, ErrUnsupportedVersion) {
			break
		}
	}
	if err != nil {
		return Document{}, err
	}
	if err := Validate(decoded); err != nil {
		return Document{}, err
	}
	return decoded, nil
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
	if document.Schema != Schema && document.Schema != SchemaV2 {
		return ErrUnsupportedVersion
	}
	named := document.Schema == SchemaV2
	if err := titleOf(document.Settings.Title, named); err != nil {
		return errors.New("project title: " + err.Error())
	}
	if document.Settings.DefaultOwner != "" {
		if err := owner(document.Settings.DefaultOwner, named); err != nil {
			return errors.New("project default owner: " + err.Error())
		}
	}
	if len(document.InterfaceVersions) == 0 && !named {
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
	if !named && (len(document.Settings.Tags) > 0 || len(document.InterfaceVersionNames) > 0) {
		return errors.New("project tags and interface version names are members of readmit-project/v2")
	}
	if err := set(document.Settings.Tags, MaxTags, named); err != nil {
		return errors.New("project tags: " + err.Error())
	}
	for i, version := range document.InterfaceVersionNames {
		if !declared[version.Version] {
			return errors.New("interface version name: names a version this project does not declare")
		}
		if i > 0 && document.InterfaceVersionNames[i-1].Version >= version.Version {
			return errors.New("interface version names must name each version once, sorted by version")
		}
		if err := titleOf(version.Name, true); err != nil {
			return errors.New("interface version name: " + err.Error())
		}
	}
	if len(document.Cases) > MaxCases {
		return errors.New("a project registers at most " + strconv.Itoa(MaxCases) + " cases")
	}
	names := make(map[string]bool, len(document.Cases))
	identities := make(map[string]bool, len(document.Cases))
	for _, entry := range document.Cases {
		if err := validateCase(entry, declared, named); err != nil {
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

func validateCase(entry Case, declared map[string]bool, unassignable bool) error {
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
	if err := titleOf(entry.Title, unassignable); err != nil {
		return errors.New("case title: " + err.Error())
	}
	if !slices.Contains(statuses, entry.Status) {
		return errors.New("case status: not one of open, investigating, resolved, closed")
	}
	if !declared[entry.InterfaceVersion] && (entry.InterfaceVersion != "" || !unassignable) {
		return errors.New("case interface version: not declared by this project")
	}
	if entry.Owner != "" {
		if err := owner(entry.Owner, unassignable); err != nil {
			return errors.New("case owner: " + err.Error())
		}
	}
	if err := set(entry.Tags, MaxTags, unassignable); err != nil {
		return errors.New("case tags: " + err.Error())
	}
	if err := set(entry.Incidents, MaxIncidents, unassignable); err != nil {
		return errors.New("linked incidents: " + err.Error())
	}
	return nil
}

// set checks a bounded collection held in sorted order with no duplicates,
// so two projects that record the same tags record the same bytes. A v1
// project's entries are identifiers; a v2 project's are labels.
func set(values []string, limit int, v2 bool) error {
	if len(values) > limit {
		return errors.New("more entries than this release stores")
	}
	for i, value := range values {
		check := name
		if v2 {
			check = func(value string) error { return label(value, maxLabelRunes) }
		}
		if err := check(value); err != nil {
			return err
		}
		if i > 0 && values[i-1] >= value {
			return errors.New("entries must be unique and sorted")
		}
	}
	return nil
}

// owner is a v1 owner's identifier, or a v2 owner's label.
func owner(value string, v2 bool) error {
	if v2 {
		return label(value, maxOwnerRunes)
	}
	return name(value)
}

// label is a readmit-project/v2 owner, tag or incident reference: printable
// Unicode text a person typed, such as "Integration team", stored exactly as
// entered. It is bounded in characters, carries no control character, and has
// no leading or trailing space, so it cannot break a rendered line.
func label(value string, limit int) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	if strings.TrimSpace(value) != value {
		return errors.New("must not begin or end with a space")
	}
	if utf8.RuneCountInString(value) > limit {
		return errors.New("must be at most " + strconv.Itoa(limit) + " characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("must not contain control characters")
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

// titleOf is the title rule of a document: v1's bytes, or v2's characters.
func titleOf(value string, v2 bool) error {
	if v2 {
		return titleRunes(value)
	}
	return title(value)
}

// titleRunes is a readmit-project/v2 title or name: 1 to 200 characters of
// printable text, and at most 800 bytes, so a name in any script has the same
// room a Latin one has.
func titleRunes(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > maxTitleRuneBytes || !utf8.ValidString(value) {
		return errors.New("must be at most " + strconv.Itoa(maxTitleRunes) + " characters of valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxTitleRunes {
		return errors.New("must be at most " + strconv.Itoa(maxTitleRunes) + " characters")
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
	data, err := canonicalFile.ReadIn(opened, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
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

// WriteDocument replaces the document of an already-resolved project directory
// atomically, exactly as Save does, for a caller that composed the new document
// itself rather than through an opened project. It writes only this document;
// no evidence is opened, moved, or rewritten.
func WriteDocument(root string, document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	return install(root, DocumentName, data)
}

// Migrate returns a v1 document as the v2 document it becomes when a person
// explicitly converts the project: every member is kept exactly, and only
// the declared contract changes. It writes nothing; the caller stores the
// result through the usual atomic replacement, which retains the v1 bytes as
// a recovery copy. A document that already declares v2 is refused rather
// than rewritten.
func Migrate(document Document) (Document, error) {
	switch document.Schema {
	case SchemaV2:
		return Document{}, ErrAlreadyCurrent
	case Schema:
	default:
		return Document{}, ErrUnsupportedVersion
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	migrated := document
	migrated.Schema = SchemaV2
	return migrated, nil
}

// VersionNamed is the name of one declared interface version: the name a
// person gave it, or its identifier when it has none.
func (d Document) VersionNamed(version string) string {
	for _, named := range d.InterfaceVersionNames {
		if named.Version == version {
			return named.Name
		}
	}
	return version
}

// RemoveCase unregisters one case and returns the document without it. The
// evidence is not touched. A case that a note or a registered revision of
// the project names is refused, because unregistering it would leave that
// note or that lineage naming evidence the project no longer registers.
func RemoveCase(document Document, revisions Revisions, entry string) (Document, error) {
	index := slices.IndexFunc(document.Cases, func(c Case) bool { return c.Name == entry })
	if index < 0 {
		return Document{}, errors.New("no case is registered under that name")
	}
	if slices.ContainsFunc(revisions.Notes, func(note Note) bool { return note.Subject == entry }) {
		return Document{}, ErrCaseNamed
	}
	if slices.ContainsFunc(revisions.Revisions, func(revision Revision) bool { return revision.Operation.Parent == entry }) {
		return Document{}, ErrCaseNamed
	}
	updated := document
	updated.Cases = slices.Delete(slices.Clone(document.Cases), index, index+1)
	if err := Validate(updated); err != nil {
		return Document{}, err
	}
	return updated, nil
}

// ErrCaseNamed reports a case a note or a registered revision names.
var ErrCaseNamed = errors.New("a note or a registered revision of this project names that case")

// CheckIdentifier is the identifier rule interface versions, and a v1
// project's owners, tags and incident references, are held to, exported for
// an editor that reports each member's problem at that member.
func CheckIdentifier(value string) error { return name(value) }

// CheckOwner is the owner rule of a document of schema: an identifier in v1,
// a label of at most 100 characters in v2.
func CheckOwner(schema, value string) error { return owner(value, schema == SchemaV2) }

// CheckTitle is the title rule of a project, a case and a version name in a
// document of schema: 200 bytes in v1, 200 characters in v2.
func CheckTitle(schema, value string) error { return titleOf(value, schema == SchemaV2) }

// CheckTags is the rule a case's or a project's tags are held to in a
// document of schema, once sorted.
func CheckTags(schema string, values []string) error { return set(values, MaxTags, schema == SchemaV2) }

// CheckIncidents is the rule a case's linked incidents are held to in a
// document of schema, once sorted.
func CheckIncidents(schema string, values []string) error {
	return set(values, MaxIncidents, schema == SchemaV2)
}

// Declares reports whether one interface version is declared. Every case names
// exactly one declared version, so a setting or a registration can only ever
// name one of these.
func (d Document) Declares(version string) bool {
	return slices.Contains(d.InterfaceVersions, version)
}

// install writes one canonical document beside the one already there and
// renames it into place through the shared document store, keeping the bytes
// it replaces as a recovery copy first. The incomplete file must not exist, so
// an interrupted write is reported rather than overwritten, and artifactpath
// refuses the whole location if the project has since been moved inside
// retained evidence.
func install(root, name string, data []byte) error {
	return installWithQuota(root, name, data, true)
}

func installWithQuota(root, name string, data []byte, check bool) error {
	physical, err := artifactpath.Directory(root)
	if err != nil {
		return err
	}
	root = physical
	if check {
		if err := enforceQuota(root, name, data); err != nil {
			return err
		}
	}
	return canonicalFile.Replace(filepath.Join(root, name), data)
}

// canonicalFile is how every canonical document of a project directory is
// written and read: project, revisions and quota alike. A read follows a link
// only within the project directory. Every replacement retains the exact
// preceding bytes as a recovery copy, named by their digest, before it
// replaces them; copies are never overwritten, including damaged ones.
var canonicalFile = artifactdir.Document{
	MaxBytes: maxDocumentBytes,
	Links:    artifactdir.FollowLinks,
	Previous: &artifactdir.Previous{
		Irregular: errors.New("recovery copy must be a regular file"),
		Damaged:   errors.New("recovery copy is damaged; current document was not changed"),
		Create:    errors.New("cannot retain recovery copy"),
		Write:     errors.New("recovery copy incomplete; current document was not changed"),
	},
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the project document here; an interrupted write may be retained beside it"),
		Create:      errors.New("cannot create the new project document; an interrupted write is retained"),
		Write:       errors.New("cannot write the new project document"),
		Install:     errors.New("cannot replace the project document"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("directory holds no readable project document"),
		Irregular: errors.New("project document must be a regular file"),
		Open:      errors.New("directory holds no readable project document"),
		Read:      errors.New("cannot read project document"),
	},
}
