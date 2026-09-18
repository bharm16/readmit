// Package secret holds references to credentials that stay in an operating
// system credential store or a customer-managed secret provider.
//
// readmit never stores a credential value, never writes one into a store, and
// never renders one. A reference records four things about a credential it does
// not hold: which kind of store the operator declared it lives in, what it may
// be presented to, how to read it back from that store, and when the operator
// last recorded a rotation. Configuration therefore exports references rather
// than values: a document of this package, a target configuration that names
// one, a manifest, a report and a log can all be shared without carrying a
// credential, because none of them ever held one.
//
// A value exists only inside a single command that resolved it. It is carried
// as a Value, which masks itself under every formatting verb and refuses to be
// serialized at all, so a credential cannot reach an artifact by being printed,
// logged or marshalled by accident.
package secret

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
)

const (
	// Schema is the only secret reference document this release reads. A new
	// member means a new version string and a reader for both, never an added
	// member here and never an in-place migration.
	Schema = "readmit-secrets/v1"

	// Mask is what readmit prints wherever a credential value would otherwise
	// appear. It is a fixed string: its length says nothing about a value.
	Mask = "********"

	// ResolveTimeout bounds one read from a declared store. A provider that has
	// not answered by then is unavailable; it is never reported as empty.
	ResolveTimeout = 5 * time.Second

	// incompleteSuffix marks the partial file a replacement document is written
	// to before it is renamed into place.
	incompleteSuffix = ".incomplete"

	// MaxReferences bounds one document. A store past the bound is refused
	// rather than truncated.
	MaxReferences = 256

	maxArguments     = 32
	maxArgumentBytes = 1024
	maxCommandBytes  = 4096
	maxAddressBytes  = 260
	maxNameBytes     = 64
	maxGeneration    = 1 << 20
	maxRotationAge   = 8760 * time.Hour
	maxDocumentBytes = 1 << 20
	maxValueBytes    = 64 << 10
)

// A scan reads local evidence and configuration in bounded amounts. Paths past
// a bound are refused so they can be checked in parts; they are never dropped
// silently, because a scan that skipped a file cannot report on it.
const (
	MaxScanFiles     = 8192
	MaxScanFileBytes = 16 << 20
	MaxScanBytes     = 256 << 20
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. A version this release cannot read is reported,
// never migrated in place.
var ErrUnsupportedVersion = errors.New("unsupported secret reference document version")

// Store is the kind of storage the operator declared. It is a declaration
// recorded as made, never a verified property of the provider: readmit runs the
// command it was given and cannot establish what answered.
type Store string

const (
	OSKeychain      Store = "os-keychain"
	CustomerManaged Store = "customer-managed"
)

var stores = []Store{OSKeychain, CustomerManaged}

// Purpose is the single use a reference may be bound to. The set is closed, and
// an unknown purpose is refused rather than treated as any other one. A
// credential for a different purpose is a different reference, so one stored
// credential is never widened by editing, and binding demands the purpose the
// caller needs, so adding a purpose cannot widen a reference registered under
// another one.
type Purpose string

const (
	// MLLPEndpoint is a credential presented to an MLLP endpoint.
	MLLPEndpoint Purpose = "mllp-endpoint"
	// SourceEndpoint is a credential a read-only transfer program is given to
	// reach one approved customer-controlled evidence source. It is read access
	// to evidence and nothing else; a credential registered for an MLLP
	// endpoint is refused here rather than presented to a transfer program.
	SourceEndpoint Purpose = "source-endpoint"
)

var purposes = []Purpose{MLLPEndpoint, SourceEndpoint}

// RotationState is what a recorded rotation says about a reference now.
type RotationState string

const (
	RotationCurrent     RotationState = "current"
	RotationOverdue     RotationState = "overdue"
	RotationNotDeclared RotationState = "not-declared"
)

// Reference names one credential without holding it.
//
// Command and Arguments are a locator: the absolute path of a program that
// prints the credential on standard output, and the arguments that select it.
// A value is never an argument, because an argument is visible to every process
// on the machine; readmit never puts one there and never prints the ones it was
// given.
//
// Generation and RotatedAt are the operator's record of a rotation performed in
// the store itself. readmit does not read a previous value and cannot verify
// that one was replaced; a recorded rotation is an assertion, not proof.
type Reference struct {
	Name       string    `json:"name"`
	Store      Store     `json:"store"`
	Purpose    Purpose   `json:"purpose"`
	Address    string    `json:"address"`
	Command    string    `json:"command"`
	Arguments  []string  `json:"arguments"`
	Generation int       `json:"generation"`
	RotatedAt  time.Time `json:"rotated_at"`
	MaxAge     string    `json:"max_age,omitzero"`
}

// Rotation reports what the recorded rotation says at a given time. A reference
// that declares no interval is reported as not-declared, never as current: an
// unknown rotation state is not a passing one.
func (r Reference) Rotation(now time.Time) RotationState {
	if r.MaxAge == "" {
		return RotationNotDeclared
	}
	interval, err := time.ParseDuration(r.MaxAge)
	if err != nil || interval <= 0 {
		return RotationNotDeclared
	}
	if now.Sub(r.RotatedAt) > interval {
		return RotationOverdue
	}
	return RotationCurrent
}

// Document is a complete secret reference store. It holds references only.
type Document struct {
	Schema     string      `json:"schema"`
	References []Reference `json:"references"`
}

// Value is a credential resolved from its store for the duration of one
// command. It masks itself under every formatting verb and refuses to be
// serialized, so the only way a value leaves this type is Expose, which a
// reader can see at the call site.
type Value struct{ raw []byte }

func (Value) String() string { return Mask }

// Format masks the value under every verb, including %x and %#v, so no
// formatting directive anywhere can print a credential.
func (Value) Format(state fmt.State, _ rune) { io.WriteString(state, Mask) }

// MarshalJSONTo refuses outright. A credential has no place in any artifact, so
// a struct that carries one cannot be written rather than being written masked.
func (Value) MarshalJSONTo(*jsontext.Encoder) error {
	return errors.New("a credential value is never serialized")
}

// MarshalJSON refuses for the same reason under the original encoder.
func (Value) MarshalJSON() ([]byte, error) {
	return nil, errors.New("a credential value is never serialized")
}

// Expose returns the resolved bytes. Every caller is a place a credential is
// deliberately used, and there are no others.
func (v Value) Expose() []byte { return v.raw }

// Decode reads a secret reference document. Unknown members and unknown
// versions are errors; there is no migration and no repair.
func Decode(data []byte) (Document, error) {
	if len(data) > maxDocumentBytes {
		return Document{}, errors.New("secret reference document exceeds its size limit")
	}
	// The declared version is read before the strict decode, so a document from
	// a later release is reported as the version it declares rather than as
	// invalid because of the members that version added.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid secret reference document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid secret reference document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Encode writes a validated document deterministically, so the same store
// produces the same bytes on every machine.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode secret reference document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("secret reference document exceeds its size limit")
	}
	return data, nil
}

// Validate reports the first reason a document cannot be stored. Diagnostics
// name the member at fault and never repeat the value that failed.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if len(document.References) > MaxReferences {
		return errors.New("a store holds at most " + strconv.Itoa(MaxReferences) + " references")
	}
	for i, entry := range document.References {
		if err := validateReference(entry); err != nil {
			return err
		}
		if i > 0 && document.References[i-1].Name >= entry.Name {
			return errors.New("references must be uniquely named and sorted")
		}
	}
	return nil
}

func validateReference(entry Reference) error {
	if err := identifier(entry.Name); err != nil {
		return errors.New("reference name: " + err.Error())
	}
	if !slices.Contains(stores, entry.Store) {
		return errors.New("reference store: not one of os-keychain, customer-managed")
	}
	if !slices.Contains(purposes, entry.Purpose) {
		return errors.New("reference purpose: not one this release knows")
	}
	if err := endpoint(entry.Address); err != nil {
		return errors.New("reference address: " + err.Error())
	}
	// The locator rule has one owner. The messages are unchanged: Validate
	// names the member, and the caller names what it is validating.
	if err := entry.Locator().Validate(); err != nil {
		return errors.New("reference " + err.Error())
	}
	if entry.Generation < 1 || entry.Generation > maxGeneration {
		return errors.New("reference generation: must be between 1 and " + strconv.Itoa(maxGeneration))
	}
	if entry.RotatedAt.IsZero() {
		return errors.New("reference rotation time: must be recorded")
	}
	if entry.MaxAge != "" {
		interval, err := time.ParseDuration(entry.MaxAge)
		if err != nil || interval <= 0 || interval > maxRotationAge {
			return errors.New("reference rotation interval: must be a positive duration at most one year")
		}
	}
	return nil
}

// identifier is the name character set: no separator, no whitespace and no
// character that changes meaning in a shell or a file name.
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

// endpoint is the scope a reference is bound to. Bind compares it to the
// address the caller is actually using, byte for byte; nothing here resolves a
// name, so no DNS answer can widen the scope of a stored credential.
func endpoint(value string) error {
	if value == "" || len(value) > maxAddressBytes {
		return errors.New("must be an explicit host and numeric port")
	}
	host, port, err := net.SplitHostPort(value)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
		return errors.New("must be an explicit host and numeric port")
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return errors.New("must contain printable ASCII without spaces")
		}
	}
	return nil
}

func command(value string) error {
	if value == "" || len(value) > maxCommandBytes {
		return errors.New("must be the absolute path of the program that reads the credential")
	}
	if !filepath.IsAbs(value) {
		return errors.New("must be an absolute path, never a name resolved through PATH")
	}
	return argumentText(value)
}

func argumentText(value string) error {
	if len(value) > maxArgumentBytes {
		return errors.New("must be at most " + strconv.Itoa(maxArgumentBytes) + " bytes")
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

// indexOf is the one lookup every operation on a registered reference uses, so
// a name means the same thing to all of them.
func indexOf(document Document, name string) int {
	return slices.IndexFunc(document.References, func(r Reference) bool { return r.Name == name })
}

// Find returns one registered reference by name.
func Find(document Document, name string) (Reference, error) {
	index := indexOf(document, name)
	if index < 0 {
		return Reference{}, errors.New("no credential reference is registered under that name")
	}
	return document.References[index], nil
}

// stamp is the recorded precision of a rotation time: one second, in UTC, so a
// document records the same bytes wherever it was written.
func stamp(at time.Time) time.Time { return at.UTC().Truncate(time.Second) }

// Bind returns the reference a caller may use for exactly one purpose at
// exactly one address. A reference scoped elsewhere is refused rather than
// used, so a credential stored for one endpoint is never presented to another.
func Bind(document Document, name string, purpose Purpose, address string) (Reference, error) {
	entry, err := Find(document, name)
	if err != nil {
		return Reference{}, err
	}
	if entry.Purpose != purpose {
		return Reference{}, errors.New("the credential reference declares a different purpose than this use")
	}
	if entry.Address != address {
		return Reference{}, errors.New("the credential reference is scoped to a different endpoint address")
	}
	return entry, nil
}

// Add registers a new reference in sorted order and returns it as stored. The
// document it is given is not modified.
func Add(document Document, entry Reference) (Document, Reference, error) {
	if _, err := Find(document, entry.Name); err == nil {
		return Document{}, Reference{}, errors.New("that name is already registered in this store")
	}
	entry.RotatedAt = stamp(entry.RotatedAt)
	updated := document
	updated.References = slices.Clip(slices.Clone(document.References))
	updated.References = append(updated.References, entry)
	slices.SortFunc(updated.References, func(a, b Reference) int { return strings.Compare(a.Name, b.Name) })
	if err := Validate(updated); err != nil {
		return Document{}, Reference{}, err
	}
	return updated, entry, nil
}

// Change is the configuration one update may replace. The name and the purpose
// are not here: they are what the stored credential is for, and a credential
// for something else is a different reference. An absent member is left exactly
// as it was, so an address can change without restating the locator.
type Change struct {
	Store     *Store
	Address   *string
	Command   *string
	Arguments *[]string
	MaxAge    *string
}

// Empty reports whether an update would change nothing.
func (c Change) Empty() bool { return c == Change{} }

// Update replaces the configuration of one registered reference. The recorded
// generation and rotation time are left exactly as recorded: re-pointing a
// reference at a different stored credential is not a rotation, and readmit
// does not invent one. The document it is given is not modified.
func Update(document Document, name string, change Change) (Document, Reference, error) {
	index := indexOf(document, name)
	if index < 0 {
		return Document{}, Reference{}, errors.New("no credential reference is registered under that name")
	}
	updated := document
	updated.References = slices.Clip(slices.Clone(document.References))
	entry := updated.References[index]
	if change.Store != nil {
		entry.Store = *change.Store
	}
	if change.Address != nil {
		entry.Address = *change.Address
	}
	if change.Command != nil {
		entry.Command = *change.Command
	}
	if change.Arguments != nil {
		entry.Arguments = slices.Clip(slices.Clone(*change.Arguments))
	}
	if change.MaxAge != nil {
		entry.MaxAge = *change.MaxAge
	}
	updated.References[index] = entry
	if err := Validate(updated); err != nil {
		return Document{}, Reference{}, err
	}
	return updated, entry, nil
}

// Rotate records that the operator replaced the stored credential. It bumps the
// generation and stamps the time; it reads no value and replaces nothing in the
// store, because readmit holds read access to a credential and nothing more.
func Rotate(document Document, name string, at time.Time) (Document, Reference, error) {
	index := indexOf(document, name)
	if index < 0 {
		return Document{}, Reference{}, errors.New("no credential reference is registered under that name")
	}
	updated := document
	updated.References = slices.Clip(slices.Clone(document.References))
	entry := updated.References[index]
	entry.Generation++
	entry.RotatedAt = stamp(at)
	updated.References[index] = entry
	if err := Validate(updated); err != nil {
		return Document{}, Reference{}, err
	}
	return updated, entry, nil
}

// Resolve reads one credential from the store the reference declares.
//
// The declared program is run directly by absolute path, never looked up on
// PATH, with the credential never passed as an argument and the provider's own
// diagnostics discarded, so a provider cannot write a value into readmit's
// output by failing noisily. Output is bounded and one trailing line ending is
// removed; empty output is an unavailable credential, never an empty one.
func Resolve(ctx context.Context, reference Reference) (Value, error) {
	if err := validateReference(reference); err != nil {
		return Value{}, err
	}
	return reference.Locator().Read(ctx)
}

// Locator is how a value is read back from a store readmit does not own: the
// absolute path of a program that prints it, and the arguments that select
// which one. It is the whole of what readmit knows about obtaining a value, and
// it is deliberately one type rather than two loose parameters that travel
// together, because a caller holding a key reference needs exactly the rule a
// caller holding a credential reference needs.
type Locator struct {
	Command   string
	Arguments []string
}

// Locator is the reference's own locator.
func (r Reference) Locator() Locator { return Locator{Command: r.Command, Arguments: r.Arguments} }

// Validate reports the first reason a locator cannot be used. The caller names
// what it is validating; this names the member at fault and never repeats the
// argument that failed.
func (l Locator) Validate() error {
	if err := command(l.Command); err != nil {
		return errors.New("command: " + err.Error())
	}
	if len(l.Arguments) > maxArguments {
		return errors.New("arguments: more than this release passes")
	}
	for _, argument := range l.Arguments {
		if err := argumentText(argument); err != nil {
			return errors.New("argument: " + err.Error())
		}
	}
	return nil
}

// Read is that same bounded read of one operator-declared program, for a caller
// holding a locator rather than a credential reference. It exists so readmit
// has exactly one way to obtain a value from a store it does not own, whatever
// kind of value it is; a second mechanism is a second place to get this wrong.
func (l Locator) Read(ctx context.Context) (Value, error) {
	if err := l.Validate(); err != nil {
		return Value{}, errors.New("resolution " + err.Error())
	}
	path, err := artifactpath.Resolve(l.Command)
	if err != nil {
		return Value{}, errors.New("the declared resolution program cannot be resolved")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Value{}, errors.New("the declared resolution program must be a regular file")
	}
	ctx, cancel := context.WithTimeout(ctx, ResolveTimeout)
	defer cancel()
	var out bytes.Buffer
	declared := exec.CommandContext(ctx, path, l.Arguments...)
	declared.Stdout = &bounded{to: &out, remaining: maxValueBytes}
	declared.Stderr = io.Discard
	if err := declared.Run(); err != nil {
		return Value{}, errors.New("the credential could not be read from its declared store")
	}
	raw := bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	raw = bytes.TrimSuffix(raw, []byte("\r"))
	if len(raw) == 0 {
		return Value{}, errors.New("the declared store returned no credential value")
	}
	return Value{raw: bytes.Clone(raw)}, nil
}

// bounded stops a provider that streams more than one credential's worth of
// output, so a hostile or misconfigured program cannot exhaust memory here.
type bounded struct {
	to        *bytes.Buffer
	remaining int
}

func (b *bounded) Write(data []byte) (int, error) {
	if len(data) > b.remaining {
		return 0, errors.New("the declared store returned more than one credential")
	}
	b.remaining -= len(data)
	return b.to.Write(data)
}

// ReadStore reads one bounded, regular secret reference document.
func ReadStore(path string) (Document, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Document{}, errors.New("cannot resolve the secret reference document")
	}
	data, err := readLocal(resolved, maxDocumentBytes)
	if err != nil {
		return Document{}, errors.New("the secret reference document must be a readable regular file within its size limit")
	}
	return Decode(data)
}

// readLocal reads one bounded regular file. It re-checks the opened file rather
// than trusting the earlier stat, so a path that changed underneath is refused.
func readLocal(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open input file")
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("input must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > limit {
		return nil, errors.New("input cannot be read within its size limit")
	}
	return data, nil
}

// WriteStore replaces the document atomically: it is written in full to a new
// owner-only file and renamed over the previous one, so a reader never observes
// a partial document and a failed write leaves the previous one exactly as it
// was. The incomplete file must not exist, so an interrupted write is reported
// rather than overwritten.
func WriteStore(path string, document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	// Both files this writes are reserved by artifactpath, and the file it
	// renames onto is the destination artifactpath itself returned. Neither
	// path is derived from the other, so there is one path policy here.
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write the secret reference document here")
	}
	incomplete, err := artifactpath.Destination(path + incompleteSuffix)
	if err != nil {
		return errors.New("cannot write the secret reference document here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new secret reference document; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new secret reference document")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the secret reference document")
	}
	return nil
}

// Collect reads the bounded contents of every regular file the named paths
// hold, keyed by the name the caller used, and reports how many entries it did
// not read so a scan over them can name its own boundary.
//
// Each named path is resolved, following symbolic links, so naming a link
// checks what it points at: that is the path the person asked about. Entries
// found *beneath* a named directory are never followed. A symbolic link inside
// the tree, and anything that is not a regular file, is counted as not read
// rather than opened, so walking a tree cannot leave it, loop, or block on a
// device. Every bound is applied to what the tree declares before anything is
// read, so a refusal costs nothing and reads nothing.
func Collect(roots []string) (map[string][]byte, int, error) {
	type candidate struct {
		name string
		path string
	}
	candidates := make([]candidate, 0, len(roots))
	skipped, total := 0, int64(0)
	declare := func(name, path string, info os.FileInfo) error {
		if info.Size() > MaxScanFileBytes {
			return errors.New("a file to check exceeds the scan's per-file size limit")
		}
		if len(candidates) >= MaxScanFiles || total+info.Size() > MaxScanBytes {
			return errors.New("the paths to check exceed the scan's limits; check them in parts")
		}
		total += info.Size()
		candidates = append(candidates, candidate{name: strings.TrimPrefix(name, "./"), path: path})
		return nil
	}
	for _, root := range roots {
		resolved, err := artifactpath.Resolve(root)
		if err != nil {
			return nil, 0, errors.New("a path to check could not be resolved")
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, 0, errors.New("a path to check could not be read")
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				skipped++
				continue
			}
			if err := declare(root, resolved, info); err != nil {
				return nil, 0, err
			}
			continue
		}
		walkErr := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				skipped++
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			relative, err := filepath.Rel(resolved, path)
			if err != nil {
				return errors.New("a path to check could not be read")
			}
			return declare(root+"/"+filepath.ToSlash(relative), path, info)
		})
		if walkErr != nil {
			return nil, 0, walkErr
		}
	}
	files := make(map[string][]byte, len(candidates))
	for _, entry := range candidates {
		data, err := readLocal(entry.path, MaxScanFileBytes)
		if err != nil {
			return nil, 0, errors.New("a file to check could not be read")
		}
		files[entry.name] = data
	}
	return files, skipped, nil
}
