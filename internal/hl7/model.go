package hl7

import "bytes"

type Format string

const (
	Raw  Format = "raw"
	MLLP Format = "mllp"
)

type Terminator string

const (
	CR   Terminator = "cr"
	LF   Terminator = "lf"
	CRLF Terminator = "crlf"
)

type Options struct {
	Format     Format
	Terminator Terminator
}

// Span is a half-open byte range in the original file, including MLLP offsets.
type Span struct{ Start, End int }

type State string

const (
	Present State = "present"
	Empty   State = "empty"
	Null    State = "null"
	Omitted State = "omitted"
)

type Field struct {
	Number      int
	Span        Span
	State       State
	Repetitions []Value
}

type Value struct {
	Span  Span
	State State
}

type Segment struct {
	ID     string
	Span   Span
	Fields []Field
}

// Field returns Omitted for a position absent from the source. Empty fields
// between separators or after a trailing separator remain explicitly present.
func (s Segment) Field(number int) Field {
	if number < 1 || number > len(s.Fields) {
		return Field{Number: number, State: Omitted}
	}
	return s.Fields[number-1]
}

type Delimiters struct {
	Field, Component, Repetition, Escape, Subcomponent byte
	Truncation                                         byte
}

type Message struct {
	Span       Span
	Terminator Terminator
	Delimiters Delimiters
	Segments   []Segment
}

type Document struct {
	Format   Format
	Messages []Message
	source   []byte
}

// Serialize returns the original evidence, not a reconstruction of its views.
// There is deliberately no editing API: modifying a view cannot rewrite evidence.
func (d *Document) Serialize() []byte { return bytes.Clone(d.source) }

// Bytes returns a copy so callers cannot mutate the underlying evidence.
func (d *Document) Bytes(span Span) []byte {
	if span.Start < 0 || span.End < span.Start || span.End > len(d.source) {
		return nil
	}
	return bytes.Clone(d.source[span.Start:span.End])
}
