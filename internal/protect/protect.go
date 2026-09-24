// Package protect holds the storage-protection controls readmit applies to the
// sensitive files, indexes, temporary work and backup copies it writes, and the
// encrypted transfer packages it produces from them.
//
// readmit holds no key material. Following
// [ADR-0006](../../docs/adr/0006-credentials-are-referenced-never-stored.md), a
// control records a *reference* to a key that stays in an operating system
// credential store or a customer-managed key provider: the absolute path of the
// program that prints it, the locator arguments that select it, the operator's
// record of rotation, the retention period packages made with it declare, and
// the at-rest storage control the operator declares for the volume the evidence
// sits on. A key exists only inside the single command that read it, carried by
// [secret.Value], which masks itself under every formatting verb and refuses to
// be serialized. readmit must not become the thing that stores key material it
// just refused to store for credentials.
//
// The storage declaration is recorded as made and is never a verified property:
// readmit runs on five CGO_ENABLED=0 targets and cannot interrogate FileVault,
// BitLocker, LUKS or a volume's wear levelling. What readmit does apply itself
// is narrow and stated: owner-only file modes on everything it writes, no
// plaintext temporary copy while packing, and AES-256-GCM over content *and*
// over the package index, because a sensitive index is sensitive data rather
// than harmless metadata.
package protect

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/secret"
)

const (
	// Schema is the only protection document this release reads. A new member
	// means a new version string and a reader for both, never an added member
	// here and never an in-place migration.
	Schema = "readmit-protection/v1"

	// MaxControls bounds one document. A document past the bound is refused
	// rather than truncated.
	MaxControls = 256

	maxNameBytes     = 64
	maxGeneration    = 1 << 20
	maxRotationAge   = 8760 * time.Hour
	maxRetention     = 10 * 8760 * time.Hour
	maxDocumentBytes = 1 << 20
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. An unreadable version is reported, never migrated.
var ErrUnsupportedVersion = errors.New("unsupported protection document version")

// Storage is the at-rest control the operator declared for the volume holding
// the evidence this control protects. It is a declaration recorded as made,
// never a verified property: readmit cannot establish that a volume is
// encrypted, that a snapshot of it is, or that a backup of it is.
type Storage string

const (
	OSVolumeEncryption Storage = "os-volume-encryption"
	CustomerKeyStorage Storage = "customer-key"
	StorageNotDeclared Storage = "none-declared"
)

var storages = []Storage{OSVolumeEncryption, CustomerKeyStorage, StorageNotDeclared}

// State is where a control is in its lifecycle. Retirement is not revocation:
// a retired control still opens the packages it wrote, and writes no new one.
// A key readmit never held cannot be destroyed by readmit.
type State string

const (
	Active  State = "active"
	Retired State = "retired"
)

var states = []State{Active, Retired}

// RotationState is what a recorded rotation says about a control now.
type RotationState string

const (
	RotationCurrent     RotationState = "current"
	RotationOverdue     RotationState = "overdue"
	RotationNotDeclared RotationState = "not-declared"
)

// RetentionState is what a declared retention period says about a package now.
type RetentionState string

const (
	WithinRetention      RetentionState = "within-retention"
	PastRetention        RetentionState = "past-retention"
	RetentionNotDeclared RetentionState = "not-declared"
)

// Control is one protection regime: a declared at-rest storage control, a
// reference to the key that encrypts transfer packages, the rotation record for
// that key, and the retention period packages written under it declare.
//
// Command and Arguments are a locator, never key material. Arguments are
// visible to every process on the machine; readmit never puts a key there and
// never prints back the arguments it was given.
type Control struct {
	Name       string    `json:"name"`
	Storage    Storage   `json:"storage"`
	State      State     `json:"state"`
	Command    string    `json:"command"`
	Arguments  []string  `json:"arguments"`
	Generation int       `json:"generation"`
	RotatedAt  time.Time `json:"rotated_at"`
	MaxAge     string    `json:"max_age,omitzero"`
	Retain     string    `json:"retain,omitzero"`
}

// Locator is where this control's key is read back from. readmit holds no key
// material; it holds the locator, exactly as it holds a credential's.
func (c Control) Locator() secret.Locator {
	return secret.Locator{Command: c.Command, Arguments: c.Arguments}
}

// Rotation reports what the recorded rotation says at a given time. A control
// that declares no interval is not-declared, never current: an unknown rotation
// state is not a passing one.
func (c Control) Rotation(now time.Time) RotationState {
	interval, err := time.ParseDuration(c.MaxAge)
	if c.MaxAge == "" || err != nil || interval <= 0 {
		return RotationNotDeclared
	}
	if now.Sub(c.RotatedAt) > interval {
		return RotationOverdue
	}
	return RotationCurrent
}

// RetainUntil reports the instant a package written at the given time stops
// being within the declared retention period, and whether one was declared.
func (c Control) RetainUntil(at time.Time) (time.Time, bool) {
	interval, err := time.ParseDuration(c.Retain)
	if c.Retain == "" || err != nil || interval <= 0 {
		return time.Time{}, false
	}
	return stamp(at).Add(interval), true
}

// Document is a complete set of protection controls. It holds references only.
type Document struct {
	Schema   string    `json:"schema"`
	Controls []Control `json:"controls"`
}

// Decode reads a protection document. Unknown members and unknown versions are
// errors; there is no migration and no repair.
func Decode(data []byte) (Document, error) {
	if len(data) > maxDocumentBytes {
		return Document{}, errors.New("protection document exceeds its size limit")
	}
	// The declared version is read before the strict decode, so a document from
	// a later release is reported as the version it declares rather than as
	// invalid because of the members that version added.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid protection document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid protection document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode writes a validated document deterministically, so the same controls
// produce the same bytes on every machine.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode protection document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("protection document exceeds its size limit")
	}
	return data, nil
}

// Validate reports the first reason a document cannot be stored. Diagnostics
// name the member at fault and never repeat a locator argument.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if len(document.Controls) > MaxControls {
		return errors.New("a protection document holds at most " + strconv.Itoa(MaxControls) + " controls")
	}
	for i, entry := range document.Controls {
		if err := validateControl(entry); err != nil {
			return err
		}
		if i > 0 && document.Controls[i-1].Name >= entry.Name {
			return errors.New("controls must be uniquely named and sorted")
		}
	}
	return nil
}

func validateControl(entry Control) error {
	if err := identifier(entry.Name); err != nil {
		return errors.New("control name: " + err.Error())
	}
	if !slices.Contains(storages, entry.Storage) {
		return errors.New("control storage: not one of os-volume-encryption, customer-key, none-declared")
	}
	if !slices.Contains(states, entry.State) {
		return errors.New("control state: not one of active, retired")
	}
	// A control names its key the way a credential reference names a
	// credential, so it is the same rule, owned by internal/secret rather than
	// restated here: an absolute path, never a PATH lookup, and locator
	// arguments that are never key material.
	if err := entry.Locator().Validate(); err != nil {
		return errors.New("control " + err.Error())
	}
	if entry.Generation < 1 || entry.Generation > maxGeneration {
		return errors.New("control generation: must be between 1 and " + strconv.Itoa(maxGeneration))
	}
	if entry.RotatedAt.IsZero() {
		return errors.New("control rotation time: must be recorded")
	}
	if err := interval(entry.MaxAge, maxRotationAge); err != nil {
		return errors.New("control rotation interval: " + err.Error())
	}
	if err := interval(entry.Retain, maxRetention); err != nil {
		return errors.New("control retention period: " + err.Error())
	}
	return nil
}

func interval(value string, limit time.Duration) error {
	if value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 || parsed > limit {
		return errors.New("must be a positive duration at most " + limit.String())
	}
	return nil
}

// identifier is the control name character set: no separator, no whitespace and
// no character that changes meaning in a shell or a file name. A control name
// is recorded in every package it writes, so it is a name, never a description.
func identifier(value string) error {
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

// indexOf is the one lookup every operation on a registered control uses, so a
// name means the same thing to all of them.
func indexOf(document Document, name string) int {
	return slices.IndexFunc(document.Controls, func(c Control) bool { return c.Name == name })
}

// Find returns one registered control by name.
func Find(document Document, name string) (Control, error) {
	index := indexOf(document, name)
	if index < 0 {
		return Control{}, errors.New("no protection control is registered under that name")
	}
	return document.Controls[index], nil
}

// Writable returns the control a new package may be written under. A retired
// control is refused here and nowhere else: it still opens what it wrote.
func Writable(document Document, name string) (Control, error) {
	entry, err := Find(document, name)
	if err != nil {
		return Control{}, err
	}
	if entry.State != Active {
		return Control{}, errors.New("the protection control is retired; it opens the packages it wrote and writes no new one")
	}
	return entry, nil
}

// stamp is the recorded precision of a time: one second, in UTC, so a document
// records the same bytes wherever it was written.
func stamp(at time.Time) time.Time { return at.UTC().Truncate(time.Second) }

// register adds a new control in sorted order and returns it as stored: active,
// at generation 1, rotated at the given instant. The document it is given is not
// modified.
func register(document Document, entry Control, at time.Time) (Document, Control, error) {
	if _, err := Find(document, entry.Name); err == nil {
		return Document{}, Control{}, errors.New("that name is already registered in this protection document")
	}
	entry.State = Active
	entry.Generation = 1
	entry.RotatedAt = stamp(at)
	updated := document
	updated.Controls = slices.Clip(slices.Clone(document.Controls))
	updated.Controls = append(updated.Controls, entry)
	slices.SortFunc(updated.Controls, func(a, b Control) int { return strings.Compare(a.Name, b.Name) })
	if err := Validate(updated); err != nil {
		return Document{}, Control{}, err
	}
	return updated, entry, nil
}

// rotate records that the operator replaced the key behind a control in its own
// store. It bumps the generation and stamps the time; it writes nothing to any
// store, because readmit holds read access to a key and nothing more. readmit
// does not read a previous key and so cannot verify that one was replaced.
// [File.Rotate] is the only caller, and it asks the declared store first.
//
// Packages written under an earlier generation keep their recorded generation.
// Whether an earlier package still opens depends entirely on whether the
// operator kept the earlier key; readmit neither holds nor destroys it.
func rotate(document Document, name string, at time.Time) (Document, Control, error) {
	return amend(document, name, func(entry *Control) error {
		entry.Generation++
		entry.RotatedAt = stamp(at)
		return nil
	})
}

// retire marks a control as writing no further packages.
func retire(document Document, name string) (Document, Control, error) {
	return amend(document, name, func(entry *Control) error {
		if entry.State == Retired {
			return errors.New("the protection control is already retired")
		}
		entry.State = Retired
		return nil
	})
}

func amend(document Document, name string, change func(*Control) error) (Document, Control, error) {
	index := indexOf(document, name)
	if index < 0 {
		return Document{}, Control{}, errors.New("no protection control is registered under that name")
	}
	updated := document
	updated.Controls = slices.Clip(slices.Clone(document.Controls))
	entry := updated.Controls[index]
	if err := change(&entry); err != nil {
		return Document{}, Control{}, err
	}
	updated.Controls[index] = entry
	if err := Validate(updated); err != nil {
		return Document{}, Control{}, err
	}
	return updated, entry, nil
}
