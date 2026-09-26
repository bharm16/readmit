// Package acklink answers, once, the question every consumer of a captured
// acknowledgement asks: which occurrence of this case does it answer? An
// acknowledgement answers the occurrence whose control identifier it echoes;
// what that means is declared here explicitly, as a scope (how far the echo
// is compared) and a comparison (what equality of two control identifiers
// means), because one product must not hold two notions of when two control
// IDs are the same while deciding it four ways.
//
// The index resolves nothing on its own and guesses nothing: when more than
// one occurrence in scope carries the echoed identifier, it reports them all,
// and choosing among them — refusing, colliding, or naming the case
// ambiguous — stays with the caller.
package acklink

import "slices"

// Scope declares how far an acknowledgement's echoed control identifier is
// compared.
type Scope int

const (
	// PerSource compares it only against occurrences the acknowledgement's
	// own case source captured.
	PerSource Scope = iota
	// CaseWide compares it against every occurrence of the case, whatever
	// source captured each.
	CaseWide
)

// Comparison declares what equality of two control identifiers means.
type Comparison int

const (
	// FieldBytes compares exact field bytes, including literal escapes, with
	// no trimming, case folding or escape decoding. That is the case bundle's
	// own rule for the same field.
	FieldBytes Comparison = iota
	// DecodedText compares decoded text through the shared field read.
	DecodedText
)

// Value is one control-identifier field in both of the forms a comparison
// can name: the exact bytes the case recorded, and the text they decode to
// when they decode. A caller fills what its own boundary gives it — a raw
// field read fills Bytes, a decoded one fills Text and Decoded — and the
// declared comparison decides which form is compared. A caller adds an
// occurrence, or asks about an echo, only when its own rules hold the field
// to be present and nonempty; this package compares what it is given and
// invents no presence of its own.
type Value struct {
	Bytes   []byte
	Text    string
	Decoded bool
}

// key reduces one value to the comparable form its comparison names, and
// answers whether the value names anything that comparison can test.
func (v Value) key(c Comparison) (string, bool) {
	if c == FieldBytes {
		return string(v.Bytes), true
	}
	return v.Text, v.Decoded
}

// Index holds the message occurrences of one case by the control identifier
// each carries, under the scope and comparison fixed when it is built.
type Index[T any] struct {
	scope      Scope
	comparison Comparison
	messages   map[string][]T
}

// New builds an empty index under an explicit scope and comparison. Nothing
// defaults: the callers of this package link under declared pairings, never
// an implied one.
func New[T any](scope Scope, comparison Comparison) *Index[T] {
	return &Index[T]{scope: scope, comparison: comparison, messages: make(map[string][]T)}
}

// NewDiagnosis builds the pairing a diagnosis links with: across the whole
// case, by decoded text. The finding review that promotes a diagnosis's
// findings builds the same pairing, so a promotion and the diagnosis it came
// from cannot disagree about which occurrence an acknowledgement answers.
func NewDiagnosis[T any]() *Index[T] { return New[T](CaseWide, DecodedText) }

// Add indexes one message occurrence by the control identifier it carries,
// in the order it is added, which is the order the case records its
// occurrences. A control identifier the declared comparison cannot test
// indexes nothing.
func (x *Index[T]) Add(sourceID string, control Value, occurrence T) {
	key, ok := x.entry(sourceID, control)
	if !ok {
		return
	}
	x.messages[key] = append(x.messages[key], occurrence)
}

// Answer reports the occurrences an acknowledgement answers: every message
// occurrence in scope whose control identifier equals the one it echoes, in
// the order the case records them. More than one answer means the case
// itself is ambiguous; an empty answer means nothing captured carries the
// echo. Either way the choice is the caller's.
func (x *Index[T]) Answer(sourceID string, echoed Value) []T {
	key, ok := x.entry(sourceID, echoed)
	if !ok {
		return nil
	}
	return slices.Clone(x.messages[key])
}

func (x *Index[T]) entry(sourceID string, value Value) (string, bool) {
	control, ok := value.key(x.comparison)
	if !ok {
		return "", false
	}
	if x.scope == PerSource {
		return sourceID + "\x00" + control, true
	}
	return control, true
}
