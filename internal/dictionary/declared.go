package dictionary

import (
	"github.com/bharm16/readmit/internal/hl7"
)

// Combination is one HL7 version and message family, as a message declares
// them in MSH-12 and MSH-9.
type Combination struct {
	Version string
	Family  string
}

// The positions a declaration is read at: the first component of the first
// repetition of MSH-12 and of MSH-9, addressed through the shared selector
// exactly as the inspector selects MSH-12.1. Every caller asks the question
// here, so no reader of a message resolves the version differently.
var (
	versionSelector = mustSelector("MSH[1]-12[1].1")
	familySelector  = mustSelector("MSH[1]-9[1].1")
)

func mustSelector(path string) hl7.Selector {
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		panic("dictionary: " + err.Error())
	}
	return selector
}

// Declared reads the combination one parsed message declares: the first
// component of MSH-12 and of MSH-9, as the bytes are written. Nothing is
// normalized and nothing is inferred from the segments present, so a message
// that declares neither, one that does not begin with MSH, or one outside the
// document declares nothing and every question about it is unknown.
func Declared(doc *hl7.Document, messageIndex int) Combination {
	if doc == nil || messageIndex < 0 || messageIndex >= len(doc.Messages) {
		return Combination{}
	}
	message := doc.Messages[messageIndex]
	if len(message.Segments) == 0 || message.Segments[0].ID != "MSH" {
		return Combination{}
	}
	return Combination{
		Version: declaredText(doc, messageIndex, versionSelector),
		Family:  declaredText(doc, messageIndex, familySelector),
	}
}

// declaredText is one declared position's bytes exactly as written. A
// position the message leaves out declares nothing.
func declaredText(doc *hl7.Document, messageIndex int, selector hl7.Selector) string {
	value, err := doc.Select(messageIndex, selector)
	if err != nil || value.State != hl7.Present {
		return ""
	}
	return string(doc.Bytes(value.Span))
}
