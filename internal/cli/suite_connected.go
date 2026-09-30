package cli

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/spf13/cobra"
)

func suiteConnectedInspectCommand() *cobra.Command {
	return &cobra.Command{Use: "inspect DIRECTORY", Short: "Verify retained connected suite or CI evidence offline without executing", Annotations: declare(capabilityFree), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(filepath.Join(args[0], "manifest.json"))
		if err != nil {
			return refusal(err)
		}
		var header struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(raw, &header) != nil {
			return refusal(errors.New("invalid connected suite manifest"))
		}
		if header.Schema == customerrunner.ConnectedCISchema {
			r, e := customerrunner.InspectConnectedCI(cmd.Context(), args[0])
			if e != nil {
				return refusal(e)
			}
			metadata, e := customerrunner.InspectConnectedCIRefusal(cmd.Context(), args[0])
			if e != nil {
				return refusal(e)
			}
			return writeJSON(cmd, struct {
				Summary suite.CIReport                           `json:"summary"`
				Refusal *customerrunner.ConnectedRefusalMetadata `json:"refusal,omitzero"`
			}{r, metadata})
		}
		r, e := suite.OpenConnectedExecution(cmd.Context(), args[0])
		if e != nil {
			return refusal(e)
		}
		return writeJSON(cmd, struct {
			Schema   string `json:"schema"`
			Identity string `json:"identity"`
			Exit     int    `json:"exit_code"`
			Jobs     int    `json:"jobs"`
			Executed int    `json:"executed"`
			Skipped  int    `json:"skipped"`
		}{suite.ConnectedExecutionSchema, r.Identity, r.Report.ExitCode(), len(r.Report.Jobs), r.Report.Executed, r.Report.Skipped})
	}}
}
