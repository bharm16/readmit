package reduce

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
)

// boundaryStatement is the honest boundary every report carries, in the words a
// reader needs before acting on it.
const boundaryStatement = "A reduction states what this oracle answered about this sequence, nothing more. " +
	"A group-1-minimal result means no single group of the declared partition could be removed without losing the " +
	"chosen failure; it is not a global minimum, not minimal over occurrences or fields, and not a claim that the " +
	"retained messages are the cause of anything. A bounded result is a search that stopped at its budget: it " +
	"ruled out no removal at all and may retain the whole sequence it started from. " +
	"An undecided result establishes nothing at all: the retained set is what the reduction was holding when it " +
	"stopped, not an answer. A reproduced signature means the same assertions failed again; it is not proof of a " +
	"defect, and no timeout, cancellation, execution error or uncertain delivery was ever read as that failure. " +
	"Nothing here was written into evidence and no derived case was produced."

// Observation is what one execution of one candidate established, in the
// vocabulary durable runs already own. Failed names the assertions the run
// reported as failed and carries no observed value.
type Observation struct {
	State             durablerun.State
	DeliveryUncertain bool
	Failed            []string
}

// Oracle is what reduction needs from one trial, and the whole of what it
// trusts. It is asked to return the environment to its declared starting state
// and then to run one candidate; every judgement about what the answer means is
// made here, so an oracle cannot report a reduced failure by accident.
type Oracle interface {
	// Reset returns the environment to its declared starting state and names
	// what it established, exactly as a reset plan names it. Only
	// [fixturereset.Confirmed] lets the trial run.
	Reset(ctx context.Context) (fixturereset.Outcome, fixturereset.Reason)

	// Observe executes the named occurrences of the parent case, in the order
	// given, and reports what the run established. An error means the oracle
	// could not answer at all, which is undecided and never a verdict.
	Observe(ctx context.Context, occurrences []string) (Observation, error)
}

// Request is one reduction: the evidence, the plan, the declarations the plan
// named, and the occurrences the oracle sends.
//
// Messages is the sequence the unreduced run sends, in the order it sends it.
// Required is the occurrences the chosen signature is stated about: a group
// holding one is never a removal candidate, because removing it would take the
// assertion away rather than test it. [DurableOracle.Required] reports them for
// the spec it read.
//
// Omitting Required is safe and never unsound: a candidate that drops the
// message the signature is about drops the assertion with it, so that candidate
// cannot report the chosen failure and the group is retained anyway. Naming
// them spends fewer trials and records why the group was kept.
type Request struct {
	Case     string
	Plan     Plan
	Rules    correlate.Rules
	Messages []string
	Required []string
}

// Run reduces one sequence against one chosen failure signature and reports
// exactly what it established.
//
// It opens and verifies the case through the same reader `readmit timeline`
// runs and leaves it exactly as it found it. It writes no evidence, produces no
// derived case and adds no member to any existing contract. Which occurrences
// belong together under GroupByCorrelation is [correlate.Run]'s answer; this
// package only keeps them together.
func Run(ctx context.Context, request Request, oracle Oracle) (Report, error) {
	if err := validatePlan(request.Plan); err != nil {
		return Report{}, err
	}
	if oracle == nil {
		return Report{}, errors.New("a reduction needs an oracle to answer whether the failure is still there")
	}
	source, err := bundle.Open(request.Case)
	if err != nil {
		return Report{}, errors.New("the case could not be verified as complete, unmodified evidence")
	}
	if source.Identity != request.Plan.Case {
		return Report{}, errors.New("this plan was authored against different evidence than the case it was applied to")
	}
	if err := validateCandidates(source, request); err != nil {
		return Report{}, err
	}
	groups, notes, err := partition(request)
	if err != nil {
		return Report{}, err
	}
	s := &session{
		request: request, groups: groups, notes: notes, oracle: oracle,
		indexed:  make(map[string]Group, len(groups)),
		position: make(map[string]int, len(request.Messages)),
		budget:   request.Plan.Trials,
		trials:   []Trial{},
	}
	for _, group := range groups {
		s.indexed[group.ID] = group
	}
	for i, id := range request.Messages {
		s.position[id] = i
	}
	s.report.Schema = ReportSchema
	s.report.Case = Artifact{Schema: source.Manifest.Schema, Identity: source.Identity}
	s.report.Plan = request.Plan
	return s.reduce(ctx), nil
}

func validateCandidates(source *bundle.Bundle, request Request) error {
	if len(request.Messages) == 0 {
		return errors.New("a reduction takes apart a sequence holding at least one occurrence")
	}
	if len(request.Messages) > MaxCandidates {
		return errors.New("a reduction takes apart at most 256 occurrences")
	}
	held := make(map[string]bool, len(source.Events))
	for i := range source.Events {
		held[source.Events[i].ID] = true
	}
	declared := make(map[string]bool, len(request.Messages))
	for _, id := range request.Messages {
		if !occurrencePattern.MatchString(id) || declared[id] {
			return errors.New("a reduction names each occurrence of its sequence once")
		}
		if !held[id] {
			return errors.New("this sequence names an occurrence the case does not hold")
		}
		declared[id] = true
	}
	required := make(map[string]bool, len(request.Required))
	for _, id := range request.Required {
		if !declared[id] || required[id] {
			return errors.New("the occurrences a signature is stated about are occurrences of the sequence, named once")
		}
		required[id] = true
	}
	return nil
}

// partition takes the sequence apart the way the plan declared. Per occurrence
// is exactly that; by correlation keeps what the declared rules related in one
// group, so a message and the acknowledgement of it are removed together or not
// at all.
func partition(request Request) ([]Group, []Unsupported, error) {
	notes := []Unsupported{}
	position := make(map[string]int, len(request.Messages))
	for i, id := range request.Messages {
		position[id] = i
	}
	required := make(map[string]bool, len(request.Required))
	for _, id := range request.Required {
		required[id] = true
	}
	members := make([][]string, 0, len(request.Messages))
	rules := make([][]string, 0, len(request.Messages))
	switch request.Plan.Grouping {
	case GroupPerOccurrence:
		if len(request.Rules.Rules) != 0 {
			return nil, nil, errors.New("this plan groups per occurrence; a reduction applies the correlation rules a plan named or none")
		}
		for _, id := range request.Messages {
			members = append(members, []string{id})
			rules = append(rules, nil)
		}
	case GroupByCorrelation:
		report, err := correlate.Run(request.Case, request.Rules)
		if err != nil {
			return nil, nil, err
		}
		if report.CaseIdentity != request.Plan.Case {
			return nil, nil, errors.New("the correlation report describes different evidence than the case it was read from")
		}
		if report.RulesSHA256 != request.Plan.Rules {
			return nil, nil, errors.New("this plan was authored against different correlation rules than the ones it was applied with")
		}
		members, rules = relate(report, request.Messages, position)
		notes = append(notes, collisionNotes(report, position)...)
	}
	groups := make([]Group, 0, len(members))
	for i, occurrences := range members {
		slices.SortFunc(occurrences, func(a, b string) int { return position[a] - position[b] })
		group := Group{ID: fmt.Sprintf(groupNameFormat, i+1), Occurrences: occurrences}
		if declared := rules[i]; len(declared) > 0 {
			slices.Sort(declared)
			group.Rules = slices.Compact(declared)
		}
		for _, id := range occurrences {
			if required[id] {
				group.Required = true
			}
		}
		if group.Required {
			notes = append(notes, Unsupported{Code: PinnedBySignature, Group: group.ID,
				Detail: "the chosen signature is stated about an occurrence of this group, so the group was never a removal candidate"})
		}
		groups = append(groups, group)
	}
	return groups, notes, nil
}

const groupNameFormat = "g%03d"

// relate merges the occurrences of the sequence every declared rule related,
// and reports each group's members and the rules that produced it. Ordering is
// the sequence's own: a group is named by where its first occurrence is sent.
func relate(report correlate.Report, messages []string, position map[string]int) ([][]string, [][]string) {
	parent := make(map[string]string, len(messages))
	for _, id := range messages {
		parent[id] = id
	}
	var find func(string) string
	find = func(id string) string {
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	related := make(map[string][]string, len(messages))
	for _, link := range report.Links {
		var inside []string
		for _, reference := range link.Occurrences {
			if _, ok := position[reference.Occurrence]; ok {
				inside = append(inside, reference.Occurrence)
			}
		}
		if len(inside) < 2 {
			continue
		}
		for _, id := range inside[1:] {
			a, b := find(inside[0]), find(id)
			if a != b {
				parent[b] = a
			}
		}
		related[find(inside[0])] = append(related[find(inside[0])], link.Rule)
	}
	// A root's rule list is collected under whichever root existed when the
	// link was read, so it is folded into the final root before it is reported.
	folded := make(map[string][]string, len(messages))
	for root, declared := range related {
		folded[find(root)] = append(folded[find(root)], declared...)
	}
	var roots []string
	sets := make(map[string][]string, len(messages))
	for _, id := range messages {
		root := find(id)
		if _, ok := sets[root]; !ok {
			roots = append(roots, root)
		}
		sets[root] = append(sets[root], id)
	}
	slices.SortFunc(roots, func(a, b string) int { return position[sets[a][0]] - position[sets[b][0]] })
	members := make([][]string, 0, len(roots))
	rules := make([][]string, 0, len(roots))
	for _, root := range roots {
		members = append(members, sets[root])
		rules = append(rules, folded[root])
	}
	return members, rules
}

// collisionNotes records the equality the declared rules found and could not
// stand behind. Those occurrences were not grouped together, so reduction may
// remove one without the others; that is a limit of the answer and is stated.
func collisionNotes(report correlate.Report, position map[string]int) []Unsupported {
	notes := []Unsupported{}
	for _, rule := range report.Rules {
		if !rule.Applied {
			notes = append(notes, Unsupported{Code: RuleNotApplied, Rule: rule.ID,
				Detail: "this case supplied no scope for the rule, so it related nothing and contributed no group"})
		}
	}
	for _, collision := range report.Collisions {
		inside := 0
		for _, reference := range collision.Occurrences {
			if _, ok := position[reference.Occurrence]; ok {
				inside++
			}
		}
		if collision.Declaring != nil {
			if _, ok := position[collision.Declaring.Occurrence]; ok {
				inside++
			}
		}
		if inside > 1 {
			notes = append(notes, Unsupported{Code: UngroupedCollision, Rule: collision.Rule,
				Detail: "the declared rules found equality they could not stand behind (" + collision.Reason + "), so these occurrences were not put in one group"})
		}
	}
	return notes
}

type session struct {
	request   Request
	groups    []Group
	indexed   map[string]Group
	position  map[string]int
	notes     []Unsupported
	oracle    Oracle
	budget    int
	attempted int
	trials    []Trial
	report    Report
}

// reduce is the whole bounded search: calibrate the oracle against the
// unreduced sequence, remove one group at a time until a full pass removes
// nothing, then ask the calibration question again of what survived.
func (s *session) reduce(ctx context.Context) Report {
	candidate := make([]string, 0, len(s.groups))
	for _, group := range s.groups {
		candidate = append(candidate, group.ID)
	}
	// 1. The unreduced sequence must reproduce the chosen failure, repeatably,
	// before a single group is removed. An oracle that cannot answer the same
	// question twice about the same sequence cannot be reduced against.
	if report, done := s.agrees(ctx, Calibration, candidate); done {
		return report
	}
	// 2. Remove one group at a time. A successful removal restarts the pass, so
	// the pass that ends the loop is a complete one over everything left, which
	// is what makes the 1-minimality claim below true rather than plausible.
	for progress := true; progress; {
		progress = false
		for _, id := range slices.Clone(candidate) {
			if len(candidate) == 1 || s.indexed[id].Required {
				continue
			}
			trial, stopped := s.trial(ctx, Removal, id, without(candidate, id))
			if stopped != "" {
				return s.finish(stoppedBy(stopped), stopped, candidate, NoClaim)
			}
			if trial.Verdict == Undecided {
				return s.finish(OutcomeUndecided, trial.Reason, candidate, NoClaim)
			}
			s.attempted++
			if trial.Verdict == Reproduced {
				candidate = without(candidate, id)
				progress = true
				break
			}
		}
	}
	// A partition with nothing removable in it is not a reduction that removed
	// nothing; no removal was ever tried, so no claim is made about one.
	if s.attempted == 0 {
		return s.finish(OutcomeNotAttempted, NoRemovableGroup, candidate, NoClaim)
	}
	// 3. Ask the surviving sequence the calibration question again. Every
	// removal committed above rests on one positive answer, and a positive
	// answer is the one an unstable oracle gets wrong in the direction that
	// throws evidence away.
	if report, done := s.agrees(ctx, Confirmation, candidate); done {
		return report
	}
	return s.finish(OutcomeReduced, EveryGroupRequired, candidate, GroupOneMinimal)
}

// agrees runs one candidate the declared number of times and requires the same
// positive answer every time. It is the whole of the controlled-oracle check,
// and it is the same check before and after reducing: the question a
// calibration asks of the unreduced sequence is the question a confirmation
// asks of what survived.
//
// It reports a finished report and true when the reduction must stop. A
// disagreement is never a smaller sequence somebody can use.
func (s *session) agrees(ctx context.Context, purpose Purpose, candidate []string) (Report, bool) {
	for i := 0; i < s.request.Plan.Confirmations; i++ {
		trial, stopped := s.trial(ctx, purpose, "", candidate)
		if stopped != "" {
			// A budget that ran out before the unreduced sequence was ever
			// established establishes nothing. One that ran out after the
			// removals were committed leaves behind a smaller sequence nobody
			// re-confirmed, and the check it skipped is exactly the one that
			// catches an unstable oracle, so it is named apart.
			if stopped == BudgetSpent && purpose == Confirmation {
				return s.finish(OutcomeBounded, BudgetSpentUnconfirmed, candidate, NoClaim), true
			}
			return s.finish(OutcomeUndecided, stopped, candidate, NoClaim), true
		}
		switch trial.Verdict {
		case Undecided:
			return s.finish(OutcomeUndecided, trial.Reason, candidate, NoClaim), true
		case NotReproduced:
			reason := FlakyOracle
			if purpose == Calibration && i == 0 {
				reason = BaselineNotReproduced
			}
			return s.finish(OutcomeUndecided, reason, candidate, NoClaim), true
		}
	}
	return Report{}, false
}

// stoppedBy is the outcome a stopped removal loop deserves: a spent budget left
// behind every removal it had already committed, and every other way of
// stopping left nothing anybody can act on.
func stoppedBy(stopped Reason) Outcome {
	if stopped == BudgetSpent {
		return OutcomeBounded
	}
	return OutcomeUndecided
}

// trial runs one candidate once. It reports the trial and, separately, the
// reason the reduction itself must stop: a spent budget and an interruption are
// answers about the reduction, not about the candidate, so neither is recorded
// as a verdict about one.
func (s *session) trial(ctx context.Context, purpose Purpose, removed string, candidate []string) (Trial, Reason) {
	if ctx.Err() != nil {
		return Trial{}, OperatorStopped
	}
	if s.budget <= 0 {
		return Trial{}, BudgetSpent
	}
	s.budget--
	trial := Trial{Index: len(s.trials) + 1, Purpose: purpose, Candidate: slices.Clone(candidate), Removed: removed, Failed: []string{}}
	outcome, reason := s.oracle.Reset(ctx)
	trial.Reset, trial.ResetReason = outcome, reason
	if outcome != fixturereset.Confirmed {
		trial.Verdict, trial.Reason = Undecided, ResetNotConfirmed
		s.trials = append(s.trials, trial)
		return trial, ""
	}
	observed, err := s.oracle.Observe(ctx, s.occurrences(candidate))
	if err != nil {
		trial.Verdict, trial.Reason = Undecided, OracleUnavailable
		s.trials = append(s.trials, trial)
		return trial, ""
	}
	trial.State = observed.State
	if len(observed.Failed) > 0 {
		trial.Failed = slices.Clone(observed.Failed)
	}
	trial.Verdict, trial.Reason = s.verdict(observed)
	s.trials = append(s.trials, trial)
	return trial, ""
}

// verdict reads one observation as an answer about the chosen failure. Only a
// run that stopped as an assertion failure is an answer at all; every other way
// a run can stop is named and undecided, because a timeout, a cancellation, an
// execution error and an uncertain delivery each say nothing about whether an
// expectation was wrong.
func (s *session) verdict(observed Observation) (Verdict, Reason) {
	if observed.DeliveryUncertain {
		return Undecided, RunDeliveryUnknown
	}
	switch observed.State {
	case durablerun.Passed:
		return NotReproduced, ""
	case durablerun.AssertionFailed:
		if s.request.Plan.Signature.matches(observed.Failed) {
			return Reproduced, ""
		}
		return NotReproduced, ""
	case durablerun.TimedOut:
		return Undecided, RunTimedOut
	case durablerun.Cancelled:
		return Undecided, RunCancelled
	case durablerun.Interrupted:
		return Undecided, RunInterrupted
	case durablerun.ExecutionError:
		return Undecided, RunExecutionError
	case durablerun.DeliveryUncertain:
		return Undecided, RunDeliveryUnknown
	default:
		return Undecided, RunNotTerminal
	}
}

// occurrences flattens a candidate back into the sequence the oracle sends, in
// the order the unreduced sequence sent it.
func (s *session) occurrences(candidate []string) []string {
	held := make(map[string]bool, len(candidate))
	for _, id := range candidate {
		held[id] = true
	}
	occurrences := make([]string, 0, len(s.request.Messages))
	for _, group := range s.groups {
		if !held[group.ID] {
			continue
		}
		occurrences = append(occurrences, group.Occurrences...)
	}
	slices.SortFunc(occurrences, func(a, b string) int { return s.position[a] - s.position[b] })
	return occurrences
}

func (s *session) finish(outcome Outcome, reason Reason, candidate []string, minimality Minimality) Report {
	retained := s.occurrences(candidate)
	removed := make([]string, 0, len(s.request.Messages))
	for _, id := range s.request.Messages {
		if !slices.Contains(retained, id) {
			removed = append(removed, id)
		}
	}
	s.report.Groups = s.groups
	s.report.Trials = s.trials
	s.report.Outcome = outcome
	s.report.Reason = reason
	s.report.Minimality = minimality
	s.report.Retained = retained
	s.report.Removed = removed
	s.report.Unsupported = s.notes
	s.report.Summary = Summary{
		Groups: len(s.groups), Trials: len(s.trials), Budget: s.request.Plan.Trials,
		Retained: len(retained), Removed: len(removed), Unsupported: len(s.notes),
	}
	s.report.Scope = boundaryStatement
	return s.report
}

func without(candidate []string, id string) []string {
	remaining := make([]string, 0, len(candidate))
	for _, held := range candidate {
		if held != id {
			remaining = append(remaining, held)
		}
	}
	return remaining
}

// Preview is what a reduction would spend before any trial runs: how the
// sequence is taken apart, which groups the signature pins, and the same
// boundary statement a finished report carries. It opens and verifies the
// case, writes nothing, and asks the oracle nothing.
type Preview struct {
	Case        Artifact      `json:"case"`
	Plan        Plan          `json:"plan"`
	Messages    []string      `json:"messages"`
	Required    []string      `json:"required"`
	Groups      []Group       `json:"groups"`
	Unsupported []Unsupported `json:"unsupported"`
	Scope       string        `json:"scope"`
}

// PreviewPlan reports how one plan would take a sequence apart. It is the
// side-effect preview a person reads before authorising execution: no reset,
// no send, and no derived case.
func PreviewPlan(request Request) (Preview, error) {
	if err := validatePlan(request.Plan); err != nil {
		return Preview{}, err
	}
	source, err := bundle.Open(request.Case)
	if err != nil {
		return Preview{}, errors.New("the case could not be verified as complete, unmodified evidence")
	}
	if source.Identity != request.Plan.Case {
		return Preview{}, errors.New("this plan was authored against different evidence than the case it was applied to")
	}
	if err := validateCandidates(source, request); err != nil {
		return Preview{}, err
	}
	groups, notes, err := partition(request)
	if err != nil {
		return Preview{}, err
	}
	return Preview{
		Case:        Artifact{Schema: source.Manifest.Schema, Identity: source.Identity},
		Plan:        request.Plan,
		Messages:    slices.Clone(request.Messages),
		Required:    slices.Clone(request.Required),
		Groups:      groups,
		Unsupported: notes,
		Scope:       boundaryStatement,
	}, nil
}
