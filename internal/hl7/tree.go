package hl7

import (
	"errors"
	"fmt"
	"strings"
)

// Node locates one structural part in original document bytes. Path is a
// canonical selector, or SEG[n] for a segment; the empty path is the message.
// An omitted selection has no byte span. No node holds a copied value.
type Node struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Parent  string `json:"parent"`
	Segment string `json:"segment"`
	Field   int    `json:"field"`
	State   State  `json:"state"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

func node(path, kind string, value Value) Node {
	parent := ""
	switch kind {
	case "field":
		parent, _, _ = strings.Cut(path, "-")
	case "repetition":
		parent = path[:strings.LastIndex(path, "[")]
	case "component", "subcomponent":
		parent = path[:strings.LastIndex(path, ".")]
	}
	result := Node{Path: path, Kind: kind, Parent: parent, State: value.State, Start: value.Span.Start, End: value.Span.End}
	if len(path) >= 3 {
		result.Segment = path[:3]
	}
	if selector, err := ParseSelector(path); err == nil {
		result.Field = selector.field
	}
	return result
}

// Navigate returns a selection and its immediate children. Only the requested
// branch is expanded, using the same spans and escape rules as Select. Missing
// selectors remain explicit omitted selections, never fabricated empty fields.
func (d *Document) Navigate(messageIndex int, path string) (Node, []Node, error) {
	if messageIndex < 0 || messageIndex >= len(d.Messages) {
		return Node{}, nil, errors.New("message index is out of range")
	}
	m := d.Messages[messageIndex]
	children := []Node{}
	counts := map[string]int{}
	if path == "" {
		for _, s := range m.Segments {
			counts[s.ID]++
			children = append(children, node(fmt.Sprintf("%s[%d]", s.ID, counts[s.ID]), "segment", Value{Span: s.Span, State: Present}))
		}
		return node("", "message", Value{Span: m.Span, State: Present}), children, nil
	}
	for _, segment := range m.Segments {
		counts[segment.ID]++
		prefix := fmt.Sprintf("%s[%d]", segment.ID, counts[segment.ID])
		if path != prefix {
			continue
		}
		for _, field := range segment.Fields {
			children = append(children, node(fmt.Sprintf("%s-%d", prefix, field.Number), "field", Value{Span: field.Span, State: field.State}))
		}
		return node(prefix, "segment", Value{Span: segment.Span, State: Present}), children, nil
	}
	// A field selector can name a segment occurrence absent from the source.
	// Its parent remains an inspectable omitted position, so navigating upward
	// never turns a valid selection into a syntax error or fabricates a span.
	if !strings.Contains(path, "-") {
		selector, err := ParseSelector(path + "-1")
		if err != nil {
			return Node{}, nil, err
		}
		canonical := fmt.Sprintf("%s[%d]", selector.segment, selector.occurrence)
		if path != canonical {
			return Node{}, nil, errors.New("segment paths require an explicit occurrence")
		}
		return node(canonical, "segment", Value{State: Omitted}), children, nil
	}
	selector, err := ParseSelector(path)
	if err != nil {
		return Node{}, nil, err
	}
	value, err := d.Select(messageIndex, selector)
	if err != nil {
		return Node{}, nil, err
	}
	kind := "repetition"
	suffix := strings.SplitN(path, "-", 2)[1]
	if !strings.Contains(suffix, "[") && selector.component == 0 {
		kind = "field"
	}
	if selector.component > 0 {
		kind = "component"
	}
	if selector.subcomponent > 0 {
		kind = "subcomponent"
	}
	canonical := selector.String()
	if kind == "field" {
		canonical = fmt.Sprintf("%s[%d]-%d", selector.segment, selector.occurrence, selector.field)
	}
	if kind == "field" {
		occurrence := 0
		for _, segment := range m.Segments {
			if segment.ID != selector.segment {
				continue
			}
			occurrence++
			if occurrence != selector.occurrence {
				continue
			}
			f := segment.Field(selector.field)
			value = Value{Span: f.Span, State: f.State}
			for i, rep := range f.Repetitions {
				children = append(children, node(fmt.Sprintf("%s[%d]", canonical, i+1), "repetition", rep))
			}
			break
		}
	} else if value.State == Present && kind != "subcomponent" && !(selector.segment == "MSH" && selector.field <= 2) {
		delimiter, nextKind := m.Delimiters.Component, "component"
		if kind == "component" {
			delimiter, nextKind = m.Delimiters.Subcomponent, "subcomponent"
		}
		// Reuse the parser's escape-aware splitter. The parser bounds fields and
		// repetitions; this separate budget bounds expanded composite syntax too.
		delimiters := m.Delimiters
		delimiters.Repetition = delimiter
		budget := parseBudget(maxSyntaxNodes)
		parts, err := repetitions(d.source, value.Span, delimiters, &budget)
		if err != nil {
			return Node{}, nil, err
		}
		for i, part := range parts {
			children = append(children, node(fmt.Sprintf("%s.%d", canonical, i+1), nextKind, part))
		}
	}
	return node(canonical, kind, value), children, nil
}
