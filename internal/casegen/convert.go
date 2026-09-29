package casegen

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/bharm16/readmit/internal/scenariogen"
)

// Settings are what a saved readmit-scenario-generator/v1 plan does not
// declare and a generation request requires: the pinned profile and pack, the
// wire form, the business bindings, and any variants a variant flow adds
// beside the plan's own.
type Settings struct {
	Profile     Pin       `json:"profile"`
	Pack        Pin       `json:"pack"`
	Wire        Wire      `json:"wire"`
	Bindings    Bindings  `json:"bindings"`
	Variants    []Variant `json:"variants"`
	DerivedFrom string    `json:"derived_from,omitzero"`
}

// Clause is one member of a saved plan this generator cannot carry, by its
// place in the plan and why.
type Clause struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// UnconvertibleError is a conversion refused because the plan declares
// clauses a request cannot carry. Nothing is dropped to make it fit: the plan
// is converted whole or not at all.
type UnconvertibleError struct{ Clauses []Clause }

func (e *UnconvertibleError) Error() string {
	parts := make([]string, len(e.Clauses))
	for i, c := range e.Clauses {
		parts[i] = c.Path + ": " + c.Reason
	}
	return "the saved plan declares clauses this generator cannot carry: " + strings.Join(parts, "; ")
}

var v1Charsets = map[string]string{"utf-8": UTF8, "iso-8859-1": Latin1}

// FromPlan converts a saved generator plan, with the settings it lacks, into a
// generation request. Every row and every variant of the plan is carried:
// the embedded scenario unchanged, the seed, each row's name, notes and
// encoding, and each variant's mutations. A variant that only changes
// encoding, time zone or arrival is declared positive; any other is declared
// negative, because the plan never said its outcome. The settings' variants
// follow the plan's. A clause without an equivalent refuses the whole
// conversion with an *UnconvertibleError naming it.
func FromPlan(data []byte, settings Settings) ([]byte, error) {
	plan, err := scenariogen.Decode(data)
	if err != nil {
		return nil, err
	}
	clauses := []Clause{}
	rows := make([]Row, 0, len(plan.Rows))
	notes := -1
	for i, r := range plan.Rows {
		charset, ok := v1Charsets[r.Encoding]
		if !ok {
			clauses = append(clauses, Clause{Path: fmt.Sprintf("rows[%d].encoding", i), Reason: "no MSH-18 character set equals it"})
		}
		rows = append(rows, Row{ID: r.ID, Family: r.PatientName, Given: "", Notes: append([]string{}, r.Notes...), Charset: charset})
		if notes >= 0 && notes != len(r.Notes) {
			notes = -2
		} else if notes == -1 {
			notes = len(r.Notes)
		}
	}
	variants := make([]Variant, 0, len(plan.Variants)+len(settings.Variants))
	for i, v := range plan.Variants {
		out := Variant{ID: v.ID, Polarity: Positive, Mutations: []Mutation{}}
		for k, m := range v.Mutations {
			path := fmt.Sprintf("variants[%d].mutations[%d]", i, k)
			switch m.Op {
			case "duplicate":
				out.Mutations = append(out.Mutations, Mutation{Op: DuplicateOp, Step: m.Step})
				out.Polarity = Negative
			case "delay":
				out.Mutations = append(out.Mutations, Mutation{Op: DelayOp, Step: m.Step, After: m.After})
			case "encoding":
				out.Mutations = append(out.Mutations, Mutation{Op: CharsetOp, Step: m.Step, Charset: v1Charsets[m.Encoding]})
			case "timezone":
				out.Mutations = append(out.Mutations, Mutation{Op: OffsetOp, Step: m.Step, Offset: m.Offset})
			case "field":
				out.Polarity = Negative
				if m.Field == "PID-5" {
					out.Mutations = append(out.Mutations, Mutation{Op: FieldOp, Step: m.Step, Selector: "PID-5", State: m.State})
					continue
				}
				// The plan changed every NTE of the step; a request names each.
				if notes < 1 {
					clauses = append(clauses, Clause{Path: path, Reason: "an NTE-3 mutation over rows without the same number of notes names no one NTE per row"})
					continue
				}
				for n := 1; n <= notes; n++ {
					out.Mutations = append(out.Mutations, Mutation{Op: FieldOp, Step: m.Step, Selector: fmt.Sprintf("NTE[%d]-3", n), State: m.State})
				}
			default:
				clauses = append(clauses, Clause{Path: path, Reason: "the operator has no equivalent"})
			}
		}
		variants = append(variants, out)
	}
	if len(clauses) > 0 {
		return nil, &UnconvertibleError{Clauses: clauses}
	}
	variants = append(variants, settings.Variants...)
	request := Request{Schema: Schema, GeneratorVersion: Version, Scenario: plan.Template, Profile: settings.Profile, Pack: settings.Pack, Seed: plan.Seed,
		Wire: settings.Wire, Bindings: settings.Bindings, Rows: rows, Variants: variants, DerivedFrom: settings.DerivedFrom}
	encoded, err := json.Marshal(request, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the converted request")
	}
	if _, _, err := Decode(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}
