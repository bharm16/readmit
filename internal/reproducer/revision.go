package reproducer

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
)

// How two reproducer revisions are related, read from the identities their own
// manifests name. A relation nothing establishes is `unrelated` rather than a
// guess: two reproducers of the same incident that were each built from the
// original case are siblings, and one built from another's derived case is its
// child, and neither fact is inferred from a folder name or a write order.
const (
	SameEvidence = "same"
	ChildOf      = "child"
	ParentOf     = "parent"
	SiblingOf    = "sibling"
	Unrelated    = "unrelated"
)

// How one occurrence's retention changed between two revisions.
//
// Dropping a prerequisite is told apart from dropping a selection because they
// mean opposite things: a person who stops selecting a message meant to, and a
// reproducer that stopped retaining the booking a reschedule refers to is a
// reproducer that may have stopped reproducing anything. A retention that
// survived under a different relation is neither, and is named as such.
const (
	DroppedSelection    = "dropped-selection"
	DroppedPrerequisite = "dropped-prerequisite"
	AddedSelection      = "added-selection"
	AddedPrerequisite   = "added-prerequisite"
	RelationChanged     = "relation-changed"
)

// How one step, edit or unsettled report differs. `added` is in the right
// revision and not the left, `removed` the other way round, and `changed` is
// one position both revisions edit differently.
const (
	Added   = "added"
	Removed = "removed"
	Changed = "changed"
)

// What a comparison of two retained runs establishes.
//
// These are execution outcomes rather than relations between occurrences, so
// they are spelled the way every other outcome in this product is spelled.
// Neither `not_attempted` nor `different_test` is a failure and neither is a
// pass: a revision nobody has run yet, and a revision run against a different
// list of expectations, are both states a person is in, and folding either into
// a verdict would be claiming proof that was never produced.
const (
	// ProofNotAttempted is one or both revisions naming no retained run.
	ProofNotAttempted = "not_attempted"
	// ProofDifferentTest is two runs that did not evaluate the same ordered
	// list of expectations, so their verdicts are not comparable.
	ProofDifferentTest = "different_test"
	// ProofCompared is two runs of the same expectations, each bound by
	// identity to the revision it was executed against.
	ProofCompared = "compared"
)

// What two retained runs said about one expectation. NotEvaluated is
// testrunner's own name for an expectation no execution reached, taken from
// there rather than respelled here: an execution error leaves every expectation
// at that verdict, so an error can never read as a failure that survived a
// transformation or as one it fixed.
const (
	SameFailure  = "same_failure"
	SamePass     = "same_pass"
	VerdictMoved = "changed"
	NotEvaluated = testrunner.NotEvaluated
)

// Revision names one built reproducer to compare and, when there is one, the
// retained run that is its proof.
//
// Path is the reproducer directory: the derived case and the manifest beside
// it. Result is one retained readmit-result/v1 directory, and it is proof of
// this revision only when it was executed against this revision's derived case,
// which is checked rather than assumed. A revision with no retained run names
// none, and the comparison says so.
type Revision struct {
	Path   string
	Result string
}

// RevisionSummary is one compared revision as its own evidence declares it.
//
// Provenance and Derivation are read from the derived bundle's manifest, never
// from a directory name and never from the manifest beside it, so a comparison
// restates what the evidence says about itself: transformed customer evidence
// is `derived` under this transformation's name, and a bundle declaring
// anything else is not a reproducer revision and is refused before it is here.
type RevisionSummary struct {
	Parent     Artifact `json:"parent"`
	Derived    Artifact `json:"derived"`
	Provenance string   `json:"provenance"`
	Derivation string   `json:"derivation"`
	Steps      int      `json:"steps"`
	Retained   int      `json:"retained"`
	Edits      int      `json:"edits"`
}

// StepChange is one authored step only one revision's plan holds.
//
// Position is its place in the plan that holds it, counted from one — the
// earlier plan for a removed step and the later plan for an added one, since
// those are the only plans each is in.
type StepChange struct {
	Position int    `json:"position"`
	Change   string `json:"change"`
	Step     Step   `json:"step"`
}

// RetentionChange is one occurrence the two revisions retain differently:
// dropped, added, or retained under a different relation. The reasons are the
// manifests' own, so a prerequisite that became a selection reads as exactly
// that rather than as no change at all.
type RetentionChange struct {
	Parent          string `json:"parent"`
	Change          string `json:"change"`
	LeftReason      string `json:"left_reason,omitzero"`
	RightReason     string `json:"right_reason,omitzero"`
	LeftRequiredBy  string `json:"left_required_by,omitzero"`
	RightRequiredBy string `json:"right_required_by,omitzero"`
}

// EditChange is one edited position the two revisions treat differently.
//
// It carries the operator and the state the position was in before each edit,
// and no value on either side — the same discipline the manifests themselves
// hold, where the bytes an edit replaced are deliberately never recorded. The
// offsets are not compared: an edit lands at a different offset simply because
// an earlier edit of the same occurrence changed length, which is not a
// difference between what the two revisions do.
type EditChange struct {
	Parent        string    `json:"parent"`
	Selector      string    `json:"selector"`
	Change        string    `json:"change"`
	LeftOperator  string    `json:"left_operator,omitzero"`
	RightOperator string    `json:"right_operator,omitzero"`
	LeftState     hl7.State `json:"left_state,omitzero"`
	RightState    hl7.State `json:"right_state,omitzero"`
}

// UnresolvedChange is one thing a dependency step reached and could not settle
// that only one revision reports.
type UnresolvedChange struct {
	Occurrence string `json:"occurrence"`
	Reason     string `json:"reason"`
	Change     string `json:"change"`
}

// ProofSide is one retained run, named by what its own reader verified.
//
// Case is the case identity that run was executed against, which is this
// revision's derived case and is checked to be; it is reported so the binding
// between a revision and its proof is visible rather than implied.
type ProofSide struct {
	Identity   string `json:"identity"`
	Case       string `json:"case"`
	Status     string `json:"status"`
	ErrorClass string `json:"error_class,omitzero"`
}

// AssertionProof is what the two retained runs said about one expectation.
// Only the expectation's identifier, its operator and the two verdicts are
// here: an assertion carries the value it expected and the value it observed,
// and neither crosses this boundary.
type AssertionProof struct {
	Assertion    string `json:"assertion"`
	Operator     string `json:"operator"`
	Outcome      string `json:"outcome"`
	LeftVerdict  string `json:"left_verdict"`
	RightVerdict string `json:"right_verdict"`
}

// Proof is what the retained runs of the two revisions establish.
//
// There is no overall verdict here, deliberately. Whether a reproducer still
// reproduces the incident is the statement one named expectation makes, and
// which expectation that is, is a person's judgement; this reports what each
// run decided about each one and stops there.
type Proof struct {
	State      string           `json:"state"`
	Left       *ProofSide       `json:"left,omitzero"`
	Right      *ProofSide       `json:"right,omitzero"`
	Assertions []AssertionProof `json:"assertions"`
}

// Comparison is what two reproducer revisions do differently.
//
// It is a comparison of plans and manifests, not of messages. The two derived
// cases are never compared byte for byte, and that is an evidence rule rather
// than an omission: where one revision edits a position the other left alone,
// the other's bytes at that position are the original evidence's own value, and
// this manifest contract refuses to record those bytes. Field-aware comparison
// of two collections is [the diff engine], which compares cases a person named
// rather than lineage.
//
// This is a typed value. Nothing here is written anywhere, no contract is added
// and neither revision is changed by producing it.
//
// [the diff engine]: docs/diff.md
type Comparison struct {
	Left       RevisionSummary    `json:"left"`
	Right      RevisionSummary    `json:"right"`
	Lineage    string             `json:"lineage"`
	Steps      []StepChange       `json:"steps"`
	Retention  []RetentionChange  `json:"retention"`
	Edits      []EditChange       `json:"edits"`
	Unresolved []UnresolvedChange `json:"unresolved"`
	Proof      Proof              `json:"proof"`
}

// CompareRevisions reads two built reproducers and reports how they differ:
// how they are related, what their plans do differently, which occurrences one
// retains and the other does not, which of those were setup dependencies rather
// than selections, and what the runs retained for each one decided.
//
// Both reproducers are opened by the same reader that reads one back after a
// build, so a manifest that no longer describes the evidence beside it is
// refused rather than compared, and a derived case declaring any other
// transformation is not a reproducer revision at all. A retained run is read by
// the result reader, which re-derives every verdict from what it retained, and
// is accepted as proof of a revision only when it was executed against that
// revision's derived case.
func CompareRevisions(left, right Revision) (Comparison, error) {
	leftManifest, leftSummary, err := summarize(left.Path)
	if err != nil {
		return Comparison{}, err
	}
	rightManifest, rightSummary, err := summarize(right.Path)
	if err != nil {
		return Comparison{}, err
	}
	proof, err := compareRuns(left.Result, right.Result, leftManifest, rightManifest)
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{
		Left:       leftSummary,
		Right:      rightSummary,
		Lineage:    lineage(leftManifest, rightManifest),
		Steps:      compareSteps(leftManifest.Plan.Steps, rightManifest.Plan.Steps),
		Retention:  compareRetention(leftManifest.Occurrences, rightManifest.Occurrences),
		Edits:      compareEdits(leftManifest.Edits, rightManifest.Edits),
		Unresolved: compareUnresolved(leftManifest.Unresolved, rightManifest.Unresolved),
		Proof:      proof,
	}, nil
}

// summarize opens one reproducer and states what its evidence declares about
// itself.
//
// The provenance and the derivation are read back out of the derived bundle's
// own manifest rather than restated from the constants Open checked them
// against. That is one extra read of one manifest, and it is the same
// discipline `project add` and `project revise` hold: provenance is read from
// evidence and never declared by the code reporting it, so a reproducer says it
// is a transformation of customer evidence because its evidence says so.
func summarize(path string) (*Manifest, RevisionSummary, error) {
	manifest, err := Open(path)
	if err != nil {
		return nil, RevisionSummary{}, err
	}
	declared, err := bundle.Describe(filepath.Join(path, CaseName))
	if err != nil {
		return nil, RevisionSummary{}, errors.New("the derived case of this reproducer could not be verified as complete, unmodified evidence")
	}
	return manifest, RevisionSummary{
		Parent:     manifest.Parent,
		Derived:    manifest.Derived,
		Provenance: string(declared.Provenance.Mode),
		Derivation: declared.Provenance.Derivation,
		Steps:      len(manifest.Plan.Steps),
		Retained:   len(manifest.Occurrences),
		Edits:      len(manifest.Edits),
	}, nil
}

// lineage reports how the two revisions are related, by the identities their
// manifests name and by nothing else.
func lineage(left, right *Manifest) string {
	switch {
	case left.Derived.Identity == right.Derived.Identity:
		return SameEvidence
	case right.Parent.Identity == left.Derived.Identity:
		return ChildOf
	case left.Parent.Identity == right.Derived.Identity:
		return ParentOf
	case left.Parent.Identity == right.Parent.Identity:
		return SiblingOf
	default:
		return Unrelated
	}
}

// compareSteps reports the authored change between two plans.
//
// A plan is an ordered transformation, so two plans are compared as the prefix
// they share and the steps each one has after it. That is what a revision of a
// plan is: steps undone back to a point and other steps added from there, which
// is the only way this editor changes a plan at all.
func compareSteps(left, right []Step) []StepChange {
	common := 0
	for common < len(left) && common < len(right) && sameStep(left[common], right[common]) {
		common++
	}
	changes := make([]StepChange, 0, len(left)-common+len(right)-common)
	for i := common; i < len(left); i++ {
		changes = append(changes, StepChange{Position: i + 1, Change: Removed, Step: left[i]})
	}
	for i := common; i < len(right); i++ {
		changes = append(changes, StepChange{Position: i + 1, Change: Added, Step: right[i]})
	}
	return changes
}

func sameStep(left, right Step) bool {
	return left.Operator == right.Operator && left.Occurrence == right.Occurrence &&
		left.Selector == right.Selector && left.Value == right.Value &&
		slices.Equal(left.Identity, right.Identity)
}

// compareRetention reports every occurrence the two revisions retain
// differently, in the order the left revision holds them and then the order the
// right revision holds the ones it added, so the same two revisions always
// compare the same way.
func compareRetention(left, right []Retained) []RetentionChange {
	parent := func(entry Retained) string { return entry.Parent }
	before, after := indexed(left, parent), indexed(right, parent)
	changes := make([]RetentionChange, 0, len(left)+len(right))
	for _, entry := range left {
		later, still := after[entry.Parent]
		switch {
		case !still && entry.Reason == Selected:
			changes = append(changes, RetentionChange{Parent: entry.Parent, Change: DroppedSelection, LeftReason: entry.Reason, LeftRequiredBy: entry.RequiredBy})
		case !still:
			changes = append(changes, RetentionChange{Parent: entry.Parent, Change: DroppedPrerequisite, LeftReason: entry.Reason, LeftRequiredBy: entry.RequiredBy})
		case entry.Reason != later.Reason || entry.RequiredBy != later.RequiredBy:
			changes = append(changes, RetentionChange{
				Parent: entry.Parent, Change: RelationChanged,
				LeftReason: entry.Reason, RightReason: later.Reason,
				LeftRequiredBy: entry.RequiredBy, RightRequiredBy: later.RequiredBy,
			})
		}
	}
	for _, entry := range right {
		if _, had := before[entry.Parent]; had {
			continue
		}
		change := AddedPrerequisite
		if entry.Reason == Selected {
			change = AddedSelection
		}
		changes = append(changes, RetentionChange{Parent: entry.Parent, Change: change, RightReason: entry.Reason, RightRequiredBy: entry.RequiredBy})
	}
	return changes
}

// position is one edited place: the occurrence of the parent case and the
// canonical selector of the field inside it. A manifest records each position
// of each occurrence at most once, which is what makes this a key.
type position struct{ parent, selector string }

// indexed keys one revision's entries by what identifies them in the other, so
// both comparisons below look the same entry up the same way.
func indexed[E any, K comparable](entries []E, key func(E) K) map[K]E {
	index := make(map[K]E, len(entries))
	for _, entry := range entries {
		index[key(entry)] = entry
	}
	return index
}

func compareEdits(left, right []Edit) []EditChange {
	at := func(entry Edit) position { return position{entry.Parent, entry.Selector} }
	before, after := indexed(left, at), indexed(right, at)
	changes := make([]EditChange, 0, len(left)+len(right))
	for _, entry := range left {
		later, still := after[position{entry.Parent, entry.Selector}]
		switch {
		case !still:
			changes = append(changes, EditChange{
				Parent: entry.Parent, Selector: entry.Selector, Change: Removed,
				LeftOperator: entry.Operator, LeftState: entry.State,
			})
		case entry.Operator != later.Operator || entry.State != later.State:
			changes = append(changes, EditChange{
				Parent: entry.Parent, Selector: entry.Selector, Change: Changed,
				LeftOperator: entry.Operator, RightOperator: later.Operator,
				LeftState: entry.State, RightState: later.State,
			})
		}
	}
	for _, entry := range right {
		if _, had := before[position{entry.Parent, entry.Selector}]; had {
			continue
		}
		changes = append(changes, EditChange{
			Parent: entry.Parent, Selector: entry.Selector, Change: Added,
			RightOperator: entry.Operator, RightState: entry.State,
		})
	}
	return changes
}

func compareUnresolved(left, right []Unresolved) []UnresolvedChange {
	changes := make([]UnresolvedChange, 0, len(left)+len(right))
	for _, entry := range left {
		if !slices.Contains(right, entry) {
			changes = append(changes, UnresolvedChange{Occurrence: entry.Occurrence, Reason: entry.Reason, Change: Removed})
		}
	}
	for _, entry := range right {
		if !slices.Contains(left, entry) {
			changes = append(changes, UnresolvedChange{Occurrence: entry.Occurrence, Reason: entry.Reason, Change: Added})
		}
	}
	return changes
}

// compareRuns reports what the retained runs of the two revisions decided.
//
// A run is proof of a revision only when it was executed against that
// revision's derived case, so the identity the result recorded must be the
// identity the manifest names; a run of some other evidence is refused rather
// than reported beside a revision it says nothing about. Two runs that did not
// evaluate the same ordered expectations are reported as a different test, and
// no expectation of either is paired with one of the other.
func compareRuns(left, right string, leftManifest, rightManifest *Manifest) (Proof, error) {
	// Each named run is read and bound to its own revision before either side
	// is compared, so a run named beside a revision it says nothing about is
	// refused even when the other revision has none and nothing would have been
	// compared anyway.
	leftRun, err := retainedRun(left, leftManifest.Derived.Identity)
	if err != nil {
		return Proof{}, err
	}
	rightRun, err := retainedRun(right, rightManifest.Derived.Identity)
	if err != nil {
		return Proof{}, err
	}
	proof := Proof{State: ProofNotAttempted, Left: leftRun.ProofSide, Right: rightRun.ProofSide, Assertions: []AssertionProof{}}
	if left == "" || right == "" {
		return proof, nil
	}
	proof.State = ProofCompared
	leftAssertions, rightAssertions := leftRun.assertions, rightRun.assertions
	if !sameExpectations(leftAssertions, rightAssertions) {
		proof.State = ProofDifferentTest
		return proof, nil
	}
	for i := range leftAssertions {
		proof.Assertions = append(proof.Assertions, AssertionProof{
			Assertion:    leftAssertions[i].Assertion.ID,
			Operator:     leftAssertions[i].Assertion.Operator,
			Outcome:      outcome(leftAssertions[i].Status, rightAssertions[i].Status),
			LeftVerdict:  leftAssertions[i].Status,
			RightVerdict: rightAssertions[i].Status,
		})
	}
	return proof, nil
}

// side is a verified retained run and the expectations it evaluated. The
// expectations stay here rather than on ProofSide: they carry the value each
// one expected and the value each one observed, and only the verdicts reach a
// caller.
type side struct {
	*ProofSide
	assertions []testrunner.AssertionResult
}

// retainedRun verifies one named run and binds it to the revision it is offered
// as proof of. A revision that names none is not an error: it is the state a
// person is in before they have run it, and compareRuns reports it as one.
func retainedRun(path, derived string) (side, error) {
	if path == "" {
		return side{}, nil
	}
	artifact, err := testrunner.Open(path)
	if err != nil {
		return side{}, errors.New("a retained run of a reproducer revision could not be verified as a complete test result")
	}
	if artifact.Result.InputBundleIdentity != derived {
		return side{}, errors.New("that retained run was executed against different evidence than the revision it is offered as proof of")
	}
	return side{
		ProofSide: &ProofSide{
			Identity:   artifact.Identity,
			Case:       artifact.Result.InputBundleIdentity,
			Status:     string(artifact.Result.Status),
			ErrorClass: artifact.Result.ErrorClass,
		},
		assertions: artifact.Result.Assertions,
	}, nil
}

// sameExpectations reports whether two runs evaluated the same ordered list of
// expectations. Each is re-encoded, so what is compared is what an assertion
// declares rather than how the spec that carried it was formatted. The spec
// identities are deliberately not compared: a test rebound to a second case
// names a different input and is the same test, and it is the expectations that
// decide whether two verdicts mean the same thing.
func sameExpectations(left, right []testrunner.AssertionResult) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		encodedLeft, leftErr := json.Marshal(left[i].Assertion, json.Deterministic(true))
		encodedRight, rightErr := json.Marshal(right[i].Assertion, json.Deterministic(true))
		if leftErr != nil || rightErr != nil || !bytes.Equal(encodedLeft, encodedRight) {
			return false
		}
	}
	return true
}

// outcome names what two verdicts on one expectation establish. An expectation
// no execution reached keeps that name on both sides, so an execution error can
// never read as a failure that survived a transformation or as one it fixed.
func outcome(left, right string) string {
	switch {
	case left == testrunner.NotEvaluated || right == testrunner.NotEvaluated:
		return NotEvaluated
	case left != right:
		return VerdictMoved
	case left == testrunner.Failed:
		return SameFailure
	default:
		return SamePass
	}
}
