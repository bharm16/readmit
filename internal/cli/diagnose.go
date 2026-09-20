package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/spf13/cobra"
)

func diagnoseCommand() *cobra.Command {
	var output, configPath string
	cmd := &cobra.Command{
		Use: "diagnose BUNDLE --output new_directory", Short: "Write evidence-bound appointment or lifecycle diagnosis as JSON and Markdown", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("diagnose requires --output with a new directory")
			}
			config := diagnose.DefaultConfig()
			if cmd.Flags().Changed("config") {
				if configPath == "" {
					return usage("diagnosis configuration file cannot be empty")
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
			report, err := diagnose.Run(args[0], config)
			if err != nil {
				return err
			}
			resolvedOutput, err := diagnosisOutputOutsideCase(args[0], output)
			if err != nil {
				return err
			}
			jsonData, err := diagnose.JSON(report)
			if err != nil {
				return errors.New("cannot encode diagnosis report")
			}
			markdown := diagnose.Markdown(report)
			if err := writeNewReportDirectory(resolvedOutput, "diagnosis", outputFile{"report.json", jsonData}, outputFile{"report.md", markdown}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Diagnosis complete: %d findings, %d unsupported items. Ruleset: %s. JSON and Markdown reports written.\n", len(report.Findings), len(report.Unsupported), report.Ruleset)
			return err
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "New directory for report.json and report.md (never overwrite)")
	cmd.Flags().StringVar(&configPath, "config", "", "Explicit readmit-diagnose-config/v1 JSON configuration selecting the profile, ruleset, rules and namespaces")
	cmd.AddCommand(diagnoseReviewCommand(), diagnosisGroupsCommand())
	return cmd
}

// A nested output directory would invalidate the verified immutable input case.
// Resolve symlinked parents and compare filesystem identity rather than names.
func diagnosisOutputOutsideCase(casePath, output string) (string, error) {
	caseInfo, err := os.Stat(casePath)
	if err != nil {
		return "", errors.New("cannot inspect diagnosis case directory")
	}
	return artifactpath.Destination(output, caseInfo)
}
