package diagnose

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

type evaluator struct {
	profile     profileDefinition
	report      Report
	rules       map[string]bool
	namespaces  map[authority]string
	unsupported map[string]bool
}

type message struct {
	event   bundle.Event
	doc     *hl7.Document
	trigger string
	utf8    bool
}

type identity struct{ namespace, value string }

// Run opens and verifies the complete bundle before interpreting any metadata or
// payload. Reports contain field references and fixed explanations, not raw IDs,
// filenames, or free-text ERR content. Config is always validated at this seam.
func Run(path string, config Config) (Report, error) {
	if err := config.validate(); err != nil {
		return Report{}, err
	}
	profile, err := loadProfile()
	if err != nil {
		return Report{}, err
	}
	b, err := bundle.Open(path)
	if err != nil {
		return Report{}, err
	}
	e := evaluator{profile: profile, report: Report{Schema: Schema, CaseIdentity: b.Identity, Profile: config.Profile, Ruleset: config.Ruleset, Rules: []string{}, Findings: []Finding{}, Unsupported: []Unsupported{}, Scope: "Only the listed rules of the named readmit fixture profile were examined over the stated observed case window. This is not HL7 conformance validation and is not proof of correctness. The capture may omit earlier or later events; observed times, declared message times, and import times are distinct."}, rules: make(map[string]bool), namespaces: make(map[authority]string), unsupported: make(map[string]bool)}
	configJSON, err := json.Marshal(config, json.Deterministic(true))
	if err != nil {
		return Report{}, errors.New("cannot encode diagnosis configuration")
	}
	configHash := sha256.Sum256(configJSON)
	e.report.ConfigSHA256 = hex.EncodeToString(configHash[:])
	e.report.Window = caseWindow(b)
	for _, ns := range config.Namespaces {
		e.namespaces[authority{ns.Namespace, ns.UniversalID, ns.UniversalIDType}] = ns.Key
	}
	for _, rule := range config.Rules {
		if !slices.Contains(supportedRules, rule) {
			e.unsupportedItem("unsupported_rule", "", "", "A configured rule is unsupported: "+rule)
			continue
		}
		e.rules[rule] = true
	}
	profileSupported := config.Profile == Profile
	if !profileSupported {
		e.unsupportedItem("unsupported_profile", "", "", "The configured profile is unsupported: "+config.Profile)
	}
	if config.Ruleset != Ruleset {
		e.unsupportedItem("unsupported_ruleset", "", "", "The configured ruleset is unsupported: "+config.Ruleset)
	}
	if !profileSupported || config.Ruleset != Ruleset {
		clear(e.rules)
	}
	if generator := b.Manifest.Provenance.Generator; generator != nil && generator.ProfileVersion != Profile {
		e.unsupportedItem("unsupported_bundle_profile", "", "", "The bundle declares an unsupported generator profile; SIU profile rules were not evaluated.")
		profileSupported = false
		delete(e.rules, RequiredField)
		delete(e.rules, BookingNotObserved)
	}
	for _, rule := range supportedRules {
		if e.rules[rule] {
			e.report.Rules = append(e.report.Rules, rule)
		}
	}
	var messages []message
	controlIDs := make(map[string][]Evidence)
	controlOrder := []string{}
	for _, event := range b.Events {
		if event.Kind == bundle.Unparsed {
			e.unsupportedItem("unparsed_occurrence", event.ID, "", "The preserved occurrence cannot be parsed; no semantic rules were evaluated.")
			continue
		}
		raw, err := b.Raw(event.ID)
		if err != nil {
			return Report{}, err
		}
		doc, err := hl7.Parse(raw, hl7.Options{Terminator: event.Terminator})
		if err != nil {
			return Report{}, errors.New("verified occurrence could not be parsed")
		}
		m := message{event: event, doc: doc}
		charset := m.value("MSH-18")
		switch string(m.doc.Bytes(charset.Span)) {
		case "", "ASCII":
		case "UNICODE UTF-8":
			m.utf8 = true
		default:
			e.unsupportedItem("unsupported_character_set", event.ID, "MSH-18", "Only ASCII (the default) or declared UNICODE UTF-8 values are interpreted.")
			continue
		}
		if !e.singleRepetition(m, "MSH-9") || !e.singleRepetition(m, "MSH-12") || !e.singleRepetition(m, "MSH-18") {
			continue
		}
		if e.rules[DuplicateControl] && e.singleRepetition(m, "MSH-10") {
			if id, ok := e.text(m, "MSH-10"); ok && id != "" {
				if _, exists := controlIDs[id]; !exists {
					controlOrder = append(controlOrder, id)
				}
				controlIDs[id] = append(controlIDs[id], m.evidence("MSH-10"))
			}
		}
		kind, kindOK := e.text(m, "MSH-9.1")
		version, versionOK := e.text(m, "MSH-12")
		if !versionOK || version != e.profile.HL7Version {
			e.unsupportedItem("unsupported_hl7_version", event.ID, "MSH-12", "Only the named HL7 2.5.1 fixture profile is supported.")
			continue
		}
		if kindOK && kind == "ACK" {
			e.ack(m)
			continue
		}
		trigger, triggerOK := e.text(m, "MSH-9.2")
		_, supportedTrigger := e.profile.trigger(trigger)
		if !kindOK || kind != "SIU" || !triggerOK || !supportedTrigger {
			e.unsupportedItem("unsupported_message_type", event.ID, "MSH-9", "Supported message types are SIU S12, S13, S15 and ACK in the fixture profile.")
			continue
		}
		m.trigger = trigger
		if !profileSupported {
			continue
		}
		if m.segmentCount("SCH") > 1 || m.segmentCount("PID") > 1 {
			e.unsupportedItem("unsupported_segment_cardinality", event.ID, "", "The fixture profile supports exactly one SCH and one PID segment.")
			continue
		}
		e.required(m)
		if !e.singleRepetition(m, "SCH-1") || !e.singleRepetition(m, "SCH-2") {
			continue
		}
		messages = append(messages, m)
	}
	if e.rules[DuplicateControl] {
		for _, id := range controlOrder {
			if refs := controlIDs[id]; len(refs) > 1 {
				e.finding(DuplicateControl, "observed_fact", fmt.Sprintf("The same decoded MSH-10 occurs %d times in this case; this alone does not establish a retransmission or error.", len(refs)), "", refs...)
			}
		}
	}
	if e.rules[BookingNotObserved] && profileSupported {
		e.bookings(messages)
	}
	if len(e.report.Findings) == 0 {
		e.report.NoFindings = fmt.Sprintf("No findings were produced by the listed rules in ruleset %s for profile %s over %s This is not proof of correctness; unsupported items were not evaluated.", e.report.Ruleset, e.report.Profile, e.report.Window.Description)
	}
	return e.report, nil
}

func caseWindow(b *bundle.Bundle) Window {
	w := Window{Occurrences: len(b.Events), Sources: []SourceWindow{}}
	for _, source := range b.Manifest.Sources {
		w.Sources = append(w.Sources, SourceWindow{SourceID: source.ID, FirstOccurrence: fmt.Sprintf("%s-e%06d", source.ID, 1), LastOccurrence: fmt.Sprintf("%s-e%06d", source.ID, source.Occurrences)})
	}
	for _, event := range b.Events {
		if event.ObservedAt == nil {
			w.UnknownObservedTimes++
			continue
		}
		t := *event.ObservedAt
		if w.ObservedStart == nil || t.Before(*w.ObservedStart) {
			start := t
			w.ObservedStart = &start
		}
		if w.ObservedEnd == nil || t.After(*w.ObservedEnd) {
			end := t
			w.ObservedEnd = &end
		}
	}
	w.Description = fmt.Sprintf("the complete verified bundle (%d occurrences; %d with unknown observed time).", w.Occurrences, w.UnknownObservedTimes)
	if w.ObservedStart != nil {
		w.Description += fmt.Sprintf(" Known observed times span %s through %s; unknown times may lie outside those bounds.", w.ObservedStart.Format(time.RFC3339Nano), w.ObservedEnd.Format(time.RFC3339Nano))
	}
	return w
}

func (m message) value(path string) hl7.Value {
	s, err := hl7.ParseSelector(path)
	if err != nil {
		panic("invalid built-in diagnosis selector")
	}
	v, err := m.doc.Select(0, s)
	if err != nil {
		panic("invalid built-in diagnosis message index")
	}
	return v
}

func (m message) evidence(path string) Evidence {
	v := m.value(path)
	e := Evidence{Occurrence: m.event.ID, Field: path, State: v.State}
	if v.State != hl7.Omitted {
		offset, length := v.Span.Start, v.Span.End-v.Span.Start
		e.Offset = &offset
		e.Length = &length
	}
	return e
}

func (m message) segmentCount(id string) int {
	count := 0
	for _, segment := range m.doc.Messages[0].Segments {
		if segment.ID == id {
			count++
		}
	}
	return count
}

func (e *evaluator) text(m message, path string) (string, bool) {
	v := m.value(path)
	if v.State != hl7.Present {
		return "", false
	}
	decoded, err := hl7.Decode(m.doc.Bytes(v.Span), m.doc.Messages[0].Delimiters)
	if err != nil || !utf8.Valid(decoded) || !m.utf8 && bytes.IndexFunc(decoded, func(r rune) bool { return r > 127 }) >= 0 {
		e.unsupportedItem("unsupported_field_encoding", m.event.ID, path, "A field uses an unsupported escape, invalid UTF-8, or undeclared non-ASCII bytes; its value was not interpreted.")
		return "", false
	}
	return string(decoded), true
}

func (e *evaluator) unsupportedItem(code, occurrence, field, detail string) {
	key := code + "\x00" + occurrence + "\x00" + field + "\x00" + detail
	if e.unsupported[key] {
		return
	}
	e.unsupported[key] = true
	e.report.Unsupported = append(e.report.Unsupported, Unsupported{Code: code, Occurrence: occurrence, Field: field, Detail: detail})
}

func (e *evaluator) finding(rule, class, summary, window string, refs ...Evidence) {
	e.report.Findings = append(e.report.Findings, Finding{ID: fmt.Sprintf("f%06d", len(e.report.Findings)+1), RuleID: rule, Classification: class, Profile: e.report.Profile, Ruleset: e.report.Ruleset, Summary: summary, Window: window, Evidence: refs})
}

func (e *evaluator) required(m message) {
	if !e.rules[RequiredField] {
		return
	}
	trigger, _ := e.profile.trigger(m.trigger)
	paths := trigger.RequiredFields
	for _, path := range paths {
		v := m.value(path)
		if v.State != hl7.Present {
			e.finding(RequiredField, "profile_violation", fmt.Sprintf("Profile %s requires %s for SIU %s; the captured field is %s.", Profile, path, m.trigger, v.State), "", m.evidence(path))
			continue
		}
		if text, ok := e.text(m, path); ok && text == "" {
			e.finding(RequiredField, "profile_violation", fmt.Sprintf("Profile %s requires a nonempty %s for SIU %s.", Profile, path, m.trigger), "", m.evidence(path))
		}
	}
	for _, paths := range e.profile.AuthorityFields {
		e.authorityFor(m, paths, true)
	}
}

// authorityFor preserves the complete EI/HD authority tuple. A namespace mapping
// is an explicit customer assertion of equivalence; raw identifier strings never
// establish equivalence across different or unknown authorities.
func (e *evaluator) authorityFor(m message, paths []string, reportMissing bool) (string, bool) {
	parts := [3]string{}
	for i, path := range paths {
		v := m.value(path)
		if v.State == hl7.Null {
			if reportMissing {
				e.finding(RequiredField, "profile_violation", fmt.Sprintf("Profile %s requires an assigning authority; explicit null is not an authority.", Profile), "", m.evidence(path))
			}
			e.unsupportedItem("unknown_assigning_authority", m.event.ID, path, "Explicit-null assigning authority cannot be used for correlation.")
			return "", false
		}
		if v.State == hl7.Present {
			var ok bool
			parts[i], ok = e.text(m, path)
			if !ok {
				return "", false
			}
		}
	}
	if parts[0] == "" && parts[1] == "" || (parts[1] == "") != (parts[2] == "") {
		if reportMissing {
			e.finding(RequiredField, "profile_violation", fmt.Sprintf("Profile %s requires an assigning authority (namespace or universal identifier and type).", Profile), "", m.evidence(paths[0]), m.evidence(paths[1]), m.evidence(paths[2]))
		}
		e.unsupportedItem("unknown_assigning_authority", m.event.ID, paths[0], "The assigning authority is missing or incomplete; identifier correlation was not evaluated.")
		return "", false
	}
	key, ok := e.namespaces[authority{parts[0], parts[1], parts[2]}]
	if !ok {
		e.unsupportedItem("unconfigured_assigning_authority", m.event.ID, paths[0], "The assigning authority has no configured namespace mapping; identifier correlation was not evaluated.")
	}
	return key, ok
}

func (e *evaluator) filler(m message) (identity, bool) {
	value, ok := e.text(m, "SCH-2.1")
	if !ok || value == "" {
		return identity{}, false
	}
	ns, ok := e.authorityFor(m, []string{"SCH-2.2", "SCH-2.3", "SCH-2.4"}, false)
	return identity{ns, value}, ok
}

func (e *evaluator) bookings(messages []message) {
	bookings := make(map[identity]bool)
	keys := make(map[string]identity)
	for _, m := range messages {
		key, ok := e.filler(m)
		if !ok {
			continue
		}
		keys[m.event.ID] = key
		if m.trigger == "S12" {
			bookings[key] = true
		}
	}
	for _, m := range messages {
		if m.trigger == "S12" {
			continue
		}
		key, ok := keys[m.event.ID]
		if !ok || bookings[key] {
			continue
		}
		e.finding(BookingNotObserved, "hypothesis", "No corresponding S12 booking for this filler identifier in the same configured namespace was found in the observed case window. An earlier or uncaptured booking may exist.", e.report.Window.Description, m.evidence("MSH-9.2"), m.evidence("SCH-2.1"), m.evidence("SCH-2.2"), m.evidence("SCH-2.3"), m.evidence("SCH-2.4"))
	}
}

func (e *evaluator) singleRepetition(m message, path string) bool {
	if m.value(path+"[2]").State == hl7.Omitted {
		return true
	}
	e.unsupportedItem("unsupported_field_repetition", m.event.ID, path, "The fixture profile interprets this field only when it has a single repetition.")
	return false
}
