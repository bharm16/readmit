package hl7

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Selector addresses one value. Segment occurrence and field repetition default
// to one; all positions in the text form are one-based. Use ParseSelector or
// NewSelector rather than constructing selectors: its private fields keep
// invalid paths out.
type Selector struct {
	segment                                                string
	occurrence, field, repetition, component, subcomponent int
}

// Parts are the positions one selector addresses. Occurrence, Field and
// Repetition are one-based. Component and Subcomponent are one-based, or zero
// when the selector addresses no component or no subcomponent.
type Parts struct {
	Segment                                                string
	Occurrence, Field, Repetition, Component, Subcomponent int
}

const segmentSyntax = `[A-Z][A-Z0-9]{2}`

var (
	selectorPattern = regexp.MustCompile(`^(` + segmentSyntax + `)(?:\[([1-9][0-9]*)\])?-([1-9][0-9]*)(?:\[([1-9][0-9]*)\])?(?:\.([1-9][0-9]*))?(?:\.([1-9][0-9]*))?$`)
	segmentPattern  = regexp.MustCompile(`^` + segmentSyntax + `$`)
	errSelector     = errors.New("invalid field selector; use SEG[occurrence]-field[repetition].component.subcomponent with positive positions")
)

func ParseSelector(path string) (Selector, error) {
	if len(path) > 96 {
		return Selector{}, errSelector
	}
	parts := selectorPattern.FindStringSubmatch(path)
	if parts == nil {
		return Selector{}, errSelector
	}
	p := Parts{Segment: parts[1], Occurrence: 1, Repetition: 1}
	positions := []*int{&p.Occurrence, &p.Field, &p.Repetition, &p.Component, &p.Subcomponent}
	for i, target := range positions {
		if parts[i+2] == "" {
			continue
		}
		n, err := strconv.Atoi(parts[i+2])
		if err != nil {
			return Selector{}, errSelector
		}
		*target = n
	}
	return NewSelector(p)
}

// NewSelector builds the selector at explicit positions. It refuses exactly
// what ParseSelector refuses, with the same fixed error, so a selector built
// from its own Parts, or parsed from its own String, is the same selector.
func NewSelector(p Parts) (Selector, error) {
	if !segmentPattern.MatchString(p.Segment) || p.Occurrence < 1 || p.Field < 1 || p.Repetition < 1 ||
		p.Component < 0 || p.Subcomponent < 0 || p.Component == 0 && p.Subcomponent != 0 ||
		max(p.Occurrence, p.Field, p.Repetition, p.Component, p.Subcomponent) > maxSyntaxNodes {
		return Selector{}, errSelector
	}
	return Selector{segment: p.Segment, occurrence: p.Occurrence, field: p.Field, repetition: p.Repetition, component: p.Component, subcomponent: p.Subcomponent}, nil
}

// Parts returns the positions the selector addresses. They are a copy, so a
// selector cannot be changed through them; build another with NewSelector.
func (s Selector) Parts() Parts {
	return Parts{Segment: s.segment, Occurrence: s.occurrence, Field: s.field, Repetition: s.repetition, Component: s.component, Subcomponent: s.subcomponent}
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
