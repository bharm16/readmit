package cli

import (
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/spf13/cobra"
)

func correlateCommand() *cobra.Command {
	var rulesPath, format, output string
	cmd := &cobra.Command{
		Use: "correlate CASE", Short: "Link case occurrences under declared source, session and authority rules", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if rulesPath == "" {
				return usage("correlate requires --rules naming a readmit-correlation-rules/v1 document")
			}
			if err := checkReportFlags(cmd, format, output, "correlate"); err != nil {
				return err
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
			return writeTerminalOrJSON(cmd, format, output, "correlate", "correlation output",
				correlate.Terminal(report), func() ([]byte, error) { return correlate.JSON(report) })
		},
	}
	cmd.Flags().StringVar(&rulesPath, "rules", "", "Declared readmit-correlation-rules/v1 document (required; there is no default rule set)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Report format: terminal or json")
	cmd.Flags().StringVar(&output, "output", "", "Write a new file outside the case (default stdout; never overwrite)")
	return cmd
}
