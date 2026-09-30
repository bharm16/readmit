package fhirr4

import (
	"sort"
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
