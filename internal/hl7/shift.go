package hl7

import (
	"errors"
	"time"
)

// maxShift bounds a date shift either way: ten 365-day years.
const maxShift = 10 * 365 * 24 * time.Hour

// secondsLayout is a whole-second timestamp with no offset. A shift moves it
// and TimestampLayout, the same with a numeric offset.
const secondsLayout = "20060102150405"

// Refusals of a date shift. None of them carries a value.
var (
	ErrShiftDuration  = errors.New("a date shift is a nonzero whole-second duration within ten 365-day years")
	ErrShiftTimestamp = errors.New("a date shift moves only whole-second timestamps with an optional numeric offset")
	ErrShiftYear      = errors.New("a date shift moves a timestamp outside years 1 to 9999")
)

// ParseShift reads the one duration a date shift is held to: nonzero whole
// seconds, at most ten 365-day years either way. Every occurrence moves by the same
// explicit amount, so the intervals between them are unchanged.
func ParseShift(value string) (time.Duration, error) {
	shift, err := time.ParseDuration(value)
	if err != nil || shift == 0 || shift%time.Second != 0 || shift < -maxShift || shift > maxShift {
		return 0, ErrShiftDuration
	}
	return shift, nil
}

// ShiftTimestamps returns the edits that move one message's timestamps by one
// date shift, in position order: MSH-7, then both appointment endpoints,
// SCH-11.4 and SCH-11.5, of every SCH occurrence and every SCH-11 repetition.
// Only a present value moves; an empty, null or omitted one stays as it is, so
// an appointment's two endpoints move together and keep its duration. Every
// moved value must be a whole-second timestamp, with an optional numeric
// offset, that stays within years 1 to 9999, or the whole shift is refused.
//
// This is the one definition the transform preview and a replay both move by,
// so what a preview shows is what a replay sends. messageIndex is zero-based.
func (d *Document) ShiftTimestamps(messageIndex int, by time.Duration) ([]Edit, error) {
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return nil, errors.New("message index is out of range")
	}
	positions := []Parts{{Segment: "MSH", Occurrence: 1, Field: 7, Repetition: 1}}
	appointments := 0
	for _, segment := range d.Messages[messageIndex].Segments {
		if segment.ID != "SCH" {
			continue
		}
		appointments++
		for repetition := range segment.Field(11).Repetitions {
			for _, endpoint := range []int{4, 5} {
				positions = append(positions, Parts{Segment: "SCH", Occurrence: appointments, Field: 11, Repetition: repetition + 1, Component: endpoint})
			}
		}
	}
	edits := make([]Edit, 0, len(positions))
	for _, position := range positions {
		selector, err := NewSelector(position)
		if err != nil {
			return nil, err
		}
		value, err := d.Select(messageIndex, selector)
		if err != nil {
			return nil, err
		}
		if value.State != Present {
			continue
		}
		moved, err := shiftTimestamp(string(d.Bytes(value.Span)), by)
		if err != nil {
			return nil, err
		}
		edits = append(edits, Edit{Selector: selector, Value: []byte(moved)})
	}
	return edits, nil
}

func shiftTimestamp(value string, by time.Duration) (string, error) {
	layout := secondsLayout
	if len(value) == len(TimestampLayout) {
		layout = TimestampLayout
	}
	parsed, err := time.Parse(layout, value)
	if err != nil || parsed.Format(layout) != value {
		return "", ErrShiftTimestamp
	}
	moved := parsed.Add(by)
	if moved.Year() < 1 || moved.Year() > 9999 {
		return "", ErrShiftYear
	}
	return moved.Format(layout), nil
}
