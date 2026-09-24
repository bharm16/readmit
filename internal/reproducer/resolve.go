package reproducer

import (
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// standard is the only delimiter declaration a field edit is performed under.
// An occurrence declaring other delimiters is refused rather than rewritten
// against an assumption, exactly as `redact` refuses one.
var standard = hl7.Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}

// Resolve reports what one plan means over one verified case: which occurrences
// it retains and why, where every edit lands, and everything a dependency step
// reached and could not settle.
//
// It writes nothing. A preview and a build resolve identically, so what a
// person is shown before they build is what the build retains.
func Resolve(source *bundle.Bundle, plan Plan) (Resolution, error) {
	resolved, err := resolve(source, plan)
	if err != nil {
		return Resolution{}, err
	}
	return resolved.resolution, nil
}

// pending is one edit before its bytes are placed: the position it addresses in
// the parent occurrence and the bytes that replace it.
type pending struct {
	step     Step
	span     hl7.Span
	state    hl7.State
	value    []byte
	selector hl7.Selector
}

// resolved is a resolution plus the derived bytes it implies, so a build writes
// exactly the evidence the preview described instead of computing it twice.
type resolved struct {
	resolution Resolution
	retained   []string          // parent occurrence IDs, in evidence order
	payloads   map[string][]byte // parent occurrence ID to its derived bytes
}

type session struct {
	source   *bundle.Bundle
	events   map[string]*bundle.Event
	position map[string]int
	reasons  map[string]Retained
	edits    map[string][]pending
	unsolved []Unresolved
}

func resolve(source *bundle.Bundle, plan Plan) (*resolved, error) {
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	if plan.Case != source.Identity {
		return nil, errors.New("this plan was authored against different evidence than the case it was applied to")
	}
	s := &session{
		source:   source,
		events:   make(map[string]*bundle.Event, len(source.Events)),
		position: make(map[string]int, len(source.Events)),
		reasons:  make(map[string]Retained),
		edits:    make(map[string][]pending),
	}
	for i := range source.Events {
		s.events[source.Events[i].ID] = &source.Events[i]
		s.position[source.Events[i].ID] = i
	}
	for _, step := range plan.Steps {
		if err := s.apply(step); err != nil {
			return nil, err
		}
	}
	return s.finish()
}

func (s *session) apply(step Step) error {
	switch step.Operator {
	case SelectOccurrence:
		if s.events[step.Occurrence] == nil {
			return errors.New("a plan step names an occurrence this case does not hold")
		}
		if s.reasons[step.Occurrence].Reason == Selected {
			return errors.New("a plan selects each occurrence once")
		}
		s.reasons[step.Occurrence] = Retained{Parent: step.Occurrence, Reason: Selected}
	case DropOccurrence:
		if s.reasons[step.Occurrence].Reason == "" {
			return errors.New("a plan step drops an occurrence this reproducer does not retain")
		}
		// Dropping an occurrence drops what was said about it too. Nothing is
		// lost: a plan is replayed from its steps, so undoing the drop resolves
		// the edits again exactly as they were.
		delete(s.reasons, step.Occurrence)
		delete(s.edits, step.Occurrence)
	case IncludeAcknowledgements:
		s.includeAcknowledgements()
	case IncludePriorIdentity:
		if err := s.includePriorIdentity(step.Identity); err != nil {
			return err
		}
	case SetField, ClearField:
		return s.edit(step)
	}
	return nil
}

// includeAcknowledgements retains the counterpart of every retained occurrence
// that the case itself correlated. It adds nothing the evidence did not already
// record: an acknowledgement the case could not tie to exactly one message is
// reported as unsettled, never resolved to a candidate here.
func (s *session) includeAcknowledgements() {
	for {
		added := false
		for _, retained := range s.snapshot() {
			for _, link := range s.source.Correlations {
				switch {
				case link.Kind == bundle.Matched && link.ACKID == retained && len(link.MessageIDs) == 1:
					added = s.require(link.MessageIDs[0], Acknowledgement, retained) || added
				case link.Kind == bundle.Matched && slices.Contains(link.MessageIDs, retained):
					added = s.require(link.ACKID, Acknowledgement, retained) || added
				case link.Kind == bundle.AmbiguousACK && link.ACKID == retained:
					s.unsettled(retained, AmbiguousACK)
				case link.Kind == bundle.UnmatchedACK && link.ACKID == retained:
					s.unsettled(retained, UnmatchedACK)
				case link.Kind == bundle.Unacknowledged && slices.Contains(link.MessageIDs, retained):
					s.unsettled(retained, UnacknowledgedMsg)
				}
			}
		}
		if !added {
			return
		}
	}
}

// includePriorIdentity retains every earlier occurrence of the same source that
// declares the same identity as one already retained. The identity is the
// ordered tuple of declared fields an operator named, compared by state and
// decoded value, so the same string under a different assigning authority is a
// different identity. Nothing about a workflow is inferred: this is what the
// evidence records, which is that two occurrences say the same thing about the
// same subject and one came first.
func (s *session) includePriorIdentity(identity []string) error {
	selectors := make([]hl7.Selector, 0, len(identity))
	for _, declared := range identity {
		selector, err := hl7.ParseSelector(declared)
		if err != nil {
			return errors.New("a declared identity selector is not one this release addresses")
		}
		selectors = append(selectors, selector)
	}
	keys := make(map[string]string, len(s.source.Events))
	for i := range s.source.Events {
		if key, ok := s.identity(&s.source.Events[i], selectors); ok {
			keys[s.source.Events[i].ID] = key
		}
	}
	for _, retained := range s.snapshot() {
		event := s.events[retained]
		key, ok := keys[retained]
		if !ok {
			if event.Kind == bundle.Unparsed {
				s.unsettled(retained, Undecodable)
			} else {
				s.unsettled(retained, NoIdentity)
			}
			continue
		}
		for i := range s.source.Events {
			candidate := &s.source.Events[i]
			if candidate.SourceID == event.SourceID && candidate.Sequence < event.Sequence && keys[candidate.ID] == key {
				s.require(candidate.ID, PriorIdentity, retained)
			}
		}
	}
	return nil
}

// identity reports the ordered (state, decoded value) tuple one occurrence
// declares under the selected fields. An occurrence nothing decoded, one whose
// declared identity is present nowhere, and one whose identity bytes carry an
// escape this release does not resolve all declare no identity: unknown is not
// a match.
func (s *session) identity(event *bundle.Event, selectors []hl7.Selector) (string, bool) {
	if event.Kind == bundle.Unparsed {
		return "", false
	}
	raw, err := s.source.Raw(event.ID)
	if err != nil {
		return "", false
	}
	doc, err := hl7.Parse(raw, s.options(event))
	if err != nil {
		return "", false
	}
	tuple := make([]string, 0, 2*len(selectors))
	present := false
	for _, selector := range selectors {
		value, err := doc.Read(0, selector, hl7.IgnoreMSH18)
		if err != nil {
			return "", false
		}
		text := ""
		if value.State == hl7.Present {
			decoded, ok := value.Text()
			if !ok {
				return "", false
			}
			text, present = decoded, true
		}
		tuple = append(tuple, string(value.State), text)
	}
	if !present {
		return "", false
	}
	key, err := json.Marshal(tuple, json.Deterministic(true))
	if err != nil {
		return "", false
	}
	return string(key), true
}

// edit records one field change against a retained, decoded occurrence. Whether
// the position it addresses can be rewritten at all is settled in finish, where
// the bytes are read.
func (s *session) edit(step Step) error {
	if s.reasons[step.Occurrence].Reason == "" {
		return errors.New("an edit names an occurrence this reproducer does not retain")
	}
	if s.events[step.Occurrence].Kind == bundle.Unparsed {
		return errors.New("an occurrence nothing decoded has no field to edit; its bytes are retained exactly as they are")
	}
	for _, existing := range s.edits[step.Occurrence] {
		if existing.step.Selector == step.Selector {
			return errors.New("a plan edits each position of an occurrence once")
		}
	}
	s.edits[step.Occurrence] = append(s.edits[step.Occurrence], pending{step: step})
	return nil
}

// require retains one occurrence a retained one depends on, and reports whether
// that changed anything. An occurrence a person selected keeps that reason: a
// dependency never relabels a deliberate selection.
func (s *session) require(id, reason, required string) bool {
	if id == required || s.reasons[id].Reason != "" || s.events[id] == nil {
		return false
	}
	s.reasons[id] = Retained{Parent: id, Reason: reason, RequiredBy: required}
	return true
}

func (s *session) unsettled(occurrence, reason string) {
	unresolved := Unresolved{Occurrence: occurrence, Reason: reason}
	if !slices.Contains(s.unsolved, unresolved) {
		s.unsolved = append(s.unsolved, unresolved)
	}
}

// snapshot is the retained occurrences in evidence order, taken before a
// dependency step adds to them, so one step is one pass over what was retained
// when it ran rather than over what it is adding.
func (s *session) snapshot() []string {
	retained := make([]string, 0, len(s.reasons))
	for id := range s.reasons {
		retained = append(retained, id)
	}
	slices.SortFunc(retained, func(a, b string) int { return s.position[a] - s.position[b] })
	return retained
}

func (s *session) options(event *bundle.Event) hl7.Options {
	for _, source := range s.source.Manifest.Sources {
		if source.ID == event.SourceID {
			return hl7.Options{Format: source.Format, Terminator: event.Terminator}
		}
	}
	return hl7.Options{Terminator: event.Terminator}
}

// finish places every edit in the bytes it rewrites and reports the resolution.
// The derived bytes are produced here and reparsed: an edit that would leave
// syntax this release cannot read again is refused rather than written.
func (s *session) finish() (*resolved, error) {
	retained := s.snapshot()
	if len(retained) > MaxOccurrences {
		return nil, errors.New("a reproducer retains at most 256 occurrences")
	}
	unresolved := s.unsolved
	if unresolved == nil {
		unresolved = []Unresolved{}
	}
	result := &resolved{
		resolution: Resolution{Occurrences: []Retained{}, Edits: []Edit{}, Unresolved: unresolved},
		retained:   retained,
		payloads:   make(map[string][]byte, len(retained)),
	}
	for _, id := range retained {
		result.resolution.Occurrences = append(result.resolution.Occurrences, s.reasons[id])
		raw, err := s.source.Raw(id)
		if err != nil {
			return nil, errors.New("the bytes of a retained occurrence are unavailable")
		}
		edits, ok := s.edits[id]
		if !ok {
			result.payloads[id] = raw
			continue
		}
		derived, placed, err := s.rewrite(s.events[id], raw, edits)
		if err != nil {
			return nil, err
		}
		result.payloads[id] = derived
		result.resolution.Edits = append(result.resolution.Edits, placed...)
	}
	return result, nil
}

// rewrite splices one occurrence's edits into its bytes and reports where each
// one landed in the result. The original bytes are never changed: this produces
// a new occurrence beside them.
func (s *session) rewrite(event *bundle.Event, raw []byte, edits []pending) ([]byte, []Edit, error) {
	doc, err := hl7.Parse(raw, s.options(event))
	if err != nil {
		return nil, nil, errors.New("a retained occurrence could not be decoded consistently with its case")
	}
	if doc.Messages[0].Delimiters != standard {
		return nil, nil, errors.New("this release edits only the standard HL7 delimiter declaration")
	}
	for i := range edits {
		selector, err := hl7.ParseSelector(edits[i].step.Selector)
		if err != nil {
			return nil, nil, errors.New("an edited field selector is not one this release addresses")
		}
		// MSH-1 and MSH-2 declare the delimiters every other position is split
		// on. Rewriting one would restate the syntax of the message rather than
		// change a value in it.
		value, err := doc.Read(0, selector, hl7.IgnoreMSH18)
		if value.Literal {
			return nil, nil, errors.New("the delimiter declarations MSH-1 and MSH-2 are not editable")
		}
		if err != nil {
			return nil, nil, errors.New("an edited field selector is not one this release addresses")
		}
		if value.State == hl7.Omitted {
			return nil, nil, errors.New("this release edits a position the message declares; an omitted position carries no bytes to replace")
		}
		edits[i].selector, edits[i].span, edits[i].state = selector, value.Span, value.State
		if edits[i].step.Operator == SetField {
			edits[i].value = []byte(edits[i].step.Value)
		}
	}
	slices.SortFunc(edits, func(a, b pending) int {
		if a.span.Start != b.span.Start {
			return a.span.Start - b.span.Start
		}
		return a.span.End - b.span.End
	})
	var output []byte
	placed := make([]Edit, 0, len(edits))
	position, delta := 0, 0
	previous := hl7.Span{Start: -1, End: -1}
	for _, change := range edits {
		// Two selectors naming one position are the same edit written twice.
		// A field with a single component, and every position below an empty
		// or explicit-null ancestor, address the ancestor's own bytes: the
		// selectors differ and the position does not, so applying both would
		// splice two values where the message declares one.
		if change.span.Start < position || change.span == previous {
			return nil, nil, errors.New("two edits of one occurrence address the same or overlapping bytes")
		}
		previous = change.span
		output = append(output, raw[position:change.span.Start]...)
		output = append(output, change.value...)
		placed = append(placed, Edit{
			Parent: event.ID, Selector: change.selector.String(), Operator: change.step.Operator,
			State: change.state, Offset: change.span.Start + delta, Length: len(change.value),
		})
		delta += len(change.value) - (change.span.End - change.span.Start)
		position = change.span.End
	}
	output = append(output, raw[position:]...)
	if len(output) > bundle.MaxSourceBytes {
		return nil, nil, errors.New("an edited occurrence exceeds the 16 MiB occurrence limit")
	}
	if _, err := hl7.Parse(output, s.options(event)); err != nil {
		return nil, nil, errors.New("these edits produce syntax this release cannot read back")
	}
	return output, placed, nil
}
