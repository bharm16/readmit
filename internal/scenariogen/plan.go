// Package scenariogen materializes bounded synthetic workflow variants. It
// records every input separately from the unchanged case and scenario formats.
package scenariogen

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/bharm16/readmit/internal/scenario"
)

const (
	Schema   = "readmit-scenario-generator/v1"
	Version  = "readmit-scenario-generator-v1"
	MaxBytes = 256 << 10
)

type Plan struct {
	Schema           string         `json:"schema"`
	GeneratorVersion string         `json:"generator_version"`
	Seed             uint64         `json:"seed"`
	Template         jsontext.Value `json:"template"`
	Rows             []Row          `json:"rows"`
	Variants         []Variant      `json:"variants"`
}

type Row struct {
	ID          string   `json:"id"`
	PatientName string   `json:"patient_name"`
	Notes       []string `json:"notes"`
	Encoding    string   `json:"encoding"`
}

type Variant struct {
	ID        string     `json:"id"`
	Mutations []Mutation `json:"mutations"`
}

// Mutation is a closed union. Each operator's own vocabulary is enforced by
// Decode; omitted and null members never become an implied default.
type Mutation struct {
	Op       string `json:"op"`
	Step     string `json:"step"`
	Field    string `json:"field,omitzero"`
	State    string `json:"state,omitzero"`
	After    string `json:"after,omitzero"`
	Encoding string `json:"encoding,omitzero"`
	Offset   string `json:"offset,omitzero"`
}

func required(data []byte, fields ...string) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return errors.New("expected a JSON object")
	}
	for _, key := range fields {
		v, ok := members[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return errors.New("generator document requires explicit non-null members")
		}
	}
	return nil
}

func (r *Row) UnmarshalJSON(data []byte) error {
	if err := required(data, "id", "patient_name", "notes", "encoding"); err != nil {
		return err
	}
	var notes struct {
		Notes []jsontext.Value `json:"notes"`
	}
	if err := json.Unmarshal(data, &notes); err != nil {
		return errors.New("invalid notes")
	}
	for _, note := range notes.Notes {
		if bytes.Equal(bytes.TrimSpace(note), []byte("null")) {
			return errors.New("a note cannot be null")
		}
	}
	type wire Row
	var v wire
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid data row members")
	}
	*r = Row(v)
	return nil
}
func (r *Variant) UnmarshalJSON(data []byte) error {
	if err := required(data, "id", "mutations"); err != nil {
		return err
	}
	type wire Variant
	var v wire
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid variant members")
	}
	*r = Variant(v)
	return nil
}
func (m *Mutation) UnmarshalJSON(data []byte) error {
	if err := required(data, "op", "step"); err != nil {
		return err
	}
	type wire Mutation
	var v wire
	if err := json.Unmarshal(data, &v, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid mutation members")
	}
	fields := []string{"op", "step"}
	switch v.Op {
	case "field":
		fields = append(fields, "field", "state")
	case "delay":
		fields = append(fields, "after")
	case "duplicate":
	case "encoding":
		fields = append(fields, "encoding")
	case "timezone":
		fields = append(fields, "offset")
	default:
		return errors.New("unsupported mutation operator")
	}
	if err := required(data, fields...); err != nil {
		return err
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	if len(members) != len(fields) {
		return errors.New("mutation carries members for another operator")
	}
	*m = Mutation(v)
	return nil
}

type template struct {
	scenario.Scenario
	Orders  []scenario.Order
	Results []scenario.Result
}

// Decode validates the entire plan, including the unchanged lifecycle reader.
// Values are never included in errors, because authored data may be sensitive.
func Decode(data []byte) (Plan, error) {
	var p Plan
	if len(data) > MaxBytes {
		return p, errors.New("generator plan exceeds 256 KiB")
	}
	if err := required(data, "schema", "generator_version", "seed", "template", "rows", "variants"); err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p, json.RejectUnknownMembers(true)); err != nil {
		return Plan{}, errors.New("invalid generator plan JSON")
	}
	if p.Schema != Schema || p.GeneratorVersion != Version {
		return Plan{}, errors.New("unsupported generator contract or version")
	}
	d, err := readTemplate(p.Template)
	if err != nil {
		return Plan{}, err
	}
	if len(p.Rows) < 1 || len(p.Rows) > 8 || len(p.Variants) < 1 || len(p.Variants) > 16 {
		return Plan{}, errors.New("require 1-8 rows and 1-16 variants")
	}
	seen := map[string]bool{}
	for _, r := range p.Rows {
		if !validPortableID(r.ID) || seen[r.ID] {
			return Plan{}, errors.New("row ids must be distinct portable names")
		}
		seen[r.ID] = true
		if !validFixtureText(r.PatientName) || len(r.Notes) > 8 || !encoding(r.Encoding) {
			return Plan{}, errors.New("unsupported row text, notes or encoding")
		}
		for _, n := range r.Notes {
			if !validFixtureText(n) {
				return Plan{}, errors.New("unsupported note text")
			}
		}
	}
	steps := map[string]bool{}
	for _, s := range d.Steps {
		steps[s.ID] = true
	}
	seen = map[string]bool{}
	for _, v := range p.Variants {
		if !validPortableID(v.ID) || seen[v.ID] || len(v.Mutations) > 32 {
			return Plan{}, errors.New("variant ids must be distinct portable names with at most 32 mutations")
		}
		seen[v.ID] = true
		targets := map[string]bool{}
		for _, m := range v.Mutations {
			if !steps[m.Step] {
				return Plan{}, errors.New("mutation names an undeclared step")
			}
			key := m.Op + "/" + m.Step + "/" + m.Field
			if targets[key] {
				return Plan{}, errors.New("a mutation target is repeated")
			}
			targets[key] = true
			switch m.Op {
			case "field":
				if m.Field != "PID-5" && m.Field != "NTE-3" {
					return Plan{}, errors.New("field mutations support PID-5 and NTE-3 only")
				}
				if m.State != "absent" && m.State != "empty" && m.State != "null" {
					return Plan{}, errors.New("unsupported field state")
				}
				if m.Field == "NTE-3" {
					for _, r := range p.Rows {
						if len(r.Notes) == 0 {
							return Plan{}, errors.New("NTE mutation requires notes in every row")
						}
					}
				}
			case "delay":
				if _, err := delay(m.After); err != nil {
					return Plan{}, err
				}
			case "timezone":
				if _, err := zone(m.Offset); err != nil {
					return Plan{}, err
				}
			case "encoding":
				if !encoding(m.Encoding) {
					return Plan{}, errors.New("unsupported encoding")
				}
			}
		}
	}
	return p, nil
}

func readTemplate(data []byte) (template, error) {
	if _, err := scenario.PreviewDocument(data); err != nil {
		return template{}, errors.New("generator requires a valid scenario with confirmed lifecycle outcomes")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return template{}, err
	}
	if header.Schema == scenario.OrderSchema {
		d, err := scenario.DecodeOrders(data)
		return template{Scenario: scenario.Scenario{Schema: d.Schema, Scenario: d.Scenario, Profile: d.Profile, BaseTime: d.BaseTime, Subjects: d.Subjects, Steps: d.Steps}, Orders: d.Orders, Results: d.Results}, err
	}
	d, err := scenario.Decode(data)
	return template{Scenario: d}, err
}

var portable = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func validPortableID(s string) bool { return portable.MatchString(s) }
func validFixtureText(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune("|^~\\&\"", r) {
			return false
		}
	}
	return true
}
func encoding(s string) bool { return s == "utf-8" || s == "iso-8859-1" }
func delay(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 || d > 8760*time.Hour || d%time.Second != 0 {
		return 0, errors.New("delay must be positive whole seconds no greater than 8760h")
	}
	return d, nil
}
func zone(s string) (*time.Location, error) {
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' || s[1] < '0' || s[1] > '9' || s[2] < '0' || s[2] > '9' || s[4] < '0' || s[4] > '5' || s[5] < '0' || s[5] > '9' {
		return nil, errors.New("timezone requires a signed HH:MM offset")
	}
	t, err := time.Parse("-07:00", s)
	if err != nil {
		return nil, errors.New("invalid timezone offset")
	}
	_, offset := t.Zone()
	if offset > 14*3600 || offset < -14*3600 {
		return nil, errors.New("timezone exceeds 14 hours")
	}
	return time.FixedZone("", offset), nil
}
