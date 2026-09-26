// Package dictionary supplies a finite set of version-specific field labels
// and answers, in one place, the questions callers used to decide for
// themselves from the raw table: which version and message family a parsed
// message declares, whether these labels apply to it, and what label, status
// and provenance name one selected position. These labels do not provide
// semantic validation or a conformance profile.
package dictionary

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"sync"
)

// Field-label data is adapted from nHapi under MPL-2.0; see
// docs/dictionary-provenance.md and licenses/nhapi-MPL-2.0.txt.
//
//go:embed fields-v251.json
var labels []byte

// Provenance is the recorded origin of the bundled labels, as every reporter
// of a label states it beside the name: one sentence naming the upstream
// revision, the license and the provenance record. A caller never writes its
// own, so two reports of one label cannot disagree about where it came from.
const Provenance = "nHapi 2495edd1e23a85ab9146cb03947c17d45120cf1f; MPL-2.0; docs/dictionary-provenance.md"

// The status vocabulary for one selected position: what the labels say the
// selection is named by. Every reporter of a label uses these values, so a
// position is reported the same way wherever it is shown.
const (
	// StatusUnsupportedVersion: the message declares a version these labels
	// do not name, so no position of it is named by them.
	StatusUnsupportedVersion = "unsupported_version"
	// StatusUnavailable: the bundled labels could not be read, so nothing
	// can be said about any position.
	StatusUnavailable = "unavailable"
	// StatusUnsupportedSegment: the labels apply to the message but name no
	// position of this segment.
	StatusUnsupportedSegment = "unsupported_segment"
	// StatusFieldLabelsOnly: the selection is a whole message or segment,
	// which the labels name by position alone, never as a whole.
	StatusFieldLabelsOnly = "field_labels_only"
	// StatusUnlabeledPosition: the labels apply and name this segment, but
	// not this position.
	StatusUnlabeledPosition = "unlabeled_position"
	// StatusLabeledField: the labels name this position.
	StatusLabeledField = "labeled_field"
)

type Dictionary struct {
	Contract   string `json:"contract"`
	HL7Version string `json:"hl7_version"`
	segments   map[string]map[int]string
}

// load reads the bundled field labels exactly as written and refuses the
// document unless it is the contract this release ships.
func load() (*Dictionary, error) {
	var decoded struct {
		Contract   string                    `json:"contract"`
		HL7Version string                    `json:"hl7_version"`
		Segments   map[string]map[int]string `json:"segments"`
	}
	if err := json.Unmarshal(labels, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("cannot load bundled field labels")
	}
	if decoded.Contract != "readmit-field-labels/v1" || decoded.HL7Version != "2.5.1" {
		return nil, fmt.Errorf("unsupported bundled field labels")
	}
	return &Dictionary{Contract: decoded.Contract, HL7Version: decoded.HL7Version, segments: decoded.Segments}, nil
}

// loaded is the one parse: the labels are embedded in the executable and
// never change while it runs, so every caller is answered from the same
// reading instead of each parsing them again.
var loaded = sync.OnceValues(load)

// Load returns the parsed bundled field labels, read from the executable
// once and shared by every caller. The table itself stays here: callers ask
// this package which labels apply and what a position is named by, and never
// walk the raw map.
func Load() (*Dictionary, error) { return loaded() }

// Applies reports whether a combination a message declares is the one these
// labels name, so they may name its positions. The bundled
// readmit-field-labels/v1 contract is version-scoped and carries no family,
// so only the declared version is asked.
func (d *Dictionary) Applies(c Combination) bool { return c.Version == d.HL7Version }

// Label is the name these labels declare for one one-based field position of
// a segment, or "" where they name nothing. An empty name is a real answer:
// a position the labels do not name is reported as such, never borrowed.
func (d *Dictionary) Label(segment string, position int) string {
	return d.segments[segment][position]
}

// LabelsSegment reports whether these labels name any position of a segment.
func (d *Dictionary) LabelsSegment(segment string) bool {
	return len(d.segments[segment]) > 0
}

// LastPosition is the highest field position these labels name for a segment,
// or zero where they name none of it. An inspection reports a labelled
// position the message leaves out as explicitly omitted rather than leaving
// it out of the listing.
func (d *Dictionary) LastPosition(segment string) int {
	last := 0
	for position := range d.segments[segment] {
		last = max(last, position)
	}
	return last
}

// Positions visits every labelled position, in no particular order. A
// describer of the labels — such as the test that shows how a profile pack
// would carry them — reads them this way instead of through the raw table.
func (d *Dictionary) Positions(visit func(segment string, position int, name string)) {
	for segment, fields := range d.segments {
		for position, name := range fields {
			visit(segment, position, name)
		}
	}
}

// Position names one selection a label question is asked about. Kind is the
// hl7 tree's kind: a whole message and a segment are named by position alone,
// and any other kind — a field, a repetition, a component or a subcomponent —
// names the field at Segment and Field.
type Position struct {
	Kind    string
	Segment string
	Field   int
}

// Answer is what the labels say about one position: the status vocabulary
// value that says how it is named, and the name when one applies.
type Answer struct {
	Status string
	Label  string
}

// At answers for one position of a message these labels apply to. A whole
// message or segment is named by position alone; a segment the labels carry
// nothing for is unsupported; a labelled position carries its name, and an
// unlabelled one says so.
func (d *Dictionary) At(p Position) Answer {
	answer := Answer{Status: StatusUnlabeledPosition}
	if p.Kind == "message" {
		answer.Status = StatusFieldLabelsOnly
		return answer
	}
	if !d.LabelsSegment(p.Segment) {
		answer.Status = StatusUnsupportedSegment
		return answer
	}
	if p.Kind == "segment" {
		answer.Status = StatusFieldLabelsOnly
		return answer
	}
	answer.Label = d.Label(p.Segment, p.Field)
	if answer.Label != "" {
		answer.Status = StatusLabeledField
	}
	return answer
}
