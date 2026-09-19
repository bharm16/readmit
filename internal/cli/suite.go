package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/runqueue"

	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func suiteCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{Use: "suite", Short: "Bind reusable regression templates to data rows and an explicit environment"}
	for _, execute := range []bool{false, true} {
		var environment, output, deadline, releases string
		var send, asJSON bool
		name := "prepare"
		if execute {
			name = "run"
		}
		child := &cobra.Command{Use: name + " FILE", Short: "Prepare a new private suite directory; run requires explicit send authorization", Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("suite requires one document")
			}
			return nil
		}, RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if environment == "" || output == "" || execute && !send {
				return &ExitError{Code: 2, Err: errors.New("suite requires --environment and --output; run additionally requires --send")}
			}
			if execute {
				ctx, cancel, err := runContext(cmd.Context(), deadline)
				if err != nil {
					return err
				}
				defer cancel()
				run := suite.Run
				if releases != "" {
					run = func(ctx context.Context, path, environment, output string) (runqueue.Report, error) {
						return suite.RunApproved(ctx, path, environment, output, releases)
					}
				}
				report, err := run(ctx, args[0], environment, output)
				if err != nil {
					return &ExitError{Code: 2, Err: err}
				}
				return printQueue(cmd, report, asJSON)
			}
			prepare := suite.Prepare
			if releases != "" {
				prepare = func(path, environment, output string) (suite.Prepared, error) {
					return suite.PrepareApproved(path, environment, output, releases)
				}
			}
			prepared, err := prepare(args[0], environment, output)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			if asJSON {
				return writeJSON(cmd, prepared.Queue)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Prepared %d jobs. Nothing was sent. Configuration and expectations remain customer-local.\n", len(prepared.Queue.Jobs))
			return err
		}}
		child.Flags().StringVar(&releases, "releases", "", "Verify every template and expectation against exact released test pins")
		child.Flags().StringVar(&environment, "environment", "", "Explicit environment ID declared by the suite")
		child.Flags().StringVar(&output, "output", "", "New private directory for compiled configuration and durable runs")
		child.Flags().BoolVar(&asJSON, "json", false, "Write the existing versioned queue plan or execution report")
		if execute {
			child.Flags().BoolVar(&send, "send", false, "Authorize this suite execution; existing output is never resumed")
			child.Flags().StringVar(&deadline, "deadline", "", "Stop the suite after this positive duration")
		}
		command.AddCommand(child)
	}
	return command
}
