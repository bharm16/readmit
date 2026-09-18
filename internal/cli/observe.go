package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/spf13/cobra"
)

// observeCommand reads the two source-neutral observation contracts. Neither
// subcommand observes anything: this release defines what a trustworthy window
// is and what a completed one records, and the collectors that fill one in are
// separate work with their own source-specific limits.
func observeCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{Use: "observe", Short: "Read declared observation windows and retained completions"}
	var windowJSON bool
	validate := &cobra.Command{Use: "validate WINDOW", Short: "Validate a declared observation window without observing anything", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("observe validate requires one observation window document")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		window, err := observewindow.ReadWindow(args[0])
		if err != nil {
			return err
		}
		if windowJSON {
			data, err := observewindow.EncodeWindow(window)
			if err != nil {
				return err
			}
			return writeObservation(cmd.OutOrStdout(), string(data)+"\n")
		}
		return writeObservation(cmd.OutOrStdout(), windowLines(window))
	}}
	validate.Flags().BoolVar(&windowJSON, "json", false, "Write the canonical readmit-observation-window/v1 document")
	var completionJSON bool
	var declared string
	explain := &cobra.Command{Use: "explain COMPLETION", Short: "Report whether a retained window completed and what it can support", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("observe explain requires one observation completion record")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		completion, err := observewindow.ReadCompletion(args[0])
		if err != nil {
			return err
		}
		if declared != "" {
			window, err := observewindow.ReadWindow(declared)
			if err != nil {
				return err
			}
			// A completion that belongs to another window, or that its own
			// samples do not support, is untrustworthy evidence rather than an
			// unreadable file, so it exits with the observation error status.
			if err := window.Verify(completion); err != nil {
				return &ExitError{Code: 2, Err: err}
			}
		}
		var rendered string
		if completionJSON {
			data, err := observewindow.EncodeCompletion(completion)
			if err != nil {
				return err
			}
			rendered = string(data)
		} else {
			rendered = completionLines(completion)
		}
		if err := writeObservation(cmd.OutOrStdout(), rendered); err != nil {
			return err
		}
		if err := completion.Err(); err != nil {
			return &ExitError{Code: 2, Err: err, Reported: !completionJSON}
		}
		return nil
	}}
	explain.Flags().BoolVar(&completionJSON, "json", false, "Write the canonical readmit-observation-completion/v1 record")
	explain.Flags().StringVar(&declared, "window", "", "Re-decide the record against the observation window that was declared")
	command.AddCommand(validate, explain)
	return command
}

func windowLines(window observewindow.Window) string {
	return fmt.Sprintf(`Observation window: %s
Source: kind %s, identity %s, scope %s
Watermark: %s
Pre-existing state: %s
Completion rule: %d stable samples spanning %s, within %s
Limits: %d records, %d samples
This validates a declared window. It observes no source and produces no verdict.
`, window.Identity(), window.Source.Kind, window.Source.Identity, window.Source.Scope,
		watermarkLine(window.Watermark), declaredPreExisting(window.PreExisting.Declaration),
		window.Completion.StableSamples, window.Completion.QuietPeriod, window.Completion.Deadline,
		window.Completion.MaxRecords, window.Completion.MaxSamples)
}

// watermarkLine states the declared start state. A kind that carries no
// position says so rather than printing an empty one.
func watermarkLine(watermark observewindow.Watermark) string {
	if watermark.Position == "" {
		return watermark.Kind
	}
	return watermark.Kind + " at " + watermark.Position
}

// declaredPreExisting describes what a window declared. A declared window has
// observed nothing, so it never reports a count of what was already there:
// presenting "we did not look" as "we looked and found none" is the one
// conflation these contracts exist to prevent.
func declaredPreExisting(declaration string) string {
	switch declaration {
	case observewindow.RecordedBaseline:
		return declaration + ", an observation of the source is required before the window opens"
	case observewindow.DeclaredEmpty:
		return declaration + ", recorded as declared and not verified"
	case observewindow.UnknownPreExisting:
		return declaration + ", nothing this window observes can be attributed to the run"
	}
	return declaration
}

// observedPreExisting describes what a completion actually retained. A
// baseline that was never taken and one whose own collection failed each say
// so, rather than reporting the zero records they never counted.
func observedPreExisting(basis string, baseline *observewindow.Sample) string {
	if basis != observewindow.RecordedBaseline {
		return declaredPreExisting(basis)
	}
	if baseline == nil {
		return basis + ", no baseline was ever taken"
	}
	if baseline.Status != observewindow.Observed {
		return fmt.Sprintf("%s, the baseline's own collection reported %s", basis, baseline.Status)
	}
	return fmt.Sprintf("%s, %d records already present", basis, baseline.RecordCount)
}

func completionLines(completion observewindow.Completion) string {
	settled := "not settled"
	if completion.Trustworthy() {
		settled = fmt.Sprint(completion.RecordsObserved)
	}
	produced := "not established"
	if count, err := completion.ProducedInWindow(); err == nil {
		produced = fmt.Sprint(count)
	}
	absence := "supported by this completion"
	if err := completion.AbsenceEvidence(); err != nil {
		absence = "not supported: " + err.Error()
	}
	// A completed window decides no run state: it is trustworthy evidence, and
	// what an assertion makes of it belongs to the run, not to the window.
	runState := completion.Status.RunState()
	if runState == "" {
		runState = "none; a completed window leaves the verdict to the assertion"
	}
	samples := fmt.Sprintf("%d", len(completion.Samples))
	if completion.Trustworthy() {
		samples += fmt.Sprintf(", ending in a run of %d identical observations spanning %s", completion.StableSamples, completion.QuietPeriod)
	}
	matched := 0
	for _, correlation := range completion.Correlations {
		if correlation.Kind == observewindow.Matched {
			matched++
		}
	}
	return fmt.Sprintf(`Observation window: %s
Boundary: %s
Source: kind %s, identity %s, scope %s
Watermark: %s
Status: %s
Stop: %s
Samples: %s
Records observed: %s
Correlations: %d recorded, %d matched
Pre-existing state: %s
Produced in the window: %s
Absence assertion: %s
Durable run state: %s
`, completion.WindowIdentity, completion.Boundary,
		completion.Source.Kind, completion.Source.Identity, completion.Source.Scope,
		watermarkLine(completion.Watermark), completion.Status, completion.Stop, samples,
		settled, len(completion.Correlations), matched,
		observedPreExisting(completion.PreExistingBasis, completion.Baseline), produced, absence, runState)
}

func writeObservation(out io.Writer, text string) error {
	if _, err := fmt.Fprint(out, text); err != nil {
		return errors.New("cannot write observation output")
	}
	return nil
}
