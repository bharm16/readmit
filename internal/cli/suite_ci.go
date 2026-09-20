package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func suiteCICommand() *cobra.Command {
	var request suite.CIRequest
	var send bool
	var deadline string
	command := &cobra.Command{Use: "ci FILE", Short: "Execute a saved suite once with private evidence and fixed-label CI summaries", Annotations: declareInterruptible(capabilityExecute), RunE: func(cmd *cobra.Command, args []string) error {
		result := suite.CIError()
		if len(args) == 1 && send {
			request.Path = args[0]
			ctx, cancel, err := deadlineContext(cmd.Context(), deadline)
			if err == nil {
				defer cancel()
				result = suite.RunCI(ctx, request)
			}
		}
		if err := writeJSON(cmd, result); err != nil {
			return &ExitError{Code: 2, Err: errors.New("cannot write CI summary")}
		}
		if result.ExitCode != 0 {
			return &ExitError{Code: result.ExitCode, Err: errors.New("suite CI gate did not pass; inspect retained evidence privately"), Reported: true}
		}
		return nil
	}}
	command.Flags().StringVar(&request.Environment, "environment", "", "Explicit suite environment")
	command.Flags().StringVar(&request.Output, "output", "", "New customer-private evidence directory")
	command.Flags().BoolVar(&send, "send", false, "Authorize one execution; never resume or retry")
	command.Flags().StringVar(&deadline, "deadline", "", "Positive execution deadline")
	command.Flags().StringVar(&request.Requirements, "requirements", "", "Explicit coverage declarations; all requirements and jobs must qualify")
	command.Flags().StringArrayVar(&request.Previous, "previous", nil, "Prior suite for coverage stability assessment; requires --requirements")
	command.Flags().StringVar(&request.Releases, "releases", "", "Exact released expectation references")
	command.Flags().StringVar(&request.Promotion, "promotion", "", "Explicit approved environment promotion")
	command.Flags().StringVar(&request.PromotionIdentity, "promotion-identity", "", "Exact full promotion identity")
	command.Flags().StringVar(&request.Revision, "revision", "", "Current operator-asserted target revision")
	return command
}
