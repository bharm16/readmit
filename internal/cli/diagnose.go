package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/spf13/cobra"
)

func diagnoseCommand(ran *bool) *cobra.Command {
	var output, configPath string
	cmd := &cobra.Command{
		Use: "diagnose BUNDLE --output NEW_DIRECTORY", Short: "Write evidence-bound SIU diagnosis as JSON and Markdown",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("diagnose requires exactly one bundle directory")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if output == "" {
				return errors.New("diagnose requires --output with a new directory")
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
			report, err := diagnose.Run(args[0], config)
			if err != nil {
				return err
			}
			if err := diagnosisOutputOutsideCase(args[0], output); err != nil {
				return err
			}
			jsonData, err := diagnose.JSON(report)
			if err != nil {
				return errors.New("cannot encode diagnosis report")
			}
			markdown := diagnose.Markdown(report)
			if err := os.Mkdir(output, 0700); err != nil {
				return errors.New("cannot create report directory; destination must be new and parent writable")
			}
			root, err := os.OpenRoot(output)
			if err != nil {
				return errors.New("cannot open new report directory")
			}
			defer root.Close()
			for _, file := range []struct {
				name string
				data []byte
			}{{"report.json", jsonData}, {"report.md", markdown}} {
				f, err := root.OpenFile(file.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return errors.New("cannot create diagnosis file; incomplete report retained")
				}
				_, writeErr := f.Write(file.data)
				if writeErr == nil {
					writeErr = f.Sync()
				}
				closeErr := f.Close()
				if writeErr != nil || closeErr != nil {
					return errors.New("cannot write diagnosis file; incomplete report retained")
				}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Diagnosis complete: %d findings, %d unsupported items. Ruleset: %s. JSON and Markdown reports written.\n", len(report.Findings), len(report.Unsupported), report.Ruleset)
			return err
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "New directory for report.json and report.md (never overwrite)")
	cmd.Flags().StringVar(&configPath, "config", "", "Explicit readmit-diagnose-config/v1 JSON configuration")
	return cmd
}

// A nested output directory would invalidate the verified immutable input case.
// Resolve symlinked parents and compare filesystem identity rather than names.
func diagnosisOutputOutsideCase(casePath, output string) error {
	caseInfo, err := os.Stat(casePath)
	if err != nil {
		return errors.New("cannot inspect diagnosis case directory")
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return errors.New("cannot resolve diagnosis output directory")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return errors.New("cannot resolve diagnosis output parent directory")
	}
	for {
		info, err := os.Stat(parent)
		if err != nil {
			return errors.New("cannot inspect diagnosis output parent directory")
		}
		if os.SameFile(caseInfo, info) {
			return errors.New("diagnosis output must be outside the immutable input case")
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return nil
}
