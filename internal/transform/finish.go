package transform

import (
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
)

// finish resolves the whole plan over the evidence and reports it. Every
// position is placed, every placed occurrence is rewritten and read back, and
// only then is the preview assembled: what a person is shown is what the plan
// actually does, or the reason it does not do it.
func (e *engine) finish(plan Plan, pack profilepack.Pack) (Preview, error) {
	retained := e.retained()
	if len(retained) == 0 {
		return Preview{}, errors.New("this transformation drops every occurrence of the case; a sequence holds at least one")
	}
	if err := e.prepare(retained); err != nil {
		return Preview{}, err
	}
	placements, err := e.placed(retained)
	if err != nil {
		return Preview{}, err
	}
	derived := make(map[string][]byte, len(retained))
	for _, id := range retained {
		rewritten, err := e.rewrite(id, placements[id])
		if err != nil {
			return Preview{}, err
		}
		derived[id] = rewritten
	}
	// The relations and the declared support are resolved before the preview is
	// assembled, because both of them report what they could not settle.
	relations := e.preserved()
	profile := e.combinations(derived, pack)
	preview := Preview{
		Schema:      PreviewSchema,
		Case:        Artifact{Schema: e.source.Manifest.Schema, Identity: e.source.Identity},
		Plan:        plan,
		Sequence:    make([]Entry, 0, len(e.sequence)),
		Changes:     []Change{},
		Relations:   relations,
		Profile:     profile,
		Unsupported: e.notes,
		Scope:       boundaryStatement,
	}
	if preview.Unsupported == nil {
		preview.Unsupported = []Unsupported{}
	}
	for position, entry := range e.sequence {
		entry.Position = position + 1
		preview.Sequence = append(preview.Sequence, entry)
		for _, placed := range placements[entry.Parent] {
			preview.Changes = append(preview.Changes, Change{
				Entry: entry.ID, Parent: entry.Parent, Operator: placed.operator, Rule: placed.rule,
				Selector: placed.selector.String(), State: placed.state, Group: placed.group, Length: len(placed.value),
			})
		}
		if entry.Copy {
			preview.Summary.Copies++
		}
	}
	preview.Summary.Occurrences = len(retained)
	preview.Summary.Entries = len(preview.Sequence)
	preview.Summary.Changes = len(preview.Changes)
	preview.Summary.Relations = len(preview.Relations)
	preview.Summary.Unsupported = len(preview.Unsupported)
	for _, relation := range preview.Relations {
		if relation.Preserved {
			preview.Summary.Preserved++
		}
	}
	return preview, nil
}

// placed resolves every position the plan rewrites, per parent occurrence. A
// rename is assigned relation by relation before anything is written, so the
// surrogate an occurrence receives is decided by what it is related to rather
// than by where it happens to sit in the sequence.
func (e *engine) placed(retained []string) (map[string][]placement, error) {
	placements := make(map[string][]placement, len(retained))
	for _, ruleID := range e.renames {
		groups, selector, err := e.renamed(ruleID, retained)
		if err != nil {
			return nil, err
		}
		for _, id := range retained {
			if groups[id] == 0 {
				continue
			}
			place, err := e.place(id, selector, ruleID, groups[id])
			if err != nil {
				return nil, err
			}
			placements[id] = append(placements[id], place)
		}
		if e.rules[ruleID].Operator == correlate.ControlID {
			if err := e.acknowledged(groups, ruleID, retained, placements); err != nil {
				return nil, err
			}
		}
	}
	if e.shifting {
		e.note(Unsupported{Code: UnshiftedPositions, Selector: "MSH-7 and SCH-11.4/5",
			Detail: "a shift moves only the positions this release reads back as whole seconds; every other date and timestamp field is left exactly as it is"})
		for _, id := range retained {
			moved, err := e.shiftedTimes(id)
			if err != nil {
				return nil, err
			}
			placements[id] = append(placements[id], moved...)
		}
	}
	return placements, nil
}

// rewrite splices one occurrence's placements into its bytes and reads the
// result back. The case is never changed: this produces bytes beside it that
// exist only long enough to establish that the transformation is readable.
//
// Two placements addressing one position are refused, and so are two addressing
// overlapping bytes. A field with a single component, and every position below
// an empty or explicit-null ancestor, resolve to the ancestor's own span: the
// selectors differ and the position does not, so applying both would splice two
// values where the message declares one place to put them.
func (e *engine) rewrite(id string, placements []placement) ([]byte, error) {
	raw := e.raws[id]
	if len(placements) == 0 {
		return raw, nil
	}
	doc := e.docs[id]
	if doc == nil {
		return nil, errors.New("a transformation names an occurrence nothing decoded")
	}
	if doc.Messages[0].Delimiters != standard {
		return nil, errors.New("this release transforms only the standard HL7 delimiter declaration")
	}
	slices.SortStableFunc(placements, func(a, b placement) int {
		if a.span.Start != b.span.Start {
			return a.span.Start - b.span.Start
		}
		return a.span.End - b.span.End
	})
	var output []byte
	position := 0
	previous := hl7.Span{Start: -1, End: -1}
	for _, placed := range placements {
		if placed.span.Start < position || placed.span == previous {
			return nil, errors.New("two changes of one occurrence address the same or overlapping bytes")
		}
		previous = placed.span
		output = append(output, raw[position:placed.span.Start]...)
		output = append(output, placed.value...)
		position = placed.span.End
	}
	output = append(output, raw[position:]...)
	if len(output) > bundle.MaxSourceBytes {
		return nil, errors.New("a transformed occurrence exceeds the 16 MiB occurrence limit")
	}
	if _, err := hl7.Parse(output, e.options(e.events[id])); err != nil {
		return nil, errors.New("these changes produce syntax this release cannot read back")
	}
	return output, nil
}

// preserved reports what the edited sequence did to every relation the declared
// rules produced. A relation is preserved only when every occurrence of it is
// still in the sequence; one a drop broke is reported as broken rather than as
// the part of it that remains.
func (e *engine) preserved() []Relation {
	relations := make([]Relation, 0, len(e.report.Links))
	for _, link := range e.report.Links {
		relation := Relation{
			Rule: link.Rule, Operator: link.Operator, Linkage: link.Linkage,
			Occurrences: make([]string, 0, len(link.Occurrences)), Entries: []string{},
		}
		missing := 0
		for _, reference := range link.Occurrences {
			relation.Occurrences = append(relation.Occurrences, reference.Occurrence)
			found := false
			for _, entry := range e.sequence {
				if entry.Parent == reference.Occurrence {
					relation.Entries = append(relation.Entries, entry.ID)
					found = true
				}
			}
			if !found {
				missing++
			}
		}
		switch {
		case missing == 0:
			relation.Preserved = true
		case missing == len(link.Occurrences):
			relation.Reason = NotRetained
		default:
			relation.Reason = SeveredByDrop
		}
		relations = append(relations, relation)
	}
	return relations
}

// combinations reports what the transformed sequence declares about itself and
// what the pinned pack says about it, one row per distinct declared
// combination, in the order the sequence declares them.
//
// A plan that pins no pack is answered by a pack that declares nothing, so every
// outcome is unknown — and unknown never passes. An occurrence that declares no
// version or family, including one nothing decoded, is reported as the empty
// combination rather than left out: a sequence row is never silently omitted.
func (e *engine) combinations(derived map[string][]byte, pack profilepack.Pack) []Combination {
	declared := make(map[string]profilepack.Combination, len(derived))
	for id, raw := range derived {
		doc, err := hl7.Parse(raw, e.options(e.events[id]))
		if err != nil {
			declared[id] = profilepack.Combination{}
			continue
		}
		declared[id] = profilepack.Declared(doc, 0)
	}
	combinations := make([]Combination, 0, len(derived))
	for _, entry := range e.sequence {
		combination := declared[entry.Parent]
		at := slices.IndexFunc(combinations, func(item Combination) bool {
			return item.Version == combination.Version && item.Family == combination.Family
		})
		if at < 0 {
			combinations = append(combinations, Combination{
				Version: combination.Version, Family: combination.Family,
				Parse:      pack.Support(combination.Version, combination.Family, profilepack.LevelParse),
				Labels:     pack.Support(combination.Version, combination.Family, profilepack.LevelLabels),
				Structural: pack.Support(combination.Version, combination.Family, profilepack.LevelStructural),
				Workflow:   pack.Support(combination.Version, combination.Family, profilepack.LevelWorkflow),
			})
			at = len(combinations) - 1
		}
		combinations[at].Entries++
	}
	for _, combination := range combinations {
		if combination.Parse.Passing() {
			continue
		}
		e.note(Unsupported{Code: UnverifiedCombination,
			Detail: "the pinned pack declares " + named(combination.Version) + " " + named(combination.Family) +
				" " + string(combination.Parse) + " at the parse level, so nothing here verifies the transformed sequence against it"})
	}
	return combinations
}

// named is how a version or family an occurrence declares nothing for is
// written, so a row of the inventory is never blank.
func named(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}
