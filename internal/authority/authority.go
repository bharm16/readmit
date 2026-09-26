// Package authority resolves an EI/HD assigning-authority tuple — the
// namespace, or the universal identifier and type, that one HL7 field names —
// to the configured key one operator configuration asserts for it. It is the
// one home of that rule: a diagnosis configuration and a correlation rules
// document declare mappings through this package's decoder and validator, and
// both resolve what they read against this package's table, so two callers
// cannot come to hold two notions of when two authorities are the same.
//
// A mapping is an explicit customer assertion of equivalence: the key it
// assigns is what makes two occurrences' authorities comparable. An
// identifier string alone never establishes equivalence across different or
// unknown authorities, and an authority no mapping names resolves nothing.
package authority

import (
	"encoding/json/v2"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxMappings bounds the mappings one configuration may declare. It is the
// bound the diagnosis configuration and the correlation rules already share;
// a document past it is refused, never truncated.
const MaxMappings = 128

// token is the grammar every mapping key is held to: a bounded identifier
// with no leading punctuation.
var token = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,127}$`)

// Parts is one assigning-authority tuple as it appears on the wire: the
// namespace, the universal identifier and its type, in that order. An EI or
// HD field names an authority through a complete tuple, never through a
// string alone.
type Parts struct {
	Namespace       string
	UniversalID     string
	UniversalIDType string
}

// Complete reports whether the tuple names an authority at all: a namespace,
// or a universal identifier together with its type. A universal identifier
// without its type, or a type without its identifier, is half of an
// assertion, and half of one is none — even beside a namespace.
func (p Parts) Complete() bool {
	return p.UniversalID == "" == (p.UniversalIDType == "") && (p.Namespace != "" || p.UniversalID != "")
}

// Mapping is one declared equivalence: the key one configuration assigns to
// one assigning authority. It is the entry a readmit-diagnose-config/v1
// document lists under namespaces and a readmit-correlation-rules/v1 document
// lists under authorities; the JSON members and the rules about them are
// spelled once, here, so the two contracts cannot drift apart.
type Mapping struct {
	Key             string `json:"key"`
	Namespace       string `json:"namespace"`
	UniversalID     string `json:"universal_id"`
	UniversalIDType string `json:"universal_id_type"`
}

// Defect names the one way a declared mapping, or a declared set of them, is
// refused. A reader maps each defect onto its own contract's wording.
type Defect int

const (
	// Valid: nothing is wrong.
	Valid Defect = iota
	// OverBound: the set declares more than MaxMappings mappings.
	OverBound
	// Absent: the bytes do not declare a key at all, or cannot be read as
	// one mapping.
	Absent
	// UnknownMember: the bytes declare a member no mapping spells.
	UnknownMember
	// Invalid: a key is outside the token grammar, or a part is not bounded
	// printable UTF-8.
	Invalid
	// Incomplete: a mapping names neither a namespace nor a universal
	// identifier and type together.
	Incomplete
	// Duplicate: one authority tuple is mapped twice.
	Duplicate
)

// Decode reads one mapping exactly as written, in two passes: presence
// first, so an omitted key cannot read as the empty string, then the same
// bytes again refusing any member beyond the four and any value a string
// position cannot hold. Whether an absent key is a decoding refusal or a
// validation refusal is the reading contract's own decision; this answers
// what the bytes declare.
func Decode(data []byte) (Mapping, Defect) {
	var required struct {
		Key *string `json:"key"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Key == nil {
		return Mapping{}, Absent
	}
	var decoded struct {
		Key             string `json:"key"`
		Namespace       string `json:"namespace"`
		UniversalID     string `json:"universal_id"`
		UniversalIDType string `json:"universal_id_type"`
	}
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return Mapping{}, UnknownMember
	}
	return Mapping(decoded), Valid
}

// Validate checks a declared mapping set once: the bound of MaxMappings, the
// token grammar of every key, the safety of every part, tuple completeness
// and distinctness. It answers the first defect in that order, so a reader
// refusing a document names the same refusal it always named.
func Validate(mappings []Mapping) Defect {
	if len(mappings) > MaxMappings {
		return OverBound
	}
	seen := make(map[Parts]bool, len(mappings))
	for _, mapping := range mappings {
		parts := Parts{mapping.Namespace, mapping.UniversalID, mapping.UniversalIDType}
		if !token.MatchString(mapping.Key) || !SafeValue(mapping.Namespace) || !SafeValue(mapping.UniversalID) || !SafeValue(mapping.UniversalIDType) {
			return Invalid
		}
		if !parts.Complete() {
			return Incomplete
		}
		if seen[parts] {
			return Duplicate
		}
		seen[parts] = true
	}
	return Valid
}

// SafeValue bounds what a mapping may say: valid UTF-8 with no control
// character, because a document somebody imported must not be able to drive
// the terminal a diagnostic is displayed on.
func SafeValue(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// Table resolves assigning-authority tuples to the keys a configuration
// assigns them. It is built once from validated mappings and read by every
// rule that asks an identifier whom its authority says it belongs to.
type Table struct {
	keys map[Parts]string
}

// NewTable indexes a declared mapping set for resolution. Distinctness is
// the reader's declaration; a table built from mappings this package's
// Validate refused keeps the last mapping it saw for a tuple.
func NewTable(mappings []Mapping) *Table {
	table := &Table{keys: make(map[Parts]string, len(mappings))}
	for _, mapping := range mappings {
		table.keys[Parts{mapping.Namespace, mapping.UniversalID, mapping.UniversalIDType}] = mapping.Key
	}
	return table
}

// Resolve answers the key one configuration asserts for an authority, and
// whether it configured that authority at all. An incomplete tuple names no
// authority and resolves nothing; reporting it is the caller's refusal.
func (t *Table) Resolve(parts Parts) (string, bool) {
	if !parts.Complete() {
		return "", false
	}
	key, ok := t.keys[parts]
	return key, ok
}

// The codes every report of an unresolvable authority states. They are owned
// here so two callers cannot name the same refusal two ways.
const (
	// UnknownCode reports a missing, incomplete or explicitly null
	// assigning authority: the field names no authority at all.
	UnknownCode = "unknown_assigning_authority"
	// UnconfiguredCode reports a complete authority no mapping names.
	UnconfiguredCode = "unconfigured_assigning_authority"
)
