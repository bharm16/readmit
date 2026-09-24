package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/spf13/cobra"
)

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
		runexplain.Instant(run.StartedAt), runexplain.Instant(run.CompletedAt), run.Elapsed())
	fmt.Fprintf(w, "Messages: %d\n", len(run.Messages))
	for _, message := range run.Messages {
		fmt.Fprintf(w, "  %s as %s: %s, delivery %s, acknowledgement %s %s, elapsed %s\n",
			message.Source, message.Outbound, message.Outcome, message.Delivery,
			absent(message.ACKCode), message.ACKCorrelation, message.Elapsed)
		fmt.Fprintf(w, "    input %s, observed %s\n", message.InputPayload(), message.ObservedPayload())
	}
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
		runexplain.Instant(observed.OpenedAt), runexplain.Instant(observed.ClosedAt),
		observed.Records, observed.StableSamples, observed.QuietPeriod)
	fmt.Fprintf(w, "Correlations: %s\n", observed.CorrelationLine())
	fmt.Fprintf(w, "Records derived again from: %s\nThe completion retains a count and a state digest rather than a key list, so the keys were read again from that capture and checked against both.\n", observed.CapturePath)
	fmt.Fprintf(w, "Keys: %s\n", observed.KeysLine(showValues))
}

func writeExplanationDetail(w io.Writer, detail runexplain.Detail, showValues bool) {
	fmt.Fprintf(w, "%s: %s %s\n", detail.ID, detail.Operator, absent(string(detail.Outcome)))
	fmt.Fprintf(w, "  Reads: %s\n", detail.ReadsLine())
	if detail.When != nil {
		fmt.Fprintf(w, "  Condition: %s\n", detail.ConditionLine(showValues))
	}
	fmt.Fprintf(w, "  Expected: %s\n", detail.ExpectedLine(showValues))
	fmt.Fprintf(w, "  Observed: %s\n", detail.ObservedLine(showValues))
	for _, link := range detail.Evidence {
		fmt.Fprintf(w, "  Evidence: %s\n", link.Line())
	}
}
