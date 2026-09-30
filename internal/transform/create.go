package transform

import (
	"errors"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
)

// Derivation is what a case written from a transformation declares about
// itself ([ADR-0004]): the sequence one plan produced over one verified case,
// kept as new derived evidence beside the case it came from.
//
// [ADR-0004]: docs/adr/0004-derived-evidence-and-generated-export.md
const Derivation = "readmit-transform/v1"

// Create writes the sequence one plan produces over one verified case as a new
// derived case at output, and answers the preview of what it wrote.
//
// The bytes written are exactly the bytes the preview read back: every entry
// of the sequence, in sequence order, holds its parent occurrence as the plan
// rewrote it, and a duplicated entry holds those bytes twice. Consecutive
// entries of one case source are kept as one source of the derived case, so
// its evidence order is the sequence's order. The case it reads is not
// touched, and output must be new.
func Create(path string, plan Plan, rules correlate.Rules, pack *profilepack.Pack, output string) (Preview, *bundle.Bundle, error) {
	e, declared, err := start(path, plan, rules, pack)
	if err != nil {
		return Preview{}, nil, err
	}
	preview, derived, err := e.finish(plan, declared)
	if err != nil {
		return Preview{}, nil, err
	}
	inputs, err := e.inputs(derived)
	if err != nil {
		return Preview{}, nil, err
	}
	written, err := bundle.Write(output, inputs, bundle.Provenance{Mode: bundle.Derived, Derivation: Derivation})
	if err != nil {
		return Preview{}, nil, err
	}
	if len(written.Events) != len(e.sequence) {
		return Preview{}, nil, errors.New("the derived case does not hold the sequence this transformation produced")
	}
	return preview, written, nil
}

// inputs rebuilds the sequence as case sources: one per run of consecutive
// entries of one declared source, in sequence order. Payload bytes carry their
// framing, so concatenating them restores a readable source without reframing
// anything.
func (e *engine) inputs(derived map[string][]byte) ([]bundle.Input, error) {
	inputs := []bundle.Input{}
	current := ""
	for _, entry := range e.sequence {
		payload, held := derived[entry.Parent]
		if !held {
			return nil, errors.New("an entry of the sequence names an occurrence the transformation did not read")
		}
		if entry.Source != current || len(inputs) == 0 {
			if len(inputs) == bundle.MaxSources {
				return nil, errors.New("this sequence alternates between case sources more often than a case can hold sources")
			}
			options := hl7.Options{}
			for _, declared := range e.source.Manifest.Sources {
				if declared.ID == entry.Source {
					options = hl7.Options{Format: declared.Format, Terminator: declared.Terminator}
				}
			}
			inputs = append(inputs, bundle.Input{Options: options, Observations: map[int]bundle.Observation{}})
			current = entry.Source
		}
		input := &inputs[len(inputs)-1]
		input.Data = append(input.Data, payload...)
		input.Observations[len(input.Observations)+1] = bundle.Observation{Direction: e.events[entry.Parent].Direction}
	}
	return inputs, nil
}
