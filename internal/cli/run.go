package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/spf13/cobra"
)

func runCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{Use: "run", Short: "Execute or recover a durable local test run"}
	var output string
	var send, asJSON bool
	start := &cobra.Command{Use: "start SPEC", Short: "Execute once into a new durable run directory", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("run start requires one spec")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if !send || output == "" {
			return &ExitError{Code: 2, Err: errors.New("run start requires --send and --output; existing jobs are never resumed")}
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		result, err := durablerun.Start(ctx, args[0], output)
		if err != nil && result.Schema == "" {
			return &ExitError{Code: 2, Err: errors.New("durable run could not finish; inspect retained output with run status")}
		}
		return printRun(cmd, result, asJSON)
	}}
	start.Flags().StringVar(&output, "output", "", "New customer-local durable run directory")
	start.Flags().BoolVar(&send, "send", false, "Explicitly authorize one execution against the test target")
	start.Flags().BoolVar(&asJSON, "json", false, "Write one versioned machine-readable summary")
	var statusJSON bool
	status := &cobra.Command{Use: "status JOB", Short: "Recover retained evidence read-only; never resend", Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("run status requires one job directory")
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		result, err := durablerun.Open(args[0])
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		return printRun(cmd, result, statusJSON)
	}}
	status.Flags().BoolVar(&statusJSON, "json", false, "Write one versioned machine-readable summary")
	command.AddCommand(start, status)
	return command
}
func printRun(cmd *cobra.Command, result durablerun.Summary, asJSON bool) error {
	var err error
	if asJSON {
		var b []byte
		b, err = json.Marshal(result, json.Deterministic(true))
		if err == nil {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
		}
	} else {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Run state: %s\nStop reason: %s\nDelivery uncertain: %t\nRecorded messages: %d/%d\nContains source values: true (customer-local-only)\n", result.State, result.StopReason, result.DeliveryUncertain, result.Recorded, result.Planned)
		if err == nil && result.Recovered {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Completion was not recorded; the writer may still be active. Recovery never sends or resumes.")
		}
	}
	if err != nil {
		return &ExitError{Code: 2, Err: errors.New("cannot write durable run summary")}
	}
	if result.ExitCode() != 0 {
		return &ExitError{Code: result.ExitCode(), Err: errors.New("run did not pass; inspect retained evidence"), Reported: true}
	}
	return nil
}
