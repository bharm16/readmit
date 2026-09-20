package cli

import (
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/redact"
	"github.com/spf13/cobra"
)

func reexecuteCommand() *cobra.Command {
	var request redact.ReexecutionRequest
	var output string
	var send bool
	cmd := &cobra.Command{Use: "reexecute REVIEW", Short: "Reexecute reviewed transformed evidence against an explicitly selected authorized target", Annotations: declareInterruptible(capabilityExecute), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if send != (output != "") {
			return usage("reexecute requires --send and --output together; omit both for local preview")
		}
		request.ReviewPath = args[0]
		ctx := cmd.Context()
		plan, err := redact.PrepareReexecution(ctx, request)
		if err != nil {
			return err
		}
		result := plan.Preview()
		if send {
			result, err = plan.Execute(ctx, output)
			if err != nil {
				return err
			}
		}
		raw, err := json.Marshal(result, json.Deterministic(true))
		if err != nil {
			return err
		}
		if _, err = cmd.OutOrStdout().Write(append(raw, '\n')); err != nil {
			return errors.New("cannot write reexecution assessment")
		}
		if send && result.Criteria != "matched" {
			return &ExitError{Code: 2, Err: errors.New("external equivalence declined; execution did not preserve selected criteria"), Reported: true}
		}
		return nil
	}}
	cmd.Flags().StringVar(&request.LocalState, "local-state", "", "Private source and policy commitments for the review")
	cmd.Flags().StringVar(&request.Approval, "approve", "", "Exact current disclosure review identity")
	cmd.Flags().StringVar(&request.OriginalPacket, "original-packet", "", "Verified retained packet whose current run supplies the original phase evidence")
	cmd.Flags().StringVar(&request.SpecPath, "spec", "", "Reviewed derived assertions with explicitly rebound case, target and observation paths")
	cmd.Flags().StringVar(&request.Phase, "phase", "", "failure or pass; execute each separately after the required operator reset")
	cmd.Flags().BoolVar(&send, "send", false, "Authorize this single nonproduction execution; no automatic retry or reset")
	cmd.Flags().StringVar(&output, "output", "", "New customer-local durable job directory; never overwrite")
	return cmd
}
