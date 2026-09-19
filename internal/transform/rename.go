package transform

import (
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
)

// renamed assigns one surrogate per relation the named rule produced, over the
// occurrences the sequence still holds.
//
// The relations are the correlation report's own: occurrences one link relates
// receive one surrogate, so the link survives the rename, and occurrences it
// related to nothing receive their own, so no link is invented. A control ID
// duplicated inside one source is a relation too — the case says the same
// identifier was observed twice — so both occurrences receive one surrogate and
// an intentional duplicate is still a duplicate afterwards.
//
// Equality the rules reported and could not qualify is not decided here: an
// identifier whose assigning authority is missing, explicitly null or
// unconfigured is left exactly as it is and reported, because renaming it apart
// would break a relation nobody established and renaming it together would
// assert one.
func (e *engine) renamed(ruleID string, retained []string) (map[string]int, hl7.Selector, error) {
	rule := e.rules[ruleID]
	path := controlIDSelector
	if rule.Operator == correlate.Identifier {
		path = rule.Value
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		return nil, hl7.Selector{}, errors.New("a renamed position is not one this release addresses")
	}
	related, refused := e.relations(ruleID)
	groups := make(map[string]int, len(retained))
	for _, id := range retained {
		if !inScope(rule, e.events[id]) {
			continue
		}
		if refused[id] {
			e.note(Unsupported{Code: UnqualifiedIdentifier, Parent: id, Rule: ruleID, Selector: selector.String(),
				Detail: "the declared rules found equal identifier bytes under no configured assigning authority; this position is left unchanged"})
			continue
		}
		if groups[id] != 0 {
			continue
		}
		if !e.declares(id, selector, ruleID) {
			continue
		}
		e.surrogates++
		groups[id] = e.surrogates
		for _, member := range related[id] {
			if member != id && slices.Contains(retained, member) && inScope(rule, e.events[member]) && e.declares(member, selector, ruleID) {
				groups[member] = e.surrogates
			}
		}
	}
	return groups, selector, nil
}

// relations reads the report once: which occurrences one rule related to each
// other, and which ones it reported equal bytes for and could not qualify.
func (e *engine) relations(ruleID string) (map[string][]string, map[string]bool) {
	related := make(map[string][]string)
	refused := make(map[string]bool)
	group := func(references []correlate.Reference) {
		members := make([]string, 0, len(references))
		for _, reference := range references {
			members = append(members, reference.Occurrence)
		}
		for _, member := range members {
			related[member] = members
		}
	}
	for _, link := range e.report.Links {
		if link.Rule == ruleID {
			group(link.Occurrences)
		}
	}
	for _, collision := range e.report.Collisions {
		if collision.Rule != ruleID {
			continue
		}
		switch collision.Reason {
		case correlate.DuplicateControlID:
			group(collision.Occurrences)
		case correlate.UnqualifiedIdentifier:
			for _, reference := range collision.Occurrences {
				refused[reference.Occurrence] = true
			}
		}
	}
	return related, refused
}

// declares reports whether one retained occurrence carries the position a rule
// reads as a present, decodable value. Unknown is not a match: an occurrence
// nothing decoded and one declaring nothing there are reported and left alone.
func (e *engine) declares(id string, selector hl7.Selector, ruleID string) bool {
	doc := e.docs[id]
	if doc == nil {
		e.note(Unsupported{Code: UndecodableOccurrence, Parent: id, Rule: ruleID,
			Detail: "nothing decoded this occurrence, so it declares no position to rename; its bytes stay as they are"})
		return false
	}
	value, err := doc.Select(0, selector)
	if err != nil || value.State != hl7.Present {
		e.note(Unsupported{Code: NoDeclaredValue, Parent: id, Rule: ruleID, Selector: selector.String(),
			Detail: "this occurrence declares nothing at the position the rule reads"})
		return false
	}
	decoded, err := hl7.Decode(doc.Bytes(value.Span), doc.Messages[0].Delimiters)
	if err != nil || !utf8.Valid(decoded) {
		e.note(Unsupported{Code: NoDeclaredValue, Parent: id, Rule: ruleID, Selector: selector.String(),
			Detail: "this position carries bytes this release does not decode as text, so no relation over it can be established"})
		return false
	}
	return true
}

// acknowledged repairs the references to every renamed message, so renaming a
// control ID cannot strand the acknowledgement that names it.
//
// The links are the case bundle's own: source-scoped and exact-byte, as the
// case contract defines them. A matched acknowledgement's reference is rewritten
// to the surrogate its message received. One the case could not tie to a single
// message **refuses** the rename rather than leaving it pointing at a control ID
// that is no longer anywhere; one naming no message of this case is left exactly
// as it is, because renaming the messages that are here cannot change what it
// refers to.
func (e *engine) acknowledged(groups map[string]int, ruleID string, retained []string, into map[string][]placement) error {
	selector, err := hl7.ParseSelector(acknowledgedSelector)
	if err != nil {
		return errors.New("a renamed position is not one this release addresses")
	}
	for _, link := range e.source.Correlations {
		if link.ACKID == "" || !slices.Contains(retained, link.ACKID) {
			continue
		}
		renamed := slices.ContainsFunc(link.MessageIDs, func(id string) bool { return groups[id] != 0 })
		switch {
		case link.Kind == bundle.Matched && len(link.MessageIDs) == 1 && renamed:
			place, err := e.place(link.ACKID, selector, ruleID, groups[link.MessageIDs[0]])
			if err != nil {
				return err
			}
			into[link.ACKID] = append(into[link.ACKID], place)
		case renamed:
			return errors.New("this case ties an acknowledgement of a renamed message to several candidate messages; renaming would strand it, so the rename is refused")
		case link.Kind == bundle.UnmatchedACK && len(groups) > 0:
			e.note(Unsupported{Code: UnmatchedAcknowledgement, Parent: link.ACKID, Rule: ruleID, Selector: selector.String(),
				Detail: "this acknowledgement names no message of this case, so no rename here changes what it refers to"})
		}
	}
	return nil
}

// place resolves one position of one occurrence and the surrogate that replaces
// it. A position the occurrence does not declare is refused rather than created:
// this release rewrites values a message already has.
func (e *engine) place(id string, selector hl7.Selector, ruleID string, group int) (placement, error) {
	doc := e.docs[id]
	if doc == nil {
		return placement{}, errors.New("a rename names an occurrence nothing decoded")
	}
	value, err := doc.Select(0, selector)
	if err != nil || value.State != hl7.Present {
		return placement{}, errors.New("a rename names a position this occurrence does not declare")
	}
	return placement{
		selector: selector, span: value.Span, state: value.State,
		value:    fmt.Appendf(nil, surrogateValueFormat, group),
		operator: RebaseIdentifiers, rule: ruleID, group: group,
	}, nil
}

// shifted moves every supported timestamp of one occurrence by the plan's one
// explicit duration. Every occurrence moves by the same amount, so the intervals
// between them — and the duration of an appointment, whose two endpoints move
// together — are exactly what they were.
//
// The supported positions are the ones `readmit replay --shift` already moves:
// MSH-7 and both appointment endpoints of every SCH occurrence and repetition,
// as whole seconds with an optional numeric offset. A timestamp outside that is
// refused rather than shifted into a form this release cannot read back.
func (e *engine) shiftedTimes(id string) ([]placement, error) {
	doc := e.docs[id]
	if doc == nil {
		e.note(Unsupported{Code: UndecodableOccurrence, Parent: id,
			Detail: "nothing decoded this occurrence, so it declares no timestamp to shift; its bytes stay as they are"})
		return nil, nil
	}
	paths := []string{"MSH-7"}
	appointments := 0
	for _, segment := range doc.Messages[0].Segments {
		if segment.ID != "SCH" {
			continue
		}
		appointments++
		for repetition := range segment.Field(11).Repetitions {
			paths = append(paths,
				fmt.Sprintf("SCH[%d]-11[%d].4", appointments, repetition+1),
				fmt.Sprintf("SCH[%d]-11[%d].5", appointments, repetition+1))
		}
	}
	placements := make([]placement, 0, len(paths))
	for _, path := range paths {
		selector, err := hl7.ParseSelector(path)
		if err != nil {
			return nil, errors.New("a shifted position is not one this release addresses")
		}
		value, err := doc.Select(0, selector)
		if err != nil || value.State != hl7.Present {
			continue
		}
		shifted, err := shift(string(doc.Bytes(value.Span)), e.shift)
		if err != nil {
			return nil, err
		}
		placements = append(placements, placement{
			selector: selector, span: value.Span, state: value.State,
			value: []byte(shifted), operator: ShiftDates,
		})
	}
	return placements, nil
}

func shift(value string, by time.Duration) (string, error) {
	layout := timestampLayout
	if len(value) == len(timestampOffsetLayout) {
		layout = timestampOffsetLayout
	}
	parsed, err := time.Parse(layout, value)
	if err != nil || parsed.Format(layout) != value {
		return "", errors.New("a date shift moves whole-second MSH-7 and SCH-11.4/5 timestamps with an optional numeric offset; this occurrence declares one it cannot move")
	}
	moved := parsed.Add(by)
	if moved.Year() < 1 || moved.Year() > 9999 {
		return "", errors.New("this date shift moves a timestamp outside the supported year range")
	}
	return moved.Format(layout), nil
}
