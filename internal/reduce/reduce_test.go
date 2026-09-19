package reduce_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/reduce"
)

const fixtureRoot = "../../testdata/fixtures/"

// The sequence every scripted reduction below takes apart: four messages of one
// source, of which the second only fails when the first is sent before it.
var sequence = []string{"s0001-e000001", "s0002-e000001", "s0003-e000001", "s0004-e000001"}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// caseOf writes one verified case holding the named fixtures, one source per
// fixture, and returns its directory and verified identity.
func caseOf(t *testing.T, names ...string) (string, string) {
	t.Helper()
	var inputs []bundle.Input
	for _, name := range names {
		inputs = append(inputs, bundle.Input{Data: fixture(t, name), Options: hl7.Options{Format: hl7.Raw}})
	}
	path := filepath.Join(t.TempDir(), "case")
	written, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{
		Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		GeneratorVersion: "readmit-test-fixture-v1", ProfileVersion: "readmit-siu-v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return path, written.Identity
}

// fourMessages is the one-source case every scripted reduction runs against.
func fourMessages(t *testing.T) (string, string) {
	t.Helper()
	return caseOf(t, "listen-s12.hl7", "listen-s13.hl7", "diagnose-booking.hl7", "diagnose-reschedule.hl7")
}

func plan(identity string, trials, confirmations int) reduce.Plan {
	return reduce.Plan{
		Schema: reduce.PlanSchema, Case: identity, Grouping: reduce.GroupPerOccurrence,
		Signature:     reduce.Signature{State: durablerun.AssertionFailed, Assertions: []string{"reschedule-accepted"}},
		Trials:        trials,
		Confirmations: confirmations,
	}
}

type oracle struct {
	reset   func(int) (fixturereset.Outcome, fixturereset.Reason)
	observe func(int, []string) (reduce.Observation, error)
	resets  int
	seen    [][]string
}

func (o *oracle) Reset(context.Context) (fixturereset.Outcome, fixturereset.Reason) {
	o.resets++
	if o.reset == nil {
		return fixturereset.Confirmed, fixturereset.EveryActionConfirmed
	}
	return o.reset(o.resets)
}

func (o *oracle) Observe(_ context.Context, occurrences []string) (reduce.Observation, error) {
	o.seen = append(o.seen, slices.Clone(occurrences))
	return o.observe(len(o.seen), occurrences)
}

func failed() reduce.Observation {
	return reduce.Observation{State: durablerun.AssertionFailed, Failed: []string{"reschedule-accepted"}}
}

func passed() reduce.Observation {
	return reduce.Observation{State: durablerun.Passed, Failed: []string{}}
}

// failsWhen is the defect the scripted reductions are looking for: the chosen
// assertion fails exactly when every named occurrence is still in the sequence.
func failsWhen(needed ...string) func(int, []string) (reduce.Observation, error) {
	return func(_ int, occurrences []string) (reduce.Observation, error) {
		for _, id := range needed {
			if !slices.Contains(occurrences, id) {
				return passed(), nil
			}
		}
		return failed(), nil
	}
}

func run(t *testing.T, path string, declared reduce.Plan, messages, required []string, answer *oracle) reduce.Report {
	t.Helper()
	report, err := reduce.Run(t.Context(), reduce.Request{
		Case: path, Plan: declared, Messages: messages, Required: required,
	}, answer)
	if err != nil {
		t.Fatalf("reduction refused a request it should have run: %v", err)
	}
	return report
}

// TestReductionRetainsTheSetupDependencyTheFailureNeeds is the delivery through
// its public interface: a four-message sequence whose chosen assertion only
// fails when an earlier message set the state up is reduced to exactly those
// two, the messages nothing needed are gone, and the claim made about the
// result is 1-minimality over the declared grouping and nothing wider.
func TestReductionRetainsTheSetupDependencyTheFailureNeeds(t *testing.T) {
	path, identity := fourMessages(t)
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	report := run(t, path, plan(identity, 64, 2), sequence, []string{sequence[1]}, answer)

	if report.Outcome != reduce.OutcomeReduced || report.Reason != reduce.EveryGroupRequired {
		t.Fatalf("a reduction that ran to a fixpoint is reduced: %s (%s)", report.Outcome, report.Reason)
	}
	if report.Minimality != reduce.GroupOneMinimal {
		t.Fatalf("a completed fixpoint claims group-1-minimality: %s", report.Minimality)
	}
	if !slices.Equal(report.Retained, sequence[:2]) {
		t.Fatalf("the setup dependency and the failing message are retained: %v", report.Retained)
	}
	if !slices.Equal(report.Removed, sequence[2:]) {
		t.Fatalf("the occurrences nothing needed are removed: %v", report.Removed)
	}
	if report.Schema != reduce.ReportSchema || report.Case.Identity != identity {
		t.Fatalf("a report names its contract and the evidence it was produced against: %+v", report.Case)
	}
	// Every trial resets first, and no trial ran after the budget.
	if answer.resets != len(report.Trials) || len(report.Trials) > report.Plan.Trials {
		t.Fatalf("each of %d trials resets once within a budget of %d: %d resets", len(report.Trials), report.Plan.Trials, answer.resets)
	}
	// The last trials are the confirmations of what survived, not removals.
	for _, trial := range report.Trials[len(report.Trials)-2:] {
		if trial.Purpose != reduce.Confirmation || trial.Verdict != reduce.Reproduced {
			t.Fatalf("the surviving sequence is asked the calibration question again: %+v", trial)
		}
	}
	// The group the signature is stated about was never a removal candidate,
	// and the report says so rather than leaving it to be inferred.
	for _, trial := range report.Trials {
		if trial.Removed != "" && slices.Contains(report.Groups[1].Occurrences, trial.Removed) {
			t.Fatalf("a group the signature is stated about was tried for removal: %+v", trial)
		}
	}
	if !slices.ContainsFunc(report.Unsupported, func(note reduce.Unsupported) bool {
		return note.Code == reduce.PinnedBySignature && note.Group == report.Groups[1].ID
	}) {
		t.Fatalf("a pinned group is recorded as one: %+v", report.Unsupported)
	}
}

// TestAnUnreducedSequenceThatDoesNotFailIsNotReduced keeps a reduction from
// starting at all when the failure it was asked to preserve is not there.
func TestAnUnreducedSequenceThatDoesNotFailIsNotReduced(t *testing.T) {
	path, identity := fourMessages(t)
	answer := &oracle{observe: func(int, []string) (reduce.Observation, error) { return passed(), nil }}
	report := run(t, path, plan(identity, 64, 2), sequence, nil, answer)

	if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.BaselineNotReproduced {
		t.Fatalf("a sequence that never reproduced the signature establishes nothing: %s (%s)", report.Outcome, report.Reason)
	}
	if report.Minimality != reduce.NoClaim || !slices.Equal(report.Retained, sequence) || len(report.Removed) != 0 {
		t.Fatalf("nothing is removed and nothing is claimed: %+v", report)
	}
	if len(report.Trials) != 1 {
		t.Fatalf("the budget is not spent on a sequence that did not fail once: %d trials", len(report.Trials))
	}
}

// TestAFlakyOracleIsNeverReducedAgainst is the controlled-oracle rule from both
// ends. An oracle that answers differently about the same unreduced sequence is
// refused before a group is removed, and one that answers differently about the
// sequence that survived withdraws the result rather than reporting it.
func TestAFlakyOracleIsNeverReducedAgainst(t *testing.T) {
	path, identity := fourMessages(t)
	for name, answers := range map[string]func(int, []string) (reduce.Observation, error){
		"during calibration": func(call int, _ []string) (reduce.Observation, error) {
			if call == 1 {
				return failed(), nil
			}
			return passed(), nil
		},
		"during confirmation": func(call int, occurrences []string) (reduce.Observation, error) {
			// Reproduces everything until the surviving sequence is asked
			// again, at which point it stops agreeing with itself.
			if len(occurrences) == 1 && call > 6 {
				return passed(), nil
			}
			return failed(), nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			answer := &oracle{observe: answers}
			report := run(t, path, plan(identity, 64, 3), sequence, nil, answer)
			if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.FlakyOracle {
				t.Fatalf("an oracle that disagreed with itself is named as one: %s (%s)", report.Outcome, report.Reason)
			}
			if report.Minimality != reduce.NoClaim {
				t.Fatalf("an undecided reduction claims no minimality: %s", report.Minimality)
			}
		})
	}
}

// TestNoStoppedRunIsReadAsTheChosenFailure is the rule the ticket names in its
// own words: a timeout is not an equivalent reduced failure. Every way a run
// can stop that is not an assertion verdict decides nothing, keeps its own
// reason, and stops the reduction with the sequence it was holding intact.
func TestNoStoppedRunIsReadAsTheChosenFailure(t *testing.T) {
	path, identity := fourMessages(t)
	for _, stopped := range []struct {
		observed reduce.Observation
		reason   reduce.Reason
	}{
		{reduce.Observation{State: durablerun.TimedOut}, reduce.RunTimedOut},
		{reduce.Observation{State: durablerun.Cancelled}, reduce.RunCancelled},
		{reduce.Observation{State: durablerun.Interrupted}, reduce.RunInterrupted},
		{reduce.Observation{State: durablerun.ExecutionError}, reduce.RunExecutionError},
		{reduce.Observation{State: durablerun.DeliveryUncertain}, reduce.RunDeliveryUnknown},
		{reduce.Observation{State: durablerun.AssertionFailed, DeliveryUncertain: true, Failed: []string{"reschedule-accepted"}}, reduce.RunDeliveryUnknown},
		{reduce.Observation{State: durablerun.Running}, reduce.RunNotTerminal},
	} {
		t.Run(string(stopped.observed.State)+"/"+string(stopped.reason), func(t *testing.T) {
			// The unreduced sequence reproduces; the first removal trial stops
			// this way instead of answering.
			answer := &oracle{observe: func(call int, _ []string) (reduce.Observation, error) {
				if call == 1 {
					return failed(), nil
				}
				return stopped.observed, nil
			}}
			report := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
			if report.Outcome != reduce.OutcomeUndecided || report.Reason != stopped.reason {
				t.Fatalf("a run that stopped without a verdict names why: %s (%s)", report.Outcome, report.Reason)
			}
			if report.Minimality != reduce.NoClaim || !slices.Equal(report.Retained, sequence) {
				t.Fatalf("an undecided trial removes nothing and claims nothing: %+v", report)
			}
			last := report.Trials[len(report.Trials)-1]
			if last.Verdict != reduce.Undecided || last.Reason != stopped.reason {
				t.Fatalf("the trial itself records the undecided verdict: %+v", last)
			}
		})
	}
}

// TestADifferentFailureIsNotTheChosenFailure keeps a reduction from walking
// away from the failure it was asked to preserve. Only the exact set of failed
// assertions the signature names is that failure.
func TestADifferentFailureIsNotTheChosenFailure(t *testing.T) {
	path, identity := fourMessages(t)
	for name, observed := range map[string]reduce.Observation{
		"another assertion instead": {State: durablerun.AssertionFailed, Failed: []string{"booking-accepted"}},
		"another assertion as well": {State: durablerun.AssertionFailed, Failed: []string{"reschedule-accepted", "booking-accepted"}},
		"nothing failed at all":     {State: durablerun.AssertionFailed, Failed: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			// The unreduced sequence reproduces the chosen failure; every
			// candidate smaller than it fails differently instead.
			answer := &oracle{observe: func(_ int, occurrences []string) (reduce.Observation, error) {
				if len(occurrences) == len(sequence) {
					return failed(), nil
				}
				return observed, nil
			}}
			report := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
			if report.Outcome != reduce.OutcomeReduced || !slices.Equal(report.Retained, sequence) {
				t.Fatalf("a different failure removes nothing: %s %v", report.Outcome, report.Retained)
			}
			for _, trial := range report.Trials[1:] {
				if trial.Purpose == reduce.Removal && trial.Verdict != reduce.NotReproduced {
					t.Fatalf("a different failure is not the chosen one: %+v", trial)
				}
			}
		})
	}
}

// TestAResetThatWasNotConfirmedStopsTheReduction keeps a trial from running
// against whatever state the last one left behind. Only a confirmed reset lets
// a candidate execute, and nothing is observed when it did not.
func TestAResetThatWasNotConfirmedStopsTheReduction(t *testing.T) {
	path, identity := fourMessages(t)
	for _, outcome := range []fixturereset.Outcome{
		fixturereset.Unconfirmed, fixturereset.Failed, fixturereset.Refused,
		fixturereset.Cancelled, fixturereset.NotAttempted,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			answer := &oracle{
				reset: func(call int) (fixturereset.Outcome, fixturereset.Reason) {
					if call == 1 {
						return fixturereset.Confirmed, fixturereset.EveryActionConfirmed
					}
					return outcome, fixturereset.LedgerNotEmpty
				},
				observe: func(int, []string) (reduce.Observation, error) { return failed(), nil },
			}
			report := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
			if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.ResetNotConfirmed {
				t.Fatalf("a reset nobody confirmed stops the reduction: %s (%s)", report.Outcome, report.Reason)
			}
			if len(answer.seen) != 1 {
				t.Fatalf("no candidate runs after a reset that was not confirmed: %d observations", len(answer.seen))
			}
			last := report.Trials[len(report.Trials)-1]
			if last.Reset != outcome || last.State != "" {
				t.Fatalf("the trial records the reset it got and no run state: %+v", last)
			}
		})
	}
}

// TestAnUnavailableOracleDecidesNothing separates "the failure is gone" from
// "nobody could tell".
func TestAnUnavailableOracleDecidesNothing(t *testing.T) {
	path, identity := fourMessages(t)
	answer := &oracle{observe: func(call int, _ []string) (reduce.Observation, error) {
		if call == 1 {
			return failed(), nil
		}
		return reduce.Observation{}, os.ErrPermission
	}}
	report := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
	if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.OracleUnavailable {
		t.Fatalf("an oracle that could not answer is not an answer: %s (%s)", report.Outcome, report.Reason)
	}
	if !slices.Equal(report.Retained, sequence) {
		t.Fatalf("nothing is removed on an unavailable oracle: %v", report.Retained)
	}
}

// TestTheTrialBudgetIsSpentAndNeverExceeded is the bound. A reduction that runs
// out reports what it reached as bounded, with no minimality claim on it.
func TestTheTrialBudgetIsSpentAndNeverExceeded(t *testing.T) {
	path, identity := fourMessages(t)
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	report := run(t, path, plan(identity, 3, 1), sequence, nil, answer)

	if report.Outcome != reduce.OutcomeBounded || report.Reason != reduce.BudgetSpent {
		t.Fatalf("a reduction stopped by its budget says so: %s (%s)", report.Outcome, report.Reason)
	}
	if report.Minimality != reduce.NoClaim {
		t.Fatalf("a bounded result is smaller, not minimal: %s", report.Minimality)
	}
	if len(report.Trials) != 3 || report.Summary.Budget != 3 {
		t.Fatalf("exactly the declared budget is spent: %d of %d", len(report.Trials), report.Summary.Budget)
	}
	if len(report.Retained) > len(sequence) {
		t.Fatalf("a bounded reduction retains at most what it started with: %v", report.Retained)
	}
}

// TestStoppingAReductionLeavesNoVerdict is the cancel path. A reduction a
// person interrupted is undecided, not a smaller sequence they can use.
func TestStoppingAReductionLeavesNoVerdict(t *testing.T) {
	path, identity := fourMessages(t)
	ctx, stop := context.WithCancel(t.Context())
	answer := &oracle{observe: func(call int, _ []string) (reduce.Observation, error) {
		if call == 2 {
			stop()
		}
		return failsWhen(sequence[0], sequence[1])(call, nil)
	}}
	// The second call answers about a candidate, so the cancellation lands
	// before the third trial starts.
	answer.observe = func(call int, occurrences []string) (reduce.Observation, error) {
		if call == 2 {
			stop()
		}
		return failsWhen(sequence[0], sequence[1])(call, occurrences)
	}
	report, err := reduce.Run(ctx, reduce.Request{Case: path, Plan: plan(identity, 64, 1), Messages: sequence}, answer)
	if err != nil {
		t.Fatalf("an interrupted reduction still reports: %v", err)
	}
	if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.OperatorStopped {
		t.Fatalf("an interrupted reduction is undecided: %s (%s)", report.Outcome, report.Reason)
	}
	if report.Minimality != reduce.NoClaim {
		t.Fatalf("an interrupted reduction claims nothing: %s", report.Minimality)
	}
}

// TestCorrelationGroupingRemovesRelatedOccurrencesTogether is the dependency
// grouping. Occurrences the declared rules related are one group, so a
// reduction removes them together or not at all.
func TestCorrelationGroupingRemovesRelatedOccurrencesTogether(t *testing.T) {
	path, identity := caseOf(t, "listen-s12.hl7", "listen-s13.hl7", "listen-s13.hl7", "adt-cr.hl7")
	messages := sequence
	rules, digest := controlIDRules(t, path)

	declared := plan(identity, 64, 1)
	declared.Grouping, declared.Rules = reduce.GroupByCorrelation, digest
	// The failure needs the first message and either observation of the second,
	// which the rules related across the two sources.
	answer := &oracle{observe: failsWhen("s0001-e000001", "s0002-e000001")}
	report, err := reduce.Run(t.Context(), reduce.Request{
		Case: path, Plan: declared, Rules: rules, Messages: messages,
	}, answer)
	if err != nil {
		t.Fatalf("reduction refused a correlation grouping it should have run: %v", err)
	}
	if len(report.Groups) != 3 {
		t.Fatalf("the related occurrences are one group of three: %+v", report.Groups)
	}
	if !slices.Equal(report.Groups[1].Occurrences, []string{"s0002-e000001", "s0003-e000001"}) {
		t.Fatalf("the rule's own relation is the group: %+v", report.Groups[1])
	}
	if len(report.Groups[1].Rules) == 0 {
		t.Fatalf("a group of more than one names the rules that related it: %+v", report.Groups[1])
	}
	// Every candidate holds the related occurrences together or not at all.
	for _, observed := range answer.seen {
		if slices.Contains(observed, "s0002-e000001") != slices.Contains(observed, "s0003-e000001") {
			t.Fatalf("a related occurrence was removed without the other: %v", observed)
		}
	}
	if report.Outcome != reduce.OutcomeReduced || !slices.Equal(report.Retained, []string{"s0001-e000001", "s0002-e000001", "s0003-e000001"}) {
		t.Fatalf("the whole relation is retained with the message that needs it: %s %v", report.Outcome, report.Retained)
	}
}

// controlIDRules declares one rule relating equal control IDs across the two
// sources of the case, and reports the SHA-256 a plan must name for it.
func controlIDRules(t *testing.T, path string) (correlate.Rules, string) {
	t.Helper()
	document := `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0002","s0003"]}]}`
	rules, err := correlate.ParseRules([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	report, err := correlate.Run(path, rules)
	if err != nil {
		t.Fatal(err)
	}
	return rules, report.RulesSHA256
}

// TestAReductionRefusesWhatItCannotStandBehind is the request boundary: the
// evidence a plan was authored against, the declarations it named, and the
// sequence it claims to take apart.
func TestAReductionRefusesWhatItCannotStandBehind(t *testing.T) {
	path, identity := fourMessages(t)
	rules, digest := controlIDRules(t, path)
	other := strings.Repeat("a", 64)
	answer := &oracle{observe: failsWhen(sequence[0])}

	for name, request := range map[string]reduce.Request{
		"different evidence":          {Case: path, Plan: plan(other, 8, 1), Messages: sequence},
		"an occurrence nobody holds":  {Case: path, Plan: plan(identity, 8, 1), Messages: []string{"s0009-e000001"}},
		"the same occurrence twice":   {Case: path, Plan: plan(identity, 8, 1), Messages: []string{sequence[0], sequence[0]}},
		"an empty sequence":           {Case: path, Plan: plan(identity, 8, 1), Messages: nil},
		"a pin outside the sequence":  {Case: path, Plan: plan(identity, 8, 1), Messages: sequence, Required: []string{"s0009-e000001"}},
		"rules the plan did not name": {Case: path, Plan: plan(identity, 8, 1), Rules: rules, Messages: sequence},
		"no case at all":              {Case: filepath.Join(t.TempDir(), "absent"), Plan: plan(identity, 8, 1), Messages: sequence},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reduce.Run(t.Context(), request, answer); err == nil {
				t.Fatal("a reduction ran against something it could not stand behind")
			}
		})
	}

	mismatched := plan(identity, 8, 1)
	mismatched.Grouping, mismatched.Rules = reduce.GroupByCorrelation, other
	if _, err := reduce.Run(t.Context(), reduce.Request{Case: path, Plan: mismatched, Rules: rules, Messages: sequence}, answer); err == nil {
		t.Fatal("a plan authored against different correlation rules was applied with these ones")
	}
	if digest == other {
		t.Fatal("the mismatched digest above must differ from the real one")
	}
	if _, err := reduce.Run(t.Context(), reduce.Request{Case: path, Plan: plan(identity, 8, 1), Messages: sequence}, nil); err == nil {
		t.Fatal("a reduction ran without an oracle to answer it")
	}
}

// TestAReductionChangesNoByteOfTheCase holds this package to the rule the
// reproducer editor and the transformation preview are held to.
func TestAReductionChangesNoByteOfTheCase(t *testing.T) {
	path, identity := fourMessages(t)
	before := tree(t, path)
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	run(t, path, plan(identity, 64, 1), sequence, nil, answer)
	if after := tree(t, path); after != before {
		t.Fatal("a reduction changed the evidence it read")
	}
}

func tree(t *testing.T, path string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(path, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		sum.Write([]byte(name))
		sum.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// TestTheEncodedReportReadsBackAndCarriesNoValueByte keeps the one rendering
// honest: it is the report, it reads back under its own contract, and it is
// never a second copy of the evidence it searched.
func TestTheEncodedReportReadsBackAndCarriesNoValueByte(t *testing.T) {
	path, identity := fourMessages(t)
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	report := run(t, path, plan(identity, 64, 1), sequence, []string{sequence[1]}, answer)

	document, err := reduce.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	var reopened reduce.Report
	if err := json.Unmarshal(document, &reopened, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("a rendered report reads back under its own contract: %v", err)
	}
	if reopened.Outcome != report.Outcome || reopened.Minimality != report.Minimality || !slices.Equal(reopened.Retained, report.Retained) {
		t.Fatalf("the rendered report is not the report: %+v", reopened)
	}
	if reopened.Scope != report.Scope || !strings.Contains(reopened.Scope, "not a global minimum") {
		t.Fatal("a report carries the boundary statement a reader needs before acting on it")
	}
	// No value of any message reaches the document.
	for _, name := range []string{"LISTEN-BOOK", "LISTEN-MOVE", "DIAGNOSE-BOOK", "SYNTH-001", "APPT-001"} {
		if strings.Contains(string(document), name) {
			t.Fatalf("a rendered reduction carried the value %q out of the evidence", name)
		}
	}
}

// TestAReportPastItsSizeLimitIsRefusedRatherThanTruncated holds the bound this
// package documents. A report records every trial it spent and the assertions
// each of them failed, so the limit is reachable.
func TestAReportPastItsSizeLimitIsRefusedRatherThanTruncated(t *testing.T) {
	oversized := reduce.Report{Schema: reduce.ReportSchema, Outcome: reduce.OutcomeReduced}
	failed := make([]string, 64)
	for i := range failed {
		failed[i] = strings.Repeat("a", 60000)
	}
	for i := 0; i < 10; i++ {
		oversized.Trials = append(oversized.Trials, reduce.Trial{Index: i + 1, Purpose: reduce.Removal, Failed: failed})
	}
	if _, err := reduce.JSON(oversized); err == nil {
		t.Fatal("a report past 32 MiB was encoded rather than refused")
	}
	if _, err := reduce.JSON(reduce.Report{Schema: reduce.ReportSchema}); err != nil {
		t.Fatalf("a report inside the bound still encodes: %v", err)
	}
}

// TestAPartitionWithNothingRemovableIsNotAReduction is the house rule for an
// answer nobody established: a reduction that could try no removal at all is
// recorded as one that was not attempted, never as a completed search that
// happened to remove nothing.
func TestAPartitionWithNothingRemovableIsNotAReduction(t *testing.T) {
	path, identity := fourMessages(t)
	for name, request := range map[string]struct {
		messages []string
		required []string
	}{
		"one group":          {sequence[:1], nil},
		"every group pinned": {sequence, sequence},
	} {
		t.Run(name, func(t *testing.T) {
			answer := &oracle{observe: func(int, []string) (reduce.Observation, error) { return failed(), nil }}
			report := run(t, path, plan(identity, 64, 2), request.messages, request.required, answer)
			if report.Outcome != reduce.OutcomeNotAttempted || report.Reason != reduce.NoRemovableGroup {
				t.Fatalf("no removal was tried, so no reduction was attempted: %s (%s)", report.Outcome, report.Reason)
			}
			if report.Minimality != reduce.NoClaim {
				t.Fatalf("a search that did not run claims no minimality: %s", report.Minimality)
			}
			if !slices.Equal(report.Retained, request.messages) || len(report.Removed) != 0 {
				t.Fatalf("nothing was removed: %+v", report)
			}
			for _, trial := range report.Trials {
				if trial.Purpose != reduce.Calibration {
					t.Fatalf("only the calibration ran: %+v", trial)
				}
			}
		})
	}
}

// TestABudgetSpentBeforeTheResultIsConfirmedSaysSo keeps the two ways a budget
// can run out apart. One of them skipped exactly the check that catches an
// unstable oracle, and that is worth its own name.
func TestABudgetSpentBeforeTheResultIsConfirmedSaysSo(t *testing.T) {
	path, identity := fourMessages(t)
	// Ten trials reduce this sequence: one calibration, eight removals over
	// three passes, one confirmation. Nine reach the confirmation and stop.
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	complete := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
	budget := len(complete.Trials) - 1

	stopped := run(t, path, plan(identity, budget, 1), sequence, nil, &oracle{observe: failsWhen(sequence[0], sequence[1])})
	if stopped.Outcome != reduce.OutcomeBounded || stopped.Reason != reduce.BudgetSpentUnconfirmed {
		t.Fatalf("a budget spent before the result was confirmed says so: %s (%s)", stopped.Outcome, stopped.Reason)
	}
	if stopped.Minimality != reduce.NoClaim || !slices.Equal(stopped.Retained, complete.Retained) {
		t.Fatalf("the removals stand, the confirmation does not: %+v", stopped)
	}
}

// TestNamingNoRequiredOccurrenceIsSafe records why Required is an optimisation
// rather than a soundness control: a candidate that drops the message the
// signature is about cannot report that failure, so the group is retained
// either way.
func TestNamingNoRequiredOccurrenceIsSafe(t *testing.T) {
	path, identity := fourMessages(t)
	// The failure needs the first two occurrences, and the second is the one
	// the signature would be stated about. Nothing is pinned here.
	answer := &oracle{observe: failsWhen(sequence[0], sequence[1])}
	report := run(t, path, plan(identity, 64, 1), sequence, nil, answer)
	if !slices.Equal(report.Retained, sequence[:2]) {
		t.Fatalf("an unpinned reduction retains the same sequence: %v", report.Retained)
	}
	if slices.ContainsFunc(report.Unsupported, func(note reduce.Unsupported) bool {
		return note.Code == reduce.PinnedBySignature
	}) {
		t.Fatalf("nothing was pinned, so nothing is recorded as pinned: %+v", report.Unsupported)
	}
	if !slices.ContainsFunc(report.Trials, func(trial reduce.Trial) bool {
		return trial.Removed == report.Groups[1].ID && trial.Verdict == reduce.NotReproduced
	}) {
		t.Fatal("the unpinned group was tried and kept, which is what makes omitting Required safe")
	}
}
