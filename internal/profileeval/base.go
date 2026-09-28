package profileeval

import (
	"fmt"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"slices"
)

func (e *evaluator) baseRule() *MessageRule {
	name := string(e.read("MSH-9.3").Decoded)
	if name == "" {
		name = string(e.read("MSH-9.1").Decoded) + "_" + string(e.read("MSH-9.2").Decoded)
	}
	for i := range e.pack.Messages {
		m := &e.pack.Messages[i]
		if m.HL7Version == e.profile.Definition.Base.HL7Version && m.Family == e.profile.Definition.Base.Family && m.Structure == name {
			return m
		}
	}
	return nil
}
func (e *evaluator) localOrigin(segment string, position int) string {
	if m := e.baseRule(); m != nil {
		for _, s := range m.Segments {
			if s.ID == segment && slices.ContainsFunc(s.Fields, func(f FieldRule) bool { return f.Position == position }) {
				return "overridden"
			}
		}
	}
	return "local"
}
func (e *evaluator) evaluateBase() {
	m := e.baseRule()
	if m == nil {
		e.report.BaseSupport = "unsupported"
		if e.components() {
			e.add("base-message-structure-unavailable", "profile", "unsupported", "MSH-9", e.read("MSH-9"))
		}
		return
	}
	e.structure(m.Sequence, "profile")
	v5 := e.pack.Schema == PackSchemaV5
	for _, decl := range m.Segments {
		occ := 0
		for _, segment := range e.doc.Messages[0].Segments {
			if segment.ID != decl.ID {
				continue
			}
			occ++
			unclassified, lengthUnavailable := false, false
			for _, f := range decl.Fields {
				if e.err != nil || e.overflow {
					return
				}
				var override *localprofile.Field
				for _, local := range e.profile.Definition.Segments {
					if local.ID == decl.ID {
						for i := range local.Fields {
							if local.Fields[i].Position == f.Position {
								field := local.Fields[i]
								override = &field
							}
						}
					}
				}

				sel := fmt.Sprintf("%s[%d]-%d", decl.ID, occ, f.Position)
				r := e.read(sel)
				field := segment.Field(f.Position)
				if override == nil {
					usage := usageOf(f.Usage, f.Required, false, true)
					unclassified = unclassified || usage == "unclassified"
					if v5 {
						e.usage(usage, f.Condition, "field", scopeOf(fmt.Sprintf("%s[%d]-", decl.ID, occ), false), sel, "profile", r)
					} else if f.Required && r.State != hl7.Present {
						e.add("required", "profile", "fail", sel, r)
					}
				}
				if (override == nil || override.Cardinality == nil) && f.MaxRepetitions != 0 && len(field.Repetitions) > f.MaxRepetitions {
					e.add("field-cardinality", "profile", "fail", sel, r)
				}
				for rep := 1; rep <= len(field.Repetitions); rep++ {
					s := fmt.Sprintf("%s[%d]-%d[%d]", decl.ID, occ, f.Position, rep)
					r := e.read(s)
					if r.State != hl7.Present {
						continue
					}
					if r.Reason != "" {
						e.add(string(r.Reason), "profile", "unsupported", s, r)
						continue
					}
					if v5 {
						e.length(f.Length, "field", s, "profile", r)
						lengthUnavailable = lengthUnavailable || f.Length.State == "unavailable"
						if code := r; f.Table != "" {
							// A coded composite carries its code in the first component.
							if first := e.read(s + ".1"); first.Reason == "" && first.State == hl7.Present {
								code = first
							}
							e.binding(f.Table, f.TableKind, f.Policy, f.Codes, "field", s, "profile", code)
						}
					} else if f.MaxLength > 0 && len(r.Decoded) > f.MaxLength {
						e.add("field-length", "profile", "fail", s, r)
					}
					if override == nil || override.Type == "" {
						e.evaluateDatatype(f.DataType, s, "profile", r, 0)
					}
				}
			}
			// One finding per segment occurrence: a field the source cannot
			// classify may be conditional, absent or present.
			whole := hl7.Reading{Value: hl7.Value{Span: segment.Span, State: hl7.Present}}
			if unclassified {
				e.add("base-field-usage-unclassified", "profile", "unsupported", fmt.Sprintf("%s[%d]", decl.ID, occ), whole)
			}
			if lengthUnavailable {
				e.add("base-field-length-unavailable", "profile", "unsupported", fmt.Sprintf("%s[%d]", decl.ID, occ), whole)
			}
		}
	}
}
