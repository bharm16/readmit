package hl7

import (
	"errors"
	"fmt"
)

// SourceNodes projects the existing syntax tree over an original byte range.
// Ancestors precede descendants. Single, undelimited values remain fields;
// composite values retain the same canonical paths Navigate accepts. No
// display text (including masked or escaped text) is ever parsed here.
func (d *Document) SourceNodes(message int, span Span) ([]Node, error) {
	if message < 0 || message >= len(d.Messages) || span.Start < d.Messages[message].Span.Start || span.End > d.Messages[message].Span.End || span.End < span.Start {
		return nil, errors.New("source window is outside the message")
	}
	m := d.Messages[message]
	intersects := func(s Span) bool { return s.Start <= span.End && s.End >= span.Start }
	nodes := []Node{}
	counts := map[string]int{}
	budget := parseBudget(maxSyntaxNodes)
	for _, segment := range m.Segments {
		counts[segment.ID]++
		if !intersects(segment.Span) {
			continue
		}
		prefix := fmt.Sprintf("%s[%d]", segment.ID, counts[segment.ID])
		nodes = append(nodes, node(prefix, "segment", Value{Span: segment.Span, State: Present}))
		for _, field := range segment.Fields {
			if !intersects(field.Span) {
				continue
			}
			path := fmt.Sprintf("%s-%d", prefix, field.Number)
			nodes = append(nodes, node(path, "field", Value{Span: field.Span, State: field.State}))
			if segment.ID == "MSH" && field.Number <= 2 {
				continue
			}
			for r, rep := range field.Repetitions {
				if !intersects(rep.Span) {
					continue
				}
				repPath := fmt.Sprintf("%s[%d]", path, r+1)
				if len(field.Repetitions) > 1 {
					nodes = append(nodes, node(repPath, "repetition", rep))
				}
				if rep.State != Present {
					continue
				}
				delimiters := m.Delimiters
				delimiters.Repetition = delimiters.Component
				components, err := repetitions(d.source, rep.Span, delimiters, &budget)
				if err != nil {
					return nil, err
				}
				for c, component := range components {
					if !intersects(component.Span) {
						continue
					}
					componentPath := fmt.Sprintf("%s.%d", repPath, c+1)
					var subs []Value
					if component.State == Present {
						delimiters.Repetition = m.Delimiters.Subcomponent
						subs, err = repetitions(d.source, component.Span, delimiters, &budget)
						if err != nil {
							return nil, err
						}
					}
					if len(components) > 1 || len(subs) > 1 {
						nodes = append(nodes, node(componentPath, "component", component))
					}
					if len(subs) > 1 {
						for s, sub := range subs {
							if intersects(sub.Span) {
								nodes = append(nodes, node(fmt.Sprintf("%s.%d", componentPath, s+1), "subcomponent", sub))
							}
						}
					}
				}
			}
		}
	}
	return nodes, nil
}
