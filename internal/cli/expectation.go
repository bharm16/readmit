package cli

import (
	"fmt"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func expectationCommand() *cobra.Command {
	root := &cobra.Command{Use: "expectation", Short: "Review and release immutable test versions with profile pins"}
	for _, approve := range []bool{false, true} {
		var id, previous, review, approver, rationale, output string
		var profiles []string
		var show bool
		var connected bool
		name := "review"
		capability := capabilityFree
		if approve {
			name = "release"
			capability = capabilityAuthor
		}
		command := &cobra.Command{Use: name + " SPEC", Annotations: declare(capability), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if connected {
				if len(profiles) > 0 || show || id != "" {
					return usage("connected releases use the sealed plan's exact definition and profile pins")
				}
				if !approve {
					r, e := expectation.ReviewConnected(args[0])
					if e != nil {
						return refusal(e)
					}
					return writeJSON(cmd, r)
				}
				if output == "" {
					return usage("connected release requires a new --output")
				}
				p, e := expectation.ApproveConnected(args[0], previous, review, approver, rationale, output)
				if e != nil {
					return refusal(e)
				}
				_, e = fmt.Fprintln(cmd.OutOrStdout(), p.Identity())
				return e
			}
			request := operation.ExpectationRequest{ID: id, Spec: args[0], Previous: previous, Output: output, Profiles: profiles, ShowValues: show, Review: review, Approver: approver, Rationale: rationale}
			if !approve {
				result, e := operation.ReviewExpectation(request)
				if e != nil {
					return e
				}
				return writeJSON(cmd, result.Comparison)
			}
			if output == "" {
				return usage("release requires a new --output file")
			}
			result, err := operation.ApproveExpectation(request)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Released test revision %d with identity %s. Local reviewer declaration is not authenticated.\n", result.Approved.Baseline.Revision, result.Approved.Identity())
			return err
		}}
		command.Flags().StringVar(&id, "id", "", "Stable test identity (required)")
		command.Flags().BoolVar(&connected, "connected-plan", false, "Review or release a sealed connected lifecycle plan without changing legacy test readers")
		command.Flags().StringVar(&previous, "previous", "", "Exact previous released test; omit for the first revision")
		command.Flags().StringArrayVar(&profiles, "profile", nil, "Local profile document to seal and pin (repeatable; omission explicitly pins none)")
		if approve {
			command.Flags().StringVar(&review, "review", "", "Exact review identity (required)")
			command.Flags().StringVar(&approver, "approver", "", "Local reviewer declaration (required)")
			command.Flags().StringVar(&rationale, "rationale", "", "Explicit rationale (required)")
			command.Flags().StringVar(&output, "output", "", "New private release file (required)")
		} else {
			command.Flags().BoolVar(&show, "show-values", false, "Reveal exact private before/after values")
		}
		root.AddCommand(command)
	}
	var show bool
	inspect := &cobra.Command{Use: "show RELEASE", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, e := expectation.Read(args[0])
		if e != nil {
			return e
		}
		b, e := expectation.Inspect(r, show)
		if e != nil {
			return e
		}
		return writeJSON(cmd, struct {
			Schema       string              `json:"schema"`
			ID           string              `json:"id"`
			Identity     string              `json:"identity"`
			Parent       string              `json:"parent"`
			Approver     string              `json:"approver"`
			Rationale    string              `json:"rationale"`
			ProfileCount int                 `json:"profile_count"`
			Baseline     baseline.Comparison `json:"baseline"`
		}{"readmit-expectation-inspection/v1", r.ID, r.Identity(), r.Parent, r.Baseline.Approver, r.Baseline.Rationale, len(r.Profiles), b})
	}}
	inspect.Flags().BoolVar(&show, "show-values", false, "Reveal retained private specification values")
	root.AddCommand(inspect)
	var refs string
	var impactValues bool
	impact := &cobra.Command{Use: "impact PREVIOUS RELEASE SUITE", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if refs == "" {
			return usage("impact requires --releases reference file")
		}
		from, e := expectation.Read(args[0])
		if e != nil {
			return e
		}
		to, e := expectation.Read(args[1])
		if e != nil {
			return e
		}
		report, e := suite.AssessReleases(args[2], refs, from, to, impactValues)
		if e != nil {
			return e
		}
		return writeJSON(cmd, report)
	}}
	impact.Flags().StringVar(&refs, "releases", "", "Explicit suite release references")
	impact.Flags().BoolVar(&impactValues, "show-values", false, "Reveal private change values")
	root.AddCommand(impact)
	return root
}
