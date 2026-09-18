package hl7

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Selector addresses one value. Segment occurrence and field repetition default
// to one; all positions in the text form are one-based. Use ParseSelector rather
// than constructing selectors: its private fields keep invalid paths out.
type Selector struct {
	segment                                                string
	occurrence, field, repetition, component, subcomponent int
}

var selectorPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{2})(?:\[([1-9][0-9]*)\])?-([1-9][0-9]*)(?:\[([1-9][0-9]*)\])?(?:\.([1-9][0-9]*))?(?:\.([1-9][0-9]*))?$`)

func ParseSelector(path string) (Selector, error) {
	invalid := errors.New("invalid field selector; use SEG[occurrence]-field[repetition].component.subcomponent with positive positions")
	if len(path) > 96 {
		return Selector{}, invalid
	}
	parts := selectorPattern.FindStringSubmatch(path)
	if parts == nil {
		return Selector{}, invalid
	}
	s := Selector{segment: parts[1], occurrence: 1, repetition: 1}
	positions := []*int{&s.occurrence, &s.field, &s.repetition, &s.component, &s.subcomponent}
	for i, target := range positions {
		if parts[i+2] == "" {
			continue
		}
		n, err := strconv.Atoi(parts[i+2])
		if err != nil || n > maxSyntaxNodes {
			return Selector{}, invalid
		}
		*target = n
	}
	return s, nil
}

// String returns the explicit canonical path; it never contains payload data.
func (s Selector) String() string {
	if s.segment == "" {
		return ""
	}
	path := fmt.Sprintf("%s[%d]-%d[%d]", s.segment, s.occurrence, s.field, s.repetition)
	if s.component != 0 {
		path += fmt.Sprintf(".%d", s.component)
	}
	if s.subcomponent != 0 {
		path += fmt.Sprintf(".%d", s.subcomponent)
	}
	return path
}

// Select returns a byte view in the document, never a reconstruction. A missing
// position is Omitted. Selecting below an explicitly empty/null ancestor retains
// that ancestor's state and span. messageIndex is zero-based, unlike path indices.
func (d *Document) Select(messageIndex int, s Selector) (Value, error) {
	if s.segment == "" {
		return Value{}, errors.New("field selector is uninitialized")
	}
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return Value{}, errors.New("message index is out of range")
	}
	m := d.Messages[messageIndex]
	count := 0
	var field Field
	for _, segment := range m.Segments {
		if segment.ID != s.segment {
			continue
		}
		count++
		if count == s.occurrence {
			field = segment.Field(s.field)
			break
		}
	}
	if count < s.occurrence || field.State == Omitted {
		return Value{State: Omitted}, nil
	}
	value := Value{Span: field.Span, State: field.State}
	// MSH-1/2 are literal delimiter declarations, not repeated composites.
	if s.segment == "MSH" && s.field <= 2 {
		if s.repetition != 1 || s.component != 0 || s.subcomponent != 0 {
			return Value{State: Omitted}, nil
		}
		return value, nil
	}
	if s.repetition > len(field.Repetitions) {
		return Value{State: Omitted}, nil
	}
	value = field.Repetitions[s.repetition-1]
	if s.component > 0 {
		value = d.selectPart(value, s.component, m.Delimiters.Component, m.Delimiters.Escape)
	}
	if s.subcomponent > 0 {
		value = d.selectPart(value, s.subcomponent, m.Delimiters.Subcomponent, m.Delimiters.Escape)
	}
	return value, nil
}

func (d *Document) selectPart(value Value, position int, separator, escape byte) Value {
	if value.State != Present {
		return value
	}
	if separator == 0 {
		if position == 1 {
			return value
		}
		return Value{State: Omitted}
	}
	start, count, escaped := value.Span.Start, 1, false
	for i := start; i <= value.Span.End; i++ {
		if i < value.Span.End && escape != 0 && d.source[i] == escape {
			escaped = !escaped
		}
		if i == value.Span.End || d.source[i] == separator && !escaped {
			if count == position {
				span := Span{Start: start, End: i}
				return Value{Span: span, State: stateAt(d.source, span)}
			}
			count++
			start = i + 1
		}
	}
	return Value{State: Omitted}
}
