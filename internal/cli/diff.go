package cli

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

func diffCommand(ran *bool) *cobra.Command {
	var options diff.Options
	var output, format, inputFormat, terminator, boundary string
	cmd := &cobra.Command{
		Use: "diff LEFT RIGHT", Short: "Compare named HL7 fields in local messages, cases, runs, or results",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("diff requires exactly two inputs")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if format != "terminal" && format != "markdown" && format != "json" {
				return errors.New("diff format must be terminal, markdown, or json")
			}
			if cmd.Flags().Changed("output") && output == "" {
				return errors.New("diff output must name a new file")
			}
			options.Boundary = diff.Boundary(boundary)
			input := func(path string) diff.Input {
				return diff.Input{Path: path, Format: hl7.Format(inputFormat), Terminator: hl7.Terminator(terminator)}
			}
			report, err := diff.Compare(input(args[0]), input(args[1]), options)
			if err != nil {
				return err
			}
			var data []byte
			switch format {
			case "terminal":
				data = diff.Terminal(report)
			case "markdown":
				data = diff.Markdown(report)
			case "json":
				data, err = diff.JSON(report)
			}
			if err != nil {
				return err
			}
			if len(data) > 32<<20 {
				return errors.New("diff output exceeds 32 MiB; select a narrower field scope")
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
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create diff output; destination must be new and parent writable")
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write diff output; incomplete file retained")
	}
	return nil
}
