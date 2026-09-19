package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/transform"
	"github.com/spf13/cobra"
)

func transformCommand(ran *bool) *cobra.Command {
	var rulesPath, planPath, profilePath, format, output string
	cmd := &cobra.Command{
		Use: "transform CASE", Short: "Preview relationship-preserving replay transformations of a case",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("transform requires exactly one case directory")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if rulesPath == "" {
				return errors.New("transform requires --rules naming a readmit-correlation-rules/v1 document")
			}
			if planPath == "" {
				return errors.New("transform requires --plan naming a readmit-transform-plan/v1 document")
			}
			if format != "terminal" && format != "json" {
				return errors.New("transform format must be terminal or json")
			}
			if cmd.Flags().Changed("output") && output == "" {
				return errors.New("transform output must name a new file")
			}
			declared, err := readInputFile(rulesPath, correlate.MaxRulesBytes)
			if err != nil {
				return err
			}
			rules, err := correlate.ParseRules(declared)
			if err != nil {
				return err
			}
			authored, err := readInputFile(planPath, transform.MaxPlanBytes)
			if err != nil {
				return err
			}
			plan, err := transform.DecodePlan(authored)
			if err != nil {
				return err
			}
			pack, err := selectedPack(profilePath)
			if err != nil {
				return err
			}
			preview, err := transform.Run(args[0], plan, rules, pack)
			if err != nil {
				return err
			}
			data := transform.Terminal(preview)
			if format == "json" {
				if data, err = transform.JSON(preview); err != nil {
					return err
				}
			}
			if len(data) > transform.MaxPreviewBytes {
				return errors.New("transform output exceeds 32 MiB; transform a smaller sequence")
			}
			if output != "" {
				return writeNewFile(output, data,
					"cannot create transformation preview; destination must be new and outside the case",
					"cannot write transformation preview")
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write transformation preview")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rulesPath, "rules", "", "Declared readmit-correlation-rules/v1 document whose relations are preserved (required)")
	cmd.Flags().StringVar(&planPath, "plan", "", "Declared readmit-transform-plan/v1 document (required; there is no default transformation)")
	cmd.Flags().StringVar(&profilePath, "profile", "", "Profile pack the plan pinned; required when it pins one and refused when it does not")
	cmd.Flags().StringVar(&format, "format", "terminal", "Preview format: terminal or json")
	cmd.Flags().StringVar(&output, "output", "", "Write a new file outside the case (default stdout; never overwrite)")
	return cmd
}

// selectedPack reads the profile pack an operator explicitly selected. A plan
// that pins none is previewed against none, and the preview refuses a pack the
// plan did not pin rather than validating against whatever was supplied.
func selectedPack(path string) (*profilepack.Pack, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readInputFile(path, profilepack.MaxPackBytes)
	if err != nil {
		return nil, err
	}
	pack, err := profilepack.Decode(data)
	if err != nil {
		return nil, err
	}
	return &pack, nil
}
