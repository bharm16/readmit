package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func suiteCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{Use: "suite", Short: "Bind reusable regression templates to data rows and an explicit environment"}
	for _, execute := range []bool{false, true} {
		var environment, output, deadline, releases, promotion, promotionIdentity, revision string
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
			if (!execute && (promotion != "" || promotionIdentity != "" || revision != "")) || (promotion == "" && (promotionIdentity != "" || revision != "")) || (promotion != "" && (promotionIdentity == "" || revision == "" || releases == "")) {
				return &ExitError{Code: 2, Err: errors.New("promotion run requires --promotion, --promotion-identity, --revision and --releases together")}
			}
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
				if promotion != "" {
					run = func(ctx context.Context, path, environment, output string) (runqueue.Report, error) {
						return suite.RunPromoted(ctx, path, environment, output, releases, promotion, promotionIdentity, revision)
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
		child.Flags().StringVar(&promotion, "promotion", "", "Explicit approved environment promotion")
		child.Flags().StringVar(&promotionIdentity, "promotion-identity", "", "Exact full promotion approval identity")
		child.Flags().StringVar(&revision, "revision", "", "Current operator-asserted target software revision; never probed")
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
	command.AddCommand(suiteCoverageCommand(ran), suitePromotionCommand(ran, false), suitePromotionCommand(ran, true))
	return command
}

func suiteCoverageCommand(ran *bool) *cobra.Command {
	var requirements, at string
	var repeats []string
	var asJSON bool
	command := &cobra.Command{Use: "coverage DIRECTORY", Short: "Assess declared requirements against retained suite executions without sending", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		now := time.Now().UTC()
		if at != "" {
			var err error
			now, err = time.Parse(time.RFC3339, at)
			if err != nil {
				return &ExitError{Code: 2, Err: errors.New("coverage --at requires an RFC3339 instant")}
			}
		}
		if requirements == "" {
			return &ExitError{Code: 2, Err: errors.New("coverage requires --requirements")}
		}
		report, err := suite.AssessCoverage(cmd.Context(), args[0], requirements, repeats, now)
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		if asJSON {
			err = writeJSON(cmd, report)
		} else {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Declared requirement coverage: %d/%d (%.2f%%) at %s\n", report.Passed, report.Denominator, report.Percent, report.At.Format(time.RFC3339))
			for _, r := range report.Requirements {
				if err == nil {
					_, err = fmt.Fprintf(cmd.OutOrStdout(), "Requirement %s: %s\n", r.ID, r.State)
				}
			}
			for _, j := range report.Jobs {
				if err == nil {
					_, err = fmt.Fprintf(cmd.OutOrStdout(), "Job %s: execution=%s reason=%q expiry=%s; exclusion=%s reason=%q expires=%s expired=%t; stability=%s (%s)\n", j.ID, j.Execution, j.Reason, j.Expiry, j.Exclusion, j.ExclusionReason, j.Expires, j.Expired, j.Stability.State, j.Stability.Reason)
				}
			}
			if err == nil {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), report.Scope)
			}
		}
		if err != nil {
			return err
		}
		if report.Passed != report.Denominator {
			return &ExitError{Code: 2, Err: errors.New("declared suite coverage is incomplete")}
		}
		for _, j := range report.Jobs {
			if !j.Eligible {
				return &ExitError{Code: 2, Err: errors.New("suite has excluded, unverified, failing or unstable jobs")}
			}
		}
		return nil
	}}
	command.Flags().StringVar(&requirements, "requirements", "", "Strict readmit-suite-coverage/v1 document with an explicit denominator")
	command.Flags().StringVar(&at, "at", "", "Assessment instant (RFC3339); defaults to current UTC")
	command.Flags().StringArrayVar(&repeats, "previous", nil, "Previous retained suite directory in the same environment; repeat up to fifteen times")
	command.Flags().BoolVar(&asJSON, "json", false, "Write the customer-local coverage view")
	return command
}

func suitePromotionCommand(ran *bool, approve bool) *cobra.Command {
	var environment, releases, revision, review, approver, rationale, output string
	name := "review-promotion"
	if approve {
		name = "approve-promotion"
	}
	command := &cobra.Command{Use: name + " FILE", Short: "Review or approve exact suite inputs for one configured environment without sending", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		if environment == "" || releases == "" || revision == "" {
			return &ExitError{Code: 2, Err: errors.New("promotion requires --environment, --releases and --revision")}
		}
		if approve {
			p, err := suite.ApprovePromotion(args[0], environment, releases, revision, review, approver, rationale, output)
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), p.Identity())
			return err
		}
		r, err := suite.ReviewPromotion(args[0], environment, releases, revision)
		if err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		return writeJSON(cmd, r)
	}}
	command.Flags().StringVar(&environment, "environment", "", "Suite environment to review")
	command.Flags().StringVar(&releases, "releases", "", "Exact released expectation references")
	command.Flags().StringVar(&revision, "revision", "", "Operator-asserted target software revision, not independently verified")
	if approve {
		command.Flags().StringVar(&review, "review", "", "Identity of the reviewed promotion")
		command.Flags().StringVar(&approver, "approver", "", "Local reviewer label; not authentication")
		command.Flags().StringVar(&rationale, "rationale", "", "Reason for approving this environment and its isolation declarations")
		command.Flags().StringVar(&output, "output", "", "New private approval file")
	}
	return command
}
