package hl7

import (
	"errors"
	"slices"
)

// standard is the one delimiter declaration a value a person wrote is checked
// against: a field |, component ^, repetition ~, escape \ and subcomponent &,
// and no truncation character.
var standard = Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}

// Standard reports whether these are the standard |^~\& delimiters with no
// truncation character.
func (d Delimiters) Standard() bool { return d == standard }

// DelimiterPolicy says which delimiter declarations a rewrite is performed
// under. Every rewrite names one; there is no default.
type DelimiterPolicy uint8

const (
	// StandardDelimiters rewrites only a message declaring the standard
	// delimiters. Callers whose values were written or checked against |^~\&
	// use it: rewriting under any other declaration would assume what its
	// separators mean.
	StandardDelimiters DelimiterPolicy = iota + 1
	// DeclaredDelimiters rewrites under whatever the message declares. Replay
	// uses it: it writes only READMIT surrogates, letters and digits that no
	// declaration can split because delimiters are punctuation, and shifted
	// timestamps punctuated exactly as the ones they replace.
	DeclaredDelimiters
)

// Refusals. A rewrite refuses with exactly one of these, or with an error
// about its own arguments; none of them carries a value.
var (
	ErrUnstandardDelimiters = errors.New("the message declares delimiters other than the standard |^~\\& this rewrite is performed under")
	ErrDelimiterDeclaration = errors.New("MSH-1 and MSH-2 declare the delimiters and are never rewritten")
	ErrOmittedPosition      = errors.New("an omitted position carries no bytes to rewrite")
	ErrOverlappingEdits     = errors.New("two edits address the same or overlapping bytes")
	ErrRewriteTooLarge      = errors.New("the rewritten message exceeds the 16 MiB input limit")
	ErrUnreadableRewrite    = errors.New("the rewritten message does not read back as HL7 syntax")
)

// Edit is one change a rewrite makes to one message: the value Selector
// addresses replaced by Value, an empty Value leaving the position empty. With
// RemoveSegment set, the edit instead removes the segment occurrence Selector
// is in, its terminator included, and Value must be empty.
type Edit struct {
	Selector      Selector
	Value         []byte
	RemoveSegment bool
}

// Placed is where one edit landed. Edit is its index in the edits a rewrite
// was given. State is the state of the position it replaced, Original the
// bytes it replaced in the source and Result the bytes it wrote in the
// rewritten document.
type Placed struct {
	Edit     int
	State    State
	Original Span
	Result   Span
}

// Rewritten is a rewritten document: its bytes, those bytes read back the way
// the source was parsed, and where every edit landed, in byte order.
type Rewritten struct {
	Bytes    []byte
	Document *Document
	Placed   []Placed
}

// Rewrite splices edits into one message and returns new bytes beside the
// source, which is never changed. messageIndex is zero-based, as for Select.
//
// Each edit is refused where it cannot mean one thing: under
// StandardDelimiters, a message declaring other delimiters; an edit of MSH-1
// or MSH-2, or removing MSH, which would restate the syntax every other
// position is split on; an omitted position, which carries no bytes to
// replace. Two edits are refused when they meet anywhere: overlapping bytes,
// the same position twice, or an empty position at or inside the bytes another
// edit replaces. A field with one component, and every position below an empty
// or null ancestor, resolve to the ancestor's own span, so two selectors can
// name one place to write. The result is refused past MaxInputBytes and
// unless it reads back.
func (d *Document) Rewrite(messageIndex int, edits []Edit, policy DelimiterPolicy) (Rewritten, error) {
	if policy != StandardDelimiters && policy != DeclaredDelimiters {
		return Rewritten{}, errors.New("delimiter policy is unset")
	}
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return Rewritten{}, errors.New("message index is out of range")
	}
	if policy == StandardDelimiters && !d.Messages[messageIndex].Delimiters.Standard() {
		return Rewritten{}, ErrUnstandardDelimiters
	}
	placed := make([]Placed, len(edits))
	for i, edit := range edits {
		span, state, err := d.locate(messageIndex, edit)
		if err != nil {
			return Rewritten{}, err
		}
		placed[i] = Placed{Edit: i, State: state, Original: span}
	}
	slices.SortStableFunc(placed, func(a, b Placed) int {
		if a.Original.Start != b.Original.Start {
			return a.Original.Start - b.Original.Start
		}
		return a.Original.End - b.Original.End
	})
	// In start order, an edit meets an earlier one when it starts before the
	// furthest earlier end, when it is empty and starts exactly there, or when
	// it starts where an earlier empty edit sits. Two non-empty edits that only
	// touch, such as two adjacent removed segments, do not meet.
	end, empty := 0, -1
	for i, edit := range placed {
		at := edit.Original
		if i > 0 && (at.Start < end || at.Start == end && at.Start == at.End || at.Start == empty) {
			return Rewritten{}, ErrOverlappingEdits
		}
		end = max(end, at.End)
		if at.Start == at.End {
			empty = at.Start
		}
	}
	output := make([]byte, 0, len(d.source))
	position := 0
	for i := range placed {
		at := placed[i].Original
		output = append(output, d.source[position:at.Start]...)
		start := len(output)
		output = append(output, edits[placed[i].Edit].Value...)
		placed[i].Result = Span{Start: start, End: len(output)}
		position = at.End
	}
	output = append(output, d.source[position:]...)
	if len(output) > MaxInputBytes {
		return Rewritten{}, ErrRewriteTooLarge
	}
	derived, err := Parse(output, d.options)
	if err != nil {
		return Rewritten{}, ErrUnreadableRewrite
	}
	return Rewritten{Bytes: output, Document: derived, Placed: placed}, nil
}

// locate resolves the bytes one edit replaces and the state they hold.
func (d *Document) locate(messageIndex int, edit Edit) (Span, State, error) {
	if !edit.RemoveSegment {
		value, err := d.Select(messageIndex, edit.Selector)
		if err != nil {
			return Span{}, "", err
		}
		// Select answers any part of MSH-1 and MSH-2 as omitted.
		if value.State != Omitted && edit.Selector.segment == "MSH" && edit.Selector.field <= 2 {
			return Span{}, "", ErrDelimiterDeclaration
		}
		if value.State == Omitted {
			return Span{}, "", ErrOmittedPosition
		}
		return value.Span, value.State, nil
	}
	if edit.Selector.segment == "" {
		return Span{}, "", errors.New("field selector is uninitialized")
	}
	if len(edit.Value) != 0 {
		return Span{}, "", errors.New("a segment removal writes no value")
	}
	m := d.Messages[messageIndex]
	count := 0
	for i, segment := range m.Segments {
		if segment.ID != edit.Selector.segment {
			continue
		}
		if count++; count != edit.Selector.occurrence {
			continue
		}
		if segment.ID == "MSH" {
			return Span{}, "", ErrDelimiterDeclaration
		}
		// Message.Span excludes MLLP framing, so framing is never removed.
		end := m.Span.End
		if i+1 < len(m.Segments) {
			end = m.Segments[i+1].Span.Start
		}
		return Span{Start: segment.Span.Start, End: end}, Present, nil
	}
	return Span{}, "", ErrOmittedPosition
}
