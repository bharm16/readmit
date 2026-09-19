// Package reproducer extracts a compact, repeatable reproducer out of one
// verified case: the occurrences a person selected, the setup dependencies
// those occurrences need, and the field edits applied to them.
//
// Nothing here changes a byte of the case it reads. A reproducer is new derived
// evidence written beside the original under a manifest that says exactly what
// was retained, why it was retained, and where every edit landed. The plan that
// produced it is an ordered list of typed operators interpreted by this package
// ([ADR-0003]): there is no rule language, no expression and no script, and the
// last step is removed again by Undo rather than by editing a document.
//
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
package reproducer

import "github.com/bharm16/readmit/internal/hl7"

const (
	// PlanSchema is the editable plan: what to extract and how to edit it.
	PlanSchema = "readmit-reproducer-plan/v1"

	// ManifestSchema is the transformation manifest written beside the derived
	// case. ManifestName is its fixed name and CaseName the derived bundle
	// beside it, so a reproducer directory is located the way every other
	// artifact of this product is located.
	ManifestSchema = "readmit-reproducer/v1"
	ManifestName   = "reproducer.json"
	CaseName       = "case"

	// Derivation is what the derived bundle declares about itself. The case
	// contract carries the name of the transformation that wrote it; this is
	// the name of this one.
	Derivation = "readmit-reproducer/v1"
)

// The operators a plan is built from. Each names the one thing it does, and a
// step carrying a member another operator uses is refused rather than ignored.
const (
	// SelectOccurrence retains one occurrence of the case.
	SelectOccurrence = "select-occurrence/v1"

	// DropOccurrence stops retaining one occurrence, whether a person selected
	// it or a dependency step reached it.
	DropOccurrence = "drop-occurrence/v1"

	// IncludeAcknowledgements retains the counterpart of every retained
	// occurrence that the case itself correlated: the acknowledgement of a
	// retained message, and the message a retained acknowledgement names.
	IncludeAcknowledgements = "include-acknowledgements/v1"

	// IncludePriorIdentity retains every earlier occurrence of the same source
	// whose declared identity equals a retained occurrence's. This is what a
	// setup dependency is in recorded evidence: the occurrences that came
	// before and are about the same thing.
	IncludePriorIdentity = "include-prior-identity/v1"

	// SetField replaces the bytes of one declared position with an explicit
	// scalar. ClearField removes them, leaving the position explicitly empty.
	SetField   = "set-field/v1"
	ClearField = "clear-field/v1"
)

// Why one occurrence is retained. A dependency reason also names the retained
// occurrence that needed it, so a reproducer never holds evidence nobody can
// account for.
const (
	Selected          = "selected"
	Acknowledgement   = "acknowledgement"
	PriorIdentity     = "prior-identity"
	AmbiguousACK      = "ambiguous-acknowledgement"
	UnmatchedACK      = "unmatched-acknowledgement"
	UnacknowledgedMsg = "unacknowledged-message"
	NoIdentity        = "no-declared-identity"
	Undecodable       = "undecodable-occurrence"
)

// Bounds. A plan or a reproducer past one of these is refused, never truncated.
const (
	MaxSteps        = 256
	MaxOccurrences  = 256
	MaxIdentityKeys = 8
	MaxValueBytes   = 1024

	maxPlanBytes     = 256 << 10
	maxManifestBytes = 4 << 20
)

// Step is one typed operator of a plan. Only the members its operator declares
// may be present; the rest are refused rather than ignored, so a step always
// means exactly one thing.
type Step struct {
	Operator   string   `json:"operator"`
	Occurrence string   `json:"occurrence,omitzero"`
	Selector   string   `json:"selector,omitzero"`
	Identity   []string `json:"identity,omitzero"`
	Value      string   `json:"value,omitzero"`
}

// Plan is the ordered transformation a reproducer is built from, bound to the
// identity of the case it was authored against. Replaying the steps in order
// over that case reproduces the same resolution, and removing the last step is
// the whole of undo: nothing else is retained about what an earlier step did.
type Plan struct {
	Schema string `json:"schema"`
	Case   string `json:"case"`
	Steps  []Step `json:"steps"`
}

// Retained is one occurrence the reproducer keeps and why it keeps it. Parent
// is its occurrence ID in the case this was derived from, and Derived the ID it
// was written under, which the writer assigns and a preview does not have yet.
type Retained struct {
	Parent     string `json:"parent"`
	Derived    string `json:"derived,omitzero"`
	Reason     string `json:"reason"`
	RequiredBy string `json:"required_by,omitzero"`
}

// Edit is one applied field change, recorded where it landed.
//
// State is what the position was before the edit — present, empty or an
// explicit HL7 null — so a person can see that a value was replaced rather than
// created. The bytes that were there are deliberately not recorded: they are
// the original evidence's own values, the manifest names the case they are
// still in, and a record of a transformation is not a second copy of what it
// transformed. Offset and Length are the span in the derived occurrence's
// payload, which is where the new bytes actually are.
type Edit struct {
	Parent   string    `json:"parent"`
	Derived  string    `json:"derived,omitzero"`
	Selector string    `json:"selector"`
	Operator string    `json:"operator"`
	State    hl7.State `json:"state"`
	Offset   int       `json:"offset"`
	Length   int       `json:"length"`
}

// Unresolved is something a dependency step reached and could not settle. It is
// reported rather than guessed: an acknowledgement the case recorded as
// ambiguous names several candidate messages and this release selects none of
// them, and an occurrence nothing decoded declares no identity to match on.
type Unresolved struct {
	Occurrence string `json:"occurrence"`
	Reason     string `json:"reason"`
}

// Resolution is what one plan means over one verified case, before anything is
// written. A preview and a build resolve the same way, so what a person is
// shown is what a build would retain.
type Resolution struct {
	Occurrences []Retained   `json:"occurrences"`
	Edits       []Edit       `json:"edits"`
	Unresolved  []Unresolved `json:"unresolved"`
}

// Artifact names one case bundle by the contract it declares and the identity
// its own reader verified.
type Artifact struct {
	Schema   string `json:"schema"`
	Identity string `json:"identity"`
}

// Manifest is the transformation manifest: the parent hash, the derived hash,
// the plan as it was applied, and what that plan resolved to.
//
// It is local working evidence about a transformation, written beside the
// derived case and never inside it. A copy of the derived bundle alone carries
// nothing about the case it came from, exactly as
// [ADR-0004] requires; this document is where the lineage is, so it stays on
// the machine that produced it.
//
// [ADR-0004]: docs/adr/0004-derived-evidence-and-generated-export.md
type Manifest struct {
	Schema      string       `json:"schema"`
	Parent      Artifact     `json:"parent"`
	Derived     Artifact     `json:"derived"`
	Plan        Plan         `json:"plan"`
	Occurrences []Retained   `json:"occurrences"`
	Edits       []Edit       `json:"edits"`
	Unresolved  []Unresolved `json:"unresolved"`
}
