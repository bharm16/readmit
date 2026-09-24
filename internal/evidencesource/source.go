// Package evidencesource collects evidence readmit did not produce, from a
// source the customer controls and an operator explicitly approved.
//
// The thing it exists to prevent is a collection that could not run being read
// as a source that held nothing. A source nobody had permission to list, one
// that disconnected mid-transfer, one whose entry was cut short by a quota, one
// this release does not support and one the operator cancelled each collected
// zero entries, and none of them is the observation that a source is empty.
// Every one of them is an execution error, named in
// [github.com/bharm16/readmit/internal/observewindow]'s source-neutral
// vocabulary rather than in a second one invented here, so a collector cannot
// widen the rule by construction.
//
// What it collects, it collects through one ingestion path. Each entry is
// streamed through [github.com/bharm16/readmit/internal/importer.Scan] under
// the same readmit-import-plan/v1 an import declares, so a collection holds one
// record and one parsing batch whatever the entry's length, refuses exactly
// what an import of the same bytes refuses, and reports what an import would
// find. Nothing here reads a source into memory, and nothing here writes a case
// bundle: a collection stages original bytes and a receipt beside them, and
// `readmit import` turns them into evidence.
//
// readmit holds no credential. Following ADR-0006 a source names a *reference*
// registered in a readmit-secrets/v1 document, bound to this source's own
// purpose and address before anything is read, and the value it resolves to
// exists only inside the one command that read it.
package evidencesource

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The three contract versions this package owns. A new or changed member of any
// of them is a new version string with a reader for every older one, never an
// added member here and never an in-place migration.
const (
	// Schema is the declaration an operator authors and readmit reads.
	Schema = "readmit-source/v1"
	// AccessSchema is what a permission diagnosis retains.
	AccessSchema = "readmit-source-access/v1"
	// CollectionSchema is what a collection retains beside the bytes it staged.
	CollectionSchema = "readmit-source-collection/v1"
)

const (
	// MaxSourceBytes bounds the declaration a command reads before decoding it.
	// The access diagnosis carries no bound of its own: it is bounded by the
	// entry count a source may declare, and has no reader in this release to
	// hold one to.
	MaxSourceBytes = 64 << 10
	// MaxCollectionBytes bounds the collection receipt an import reads. A
	// receipt holds at most MaxEntries entries, each naming one listed entry,
	// at most the one it repeats, a digest and a bounded reason, which is well
	// inside this.
	MaxCollectionBytes = 8 << 20

	// MaxEntries bounds the entries one source may declare. It is the bound one
	// folder or archive is read under, because a collection stages a folder an
	// import then reads as one container.
	MaxEntries = 4096
	// MaxEntryBytes bounds one collected entry. It is the bound one case bundle
	// source is held to: an entry past it is evidence no import could store.
	MaxEntryBytes = 16 << 20
	// MaxTotalBytes bounds one collection. It is the bytes one container may be
	// read for, for the same reason.
	MaxTotalBytes = 128 << 20
	// MaxAttempts bounds how many times one entry may be read. A read that has
	// not succeeded by then is reported as a read that did not succeed.
	MaxAttempts = 3
	// MaxBackoff bounds the wait between attempts.
	MaxBackoff = 5 * time.Second

	maxNameBytes    = 64
	maxScopeBytes   = 64
	maxAddressBytes = 260
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. A version this release cannot read is reported,
// never migrated in place.
var ErrUnsupportedVersion = errors.New("unsupported evidence source document version")

// Kind is how readmit reaches the source. It is a dispatch, not a label: each
// kind is reached a different way and each is refused a different way.
type Kind string

const (
	// Directory is a tree this machine can already open — a local folder, a
	// mounted export share, or a directory some other tool already synced.
	// Nothing leaves the machine, so no destination is decided for one.
	Directory Kind = "directory"
	// Transfer is a remote source reached by running the read-only transfer
	// program the operator declared. It is how an SFTP export is collected:
	// readmit speaks no SSH, runs the customer's own client by absolute path,
	// and cannot establish what answered.
	Transfer Kind = "transfer"
	// API is a source behind a documented read-only application interface.
	// This release reads the declaration and collects nothing from it: no
	// interface has been approved, and an unsupported source is an execution
	// error rather than an empty one.
	API Kind = "api"
)

var kinds = []Kind{Directory, Transfer, API}

// The recorded environment classes a source may declare. They are the spellings
// readmit-target records and internal/sendpolicy compares against; this package
// keeps no classification type of its own, so a recorded class travels to the
// decision exactly as it was written. An unknown spelling is refused when the
// declaration is read, because a typo that merely fails to be nonproduction is
// a denial nobody asked for rather than the declaration error it is.
const (
	Nonproduction = "nonproduction"
	Production    = "production"
	Unclassified  = "unclassified"
)

var classifications = []string{Nonproduction, Production, Unclassified}

// Quota is what one collection may read. Every member is required and none has
// a default: how much of a customer's evidence readmit may pull is the
// operator's declaration, never a number this release picked for them.
type Quota struct {
	MaxEntries    int   `json:"max_entries"`
	MaxEntryBytes int64 `json:"max_entry_bytes"`
	MaxTotalBytes int64 `json:"max_total_bytes"`
}

// Retry is how many times one entry may be read and how long to wait between
// attempts. Attempts of 1 declares that a failed read is a failed read.
type Retry struct {
	Attempts int    `json:"attempts"`
	Backoff  string `json:"backoff"`
}

// Wait is the declared backoff as a duration. Validate has already accepted it.
func (r Retry) Wait() time.Duration { d, _ := time.ParseDuration(r.Backoff); return d }

// Source is one declared customer-controlled source. It is explicitly selected,
// never discovered: there is no default source, no implicit file and no
// environment variable that supplies one.
//
// The members below the quota belong to one kind each and are refused on any
// other, so a declaration cannot carry a remote address that nothing reaches or
// a local root that nothing opens. Credential holds a reference and the
// document it is registered in, never a credential: a source declaration is
// shared as written, so a value in one would be a value in every copy of it.
type Source struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"`
	// Scope names the subset of the source this declaration covers. It is
	// load-bearing for the same reason an observation window's scope is: what a
	// collection says about a source is only ever about this scope.
	Scope string `json:"scope"`
	Quota Quota  `json:"quota"`
	Retry Retry  `json:"retry"`

	// Root is the directory kind's tree. It is left exactly as the declaration
	// writes it and resolved when it is opened, never when it is read.
	Root string `json:"root,omitzero"`

	// Address and Classification belong to the two kinds that reach off this
	// machine. The class is a claim recorded as made; what a collection may
	// actually reach is decided against the addresses the name resolves to at
	// the moment of the collection, in internal/sendpolicy.
	Address        string `json:"address,omitzero"`
	Classification string `json:"classification,omitzero"`

	// Command and Arguments are the transfer kind's locator: the absolute path
	// of the operator's read-only transfer program and the arguments that
	// select which source it speaks to. A credential is never an argument,
	// because an argument is visible to every process on the machine.
	Command   string   `json:"command,omitzero"`
	Arguments []string `json:"arguments,omitzero"`
	// SecretsFile and Credential name the registered reference the transfer
	// program is given. Both are declared together or neither is.
	SecretsFile string `json:"secrets_file,omitzero"`
	Credential  string `json:"credential,omitzero"`
}

// Identity is what every document this package writes says about the source it
// is about: the name a person gave it, how readmit reaches it, and the scope it
// covers. It carries no address, no root and no locator, so a retained document
// names the source without republishing where it lives.
type Identity struct {
	Name  string `json:"name"`
	Kind  Kind   `json:"kind"`
	Scope string `json:"scope"`
}

// Identity reports what this source is called, for the documents that record it.
func (s Source) Identity() Identity {
	return Identity{Name: s.Name, Kind: s.Kind, Scope: s.Scope}
}

// Declared reports whether this source names a credential reference at all.
func (s Source) Declared() bool { return s.SecretsFile != "" || s.Credential != "" }

// Reaches reports whether collecting from this source reaches off this machine,
// and therefore whether a destination decision governs it. A directory source
// opens files this machine already has; nothing about it is a destination.
func (s Source) Reaches() bool { return s.Kind == Transfer || s.Kind == API }

// UnmarshalJSON requires every member every kind declares, so an omitted quota
// or retry declaration cannot decode into a permissive zero value, and then
// re-decodes rejecting unknown members so a source authored against a later
// contract is never read as though this one had always allowed it.
func (s *Source) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema *string `json:"schema"`
		Name   *string `json:"name"`
		Kind   *string `json:"kind"`
		Scope  *string `json:"scope"`
		Quota  *Quota  `json:"quota"`
		Retry  *Retry  `json:"retry"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return errors.New("invalid evidence source JSON")
	}
	if required.Schema == nil || required.Name == nil || required.Kind == nil || required.Scope == nil || required.Quota == nil || required.Retry == nil {
		return errors.New("an evidence source requires a schema, name, kind, scope, quota, and retry declaration")
	}
	if *required.Schema != Schema {
		return ErrUnsupportedVersion
	}
	type plainSource Source
	var value plainSource
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid evidence source JSON")
	}
	*s = Source(value)
	return nil
}

// UnmarshalJSON on each nested type applies its own presence check and its own
// unknown-member refusal rather than inheriting the enclosing document's.
func (q *Quota) UnmarshalJSON(data []byte) error {
	var required struct {
		MaxEntries    *int   `json:"max_entries"`
		MaxEntryBytes *int64 `json:"max_entry_bytes"`
		MaxTotalBytes *int64 `json:"max_total_bytes"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.MaxEntries == nil || required.MaxEntryBytes == nil || required.MaxTotalBytes == nil {
		return errors.New("a quota requires an entry count, a per-entry byte limit, and a total byte limit")
	}
	type plainQuota Quota
	var value plainQuota
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid quota")
	}
	*q = Quota(value)
	return nil
}

func (r *Retry) UnmarshalJSON(data []byte) error {
	var required struct {
		Attempts *int    `json:"attempts"`
		Backoff  *string `json:"backoff"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Attempts == nil || required.Backoff == nil {
		return errors.New("a retry declaration requires an attempt count and a backoff")
	}
	type plainRetry Retry
	var value plainRetry
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid retry declaration")
	}
	*r = Retry(value)
	return nil
}

// Decode reads one evidence source declaration exactly as written. Unknown
// members and unknown versions are errors; there is no migration and no repair.
func Decode(data []byte) (Source, error) {
	if len(data) > MaxSourceBytes {
		return Source{}, errors.New("evidence source declaration exceeds its size limit")
	}
	var source Source
	if err := json.Unmarshal(data, &source); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Source{}, ErrUnsupportedVersion
		}
		return Source{}, errors.New("invalid evidence source JSON")
	}
	if err := source.Validate(); err != nil {
		return Source{}, err
	}
	return source, nil
}

// Validate reports the first reason a declaration cannot be used. Diagnostics
// name the member at fault and never repeat the value that failed.
func (s Source) Validate() error {
	if s.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := label(s.Name, maxNameBytes); err != nil {
		return errors.New("source name: " + err.Error())
	}
	if err := label(s.Scope, maxScopeBytes); err != nil {
		return errors.New("source scope: " + err.Error())
	}
	if !slices.Contains(kinds, s.Kind) {
		return errors.New("source kind: not one of directory, transfer, api")
	}
	if err := s.Quota.validate(); err != nil {
		return err
	}
	if err := s.Retry.validate(); err != nil {
		return err
	}
	return s.kindMembers()
}

// kindMembers holds each kind to exactly the members it declares. A member that
// belongs to another kind is refused rather than ignored: a declaration that
// names a remote address nothing dials, or a local root nothing opens, is a
// mistake an operator wants reported rather than silently dropped.
func (s Source) kindMembers() error {
	switch s.Kind {
	case Directory:
		if s.Root == "" {
			return errors.New("source root: a directory source declares the tree it reads")
		}
		if s.Address != "" || s.Classification != "" || s.Command != "" || len(s.Arguments) != 0 || s.Declared() {
			return errors.New("source members: a directory source declares no address, classification, transfer program, or credential")
		}
		return nil
	case Transfer:
		if s.Root != "" {
			return errors.New("source members: a transfer source declares no local root")
		}
		if err := s.destinationMembers(); err != nil {
			return err
		}
		if err := absoluteProgram(s.Command); err != nil {
			return errors.New("source command: " + err.Error())
		}
		if len(s.Arguments) > 32 {
			return errors.New("source arguments: more than this release passes")
		}
		for _, argument := range s.Arguments {
			if err := printableText(argument, 1024); err != nil {
				return errors.New("source argument: " + err.Error())
			}
		}
		if (s.SecretsFile == "") != (s.Credential == "") {
			return errors.New("source credential: a reference and the document it is registered in are declared together")
		}
		if s.Credential != "" {
			if err := label(s.Credential, maxNameBytes); err != nil {
				return errors.New("source credential: " + err.Error())
			}
			if err := printableText(s.SecretsFile, 1024); err != nil {
				return errors.New("source secrets file: " + err.Error())
			}
		}
		return nil
	default:
		if s.Root != "" || s.Command != "" || len(s.Arguments) != 0 || s.Declared() {
			return errors.New("source members: an api source declares no local root, transfer program, or credential")
		}
		return s.destinationMembers()
	}
}

// destinationMembers holds the two kinds that reach off this machine to an
// explicit address and a recorded class. Neither is optional: an address is how
// the destination is decided, and an absent class is not a nonproduction one.
func (s Source) destinationMembers() error {
	if err := printableText(s.Address, maxAddressBytes); err != nil || s.Address == "" {
		return errors.New("source address: must be an explicit host and numeric port")
	}
	if strings.Count(s.Address, ":") == 0 {
		return errors.New("source address: must be an explicit host and numeric port")
	}
	if !slices.Contains(classifications, s.Classification) {
		return errors.New("source classification: not one of nonproduction, production, unclassified")
	}
	return nil
}

func (q Quota) validate() error {
	if q.MaxEntries < 1 || q.MaxEntries > MaxEntries {
		return errors.New("quota entry count: must be between 1 and " + strconv.Itoa(MaxEntries))
	}
	if q.MaxEntryBytes < 1 || q.MaxEntryBytes > MaxEntryBytes {
		return errors.New("quota per-entry byte limit: must be between 1 and " + strconv.Itoa(MaxEntryBytes))
	}
	if q.MaxTotalBytes < 1 || q.MaxTotalBytes > MaxTotalBytes {
		return errors.New("quota total byte limit: must be between 1 and " + strconv.Itoa(MaxTotalBytes))
	}
	if q.MaxEntryBytes > q.MaxTotalBytes {
		return errors.New("quota per-entry byte limit: must not exceed the total byte limit")
	}
	return nil
}

func (r Retry) validate() error {
	if r.Attempts < 1 || r.Attempts > MaxAttempts {
		return errors.New("retry attempts: must be between 1 and " + strconv.Itoa(MaxAttempts))
	}
	wait, err := time.ParseDuration(r.Backoff)
	if err != nil || wait < 0 || wait > MaxBackoff {
		return errors.New("retry backoff: must be a Go duration between 0s and " + MaxBackoff.String())
	}
	return nil
}

// label is the operator-chosen name character set: printable, bounded, and
// holding no separator that would change meaning in a path or a diagnostic.
func label(value string, limit int) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if len(value) > limit {
		return errors.New("must be at most " + strconv.Itoa(limit) + " characters")
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

// absoluteProgram is the same rule internal/secret holds a credential locator
// to: an absolute path, never a name resolved through PATH.
func absoluteProgram(value string) error {
	if err := printableText(value, 4096); err != nil || value == "" {
		return errors.New("must be the absolute path of the read-only transfer program")
	}
	if !strings.HasPrefix(value, "/") && !windowsAbsolute(value) {
		return errors.New("must be an absolute path, never a name resolved through PATH")
	}
	return nil
}

// windowsAbsolute recognises the two absolute spellings a Windows path takes,
// so a declaration validates the same way on every released target rather than
// only on the one it was authored on.
func windowsAbsolute(value string) bool {
	if strings.HasPrefix(value, `\\`) {
		return true
	}
	return len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func printableText(value string, limit int) error {
	if len(value) > limit {
		return errors.New("must be at most " + strconv.Itoa(limit) + " bytes")
	}
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return errors.New("must not contain control characters")
		}
	}
	return nil
}
