package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/spf13/cobra"
)

// Paths and literal values belong in customer-local artifacts. A placeholder
// instruction deliberately avoids echoing private command-line paths.
const testRerun = "Rerun: reset the declared initial state, then readmit test SPEC --send --output NEW_RESULT"

func testCommand(ran *bool) *cobra.Command {
	var output string
	var send bool
	command := &cobra.Command{
		Use: "test SPEC", Short: "Evaluate a declarative regression test against an explicit test target", Annotations: declare(capabilityExecuteIfSend),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("test requires exactly one spec file")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if send && output == "" || !send && output != "" {
				return &ExitError{Code: 2, Err: errors.New("test requires --send and --output together; omit both for local validation")}
			}
			if !send {
				plan, err := testrunner.Prepare(args[0])
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
					fmt.Fprintln(w, "Local validation: no connection opened, no verdict or result artifact produced")
					writeEnvironmentBanner(w, plan.Environment())
					fmt.Fprintf(w, "Observation boundary: %s\nMessages: %d\n%s\n", plan.Boundary(), plan.Count(), testRerun)
				}); err != nil {
					return &ExitError{Code: 2, Err: errors.New("cannot write test preview")}
				}
				return nil
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			artifact, err := testrunner.Run(ctx, args[0], output)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			result := artifact.Result
			label := "Test execution error"
			if result.Status == testrunner.AssertionFailure {
				label = "Test assertions failed"
			}
			if result.Status == testrunner.Pass {
				label = "Appointment ledger assertions passed"
				if result.ObservationBoundary == testrunner.ACKBoundary {
					label = "ACK contract passed"
				}
			}
			if err := writeLines(cmd.OutOrStdout(), func(w io.Writer) {
				fmt.Fprintln(w, label)
				// A spec whose configuration never validated names no
				// environment, and readmit does not invent one to print.
				if artifact.Environment.Classification != "" {
					writeEnvironmentBanner(w, artifact.Environment)
				}
				fmt.Fprintf(w, "Result: %s\nOutcome: %s\nObservation boundary: %s\nAssertions: %d\nContains source values: true (customer-local-only)\n%s\n", artifact.Identity, result.Status, result.ObservationBoundary, len(result.Assertions), testRerun)
			}); err != nil {
				return &ExitError{Code: 2, Err: errors.New("cannot write test summary")}
			}
			if result.Status != testrunner.Pass {
				return &ExitError{Code: result.Status.ExitCode(), Err: errors.New("test completed with a nonpassing result; inspect customer-local evidence"), Reported: true}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&send, "send", false, "Explicitly connect and execute; default performs local-only validation")
	command.Flags().StringVar(&output, "output", "", "New customer-local test result directory, including replay and observations")
	return command
}
