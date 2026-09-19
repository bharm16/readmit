package diagnose

import (
	"fmt"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
)

// hypotheses fixes the prose of every correlation rule in Go. A profile document
// declares which identities a trigger establishes or depends on; it never
// supplies an explanation, and no summary claims an event did not happen outside
// the observed case window.
var hypotheses = map[string]string{
	BookingNotObserved:         "No corresponding S12 booking for this filler identifier in the same configured namespace was found in the observed case window. An earlier or uncaptured booking may exist.",
	AppointmentNotObserved:     "No occurrence booking this filler identifier in the same configured namespace was found in the observed case window. An earlier or uncaptured booking may exist, and equivalence comes only from the configured namespace mapping.",
	VisitNotObserved:           "No occurrence opening this visit identifier in the same configured namespace was found in the observed case window. An earlier or uncaptured registration or admission may exist, and equivalence comes only from the configured namespace mapping.",
	MergeIdentifierNotObserved: "No occurrence carrying this prior patient identifier as its first patient identifier repetition in the same configured namespace was found in the observed case window. Further identifier repetitions were not compared, an earlier or uncaptured identity may exist, and equivalence comes only from the configured namespace mapping.",
}

// eventType compares the segment-level event declaration with the trigger the
// header declares. Neither value is copied into the report; an absent or
// uninterpretable declaration belongs to the required-field rule instead.
func (e *evaluator) eventType(m message) {
	if !e.rules[EventTypeMismatch] {
		return
	}
	trigger, _ := e.profile.trigger(m.kind, m.trigger)
	if trigger.EventType == "" {
		return
	}
	v, supported := e.value(m, trigger.EventType)
	if !supported || v.State != hl7.Present {
		return
	}
	declared, ok := e.text(m, trigger.EventType)
	if !ok || declared == m.trigger {
		return
	}
	e.finding(EventTypeMismatch, "profile_violation", fmt.Sprintf("Profile %s requires %s of a %s occurrence to repeat its MSH-9.2 trigger; the captured event type differs. This reports a disagreement inside one occurrence, not which value is correct.", e.profile.Profile, trigger.EventType, m.kind), e.findingWindow, m.evidence("MSH-9.2"), m.evidence(trigger.EventType))
}

type subjectIdentity struct {
	rule string
	identity
}

type dependent struct {
	m    message
	decl correlation
	key  subjectIdentity
}

// correlate relates declared identities across the whole verified window.
// Antecedents are collected from every interpreted occurrence, so a finding
// never depends on capture order, declared times, or a transition model. An
// identity whose assigning authority has no configured mapping correlates with
// nothing and is reported as unsupported rather than assumed equivalent.
func (e *evaluator) correlate(messages []message) {
	observed := make(map[subjectIdentity]bool)
	var dependents []dependent
	for _, m := range messages {
		trigger, _ := e.profile.trigger(m.kind, m.trigger)
		for _, decl := range trigger.Correlations {
			if !e.rules[decl.Rule] {
				continue
			}
			key, ok := e.subject(m, decl)
			if !ok {
				continue
			}
			if decl.Role == antecedent {
				observed[key] = true
				continue
			}
			dependents = append(dependents, dependent{m, decl, key})
		}
	}
	for _, d := range dependents {
		if observed[d.key] {
			continue
		}
		refs := make([]Evidence, 0, len(d.decl.Evidence))
		for _, path := range d.decl.Evidence {
			refs = append(refs, d.m.evidence(path))
		}
		e.finding(d.decl.Rule, "hypothesis", hypotheses[d.decl.Rule], e.report.Window.Description, refs...)
	}
}

// subject decodes one declared identity. A field the profile reads only as its
// first repetition — PID-3 — can hold further identifiers that correlate with
// nothing, so that partial coverage is recorded rather than left implied.
func (e *evaluator) subject(m message, decl correlation) (subjectIdentity, bool) {
	field, _, _ := strings.Cut(decl.Value, ".")
	if m.value(field+"[2]").State != hl7.Omitted {
		e.unsupportedItem("partial_identifier_repetition", m.event.ID, field, "Only the first repetition of this identifier was compared; further repetitions were not correlated.")
	}
	value, ok := e.text(m, decl.Value)
	if !ok || value == "" {
		return subjectIdentity{}, false
	}
	ns, ok := e.authorityFor(m, decl.Authority, false)
	return subjectIdentity{decl.Rule, identity{ns, value}}, ok
}
