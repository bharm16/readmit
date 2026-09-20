package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

func normalizeCommand(ran *bool) *cobra.Command {
	var options diff.Options
	var policy, format, inputFormat, terminator, boundary string
	cmd := &cobra.Command{
		Use: "normalize LEFT RIGHT --policy FILE", Short: "Compare fields under a declared normalization policy and list every difference it suppresses", Annotations: declare(capabilityFree),
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("normalize requires exactly two inputs")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if format != "terminal" && format != "markdown" && format != "json" {
				return errors.New("normalize format must be terminal, markdown, or json")
			}
			if policy == "" {
				return errors.New("normalize requires a --policy document")
			}
			rules, err := diff.ReadPolicy(policy)
			if err != nil {
				return err
			}
			options.Boundary = diff.Boundary(boundary)
			input := func(path string) diff.Input {
				return diff.Input{Path: path, Format: hl7.Format(inputFormat), Terminator: hl7.Terminator(terminator)}
			}
			report, err := diff.Normalize(input(args[0]), input(args[1]), options, rules)
			if err != nil {
				return err
			}
			var data []byte
			switch format {
			case "terminal":
				data = diff.NormalizationTerminal(report)
			case "markdown":
				data = diff.NormalizationMarkdown(report)
			case "json":
				data, err = diff.NormalizationJSON(report)
			}
			if err != nil {
				return err
			}
			if len(data) > 32<<20 {
				return errors.New("normalize output exceeds 32 MiB; select a narrower field scope")
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return errors.New("cannot write normalize output")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policy, "policy", "", "Local readmit-normalization-policy/v1 document declaring every rule (required)")
	cmd.Flags().StringArrayVar(&options.Fields, "field", nil, "Shared field selector to compare (repeatable; default all field repetitions)")
	cmd.Flags().StringArrayVar(&options.Keys, "key", nil, "Declared alignment-key selector (repeatable composite key; duplicates remain ambiguous)")
	cmd.Flags().StringVar(&boundary, "boundary", "messages", "Payload boundary: messages (actual sent bytes for runs) or acks (received bytes)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Report format: terminal, markdown, or json")
	cmd.Flags().StringVar(&inputFormat, "input-format", "auto", "Standalone-file framing: auto, raw, or mllp")
	cmd.Flags().StringVar(&terminator, "terminator", "auto", "Standalone-file terminator: auto, cr, lf, or crlf")
	return cmd
}
