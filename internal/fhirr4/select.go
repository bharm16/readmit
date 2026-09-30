package fhirr4

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/dataset"
)

type selected struct {
	value, metadata, parent *node
	typ, key, parentType    string
	pointer                 string
}

func (d *Document) Select(ctx context.Context, occurrenceID string, selector Selector) Selection {
	out := Selection{State: "absent", Readings: []Reading{}}
	resource := d.resource(occurrenceID)
	if resource == nil || selector.Validate() != nil {
		out.State = "invalid"
		return out
	}
	if !supported(resource.resource.Type) {
		out.State = "unsupported"
		return out
	}
	current := []selected{{value: resource.node, typ: resource.resource.Type, pointer: resource.node.pointer}}
	for _, step := range selector.Steps {
		fieldName := step.Field
		if step.Each && step.Index != nil || step.Index != nil && *step.Index < 0 {
			out.State = "invalid"
			return out
		}
		choice := strings.HasSuffix(fieldName, "[x]")
		plain := strings.TrimSuffix(strings.TrimPrefix(fieldName, "_"), "[x]")
		if !name.MatchString(plain) {
			out.State = "invalid"
			return out
		}
		next := []selected{}
		for _, parent := range current {
			if ctx.Err() != nil || len(next) > MaxMatches {
				out.State = "unsupported"
				return out
			}
			def := definitions[parent.typ]
			if def == nil {
				out.State = "unsupported"
				return out
			}
			keys := []string{fieldName}
			if choice {
				keys = nil
				for key, f := range def {
					if f.choice == plain {
						keys = append(keys, key)
					}
				}
				sort.Strings(keys)
				if len(keys) == 0 {
					out.State = "unsupported"
					return out
				}
			}
			foundChoices := 0
			for _, key := range keys {
				f, known := def[key]
				metadataSelection := false
				if strings.HasPrefix(key, "_") {
					original, ok := def[strings.TrimPrefix(key, "_")]
					known = ok && primitive(original.typ)
					f = field{typ: "PrimitiveMetadata", many: original.many}
					metadataSelection = true
				}
				if !known {
					out.State = "unsupported"
					return out
				}
				value, metadata := parent.value.field(key), parent.value.field("_"+key)
				if metadataSelection {
					metadata = nil
				}
				if value == nil && metadata == nil {
					if !choice {
						if len(next) >= MaxMatches {
							out.State = "unsupported"
							return out
						}
						next = append(next, selected{typ: f.typ, key: key, parentType: parent.typ, parent: parent.value, pointer: pointer(parent.pointer, key)})
					}
					continue
				}
				foundChoices++
				if f.many {
					if value != nil && value.kind != '[' || metadata != nil && metadata.kind != '[' {
						out.State = "invalid"
						return out
					}
					count := max(len(value.array()), len(metadata.array()))
					indexes := []int{}
					if step.Index != nil {
						if *step.Index < count {
							indexes = append(indexes, *step.Index)
						}
					} else {
						for i := 0; i < count; i++ {
							indexes = append(indexes, i)
						}
					}
					if !step.Each && step.Index == nil && count > 1 {
						out.State = "ambiguous"
						return out
					}
					for _, i := range indexes {
						v, m := item(value, i), item(metadata, i)
						if primitive(f.typ) || metadataSelection {
							if v != nil && v.kind == 'n' {
								v = nil
							}
							if m != nil && m.kind == 'n' {
								m = nil
							}
						}
						path := pointer(pointer(parent.pointer, key), strconv.Itoa(i))
						if len(next) >= MaxMatches {
							out.State = "unsupported"
							return out
						}
						next = append(next, selected{value: v, metadata: m, parent: parent.value, typ: f.typ, key: key, parentType: parent.typ, pointer: path})
					}
				} else {
					if step.Each || step.Index != nil {
						out.State = "invalid"
						return out
					}
					if len(next) >= MaxMatches {
						out.State = "unsupported"
						return out
					}
					next = append(next, selected{value: value, metadata: metadata, parent: parent.value, typ: f.typ, key: key, parentType: parent.typ, pointer: pointer(parent.pointer, key)})
				}
			}
			if choice && foundChoices > 1 {
				out.State = "invalid"
				return out
			}
		}
		current = next
	}
	for _, element := range current {
		reading := Reading{Datatype: element.typ, Pointer: element.pointer, source: element.value}
		if element.value != nil {
			reading.Raw = bytes.Clone(d.raw[element.value.start:element.value.end])
		}
		if element.metadata != nil {
			reading.Companion = bytes.Clone(d.raw[element.metadata.start:element.metadata.end])
		}
		if primitive(element.typ) {
			if element.typ == "canonical" && element.value != nil {
				c, err := ParseCanonical(element.value.string())
				if err == nil {
					reading.Canonical = &c
				}
			}
			reading.Value = scalar(element.typ, element.value)
			if reading.Value.Type == "code" {
				reading.Value.CodeSystem = codeSystem(element.parentType, element.key, element.parent)
			}
		} else {
			reading.Value = dataset.Value{State: "present"}
			if element.value == nil {
				reading.Value.State = "absent"
			}
		}
		out.Readings = append(out.Readings, reading)
	}
	if len(out.Readings) > 1 {
		out.State = "multiple"
	} else if len(out.Readings) == 1 {
		out.State = out.Readings[0].Value.State
	}
	for _, reading := range out.Readings {
		if reading.Value.State == "invalid" {
			out.State = "invalid"
		}
		if reading.Value.State == "unsupported" && out.State != "invalid" {
			out.State = "unsupported"
		}
	}
	if resource.invalid {
		out.State = "invalid"
	} else if resource.unsupported {
		out.State = "unsupported"
	}
	return out
}

// MatchingResources preserves occurrence multiplicity. Business identifiers do
// not become row keys; an empty system never matches a qualified identifier.
func (d *Document) MatchingResources(typ string, business *BusinessID) []Resource {
	out := []Resource{}
	for _, entry := range d.resources {
		r := entry.resource
		if r.Type != typ {
			continue
		}
		if business != nil {
			found := false
			for _, id := range r.Identifiers {
				if id == *business && id.System != "" && id.Value != "" {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, clone(r))
	}
	return out
}

// Validate accepts only finite field/choice/index/all steps, never expressions.
func (s Selector) Validate() error {
	if len(s.Steps) < 1 || len(s.Steps) > 16 {
		return invalid
	}
	for _, step := range s.Steps {
		plain := strings.TrimSuffix(strings.TrimPrefix(step.Field, "_"), "[x]")
		if !name.MatchString(plain) || step.Each && step.Index != nil || step.Index != nil && (*step.Index < 0 || *step.Index >= MaxMatches) {
			return invalid
		}
	}
	return nil
}

// String names the finite typed position for readers and reviewed edit plans.
// It is a label, never a FHIRPath expression or an alternative parser.
func (s Selector) String() string {
	var label strings.Builder
	for i, step := range s.Steps {
		if i > 0 {
			label.WriteByte('.')
		}
		label.WriteString(step.Field)
		if step.Each {
			label.WriteString("[]")
		} else if step.Index != nil {
			label.WriteString("[" + strconv.Itoa(*step.Index) + "]")
		}
	}
	return label.String()
}
