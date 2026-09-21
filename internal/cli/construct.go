package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// usage lives in exit.go with the rest of the status vocabulary.

// maxRenderedOutput bounds one rendered report a command holds in memory before
// it is written. A larger rendering is refused rather than truncated, and the
// refusal names the one bound instead of five near copies of it.
const maxRenderedOutput = 32 << 20

// renderSelection is the one seam between a command's document and the format
// the operator selected. The command supplies its three renderings — terminal
// and markdown as bytes, the JSON document for deterministic encoding — and the
// seam owns the choice, the encoding discipline, and the size bound. What the
// command still decides is where the bytes go: stdout, or one new file.
func renderSelection(format string, terminal, markdown []byte, document any) ([]byte, error) {
	switch format {
	case "terminal":
		return terminal, nil
	case "markdown":
		return markdown, nil
	case "json":
		data, err := jsonMarshalDeterministic(document)
		if err != nil {
			return nil, err
		}
		// One document per line, the discipline writeJSON holds on stdout.
		data = append(data, '\n')
		if len(data) > maxRenderedOutput {
			return nil, usage("output exceeds %s; select a narrower scope", humanBytes(maxRenderedOutput))
		}
		return data, nil
	}
	return nil, usage("format must be terminal, markdown, or json")
}

// checkRendered applies the size bound to a rendering a command built itself,
// the way renderSelection does for the ones it builds.
func checkRendered(data []byte) error {
	if len(data) > maxRenderedOutput {
		return usage("output exceeds %s; select a narrower scope", humanBytes(maxRenderedOutput))
	}
	return nil
}

func humanBytes(n int) string {
	return fmt.Sprintf("%d MiB", n>>20)
}

func jsonMarshalDeterministic(document any) ([]byte, error) {
	return json.Marshal(document, json.Deterministic(true))
}

// writeTerminalOrJSON is the shared tail of every command whose report is one
// rendered document: the format rule, the size bound, and the destination —
// stdout, or one new file outside the case — are decided here, while the
// command supplies its two renderings and the nouns its refusals name. The
// command noun carries the flag rule ("correlate format must be…"); the
// artifact noun carries the write refusals ("cannot write correlation
// output").
// checkReportFlags holds the two flag rules a rendered-report command answers
// to. writeTerminalOrJSON enforces them again at the point of writing; a
// command that reads its inputs first calls this before that work, so a
// misuse keeps its precedence over whatever the inputs refuse.
func checkReportFlags(cmd *cobra.Command, format, output, commandNoun string) error {
	if format != "terminal" && format != "json" {
		return usage("%s format must be terminal or json", commandNoun)
	}
	if cmd.Flags().Changed("output") && output == "" {
		return usage("%s output must name a new file", commandNoun)
	}
	return nil
}

func writeTerminalOrJSON(cmd *cobra.Command, format, output, commandNoun, artifactNoun string, terminal []byte, jsonDocument func() ([]byte, error)) error {
	if err := checkReportFlags(cmd, format, output, commandNoun); err != nil {
		return err
	}
	data := terminal
	if format == "json" {
		var err error
		if data, err = jsonDocument(); err != nil {
			return err
		}
	}
	if err := checkRendered(data); err != nil {
		return err
	}
	if output != "" {
		return writeNewFile(output, data,
			"cannot create "+artifactNoun+"; destination must be new and outside the case",
			"cannot write "+artifactNoun)
	}
	if _, err := cmd.OutOrStdout().Write(data); err != nil {
		return errors.New("cannot write " + artifactNoun)
	}
	return nil
}

// writeReport is the one terminal-or-JSON decision for a command summary: the
// machine form is writeJSON's deterministic encoding, the human form is the
// command's own rendering, and failing to write either is the command's one
// refusal at status 2.
func writeReport(cmd *cobra.Command, asJSON bool, document any, terminal func(*cobra.Command) error, refused string) error {
	var err error
	if asJSON {
		err = writeJSON(cmd, document)
	} else {
		err = terminal(cmd)
	}
	if err != nil {
		return refusal(errors.New(refused))
	}
	return nil
}

// declareInterruptible marks a command's work as stoppable by the construction
// pass, which gives its RunE a signal-cancelling context.
func declareInterruptible(capability string) map[string]string {
	annotations := declare(capability)
	annotations[interruptibleAnnotation] = "true"
	return annotations
}
