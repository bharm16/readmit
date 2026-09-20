package cli

import (
	"errors"

	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

func normalizeCommand() *cobra.Command {
	var options diff.Options
	var policy, format, inputFormat, terminator, boundary string
	cmd := &cobra.Command{
		Use: "normalize LEFT RIGHT --policy file", Short: "Compare fields under a declared normalization policy and list every difference it suppresses", Annotations: declare(capabilityFree),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policy == "" {
				return usage("normalize requires a --policy document")
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
			data, err := renderSelection(format, diff.NormalizationTerminal(report), diff.NormalizationMarkdown(report), report)
			if err != nil {
				return err
			}
			if err := checkRendered(data); err != nil {
				return err
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
