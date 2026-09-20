package cli

import (
	"encoding/json/v2"
	"fmt"
)

// usage refuses a command that was invoked wrongly: a missing or empty flag, a
// format that is not one of the declared choices, a deadline that does not
// parse. It is the one shape for that refusal, so a misuse carries one process
// status everywhere — the same one a command reaches through argument parsing —
// while an execution failure keeps status 1. The message stays the command's
// own; only the decision is shared.
func usage(format string, args ...any) error {
	return &ExitError{Code: 2, Err: fmt.Errorf(format, args...)}
}

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

// declareInterruptible marks a command's work as stoppable by the construction
// pass, which gives its RunE a signal-cancelling context.
func declareInterruptible(capability string) map[string]string {
	annotations := declare(capability)
	annotations[interruptibleAnnotation] = "true"
	return annotations
}
