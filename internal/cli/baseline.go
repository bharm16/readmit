package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/spf13/cobra"
)

func baselineCommand(ran *bool) *cobra.Command {
	root := &cobra.Command{Use: "baseline", Short: "Review and explicitly approve immutable regression expectations"}
	for _, approve := range []bool{false, true} {
		var previous, identity, approver, rationale, output string
		var show bool
		name := "review"
		if approve {
			name = "approve"
		}
		cmd := &cobra.Command{Use: name + " SPEC", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			data, err := baseline.ReadBytes(args[0], testrunner.MaxSpecBytes)
			if err != nil {
				return err
			}
			var parent *baseline.Revision
			if previous != "" {
				r, err := baseline.Read(previous)
				if err != nil {
					return err
				}
				parent = &r
			}
			if !approve {
				r, err := baseline.Review(data, parent, show)
				if err != nil {
					return err
				}
				if _, err := cmd.OutOrStdout().Write(r.JSON()); err != nil {
					return errors.New("cannot write baseline review")
				}
				return nil
			}
			if output == "" {
				return errors.New("baseline approval requires a new --output file")
			}
			r, err := baseline.Approve(data, parent, identity, approver, rationale)
			if err != nil {
				return err
			}
			if err := baseline.Save(output, r); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Approved local baseline revision %d; identity is not authenticated.\n", r.Revision)
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
	inspect := &cobra.Command{Use: "show REVISION", Short: "Inspect a retained baseline and its local approval", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		revision, err := baseline.Read(args[0])
		if err != nil {
			return err
		}
		report, err := baseline.Inspect(revision, show)
		if err != nil {
			return err
		}
		data, err := json.Marshal(struct {
			Schema    string              `json:"schema"`
			Approver  string              `json:"approver"`
			Rationale string              `json:"rationale"`
			Baseline  baseline.Comparison `json:"baseline"`
		}{"readmit-baseline-inspection/v1", revision.Approver, revision.Rationale, report}, json.Deterministic(true))
		if err != nil {
			return errors.New("cannot render baseline")
		}
		if _, err := cmd.OutOrStdout().Write(append(data, '\n')); err != nil {
			return errors.New("cannot write baseline inspection")
		}
		return nil
	}}
	inspect.Flags().BoolVar(&show, "show-values", false, "Reveal retained expected values and configuration")
	root.AddCommand(inspect)
	return root
}
