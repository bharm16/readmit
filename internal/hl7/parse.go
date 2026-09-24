package hl7

import (
	"bytes"
	"fmt"
)

const MaxInputBytes = 16 << 20

// The shared budget includes segments, fields, and repetitions across all frames.
// It limits memory expansion even for tiny fields separated by millions of bytes.
const maxSyntaxNodes = 200000

type parseBudget int

func (b *parseBudget) take(offset int) error {
	if *b <= 0 {
		return invalid(offset, "syntax node limit exceeded (200000)")
	}
	*b--
	return nil
}

// Parse keeps a private copy of the evidence and indexes its syntax by byte span.
func Parse(source []byte, options Options) (*Document, error) {
	if len(source) > MaxInputBytes {
		return nil, fmt.Errorf("input exceeds 16 MiB limit")
	}
	budget := parseBudget(maxSyntaxNodes)
	format := options.Format
	if format == "" || format == "auto" {
		format = Raw
		if len(source) > 0 && source[0] == 0x0b {
			format = MLLP
		}
	}
	if format != Raw && format != MLLP {
		return nil, fmt.Errorf("format must be auto, raw, or mllp")
	}
	if options.Terminator != "" && options.Terminator != "auto" && options.Terminator != CR && options.Terminator != LF && options.Terminator != CRLF {
		return nil, fmt.Errorf("terminator must be auto, cr, lf, or crlf")
	}
	doc := &Document{Format: format, source: bytes.Clone(source), options: options}
	if format == Raw {
		message, err := parseMessage(doc.source, Span{0, len(source)}, options.Terminator, &budget)
		if err != nil {
			return nil, err
		}
		doc.Messages = []Message{message}
	} else {
		for start := 0; start < len(source); {
			if source[start] != 0x0b {
				return nil, invalid(start, "expected MLLP start block")
			}
			length := bytes.IndexByte(source[start+1:], 0x1c)
			if length < 0 {
				return nil, invalid(start, "truncated MLLP frame: missing end block")
			}
			end := start + 1 + length
			if end+1 >= len(source) || source[end+1] != '\r' {
				return nil, invalid(end, "truncated MLLP frame: end block must be followed by CR")
			}
			message, err := parseMessage(doc.source, Span{start + 1, end}, options.Terminator, &budget)
			if err != nil {
				return nil, err
			}
			doc.Messages = append(doc.Messages, message)
			start = end + 2
		}
		if len(doc.Messages) == 0 {
			return nil, invalid(0, "empty MLLP input")
		}
	}
	return doc, nil
}

func parseMessage(source []byte, span Span, declared Terminator, budget *parseBudget) (Message, error) {
	m := Message{Span: span}
	if span.End-span.Start < 8 || !bytes.HasPrefix(source[span.Start:span.End], []byte("MSH")) {
		return m, invalid(span.Start, "expected an MSH header")
	}
	terminator, delimiter, err := segmentTerminator(source, span, declared)
	if err != nil {
		return m, err
	}
	m.Terminator = terminator
	h := source[span.Start:span.End]
	encodingLength := bytes.IndexByte(h[4:min(len(h), 10)], h[3])
	if encodingLength < 2 || encodingLength > 5 {
		return m, invalid(span.Start+4, "MSH-2 must declare two to five encoding characters")
	}
	encodingEnd := 4 + encodingLength
	m.Delimiters = Delimiters{Field: h[3], Component: h[4], Repetition: h[5]}
	if encodingLength >= 3 {
		m.Delimiters.Escape = h[6]
	}
	if encodingLength >= 4 {
		m.Delimiters.Subcomponent = h[7]
	}
	if encodingLength == 5 {
		m.Delimiters.Truncation = h[8]
	}
	var seen [128]bool
	for i, c := range h[3:encodingEnd] {
		if c < 33 || c > 126 || c == '"' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || seen[c] {
			return m, invalid(span.Start+3+i, "delimiters must be distinct ASCII punctuation excluding double quote")
		}
		seen[c] = true
	}
	for start := span.Start; start < span.End; {
		if err := budget.take(start); err != nil {
			return m, err
		}
		length := bytes.Index(source[start:span.End], delimiter)
		if length < 0 {
			return m, invalid(start, "missing final segment terminator")
		}
		end := start + length
		if end-start < 3 || end-start > 3 && source[start+3] != m.Delimiters.Field {
			return m, invalid(start, "invalid segment header")
		}
		for i, c := range source[start : start+3] {
			if !(c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
				return m, invalid(start, "segment identifier must be three uppercase letters or digits, starting with a letter")
			}
		}
		segment := Segment{ID: string(source[start : start+3]), Span: Span{start, end}}
		if start != span.Start && segment.ID == "MSH" {
			return m, invalid(start, "ambiguous message boundary; supply one raw message per file or declare --format mllp with one message per frame")
		}
		fieldStart := start + 4
		if start == span.Start {
			if end-start < encodingEnd+1 {
				return m, invalid(start, "truncated MSH header")
			}
			for range 2 {
				if err := budget.take(start); err != nil {
					return m, err
				}
			}
			segment.Fields = append(segment.Fields,
				Field{Number: 1, Span: Span{start + 3, start + 4}, State: Present},
				Field{Number: 2, Span: Span{start + 4, start + encodingEnd}, State: Present})
			fieldStart = start + encodingEnd + 1
		}
		escaped := false
		for i := fieldStart; i <= end; i++ {
			if i < end {
				if source[i] < 32 || source[i] == 127 {
					return m, invalid(i, "unexpected control byte in segment")
				}
				if source[i] == m.Delimiters.Escape {
					escaped = !escaped
				}
			} else if escaped {
				return m, invalid(i, "unterminated escape sequence")
			}
			if i == end || source[i] == m.Delimiters.Field && !escaped {
				if err := budget.take(fieldStart); err != nil {
					return m, err
				}
				fieldSpan := Span{fieldStart, i}
				reps, err := repetitions(source, fieldSpan, m.Delimiters, budget)
				if err != nil {
					return m, err
				}
				segment.Fields = append(segment.Fields, Field{Number: len(segment.Fields) + 1, Span: fieldSpan, State: stateAt(source, fieldSpan), Repetitions: reps})
				fieldStart = i + 1
			}
		}
		m.Segments = append(m.Segments, segment)
		start = end + len(delimiter)
	}
	return m, nil
}

func stateAt(source []byte, span Span) State {
	if span.Start == span.End {
		return Empty
	}
	if bytes.Equal(source[span.Start:span.End], []byte(`""`)) {
		return Null
	}
	return Present
}

func repetitions(source []byte, span Span, delimiters Delimiters, budget *parseBudget) ([]Value, error) {
	var values []Value
	start, escaped := span.Start, false
	for i := span.Start; i <= span.End; i++ {
		if i < span.End && source[i] == delimiters.Escape {
			escaped = !escaped
		}
		if i == span.End || source[i] == delimiters.Repetition && !escaped {
			if err := budget.take(start); err != nil {
				return nil, err
			}
			valueSpan := Span{start, i}
			values = append(values, Value{Span: valueSpan, State: stateAt(source, valueSpan)})
			start = i + 1
		}
	}
	return values, nil
}

func segmentTerminator(source []byte, span Span, declared Terminator) (Terminator, []byte, error) {
	var detected Terminator
	for i := span.Start; i < span.End; i++ {
		var found Terminator
		switch source[i] {
		case '\r':
			found = CR
			if i+1 < span.End && source[i+1] == '\n' {
				found = CRLF
				i++
			}
		case '\n':
			found = LF
		default:
			continue
		}
		if detected != "" && detected != found {
			return "", nil, invalid(i, "ambiguous mixed segment terminators; declare --terminator and supply uniformly terminated input")
		}
		detected = found
	}
	if detected == "" {
		return "", nil, invalid(span.Start, "missing segment terminator; declare --terminator cr, lf, or crlf")
	}
	if declared != "" && declared != "auto" && declared != detected {
		return "", nil, invalid(span.Start, "segment terminator does not match --terminator")
	}
	delimiters := map[Terminator][]byte{CR: {'\r'}, LF: {'\n'}, CRLF: {'\r', '\n'}}
	return detected, delimiters[detected], nil
}

// Diagnostics expose offsets and fixed error classes, never payload or filenames.
func invalid(offset int, reason string) error {
	return fmt.Errorf("invalid input at byte %d: %s", offset, reason)
}
