package cli

import (
	"errors"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/spf13/cobra"
)

func profileEvaluateCommand(name string) *cobra.Command {
	var complete bool
	cmd := &cobra.Command{Use: name + " PROFILE PACK CASE", Short: "Evaluate pinned v2 profile constraints against retained messages", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(3), RunE: func(c *cobra.Command, a []string) error {
		profile, err := readInputFile(a[0], profileeval.MaxBytes)
		if err != nil {
			return err
		}
		pack, err := readInputFile(a[1], profileeval.MaxBytes)
		if err != nil {
			return err
		}
		report, err := profileeval.EvaluateBundle(c.Context(), profile, pack, a[2], profileeval.Options{CompleteCapture: complete})
		if err != nil {
			return refusal(err)
		}
		return writeProfileEvaluation(c, report)
	}}
	cmd.Flags().BoolVar(&complete, "complete-capture", false, "Explicitly declare that all prerequisite workflow events were captured")
	return cmd
}
func connectedProfileCommand() *cobra.Command {
	var complete bool
	cmd := &cobra.Command{Use: "profile-checks PLAN PROFILE_ID PACK_ID", Short: "Evaluate exact retained profile pins against prepared v2 inputs", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(3), RunE: func(c *cobra.Command, a []string) error {
		p, err := connectedtest.OpenPlan(a[0])
		if err != nil {
			return refusal(err)
		}
		r, err := connectedtest.EvaluateProfiles(c.Context(), p, a[1], a[2], profileeval.Options{CompleteCapture: complete})
		if err != nil {
			return refusal(err)
		}
		return writeProfileEvaluation(c, r)
	}}
	cmd.Flags().BoolVar(&complete, "complete-capture", false, "Explicitly declare complete prerequisite capture")
	return cmd
}
func writeProfileEvaluation(c *cobra.Command, r profileeval.Report) error {
	if err := writeConnected(c, r); err != nil {
		return err
	}
	if r.Verdict == "fail" {
		return verdict(1, errors.New("profile constraints failed"))
	}
	if r.Verdict != "pass" {
		return verdict(2, errors.New("profile evaluation has unsupported or undecided requirements"))
	}
	return nil
}
