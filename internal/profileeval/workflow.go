package profileeval

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/hl7"
	"slices"
)

func (e *evaluator) workflow(ctx context.Context, inputs []Occurrence, options Options) error {
	if len(e.profile.Workflows) == 0 {
		return nil
	}
	e.report.WorkflowSupport = "evaluated"
	for _, w := range e.profile.Workflows {
		states := map[string]string{}
		for _, in := range inputs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			doc, err := hl7.Parse(in.Bytes, hl7.Options{})
			if err != nil {
				return err
			}
			e.doc = doc
			e.id = in.ID
			if string(e.read("MSH-12.1").Decoded) != e.profile.Definition.Base.HL7Version || string(e.read("MSH-9.1").Decoded) != e.profile.Definition.Base.Family {
				continue
			}
			count := 1
			if w.RepeatSegment != "" {
				count = 0
				for _, s := range doc.Messages[0].Segments {
					if s.ID == w.RepeatSegment {
						count++
					}
				}
			}
			if count == 0 {
				e.add(w.ID+":unobserved-subject", "workflow", "undecided", w.Status, hl7.Reading{})
				continue
			}
			ambiguous := false
			for _, path := range append(slices.Clone(w.Identity), w.Status) {
				selector, _ := hl7.ParseSelector(path)
				parts := selector.Parts()
				if parts.Segment == w.RepeatSegment {
					continue
				}
				n := 0
				for _, segment := range doc.Messages[0].Segments {
					if segment.ID == parts.Segment {
						n++
					}
				}
				if n > 1 {
					e.add(w.ID+":ambiguous-parent-binding", "workflow", "undecided", path, hl7.Reading{})
					ambiguous = true
					break
				}
			}
			if ambiguous {
				continue
			}
			for occurrence := 1; occurrence <= count; occurrence++ {
				if e.err != nil {
					return e.err
				}
				if e.overflow {
					return invalid
				}
				read := func(path string) hl7.Reading {
					s, _ := hl7.ParseSelector(path)
					parts := s.Parts()
					if parts.Segment == w.RepeatSegment {
						parts.Occurrence = occurrence
						s, _ = hl7.NewSelector(parts)
					}
					return e.read(s.String())
				}
				key := []string{w.ID, w.Version}
				valid := true
				for _, selector := range w.Identity {
					r := read(selector)
					if r.State != hl7.Present || r.Reason != "" {
						e.add(w.ID+":identity-unreadable", "workflow", "undecided", selector, r)
						valid = false
						break
					}
					key = append(key, string(r.Decoded))
				}
				if !valid {
					continue
				}
				status := read(w.Status)
				if status.State != hl7.Present || status.Reason != "" {
					e.add(w.ID+":status-unreadable", "workflow", "undecided", w.Status, status)
					continue
				}
				encoded, _ := json.Marshal(key)
				identity := string(encoded)
				current := string(status.Decoded)
				previous, seen := states[identity]
				if !seen && !slices.Contains(w.Initial, current) {
					outcome := "fail"
					if !options.CompleteCapture && slices.ContainsFunc(w.Initial, func(initial string) bool { return reachable(w.Transitions, initial, current) }) {
						outcome = "undecided"
					}
					e.add(w.ID+":unobserved-prerequisite", "workflow", outcome, w.Status, status)
				}
				if seen && !slices.ContainsFunc(w.Transitions, func(t Transition) bool { return t.From == previous && t.To == current }) {
					outcome := "fail"
					if !options.CompleteCapture && reachable(w.Transitions, previous, current) {
						outcome = "undecided"
					}
					e.add(w.ID+":transition", "workflow", outcome, w.Status, status)
				}
				states[identity] = current
			}
		}
	}
	return nil
}

func reachable(transitions []Transition, from, to string) bool {
	queue := []string{from}
	seen := map[string]bool{from: true}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for _, tr := range transitions {
			if tr.From != state {
				continue
			}
			if tr.To == to {
				return true
			}
			if !seen[tr.To] {
				seen[tr.To] = true
				queue = append(queue, tr.To)
			}
		}
	}
	return false
}
