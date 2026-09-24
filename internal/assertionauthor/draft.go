// Package assertionauthor builds a readmit-assertion-set/v1 document through
// typed draft answers, without anyone opening a text editor for the normal
// path.
//
// A draft holds a name and a list of typed clauses. Each clause names one of
// the sixteen operators internal/assertion already evaluates ([ADR-0003]);
// there is no expression language and no second evaluator. What Generate
// returns is bytes assertion.Decode has already accepted, so a saved set is
// one `readmit explain` can re-decide.
//
// Nothing here evaluates an assertion, opens evidence, or reaches a network.
// Incomplete collection cannot support absence is an evaluation rule of the
// shared engine, not a draft invention: this package refuses malformed
// clauses and leaves that execution refusal to the evaluator.
//
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
package assertionauthor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
)

// Schema is the draft a structured authoring session holds. It is not an
// assertion set: it is what has been answered so far.
const Schema = "readmit-assertion-set-draft/v1"

const (
	MaxDraftBytes = assertion.MaxSetBytes
	MaxNameBytes  = 256
	MaxEntryBytes = 255
)

// ErrCannotWrite is the refusal a caller separates when the destination could
// not be created at all.
var ErrCannotWrite = errors.New("cannot write assertion set; destination must be new and parent writable")

// Clause is one typed assertion as a person authored it. Members mirror the
// public JSON of readmit-assertion-set/v1 so Generate can hand the shared
// reader exact bytes without inventing a parallel vocabulary.
type Clause struct {
	ID       string               `json:"id"`
	Operator assertion.Operator   `json:"operator"`
	Subject  assertion.Subject    `json:"subject"`
	When     *assertion.Condition `json:"when"`
	Expected assertion.Expected   `json:"expected"`
}

// Draft is one authoring session over an assertion set.
type Draft struct {
	Schema     string   `json:"schema"`
	Name       string   `json:"name"`
	Assertions []Clause `json:"assertions"`
}

// Saved names the entry written and the identity of the bytes on disk.
type Saved struct {
	Output   string `json:"output"`
	Identity string `json:"identity"`
}

// NewDraft begins an empty draft.
func NewDraft() Draft {
	return Draft{Schema: Schema, Assertions: []Clause{}}
}

// SetName replaces the local description.
func (d Draft) SetName(name string) (Draft, error) {
	if err := checkName(name); err != nil {
		return Draft{}, err
	}
	d.Name = name
	return d, nil
}

// SetAssertions replaces every clause. The shared reader decides whether the
// set is well-formed when Generate runs; this only holds the draft itself to
// local bounds and identifier uniqueness.
func (d Draft) SetAssertions(clauses []Clause) (Draft, error) {
	if err := checkClauses(clauses); err != nil {
		return Draft{}, err
	}
	d.Assertions = slices.Clone(clauses)
	return d, nil
}

// DecodeDraft reads a draft strictly. Unknown members and versions are errors.
func DecodeDraft(data []byte) (Draft, error) {
	if len(data) > MaxDraftBytes {
		return Draft{}, errors.New("assertion set draft exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Draft{}, errors.New("invalid assertion set draft")
	}
	if declared.Schema != Schema {
		return Draft{}, errors.New("assertion set draft declares a contract version this release does not read")
	}
	var present struct {
		Name       *string   `json:"name"`
		Assertions *[]Clause `json:"assertions"`
	}
	if json.Unmarshal(data, &present) != nil || present.Name == nil || present.Assertions == nil {
		return Draft{}, errors.New("an assertion set draft declares its name and assertions, answered or not")
	}
	var draft Draft
	if json.Unmarshal(data, &draft, json.RejectUnknownMembers(true)) != nil {
		return Draft{}, errors.New("invalid assertion set draft")
	}
	if err := check(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// ImportDocument loads a complete assertion set into a draft, preserving every
// clause and selector spelling the shared reader retained. Unknown versions
// and operators are refused by assertion.Decode rather than dropped.
func ImportDocument(data []byte) (Draft, error) {
	set, err := assertion.Decode(data)
	if err != nil {
		return Draft{}, err
	}
	clauses := make([]Clause, 0, len(set.Assertions))
	for _, item := range set.Assertions {
		clauses = append(clauses, Clause{
			ID: item.ID, Operator: item.Operator, Subject: item.Subject,
			When: item.When, Expected: item.Expected,
		})
	}
	draft := Draft{Schema: Schema, Name: set.Name, Assertions: clauses}
	if err := check(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// Generate writes the draft as readmit-assertion-set/v1 bytes accepted by the
// shared reader.
func Generate(draft Draft) ([]byte, error) {
	if err := check(draft); err != nil {
		return nil, err
	}
	if strings.TrimSpace(draft.Name) == "" {
		return nil, errors.New("this assertion set has no name yet")
	}
	if len(draft.Assertions) == 0 {
		return nil, errors.New("this assertion set declares no assertions yet")
	}
	doc := struct {
		Schema     string   `json:"schema"`
		Name       string   `json:"name"`
		Assertions []Clause `json:"assertions"`
	}{Schema: assertion.Schema, Name: draft.Name, Assertions: draft.Assertions}
	data, err := json.Marshal(doc, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("this draft could not be written as an assertion set")
	}
	data = append(data, '\n')
	if _, err := assertion.Decode(data); err != nil {
		return nil, errors.New("this draft does not generate an assertion set this release reads")
	}
	return data, nil
}

// Save writes the generated set into one new workspace entry and returns the
// identity of the bytes that are on disk.
func Save(root string, draft Draft, output string) (Saved, error) {
	data, err := Generate(draft)
	if err != nil {
		return Saved{}, err
	}
	if artifactpath.EntryName(output) != nil || !printable(output, MaxEntryBytes) {
		return Saved{}, errors.New("an assertion set is written to one new entry of the open workspace")
	}
	destination, err := artifactpath.Destination(artifactpath.JoinReference(root, output))
	if err != nil {
		return Saved{}, errors.New("an assertion set must be written outside every retained artifact of this workspace")
	}
	if err := write(destination, data); err != nil {
		return Saved{}, err
	}
	written, err := os.ReadFile(destination)
	if err != nil || string(written) != string(data) {
		return Saved{}, errors.New("the assertion set could not be read back from the workspace")
	}
	if _, err := assertion.Decode(written); err != nil {
		return Saved{}, errors.New("the assertion set written to the workspace is not one this release reads")
	}
	sum := sha256.Sum256(written)
	return Saved{Output: output, Identity: hex.EncodeToString(sum[:])}, nil
}

// Import reads a complete assertion set from one regular workspace entry without
// rewriting it through Generate. Keeping the original bytes retains every
// supported clause spelling the shared reader accepted.
func Import(root, entry string) ([]byte, error) {
	if artifactpath.EntryName(entry) != nil {
		return nil, errors.New("an assertion set must be one regular entry of the workspace")
	}
	path := artifactpath.JoinReference(root, entry)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxDraftBytes {
		return nil, errors.New("that entry could not be read")
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != info.Size() {
		return nil, errors.New("that entry could not be read")
	}
	if _, err := assertion.Decode(data); err != nil {
		return nil, err
	}
	return data, nil
}

// Export writes exact reviewed assertion-set bytes as a new workspace entry.
// It never regenerates through a draft, so relative references and formatting
// the reviewer accepted stay exactly as they were.
func Export(root string, data []byte, output string) (Saved, error) {
	if _, err := assertion.Decode(data); err != nil {
		return Saved{}, err
	}
	if artifactpath.EntryName(output) != nil || !printable(output, MaxEntryBytes) {
		return Saved{}, errors.New("an assertion set is written to one new entry of the open workspace")
	}
	destination, err := artifactpath.Destination(artifactpath.JoinReference(root, output))
	if err != nil {
		return Saved{}, errors.New("an assertion set must be written outside every retained artifact of this workspace")
	}
	if err := write(destination, data); err != nil {
		return Saved{}, err
	}
	written, err := Import(root, output)
	if err != nil || string(written) != string(data) {
		return Saved{}, errors.New("the assertion set could not be read back from the workspace")
	}
	sum := sha256.Sum256(written)
	return Saved{Output: output, Identity: hex.EncodeToString(sum[:])}, nil
}

func write(destination string, data []byte) error {
	return assertionFile.Create(destination, data)
}

// assertionFile is how an assertion set is created, through the shared
// document store.
var assertionFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Destination: ErrCannotWrite,
		Create:      ErrCannotWrite,
		Write:       errors.New("the assertion set could not be written into the workspace"),
	},
}

func check(draft Draft) error {
	if draft.Schema != Schema {
		return errors.New("assertion set draft declares a contract version this release does not read")
	}
	if draft.Name != "" {
		if err := checkName(draft.Name); err != nil {
			return err
		}
	}
	return checkClauses(draft.Assertions)
}

func checkName(name string) error {
	if !printable(name, MaxNameBytes) {
		return errors.New("an assertion set name is 1 to 256 bytes of printable text")
	}
	return nil
}

func checkClauses(clauses []Clause) error {
	if len(clauses) > assertion.MaxAssertions {
		return errors.New("an assertion set holds at most 256 assertions")
	}
	ids := make(map[string]bool, len(clauses))
	for _, clause := range clauses {
		if clause.ID == "" || ids[clause.ID] {
			return errors.New("an assertion is named once")
		}
		ids[clause.ID] = true
		if clause.Operator == "" {
			return errors.New("an assertion names one of the operators this contract carries")
		}
	}
	return nil
}

func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
