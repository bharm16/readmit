package desktop

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/reportshare"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/transform"
)

// Case comparison (#558): two named cases or variants of the project, compared
// field by field by the engine `readmit diff` runs, optionally read under a
// named normalization policy by the engine `readmit normalize` runs. Neither
// case is changed, the raw differences stay available however a policy
// presents them, and a variant also shows its lineage and its plan.

// NormalizationPolicySummary is how many rules a normalization policy holds.
type NormalizationPolicySummary struct {
	Rules int `json:"rules"`
}

func readPolicyFile(path string) (diff.Policy, error) {
	data, err := boundedFile(path, diff.MaxPolicyBytes)
	if err != nil {
		return diff.Policy{}, err
	}
	return diff.DecodePolicy(data)
}

func readNormalizationPolicyItem(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
	policy, err := readPolicyFile(paths[string(NormalizationPolicyItem)])
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{NormalizationPolicy: &NormalizationPolicySummary{Rules: len(policy.Rules)}}}, nil
}

// validateNormalizationPolicyDraft validates a whole policy draft. A rule the
// editor added has no identity yet and is given the next free one; each rule
// is checked on its own, so a problem is reported at the rule it is about.
func validateNormalizationPolicyDraft(draft ItemDraft) ([]catalog.Staged, *diff.Policy, []FieldProblem) {
	problems := []FieldProblem{}
	if strings.TrimSpace(draft.Name) == "" {
		problems = append(problems, FieldProblem{Field: "name", Problem: "a normalization policy is saved under a name"})
	}
	if draft.NormalizationPolicy == nil || len(draft.NormalizationPolicy.Rules) == 0 {
		return nil, nil, append(problems, FieldProblem{Field: "normalization_policy.rules", Problem: "a normalization policy holds at least one rule"})
	}
	policy := diff.Policy{Schema: diff.PolicySchema, Rules: slices.Clone(draft.NormalizationPolicy.Rules)}
	used := map[string]bool{}
	for _, rule := range policy.Rules {
		used[rule.ID] = true
	}
	next := 1
	for i := range policy.Rules {
		if policy.Rules[i].ID != "" {
			continue
		}
		for used["rule-"+strconv.Itoa(next)] {
			next++
		}
		policy.Rules[i].ID = "rule-" + strconv.Itoa(next)
		used[policy.Rules[i].ID] = true
	}
	selectors := map[string]bool{}
	for i, rule := range policy.Rules {
		field := "normalization_policy.rules[" + strconv.Itoa(i) + "]"
		if selector, err := hl7.ParseSelector(rule.Selector); err != nil {
			problems = append(problems, FieldProblem{Field: field + ".selector", Problem: "a rule names one field this release addresses, such as MSH-7"})
			continue
		} else if selectors[selector.String()] {
			problems = append(problems, FieldProblem{Field: field + ".selector", Problem: "another rule already addresses this field"})
			continue
		} else {
			selectors[selector.String()] = true
		}
		if err := (diff.Policy{Schema: diff.PolicySchema, Rules: []diff.Rule{rule}}).Validate(); err != nil {
			problems = append(problems, FieldProblem{Field: field, Problem: ruleProblem(rule)})
		}
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	data, err := json.Marshal(policy, json.Deterministic(true))
	if err == nil {
		_, err = diff.DecodePolicy(data)
	}
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "normalization_policy", Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: string(NormalizationPolicyItem), File: "policy.json", Data: append(data, '\n')}}, &policy, nil
}

// ruleProblem says what one rule the policy reader refused needs.
func ruleProblem(rule diff.Rule) string {
	switch rule.Operator {
	case diff.TimestampOperator:
		return "a timestamp rule compares to one precision, from year to second"
	case diff.NumericOperator:
		return "a number rule compares within one tolerance of zero or more"
	case diff.IgnoreOperator:
		return "an ignore rule takes no precision or tolerance"
	}
	return "a rule ignores a difference, or compares it as a timestamp or a number"
}

// CaseComparisonRequest names the two cases compared, in their roles: the
// current case and the other one. Keys are the fields that identify one
// record on both sides, which are never guessed; Fields narrow the comparison,
// none comparing every field; Policy names the normalization policy the
// differences are read under, and Original shows every raw difference however
// the policy presents it. Reveal asks for the values each side holds.
type SelectedMessagePair struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type CaseComparisonRequest struct {
	Pair     *SelectedMessagePair `json:"pair,omitzero"`
	Context  RequestContext       `json:"context"`
	Current  ItemRef              `json:"current"`
	Other    ItemRef              `json:"other"`
	Keys     []string             `json:"keys"`
	Fields   []string             `json:"fields"`
	Policy   *ItemRef             `json:"policy,omitzero"`
	Original bool                 `json:"original"`
	Reveal   bool                 `json:"reveal"`
	Offset   int                  `json:"offset"`
	Limit    int                  `json:"limit"`
}

// CaseComparisonResult answers one comparison. One the engine refuses to
// align still carries both sides and the lineage of each variant, with no row.
type CaseComparisonResult struct {
	State      State           `json:"state"`
	Reason     string          `json:"reason,omitzero"`
	Context    RequestContext  `json:"context"`
	Comparison *CaseComparison `json:"comparison,omitzero"`
}

func (r *CaseComparisonResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ComparisonSide is one compared case: its reference and version, its name
// and how many messages it holds.
type ComparisonSide struct {
	Occurrence string  `json:"occurrence,omitzero"`
	Identity   string  `json:"identity,omitzero"`
	RawSHA256  string  `json:"raw_sha256,omitzero"`
	RawBytes   int     `json:"raw_bytes,omitzero"`
	Ref        ItemRef `json:"ref"`
	Name       string  `json:"name"`
	Messages   int     `json:"messages"`
}

// CaseComparison is one comparison, windowed: the two sides, the keys and
// fields it ran under, the policy and what each of its rules did, the rows of
// the window, every count over the whole comparison, and the lineage of each
// side that is a variant.
type CaseComparison struct {
	PairOutput  *ShareOutput        `json:"pair_output,omitzero"`
	RawEqual    *bool               `json:"raw_equal,omitzero"`
	Current     ComparisonSide      `json:"current"`
	Other       ComparisonSide      `json:"other"`
	Keys        []string            `json:"keys"`
	Fields      []string            `json:"fields"`
	Alignment   string              `json:"alignment"`
	Policy      *ItemRef            `json:"policy,omitzero"`
	PolicyName  string              `json:"policy_name,omitzero"`
	Rules       []diff.RuleReport   `json:"rules"`
	Summary     diff.Summary        `json:"summary"`
	Suppressed  int                 `json:"suppressed"`
	Original    bool                `json:"original"`
	Revealed    bool                `json:"revealed"`
	Rows        []CaseComparisonRow `json:"rows"`
	Offset      int                 `json:"offset"`
	Limit       int                 `json:"limit"`
	Total       int                 `json:"total"`
	Unsupported []diff.Unsupported  `json:"unsupported"`
	Lineage     []VariantLineage    `json:"lineage"`
}

// CaseComparisonRow is one line of the comparison: a field that differs
// between two aligned messages, or a message only one side holds, one the
// keys matched ambiguously, or one no key placed. Earlier is the current
// case's message and Later the other's. States are always present; values
// only when asked for. Outcome is what the policy did about a difference.
type CaseComparisonRow struct {
	Position     int          `json:"position"`
	Kind         RowKind      `json:"kind"`
	Earlier      *TestMessage `json:"earlier,omitzero"`
	Later        *TestMessage `json:"later,omitzero"`
	Field        string       `json:"field,omitzero"`
	Name         string       `json:"name,omitzero"`
	Change       string       `json:"change"`
	EarlierState hl7.State    `json:"earlier_state,omitzero"`
	LaterState   hl7.State    `json:"later_state,omitzero"`
	EarlierValue *string      `json:"earlier_value,omitzero"`
	LaterValue   *string      `json:"later_value,omitzero"`
	Outcome      string       `json:"outcome,omitzero"`
	Rule         string       `json:"rule,omitzero"`
	Reason       string       `json:"reason,omitzero"`
	Group        int          `json:"group,omitzero"`
}

// VariantLineage is what one variant side of a comparison was derived from
// and how: its source, the transformation it was saved under, every message
// it includes and why, and every step of its plan.
type VariantLineage struct {
	Variant     ItemRef           `json:"variant"`
	VariantName string            `json:"variant_name"`
	Source      *ItemRef          `json:"source,omitzero"`
	SourceName  string            `json:"source_name,omitzero"`
	Operation   string            `json:"operation,omitzero"`
	Included    []VariantMessage  `json:"included"`
	Steps       []VariantPlanStep `json:"steps"`
	Reason      string            `json:"reason,omitzero"`
}

// VariantPlanStep is one step of a saved variant's plan, at the source
// message it names.
type VariantPlanStep struct {
	Operator string       `json:"operator"`
	Message  *TestMessage `json:"message,omitzero"`
	Selector string       `json:"selector,omitzero"`
	Identity []string     `json:"identity"`
	Rule     string       `json:"rule,omitzero"`
	Shift    string       `json:"shift,omitzero"`
	Position int          `json:"position,omitzero"`
}

// CompareCases compares two cases of the project in their roles, under the
// keys, fields and policy named, and answers one window of the rows. It is a
// read: neither case, the policy nor anything else is written.
func (a *App) CompareCases(request CaseComparisonRequest) CaseComparisonResult {
	return runRead(a, false, func(ctx context.Context) CaseComparisonResult {
		result := CaseComparisonResult{Context: request.Context}
		if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxComparisonRows {
			result.refuse(Failed, "a comparison renders a window beginning at or after its first row, of between 1 and "+strconv.Itoa(MaxComparisonRows)+" rows")
			return result
		}
		if request.Current.ID == request.Other.ID && (request.Pair == nil || request.Pair.Left == request.Pair.Right) {
			result.refuse(Failed, "choose another case to compare with")
			return result
		}
		for _, ref := range []ItemRef{request.Current, request.Other} {
			if ref.Kind != CaseItem && ref.Kind != VariantItem {
				result.refuse(Failed, "a comparison reads two cases or variants of the project")
				return result
			}
		}
		loaded, items, records, declined := a.scoped(ctx, request.Context, []ItemRef{{Kind: request.Current.Kind, ID: request.Current.ID}, {Kind: request.Other.Kind, ID: request.Other.ID}})
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		sides := make([]*bundle.Bundle, 2)
		paths := make([]string, 2)
		for i, record := range records {
			entry := caseEntryOf(items[i], record)
			path, refused := comparedCase(loaded.root, entry)
			if path == "" {
				result.refuse(refused.state, refused.reason)
				return result
			}
			opened, err := bundle.Open(path)
			if err != nil {
				result.refuse(Failed, "the case "+items[i].Name+" could not be verified as complete, unmodified evidence")
				return result
			}
			sides[i], paths[i] = opened, path
		}
		// What each variant side was made from does not depend on how the
		// records align, so a comparison the engine refuses still carries the
		// two sides and their lineage, with no row.
		lineage := []VariantLineage{}
		for i, item := range items {
			if item.Ref.Kind == VariantItem {
				lineage = append(lineage, loaded.lineageOf(item, records[i]))
			}
		}
		comparison := &CaseComparison{
			Current: ComparisonSide{Ref: items[0].Ref, Name: items[0].Name, Messages: len(sides[0].Events)},
			Other:   ComparisonSide{Ref: items[1].Ref, Name: items[1].Name, Messages: len(sides[1].Events)},
			Keys:    nonNil(request.Keys), Fields: nonNil(request.Fields), Rules: []diff.RuleReport{},
			Original: request.Original || request.Policy == nil, Revealed: request.Reveal,
			Offset: request.Offset, Limit: request.Limit, Rows: []CaseComparisonRow{},
			Unsupported: []diff.Unsupported{}, Lineage: lineage,
		}
		refuseAligned := func(reason string) CaseComparisonResult {
			result.refuse(Failed, reason)
			result.Comparison = comparison
			return result
		}
		options := diff.Options{Keys: request.Keys, Fields: request.Fields}
		shown := options
		shown.ShowValues = request.Reveal
		var report diff.Report
		var err error
		if request.Pair != nil {
			if request.Policy != nil {
				return refuseAligned("an explicit message pair shows original differences; choose the two-capture view to apply a named normalization policy")
			}
			leftBytes, leftKnown := sides[0].Raw(request.Pair.Left)
			rightBytes, rightKnown := sides[1].Raw(request.Pair.Right)
			if leftKnown != nil || rightKnown != nil {
				return refuseAligned("the selected source occurrence is unavailable")
			}
			equal := bytes.Equal(leftBytes, rightBytes)
			comparison.RawEqual = &equal
			comparison.Current.Occurrence, comparison.Other.Occurrence = request.Pair.Left, request.Pair.Right
			comparison.Current.Identity, comparison.Other.Identity = sides[0].Identity, sides[1].Identity
			comparison.Current.RawSHA256, comparison.Other.RawSHA256 = digestOf(leftBytes), digestOf(rightBytes)
			comparison.Current.RawBytes, comparison.Other.RawBytes = len(leftBytes), len(rightBytes)
			if request.Reveal && len(leftBytes)+len(rightBytes) <= reportshare.MaxSelectedMessageBytes {
				output := shareOutput(&reportshare.Share{SourceValues: true, Files: []reportshare.File{{Name: "Left original message", Kind: reportshare.MessageType, Data: leftBytes}, {Name: "Right original message", Kind: reportshare.MessageType, Data: rightBytes}}}, "original-message-bytes", ReportShareOptions{Format: "original-message-bytes"})
				comparison.PairOutput = &output
			}
			report, err = diff.CompareSelected(diff.Input{Path: paths[0]}, diff.Input{Path: paths[1]}, request.Pair.Left, request.Pair.Right, shown)
		} else {
			report, err = diff.Compare(diff.Input{Path: paths[0]}, diff.Input{Path: paths[1]}, shown)
		}
		if err != nil {
			return refuseAligned(refusedComparison(err))
		}
		comparison.Keys, comparison.Fields = nonNil(report.Keys), nonNil(report.Fields)
		comparison.Alignment, comparison.Summary, comparison.Unsupported = report.Alignment, report.Summary, nonNil(report.Unsupported)
		outcomes := map[string]diff.Difference{}
		if request.Policy != nil {
			item, exact, policyPaths, err := loaded.held(NormalizationPolicyItem, *request.Policy)
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			policy, err := readPolicyFile(policyPaths[string(NormalizationPolicyItem)])
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			normalized, err := diff.Normalize(diff.Input{Path: paths[0]}, diff.Input{Path: paths[1]}, options, policy)
			if err != nil {
				return refuseAligned(refusedComparison(err))
			}
			comparison.Policy, comparison.PolicyName, comparison.Rules = &exact, item.Name, normalized.Rules
			comparison.Suppressed = normalized.Summary.Suppressed
			for _, difference := range normalized.Differences {
				outcomes[difference.LeftOccurrence+"\x00"+difference.RightOccurrence+"\x00"+difference.Selector] = difference
			}
		}
		rows := caseComparisonRows(report, caseMessageIndex(sides[0]), caseMessageIndex(sides[1]), outcomes, comparison.Original)
		comparison.Total = len(rows)
		if request.Offset < len(rows) {
			comparison.Rows = append(comparison.Rows, rows[request.Offset:min(len(rows), request.Offset+request.Limit)]...)
		}
		result.State, result.Comparison = Completed, comparison
		return result
	})
}

// caseEntryOf is the case bundle one case or variant object is.
func caseEntryOf(item CatalogItem, record catalog.Item) string {
	if item.Summary.Variant != nil && item.Summary.Variant.Entry != "" {
		return item.Summary.Variant.Entry
	}
	if item.Summary.Case != nil && item.Summary.Case.Entry != "" {
		return item.Summary.Case.Entry
	}
	return record.Entry
}

// caseMessageIndex is every message of a case by its occurrence.
func caseMessageIndex(source *bundle.Bundle) map[string]TestMessage {
	index := map[string]TestMessage{}
	for _, message := range caseMessages(source) {
		index[message.ID] = message
	}
	return index
}

// caseComparisonRows lays a comparison out as rows: each differing field of
// each aligned pair, then every message the other case does not hold, every
// one only it holds, every candidate of an ambiguous key and every message no
// key placed. A difference the policy suppresses is left out unless the
// original differences are asked for.
func caseComparisonRows(report diff.Report, earlier, later map[string]TestMessage, outcomes map[string]diff.Difference, original bool) []CaseComparisonRow {
	message := func(index map[string]TestMessage, reference *diff.Reference) *TestMessage {
		if reference == nil {
			return nil
		}
		held, known := index[reference.Occurrence]
		if !known {
			held = TestMessage{ID: reference.Occurrence, Kind: bundle.EventKind(reference.Kind)}
		}
		return &held
	}
	rows := []CaseComparisonRow{}
	for _, pair := range report.Pairs {
		left, right := pair.Left, pair.Right
		for _, field := range pair.Fields {
			row := CaseComparisonRow{Kind: PairedRow, Earlier: message(earlier, &left), Later: message(later, &right), Field: field.Selector, Name: field.Name,
				Change: field.Status, EarlierState: field.Left.State, LaterState: field.Right.State, EarlierValue: field.Left.Display, LaterValue: field.Right.Display}
			if outcome, held := outcomes[left.Occurrence+"\x00"+right.Occurrence+"\x00"+field.Selector]; held {
				row.Outcome, row.Rule, row.Reason = outcome.Outcome, outcome.Rule, outcome.Reason
				if outcome.Outcome == diff.Suppressed && !original {
					continue
				}
			}
			rows = append(rows, row)
		}
	}
	for _, missing := range report.Missing {
		rows = append(rows, CaseComparisonRow{Kind: MissingRow, Earlier: message(earlier, &missing), Change: string(MissingRow)})
	}
	for _, inserted := range report.Inserted {
		rows = append(rows, CaseComparisonRow{Kind: InsertedRow, Later: message(later, &inserted), Change: string(InsertedRow)})
	}
	for number, ambiguity := range report.Ambiguous {
		for _, candidate := range ambiguity.Left {
			rows = append(rows, CaseComparisonRow{Kind: AmbiguousRow, Earlier: message(earlier, &candidate), Change: string(AmbiguousRow), Reason: ambiguity.Reason, Group: number + 1})
		}
		for _, candidate := range ambiguity.Right {
			rows = append(rows, CaseComparisonRow{Kind: AmbiguousRow, Later: message(later, &candidate), Change: string(AmbiguousRow), Reason: ambiguity.Reason, Group: number + 1})
		}
	}
	for _, unaligned := range report.Unaligned {
		row := CaseComparisonRow{Kind: UnalignedRow, Change: string(UnalignedRow), Reason: unaligned.Reason}
		if unaligned.Side == "left" {
			row.Earlier = message(earlier, &unaligned.Reference)
		} else {
			row.Later = message(later, &unaligned.Reference)
		}
		rows = append(rows, row)
	}
	for i := range rows {
		rows[i].Position = i + 1
	}
	return rows
}

// lineageOf is what one variant was derived from and the plan it was saved
// with, read from its saved plans and resolved over its source. A variant
// whose plan or source cannot be read says why rather than showing less.
func (c *loadedCatalog) lineageOf(item CatalogItem, record catalog.Item) VariantLineage {
	lineage := VariantLineage{Variant: item.Ref, VariantName: item.Name, Included: []VariantMessage{}, Steps: []VariantPlanStep{}}
	if summary := item.Summary.Variant; summary != nil {
		lineage.Source, lineage.Operation = summary.Parent, summary.Operation
	}
	if lineage.Source != nil {
		if index := c.document.Find(lineage.Source.ID); index >= 0 {
			lineage.SourceName = c.read(c.document.Items[index]).Name
		}
	}
	paths, availability, _ := c.backing(record)
	if availability != ItemAvailable || paths["plan"] == "" || lineage.Source == nil {
		lineage.Reason = "this variant was not saved with its plan"
		return lineage
	}
	data, err := boundedFile(paths["plan"], catalog.MaxMemberBytes)
	if err != nil {
		lineage.Reason = err.Error()
		return lineage
	}
	plan, err := reproducer.DecodePlan(data)
	if err != nil {
		lineage.Reason = err.Error()
		return lineage
	}
	source, problem := resolveVariantSource(draftScope{root: c.root, loaded: c}, *lineage.Source)
	if problem != nil {
		lineage.Reason = problem.Problem
		return lineage
	}
	resolution, err := reproducer.Resolve(source.bundle, plan)
	if err != nil {
		lineage.Reason = err.Error()
		return lineage
	}
	messages := caseMessageIndex(source.bundle)
	labelled := func(id string) *TestMessage {
		if id == "" {
			return nil
		}
		held, known := messages[id]
		if !known {
			held = TestMessage{ID: id}
		}
		return &held
	}
	for _, held := range resolution.Occurrences {
		lineage.Included = append(lineage.Included, VariantMessage{Message: *labelled(held.Parent), Included: true, Reason: held.Reason, RequiredBy: held.RequiredBy, Unresolved: []string{}})
	}
	for _, step := range plan.Steps {
		lineage.Steps = append(lineage.Steps, VariantPlanStep{Operator: step.Operator, Message: labelled(step.Occurrence), Selector: step.Selector, Identity: nonNil(step.Identity)})
	}
	if paths["transform"] == "" {
		return lineage
	}
	data, err = boundedFile(paths["transform"], transform.MaxPlanBytes)
	if err != nil {
		lineage.Reason = err.Error()
		return lineage
	}
	sequence, err := transform.DecodePlan(data)
	if err != nil {
		lineage.Reason = err.Error()
		return lineage
	}
	entries := intermediateEntries(source.bundle, resolution, sequence.Steps)
	for _, step := range sequence.Steps {
		lineage.Steps = append(lineage.Steps, VariantPlanStep{Operator: step.Operator, Message: labelled(entries[step.Entry]), Rule: step.Rule, Shift: step.Shift,
			Position: step.Position, Identity: []string{}})
	}
	return lineage
}

// intermediateEntries names the entries of the sequence stage by the source
// message each holds: the reproducer writes the retained messages source by
// source in the order the case declares its sources, and the engine names
// them in that order and every copy after them.
func intermediateEntries(source *bundle.Bundle, resolution reproducer.Resolution, steps []transform.Step) map[string]string {
	sources := map[string]string{}
	for _, event := range source.Events {
		sources[event.ID] = event.SourceID
	}
	entries := map[string]string{}
	next := 0
	for _, declared := range source.Manifest.Sources {
		for _, held := range resolution.Occurrences {
			if sources[held.Parent] == declared.ID {
				next++
				entries[fmt.Sprintf("t%06d", next)] = held.Parent
			}
		}
	}
	for _, step := range steps {
		if step.Operator == transform.DuplicateOccurrence {
			next++
			entries[fmt.Sprintf("t%06d", next)] = entries[step.Entry]
		}
	}
	return entries
}

// nonNil is a list, empty rather than absent.
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
