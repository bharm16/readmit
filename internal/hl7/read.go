package hl7

import (
	"errors"
	"unicode/utf8"
)

// Reason names why a present value's bytes are not text. A reading carries at
// most one, and a reason never carries a value.
type Reason string

const (
	// UnsupportedEscape is an escape other than the F, S, R, T and E
	// separators and X hexadecimal bytes, or a malformed one. Nothing is
	// decoded, stripped or guessed.
	UnsupportedEscape Reason = "unsupported_escape"
	// InvalidUTF8 is decoded bytes that are not valid UTF-8.
	InvalidUTF8 Reason = "invalid_utf8"
	// UndeclaredCharacterSet is, under EnforceMSH18, decoded bytes outside the
	// character set the message's MSH-18 declares, or any value of a message
	// whose MSH-18 declares a character set readmit does not read.
	UndeclaredCharacterSet Reason = "undeclared_character_set"
)

// CharacterSetPolicy says whether a read holds text to the character set its
// message's MSH-18 declares. Every read names one; there is no default.
type CharacterSetPolicy uint8

const (
	// IgnoreMSH18 reads any valid UTF-8 as text, whatever MSH-18 declares.
	IgnoreMSH18 CharacterSetPolicy = iota + 1
	// EnforceMSH18 reads text only in the character set MSH-18 declares:
	// ASCII by default, UTF-8 where MSH-18 declares UNICODE UTF-8. A message
	// declaring any other character set has no text readmit reads.
	EnforceMSH18
)

// CharacterSet is a character set a message's MSH-18 declares that readmit
// reads. Nothing is ever transcoded from another one.
type CharacterSet string

const (
	ASCII CharacterSet = "ASCII"
	UTF8  CharacterSet = "UNICODE UTF-8"
)

// Reading is one selected value read as text. Its State is the selector's
// four-state answer and only a present value is read. A present value is text
// when Reason is empty; otherwise Reason names the one reason it is not.
type Reading struct {
	Value
	// Literal marks MSH-1 and MSH-2: they declare the delimiters themselves,
	// escape character included, so they are read exactly as written and
	// never decoded.
	Literal bool
	// Decoded holds a present value's escape-resolved bytes, or the literal
	// bytes of MSH-1 and MSH-2, whenever the escapes resolved, even where
	// Reason says they are not text: a caller that compares or reports bytes
	// rather than text reads them here. It is nil otherwise, and a private copy.
	Decoded []byte
	Reason  Reason
}

// Text is the text of a present value that reads as text under its policy.
func (r Reading) Text() (string, bool) {
	if r.State != Present || r.Reason != "" {
		return "", false
	}
	return string(r.Decoded), true
}

// Read selects one value and reads it as text, so every caller turns selected
// bytes into text by one rule: the state first, MSH-1 and MSH-2 literally,
// the standard escapes, then valid UTF-8 and, under EnforceMSH18, the
// character set MSH-18 declares. There is no Unicode normalization, trimming,
// case folding or transcoding. messageIndex is zero-based, as for Select.
func (d *Document) Read(messageIndex int, s Selector, policy CharacterSetPolicy) (Reading, error) {
	value, err := d.Select(messageIndex, s)
	if err != nil {
		return Reading{}, err
	}
	return d.read(messageIndex, value, s.segment, s.field, policy)
}

// ReadNode reads one field, repetition, component or subcomponent Navigate
// returned, by the same rule as Read. A whole field reads with its repetition
// separators as written. A message or a segment is structure, not text.
func (d *Document) ReadNode(messageIndex int, n Node, policy CharacterSetPolicy) (Reading, error) {
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return Reading{}, errors.New("message index is out of range")
	}
	switch n.Kind {
	case "field", "repetition", "component", "subcomponent":
	default:
		return Reading{}, errors.New("only a field or a part of one is read as text")
	}
	value, message := Value{Span: Span{Start: n.Start, End: n.End}, State: n.State}, d.Messages[messageIndex].Span
	if value.State != Omitted && (value.Span.Start < message.Start || value.Span.End < value.Span.Start || value.Span.End > message.End) {
		return Reading{}, errors.New("the node is outside the message")
	}
	return d.read(messageIndex, value, n.Segment, n.Field, policy)
}

// read reads one value selected inside the named segment and field.
func (d *Document) read(messageIndex int, value Value, segment string, field int, policy CharacterSetPolicy) (Reading, error) {
	if policy != IgnoreMSH18 && policy != EnforceMSH18 {
		return Reading{}, errors.New("character-set policy is unset")
	}
	reading := Reading{Value: value}
	if value.State != Present {
		return reading, nil
	}
	raw := d.Bytes(value.Span)
	// MSH-1 and MSH-2 are present only as themselves: Select and Navigate
	// answer any part of them as omitted.
	if segment == "MSH" && field <= 2 {
		reading.Literal, reading.Decoded = true, raw
	} else if decoded, err := decode(raw, d.Messages[messageIndex].Delimiters); err != nil {
		reading.Reason = UnsupportedEscape
		return reading, nil
	} else {
		reading.Decoded = decoded
	}
	if policy == EnforceMSH18 {
		set, readable := d.CharacterSet(messageIndex)
		if !readable || set == ASCII && !ascii(reading.Decoded) {
			reading.Reason = UndeclaredCharacterSet
			return reading, nil
		}
	}
	if !utf8.Valid(reading.Decoded) {
		reading.Reason = InvalidUTF8
	}
	return reading, nil
}

// CharacterSet reports the character set one message's MSH-18 declares, read
// exactly as written and never decoded. An omitted or empty MSH-18 is the
// ASCII default. readable is false for any other declaration, including an
// explicit null or more than one repetition: readmit neither transcodes nor
// switches character sets. messageIndex is zero-based.
func (d *Document) CharacterSet(messageIndex int) (set CharacterSet, readable bool) {
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return "", false
	}
	field := d.Messages[messageIndex].Segments[0].Field(18)
	declared := CharacterSet(d.Bytes(field.Span))
	switch {
	case field.State == Omitted || field.State == Empty || declared == ASCII:
		return ASCII, true
	case declared == UTF8:
		return UTF8, true
	}
	return "", false
}

func ascii(value []byte) bool {
	for _, b := range value {
		if b >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
