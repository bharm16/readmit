package testrunner

import (
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

// ACKReadFailure names the step at which one retained acknowledgement could
// not be read. The reading is this module's; each caller turns the failure
// into its own refusal sentence, so the verdict and the authoring proposal
// can never read the same bytes two ways.
type ACKReadFailure uint8

const (
	// ACKUndecodable means the bytes did not decode as exactly one message.
	ACKUndecodable ACKReadFailure = iota + 1
	// ACKNoSuchPosition means the acknowledgement does not address the
	// selector, or the selector itself cannot be read.
	ACKNoSuchPosition
	// ACKNotText means the value at the position is not text an expectation
	// can state.
	ACKNotText
)

// ParseACK decodes one retained acknowledgement payload as exactly one
// message. It is the one parse of the bytes a verdict is decided against and
// a proposal is suggested from.
func ParseACK(raw []byte) (*hl7.Document, ACKReadFailure) {
	document, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(document.Messages) != 1 {
		return nil, ACKUndecodable
	}
	return document, 0
}

// ACKField reads the value one decoded acknowledgement held at one selector.
// An absent position is a value: only a position that cannot be read at all,
// or a value that is not text, is a failure.
func ACKField(document *hl7.Document, selector string) (*FieldValue, ACKReadFailure) {
	parsed, err := hl7.ParseSelector(selector)
	if err != nil {
		return nil, ACKNoSuchPosition
	}
	value, err := document.Select(0, parsed)
	if err != nil {
		return nil, ACKNoSuchPosition
	}
	field := &FieldValue{State: value.State}
	if value.State == hl7.Present {
		decoded, err := hl7.Decode(document.Bytes(value.Span), document.Messages[0].Delimiters)
		if err != nil || !utf8.Valid(decoded) {
			return nil, ACKNotText
		}
		text := string(decoded)
		field.Text = &text
	}
	return field, 0
}
