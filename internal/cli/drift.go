package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/drift"
	"github.com/spf13/cobra"
)

func driftCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use: "drift LEFT RIGHT", Short: "Separate input, target, environment, and rule drift between two artifacts", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := drift.Compare(args[0], args[1])
			if err != nil {
				return err
			}
			data, err := renderSelection(format, drift.Terminal(report), drift.Markdown(report), report)
			if err != nil {
				return err
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write drift output")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "terminal", "Report format: terminal, markdown, or json")
	return cmd
}
