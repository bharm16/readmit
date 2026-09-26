package fixturetrial

import (
	"bytes"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/receiver"
)

// VerifyACK reports whether raw is the built-in fixture's acknowledgement of
// request: the request's own trigger and control ID, an AA acceptance, the
// sequence's own READMITACK control ID, a valid UTC timestamp and a receipt
// naming sessionID and occurrenceID. The wire bytes are spelled once, in the
// fixture that produces them; verifying a retained copy rebuilds them, so a
// copied check cannot drift from the fixture, and arbitrary extra segments or
// values cannot hide in synthetic evidence. request may be the approved
// request bytes or the frame that carried them.
func VerifyACK(request, raw []byte, sequence int, sessionID, occurrenceID string) error {
	invalid := errors.New("retained ACK is not the canonical built-in fixture acknowledgement")
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(doc.Messages) != 1 {
		return invalid
	}
	stamp := string(doc.Bytes(doc.Messages[0].Segments[0].Field(7).Span))
	parsed, err := time.Parse("20060102150405-0700", stamp)
	if err != nil || parsed.UTC().Format("20060102150405-0700") != stamp {
		return invalid
	}
	want, err := receiver.CanonicalACK(request, stamp, sequence, sessionID, occurrenceID)
	if err != nil {
		return invalid
	}
	if !bytes.Equal(raw, want) {
		return invalid
	}
	return nil
}
