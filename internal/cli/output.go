package cli

import (
	"encoding/json/v2"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// writeJSON is the one machine-readable writer: deterministic encoding, one
// document per line, to stdout. Every command that emits JSON writes through
// it, so a change to the encoding discipline is made once and holds everywhere.
func writeJSON(cmd *cobra.Command, document any) error {
	return writeJSONTo(cmd.OutOrStdout(), document)
}

func writeJSONTo(out io.Writer, document any) error {
	b, err := json.Marshal(document, json.Deterministic(true))
	if err == nil {
		_, err = fmt.Fprintln(out, string(b))
	}
	return err
}
