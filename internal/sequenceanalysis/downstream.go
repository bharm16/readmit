package sequenceanalysis

import (
	"errors"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
)

func explainDownstream(d Declaration, events map[string]bundle.Event, windows map[string]Window, links *correlate.Report, r *Report) error {
	for _, expected := range d.Downstream {
		e, ok := events[expected.Occurrence]
		w, covered := windows[expected.Source]
		if !ok || e.Kind != bundle.Message || e.SourceID == expected.Source || !covered || links == nil {
			return errors.New("downstream expectations require a known message, another windowed source and correlation rules")
		}
		applied := false
		for _, rule := range links.Rules {
			if rule.ID == expected.Rule {
				applied = rule.Applied && rule.Scope != correlate.SourceScope && rule.Operator != correlate.Acknowledges
				if rule.Scope == correlate.DeclaredScope {
					a, z := false, false
					for _, source := range rule.Sources {
						a = a || source == e.SourceID
						z = z || source == expected.Source
					}
					applied = applied && a && z
				}
			}
		}
		if !applied {
			return errors.New("downstream expectations require a named applied correlation rule")
		}
		kind := "unobserved_downstream_output"
		detail := "No output is linked by the declared rule inside the downstream observation window. Coverage is operator-declared; missing evidence is not proof of a dropped message or invalid downstream state."
		for _, u := range links.Unsupported {
			if (u.Rule == expected.Rule || u.Rule == "") && (u.Occurrence == "" || u.Occurrence == e.ID || events[u.Occurrence].SourceID == expected.Source) {
				kind = "downstream_unresolved"
				detail = "The declared rule cannot evaluate part of the requested evidence; absence cannot be assessed."
			}
		}
		for _, c := range links.Collisions {
			if c.Rule == expected.Rule {
				for _, candidate := range c.Occurrences {
					if candidate.Occurrence == e.ID {
						kind = "downstream_unresolved"
						detail = "The declared rule has an ambiguous input; no downstream conclusion is selected."
					}
				}
			}
		}
		for _, link := range links.Links {
			if link.Rule != expected.Rule {
				continue
			}
			contains := false
			for _, ref := range link.Occurrences {
				if ref.Occurrence == e.ID {
					contains = true
				}
			}
			if !contains {
				continue
			}
			for _, ref := range link.Occurrences {
				target := events[ref.Occurrence]
				if target.SourceID != expected.Source || target.Kind != bundle.Message {
					continue
				}
				if inside(target, w) {
					kind = "downstream_link_observed"
					detail = "A retained message in the declared downstream window is linked by the selected rule. This is correlation evidence, not proof of downstream processing."
				} else if kind != "downstream_link_observed" {
					kind = "downstream_unresolved"
					detail = "A linked target has unknown observation time or lies outside the declared downstream window."
				}
			}
		}
		r.Findings = append(r.Findings, Finding{Kind: kind, Occurrence: e.ID, Source: expected.Source, Detail: detail})
	}
	return nil
}
