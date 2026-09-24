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
	// NoState is the zero State. It names no decoded state, where a document
	// states a value in place of one, as a filter's value predicate does.
	NoState State = ""
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
	// options are the ones the source was parsed with, so a rewrite reads its
	// result back exactly as the source was read.
	options Options
}

// Serialize returns the original evidence, not a reconstruction of its views.
// Nothing edits a parsed document: modifying a view cannot rewrite evidence,
// and Rewrite writes new bytes beside it.
func (d *Document) Serialize() []byte { return bytes.Clone(d.source) }

// Bytes returns a copy so callers cannot mutate the underlying evidence.
func (d *Document) Bytes(span Span) []byte {
	if span.Start < 0 || span.End < span.Start || span.End > len(d.source) {
		return nil
	}
	return bytes.Clone(d.source[span.Start:span.End])
}
