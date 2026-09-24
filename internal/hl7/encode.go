package hl7

import (
	"fmt"
	"strings"
	"time"
)

// TimestampLayout is the HL7 timestamp (TS) layout every readmit writer
// carries: whole seconds and an explicit four-digit zone offset. It is the
// writing half of the layout the parser decodes, so the two cannot drift.
const TimestampLayout = "20060102150405-0700"

// Time encodes one instant as an HL7 timestamp the way fixture generators and
// receipts carry one: UTC, whole seconds, an explicit offset. A writer whose
// contract is a declared offset rather than UTC — the scenario generator's
// timezone variant — formats TimestampLayout itself and says why.
func Time(value time.Time) string {
	return value.UTC().Format(TimestampLayout)
}

// Surrogate is the value readmit writes in place of the n-th identifier it
// renames: READMIT and n in six digits. The transform preview and a replay
// both write this one form, each numbering its own renames.
func Surrogate(n int) []byte {
	return fmt.Appendf(nil, "READMIT%06d", n)
}

// Encode joins a segment table into one unframed HL7 message: fields joined
// with the field separator, every segment terminated, nothing escaped, and no
// empty trailing field trimmed — the segments are exactly what the writer
// means to put on the wire. Framing is a transport question: hand the result
// to mllp.Frame where the wire needs it. Field values a message may carry
// from untrusted text are the writer's to escape before they reach a segment.
func Encode(segments [][]string) []byte {
	var b strings.Builder
	for _, segment := range segments {
		b.WriteString(strings.Join(segment, "|"))
		b.WriteByte('\r')
	}
	return []byte(b.String())
}
