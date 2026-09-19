// Package transform previews relationship-preserving replay transformations
// over one verified case: renaming the identifiers a correlation rule relates,
// shifting the dates every occurrence declares, and reordering, duplicating or
// dropping occurrences of the sequence a replay would send.
//
// Nothing here writes a byte. A preview is a derived, disposable reading of
// evidence the case reader already verified, and it produces no case, no run
// and no derived bundle: [ADR-0004]'s derivation names are untouched, and so is
// every contract the reproducer editor owns.
//
// Relating is not reimplemented. Which occurrences belong together is answered
// by [github.com/bharm16/readmit/internal/correlate] under the rules an
// operator declared, and a rename is assigned per relation rather than per
// occurrence: occurrences one rule related receive one surrogate, so the
// relation survives the rename, and occurrences it related to nothing receive
// their own, so no relation is invented. Equality the rules could not stand
// behind is never decided here — it refuses the rename and says so.
//
// A plan is data interpreted by typed Go operators ([ADR-0003]): there is no
// rule language, no expression and no script, and an operator this release does
// not perform is refused by name rather than ignored.
//
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
// [ADR-0004]: docs/adr/0004-derived-evidence-and-generated-export.md
package transform

import (
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
)

const (
	// PlanSchema is the declared transformation: which relations to rename,
	// how far to shift dates, and what to do to the sequence.
	PlanSchema = "readmit-transform-plan/v1"

	// PreviewSchema is what that plan means over one verified case. It is a
	// report, not evidence: nothing reads it back as a case and no case gains
	// a member from it.
	PreviewSchema = "readmit-transform-preview/v1"
)

// The operators a plan is built from. Each names the one thing it does, and a
// step carrying a member another operator uses is refused rather than ignored.
const (
	// DropOccurrence removes one entry from the sequence. The occurrence stays
	// in the case; the sequence a replay would send no longer holds it.
	DropOccurrence = "drop-occurrence/v1"

	// DuplicateOccurrence repeats one entry immediately after itself. The copy
	// is a second entry naming the same parent occurrence and the same case
	// source, so a repeated message is still traceable to the evidence it came
	// from.
	DuplicateOccurrence = "duplicate-occurrence/v1"

	// ReorderOccurrence moves one entry to an explicit one-based position of
	// the sequence.
	ReorderOccurrence = "reorder-occurrence/v1"

	// RebaseIdentifiers renames the values one declared correlation rule
	// relates. One surrogate is assigned per relation, in first-occurrence
	// order, so occurrences the rule related stay related and occurrences it
	// did not stay apart.
	RebaseIdentifiers = "rebase-identifiers/v1"

	// ShiftDates moves every supported timestamp of every entry by one
	// explicit duration, so the intervals between occurrences are unchanged.
	ShiftDates = "shift-dates/v1"
)

// Why a position was not transformed, or a relation not preserved. Every one
// of these is reported rather than decided: a preview that could not stand
// behind a rename says so instead of renaming anyway.
const (
	// UnqualifiedIdentifier: the rules found equal identifier bytes whose
	// assigning authority is missing, explicitly null or unconfigured. Renaming
	// them apart would break a relation nobody established and renaming them
	// together would assert one, so neither is done. It is this contract's own
	// spelling of the collision a correlation report calls
	// [correlate.UnqualifiedIdentifier]; the two documents are read separately
	// and neither borrows the other's vocabulary.
	UnqualifiedIdentifier = "unqualified-identifier"

	// UnmatchedAcknowledgement: the acknowledgement names no message of this
	// case. It is left exactly as it is, because renaming the messages that are
	// here cannot change what it refers to.
	UnmatchedAcknowledgement = "unmatched-acknowledgement"

	// UndecodableOccurrence: nothing decoded this occurrence, so it declares no
	// position to transform. Its bytes stay in the sequence exactly as they are.
	UndecodableOccurrence = "undecodable-occurrence"

	// NoDeclaredValue: the occurrence declares none of the positions the rule
	// reads, so there is nothing of that relation in it to rename.
	NoDeclaredValue = "no-declared-value"

	// UnverifiedCombination: the pinned pack does not declare this HL7 version
	// and message family supported at the parse level, so nothing here verifies
	// that the transformed sequence is readable under it. A plan pinning no pack
	// reports every combination this way, because it validated against nothing.
	UnverifiedCombination = "unverified-combination"

	// UnshiftedPositions: a date shift moves the positions this release can read
	// back as whole seconds and leaves every other date and timestamp field
	// exactly as it is. Stated once per preview, because nothing here can
	// establish which other fields carry a date.
	UnshiftedPositions = "unshifted-positions"

	// SeveredByDrop: a relation whose occurrences are not all in the sequence
	// any more. The relation is not preserved and the preview says which
	// occurrences are missing rather than reporting the remainder as intact.
	SeveredByDrop = "severed-by-drop"

	// NotRetained: none of the relation's occurrences is in the sequence.
	NotRetained = "not-retained"
)

// Bounds. A plan or a sequence past one of these is refused, never truncated.
const (
	MaxSteps   = 256
	MaxEntries = 1024
	MaxShift   = 10 * 365 * 24 * 60 * 60 // seconds
	// MaxPlanBytes bounds the plan document a reader decodes.
	MaxPlanBytes = 256 << 10

	// MaxPreviewBytes bounds one rendered preview, as the correlation report
	// the relations are read from is bounded.
	MaxPreviewBytes = 32 << 20
)

// Step is one typed operator of a plan. Only the members its operator declares
// may be present; the rest are refused rather than ignored, so a step always
// means exactly one thing.
type Step struct {
	Operator string `json:"operator"`
	Entry    string `json:"entry,omitzero"`
	Position int    `json:"position,omitzero"`
	Rule     string `json:"rule,omitzero"`
	Shift    string `json:"shift,omitzero"`
}

// Plan is the ordered transformation, bound to the evidence and the
// declarations it was authored against: the verified identity of the case, and
// the SHA-256 of the exact correlation rules whose relations it preserves. A
// plan applied to anything else is refused rather than applied to whatever
// happens to be there.
//
// Profile, when declared, pins the profile pack the preview validates the
// transformed sequence against. A different version of the same pack is a
// different pack.
type Plan struct {
	Schema  string               `json:"schema"`
	Case    string               `json:"case"`
	Rules   string               `json:"rules"`
	Profile profilepack.Identity `json:"profile,omitzero"`
	Steps   []Step               `json:"steps"`
}

// Artifact names one case by the contract it declares and the identity its own
// reader verified.
type Artifact struct {
	Schema   string `json:"schema"`
	Identity string `json:"identity"`
}

// Entry is one occurrence of the sequence a replay would send. ID is this
// entry's own name inside the plan, assigned in evidence order and never
// reused; Parent and Source are the case occurrence and case source it came
// from, retained across every reorder and duplication so an edited sequence
// still maps back onto the evidence.
type Entry struct {
	ID       string `json:"id"`
	Parent   string `json:"parent"`
	Source   string `json:"source"`
	Position int    `json:"position"`
	Copy     bool   `json:"copy,omitzero"`
}

// Change is one position the transformation rewrites, recorded where it lands.
//
// State is what the position was before, so a person can see that a value was
// replaced rather than created. Group is the relation a rename assigned: two
// changes carrying one group receive one value, which is the whole of what
// "relationship-preserving" means and is why an intentional duplicate is still
// a duplicate afterwards. Length is the size of the new value.
//
// No byte of any value is recorded, before or after. A position, an operator
// and a relation number are locations; the values are the evidence's own, and
// a record of a transformation is not a second copy of what it transformed.
type Change struct {
	Entry    string    `json:"entry"`
	Parent   string    `json:"parent"`
	Operator string    `json:"operator"`
	Rule     string    `json:"rule,omitzero"`
	Selector string    `json:"selector"`
	State    hl7.State `json:"state"`
	Group    int       `json:"group,omitzero"`
	Length   int       `json:"length"`
}

// Relation is one correlation the declared rules produced, and what the edited
// sequence did to it. Preserved is true only when every occurrence of the
// relation is still in the sequence; a relation a drop broke is reported as
// broken rather than as the part of it that remains.
type Relation struct {
	Rule        string             `json:"rule"`
	Operator    correlate.Operator `json:"operator"`
	Linkage     correlate.Linkage  `json:"linkage"`
	Occurrences []string           `json:"occurrences"`
	Entries     []string           `json:"entries"`
	Preserved   bool               `json:"preserved"`
	Reason      string             `json:"reason,omitzero"`
}

// Combination is what the transformed sequence declares about itself and what
// the pinned pack says about it. Only a supported outcome passes; an unknown,
// untested or unsupported one is reported as the reason it is not a verdict.
type Combination struct {
	Version    string              `json:"version"`
	Family     string              `json:"family"`
	Entries    int                 `json:"entries"`
	Parse      profilepack.Outcome `json:"parse"`
	Labels     profilepack.Outcome `json:"labels"`
	Structural profilepack.Outcome `json:"structural"`
	Workflow   profilepack.Outcome `json:"workflow"`
}

// Unsupported is a position the transformation did not reach and a relation it
// did not decide. It never passes: a position listed here was left exactly as
// the evidence has it.
type Unsupported struct {
	Code     string `json:"code"`
	Entry    string `json:"entry,omitzero"`
	Parent   string `json:"parent,omitzero"`
	Rule     string `json:"rule,omitzero"`
	Selector string `json:"selector,omitzero"`
	Detail   string `json:"detail"`
}

// Summary totals the preview so a reader sees the shape before the detail.
type Summary struct {
	Occurrences int `json:"occurrences"`
	Entries     int `json:"entries"`
	Copies      int `json:"copies"`
	Changes     int `json:"changes"`
	Relations   int `json:"relations"`
	Preserved   int `json:"preserved"`
	Unsupported int `json:"unsupported"`
}

// Preview is what one plan means over one verified case, before anything is
// replayed. It is the whole answer: the sequence, every position the
// transformation rewrites, what happened to every declared relation, what the
// pinned pack says about the result, and everything that was left alone.
type Preview struct {
	Schema      string        `json:"schema"`
	Case        Artifact      `json:"case"`
	Plan        Plan          `json:"plan"`
	Summary     Summary       `json:"summary"`
	Sequence    []Entry       `json:"sequence"`
	Changes     []Change      `json:"changes"`
	Relations   []Relation    `json:"relations"`
	Profile     []Combination `json:"profile"`
	Unsupported []Unsupported `json:"unsupported"`
	// Scope is the boundary statement a reader needs before acting on the
	// preview: what it establishes and what it does not.
	Scope string `json:"scope"`
}
