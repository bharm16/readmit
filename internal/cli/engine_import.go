package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/spf13/cobra"
	"time"
)

func engineImportCommand(ran *bool) *cobra.Command {
	var planFile, input, output string
	var preview bool
	cmd := &cobra.Command{Use: "engine --plan FILE --file FILE --output NEW_DIRECTORY", Short: "Import an explicitly declared engine export (unqualified compatibility)", Annotations: declare(capabilityAuthor), Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		if planFile == "" || input == "" || preview && output != "" || !preview && output == "" {
			return errors.New("engine import requires --plan, --file, and either --preview or --output")
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
		data, err := readInputFile(input, engineexport.MaxBytes)
		if err != nil {
			return err
		}
		if preview {
			records, err := engineexport.Extract(plan, data)
			if err != nil {
				return err
			}
			document := struct {
				Schema        string                `json:"schema"`
				Plan          engineexport.Plan     `json:"plan"`
				Qualification string                `json:"qualification"`
				Records       []engineexport.Record `json:"records"`
			}{"readmit-engine-export-preview/v1", plan, "unqualified", records}
			encoded, err := json.Marshal(document, json.Deterministic(true))
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		}
		b, err := bundle.WriteEngineExport(cmd.Context(), output, input, data, plan, time.Now().UTC())
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
