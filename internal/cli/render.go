package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

func render(out io.Writer, doc *hl7.Document, dictionary *dictionary.Dictionary, options hl7.Options, showValues bool) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "Format: %s (%s)\nMessages: %d\n", doc.Format, selection(string(options.Format)), len(doc.Messages))
	for i, message := range doc.Messages {
		labels := messageLabels(doc, message, dictionary)
		profile := "positional only"
		if labels != nil {
			profile = "HL7 v2.5.1 field labels v1 (syntax only)"
		}
		fmt.Fprintf(w, "Message %d: terminator=%s (%s), bytes [%d,%d), %s\n", i+1, message.Terminator, selection(string(options.Terminator)), message.Span.Start, message.Span.End, profile)
		for _, segment := range message.Segments {
			fmt.Fprintf(w, "  %s bytes [%d,%d)\n", segment.ID, segment.Span.Start, segment.Span.End)
			last := len(segment.Fields)
			for number := range labels[segment.ID] {
				last = max(last, number)
			}
			for number := 1; number <= last; number++ {
				field := segment.Field(number)
				name := fmt.Sprintf("%s-%d", segment.ID, number)
				if label := labels[segment.ID][number]; label != "" {
					name += " " + label
				}
				fmt.Fprintf(w, "    %s: %s", name, field.State)
				if field.State != hl7.Omitted {
					fmt.Fprintf(w, " (%d bytes)", field.Span.End-field.Span.Start)
					if showValues {
						fmt.Fprintf(w, " %s", strconv.QuoteToASCII(string(doc.Bytes(field.Span))))
					}
				}
				fmt.Fprintln(w)
				if len(field.Repetitions) > 1 {
					for n, repetition := range field.Repetitions {
						fmt.Fprintf(w, "      repetition %d: %s (%d bytes)\n", n+1, repetition.State, repetition.Span.End-repetition.Span.Start)
					}
				}
			}
		}
	}
	return w.Flush()
}
