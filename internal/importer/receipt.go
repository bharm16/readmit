package importer

import (
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
)

// MaxDocumentBytes bounds one preview or receipt. A document past the bound is
// refused rather than truncated, so a receipt is complete or it is an error.
const MaxDocumentBytes = 4 << 20

// Preview is what an import would extract, before anything is written. It
// carries the plan it was produced under, every container member with the
// records it would become, and the totals. It names no case, because no case
// exists: a preview creates, modifies and removes nothing.
type Preview struct {
	Schema     string      `json:"schema"`
	Plan       Plan        `json:"plan"`
	Containers []Container `json:"containers"`
	Totals     Totals      `json:"totals"`
}

// CaseRef names the evidence an import wrote, exactly as the case bundle
// reader reported it. Nothing here is derived a second time.
type CaseRef struct {
	Identity   string `json:"identity"`
	Schema     string `json:"schema"`
	Provenance string `json:"provenance"`
}

// Quarantined is one retained occurrence this release could not parse. It is
// evidence, kept in the case with all of its original bytes; the reason is the
// parser's own bounded diagnostic and holds no value from the message.
type Quarantined struct {
	SourceID string `json:"source_id"`
	EventID  string `json:"event_id"`
	Reason   string `json:"reason"`
}

// Receipt is what an import did write: the same plan and containers the preview
// carried, the case it produced, and every occurrence that was retained as
// quarantined evidence rather than parsed.
type Receipt struct {
	Schema      string        `json:"schema"`
	Plan        Plan          `json:"plan"`
	ImportedAt  time.Time     `json:"imported_at"`
	Case        CaseRef       `json:"case"`
	Containers  []Container   `json:"containers"`
	Quarantined []Quarantined `json:"quarantined"`
	Totals      Totals        `json:"totals"`
}

// NewPreview describes an extraction without writing anything.
func NewPreview(plan Plan, e *Extraction) Preview {
	return Preview{Schema: PreviewSchema, Plan: plan, Containers: e.Containers, Totals: e.Totals}
}

// NewReceipt describes the case an extraction actually wrote. The written
// evidence is checked against the extraction it came from first: the case must
// hold exactly the sources the extraction produced, each with the same bytes,
// so a receipt can never describe evidence other than the evidence beside it.
func NewReceipt(plan Plan, e *Extraction, importedAt time.Time, b *bundle.Bundle) (Receipt, error) {
	if err := e.verifyWritten(b); err != nil {
		return Receipt{}, err
	}
	return Receipt{
		Schema:      ReceiptSchema,
		Plan:        plan,
		ImportedAt:  importedAt,
		Case:        CaseRef{Identity: b.Identity, Schema: b.Manifest.Schema, Provenance: string(b.Manifest.Provenance.Mode)},
		Containers:  e.Containers,
		Quarantined: quarantinedOccurrences(b),
		Totals:      e.Totals,
	}, nil
}

// verifyWritten checks a written case against the extraction it came from: the
// same number of sources, each with the same bytes. A receipt that did not pass
// it could describe evidence other than the evidence beside it.
func (e *Extraction) verifyWritten(b *bundle.Bundle) error {
	if len(b.Manifest.Sources) != len(e.Inputs) {
		return errors.New("the written case does not hold the extracted sources")
	}
	for i, source := range b.Manifest.Sources {
		if source.Size != len(e.Inputs[i].Data) || source.SHA256 != digest(e.Inputs[i].Data) {
			return errors.New("the written case does not hold the extracted bytes")
		}
	}
	return nil
}

// quarantinedOccurrences lists every occurrence the case retained rather than
// parsed, with the parser's own bounded diagnostic as the reason.
func quarantinedOccurrences(b *bundle.Bundle) []Quarantined {
	quarantined := []Quarantined{}
	for _, event := range b.Events {
		if event.Kind != bundle.Unparsed {
			continue
		}
		reason := event.ParseError
		if reason == "" {
			reason = "retained without a parser diagnostic"
		}
		quarantined = append(quarantined, Quarantined{SourceID: event.SourceID, EventID: event.ID, Reason: reason})
	}
	return quarantined
}

// EncodePreview and EncodeReceipt write one document deterministically, so the
// same import produces the same bytes on every machine.
func EncodePreview(preview Preview) ([]byte, error) {
	if preview.Schema != PreviewSchema {
		return nil, ErrUnsupportedVersion
	}
	return encodeDocument(preview)
}

func EncodeReceipt(receipt Receipt) ([]byte, error) {
	if receipt.Schema != ReceiptSchema {
		return nil, ErrUnsupportedVersion
	}
	if receipt.Case.Identity == "" {
		return nil, errors.New("an import receipt names the case it wrote")
	}
	return encodeDocument(receipt)
}

func encodeDocument(document any) ([]byte, error) {
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the import document")
	}
	data = append(data, '\n')
	if len(data) > MaxDocumentBytes {
		return nil, errors.New("the import document exceeds its size limit")
	}
	return data, nil
}
