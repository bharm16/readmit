package testrunner_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
)

// One acknowledgement with everything the reader has to decide about: a
// present control ID, a value carrying delimiter escapes, and a value whose
// escape decodes to bytes that are not text.
const ackMessage = "MSH|^~\\&|RECV|B|SEND|A|20260101120101||ACK^S12|ACK-1|P|2.5.1\r" +
	"MSA|AA|CTL-1\r" +
	"ZEA|1|A\\F\\B|\\X00FF\\\r"

// A retained payload is MLLP framed; the reader is given the framed bytes.
const ackBytes = "\x0b" + ackMessage + "\x1c\r"

func ack() *hl7.Document {
	document, failure := testrunner.ParseACK([]byte(ackBytes))
	if failure != 0 {
		panic("fixture acknowledgement did not decode")
	}
	return document
}

// ParseACK and ACKField are the one reading the verdict and the proposal
// share: what a spec is decided against is exactly what authoring proposes.
func TestACKReadingIsOneDecisionForVerdictAndProposal(t *testing.T) {
	document := ack()

	field, failure := testrunner.ACKField(document, "MSA-2")
	if failure != 0 || field == nil || field.State != hl7.Present || field.Text == nil || *field.Text != "CTL-1" {
		t.Fatalf("present value misread: %+v failure=%d", field, failure)
	}

	// Delimiter escapes decode to the separators they stand for.
	escaped, failure := testrunner.ACKField(document, "ZEA-2")
	if failure != 0 || escaped.Text == nil || *escaped.Text != "A|B" {
		t.Fatalf("escaped value misread: %+v failure=%d", escaped, failure)
	}

	// An escaped value that is not text is a failure, not an empty value.
	notText, failure := testrunner.ACKField(document, "ZEA-3")
	if failure != testrunner.ACKNotText || notText != nil {
		t.Fatalf("non-text value read as a value: %+v failure=%d", notText, failure)
	}

	// A position the acknowledgement does not carry is an absent value.
	absent, failure := testrunner.ACKField(document, "ZEA-9")
	if failure != 0 || absent.State != hl7.Omitted {
		t.Fatalf("an absent position read as a failure: %+v failure=%d", absent, failure)
	}
}

func TestACKReadingRefusesUndecodableBytesAndUnaddressedPositions(t *testing.T) {
	for name, raw := range map[string]string{
		"not hl7":       "\x0bNOT-HL7-AT-ALL\x1c\r",
		"two messages":  "\x0bMSH|^~\\&|A|B|C|D|20260101120101||ACK^S12|1|P|2.5.1\rMSA|AA|1\r\x0bMSH|^~\\&|A|B|C|D|20260101120101||ACK^S12|2|P|2.5.1\rMSA|AA|2\r\x1c\r",
		"truncated msh": "\x0bMSH|^~\\&|RECV\x1c\r",
	} {
		if _, failure := testrunner.ParseACK([]byte(raw)); failure != testrunner.ACKUndecodable {
			t.Fatalf("%s decoded: failure=%d", name, failure)
		}
	}
	if _, failure := testrunner.ACKField(ack(), "MSA-2(3)"); failure != testrunner.ACKNoSuchPosition {
		t.Fatalf("an unaddressable position read as a value: failure=%d", failure)
	}
	if _, failure := testrunner.ACKField(ack(), "NOT A SELECTOR"); failure != testrunner.ACKNoSuchPosition {
		t.Fatalf("an unreadable selector read as a value: failure=%d", failure)
	}
}
