package importer

import (
	"errors"
)

// EnvelopeShape is the declared container format of one bounded document, with
// no mapping of its contents onto anything. It is the half of a Recipe that
// says how bytes divide into records and where a value sits inside one, without
// the half that turns a record into an HL7 occurrence.
//
// It exists so a caller that reads a JSON, CSV, XML or text export for some
// other reason reads it through this package's readers rather than writing a
// second set. The bounds, the refusals and the byte preservation are the ones
// [Recipe] already applies: nothing is normalized, a line ending inside a
// quoted CSV field is kept exactly as it was written, and a structure that
// contradicts the declaration is refused rather than read a different way.
type EnvelopeShape struct {
	Envelope Envelope
	Encoding Encoding
	// Exactly one dialect is declared, and it is the one Envelope names.
	CSV  *CSVDialect
	Text *TextDialect
	JSON *DocumentDialect
	XML  *DocumentDialect
}

// LocatedRecord is one record of a divided document: the values the declared
// locators name, or the one reason the record could not be read into them. A
// record is never half-read, so a reason and a value never appear together.
type LocatedRecord struct {
	Values map[string]string
	Reason string
}

// Value returns what one declared locator names in this record.
func (r LocatedRecord) Value(locator Locator) (string, bool) {
	value, ok := r.Values[locatorKey(locator)]
	return value, ok
}

// recipe is the shape expressed as the recipe the envelope readers take. The
// mapping half stays zero: Divide resolves locators and nothing else, so no
// payload, time, label or direction operator can run from here.
func (s EnvelopeShape) recipe() Recipe {
	return Recipe{Envelope: s.Envelope, Encoding: s.Encoding, CSV: s.CSV, Text: s.Text, JSON: s.JSON, XML: s.XML}
}

// Validate holds a shape and the locators read through it to exactly the rules
// a mapping recipe's envelope half is held to, so a declaration this package
// would refuse in a recipe is refused here too.
func (s EnvelopeShape) Validate(locators []Locator) error {
	shaped := s.recipe()
	if err := shaped.validateDialect(); err != nil {
		return err
	}
	if err := shaped.validateEncoding(); err != nil {
		return err
	}
	if len(locators) == 0 {
		return errors.New("a divided document declares at least one locator")
	}
	if len(locators) > MaxEnvelopeFields {
		return errors.New("a divided document declares more locators than one record holds")
	}
	for _, locator := range locators {
		if err := shaped.validateLocator(locator, "declared"); err != nil {
			return err
		}
	}
	return nil
}

// Divide reads one bounded document into its records, resolving exactly the
// locators it is given. The caller validated the shape; the bytes are checked
// against the declared encoding here, because only the bytes can contradict it.
//
// An error is a document that contradicts its declaration or exceeds the
// envelope bounds. A record this reader could not divide is not an error: it
// comes back carrying its reason, so a caller can report evidence that has no
// single reading rather than a document that held fewer records.
func (s EnvelopeShape) Divide(locators []Locator, data []byte) ([]LocatedRecord, error) {
	if err := s.Validate(locators); err != nil {
		return nil, err
	}
	if err := declaredEncoding(s.Encoding, data); err != nil {
		return nil, err
	}
	divided, err := envelopeRecords(s.recipe(), locators, data)
	if err != nil {
		return nil, err
	}
	records := make([]LocatedRecord, 0, len(divided))
	for _, record := range divided {
		located := LocatedRecord{Reason: record.reason}
		if record.reason == "" {
			located.Values = make(map[string]string, len(record.values))
			for key, value := range record.values {
				located.Values[key] = string(value)
			}
		}
		records = append(records, located)
	}
	return records, nil
}
