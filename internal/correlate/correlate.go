package correlate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/bharm16/readmit/internal/acklink"
	"github.com/bharm16/readmit/internal/authority"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// boundaryStatement is the honest boundary every report carries, in the words a
// reader of the report needs before acting on it.
const boundaryStatement = "Only the declared rules were applied, and only to the occurrences of this one verified case. A link records that a rule's declared key was equal inside its declared scope; equality of a control ID or a clinical identifier is not proof that two occurrences describe the same message or the same encounter, and no link asserts an order, a duration or a cause. Colliding identifiers were never merged. Occurrences listed as unsupported were not evaluated and did not pass."

// acknowledgedReference is one MSA-2 exactly as the case recorded it: its
// decoded state and, when present, the original field bytes.
type acknowledgedReference struct {
	state hl7.State
	value []byte
}

type occurrence struct {
	event bundle.Event
	ref   Reference
	doc   *hl7.Document
	// control is the present MSH-10 bytes, or nil when the field is not
	// present. A present control ID is compared byte for byte.
	control      []byte
	acknowledged []acknowledgedReference
}

// errUndecodable is one occurrence's answer that a field cannot be compared as
// text: an unsupported escape, or bytes that are not valid UTF-8 once decoded.
var errUndecodable = errors.New("field does not decode to UTF-8 text")

// decode reads the selected field through the shared hl7 read and answers
// with its state and its text. Nothing outside this method reaches into the
// parsed document. Correlation does not hold a value to the character set
// MSH-18 declares: any valid UTF-8 is comparable text.
func (o *occurrence) decode(selector hl7.Selector) (hl7.State, string, error) {
	reading, err := o.doc.Read(0, selector, hl7.IgnoreMSH18)
	if err != nil {
		return hl7.Omitted, "", nil
	}
	if reading.State != hl7.Present {
		return reading.State, "", nil
	}
	text, ok := reading.Text()
	if !ok {
		return reading.State, "", errUndecodable
	}
	return reading.State, text, nil
}

// field is one selector as a rule declared it, with the parsed form the engine
// selects by. The report echoes the declared spelling rather than the canonical
// one, so a position an operator wrote comes back the way they wrote it. A rule
// parses its selectors once, not once per occurrence.
type field struct {
	declared string
	selector hl7.Selector
}

// declaredField parses one selector the rules reader already accepted.
func declaredField(path string) field {
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		panic("correlation selector was not validated at the rules boundary")
	}
	return field{declared: path, selector: selector}
}

// grouping keeps insertion order beside the groups themselves, so a report
// reads in case order whatever the map iteration does.
type grouping struct {
	items map[string][]*occurrence
	order []string
}

func newGrouping() *grouping { return &grouping{items: make(map[string][]*occurrence)} }

func (g *grouping) add(key string, item *occurrence) {
	if _, exists := g.items[key]; !exists {
		g.order = append(g.order, key)
	}
	g.items[key] = append(g.items[key], item)
}

// each visits every group in insertion order, with the key it was grouped by.
func (g *grouping) each(visit func(key string, items []*occurrence)) {
	for _, key := range g.order {
		visit(key, g.items[key])
	}
}

type engine struct {
	items       []*occurrence
	sources     map[string]bool
	authorities *authority.Table
	report      Report
	seen        map[string]bool
}

// Run opens and verifies the complete case before applying any rule, and
// leaves it exactly as it found it. The returned report carries occurrence
// identifiers, source identifiers and field selectors; it carries no field
// bytes, no original path and no filename.
func Run(path string, rules Rules) (Report, error) {
	if err := rules.validate(); err != nil {
		return Report{}, err
	}
	b, err := bundle.Open(path)
	if err != nil {
		return Report{}, err
	}
	encoded, err := json.Marshal(rules, json.Deterministic(true))
	if err != nil {
		return Report{}, errors.New("cannot encode correlation rules")
	}
	sum := sha256.Sum256(encoded)
	e := &engine{
		sources:     make(map[string]bool, len(b.Manifest.Sources)),
		authorities: authority.NewTable(mappingsOf(rules.Authorities)),
		seen:        make(map[string]bool),
		report: Report{
			Schema: ReportSchema, CaseIdentity: b.Identity, RulesSHA256: hex.EncodeToString(sum[:]),
			SessionDeclared: b.Manifest.Provenance.SessionID != "",
			Rules:           []RuleReport{}, Links: []Link{}, Collisions: []Collision{}, Unsupported: []Unsupported{},
			Summary: Summary{Occurrences: len(b.Events)}, Scope: boundaryStatement,
		},
	}
	for _, source := range b.Manifest.Sources {
		e.sources[source.ID] = true
	}
	needsPayloads := slices.ContainsFunc(rules.Rules, func(rule Rule) bool { return rule.Operator == Identifier })
	if err := e.load(b, needsPayloads); err != nil {
		return Report{}, err
	}
	for _, rule := range rules.Rules {
		e.apply(rule)
	}
	return e.report, nil
}

// load reads every occurrence once. An occurrence the case preserved but could
// not parse takes part in no rule, and says so once rather than once per rule.
func (e *engine) load(b *bundle.Bundle, payloads bool) error {
	for _, event := range b.Events {
		item := &occurrence{event: event, ref: Reference{Occurrence: event.ID, SourceID: event.SourceID, Kind: string(event.Kind)}}
		if event.Kind == bundle.Unparsed || event.Fields == nil {
			e.unsupportedItem("unparsed_occurrence", "", event.ID, "", "The case preserved this occurrence without parsing it; no correlation rule was applied to it.")
			continue
		}
		if payloads {
			raw, err := b.Raw(event.ID)
			if err != nil {
				return err
			}
			doc, err := hl7.Parse(raw, hl7.Options{Terminator: event.Terminator})
			if err != nil {
				return errors.New("a verified occurrence could not be parsed")
			}
			item.doc = doc
		}
		item.control = b.Value(event.ID, event.Fields.ControlID)
		if event.Fields.ControlID.State != hl7.Present {
			item.control = nil
		}
		for _, field := range event.Fields.AcknowledgedControlIDs {
			item.acknowledged = append(item.acknowledged, acknowledgedReference{state: field.State, value: b.Value(event.ID, field)})
		}
		e.items = append(e.items, item)
	}
	return nil
}

// apply runs one rule and records it, applied or not: a rule whose scope this
// case cannot supply still appears in the report, saying so.
func (e *engine) apply(rule Rule) {
	report := e.evaluate(rule)
	report.Unlinked = report.Considered - report.Linked
	e.report.Rules = append(e.report.Rules, report)
}

func (e *engine) evaluate(rule Rule) RuleReport {
	report := RuleReport{ID: rule.ID, Operator: rule.Operator, Scope: rule.Scope, Sources: slices.Clone(rule.Sources)}
	switch rule.Scope {
	case SessionScope:
		if !e.report.SessionDeclared {
			e.unsupportedItem("no_declared_session", rule.ID, "", "", "The case declares no recorded session, so a session-scoped rule was not applied. An imported or generated case does not acquire a session.")
			return report
		}
	case DeclaredScope:
		known := false
		for _, source := range rule.Sources {
			if e.sources[source] {
				known = true
				continue
			}
			e.unsupportedItem("unknown_source", rule.ID, "", "", "A declared scope names source "+source+", which this case does not declare.")
		}
		if !known {
			return report
		}
	}
	report.Applied = true
	linked := make(map[string]bool)
	switch rule.Operator {
	case Acknowledges:
		e.acknowledges(rule, &report, linked)
	case ControlID:
		e.controlIDs(rule, &report, linked)
	case Identifier:
		e.identifiers(rule, &report, linked)
	}
	report.Linked = len(linked)
	return report
}

// acknowledges resolves what an acknowledgement itself declares. The match is
// over exact field bytes, including literal escapes, with no trimming, case
// folding or escape decoding, so a control ID means here exactly what it means
// in the case bundle's own same-source correlation. The scope an operator
// declared is passed to [acklink] explicitly: a source-scoped rule answers an
// acknowledgement inside the source that captured it alone, and a session or
// declared scope answers it across the case, narrowed to its listed sources.
func (e *engine) acknowledges(rule Rule, report *RuleReport, linked map[string]bool) {
	scope := acklink.CaseWide
	if rule.Scope == SourceScope {
		scope = acklink.PerSource
	}
	index := acklink.New[*occurrence](scope, acklink.FieldBytes)
	for _, item := range e.items {
		if !inside(rule, item) || item.event.Kind != bundle.Message || item.control == nil {
			continue
		}
		index.Add(item.ref.SourceID, acklink.Value{Bytes: item.control}, item)
		report.Considered++
	}
	for _, item := range e.items {
		if !inside(rule, item) || item.event.Kind != bundle.Acknowledgement {
			continue
		}
		switch {
		case len(item.acknowledged) == 0:
			e.unsupportedItem("no_declared_reference", rule.ID, item.ref.Occurrence, "MSA-2", "The acknowledgement declares no MSA-2 reference, so no initiating occurrence was resolved.")
			continue
		case len(item.acknowledged) > 1:
			e.unsupportedItem("multiple_declared_references", rule.ID, item.ref.Occurrence, "MSA-2", "The acknowledgement carries more than one MSA segment; no single initiating occurrence can be chosen.")
			continue
		case item.acknowledged[0].state != hl7.Present:
			e.unsupportedItem("unusable_declared_reference", rule.ID, item.ref.Occurrence, "MSA-2", "The acknowledged control ID is "+string(item.acknowledged[0].state)+", which references no occurrence.")
			continue
		}
		report.Considered++
		candidates := index.Answer(item.ref.SourceID, acklink.Value{Bytes: item.acknowledged[0].value})
		switch len(candidates) {
		case 0:
		case 1:
			e.link(rule, Observed, "", linked, item, candidates[0])
		default:
			e.collide(rule, AmbiguousAcknowledgement, &item.ref, candidates)
		}
	}
}

// inside reports whether one occurrence is within the boundary this rule
// compares across. Only a declared scope narrows the case to the sources it
// lists; a source or session scope decides its reach through the scope
// [acklink] applies.
func inside(rule Rule, item *occurrence) bool {
	if rule.Scope != DeclaredScope {
		return true
	}
	return slices.Contains(rule.Sources, item.ref.SourceID)
}

// controlIDs groups equal message control IDs inside one declared scope. A
// control ID is unique only to its sending application, so equality across two
// declared sources is a candidate observation of one message and is recorded
// as inferred; equality inside one source tells no observation from another
// and is recorded as a collision that merges nothing.
func (e *engine) controlIDs(rule Rule, report *RuleReport, linked map[string]bool) {
	groups := newGrouping()
	for _, item := range e.items {
		key, ok := e.scopeKey(rule, item)
		if !ok || item.control == nil {
			continue
		}
		groups.add(key+"\x00"+item.ref.Kind+"\x00"+string(item.control), item)
		report.Considered++
	}
	groups.each(func(_ string, group []*occurrence) {
		if len(group) < 2 {
			return
		}
		perSource := make(map[string]int, len(group))
		duplicated := false
		for _, item := range group {
			perSource[item.ref.SourceID]++
			duplicated = duplicated || perSource[item.ref.SourceID] > 1
		}
		if duplicated {
			e.collide(rule, DuplicateControlID, nil, group)
			return
		}
		e.link(rule, Inferred, "", linked, group...)
	})
}

// identifiers groups equal clinical identifiers under the same configured
// assigning authority. An identifier string alone establishes nothing: equal
// strings under different configured authorities stay in separate links, and
// equal strings whose authority is missing, explicitly null or unconfigured
// are recorded as a collision and are merged into nothing.
func (e *engine) identifiers(rule Rule, report *RuleReport, linked map[string]bool) {
	value := declaredField(rule.Value)
	var parts [authorityParts]field
	for i, selector := range rule.Authority {
		parts[i] = declaredField(selector)
	}
	groups, values := newGrouping(), newGrouping()
	resolved := make(map[*occurrence]string)
	unqualified := make(map[string]bool)
	for _, item := range e.items {
		key, ok := e.scopeKey(rule, item)
		if !ok || item.doc == nil {
			continue
		}
		text, ok := e.text(rule, item, value)
		if !ok || text == "" {
			continue
		}
		valueKey := key + "\x00" + text
		values.add(valueKey, item)
		authorityKey, ok := e.authorityOf(rule, item, parts)
		if !ok {
			unqualified[valueKey] = true
			continue
		}
		resolved[item] = authorityKey
		report.Considered++
		groups.add(valueKey+"\x00"+authorityKey, item)
	}
	groups.each(func(_ string, group []*occurrence) {
		if len(group) > 1 {
			e.link(rule, Inferred, resolved[group[0]], linked, group...)
		}
	})
	values.each(func(valueKey string, sharing []*occurrence) {
		if len(sharing) < 2 {
			return
		}
		// Equal strings nobody could qualify are not two records of one
		// patient and not one record either. They are a collision.
		if unqualified[valueKey] {
			e.collide(rule, UnqualifiedIdentifier, nil, sharing)
			return
		}
		authorities := make(map[string]bool, len(sharing))
		for _, item := range sharing {
			authorities[resolved[item]] = true
		}
		if len(authorities) > 1 {
			e.collide(rule, DistinctAuthorities, nil, sharing)
		}
	})
}

// scopeKey is the only place a scope boundary is decided. Nothing is compared
// across one, so equal bytes in two scopes never become one link.
func (e *engine) scopeKey(rule Rule, item *occurrence) (string, bool) {
	switch rule.Scope {
	case SourceScope:
		return "source\x00" + item.ref.SourceID, true
	case SessionScope:
		return "session", true
	default:
		if !slices.Contains(rule.Sources, item.ref.SourceID) {
			return "", false
		}
		return "declared\x00" + rule.ID, true
	}
}

// authorityOf preserves the complete assigning-authority tuple. A mapping is
// an explicit customer assertion of equivalence; an identifier string never
// establishes equivalence across different or unestablished authorities. The
// reading of the tuple, as decoded text without holding it to MSH-18, stays
// here; what a complete tuple is and when it resolves is the one rule
// internal/authority owns.
func (e *engine) authorityOf(rule Rule, item *occurrence, parts [authorityParts]field) (string, bool) {
	var tuple [authorityParts]string
	for i, part := range parts {
		state, text, err := item.decode(part.selector)
		if err != nil {
			e.undecodable(rule, item, part)
			return "", false
		}
		if state == hl7.Null {
			e.unsupportedItem(authority.UnknownCode, rule.ID, item.ref.Occurrence, part.declared, "An explicit-null assigning authority is not an authority; the identifier was not correlated.")
			return "", false
		}
		tuple[i] = text
	}
	read := authority.Parts{Namespace: tuple[0], UniversalID: tuple[1], UniversalIDType: tuple[2]}
	if !read.Complete() {
		e.unsupportedItem(authority.UnknownCode, rule.ID, item.ref.Occurrence, parts[0].declared, "The assigning authority is missing or incomplete; the identifier was not correlated.")
		return "", false
	}
	key, ok := e.authorities.Resolve(read)
	if !ok {
		e.unsupportedItem(authority.UnconfiguredCode, rule.ID, item.ref.Occurrence, parts[0].declared, "The assigning authority has no configured mapping; the identifier was not correlated.")
		return "", false
	}
	return key, true
}

// text is one present, decodable value of this occurrence, or nothing. It owns
// only what the engine adds to a decode: reporting what could not be read.
func (e *engine) text(rule Rule, item *occurrence, selected field) (string, bool) {
	state, text, err := item.decode(selected.selector)
	if err != nil {
		e.undecodable(rule, item, selected)
		return "", false
	}
	return text, state == hl7.Present
}

func (e *engine) undecodable(rule Rule, item *occurrence, selected field) {
	e.unsupportedItem("unsupported_field_encoding", rule.ID, item.ref.Occurrence, selected.declared, "The field uses an unsupported escape or decodes to bytes that are not valid UTF-8; it was not correlated.")
}

func (e *engine) link(rule Rule, linkage Linkage, authorityKey string, linked map[string]bool, items ...*occurrence) {
	refs := make([]Reference, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.ref)
		linked[item.ref.Occurrence] = true
	}
	e.report.Links = append(e.report.Links, Link{
		ID: fmt.Sprintf("l%06d", len(e.report.Links)+1), Rule: rule.ID, Operator: rule.Operator,
		Linkage: linkage, Authority: authorityKey, Occurrences: refs,
	})
	e.report.Summary.Links++
	if linkage == Observed {
		e.report.Summary.Observed++
		return
	}
	e.report.Summary.Inferred++
}

func (e *engine) collide(rule Rule, reason string, declaring *Reference, items []*occurrence) {
	refs := make([]Reference, 0, len(items))
	for _, item := range items {
		refs = append(refs, item.ref)
	}
	collision := Collision{Rule: rule.ID, Operator: rule.Operator, Reason: reason, Occurrences: refs}
	if declaring != nil {
		reference := *declaring
		collision.Declaring = &reference
	}
	e.report.Collisions = append(e.report.Collisions, collision)
	e.report.Summary.Collisions++
}

// unsupportedItem records one item once. The detail is part of the key because
// two items can share a code, a rule and an occurrence and still be separate
// facts: a declared scope naming two sources this case does not have reports
// both of them.
func (e *engine) unsupportedItem(code, rule, occurrenceID, field, detail string) {
	key := code + "\x00" + rule + "\x00" + occurrenceID + "\x00" + field + "\x00" + detail
	if e.seen[key] {
		return
	}
	e.seen[key] = true
	e.report.Unsupported = append(e.report.Unsupported, Unsupported{Code: code, Rule: rule, Occurrence: occurrenceID, Field: field, Detail: detail})
	e.report.Summary.Unsupported++
}
