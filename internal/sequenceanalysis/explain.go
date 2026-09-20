package sequenceanalysis

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
)

func inside(e bundle.Event, w Window) bool {
	return e.ObservedAt != nil && !e.ObservedAt.Before(w.Start) && !e.ObservedAt.After(w.End)
}
func explain(b *bundle.Bundle, d Declaration, windows map[string]Window, r *Report, links *correlate.Report) error {
	events := map[string]bundle.Event{}
	first := map[string]bundle.Event{}
	add := func(kind string, e bundle.Event, related, detail string) {
		r.Findings = append(r.Findings, Finding{Kind: kind, Occurrence: e.ID, Related: related, Source: e.SourceID, Detail: detail})
	}
	for _, e := range b.Events {
		events[e.ID] = e
		key := e.Payload.SHA256
		if prior, ok := first[key]; ok {
			a, _ := b.Raw(e.ID)
			z, _ := b.Raw(prior.ID)
			if bytes.Equal(a, z) {
				add("duplicate_occurrence", e, prior.ID, "Distinct retained occurrences contain identical bytes. Duplicate capture or retransmission remains unresolved without additional evidence.")
			}
		} else {
			first[key] = e
		}
		if e.Fields == nil || e.Fields.DeclaredTime.State != hl7.Present || e.ObservedAt == nil {
			add("clock_unknown", e, "", "Observed time or a readable message-declared instant is missing; import time is not substituted.")
			continue
		}
		declared := string(b.Value(e.ID, e.Fields.DeclaredTime))
		// Deliberately finite: only second precision with an explicit known offset.
		// Fractions, reduced precision and timezone-free DTM remain unknown here.
		if len(declared) != 19 || strings.HasSuffix(declared, "-0000") {
			add("clock_unknown", e, "", "Declared time has no supported second-precision instant with a known explicit offset; no timezone is inferred.")
			continue
		}
		at, err := time.Parse("20060102150405-0700", declared)
		if err != nil || at.Year() < 1 || declared[15:17] > "23" || declared[17:19] > "59" {
			add("clock_unknown", e, "", "Declared time is not a supported calendar instant.")
			continue
		}
		delta := at.Sub(*e.ObservedAt)
		if delta > time.Duration(d.ClockToleranceSeconds)*time.Second || delta < -time.Duration(d.ClockToleranceSeconds)*time.Second {
			add("clock_mismatch", e, "", "Observed and message-declared instants differ beyond the declared tolerance. Clock disagreement or transit delay is unresolved; no clock is corrected and no causality is inferred.")
		}
	}
	ackLinks := map[string][]bundle.Correlation{}
	for _, link := range b.Correlations {
		for _, id := range link.MessageIDs {
			ackLinks[id] = append(ackLinks[id], link)
		}
	}
	if links != nil {
		for _, link := range links.Links {
			if link.Operator != correlate.Acknowledges {
				continue
			}
			for _, message := range link.Occurrences {
				if events[message.Occurrence].Kind != bundle.Message {
					continue
				}
				for _, ack := range link.Occurrences {
					if events[ack.Occurrence].Kind == bundle.Acknowledgement {
						ackLinks[message.Occurrence] = append(ackLinks[message.Occurrence], bundle.Correlation{Kind: bundle.Matched, ACKID: ack.Occurrence})
					}
				}
			}
		}
		ackRules := map[string]bool{}
		for _, rule := range links.Rules {
			ackRules[rule.ID] = rule.Operator == correlate.Acknowledges
		}
		for _, collision := range links.Collisions {
			if !ackRules[collision.Rule] {
				continue
			}
			for _, candidate := range collision.Occurrences {
				if events[candidate.Occurrence].Kind == bundle.Message {
					ackLinks[candidate.Occurrence] = append(ackLinks[candidate.Occurrence], bundle.Correlation{Kind: bundle.AmbiguousACK})
				}
			}
		}
	}
	for _, e := range b.Events {
		if e.Kind != bundle.Message {
			continue
		}
		w, known := windows[e.SourceID]
		if !known || !inside(e, w) {
			add("ack_coverage_unknown", e, "", "Acknowledgement absence is not assessed without a containing declared observation window.")
			continue
		}
		found, uncertain := false, false
		for _, link := range ackLinks[e.ID] {
			if link.Kind == bundle.AmbiguousACK {
				uncertain = true
			}
			if link.Kind != bundle.Matched {
				continue
			}
			ack := events[link.ACKID]
			ackWindow, declared := windows[ack.SourceID]
			if !declared {
				uncertain = true
				continue
			}
			if inside(ack, ackWindow) {
				found = true
			} else if ack.ObservedAt == nil {
				uncertain = true
			}
		}
		if b.Collection != nil {
			for _, received := range b.Collection.Received {
				if received.OccurrenceID != e.ID {
					continue
				}
				add("accept_ack_stage", e, "", "Collection records accept-stage code "+received.Accept.Code+" and destination "+received.Accept.Destination+". A commit acceptance is not an application result.")
				add("application_ack_stage", e, "", "Collection records application-stage code "+received.Application.Code+" and destination "+received.Application.Destination+". A missing stage may be unrequested or unavailable; no downstream processing is proved.")
				uncertain = true
			}
		}
		if found {
			continue
		}
		if uncertain {
			add("ack_coverage_unknown", e, "", "Acknowledgement linkage, stage expectation or timing is unresolved; no missing required ACK is inferred.")
			continue
		}
		add("missing_ack", e, "", "No acknowledgement is linked inside this declared observation window. This is missing evidence, not proof none was sent; required enhanced ACK stages are not inferred.")
	}
	seen := map[string]bool{}
	for _, retry := range d.Retries {
		a, aok := events[retry.First]
		z, zok := events[retry.Retry]
		if !aok || !zok || a.ID == z.ID || retry.Basis != "operator_reported_retry" || seen[z.ID] {
			return errors.New("retry declarations require distinct known occurrences, one retry per occurrence and operator_reported_retry basis")
		}
		seen[z.ID] = true
		w, ok := windows[a.SourceID]
		if !ok || a.SourceID != z.SourceID || a.Kind != bundle.Message || z.Kind != bundle.Message || a.Direction == bundle.Unknown || a.Direction != z.Direction || !inside(a, w) || !inside(z, w) || !a.ObservedAt.Before(*z.ObservedAt) {
			add("retry_unresolved", z, a.ID, "The reported retry lacks same-source, direction and ordered observation evidence inside a declared window.")
			continue
		}
		x, _ := b.Raw(a.ID)
		y, _ := b.Raw(z.ID)
		if !bytes.Equal(x, y) {
			add("retry_unresolved", z, a.ID, "The operator-reported retry has different bytes; transformed retries are unsupported.")
			continue
		}
		add("likely_retransmission", z, a.ID, "Inference backed by an operator-reported retry and repeated same-source message bytes with increasing observed times and equal known direction. The declaration is not authenticated transport proof; a duplicate capture remains possible.")
	}
	return explainDownstream(d, events, windows, links, r)
}
