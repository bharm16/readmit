package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/spf13/cobra"
)

// Execute owns command-line wiring and diagnostic privacy. Domain parsing does
// not depend on Cobra, and Cobra's errors never echo arbitrary arguments.
func Execute(version string, args []string, stdout, stderr io.Writer) error {
	root, ran := rootCommand(version)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	selected, err := root.ExecuteC()
	if err != nil {
		if !*ran {
			// A command whose operation never began was invoked wrongly: bad
			// arguments, bad flags, an unknown command. That refusal carries the
			// same usage status for every command, so a script cannot tell one
			// entry point apart from another by how it misuses it.
			err = usage("invalid command or arguments; use readmit --help")
		}
		var status *ExitError
		if !errors.As(err, &status) || !status.Reported {
			fmt.Fprintln(stderr, "readmit:", err)
			if selected != nil && selected.Name() == "test" {
				fmt.Fprintln(stdout, testRerun)
			}
		}
	}
	return err
}

func rootCommand(version string) (*cobra.Command, *bool) {
	root := &cobra.Command{
		Use: "readmit", Short: "Local HL7 v2 inspection and case evidence", Version: version,
		SilenceUsage: true, SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about a command",
		// The help topic lookup reports its own refusal the way every other
		// pre-execution refusal is reported, so the construction pass does not
		// mark this command's work as started.
		Annotations: mergeAnnotations(declare(capabilityFree), map[string]string{helpTopicAnnotation: "true"}),
		RunE: func(_ *cobra.Command, args []string) error {
			target, remaining, err := root.Find(args)
			if err != nil || len(remaining) > 0 {
				return errors.New("unknown help topic")
			}
			return target.Help()
		},
	})
	var format, terminator, roundtrip string
	var showValues bool
	var ran bool
	inspect := &cobra.Command{
		Use: "inspect FILE", Short: "Inspect syntax without changing the source", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("roundtrip") && roundtrip == "" {
				return usage("round-trip destination cannot be empty")
			}
			options, err := operation.InspectOptions(format, terminator)
			if err != nil {
				return usage("%s", err)
			}
			return inspectFile(cmd.OutOrStdout(), args[0], options, showValues, roundtrip)
		},
	}
	inspect.Flags().StringVar(&format, "format", "auto", "Input framing: auto, raw, or mllp")
	inspect.Flags().StringVar(&terminator, "terminator", "auto", "Segment terminator: auto, cr, lf, or crlf")
	inspect.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display message values as escaped byte strings")
	inspect.Flags().StringVar(&roundtrip, "roundtrip", "", "Write byte-identical evidence to a new file (never overwrite)")
	root.AddCommand(inspect)
	root.AddCommand(captureCommand(), timelineCommand())
	root.AddCommand(importCommand())
	root.AddCommand(indexCommand())
	root.AddCommand(corpusCommand())
	root.AddCommand(listenCommand())
	root.AddCommand(collectCommand())
	root.AddCommand(sourceCommand())
	root.AddCommand(projectCommand())
	root.AddCommand(backupCommand())
	root.AddCommand(upgradeCommand())
	root.AddCommand(licenseCommand())
	root.AddCommand(secretCommand())
	root.AddCommand(protectCommand())
	root.AddCommand(targetCommand())

	root.AddCommand(diagnoseCommand())
	root.AddCommand(correlateCommand())
	root.AddCommand(transformCommand())

	root.AddCommand(synthCommand(), sampleCommand())
	root.AddCommand(scenarioCommand())
	root.AddCommand(profileCommand())
	root.AddCommand(replayCommand())
	root.AddCommand(testCommand())
	root.AddCommand(runCommand())
	root.AddCommand(suiteCommand())
	root.AddCommand(runnerCommand())
	root.AddCommand(observeCommand())
	root.AddCommand(explainCommand())
	root.AddCommand(redactCommand())
	root.AddCommand(diffCommand())
	root.AddCommand(driftCommand())
	root.AddCommand(normalizeCommand())
	root.AddCommand(baselineCommand())
	root.AddCommand(expectationCommand())
	root.AddCommand(reportCommand())
	root.AddCommand(shareCommand())
	var operationPolicy string
	root.PersistentFlags().StringVar(&operationPolicy, "operation-policy", "", "Local operation admission policy for new authoring and execution; without it, this computer's installed license")
	wireOperations(root, &operationPolicy, &ran)
	return root, &ran
}

// inspectFile is the shared inspection rendered for a terminal. Reading,
// parsing and the round trip are the operation's, so the desktop facade
// reports the same file the same way and refuses it for the same reasons.
func inspectFile(out io.Writer, path string, options hl7.Options, showValues bool, roundtrip string) error {
	inspected, err := operation.InspectFile(path, options, roundtrip)
	if err != nil {
		return err
	}
	if err := render(out, inspected, showValues); err != nil {
		return errors.New("cannot write inspection output")
	}
	return nil
}

// mergeAnnotations joins the annotation maps a command is built with.
func mergeAnnotations(groups ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, group := range groups {
		for key, value := range group {
			merged[key] = value
		}
	}
	return merged
}
