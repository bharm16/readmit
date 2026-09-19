package diff

import "github.com/bharm16/readmit/internal/hl7"

// NormalizationSchema is the contract this second report declares. It exists
// because a normalization policy is a new meaning rather than a new member:
// readmit-diff/v1 keeps exactly the shape it shipped with, and the raw
// comparison it carries is never edited by a rule.
const NormalizationSchema = "readmit-normalization/v1"

const normalizationScope = "A normalization policy decides which differences a reader is asked to look at; it never decides that two collections agree. Every difference this comparison found is listed here, including every one a rule suppressed, beside the rule that suppressed it. A field this build could not decode is listed as uncompared rather than as a difference, and no rule suppresses one. A rule that cannot read the values it was scoped to settles nothing and is undecided, which is neither agreement nor a reason to conceal a difference. No value, no alignment-key value and no message byte appears in this report in any mode. Occurrences missing, inserted, ambiguous or unaligned are not differences a rule can address and none is suppressed here. The raw field comparison remains readmit-diff/v1, unchanged by anything stated in this document, and no outcome here is a verdict."

// RuleReport is one authored rule as it was applied. Every rule appears in
// every rendering, including a rule that addressed nothing, because a policy
// whose effect a reader cannot see is worse than no policy at all.
type RuleReport struct {
	ID         string `json:"id"`
	Selector   string `json:"selector"`
	Operator   string `json:"operator"`
	Precision  string `json:"precision,omitzero"`
	Tolerance  string `json:"tolerance,omitzero"`
	Compared   int    `json:"compared"`
	Suppressed int    `json:"suppressed"`
	Retained   int    `json:"retained"`
	Undecided  int    `json:"undecided"`
}

// Difference is one field the comparison reported and what the policy did about
// it. Status is the raw comparison's own word for the field — `changed`, or
// `uncompared` for one this build did not decode — so the fourth state the
// field comparison distinguishes is not lost on the way into this report.
//
// It carries the canonical selector, the bundled dictionary label and each
// side's decoded state, the same three things a comparison row already shows,
// and it has no member that could hold a value, in any mode, by construction.
type Difference struct {
	LeftOccurrence  string    `json:"left_occurrence"`
	RightOccurrence string    `json:"right_occurrence"`
	Selector        string    `json:"selector"`
	Name            string    `json:"name,omitzero"`
	Status          string    `json:"status"`
	LeftState       hl7.State `json:"left_state"`
	RightState      hl7.State `json:"right_state"`
	Outcome         string    `json:"outcome"`
	Rule            string    `json:"rule,omitzero"`
	Reason          string    `json:"reason,omitzero"`
}

// NormalizationSummary counts what the policy did and what it could not touch.
// Differences is every field reported as differing before any rule was
// consulted, so the share of a quiet comparison the policy is responsible for
// stays visible. Uncompared is counted apart from it, because a field this
// build did not decode is an evidence gap rather than a difference.
type NormalizationSummary struct {
	Paired      int `json:"paired"`
	Differences int `json:"differences"`
	Uncompared  int `json:"uncompared"`
	Suppressed  int `json:"suppressed"`
	Retained    int `json:"retained"`
	Undecided   int `json:"undecided"`
	Unaddressed int `json:"unaddressed"`
	Inserted    int `json:"inserted"`
	Missing     int `json:"missing"`
	Ambiguous   int `json:"ambiguous"`
	Unaligned   int `json:"unaligned"`
}

// NormalizationReport is the inspectable boundary for a policy-scoped reading
// of one comparison. It restates the alignment it ran under, because a report
// that hid how records were paired would hide the assumption that matters most.
type NormalizationReport struct {
	Schema       string               `json:"schema"`
	PolicySchema string               `json:"policy_schema"`
	Scope        string               `json:"scope"`
	Boundary     Boundary             `json:"boundary"`
	Left         InputSummary         `json:"left"`
	Right        InputSummary         `json:"right"`
	Alignment    string               `json:"alignment"`
	Keys         []string             `json:"keys"`
	Fields       []string             `json:"fields"`
	Rules        []RuleReport         `json:"rules"`
	Summary      NormalizationSummary `json:"summary"`
	Differences  []Difference         `json:"differences"`
	Unsupported  []Unsupported        `json:"unsupported"`
}
