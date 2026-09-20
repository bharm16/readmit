package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/drift"
	"github.com/spf13/cobra"
)

func driftCommand(ran *bool) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use: "drift LEFT RIGHT", Short: "Separate input, target, environment, and rule drift between two artifacts", Annotations: declare(capabilityFree),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("drift requires exactly two artifact directories")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if format != "terminal" && format != "markdown" && format != "json" {
				return errors.New("drift format must be terminal, markdown, or json")
			}
			report, err := drift.Compare(args[0], args[1])
			if err != nil {
				return err
			}
			var data []byte
			switch format {
			case "terminal":
				data = drift.Terminal(report)
			case "markdown":
				data = drift.Markdown(report)
			case "json":
				data, err = drift.JSON(report)
			}
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
