package bundle

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
)

// WriteEngineExport retains the exact export container and adapter declaration
// inside a v5 case. Reopening recomputes extraction before trusting its sources.
// The supplied engine/version are operator declarations, never authenticated.
func WriteEngineExport(ctx context.Context, path, source string, data []byte, plan engineexport.Plan, importedAt time.Time) (*Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := engineexport.Extract(plan, data)
	if err != nil {
		return nil, err
	}
	inputs := make([]Input, len(records))
	for i, r := range records {
		inputs[i] = Input{Path: source, Data: r.Payload, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.Terminator(plan.Terminator)}}
	}
	b, err := build(inputs, Provenance{Mode: Imported, ImportedAt: &importedAt})
	if err != nil {
		return nil, err
	}
	if err := attachEngineExport(b, plan, data); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return writeBundle(ctx, path, b)
}
func attachEngineExport(b *Bundle, plan engineexport.Plan, data []byte) error {
	records, err := engineexport.Extract(plan, data)
	if err != nil {
		return err
	}
	if len(records) != len(b.Events) || len(records) != len(b.Manifest.Sources) {
		return errors.New("engine export extraction disagrees with retained sources")
	}
	for i, r := range records {
		e := b.Events[i]
		if !bytes.Equal(r.Payload, b.payloads[e.ID]) || e.SourceID != b.Manifest.Sources[i].ID || e.Direction != Unknown || e.ObservedAt != nil || b.Manifest.Sources[i].Format != hl7.Raw || b.Manifest.Sources[i].Terminator != hl7.Terminator(plan.Terminator) {
			return errors.New("engine export extraction disagrees with retained evidence")
		}
	}
	encoded, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return err
	}
	b.Manifest.Schema = EngineExportSchema
	b.Manifest.EngineExport = &Payload{Path: "engine-export.json", Size: len(encoded), SHA256: digest(encoded)}
	b.EngineExport = &plan
	b.engineContainer = bytes.Clone(data)
	return nil
}

// EngineContainer returns the exact retained export bytes, including framing,
// XML syntax and exporter-added separators. The caller explicitly exposes data.
func (b *Bundle) EngineContainer() ([]byte, error) {
	if b.EngineExport == nil {
		return nil, errors.New("case has no retained engine export")
	}
	return bytes.Clone(b.engineContainer), nil
}
