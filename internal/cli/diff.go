package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

func diffCommand() *cobra.Command {
	var options diff.Options
	var output, format, inputFormat, terminator, boundary string
	cmd := &cobra.Command{
		Use: "diff LEFT RIGHT", Short: "Compare named HL7 fields in local messages, cases, runs, or results", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("output") && output == "" {
				return usage("diff output must name a new file")
			}
			options.Boundary = diff.Boundary(boundary)
			input := func(path string) diff.Input {
				return diff.Input{Path: path, Format: hl7.Format(inputFormat), Terminator: hl7.Terminator(terminator)}
			}
			report, err := diff.Compare(input(args[0]), input(args[1]), options)
			if err != nil {
				return err
			}
			data, err := renderSelection(format, diff.Terminal(report), diff.Markdown(report), report)
			if err != nil {
				return err
			}
			if err := checkRendered(data); err != nil {
				return err
			}
			if output != "" {
				resolved, err := artifactpath.Destination(output)
				if err != nil {
					return err
				}
				return writeDiff(resolved, data)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write diff output")
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&options.Fields, "field", nil, "Shared field selector to compare (repeatable; default all field repetitions)")
	cmd.Flags().StringArrayVar(&options.Keys, "key", nil, "Declared alignment-key selector (repeatable composite key; duplicates remain ambiguous)")
	cmd.Flags().StringArrayVar(&options.Ignore, "ignore", nil, "Ignore exactly this shared selector (repeatable; always listed in report)")
	cmd.Flags().StringVar(&boundary, "boundary", "messages", "Payload boundary: messages (actual sent bytes for runs) or acks (received bytes)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Report format: terminal, markdown, or json")
	cmd.Flags().StringVar(&inputFormat, "input-format", "auto", "Standalone-file framing: auto, raw, or mllp")
	cmd.Flags().StringVar(&terminator, "terminator", "auto", "Standalone-file terminator: auto, cr, lf, or crlf")
	cmd.Flags().StringVar(&output, "output", "", "Write a new file outside input artifacts (default stdout; never overwrite)")
	cmd.Flags().BoolVar(&options.ShowValues, "show-values", false, "Explicitly display compared values as escaped strings")
	return cmd
}

func writeDiff(path string, data []byte) error {
	return diffOutput.Create(path, data)
}

// diffOutput is how a diff report is written to a new file, through the
// shared document store. An interrupted write is retained incomplete.
var diffOutput = artifactdir.Document{
	RetainFailed: true,
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("cannot create diff output; destination must be new and parent writable"),
		Write:  errors.New("cannot write diff output; incomplete file retained"),
	},
}
