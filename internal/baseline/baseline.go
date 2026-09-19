// Package baseline retains explicit local approvals of regression expectations.
// A revision is an authored specification, never a passing result promoted by a run.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/testrunner"
)

const Schema = "readmit-baseline/v1"
const MaxBytes = 2 << 20

// Revision records a complete specification and a digest pin to its predecessor.
// Approver is a local declaration, not authenticated organizational identity.
type Revision struct {
	Schema    string          `json:"schema"`
	Revision  int             `json:"revision"`
	Parent    string          `json:"parent"`
	Spec      testrunner.Spec `json:"spec"`
	Review    string          `json:"review"`
	Approver  string          `json:"approver"`
	Rationale string          `json:"rationale"`
}

type Change struct {
	Part   string `json:"part"`
	Kind   string `json:"kind"`
	Before string `json:"before,omitzero"`
	After  string `json:"after,omitzero"`
}

// Comparison is a private-by-default view of all changed specification parts.
// Before and After are quoted JSON strings, present only after explicit opt-in.
type Comparison struct {
	Schema      string   `json:"schema"`
	Identity    string   `json:"identity"`
	Revision    int      `json:"revision"`
	Parent      string   `json:"parent"`
	ValuesShown bool     `json:"values_shown"`
	Changes     []Change `json:"changes"`
}

func canonical(v any) []byte    { data, _ := json.Marshal(v, json.Deterministic(true)); return data }
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func hexDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func text(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func reviewIdentity(spec testrunner.Spec, parent string, revision int) string {
	return digest(canonical(struct {
		Schema   string
		Spec     testrunner.Spec
		Parent   string
		Revision int
	}{"readmit-baseline-review/v1", spec, parent, revision}))
}

func (r Revision) Validate() error {
	if _, err := testrunner.DecodeSpec(canonical(r.Spec)); err != nil {
		return errors.New("invalid or oversized baseline specification")
	}
	if r.Schema != Schema || r.Revision < 1 || r.Revision > 1000000 || (r.Revision == 1 && r.Parent != "") || (r.Revision > 1 && !hexDigest(r.Parent)) || !text(r.Approver, 256) || !text(r.Rationale, 8192) || r.Spec.Validate() != nil || r.Review != reviewIdentity(r.Spec, r.Parent, r.Revision) {
		return errors.New("invalid baseline revision or changed approval commitment")
	}
	return nil
}
func (r Revision) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data := canonical(r)
	if len(data) > MaxBytes {
		return nil, errors.New("baseline exceeds size limit")
	}
	return append(data, '\n'), nil
}
func Decode(data []byte) (Revision, error) {
	var r Revision
	if len(data) > MaxBytes {
		return r, errors.New("baseline exceeds size limit")
	}
	var required struct {
		Schema    *string        `json:"schema"`
		Revision  *int           `json:"revision"`
		Parent    *string        `json:"parent"`
		Spec      jsontext.Value `json:"spec"`
		Review    *string        `json:"review"`
		Approver  *string        `json:"approver"`
		Rationale *string        `json:"rationale"`
	}
	if json.Unmarshal(data, &required) != nil || required.Schema == nil || required.Revision == nil || required.Parent == nil || len(required.Spec) == 0 || required.Review == nil || required.Approver == nil || required.Rationale == nil || json.Unmarshal(data, &r, json.RejectUnknownMembers(true)) != nil {
		return r, errors.New("invalid baseline JSON: all members are required and unknown members are refused")
	}
	spec, err := testrunner.DecodeSpec(required.Spec)
	if err != nil {
		return Revision{}, err
	}
	r.Spec = spec
	return r, r.Validate()
}

// Review never writes and never evaluates a test. Every full assertion is
// compared, including its scope/operator; non-assertion changes stay separate.
func Review(data []byte, previous *Revision, showValues bool) (Comparison, error) {
	spec, err := testrunner.DecodeSpec(data)
	if err != nil {
		return Comparison{}, err
	}
	result := Comparison{Schema: "readmit-baseline-review/v1", Revision: 1, ValuesShown: showValues, Changes: []Change{}}
	var old *testrunner.Spec
	if previous != nil {
		if err := previous.Validate(); err != nil {
			return Comparison{}, err
		}
		if previous.Revision >= 1000000 {
			return Comparison{}, errors.New("baseline revision limit reached")
		}
		result.Parent = digest(canonical(*previous))
		result.Revision = previous.Revision + 1
		old = &previous.Spec
	}
	result.Identity = reviewIdentity(spec, result.Parent, result.Revision)
	add := func(part string, before, after any) {
		if string(canonical(before)) == string(canonical(after)) {
			return
		}
		c := Change{Part: part, Kind: "changed"}
		if before == nil {
			c.Kind = "added"
		}
		if after == nil {
			c.Kind = "removed"
		}
		if showValues {
			if before != nil {
				c.Before = string(canonical(before))
			}
			if after != nil {
				c.After = string(canonical(after))
			}
		}
		result.Changes = append(result.Changes, c)
	}
	before := testrunner.Spec{}
	if old != nil {
		before = *old
	}
	for _, part := range []struct {
		name          string
		before, after any
	}{
		{"schema", before.Schema, spec.Schema},
		{"name", before.Name, spec.Name},
		{"input", before.Input, spec.Input},
		{"target", before.Target, spec.Target},
		{"setup", before.Setup, spec.Setup},
		{"observation", before.Observation, spec.Observation},
	} {
		if old == nil {
			part.before = nil
		}
		add(part.name, part.before, part.after)
	}
	prior := map[string]testrunner.Assertion{}
	if old != nil {
		for _, a := range old.Assertions {
			prior[a.ID] = a
		}
	}
	for _, a := range spec.Assertions {
		if b, ok := prior[a.ID]; ok {
			add("assertion:"+a.ID, b, a)
			delete(prior, a.ID)
		} else {
			add("assertion:"+a.ID, nil, a)
		}
	}
	if old != nil {
		for _, a := range old.Assertions {
			if _, ok := prior[a.ID]; ok {
				add("assertion:"+a.ID, a, nil)
			}
		}
	}
	// Assertion order is part of the exact specification, even when all values match.
	if old != nil {
		ids := func(s testrunner.Spec) []string {
			out := []string{}
			for _, a := range s.Assertions {
				out = append(out, a.ID)
			}
			return out
		}
		add("assertion_order", ids(*old), ids(spec))
	}
	return result, nil
}

// Approve binds an explicit review decision to the exact candidate and parent.
func Approve(data []byte, previous *Revision, identity, approver, rationale string) (Revision, error) {
	review, err := Review(data, previous, false)
	if err != nil {
		return Revision{}, err
	}
	if identity == "" || identity != review.Identity {
		return Revision{}, errors.New("baseline review changed; review the candidate and parent again")
	}
	spec, err := testrunner.DecodeSpec(data)
	if err != nil {
		return Revision{}, err
	}
	r := Revision{Schema: Schema, Revision: review.Revision, Parent: review.Parent, Spec: spec, Review: identity, Approver: approver, Rationale: rationale}
	return r, r.Validate()
}

// ReadBytes accepts bounded regular files only and never discloses their paths.
func ReadBytes(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("baseline input must be a readable regular file, not a symlink")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open baseline input")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return nil, errors.New("baseline input changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read baseline input within its size limit")
	}
	return data, nil
}
func Read(path string) (Revision, error) {
	data, err := ReadBytes(path, MaxBytes)
	if err != nil {
		return Revision{}, err
	}
	return Decode(data)
}

// Save exclusively creates a revision outside retained evidence. Interrupted
// writes remain incomplete and are refused by Read; an existing file is never replaced.
func Save(path string, r Revision) error {
	data, err := r.Encode()
	if err != nil {
		return err
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot create baseline; destination must be new and writable")
	}
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot finish baseline; incomplete file retained")
	}
	return nil
}

// JSON is the common CLI rendering; encoding escapes controls in opt-in values.
func (c Comparison) JSON() []byte { return append(canonical(c), '\n') }
func (r Revision) Identity() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return digest(canonical(r)), nil
}

// Inspect renders a retained revision without needing its source specification.
// It lists all pinned parts as additions, not as a claim about a missing parent.
func Inspect(r Revision, showValues bool) (Comparison, error) {
	if err := r.Validate(); err != nil {
		return Comparison{}, err
	}
	report, err := Review(canonical(r.Spec), nil, showValues)
	if err != nil {
		return Comparison{}, err
	}
	report.Identity = r.Review
	report.Parent = r.Parent
	report.Revision = r.Revision
	return report, nil
}
