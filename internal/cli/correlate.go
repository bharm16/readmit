package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/spf13/cobra"
)

func correlateCommand(ran *bool) *cobra.Command {
	var rulesPath, format, output string
	cmd := &cobra.Command{
		Use: "correlate CASE", Short: "Link case occurrences under declared source, session and authority rules",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("correlate requires exactly one case directory")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if rulesPath == "" {
				return errors.New("correlate requires --rules naming a readmit-correlation-rules/v1 document")
			}
			if format != "terminal" && format != "json" {
				return errors.New("correlate format must be terminal or json")
			}
			if cmd.Flags().Changed("output") && output == "" {
				return errors.New("correlate output must name a new file")
			}
			declared, err := readInputFile(rulesPath, correlate.MaxRulesBytes)
			if err != nil {
				return err
			}
			rules, err := correlate.ParseRules(declared)
			if err != nil {
				return err
			}
			report, err := correlate.Run(args[0], rules)
			if err != nil {
				return err
			}
			data := correlate.Terminal(report)
			if format == "json" {
				if data, err = correlate.JSON(report); err != nil {
					return err
				}
			}
			if len(data) > 32<<20 {
				return errors.New("correlate output exceeds 32 MiB; declare fewer rules or a narrower scope")
			}
			if output != "" {
				return writeNewFile(output, data,
					"cannot create correlation output; destination must be new and outside the case",
					"cannot write correlation output")
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write correlation output")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rulesPath, "rules", "", "Declared readmit-correlation-rules/v1 document (required; there is no default rule set)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Report format: terminal or json")
	cmd.Flags().StringVar(&output, "output", "", "Write a new file outside the case (default stdout; never overwrite)")
	return cmd
}
