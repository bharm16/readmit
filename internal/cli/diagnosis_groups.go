package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/spf13/cobra"
)

func diagnosisGroupsCommand(ran *bool) *cobra.Command {
	var output, configPath string
	cmd := &cobra.Command{
		Use:   "groups CASE [CASE...] --output NEW_DIRECTORY",
		Short: "Compare recurring diagnosis signatures without hiding individual findings",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > diagnose.MaxGroupCases {
				return errors.New("diagnosis grouping requires 1 to 16 cases")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if output == "" {
				return errors.New("diagnosis grouping requires --output with a new directory")
			}
			config := diagnose.DefaultConfig()
			if cmd.Flags().Changed("config") {
				if configPath == "" {
					return errors.New("diagnosis configuration file cannot be empty")
				}
				data, err := readInputFile(configPath, 1<<20)
				if err != nil {
					return err
				}
				config, err = diagnose.ParseConfig(data)
				if err != nil {
					return err
				}
			}
			resolvedOutput := output
			for _, path := range args {
				var err error
				resolvedOutput, err = diagnosisOutputOutsideCase(path, resolvedOutput)
				if err != nil {
					return err
				}
			}
			report, err := diagnose.GroupCases(ctx, args, config)
			if err != nil {
				return err
			}
			data, err := diagnose.GroupsJSON(report)
			if err != nil {
				return err
			}
			markdown := diagnose.GroupsMarkdown(report)
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := writeNewReportDirectory(resolvedOutput, "diagnosis groups", outputFile{"report.json", data}, outputFile{"report.md", markdown}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Diagnosis grouping complete: %d cases, %d signatures. Every finding and unsupported item retained; counts apply only to these captures.\n", len(report.Cases), len(report.Groups))
			return err
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "New directory for report.json and report.md (never overwrite)")
	cmd.Flags().StringVar(&configPath, "config", "", "Explicit readmit-diagnose-config/v1 applied independently to every case")
	return cmd
}
