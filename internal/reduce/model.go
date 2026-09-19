// Package reduce shrinks the sequence a regression test sends until removing
// anything more loses the failure a person chose to keep, and says exactly how
// much it established.
//
// Reduction is only ever as sound as the thing that answers "does it still
// fail". That answer is an [Oracle] here, and it is treated as untrusted: the
// unreduced sequence must reproduce the chosen signature a declared number of
// times before a single group is removed, the sequence that survives must
// reproduce it that many times again, and an execution that timed out, was
// cancelled, errored or left a delivery uncertain is never read as either
// answer. A timeout is not a reduced failure; it is an observation nothing can
// be concluded from, so reduction stops and says so rather than dropping the
// message it happened to be testing.
//
// Every trial is bounded and every trial resets first. The plan declares how
// many trials it may spend, reduction stops at that budget and reports what it
// reached as bounded rather than continuing, and a reset this release cannot
// confirm ends the reduction instead of running the next candidate against
// whatever state the last one left.
//
// Nothing here writes into evidence, and nothing here produces a derived case.
// A reduction reports which occurrences of the parent case it retained; turning
// that answer into new derived evidence is [the reproducer editor]'s build,
// which is where [ADR-0004]'s derivation names live. No derivation name is
// added by this package.
//
// A plan is data interpreted by typed Go operators ([ADR-0003]): there is no
// rule language, no expression and no script, and a grouping this release does
// not perform is refused by name rather than ignored.
//
// [the reproducer editor]: docs/reproducer.md
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
// [ADR-0004]: docs/adr/0004-derived-evidence-and-generated-export.md
package reduce

import (
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
)

const (
	// PlanSchema is the declared reduction: the failure to hold, how the
	// sequence may be taken apart, and what may be spent establishing it.
	PlanSchema = "readmit-reduction-plan/v1"

	// ReportSchema is what that plan reached over one verified case. It is a
	// report, not evidence: nothing reads it back as a case, and no case and no
	// reproducer contract gains a member from it.
	ReportSchema = "readmit-reduction/v1"
)

// The groupings a plan is built from. Each names the one thing it does, and a
// plan carrying a member another grouping uses is refused rather than ignored.
const (
	// GroupPerOccurrence makes every occurrence its own group. Reduction may
	// then remove any single message, which is the right reading when nothing
	// in the evidence ties one message to another.
	GroupPerOccurrence = "group-per-occurrence/v1"

	// GroupByCorrelation keeps the occurrences one declared correlation rule
	// related in one group, so they are removed together or not at all. Which
	// occurrences belong together is not decided here: it is
	// [github.com/bharm16/readmit/internal/correlate]'s answer under the rules
	// an operator wrote down.
	GroupByCorrelation = "group-by-correlation/v1"
)

// Verdict is what one trial established about one candidate. The set is closed
// and Undecided is never folded into either of the other two: an observation
// nothing can be concluded from is reported as one.
type Verdict string

const (
	// Reproduced: the run failed exactly the assertions the signature names.
	Reproduced Verdict = "reproduced"
	// NotReproduced: the run passed, or failed a different set of assertions.
	// A different failure is not the chosen failure.
	NotReproduced Verdict = "not_reproduced"
	// Undecided: nothing about the chosen failure was established. The reset
	// was not confirmed, the oracle was unavailable, or the run stopped for a
	// reason that is not a verdict about an assertion.
	Undecided Verdict = "undecided"
)

// Outcome is how one whole reduction ended. The set is closed, and only
// Reduced carries a minimality claim.
type Outcome string

const (
	// OutcomeReduced: every remaining group was tried and required, and the
	// sequence that survived reproduced the signature again afterwards.
	OutcomeReduced Outcome = "reduced"
	// OutcomeBounded: the trial budget ran out first. What is retained still
	// reproduced the chosen failure the last time it was asked, no removal was
	// ruled out, and nothing more is claimed about it. It may be no smaller
	// than the sequence the reduction started from.
	OutcomeBounded Outcome = "bounded"
	// OutcomeNotAttempted: no group of the partition could be removed at all,
	// because every one of them is pinned by the signature or there is only
	// one. The oracle agreed with itself about the sequence and nothing was
	// reduced; it is recorded as what it is rather than as a reduction that
	// happened to remove nothing.
	OutcomeNotAttempted Outcome = "not_attempted"

	// OutcomeUndecided: nothing was established. The oracle disagreed with
	// itself, an observation could not be read as a verdict, or a person
	// stopped the reduction.
	OutcomeUndecided Outcome = "undecided"
)

// Minimality is the claim a report is willing to stand behind. There are two,
// and neither is a global minimum: this release does not search the whole
// powerset of a sequence and does not pretend to.
type Minimality string

const (
	// GroupOneMinimal: every removable group was removed in turn and the
	// signature was lost each time, so no single group of this partition can
	// be dropped. It is 1-minimality over the declared grouping as this oracle
	// answered, not over occurrences, not over fields and not globally. A
	// reduction that could try no removal at all reports OutcomeNotAttempted
	// instead, so this claim is never made about a search that did not run.
	GroupOneMinimal Minimality = "group-1-minimal"

	// NoClaim: no removal was ruled out. The result may be smaller than what
	// was started from, and that is the whole of it. Every bounded and
	// undecided reduction reports this.
	NoClaim Minimality = "none"
)

// Reason names why a trial or a reduction came out the way it did, in this
// package's own closed vocabulary. It holds no path, no address and no value
// read out of a message, so a report can be read and kept without carrying any.
type Reason string

const (
	// Why one trial was undecided.
	ResetNotConfirmed  Reason = "reset_not_confirmed"
	OracleUnavailable  Reason = "oracle_unavailable"
	RunTimedOut        Reason = "run_timed_out"
	RunCancelled       Reason = "run_cancelled"
	RunInterrupted     Reason = "run_interrupted"
	RunExecutionError  Reason = "run_execution_error"
	RunDeliveryUnknown Reason = "run_delivery_uncertain"
	RunNotTerminal     Reason = "run_did_not_finish"

	// Why one reduction ended.
	BaselineNotReproduced Reason = "unreduced_sequence_did_not_reproduce_the_signature"
	FlakyOracle           Reason = "oracle_disagreed_with_itself"
	BudgetSpent           Reason = "trial_budget_spent"
	// BudgetSpentUnconfirmed is a budget that ran out after the removals were
	// committed and before the surviving sequence was asked the calibration
	// question again. It is named apart from BudgetSpent because the check
	// that catches an unstable oracle is exactly the one that did not run.
	BudgetSpentUnconfirmed Reason = "trial_budget_spent_before_the_result_was_confirmed"
	EveryGroupRequired     Reason = "every_remaining_group_is_required"
	// NoRemovableGroup is a partition with nothing in it to try: one group, or
	// every group pinned by the signature.
	NoRemovableGroup Reason = "no_group_of_this_partition_could_be_removed"
	OperatorStopped  Reason = "interrupted"
)

// Purpose is why one trial was run, so a reader can tell a calibration from a
// removal without counting. The set is closed.
type Purpose string

const (
	// Calibration establishes that the unreduced sequence reproduces the
	// signature at all, and that it does so repeatably.
	Calibration Purpose = "calibration"
	// Removal asks whether one group can be dropped.
	Removal Purpose = "removal"
	// Confirmation asks the surviving sequence the calibration question again,
	// after every removal has been committed.
	Confirmation Purpose = "confirmation"
)

// What a reduction could not take into account. It never passes: an entry here
// is a limit of the answer, stated rather than left to a doc page.
const (
	// UngroupedCollision: the declared rules found equality they could not
	// stand behind — an ambiguous acknowledgement, a control ID duplicated
	// inside one source, or identifiers under no configured assigning
	// authority — so those occurrences were not put in one group. Reduction
	// may therefore remove one of them without the others.
	UngroupedCollision = "ungrouped-collision"

	// RuleNotApplied: the case supplied no scope for one declared rule, so it
	// related nothing and contributed no group.
	RuleNotApplied = "rule-not-applied"

	// PinnedBySignature: this group holds an occurrence the chosen signature is
	// stated about. Removing it would remove the assertion the signature names
	// rather than test it, so it is never a removal candidate and the result is
	// not minimal with respect to it.
	PinnedBySignature = "pinned-by-signature"
)

// Bounds. A plan, a candidate set or a report past one of these is refused,
// never truncated.
const (
	// MaxTrials bounds what one plan may declare it will spend. Reduction is
	// quadratic in groups in the worst case, and an unbounded search against a
	// live environment is the failure this package exists to prevent.
	MaxTrials = 512
	// MaxConfirmations bounds the repeated agreement a plan may demand before
	// and after reducing.
	MaxConfirmations = 8
	// MaxCandidates bounds the occurrences one reduction takes apart. It is far
	// below the 4000 messages a replay may send, because a budget that could
	// cover a larger sequence does not exist.
	MaxCandidates = 256
	// MaxPlanBytes bounds the plan document a reader decodes.
	MaxPlanBytes = 64 << 10
	// MaxReportBytes bounds one rendered report.
	MaxReportBytes = 32 << 20
)

// Signature is the failure a reduction holds: the state a run must stop in and
// the exact set of assertions that must have failed.
//
// State is a member rather than an assumption so that a plan says in its own
// bytes which class of failure it is about, and so that every other class is
// refused by name. Only durablerun.AssertionFailed may be declared: a run that
// timed out, errored or was cancelled establishes nothing about an expectation,
// and reducing against one would shrink a sequence towards a flaky environment
// rather than towards a defect.
//
// The set is exact. A run that fails the named assertions and something else as
// well is a different failure and is not reproduced, which keeps a reduction
// from walking away from the failure it was asked to preserve.
type Signature struct {
	State      durablerun.State `json:"state"`
	Assertions []string         `json:"assertions"`
}

// Plan is the reduction an operator selected explicitly, bound to the evidence
// it was authored against.
//
// Rules is the SHA-256 of the exact correlation rules whose relations form the
// groups, and belongs only to GroupByCorrelation: a plan that groups per
// occurrence relates nothing and declaring rules there would say something the
// plan cannot mean.
type Plan struct {
	Schema        string    `json:"schema"`
	Case          string    `json:"case"`
	Grouping      string    `json:"grouping"`
	Rules         string    `json:"rules,omitzero"`
	Signature     Signature `json:"signature"`
	Trials        int       `json:"trials"`
	Confirmations int       `json:"confirmations"`
}

// Artifact names one case by the contract it declares and the identity its own
// reader verified.
type Artifact struct {
	Schema   string `json:"schema"`
	Identity string `json:"identity"`
}

// Group is one set of occurrences reduction removes together or not at all.
// Rules names the declared rules that related them, so a reader can see why a
// group holds more than one occurrence. Required marks a group the signature is
// stated about, which is never a removal candidate.
type Group struct {
	ID          string   `json:"id"`
	Occurrences []string `json:"occurrences"`
	Rules       []string `json:"rules,omitzero"`
	Required    bool     `json:"required,omitzero"`
}

// Trial is one execution of one candidate: the reset that preceded it, the run
// that followed, and what the two established about the chosen failure.
//
// Failed is the assertions the run reported as failed, by the identifiers the
// spec's own author gave them. It carries no observed value and no message
// byte: it is the evidence behind the verdict, which is whether this is the
// same failure or a different one.
type Trial struct {
	Index       int                  `json:"index"`
	Purpose     Purpose              `json:"purpose"`
	Candidate   []string             `json:"candidate"`
	Removed     string               `json:"removed,omitzero"`
	Reset       fixturereset.Outcome `json:"reset"`
	ResetReason fixturereset.Reason  `json:"reset_reason"`
	State       durablerun.State     `json:"state,omitzero"`
	Failed      []string             `json:"failed"`
	Verdict     Verdict              `json:"verdict"`
	Reason      Reason               `json:"reason,omitzero"`
}

// Unsupported is something the reduction could not take into account. It never
// passes: a group listed here was not reduced against.
type Unsupported struct {
	Code   string `json:"code"`
	Group  string `json:"group,omitzero"`
	Rule   string `json:"rule,omitzero"`
	Detail string `json:"detail"`
}

// Summary totals the report so a reader sees the shape before the detail.
type Summary struct {
	Groups      int `json:"groups"`
	Trials      int `json:"trials"`
	Budget      int `json:"budget"`
	Retained    int `json:"retained"`
	Removed     int `json:"removed"`
	Unsupported int `json:"unsupported"`
}

// Report is what one reduction reached: how the sequence was taken apart, every
// trial it spent, what it retained, and exactly how much it is willing to claim
// about that.
//
// Retained and Removed are occurrences of the parent case, in evidence order.
// Nothing is written: turning a retained set into new derived evidence is the
// reproducer editor's build.
type Report struct {
	Schema      string        `json:"schema"`
	Case        Artifact      `json:"case"`
	Plan        Plan          `json:"plan"`
	Groups      []Group       `json:"groups"`
	Trials      []Trial       `json:"trials"`
	Outcome     Outcome       `json:"outcome"`
	Reason      Reason        `json:"reason"`
	Minimality  Minimality    `json:"minimality"`
	Retained    []string      `json:"retained"`
	Removed     []string      `json:"removed"`
	Summary     Summary       `json:"summary"`
	Unsupported []Unsupported `json:"unsupported"`
	// Scope is the boundary statement a reader needs before acting on the
	// report: what it establishes and what it does not.
	Scope string `json:"scope"`
}
