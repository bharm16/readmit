package cli

import (
	"fmt"

	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

func engineImportCommand() *cobra.Command {
	var planFile, input, output string
	var preview bool
	cmd := &cobra.Command{Use: "engine --plan FILE --file FILE --output NEW_DIRECTORY", Short: "Import an explicitly declared engine export (unqualified compatibility)", Annotations: declare(capabilityAuthor), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if planFile == "" || input == "" || preview && output != "" || !preview && output == "" {
			return usage("engine import requires --plan, --file, and either --preview or --output")
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		rawPlan, err := readInputFile(planFile, 4096)
		if err != nil {
			return err
		}
		plan, err := engineexport.Decode(rawPlan)
		if err != nil {
			return err
		}
		if preview {
			document, err := operation.ImportEnginePreview(cmd.Context(), plan, input)
			if err != nil {
				return err
			}
			return writeJSON(cmd, document)
		}
		b, err := operation.ImportEngineCommit(cmd.Context(), plan, input, output)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Engine export imported; compatibility remains unqualified. Content stages and offsets are reproducible from the retained container and adapter plan.")
		return renderBundle(cmd.OutOrStdout(), b, false, false)
	}}
	cmd.Flags().StringVar(&planFile, "plan", "", "Explicit readmit-engine-export/v1 adapter declaration")
	cmd.Flags().StringVar(&input, "file", "", "One local export, retained byte for byte")
	cmd.Flags().StringVar(&output, "output", "", "New case bundle directory")
	cmd.Flags().BoolVar(&preview, "preview", false, "Report bounded content records without writing evidence")
	return cmd
}
