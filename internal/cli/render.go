package cli

import (
	"bufio"
	"fmt"
	"io"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
)

// render prints what the shared inspection reports, one line per row. The
// rows, their order and their values are the operation's; this decides only
// how each one reads on a terminal.
func render(out io.Writer, inspected *operation.Inspected, showValues bool) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "Format: %s (%s)\nMessages: %d\n", inspected.Format, inspected.FormatSelection, inspected.Messages())
	inspected.Rows(func(row operation.InspectionRow) bool {
		switch row.Kind {
		case operation.InspectMessageRow:
			fmt.Fprintf(w, "Message %d: terminator=%s (%s), bytes [%d,%d), %s\n", row.Message, row.Terminator, inspected.TerminatorSelection, row.Start, row.End, row.Profile)
		case operation.InspectSegmentRow:
			fmt.Fprintf(w, "  %s bytes [%d,%d)\n", row.Segment, row.Start, row.End)
		case operation.InspectFieldRow:
			name := fmt.Sprintf("%s-%d", row.Segment, row.Field)
			if row.Label != "" {
				name += " " + row.Label
			}
			fmt.Fprintf(w, "    %s: %s", name, row.State)
			if row.State != hl7.Omitted {
				fmt.Fprintf(w, " (%d bytes)", row.End-row.Start)
				if showValues {
					value, _, _ := inspected.Value(row, 0)
					fmt.Fprintf(w, " %s", value)
				}
			}
			fmt.Fprintln(w)
		case operation.InspectRepetitionRow:
			fmt.Fprintf(w, "      repetition %d: %s (%d bytes)\n", row.Repetition, row.State, row.End-row.Start)
		}
		return true
	})
	return w.Flush()
}
