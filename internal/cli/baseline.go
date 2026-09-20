package cli

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

func baselineCommand() *cobra.Command {
	root := &cobra.Command{Use: "baseline", Short: "Review and explicitly approve immutable regression expectations"}
	for _, approve := range []bool{false, true} {
		var previous, identity, approver, rationale, output string
		var show bool
		name := "review"
		capability := capabilityFree
		if approve {
			name = "approve"
			capability = capabilityAuthor
		}
		cmd := &cobra.Command{Use: name + " SPEC", Annotations: declare(capability), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			request := operation.BaselineRequest{Spec: args[0], Previous: previous, Output: output, ShowValues: show, Review: identity, Approver: approver, Rationale: rationale}
			if !approve {
				result, err := operation.ReviewBaseline(request)
				if err != nil {
					return err
				}
				if _, err := cmd.OutOrStdout().Write(result.Comparison.JSON()); err != nil {
					return errors.New("cannot write baseline review")
				}
				return nil
			}
			if output == "" {
				return usage("baseline approval requires a new --output file")
			}
			result, err := operation.ApproveBaseline(request)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Approved local baseline revision %d; identity is not authenticated.\n", result.Approved.Revision)
			if err != nil {
				return errors.New("baseline saved but confirmation could not be written")
			}
			return nil
		}}
		cmd.Flags().StringVar(&previous, "previous", "", "Exact previous readmit-baseline/v1 revision; omit only for the first revision")
		if approve {
			cmd.Flags().StringVar(&identity, "review", "", "Exact identity returned by baseline review (required)")
			cmd.Flags().StringVar(&approver, "approver", "", "Local reviewer declaration, not authenticated identity (required)")
			cmd.Flags().StringVar(&rationale, "rationale", "", "Explicit approval rationale (required)")
			cmd.Flags().StringVar(&output, "output", "", "New private baseline file (required)")
		} else {
			cmd.Flags().BoolVar(&show, "show-values", false, "Reveal exact before/after specification values in escaped JSON")
		}
		root.AddCommand(cmd)
	}

	var show bool
	inspect := &cobra.Command{Use: "show REVISION", Short: "Inspect a retained baseline and its local approval", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := baseline.Read(args[0])
		if err != nil {
			return err
		}
		report, err := baseline.Inspect(revision, show)
		if err != nil {
			return err
		}
		document := struct {
			Schema    string              `json:"schema"`
			Approver  string              `json:"approver"`
			Rationale string              `json:"rationale"`
			Baseline  baseline.Comparison `json:"baseline"`
		}{"readmit-baseline-inspection/v1", revision.Approver, revision.Rationale, report}
		if err := writeJSONTo(cmd.OutOrStdout(), document); err != nil {
			return errors.New("cannot write baseline inspection")
		}
		return nil
	}}
	inspect.Flags().BoolVar(&show, "show-values", false, "Reveal retained expected values and configuration")
	root.AddCommand(inspect)
	return root
}
