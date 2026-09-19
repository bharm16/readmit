// Package correlate links the occurrences of one verified case across the
// sources and identifier authorities an operator declared, and refuses to
// merge identifiers that collide. It has no clock, no random source, no
// network access and no CLI dependency, and it never changes what it reads.
//
// Rules are data that name typed Go operators, exactly as a test spec is:
// a rules document carries no command, script, expression or program path,
// and the closed set of operators below is the only thing it can ask for.
//
// Equality is not identity. A link records that a declared rule's key was
// equal in the declared scope; it never establishes that two occurrences
// describe the same encounter, and the rules that produced it are always
// reported beside it. Where equality could mean more than one thing — the
// same control ID twice in one source, the same identifier string under
// different or unestablished assigning authorities — the occurrences are
// recorded as a collision and are never merged.
package correlate

const (
	// RulesSchema is the contract a correlation rules document declares.
	RulesSchema = "readmit-correlation-rules/v1"
	// ReportSchema is the contract this package's report declares. It is a
	// derived, disposable reading of a case; no case gains a member from it.
	ReportSchema = "readmit-correlation/v1"
)

// Operator names one of the closed set of typed correlations. A document
// naming anything else is refused by the reader rather than ignored.
type Operator string

const (
	// Acknowledges resolves an acknowledgement's own declared reference to the
	// initiating occurrence that carries that control ID within scope.
	Acknowledges Operator = "acknowledges"
	// ControlID groups occurrences carrying equal message control IDs within
	// scope. Equal control IDs in one source are a collision, not a link.
	ControlID Operator = "control-id"
	// Identifier groups occurrences carrying equal clinical identifiers under
	// the same configured assigning authority within scope.
	Identifier Operator = "identifier"
)

// Scope is the boundary a rule compares within. Nothing is compared across a
// scope boundary, so equal bytes in two scopes never become one link.
type Scope string

const (
	// SourceScope compares within one declared case source.
	SourceScope Scope = "source"
	// SessionScope compares within the recorded session the case declares.
	// A case that declares no session does not acquire one: the rule is not
	// applied, and the report says so.
	SessionScope Scope = "session"
	// DeclaredScope compares within the exact source IDs the rule lists.
	DeclaredScope Scope = "declared"
)

// Linkage separates what the evidence itself declares from what a configured
// rule inferred by comparing keys.
type Linkage string

const (
	// Observed: one occurrence's own bytes name the identifier the other
	// occurrence declares, and exactly one candidate carried it in scope.
	Observed Linkage = "observed"
	// Inferred: a configured rule found equal keys. No occurrence refers to
	// the other, and readmit asserts no causal or chronological relation.
	Inferred Linkage = "inferred"
)

// Authority maps one assigning authority, exactly as it appears on the wire,
// onto an operator-chosen key. Two authorities sharing a key is an explicit
// customer assertion of equivalence; identifier strings never establish it.
type Authority struct {
	Key             string `json:"key"`
	Namespace       string `json:"namespace"`
	UniversalID     string `json:"universal_id"`
	UniversalIDType string `json:"universal_id_type"`
}

// Rule is one correlation an operator declared. Sources belong to the
// declared scope alone; Value and Authority belong to the identifier operator
// alone. A member an operator does not use is refused rather than ignored.
type Rule struct {
	ID        string   `json:"id"`
	Operator  Operator `json:"operator"`
	Scope     Scope    `json:"scope"`
	Sources   []string `json:"sources,omitzero"`
	Value     string   `json:"value,omitzero"`
	Authority []string `json:"authority,omitzero"`
}

// Rules is one correlation rules document exactly as written.
type Rules struct {
	Schema      string      `json:"schema"`
	Authorities []Authority `json:"authorities,omitzero"`
	Rules       []Rule      `json:"rules"`
}

// Reference names one occurrence of the case. It carries no field bytes: an
// identifier copied out of a message is the same patient data it was inside
// the message, so a correlation report holds none of them.
type Reference struct {
	Occurrence string `json:"occurrence"`
	SourceID   string `json:"source_id"`
	Kind       string `json:"kind"`
}

// Link is one correlation the declared rules produced.
type Link struct {
	ID          string      `json:"id"`
	Rule        string      `json:"rule"`
	Operator    Operator    `json:"operator"`
	Linkage     Linkage     `json:"linkage"`
	Authority   string      `json:"authority,omitzero"`
	Occurrences []Reference `json:"occurrences"`
}

// Collision is equal identifier bytes that did not become one link. Declaring
// is the occurrence whose own declaration could not be resolved, where one
// exists; Occurrences are the candidates or the colliding occurrences.
type Collision struct {
	Rule        string      `json:"rule"`
	Operator    Operator    `json:"operator"`
	Reason      string      `json:"reason"`
	Declaring   *Reference  `json:"declaring,omitzero"`
	Occurrences []Reference `json:"occurrences"`
}

// Collision reasons. Every one of them means the same thing about the
// evidence: readmit found equality it could not stand behind, and merged
// nothing.
const (
	// DuplicateControlID: one source carries the same control ID more than
	// once, so no observation of it can be told from another.
	DuplicateControlID = "duplicate_control_id"
	// AmbiguousAcknowledgement: the acknowledgement's declared reference
	// matches more than one occurrence in scope; none is selected.
	AmbiguousAcknowledgement = "ambiguous_acknowledgement"
	// UnqualifiedIdentifier: equal identifier strings whose assigning
	// authority is missing, explicitly null, or not configured.
	UnqualifiedIdentifier = "unqualified_identifier"
	// DistinctAuthorities: equal identifier strings under different
	// configured authorities, kept in separate links on purpose.
	DistinctAuthorities = "distinct_authorities"
)

// Unsupported is evidence or a declaration the rules could not be applied to.
// It never passes: an occurrence listed here is in no link.
type Unsupported struct {
	Code       string `json:"code"`
	Rule       string `json:"rule,omitzero"`
	Occurrence string `json:"occurrence,omitzero"`
	Field      string `json:"field,omitzero"`
	Detail     string `json:"detail"`
}

// RuleReport restates one declared rule and what it reached. Considered counts
// the occurrences the rule obtained a usable key from; Linked counts those that
// ended in at least one link; Unlinked is the remainder, which includes every
// occurrence a collision over its own key held back. An occurrence the rule
// obtained no usable key from is in none of the three: it is listed under
// Unsupported, because counting it as unlinked would claim the rule reached
// evidence it could not read.
type RuleReport struct {
	ID         string   `json:"id"`
	Operator   Operator `json:"operator"`
	Scope      Scope    `json:"scope"`
	Sources    []string `json:"sources,omitzero"`
	Applied    bool     `json:"applied"`
	Considered int      `json:"considered"`
	Linked     int      `json:"linked"`
	Unlinked   int      `json:"unlinked"`
}

type Summary struct {
	Occurrences int `json:"occurrences"`
	Links       int `json:"links"`
	Observed    int `json:"observed"`
	Inferred    int `json:"inferred"`
	Collisions  int `json:"collisions"`
	Unsupported int `json:"unsupported"`
}

// Report is the inspectable consumer boundary. It restates the case identity
// and the SHA-256 of the exact rules it ran, so a reader can tell which
// declarations produced which link, and carries no field bytes at all.
type Report struct {
	Schema          string        `json:"schema"`
	CaseIdentity    string        `json:"case_identity"`
	RulesSHA256     string        `json:"rules_sha256"`
	SessionDeclared bool          `json:"session_declared"`
	Rules           []RuleReport  `json:"rules"`
	Summary         Summary       `json:"summary"`
	Links           []Link        `json:"links"`
	Collisions      []Collision   `json:"collisions"`
	Unsupported     []Unsupported `json:"unsupported"`
	// Scope is the boundary statement a reader needs before acting on the
	// report: what a link is and what it is not. It is prose, not a Scope.
	Scope string `json:"scope"`
}
