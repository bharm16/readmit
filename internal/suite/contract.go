// Package suite binds reusable test templates to explicit environment mappings
// and typed data tables, then executes through the existing durable queue.
package suite

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testrunner"
)

const Schema = "readmit-suite/v1"
const MaxBytes = 1 << 20

// Document organizes templates; environment bindings cannot change expectations.
// Data rows may replace only the case and explicitly named typed expected values.
type Document struct {
	Schema       string        `json:"schema"`
	ID           string        `json:"id"`
	Owner        string        `json:"owner"`
	Tags         []string      `json:"tags"`
	Parallelism  int           `json:"parallelism"`
	Environments []Environment `json:"environments"`
	Tables       []Table       `json:"tables"`
	Tests        []Test        `json:"tests"`
}
type Environment struct {
	ID       string    `json:"id"`
	Site     string    `json:"site"`
	Bindings []Binding `json:"bindings"`
}
type Binding struct {
	Parameter   string `json:"parameter"`
	Target      string `json:"target"`
	Observation string `json:"observation,omitzero"`
}
type Table struct {
	ID   string `json:"id"`
	Rows []Row  `json:"rows"`
}
type Row struct {
	ID       string                      `json:"id"`
	Case     string                      `json:"case"`
	Expected map[string]testrunner.Value `json:"expected,omitzero"`
}
type Test struct {
	ID        string             `json:"id"`
	Spec      string             `json:"spec"`
	Owner     string             `json:"owner"`
	Tags      []string           `json:"tags"`
	Parameter string             `json:"parameter"`
	Table     string             `json:"table"`
	Isolation runqueue.Isolation `json:"isolation"`
	Sequence  []string           `json:"sequence"`
	After     []string           `json:"after,omitzero"`
}

// required checks presence before strict decoding, including nested custom
// readers. Explicit null is never a declaration of an empty list or value.
func required(data []byte, into any, names ...string) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("invalid suite JSON")
	}
	for _, value := range members {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("suite members cannot be null")
		}
	}
	for _, name := range names {
		if len(members[name]) == 0 {
			return errors.New("suite is missing a required member")
		}
	}
	if err := json.Unmarshal(data, into, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid or unsupported suite member")
	}
	return nil
}
func (v *Document) UnmarshalJSON(data []byte) error {
	type plain Document
	return required(data, (*plain)(v), "schema", "id", "owner", "tags", "parallelism", "environments", "tables", "tests")
}
func (v *Environment) UnmarshalJSON(data []byte) error {
	type plain Environment
	return required(data, (*plain)(v), "id", "site", "bindings")
}
func (v *Binding) UnmarshalJSON(data []byte) error {
	type plain Binding
	return required(data, (*plain)(v), "parameter", "target")
}
func (v *Table) UnmarshalJSON(data []byte) error {
	type plain Table
	return required(data, (*plain)(v), "id", "rows")
}
func (v *Row) UnmarshalJSON(data []byte) error {
	type plain Row
	return required(data, (*plain)(v), "id", "case")
}
func (v *Test) UnmarshalJSON(data []byte) error {
	type plain Test
	return required(data, (*plain)(v), "id", "spec", "owner", "tags", "parameter", "table", "isolation", "sequence")
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var occurrence = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)

func text(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func local(s string) bool { return text(s, 4096) && filepath.IsLocal(s) }
func unique(values []string, valid func(string) bool) bool {
	seen := map[string]bool{}
	for _, s := range values {
		if !valid(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func tags(values []string) bool { return len(values) <= 32 && unique(values, identifier.MatchString) }

func Decode(data []byte) (Document, error) {
	var d Document
	if len(data) > MaxBytes || json.Unmarshal(data, &d, json.RejectUnknownMembers(true)) != nil {
		return Document{}, errors.New("invalid suite JSON or size")
	}
	if err := d.validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}
func (d Document) validate() error {
	invalid := errors.New("invalid suite declarations or references")
	if d.Schema != Schema || !identifier.MatchString(d.ID) || !text(d.Owner, 256) || !tags(d.Tags) || len(d.Environments) < 1 || len(d.Environments) > 32 || len(d.Tables) < 1 || len(d.Tables) > 64 || len(d.Tests) < 1 || len(d.Tests) > 64 {
		return invalid
	}
	tables := map[string]Table{}
	for _, table := range d.Tables {
		if !identifier.MatchString(table.ID) || len(table.Rows) < 1 || len(table.Rows) > 64 {
			return invalid
		}
		if _, found := tables[table.ID]; found {
			return invalid
		}
		tables[table.ID] = table
		rows := map[string]bool{}
		for _, row := range table.Rows {
			if !identifier.MatchString(row.ID) || rows[row.ID] || !local(row.Case) || len(row.Expected) > testrunner.MaxAssertions {
				return invalid
			}
			rows[row.ID] = true
			for id := range row.Expected {
				if !identifier.MatchString(id) {
					return invalid
				}
			}
		}
	}
	tests := map[string]bool{}
	parameters := map[string]bool{}
	for _, test := range d.Tests {
		if !identifier.MatchString(test.ID) || tests[test.ID] || !local(test.Spec) || !text(test.Owner, 256) || !tags(test.Tags) || !identifier.MatchString(test.Parameter) || len(test.Sequence) < 1 || len(test.Sequence) > 4000 || !unique(test.Sequence, occurrence.MatchString) {
			return invalid
		}
		if _, found := tables[test.Table]; !found {
			return invalid
		}
		tests[test.ID] = true
		parameters[test.Parameter] = true
	}
	environments := map[string]bool{}
	for _, env := range d.Environments {
		if !identifier.MatchString(env.ID) || environments[env.ID] || !text(env.Site, 256) || len(env.Bindings) != len(parameters) {
			return invalid
		}
		environments[env.ID] = true
		bound := map[string]bool{}
		for _, b := range env.Bindings {
			if !parameters[b.Parameter] || bound[b.Parameter] || !local(b.Target) || b.Observation != "" && !local(b.Observation) {
				return invalid
			}
			bound[b.Parameter] = true
		}
	}
	_, err := d.queue()
	return err
}

// Each template dependency means all of that setup's data rows must pass.
// runqueue owns cycle, duplicate, isolation, identifier and total-job bounds.
func (d Document) queue() (runqueue.Plan, error) {
	ids := map[string][]string{}
	for _, test := range d.Tests {
		for _, table := range d.Tables {
			if table.ID == test.Table {
				for _, row := range table.Rows {
					ids[test.ID] = append(ids[test.ID], test.ID+"-"+row.ID)
				}
			}
		}
	}
	q := runqueue.Plan{Schema: runqueue.PlanSchema, Parallelism: d.Parallelism}
	for _, test := range d.Tests {
		var after []string
		for _, dep := range test.After {
			if len(ids[dep]) == 0 {
				return q, errors.New("suite depends on an undeclared test")
			}
			after = append(after, ids[dep]...)
		}
		for _, id := range ids[test.ID] {
			q.Jobs = append(q.Jobs, runqueue.Job{ID: id, Spec: id + ".json", Isolation: test.Isolation, After: after})
		}
	}
	raw, err := json.Marshal(q)
	if err != nil {
		return q, err
	}
	return runqueue.DecodePlan(raw)
}
