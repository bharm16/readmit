// Package drift names why two comparable executions differ, and keeps four
// causes apart while doing it: the input that was fed in, the target it was
// sent to, the environment that evaluated it, and the rules it was evaluated
// against.
//
// This is not a second comparison engine. Which fields differ between two
// collections is [the diff engine]'s answer and stays there; no report here
// opens a payload, parses a message or names a field. A drift report answers
// the question that is left over once a difference is already visible — which
// of the four things that could have changed actually did — and it answers it
// only from what the evidence itself retained.
//
// Each cause is read from the one record that carries it. The input is the
// case identity an artifact names as its own source, together with the replay
// transformations a run declares over it. The target is the readmit-target
// configuration a run or result retained, compared part by part. The
// environment is the readmit-engine/v1 pin a durable run retained beside its
// plan: the build that executed it and the spec contract that build read. The
// rule is the semantic profile that same pin names.
//
// Three refusals matter more than any answer:
//
//   - A cause no side records is `undeclared`, never `unchanged`. Two case
//     bundles retain no target, no engine and no rule, so a comparison of them
//     can say the input changed and cannot say that nothing else did.
//   - A cause only one side records is `undecided`. Half a comparison is not a
//     comparison.
//   - A profile identity this build cannot resolve to real content is
//     `undecided`, never `unchanged`. This release bundles no profile library
//     and extracts no pack, so an identity other than the profile this build
//     implements stands for content nothing here has. Equal unresolvable names
//     are not equal rules.
//
// And the whole point of keeping them apart: when more than one cause changed,
// the attribution is `several_causes` and names all of them. Nothing here ever
// reports one change as the reason a verdict moved. A drift report carries no
// verdict at all.
//
// [the diff engine]: docs/diff.md
package drift

// Schema is the contract this report declares. It is a new document beside the
// existing ones: readmit-diff/v1, readmit-engine/v1, readmit-target/v1 and the
// profile-version contracts gain no member and change no byte, exactly as
// ADR-0003 requires of a contract that already shipped.
const Schema = "readmit-drift/v1"

// The four causes, in the order every report lists them.
const (
	InputCause       = "input"
	TargetCause      = "target"
	EnvironmentCause = "environment"
	RuleCause        = "rule"
)

// What one side records about one cause.
//
// Unreadable is a record that is present and that this build does not read —
// a pin written by a later release, for instance. It is told apart from
// Undeclared because they are opposite situations: nothing was retained, and
// something was retained that cannot be interpreted here.
const (
	Declared   = "declared"
	Undeclared = "undeclared"
	Unreadable = "unreadable"
)

// What a comparison of one cause established.
//
// Undeclared here means neither side retained the record, so there was nothing
// to compare. Undecided means there was, and it did not settle the question.
const (
	Unchanged = "unchanged"
	Changed   = "changed"
	Undecided = "undecided"
)

// How a cause was compared.
//
// Semantic is the retained record read as the contract it declares and
// compared part by part. Raw is the retained document's digest and nothing
// else: it is what remains when a record cannot be interpreted here, and it
// still answers one honest question, because two byte-identical documents
// declare the same thing whatever that is. NotCompared is no comparison at all.
const (
	Semantic    = "semantic"
	RawDocument = "raw"
	NotCompared = "not_compared"
)

// Why a cause did not settle.
const (
	// DeclaredOnOneSide is one side retaining the record and the other not.
	DeclaredOnOneSide = "declared_on_one_side"
	// RecordUnreadable is two differing documents at least one of which this
	// build does not read, so which part of it differs is unknown.
	RecordUnreadable = "record_unreadable"
	// ProfileUnresolved is a profile identity that resolves to no content
	// here, so equal identities are not established to be equal rules.
	ProfileUnresolved = "profile_unresolved"
)

// What the four outcomes together support saying.
//
// NoDeclaredChange is every cause compared and unchanged. SingleCause is
// exactly one cause changed and every other cause compared and unchanged — it
// says one named thing drifted, and it still does not say a verdict moved
// because of it. SeveralCauses is more than one. Undecided is any cause left
// undecided or undeclared, which is most comparisons and is the honest answer
// to nearly all of them.
const (
	NoDeclaredChange = "no_declared_change"
	SingleCause      = "single_cause"
	SeveralCauses    = "several_causes"
)

// The artifact kinds a side may be.
const (
	CaseKind   = "case"
	RunKind    = "run"
	ResultKind = "result"
	JobKind    = "job"
)

// UnknownRevision is what every side says about the software the receiving
// application runs. readmit records a target's configuration, never the build
// answering at it: an ACK proves something replied, not what. The word is
// written into every report rather than left out, because a missing member
// reads as an oversight and this one is a statement.
const UnknownRevision = "unknown"

// How a named profile resolves in this build.
const (
	// BundledProfile is the profile this build implements.
	BundledProfile = "bundled"
	// UnresolvedProfile is any other identity. This release bundles no profile
	// library and extracts no pack, so nothing here holds its content.
	UnresolvedProfile = "unresolved"
)

// InputSide is what one side declares about the evidence that went in.
//
// Identity is the case identity the artifact names as its own source, so a
// case, a replay of that case and a result of that replay all name the same
// string. Transformations are the replay operators a run declares over that
// case, by name only, the way `explain` already prints them. RecordedChanges
// is how many field changes those operators actually made; no changed value
// crosses this boundary, and neither does the position of one.
type InputSide struct {
	State           string   `json:"state"`
	Identity        string   `json:"identity,omitzero"`
	Transformations []string `json:"transformations"`
	RecordedChanges int      `json:"recorded_changes"`
}

// TargetSide is what one side declares about where the messages were sent.
//
// Fingerprint is the identity of the whole retained configuration, computed
// the way every other reader of this product computes it. The address itself
// is not here and is not printed anywhere in this report, exactly as the diff
// report holds the line: a changed address is named as a changed part, never
// shown.
type TargetSide struct {
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint,omitzero"`
	Revision    string `json:"revision"`
}

// EnvironmentSide is what one side declares about what evaluated it: the
// engine build and the spec contract that build read, from the pin the durable
// run retained. Fingerprint is that pin document's digest, which is what is
// left to compare when the pin itself cannot be read here.
type EnvironmentSide struct {
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint,omitzero"`
	Engine      string `json:"engine,omitzero"`
	Spec        string `json:"spec,omitzero"`
}

// RuleSide is the semantic profile the same pin names, and whether this build
// resolves that identity to content it actually has.
type RuleSide struct {
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint,omitzero"`
	Profile     string `json:"profile,omitzero"`
	Resolution  string `json:"resolution,omitzero"`
}

// Side is one half of the comparison, stated entirely from what that half
// retained. Identity is the artifact's own content identity; it detects a
// changed artifact, and it is not an authenticity signature.
type Side struct {
	Kind        string          `json:"kind"`
	Identity    string          `json:"identity"`
	Input       InputSide       `json:"input"`
	Target      TargetSide      `json:"target"`
	Environment EnvironmentSide `json:"environment"`
	Rule        RuleSide        `json:"rule"`
}

// Drift is one cause and what comparing it established.
//
// Parts are the named parts of the record that differ, never their values: a
// target whose address changed reports `address`, and the two addresses stay
// where they were retained.
type Drift struct {
	Cause      string   `json:"cause"`
	Outcome    string   `json:"outcome"`
	Comparison string   `json:"comparison"`
	Parts      []string `json:"parts"`
	Reason     string   `json:"reason,omitzero"`
}

// Attribution is what the four outcomes together support saying, and it is
// deliberately easy to leave unsettled. Changed names every cause that drifted.
// Unresolved names every cause that was not established either way, and while
// it holds anything at all the outcome is `undecided`.
type Attribution struct {
	Outcome    string   `json:"outcome"`
	Changed    []string `json:"changed"`
	Unresolved []string `json:"unresolved"`
}

// Report is the whole answer. It holds no field value, no message byte, no
// target address and no filesystem path, and it carries no verdict: whether a
// test passed is the result's own statement, and whether a difference matters
// is a person's.
type Report struct {
	Schema      string      `json:"schema"`
	Scope       string      `json:"scope"`
	Left        Side        `json:"left"`
	Right       Side        `json:"right"`
	Drift       []Drift     `json:"drift"`
	Attribution Attribution `json:"attribution"`
}
