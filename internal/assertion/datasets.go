package assertion

import (
	"context"
	"encoding/json/v2"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/dataset"
)

const DatasetSchema = "readmit-dataset-assertion-set/v1"

type DatasetBinding struct {
	ProjectionIdentity string `json:"projection_identity"`
	Name               string `json:"name"`
	Namespace          string `json:"namespace"`
	Phase              string `json:"phase"`
	Source             string `json:"source_identity"`
}
type RowFilter struct {
	Column string        `json:"column"`
	Equals dataset.Value `json:"equals"`
}
type RowSelection struct {
	Dataset string      `json:"dataset"`
	Row     string      `json:"row,omitzero"`
	Where   []RowFilter `json:"where"`
}
type DatasetCondition struct {
	Subject RowSelection  `json:"subject"`
	Column  string        `json:"column"`
	Equals  dataset.Value `json:"equals"`
}
type DatasetAssertion struct {
	ID          string            `json:"id"`
	Operator    string            `json:"operator"`
	Subject     RowSelection      `json:"subject"`
	Column      string            `json:"column,omitzero"`
	Expected    *dataset.Value    `json:"expected,omitzero"`
	Other       *RowSelection     `json:"other,omitzero"`
	OtherColumn string            `json:"other_column,omitzero"`
	Count       *int              `json:"count,omitzero"`
	Quantifier  string            `json:"quantifier,omitzero"`
	Sequence    []dataset.Value   `json:"sequence,omitzero"`
	When        *DatasetCondition `json:"when,omitzero"`
}
type DatasetSetDocument struct {
	Schema     string             `json:"schema"`
	Bindings   []DatasetBinding   `json:"bindings"`
	Assertions []DatasetAssertion `json:"assertions"`
}
type DatasetSet struct{ document DatasetSetDocument }

var datasetID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var datasetHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func DecodeDatasets(raw []byte) (*DatasetSet, error) {
	bad := errors.New("invalid dataset assertion set")
	var d DatasetSetDocument
	if len(raw) > MaxSetBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != DatasetSchema || len(d.Bindings) < 1 || len(d.Bindings) > 32 || len(d.Assertions) < 1 || len(d.Assertions) > MaxAssertions {
		return nil, bad
	}
	bindings := map[string]bool{}
	for _, b := range d.Bindings {
		if !datasetID.MatchString(b.Name) || bindings[b.Name] || !datasetID.MatchString(b.Namespace) || !datasetHash.MatchString(b.Source) || !datasetHash.MatchString(b.ProjectionIdentity) || b.Phase != "before" && b.Phase != "after" {
			return nil, bad
		}
		bindings[b.Name] = true
	}
	selection := func(s RowSelection) bool {
		if !bindings[s.Dataset] || len(s.Row) > 64 || len(s.Where) > 16 {
			return false
		}
		seen := map[string]bool{}
		for _, w := range s.Where {
			if !datasetID.MatchString(w.Column) || seen[w.Column] || !dataset.ValidExpected(w.Equals) {
				return false
			}
			seen[w.Column] = true
		}
		return true
	}
	seen := map[string]bool{}
	for _, a := range d.Assertions {
		if !datasetID.MatchString(a.ID) || seen[a.ID] || !selection(a.Subject) {
			return nil, bad
		}
		seen[a.ID] = true
		if a.When != nil && (!selection(a.When.Subject) || !datasetID.MatchString(a.When.Column) || !dataset.ValidExpected(a.When.Equals)) {
			return nil, bad
		}
		field, expected, other, count, quantifier, sequence := false, false, false, false, false, false
		switch a.Operator {
		case "value-equals", "decimal-equals", "instant-equals":
			field, expected = true, true
		case "value-related", "value-changed":
			field, other = true, true
		case "row-count":
			count = true
		case "unique-keys":
		case "each-equals":
			field, expected, quantifier = true, true, true
		case "sequence-equals":
			field, sequence = true, true
		default:
			return nil, bad
		}
		if field != (a.Column != "") || field && !datasetID.MatchString(a.Column) || expected != (a.Expected != nil) || a.Expected != nil && !dataset.ValidExpected(*a.Expected) || other != (a.Other != nil) || other != (a.OtherColumn != "") || a.Other != nil && (!selection(*a.Other) || !datasetID.MatchString(a.OtherColumn)) || count != (a.Count != nil) || a.Count != nil && (*a.Count < 0 || *a.Count > 10000) || quantifier != (a.Quantifier != "") || quantifier && !slices.Contains([]string{"every", "any", "none"}, a.Quantifier) || sequence != (a.Sequence != nil) || len(a.Sequence) > 10000 {
			return nil, bad
		}
		for _, v := range a.Sequence {
			if !dataset.ValidExpected(v) {
				return nil, bad
			}
		}
	}
	return &DatasetSet{document: d}, nil
}
func (s *DatasetSet) Document() DatasetSetDocument {
	var d DatasetSetDocument
	raw, _ := json.Marshal(s.document)
	_ = json.Unmarshal(raw, &d)
	return d
}

type DatasetResult struct {
	ID       string   `json:"id"`
	Operator string   `json:"operator"`
	Outcome  Outcome  `json:"outcome"`
	Reason   string   `json:"reason"`
	Rows     []string `json:"rows"`
	Count    int      `json:"count"`
}
type DatasetReport struct {
	Verdict  Verdict           `json:"verdict"`
	Results  []DatasetResult   `json:"results"`
	Evidence map[string]string `json:"evidence"`
}

// Evaluate uses only verified retained snapshots. Old occurrence and key-only
// assertions keep their independent contract and semantics.
func (s *DatasetSet) Evaluate(ctx context.Context, run string, evidence map[string]*dataset.Snapshot) (DatasetReport, error) {
	if s == nil || !datasetID.MatchString(run) {
		return DatasetReport{}, &Error{Class: ErrorNotDecoded}
	}
	documents := map[string]dataset.Document{}
	report := DatasetReport{Results: []DatasetResult{}, Evidence: map[string]string{}}
	for _, b := range s.document.Bindings {
		snapshot := evidence[b.Name]
		if snapshot == nil || !snapshot.Usable() {
			return DatasetReport{}, &Error{Class: ErrorIncompleteObservation}
		}
		d := snapshot.Document()
		if d.Binding.Run != run || d.Binding.Namespace != b.Namespace || d.Binding.Phase != b.Phase || d.Binding.Source != b.Source || d.Projection.Identity() != b.ProjectionIdentity {
			return DatasetReport{}, &Error{Class: "dataset_binding_mismatch"}
		}
		documents[b.Name] = d
		report.Evidence[b.Name] = snapshot.Identity()
	}
	passed, failed, undecided := 0, 0, 0
	for _, a := range s.document.Assertions {
		if ctx.Err() != nil {
			return DatasetReport{}, &Error{Class: ErrorCancelled}
		}
		r := decideDataset(a, documents)
		report.Results = append(report.Results, r)
		switch r.Outcome {
		case OutcomePassed:
			passed++
		case OutcomeFailed:
			failed++
		case OutcomeUndecided:
			undecided++
		}
	}
	report.Verdict = overallVerdict(passed, failed, undecided)
	return report, nil
}
func selectRows(s RowSelection, docs map[string]dataset.Document) (dataset.Document, []dataset.Row, string) {
	d, ok := docs[s.Dataset]
	if !ok {
		return d, nil, "unknown-dataset"
	}
	indexes := []int{}
	for _, w := range s.Where {
		i := columnIndex(d, w.Column)
		if i < 0 {
			return d, nil, "unknown-column"
		}
		if !dataset.Compatible(d.Projection.Columns[i], w.Equals) {
			return d, nil, "incompatible-filter"
		}
		indexes = append(indexes, i)
	}
	rows := []dataset.Row{}
	for _, row := range d.Rows {
		if s.Row != "" && row.ID != s.Row {
			continue
		}
		match := true
		for i, w := range s.Where {
			if !dataset.Equal(row.Values[indexes[i]], w.Equals) {
				match = false
				break
			}
		}
		if match {
			rows = append(rows, row)
		}
	}
	return d, rows, ""
}
func columnIndex(d dataset.Document, name string) int {
	return slices.IndexFunc(d.Projection.Columns, func(c dataset.Column) bool { return c.Name == name })
}
func oneValue(s RowSelection, column string, docs map[string]dataset.Document) (dataset.Value, string) {
	d, rows, reason := selectRows(s, docs)
	if reason != "" {
		return dataset.Value{}, reason
	}
	i := columnIndex(d, column)
	if i < 0 {
		return dataset.Value{}, "unknown-column"
	}
	if len(rows) == 0 {
		return dataset.Value{}, "no-row"
	}
	if len(rows) != 1 {
		return dataset.Value{}, "multiple-rows"
	}
	return rows[0].Values[i], ""
}
func decideDataset(a DatasetAssertion, docs map[string]dataset.Document) DatasetResult {
	r := DatasetResult{ID: a.ID, Operator: a.Operator, Outcome: OutcomeUndecided, Rows: []string{}}
	if a.When != nil {
		v, why := oneValue(a.When.Subject, a.When.Column, docs)
		if why != "" {
			r.Reason = "condition-" + why
			return r
		}
		conditionDoc := docs[a.When.Subject.Dataset]
		conditionColumn := columnIndex(conditionDoc, a.When.Column)
		if conditionColumn < 0 || !dataset.Compatible(conditionDoc.Projection.Columns[conditionColumn], a.When.Equals) {
			r.Reason = "incompatible-condition"
			return r
		}
		if !dataset.Equal(v, a.When.Equals) {
			r.Outcome = OutcomeSkipped
			r.Reason = "condition-false"
			return r
		}
	}
	d, rows, reason := selectRows(a.Subject, docs)
	if reason != "" {
		r.Reason = reason
		return r
	}
	r.Count = len(rows)
	// The snapshot identity and selector retain the complete multi-row proof;
	// do not duplicate up to 10000 qualified row IDs in every assertion result.
	if len(rows) == 1 {
		row := rows[0]
		r.Rows = append(r.Rows, d.Binding.Run+"/"+d.Binding.Phase+"/"+d.Binding.Namespace+"/"+d.Binding.Source+"/"+d.Projection.ID+"/"+row.ID)
	}
	if a.Expected != nil {
		column := columnIndex(d, a.Column)
		if column < 0 {
			r.Reason = "unknown-column"
			return r
		}
		if !dataset.Compatible(d.Projection.Columns[column], *a.Expected) {
			r.Reason = "incompatible-expectation"
			return r
		}
	}
	if a.Sequence != nil {
		column := columnIndex(d, a.Column)
		if column < 0 {
			r.Reason = "unknown-column"
			return r
		}
		for _, v := range a.Sequence {
			if !dataset.Compatible(d.Projection.Columns[column], v) {
				r.Reason = "incompatible-expectation"
				return r
			}
		}
	}
	matches := false
	switch a.Operator {
	case "row-count":
		matches = len(rows) == *a.Count
	case "unique-keys":
		keys := map[string]bool{}
		matches = true
		for _, row := range rows {
			values := []dataset.Value{}
			for i, c := range d.Projection.Columns {
				if c.Key {
					if row.Values[i].State != "present" {
						r.Reason = "unusable-key"
						return r
					}
					values = append(values, row.Values[i])
				}
			}
			raw, _ := json.Marshal(values, json.Deterministic(true))
			key := string(raw)
			if keys[key] {
				matches = false
			}
			keys[key] = true
		}
	case "value-equals", "decimal-equals", "instant-equals", "value-related", "value-changed":
		left, why := oneValue(a.Subject, a.Column, docs)
		if why != "" {
			r.Reason = why
			return r
		}
		if a.Operator == "decimal-equals" || a.Operator == "instant-equals" {
			var comparable bool
			matches, comparable = typedSemanticEqual(a.Operator, left, *a.Expected)
			if !comparable {
				r.Reason = "incompatible-value"
				return r
			}
		} else if a.Operator == "value-equals" {
			matches = dataset.Equal(left, *a.Expected)
		} else {
			right, why := oneValue(*a.Other, a.OtherColumn, docs)
			if why != "" {
				r.Reason = "other-" + why
				return r
			}
			if left.State != "present" || right.State != "present" || left.Type != right.Type || (left.Items == nil) != (right.Items == nil) {
				r.Reason = "unusable-relationship"
				return r
			}
			matches = dataset.Equal(left, right)
			if a.Operator == "value-changed" {
				matches = !matches
			}
		}
	case "each-equals", "sequence-equals":
		column := columnIndex(d, a.Column)
		if column < 0 {
			r.Reason = "unknown-column"
			return r
		}
		if a.Operator == "sequence-equals" {
			if d.Projection.Order != "source" || d.Acquisition.Kind == "database" {
				r.Reason = "order-not-declared"
				return r
			}
			matches = len(rows) == len(a.Sequence)
			if matches {
				for i, row := range rows {
					matches = matches && dataset.Equal(row.Values[column], a.Sequence[i])
				}
			}
		} else {
			n := 0
			for _, row := range rows {
				if dataset.Equal(row.Values[column], *a.Expected) {
					n++
				}
			}
			switch a.Quantifier {
			case "every":
				if len(rows) == 0 {
					r.Reason = "no-row"
					return r
				}
				matches = n == len(rows)
			case "any":
				matches = n > 0
			case "none":
				matches = n == 0
			}
		}
	}
	r.Outcome = decide(matches)
	r.Reason = "decided"
	return r
}
func overallVerdict(passed, failed, undecided int) Verdict {
	if failed > 0 {
		return VerdictFail
	}
	if undecided > 0 || passed == 0 {
		return VerdictUndecided
	}
	return VerdictPass
}

func typedSemanticEqual(operator string, a, b dataset.Value) (bool, bool) {
	if a.State != "present" || b.State != "present" || a.Items != nil || b.Items != nil {
		return false, false
	}
	if operator == "decimal-equals" {
		if a.Type != "decimal" || b.Type != "decimal" {
			return false, false
		}
		x, ok := new(big.Rat).SetString(a.Text)
		y, other := new(big.Rat).SetString(b.Text)
		if !ok || !other {
			return false, false
		}
		return x.Cmp(y) == 0, true
	}
	if a.Type != "datetime" || b.Type != "datetime" || a.Timezone == "absent" || b.Timezone == "absent" {
		return false, false
	}
	for _, v := range []dataset.Value{a, b} {
		if strings.HasPrefix(v.Precision, "fraction-") {
			n, err := strconv.Atoi(strings.TrimPrefix(v.Precision, "fraction-"))
			if err != nil || n > 9 {
				return false, false
			}
		}
	}
	parse := func(s string) (time.Time, bool) {
		for _, layout := range []string{time.RFC3339Nano, "20060102150405-0700"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	x, ok := parse(a.Text)
	y, other := parse(b.Text)
	if !ok || !other {
		return false, false
	}
	return x.Equal(y), true
}
