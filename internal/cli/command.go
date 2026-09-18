package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

// Execute owns command-line wiring and diagnostic privacy. Domain parsing does
// not depend on Cobra, and Cobra's errors never echo arbitrary arguments.
func Execute(version string, args []string, stdout, stderr io.Writer) error {
	root := &cobra.Command{
		Use: "readmit", Short: "Local HL7 v2 inspection and case evidence", Version: version,
		SilenceUsage: true, SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{
		Use: "help [command]", Short: "Help about a command",
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
		Use: "inspect FILE", Short: "Inspect syntax without changing the source",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("inspect requires exactly one input file")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ran = true
			if cmd.Flags().Changed("roundtrip") && roundtrip == "" {
				return errors.New("round-trip destination cannot be empty")
			}
			return inspectFile(cmd.OutOrStdout(), args[0], hl7.Options{Format: hl7.Format(format), Terminator: hl7.Terminator(terminator)}, showValues, roundtrip)
		},
	}
	inspect.Flags().StringVar(&format, "format", "auto", "Input framing: auto, raw, or mllp")
	inspect.Flags().StringVar(&terminator, "terminator", "auto", "Segment terminator: auto, cr, lf, or crlf")
	inspect.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display message values as escaped byte strings")
	inspect.Flags().StringVar(&roundtrip, "roundtrip", "", "Write byte-identical evidence to a new file (never overwrite)")
	root.AddCommand(inspect)
	root.AddCommand(captureCommand(&ran), timelineCommand(&ran))
	root.AddCommand(listenCommand(&ran))
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		if !ran {
			err = errors.New("invalid command or arguments; use readmit --help")
		}
		fmt.Fprintln(stderr, "readmit:", err)
	}
	return err
}

func inspectFile(out io.Writer, path string, options hl7.Options, showValues bool, roundtrip string) error {
	data, err := readInputFile(path, hl7.MaxInputBytes)
	if err != nil {
		return err
	}
	doc, err := hl7.Parse(data, options)
	if err != nil {
		return err
	}
	labels, err := dictionary.Load()
	if err != nil {
		return err
	}
	if roundtrip != "" {
		if err := writeEvidence(roundtrip, doc.Serialize()); err != nil {
			return err
		}
	}
	if err := render(out, doc, labels, options, showValues); err != nil {
		return errors.New("cannot write inspection output")
	}
	return nil
}

func writeEvidence(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create round-trip file; destination must be new and writable")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return errors.New("cannot write round-trip file")
	}
	return nil
}

func selection(value string) string {
	if value == "" || value == "auto" {
		return "detected"
	}
	return "declared"
}

func messageLabels(doc *hl7.Document, message hl7.Message, labels *dictionary.Dictionary) map[string]map[int]string {
	version := doc.Bytes(message.Segments[0].Field(12).Span)
	if strings.SplitN(string(version), string(message.Delimiters.Component), 2)[0] == labels.HL7Version {
		return labels.Segments
	}
	return nil
}
