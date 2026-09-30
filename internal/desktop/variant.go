package desktop

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/transform"
)

// The variant editor (#558). A variant is one plan over one registered case or
// revision: the reproducer plan chooses the messages it includes and edits
// their fields, and the sequence stage — renaming related identifiers,
// shifting dates, moving, duplicating and excluding entries — is the
// transformation plan applied to what the reproducer retained. Both engines
// run exactly as a build runs them, so what the editor shows is what Save
// writes; nothing here writes into the project.

// VariantTransform is the sequence stage of a variant: the link rules whose
// relations it keeps (none names the acknowledgement relation alone), the
// profile its result is checked against, and the ordered steps.
type VariantTransform struct {
	Rules   *ItemRef              `json:"rules,omitzero"`
	Profile *ItemRef              `json:"profile,omitzero"`
	Steps   []VariantSequenceStep `json:"steps"`
}

// VariantSequenceStep is one transformation step and the source message the
// entry it names holds. The entry is the engine's own name for a place in the
// sequence; the message pins it, so a step never silently lands on another
// message after the included messages change.
type VariantSequenceStep struct {
	Step       transform.Step `json:"step"`
	Occurrence string         `json:"occurrence,omitzero"`
}

// acknowledgementRules are the relations a variant keeps when it names no link
// rules: each acknowledgement and the message it acknowledges.
var acknowledgementRules = correlate.Rules{Schema: correlate.RulesSchema, Rules: []correlate.Rule{
	{ID: "acknowledgements", Operator: correlate.Acknowledges, Scope: correlate.SourceScope},
}}

// variantResolved is one variant draft resolved over its source, as a build
// resolves it.
type variantResolved struct {
	fhir       *fhirVariantResolved
	source     variantSource
	plan       reproducer.Plan
	resolution reproducer.Resolution
	sequence   *variantSequence
}

// variantSequence is the sequence stage resolved over what the reproducer
// retained: the transformation plan bound to that intermediate case, the
// rules and pack it was applied with, and what it did.
type variantSequence struct {
	plan      transform.Plan
	rules     correlate.Rules
	rulesName string
	pack      *profilepack.Pack
	preview   transform.Preview
	// parents maps an occurrence of the intermediate case to the source
	// message it came from; entries maps every entry name, copies included.
	parents map[string]string
	entries map[string]string
	// before and after are the intermediate case and, when values were
	// asked for, the case the sequence stage writes.
	before *bundle.Bundle
	after  *bundle.Bundle
}

func variantProblem(field, problem string) []FieldProblem {
	return []FieldProblem{{Field: field, Problem: problem}}
}

// resolveVariant resolves a whole draft: the source it names, the reproducer
// plan and then the sequence stage over what the plan retains. written asks
// for the case the sequence stage produces, which only values need.
func resolveVariant(scope draftScope, draft VariantDraft, written bool) (*variantResolved, []FieldProblem) {
	source, problem := resolveVariantSource(scope, draft.Source)
	if problem != nil {
		return nil, []FieldProblem{*problem}
	}
	if source.fhir != nil || draft.FHIR != nil {
		return resolveFHIRVariant(source, draft)
	}
	if draft.Plan.Case != source.bundle.Identity {
		return nil, variantProblem("variant.plan", "the plan was made for different evidence than the source it names")
	}
	// The plan is read as the document it is saved as, so a step the reader
	// refuses is refused here and every selector is written canonically.
	document, err := json.Marshal(draft.Plan, json.Deterministic(true))
	if err != nil {
		return nil, variantProblem("variant.plan", "the plan cannot be read as a reproducer plan document")
	}
	plan, err := reproducer.DecodePlan(document)
	if err != nil {
		return nil, variantProblem("variant.plan", err.Error())
	}
	resolution, err := reproducer.Resolve(source.bundle, plan)
	if err != nil {
		return nil, variantProblem("variant.plan", err.Error())
	}
	resolved := &variantResolved{source: source, plan: plan, resolution: resolution}
	if draft.Transform == nil || len(resolution.Occurrences) == 0 {
		return resolved, nil
	}
	sequence, problems := resolveSequence(scope, source, plan, *draft.Transform, written)
	if problems != nil {
		return nil, problems
	}
	resolved.sequence = sequence
	return resolved, nil
}

// resolveSequence builds what the reproducer retains where the project cannot
// see it and applies the sequence stage to it.
func resolveSequence(scope draftScope, source variantSource, plan reproducer.Plan, stage VariantTransform, written bool) (*variantSequence, []FieldProblem) {
	folder, err := os.MkdirTemp("", "readmit-variant-")
	if err != nil {
		return nil, variantProblem("variant", "the variant cannot be resolved now")
	}
	defer os.RemoveAll(folder)
	built := filepath.Join(folder, "reproducer")
	manifest, err := reproducer.Create(source.bundle, source.path, plan, built)
	if err != nil {
		return nil, variantProblem("variant.plan", err.Error())
	}
	intermediate := filepath.Join(built, reproducer.CaseName)
	before, err := bundle.Open(intermediate)
	if err != nil {
		return nil, variantProblem("variant.plan", "the included messages could not be read back")
	}
	sequence := &variantSequence{rules: acknowledgementRules, before: before, parents: map[string]string{}, entries: map[string]string{}}
	for _, retained := range manifest.Occurrences {
		sequence.parents[retained.Derived] = retained.Parent
	}
	if stage.Rules != nil {
		if scope.loaded == nil {
			return nil, variantProblem("variant.transform.rules", "the project's link rules cannot be read")
		}
		rules, _, name, err := scope.loaded.linkRulesAt(*stage.Rules)
		if err != nil {
			return nil, variantProblem("variant.transform.rules", err.Error())
		}
		sequence.rules, sequence.rulesName = rules, name
	}
	pin := profilepack.Identity{}
	if stage.Profile != nil {
		if scope.loaded == nil {
			return nil, variantProblem("variant.transform.profile", "the project's profiles cannot be read")
		}
		pack, err := scope.loaded.packOf(*stage.Profile)
		if err != nil {
			return nil, variantProblem("variant.transform.profile", err.Error())
		}
		sequence.pack, pin = &pack, pack.Identity
	}
	digest, err := correlate.RulesDigest(sequence.rules)
	if err != nil {
		return nil, variantProblem("variant.transform.rules", err.Error())
	}
	// The engine names the entries of the intermediate case in its evidence
	// order and every copy after them, in the order the steps make them.
	next := len(before.Events)
	for i, event := range before.Events {
		sequence.entries[fmt.Sprintf("t%06d", i+1)] = sequence.parents[event.ID]
	}
	steps := make([]transform.Step, 0, len(stage.Steps))
	for i, step := range stage.Steps {
		field := fmt.Sprintf("variant.transform.steps[%d]", i)
		if step.Step.Entry != "" {
			held, named := sequence.entries[step.Step.Entry]
			if !named || held != step.Occurrence {
				return nil, variantProblem(field, "this change names a message the variant no longer holds in that place; remove it and add it again")
			}
		}
		if step.Step.Operator == transform.DuplicateOccurrence {
			next++
			sequence.entries[fmt.Sprintf("t%06d", next)] = sequence.entries[step.Step.Entry]
		}
		steps = append(steps, step.Step)
	}
	document, err := json.Marshal(transform.Plan{Schema: transform.PlanSchema, Case: before.Identity, Rules: digest, Profile: pin, Steps: steps}, json.Deterministic(true))
	if err == nil {
		sequence.plan, err = transform.DecodePlan(document)
	}
	if err != nil {
		return nil, variantProblem("variant.transform", err.Error())
	}
	if written {
		sequence.preview, sequence.after, err = transform.Create(intermediate, sequence.plan, sequence.rules, sequence.pack, filepath.Join(folder, "written"))
	} else {
		sequence.preview, err = transform.Run(intermediate, sequence.plan, sequence.rules, sequence.pack)
	}
	if err != nil {
		return nil, variantProblem("variant.transform", err.Error())
	}
	return sequence, nil
}

// blocking is every relation the variant breaks and every relation it could
// not decide: each blocks a save, at the row it is about.
func (r *variantResolved) blocking() []VariantNote {
	if r.fhir != nil {
		return []VariantNote{}
	}
	notes := []VariantNote{}
	if len(r.resolution.Occurrences) == 0 {
		notes = append(notes, VariantNote{Code: "no-messages", Detail: "a variant includes at least one message"})
	}
	if r.sequence == nil {
		return notes
	}
	for _, relation := range r.sequence.preview.Relations {
		if relation.Reason == transform.SeveredByDrop {
			notes = append(notes, VariantNote{Code: relation.Reason, Rule: relation.Rule, Occurrences: r.sequence.sources(relation.Occurrences),
				Detail: "an excluded entry breaks a relation the variant still holds part of"})
		}
	}
	for _, item := range r.sequence.preview.Unsupported {
		if item.Code == transform.UnqualifiedIdentifier {
			notes = append(notes, VariantNote{Code: item.Code, Rule: item.Rule, Occurrences: r.sequence.sources([]string{item.Parent}), Detail: item.Detail})
		}
	}
	return notes
}

// sources is the source messages some intermediate occurrences came from.
func (s *variantSequence) sources(occurrences []string) []string {
	held := make([]string, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if parent := s.parents[occurrence]; parent != "" && !slices.Contains(held, parent) {
			held = append(held, parent)
		}
	}
	return held
}

// VariantRequest asks what one variant draft means over its source. Reveal
// asks for the values the changes read and write.
type VariantRequest struct {
	Context RequestContext `json:"context"`
	Draft   VariantDraft   `json:"draft"`
	Reveal  bool           `json:"reveal"`
}

// VariantResult answers one draft: what it means, or the problems that make
// it mean nothing, at the member each is about.
type VariantResult struct {
	State    State          `json:"state"`
	Reason   string         `json:"reason,omitzero"`
	Context  RequestContext `json:"context"`
	Problems []FieldProblem `json:"problems"`
	Variant  *VariantView   `json:"variant,omitzero"`
}

func (r *VariantResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// VariantView is what one variant would hold: every message of its source and
// whether it is included and why, the sequence it holds in order, every field
// it changes, what it does to each relation and to profile support, what it
// could not settle, and what blocks saving it.
type VariantView struct {
	FHIR      *FHIRVariantView        `json:"fhir,omitzero"`
	Messages  []VariantMessage        `json:"messages"`
	Sequence  []VariantEntry          `json:"sequence"`
	Changes   []VariantChange         `json:"changes"`
	Relations []VariantRelation       `json:"relations"`
	Profile   []transform.Combination `json:"profile"`
	Notes     []VariantNote           `json:"notes"`
	Blocking  []VariantNote           `json:"blocking"`
	RulesName string                  `json:"rules_name,omitzero"`
	Revealed  bool                    `json:"revealed"`
}

// VariantMessage is one message of the source, at its place in source order:
// whether the variant includes it, why, and the dependency it was reached for.
type VariantMessage struct {
	Message    TestMessage `json:"message"`
	Position   int         `json:"position"`
	Included   bool        `json:"included"`
	Reason     string      `json:"reason,omitzero"`
	RequiredBy string      `json:"required_by,omitzero"`
	Unresolved []string    `json:"unresolved"`
}

// VariantEntry is one entry of the variant's sequence: the source message it
// holds, its engine name when the sequence stage names it, and whether it is
// a copy.
type VariantEntry struct {
	Entry      string `json:"entry,omitzero"`
	Occurrence string `json:"occurrence"`
	Position   int    `json:"position"`
	Copy       bool   `json:"copy"`
}

// VariantChange is one field the variant changes: the operation, the source
// message and entry, the position, and the state before and after. Values are
// present only when asked for.
type VariantChange struct {
	Operator    string    `json:"operator"`
	Occurrence  string    `json:"occurrence"`
	Entry       string    `json:"entry,omitzero"`
	Selector    string    `json:"selector"`
	Rule        string    `json:"rule,omitzero"`
	Before      hl7.State `json:"before"`
	After       hl7.State `json:"after"`
	BeforeValue *string   `json:"before_value,omitzero"`
	AfterValue  *string   `json:"after_value,omitzero"`
}

// VariantRelation is one relation the rules found among the included
// messages, and whether the variant keeps it whole.
type VariantRelation struct {
	Rule        string             `json:"rule"`
	Operator    correlate.Operator `json:"operator"`
	Occurrences []string           `json:"occurrences"`
	Preserved   bool               `json:"preserved"`
	Reason      string             `json:"reason,omitzero"`
}

// VariantNote is something the variant could not settle, or a reason it
// cannot be saved, at the messages it is about.
type VariantNote struct {
	Code        string   `json:"code"`
	Rule        string   `json:"rule,omitzero"`
	Occurrences []string `json:"occurrences"`
	Detail      string   `json:"detail"`
}

// ResolveVariant resolves one variant draft over its source exactly as Save
// would build it, and writes nothing into the project. A draft the engines
// refuse answers the problem at the member it is about, so the editor keeps
// the plan it had.
func (a *App) ResolveVariant(request VariantRequest) VariantResult {
	return runRead(a, false, func(ctx context.Context) VariantResult {
		result := VariantResult{Context: request.Context, Problems: []FieldProblem{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		resolved, problems := resolveVariant(draftScope{root: loaded.root, loaded: loaded}, request.Draft, request.Reveal)
		if problems != nil {
			result.State, result.Problems = Failed, problems
			result.Reason = problems[0].Problem
			return result
		}
		result.State, result.Variant = Completed, resolved.view(request.Reveal)
		return result
	})
}

// view lays one resolved draft out for the editor.
func (r *variantResolved) view(reveal bool) *VariantView {
	if r.fhir != nil {
		return r.fhirView(reveal)
	}
	view := &VariantView{Messages: []VariantMessage{}, Sequence: []VariantEntry{}, Changes: []VariantChange{}, Relations: []VariantRelation{},
		Profile: []transform.Combination{}, Notes: []VariantNote{}, Blocking: r.blocking(), Revealed: reveal}
	retained := map[string]reproducer.Retained{}
	for _, held := range r.resolution.Occurrences {
		retained[held.Parent] = held
	}
	unresolved := map[string][]string{}
	for _, item := range r.resolution.Unresolved {
		unresolved[item.Occurrence] = append(unresolved[item.Occurrence], item.Reason)
		view.Notes = append(view.Notes, VariantNote{Code: item.Reason, Occurrences: []string{item.Occurrence}, Detail: item.Reason})
	}
	for i, message := range caseMessages(r.source.bundle) {
		shown := VariantMessage{Message: message, Position: i + 1, Unresolved: unresolved[message.ID]}
		if shown.Unresolved == nil {
			shown.Unresolved = []string{}
		}
		if held, included := retained[message.ID]; included {
			shown.Included, shown.Reason, shown.RequiredBy = true, held.Reason, held.RequiredBy
		}
		view.Messages = append(view.Messages, shown)
	}
	sourceRaw := func(id string) []byte { raw, _ := r.source.bundle.Raw(id); return raw }
	for _, edit := range r.resolution.Edits {
		change := VariantChange{Operator: edit.Operator, Occurrence: edit.Parent, Selector: edit.Selector, Before: edit.State, After: hl7.Empty}
		value := ""
		if edit.Operator == reproducer.SetField {
			change.After = hl7.Present
			for _, step := range r.plan.Steps {
				if step.Operator == reproducer.SetField && step.Occurrence == edit.Parent && canonical(step.Selector) == edit.Selector {
					value = step.Value
				}
			}
		}
		if reveal {
			before := readField(r.source.bundle, edit.Parent, sourceRaw(edit.Parent), edit.Selector)
			change.BeforeValue, change.AfterValue = &before, &value
		}
		view.Changes = append(view.Changes, change)
	}
	if r.sequence == nil {
		// The reproducer writes the included messages source by source, and
		// the sequence stage names them in that order.
		entries := intermediateEntries(r.source.bundle, r.resolution, nil)
		for i := 1; i <= len(entries); i++ {
			name := fmt.Sprintf("t%06d", i)
			view.Sequence = append(view.Sequence, VariantEntry{Entry: name, Occurrence: entries[name], Position: i})
		}
		return view
	}
	s := r.sequence
	view.RulesName = s.rulesName
	for _, entry := range s.preview.Sequence {
		view.Sequence = append(view.Sequence, VariantEntry{Entry: entry.ID, Occurrence: s.parents[entry.Parent], Position: entry.Position, Copy: entry.Copy})
	}
	positions := map[string]int{}
	for i, entry := range s.preview.Sequence {
		positions[entry.ID] = i
	}
	for _, placed := range s.preview.Changes {
		change := VariantChange{Operator: placed.Operator, Occurrence: s.parents[placed.Parent], Entry: placed.Entry, Selector: placed.Selector,
			Rule: placed.Rule, Before: placed.State, After: hl7.Present}
		if reveal && s.after != nil {
			raw, _ := s.before.Raw(placed.Parent)
			before := readField(s.before, placed.Parent, raw, placed.Selector)
			after := ""
			if at, held := positions[placed.Entry]; held && at < len(s.after.Events) {
				written, _ := s.after.Raw(s.after.Events[at].ID)
				after = readField(s.after, s.after.Events[at].ID, written, placed.Selector)
			}
			change.BeforeValue, change.AfterValue = &before, &after
		}
		view.Changes = append(view.Changes, change)
	}
	for _, relation := range s.preview.Relations {
		view.Relations = append(view.Relations, VariantRelation{Rule: relation.Rule, Operator: relation.Operator,
			Occurrences: s.sources(relation.Occurrences), Preserved: relation.Preserved, Reason: relation.Reason})
	}
	view.Profile = append(view.Profile, s.preview.Profile...)
	for _, item := range s.preview.Unsupported {
		note := VariantNote{Code: item.Code, Rule: item.Rule, Occurrences: []string{}, Detail: item.Detail}
		if item.Parent != "" {
			note.Occurrences = s.sources([]string{item.Parent})
		}
		view.Notes = append(view.Notes, note)
	}
	return view
}

// canonical is a selector as the engines record it.
func canonical(selector string) string {
	parsed, err := hl7.ParseSelector(selector)
	if err != nil {
		return selector
	}
	return parsed.String()
}

// readField is the text of one position of one occurrence, or empty when it
// holds no text; the change's own state says which.
func readField(source *bundle.Bundle, id string, raw []byte, selector string) string {
	parsed, err := hl7.ParseSelector(selector)
	if err != nil {
		return ""
	}
	options := hl7.Options{}
	for _, event := range source.Events {
		if event.ID != id {
			continue
		}
		for _, declared := range source.Manifest.Sources {
			if declared.ID == event.SourceID {
				options = hl7.Options{Format: declared.Format, Terminator: event.Terminator}
			}
		}
	}
	doc, err := hl7.Parse(raw, options)
	if err != nil || len(doc.Messages) == 0 {
		return ""
	}
	reading, err := doc.Read(0, parsed, hl7.IgnoreMSH18)
	if err != nil {
		return ""
	}
	if text, ok := reading.Text(); ok {
		return text
	}
	return ""
}
