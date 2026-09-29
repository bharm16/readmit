package casegen

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

// field is one field as the generator means it: present repetitions of
// components of subcomponents, each unescaped text, or an explicit empty or
// null field. literal is bytes written exactly as they stand, never escaped:
// only a declared invalid value is written that way.
type field struct {
	state   hl7.State
	reps    [][][]string
	literal string
}

// segment is one segment's fields by one-based position. MSH-1 and MSH-2 are
// never stored: the delimiters declare them.
type segment struct {
	id     string
	fields []*field
	// group is the scenario subject or record a repeated segment belongs to,
	// for the ledger; it is never written.
	group string
}

func newSegment(id string) *segment { return &segment{id: id} }

func (s *segment) set(position int, f *field) {
	for len(s.fields) < position {
		s.fields = append(s.fields, nil)
	}
	s.fields[position-1] = f
}

// clear removes a position; a trailing position leaves the segment shorter.
func (s *segment) clear(position int) {
	if position <= len(s.fields) {
		s.fields[position-1] = nil
	}
	for len(s.fields) > 0 && s.fields[len(s.fields)-1] == nil {
		s.fields = s.fields[:len(s.fields)-1]
	}
}

// plain is one present value of one component.
func plain(value string) *field {
	return &field{state: hl7.Present, reps: [][][]string{{{value}}}}
}

// components is one present repetition of the given components. Trailing empty
// components are not written.
func components(values ...string) *field {
	rep := make([][]string, len(values))
	for i, v := range values {
		rep[i] = []string{v}
	}
	return &field{state: hl7.Present, reps: [][][]string{rep}}
}

// repeated joins several one-repetition fields into repetitions of one field.
func repeated(fields ...*field) *field {
	f := &field{state: hl7.Present}
	for _, one := range fields {
		f.reps = append(f.reps, one.reps...)
	}
	return f
}

// delimiters are the five declared characters in MSH-1/MSH-2 order.
type delimiters struct{ field, component, repetition, escape, subcomponent byte }

func declared(d string) delimiters {
	return delimiters{field: d[0], component: d[1], repetition: d[2], escape: d[3], subcomponent: d[4]}
}

// escaped writes one text value with the standard escape sequences for every
// delimiter it holds, so the value reads back exactly.
func (d delimiters) escaped(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		var code byte
		switch c {
		case d.field:
			code = 'F'
		case d.component:
			code = 'S'
		case d.repetition:
			code = 'R'
		case d.escape:
			code = 'E'
		case d.subcomponent:
			code = 'T'
		}
		if code == 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte(d.escape)
		b.WriteByte(code)
		b.WriteByte(d.escape)
	}
	return b.String()
}

func (d delimiters) encodeField(f *field) string {
	switch {
	case f == nil:
		return ""
	case f.literal != "":
		return f.literal
	case f.state == hl7.Null:
		return `""`
	case f.state == hl7.Empty:
		return ""
	}
	reps := make([]string, len(f.reps))
	for r, rep := range f.reps {
		last := len(rep)
		for last > 0 && joinedEmpty(rep[last-1]) {
			last--
		}
		parts := make([]string, last)
		for k, component := range rep[:last] {
			subs := make([]string, len(component))
			for s, sub := range component {
				subs[s] = d.escaped(sub)
			}
			end := len(subs)
			for end > 1 && subs[end-1] == "" {
				end--
			}
			parts[k] = strings.Join(subs[:end], string(d.subcomponent))
		}
		reps[r] = strings.Join(parts, string(d.component))
	}
	return strings.Join(reps, string(d.repetition))
}

func joinedEmpty(component []string) bool {
	for _, s := range component {
		if s != "" {
			return false
		}
	}
	return true
}

// encode writes one message: every segment terminated by CR, MSH-1 and MSH-2
// from the delimiters, no trailing unset field. The result is UTF-8 text; the
// character set is applied by transcode.
func (d delimiters) encode(segments []*segment) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.id)
		start := 1
		if s.id == "MSH" {
			b.WriteByte(d.field)
			b.WriteByte(d.component)
			b.WriteByte(d.repetition)
			b.WriteByte(d.escape)
			b.WriteByte(d.subcomponent)
			start = 3
		}
		last := len(s.fields)
		for last >= start && s.fields[last-1] == nil {
			last--
		}
		for position := start; position <= last; position++ {
			b.WriteByte(d.field)
			b.WriteString(d.encodeField(s.fields[position-1]))
		}
		b.WriteByte('\r')
	}
	return b.String()
}

// transcode writes UTF-8 text in the declared character set, refusing text the
// set cannot represent rather than substituting for it.
func transcode(text, charset string) ([]byte, error) {
	switch charset {
	case UTF8:
		return []byte(text), nil
	case ASCII:
		for i := 0; i < len(text); i++ {
			if text[i] >= 0x80 {
				return nil, errors.New("row text cannot be represented in ASCII")
			}
		}
		return []byte(text), nil
	case Latin1:
		out := make([]byte, 0, len(text))
		for _, r := range text {
			if r > 0xff || r == utf8.RuneError {
				return nil, errors.New("row text cannot be represented in ISO-8859-1")
			}
			out = append(out, byte(r))
		}
		return out, nil
	}
	return nil, errors.New("unsupported character set")
}

// stamp writes one declared instant at the declared precision, in the declared
// zone offset with its offset suffix, or as UTC wall time with no suffix when
// the offset is declared none.
func stamp(at time.Time, precision, offset string) string {
	layout := "20060102150405"
	if precision == "minute" {
		layout = "200601021504"
	}
	if offset == "none" {
		return at.UTC().Format(layout)
	}
	loc, _ := zone(offset)
	return at.In(loc).Format(layout + "-0700")
}
