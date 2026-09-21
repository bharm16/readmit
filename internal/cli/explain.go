package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/spf13/cobra"
)

// hidden is what stands in for one customer-local value nobody asked to see.
// Expected and observed values are both message content — an expectation is
// the value an interface should have produced — so both are hidden by the same
// flag `timeline`, `diff` and `index search` already use.
const hidden = "hidden"

// explainCommand explains what one run's evidence decided.
//
// It is the reading half of `observe explain`, and it keeps that command's
// discipline: it re-decides from retained evidence rather than reporting a
// stored verdict, and it refuses a question the evidence cannot answer instead
// of answering a narrower one. Nothing is opened but the artifacts named on the
// command line, nothing is sent, and nothing is written.
func explainCommand() *cobra.Command {
	var assertions, before, beforeSource, after, afterSource string
	var showValues bool
	command := &cobra.Command{
		Use:         "explain RUN --assertions set",
		Annotations: declare(capabilityFree),
		Short:       "Explain what a run's evidence decided, assertion by assertion",
		RunE: func(cmd *cobra.Command, args []string) error {
			if assertions == "" {
				return usage("explain requires --assertions naming one %s document", assertion.Schema)
			}
			explanation, err := runexplain.Explain(cmd.Context(), runexplain.Input{
				Run: args[0], Assertions: assertions,
				Before: runexplain.ObservationInput{Completion: before, Source: beforeSource},
				After:  runexplain.ObservationInput{Completion: after, Source: afterSource},
			})
			if err != nil {
				return refusal(err)
			}
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				writeExplanation(w, explanation, cmd.Root().Version, showValues)
			}); err != nil {
				return refusal(errors.New("cannot write the explanation"))
			}
			return explainStatus(explanation)
		},
	}
	command.Flags().StringVar(&assertions, "assertions", "", "Existing "+assertion.Schema+" document holding the expectations to re-decide")
	command.Flags().StringVar(&before, "before", "", "Existing observation completion record for the records observed before the run")
	command.Flags().StringVar(&beforeSource, "before-source", "", "Existing observation source document naming the evidence that observation read")
	command.Flags().StringVar(&after, "after", "", "Existing observation completion record for the records observed after the run")
	command.Flags().StringVar(&afterSource, "after-source", "", "Existing observation source document naming the evidence that observation read")
	command.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display expected and observed values as escaped byte strings")
	return command
}

// explainStatus turns one explanation into a process status. A decided
// disagreement is status 1, exactly as a failing test is. Everything that left
// the question open — an undecided verdict and an execution error alike — is
// status 2, because neither told the caller what the interface did.
//
// Each diagnostic here is already reported: the rendering above names the
// execution error's class and the assertion it was asking about, and which of
// the six classes it was is what distinguishes a cancellation from evidence
// nobody could read. Restating one of them here would state it for all six.
func explainStatus(explanation runexplain.Explanation) error {
	if explanation.Failure != nil {
		return statedRefusal(errors.New("this run has no verdict; the explanation names the execution error"))
	}
	switch explanation.Verdict {
	case assertion.VerdictFail:
		return failure(errors.New("this run disagreed with its expectations"))
	case assertion.VerdictPass:
		return nil
	default:
		return statedRefusal(errors.New("this run's evidence decided nothing; unknown is not a pass"))
	}
}

// writeExplanation renders one explanation in the order somebody reads it:
// what it decided, what it was decided from, and then every assertion with the
// evidence it was decided on.
func writeExplanation(w io.Writer, explanation runexplain.Explanation, version string, showValues bool) {
	writeExplanationSummary(w, explanation)
	writeExplanationRun(w, explanation)
	for _, observed := range explanation.Observations {
		writeExplanationObservation(w, observed, showValues)
	}
	fmt.Fprintln(w)
	for _, detail := range explanation.Details {
		writeExplanationDetail(w, detail, showValues)
	}
	fmt.Fprintln(w, "Unknown is a third answer and it is not a pass: an assertion the evidence could not decide is undecided, and one whose condition did not hold asserted nothing.")
	fmt.Fprintf(w, "Explained by readmit %s, which is the build that re-decided this set; a run bundle records no build of its own.\n", absent(version))
	fmt.Fprintln(w, "This explanation re-decided the set against retained evidence. It opened nothing else, sent nothing and wrote nothing.")
	if !showValues {
		fmt.Fprintln(w, "Expected and observed values are customer-local evidence and are hidden; --show-values displays them as escaped byte strings.")
	}
}

func writeExplanationSummary(w io.Writer, explanation runexplain.Explanation) {
	if explanation.Failure != nil {
		fmt.Fprintf(w, "Verdict: none, execution error %s\nAssertion: %s\nEvery assertion is left unevaluated rather than reported against evidence that could not be read.\n",
			explanation.Failure.Class, absent(explanation.Failure.Assertion))
	} else {
		fmt.Fprintf(w, "Verdict: %s\n", explanation.Verdict)
	}
	fmt.Fprintf(w, "Assertions: %d declared; %d passed, %d failed, %d undecided, %d skipped\n",
		explanation.Set.Count, explanation.Passed, explanation.Failed, explanation.Undecided, explanation.Skipped)
	fmt.Fprintf(w, "Assertion set: %s\nSet contract: %s\nSet identity: %s\n",
		explanation.Set.Name, explanation.Set.Schema, explanation.Set.Identity)
}

func writeExplanationRun(w io.Writer, explanation runexplain.Explanation) {
	run := explanation.Run
	fmt.Fprintf(w, "\nRun contract: %s\nRun state: %s\nRun identity: %s\nInput case identity: %s\nContains source values: %t (%s)\n",
		run.Schema, run.State, run.Identity, run.SourceBundleIdentity, run.ContainsSourceValues, run.ExportPolicy)
	fmt.Fprintf(w, "Target: %s over %s, test endpoint %t, approved transport %t\nTarget identity: %s\n",
		run.Target.Address, run.Target.Transport, run.Target.TestEndpoint, run.Target.ApprovedTransport, run.TargetIdentity)
	fmt.Fprintf(w, "Certificate authority: %s\nTimeouts: connect %s, message %s; maximum acknowledgement %d bytes\n",
		absent(run.Target.CASHA256), run.Target.ConnectTimeout, run.Target.MessageTimeout, run.Target.MaxACKBytes)
	fmt.Fprintf(w, "Transformations: %s\n", absent(transformationNames(run)))
	fmt.Fprintf(w, "Started: %s\nCompleted: %s\nElapsed: %s\n",
		instant(run.StartedAt), instant(run.CompletedAt), run.Elapsed())
	fmt.Fprintf(w, "Messages: %d\n", len(run.Messages))
	for _, message := range run.Messages {
		fmt.Fprintf(w, "  %s as %s: %s, delivery %s, acknowledgement %s %s, elapsed %s\n",
			message.Source, message.Outbound, message.Outcome, message.Delivery,
			absent(message.ACKCode), message.ACKCorrelation, message.Elapsed)
		fmt.Fprintf(w, "    input %s, observed %s\n", sentState(message), payloadState(message.Received, message.ReceivedReadable))
	}
}

// instant renders one recorded time the way every other retained record spells
// one.
func instant(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// sentState describes the payload an input assertion would read. A send fewer
// bytes of which reached the peer than the run intended is named as the
// truncation it was: a field those bytes never reached is not a field the
// message omitted, so a partial send is never evidence.
func sentState(message runexplain.Message) string {
	if message.SentPartial && message.Sent != "" {
		return message.Sent + " (retained; a partial send, so it is not evidence of any value)"
	}
	return payloadState(message.Sent, message.SentReadable)
}

// payloadState names one retained payload and whether it could be read as a
// message. A payload that was retained and did not parse is reported as that
// rather than left out, because an assertion naming it is an execution error
// and this is the line that explains why.
func payloadState(path string, readable bool) string {
	if path == "" {
		return "no payload retained"
	}
	if !readable {
		return path + " (retained; not readable as one message)"
	}
	return path
}

func transformationNames(run runexplain.RunContext) string {
	names := make([]string, 0, len(run.Transformations))
	for _, transformation := range run.Transformations {
		names = append(names, transformation.Name)
	}
	return strings.Join(names, ", ")
}

func writeExplanationObservation(w io.Writer, observed runexplain.ObservationContext, showValues bool) {
	fmt.Fprintf(w, "\nObservation (%s): %s\nObservation contract: %s, read through %s\nWindow: %s\nSource: kind %s, identity %s, scope %s\n",
		observed.Scope, observed.Status, observed.Schema, observed.SourceSchema, observed.WindowIdentity,
		observed.Source.Kind, observed.Source.Identity, observed.Source.Scope)
	fmt.Fprintf(w, "Opened: %s\nClosed: %s\nSettled: %d records after %d identical observations spanning %s\n",
		instant(observed.OpenedAt), instant(observed.ClosedAt),
		observed.Records, observed.StableSamples, observed.QuietPeriod)
	fmt.Fprintf(w, "Correlations: %s\n", correlationLine(observed))
	fmt.Fprintf(w, "Records derived again from: %s\nThe completion retains a count and a state digest rather than a key list, so the keys were read again from that capture and checked against both.\n", observed.CapturePath)
	fmt.Fprintf(w, "Keys: %s\n", keyList(observed.Keys, showValues))
}

// correlationLine states what binds this observation to this run. The
// correlations a collection recorded are the only thing in a completion that
// names a run at all, and every one of them has been checked against what this
// run actually produced. A record carrying none says so rather than implying a
// link nobody recorded.
func correlationLine(observed runexplain.ObservationContext) string {
	if len(observed.Correlations) == 0 {
		return "none recorded, so nothing in this record binds the observation to this run"
	}
	return fmt.Sprintf("%d recorded, %d matched, %d unmatched, %d ambiguous; every one was produced by this run",
		len(observed.Correlations), observed.Matched, observed.Unmatched, observed.Ambiguous)
}

func keyList(keys []string, showValues bool) string {
	if len(keys) == 0 {
		return "none"
	}
	if !showValues {
		return fmt.Sprintf("%d, %s", len(keys), hidden)
	}
	quoted := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted = append(quoted, strconv.QuoteToASCII(key))
	}
	return strings.Join(quoted, ", ")
}

func writeExplanationDetail(w io.Writer, detail runexplain.Detail, showValues bool) {
	fmt.Fprintf(w, "%s: %s %s\n", detail.ID, detail.Operator, absent(string(detail.Outcome)))
	fmt.Fprintf(w, "  Reads: %s\n", subjectLine(detail.Subject))
	if detail.When != nil {
		fmt.Fprintf(w, "  Condition: %s equals %s\n", fieldRefLine(detail.When.Field), valueLine(detail.When.Equals, showValues))
	}
	fmt.Fprintf(w, "  Expected: %s\n", expectedLine(detail, showValues))
	fmt.Fprintf(w, "  Observed: %s\n", observedLine(detail, showValues))
	for _, link := range detail.Evidence {
		fmt.Fprintf(w, "  Evidence: %s\n", linkLine(link))
	}
}

func subjectLine(subject assertion.Subject) string {
	switch {
	case subject.Field != nil:
		return fieldRefLine(*subject.Field)
	case subject.Pair != nil:
		return fieldRefLine(subject.Pair.Left) + " and " + fieldRefLine(subject.Pair.Right)
	case subject.Collection != nil:
		return "the records of the " + string(subject.Collection.Scope) + " observation"
	case subject.Each != nil:
		return string(subject.Each.Quantifier) + " record of the " + string(subject.Each.Scope) + " observation"
	case subject.Transition != nil:
		return "the transition from the " + string(subject.Transition.From) + " observation to the " + string(subject.Transition.To) + " observation"
	}
	return "nothing this release can address"
}

func fieldRefLine(ref assertion.FieldRef) string {
	return ref.Selector + " of " + string(ref.Scope) + " message " + ref.Message
}

// valueLine renders one four-state value. Only a present value carries text,
// so only a present value can hide any.
func valueLine(value assertion.FieldValue, showValues bool) string {
	if value.Text == nil {
		return string(value.State)
	}
	if !showValues {
		return string(value.State) + ", text " + hidden
	}
	return string(value.State) + ", text " + strconv.QuoteToASCII(*value.Text)
}

// expectedLine states what one assertion required, in the operator's own
// terms. Counts are aggregates and are always shown; every value drawn from
// message content is hidden by default, including one an author wrote down.
func expectedLine(detail runexplain.Detail, showValues bool) string {
	expected := detail.Expected
	switch {
	case expected.Field != nil && detail.Operator == assertion.FieldNotEquals:
		return "a value differing from " + valueLine(*expected.Field, showValues)
	case expected.Field != nil:
		return valueLine(*expected.Field, showValues)
	case expected.State != nil:
		return "state " + string(*expected.State)
	case expected.Pattern != nil:
		if !showValues {
			return "present text matching one bounded declared pattern (" + hidden + ")"
		}
		return "present text matching " + strconv.QuoteToASCII(*expected.Pattern)
	case expected.Range != nil:
		if !showValues {
			return "a present decimal inside a declared range (" + hidden + ")"
		}
		return "a present decimal inside " + strconv.QuoteToASCII(expected.Range.Min) + " to " + strconv.QuoteToASCII(expected.Range.Max) + ", inclusively"
	case expected.Tolerance != nil:
		if !showValues {
			return "a present decimal within a declared tolerance of a declared value (" + hidden + ")"
		}
		return "a present decimal within " + strconv.QuoteToASCII(expected.Tolerance.Tolerance) + " of " + strconv.QuoteToASCII(expected.Tolerance.Value)
	case expected.Window != nil:
		if !showValues {
			return "a present HL7 timestamp inside a declared window (" + hidden + ")"
		}
		return "a present HL7 timestamp inside " + strconv.QuoteToASCII(expected.Window.From) + " to " + strconv.QuoteToASCII(expected.Window.To) + ", inclusively"
	case expected.Count != nil:
		return fmt.Sprintf("exactly %d records", *expected.Count)
	case expected.Keys != nil:
		return declaredKeys(*expected.Keys, showValues)
	case expected.Multiplicity != nil:
		if !showValues {
			return fmt.Sprintf("one declared key (%s) producing exactly %d records", hidden, expected.Multiplicity.Count)
		}
		return fmt.Sprintf("the key %s producing exactly %d records", strconv.QuoteToASCII(expected.Multiplicity.Key), expected.Multiplicity.Count)
	case expected.Change != nil:
		return fmt.Sprintf("exactly %d distinct keys added and %d removed", expected.Change.Added, expected.Change.Removed)
	case expected.Holds != nil:
		return holdsLine(detail.Operator, *expected.Holds)
	}
	return "nothing this release can express"
}

// holdsLine spells out what a boolean expectation means for the one operator
// that carries it, so a reader never has to remember which way true points.
func holdsLine(operator assertion.Operator, holds bool) string {
	positive, negative := "the condition holds", "the condition does not hold"
	switch operator {
	case assertion.ValuesEqual:
		positive, negative = "the two values are equal", "the two values differ"
	case assertion.RecordsUnique:
		positive, negative = "every observed key is distinct", "some observed key repeats"
	case assertion.RecordsAbsent:
		positive, negative = "the observation held no record at all", "the observation held at least one record"
	}
	if holds {
		return positive
	}
	return negative
}

// observedLine states what deciding one assertion actually read. A skipped
// assertion read nothing beyond its own condition, and saying so is the point:
// a skipped assertion is not a pass.
func observedLine(detail runexplain.Detail, showValues bool) string {
	reading := detail.Observed
	switch {
	case detail.Outcome == assertion.OutcomeSkipped:
		return "not read; the condition did not hold, so this assertion asserted nothing"
	case detail.Outcome == "":
		return "not evaluated"
	case reading.Field != nil && reading.Compared != nil:
		return valueLine(*reading.Field, showValues) + " and " + valueLine(*reading.Compared, showValues)
	case reading.Field != nil:
		return valueLine(*reading.Field, showValues)
	case reading.Added != nil && reading.Removed != nil:
		return fmt.Sprintf("%d distinct keys added and %d removed", *reading.Added, *reading.Removed)
	case reading.Records != nil && reading.Matched != nil:
		return fmt.Sprintf("%d records held, %d matched", *reading.Records, *reading.Matched)
	case reading.Records != nil:
		return fmt.Sprintf("%d records held", *reading.Records)
	}
	return "nothing the evidence could decide"
}

// linkLine names where one value can be read again. A field link names the
// exact payload file where the run retained one; a link the run holds no
// occurrence for says so, which is what makes an unknown_message actionable.
func linkLine(link runexplain.Link) string {
	if link.Selector == "" {
		return "the " + link.Scope + " observation's records, in " + absent(link.Artifact)
	}
	if link.Payload == "" {
		return link.Scope + " " + link.Selector + " of " + link.Message + ": this run retained no readable payload for that occurrence"
	}
	return link.Scope + " " + link.Selector + " of " + link.Message + ": " + link.Artifact + "/" + link.Payload
}

// declaredKeys states how many record keys an expectation named, and which
// ones only when values were asked for. An expectation is the value an
// interface should have produced, so it is the same customer-local evidence
// the reading is.
func declaredKeys(keys []string, showValues bool) string {
	label := fmt.Sprintf("%d declared keys", len(keys))
	if len(keys) == 1 {
		label = "1 declared key"
	}
	if !showValues {
		return label + ", " + hidden
	}
	return label + ": " + keyList(keys, showValues)
}
