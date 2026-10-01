package fhirr4

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// ProjectionField is a finite supported primitive position. A picker uses
// the same definitions as Select; it cannot submit a FHIRPath expression.
type ProjectionField struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	CodeSystem string   `json:"code_system,omitzero"`
	Selector   Selector `json:"selector"`
	Repeated   bool     `json:"repeated"`
}

// InspectionFields adds concrete positions for repeated values actually held
// by one resource. The same Select reading supplies the indexes; a webview
// never constructs selectors from pointers or interprets repetition itself.
func (d *Document) InspectionFields(ctx context.Context, id string) ([]ProjectionField, bool) {
	resource := d.resource(id)
	if resource == nil {
		return []ProjectionField{}, true
	}
	base := ProjectionFields(resource.resource.Type)
	fields := append([]ProjectionField(nil), base...)
	seen := map[string]bool{}
	for _, field := range base {
		seen[field.ID] = true
	}
	for _, field := range base {
		if !field.Repeated {
			continue
		}
		selection := d.Select(ctx, id, field.Selector)
		for _, reading := range selection.Readings {
			if ctx.Err() != nil || len(fields) >= MaxMatches {
				return fields, false
			}
			parts := strings.Split(strings.TrimPrefix(reading.Pointer, resource.resource.Pointer+"/"), "/")
			at, valid := 0, true
			steps := append([]Step(nil), field.Selector.Steps...)
			for i, step := range steps {
				if at >= len(parts) || parts[at] != step.Field {
					valid = false
					break
				}
				at++
				if step.Each || step.Index != nil {
					if at >= len(parts) {
						valid = false
						break
					}
					index, err := strconv.Atoi(parts[at])
					at++
					if err != nil || index < 0 {
						valid = false
						break
					}
					steps[i].Each, steps[i].Index = false, &index
				}
			}
			if !valid || at != len(parts) {
				continue
			}
			selector := Selector{Steps: steps}
			name := selector.String()
			if seen[name] {
				continue
			}
			seen[name] = true
			concrete := field
			concrete.ID, concrete.Selector, concrete.Repeated = name, selector, false
			fields = append(fields, concrete)
		}
	}
	return fields, true
}

func ProjectionFields(resource string) []ProjectionField {
	out := []ProjectionField{}
	if !supported(resource) {
		return out
	}
	var walk func(string, []Step, string, int, bool)
	walk = func(typ string, steps []Step, prefix string, depth int, repeated bool) {
		if depth > 3 {
			return
		}
		keys := []string{}
		for key := range definitions[typ] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "extension" || key == "modifierExtension" || key == "contained" || key == "text" {
				continue
			}
			f := definitions[typ][key]
			stepsOffered := []Step{{Field: key}}
			if f.many {
				zero := 0
				stepsOffered = []Step{{Field: key, Each: true}, {Field: key, Index: &zero}}
			}
			for _, step := range stepsOffered {
				next := append(append([]Step(nil), steps...), step)
				label := prefix + key
				if step.Each {
					label += "[]"
				} else if step.Index != nil {
					label += "[0]"
				}
				many := repeated || step.Each
				if primitive(f.typ) {
					v := scalar(f.typ, nil)
					system := codeSystem(typ, key, nil)
					if v.Type == "code" && system == "" {
						continue
					}
					out = append(out, ProjectionField{ID: label, Type: v.Type, CodeSystem: system, Selector: Selector{Steps: next}, Repeated: many})
				} else {
					walk(f.typ, next, label+".", depth+1, many)
				}
			}
		}
	}
	walk(resource, nil, "", 0, false)
	return out
}

// ProjectionResources are resource types this interpreter can project.
func ProjectionResources() []string {
	return []string{"Appointment", "DiagnosticReport", "Encounter", "Location", "Observation", "Patient", "Practitioner", "ServiceRequest"}
}
