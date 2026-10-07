package hl7reference

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type PreviewAttribute string

type PreviewKind string

const (
	DatatypePreview  PreviewKind = "datatype"
	TablePreview     PreviewKind = "table"
	ElementPreview   PreviewKind = "element"
	SectionPreview   PreviewKind = "section"
	AttributePreview PreviewKind = "attribute"
	TablesPreview    PreviewKind = "tables"
)

const (
	PreviewDatatype          PreviewAttribute = "datatype"
	PreviewTable             PreviewAttribute = "table"
	PreviewItem              PreviewAttribute = "item"
	PreviewSection           PreviewAttribute = "section"
	PreviewOptionality       PreviewAttribute = "optionality"
	PreviewLength            PreviewAttribute = "length"
	PreviewConformanceLength PreviewAttribute = "conformance_length"
	PreviewRepetition        PreviewAttribute = "repetition"
)

// Preview is reference-only presentation. It contains no evidence values and
// never interprets an attribute as a validation result. Context remains the
// source field/component even when the card describes another reference entity.
type Preview struct {
	Kind          PreviewKind   `json:"kind"`
	Code          string        `json:"code"`
	Title         string        `json:"title"`
	Definition    string        `json:"definition"`
	MaximumLength string        `json:"maximum_length,omitzero"`
	DatatypeName  string        `json:"datatype_name,omitzero"`
	TargetKey     string        `json:"target_key,omitzero"`
	Context       *Record       `json:"context,omitzero"`
	Tables        []PreviewLink `json:"tables,omitzero"`
	Uses          []PreviewLink `json:"uses,omitzero"`
}

type PreviewLink struct {
	Key  string `json:"key"`
	Code string `json:"code"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// AttributePreview resolves the relationship in the same catalog read as the
// resulting entity. The webview does not reconstruct reference keys or borrow a
// field definition for a missing datatype, component, table or data element.
func (c *Catalog) AttributePreview(version, key string, attribute PreviewAttribute, offset, limit int, query string) (Preview, Answer, []Record, int, int, error) {
	if len(key) > 128 || offset < 0 || limit < 1 || limit > 100 || len(query) > 128 {
		return Preview{}, Answer{}, nil, 0, 0, errors.New("reference preview must name a bounded entity window and search")
	}
	kind := map[PreviewAttribute]PreviewKind{PreviewDatatype: DatatypePreview, PreviewTable: TablePreview, PreviewItem: ElementPreview, PreviewSection: SectionPreview, PreviewOptionality: AttributePreview, PreviewLength: AttributePreview, PreviewConformanceLength: AttributePreview, PreviewRepetition: AttributePreview}[attribute]
	if kind == "" {
		return Preview{}, Answer{}, nil, 0, 0, errors.New("unknown reference preview attribute")
	}
	owner, _, _, err := c.Entity(version, key, 0, 1)
	if err != nil {
		return Preview{}, Answer{}, nil, 0, 0, err
	}
	p := Preview{Kind: kind, Context: owner.Record}
	if owner.Record == nil {
		return p, owner, nil, 0, 0, nil
	}
	r := owner.Record
	var a Attribute
	switch attribute {
	case "datatype":
		a, p.TargetKey = r.Datatype, owner.DatatypeKey
	case "table":
		a = r.Table
		if len(owner.TableKeys) == 1 {
			p.TargetKey = owner.TableKeys[0]
		}
	case "item":
		a, p.TargetKey = r.Item, owner.ElementKey
	case "section":
		a, p.TargetKey = r.Section, owner.SectionKey
	case "optionality":
		a = r.Optionality
	case "length":
		a = r.Length
	case "conformance_length":
		a = r.ConformanceLength
	case "repetition":
		a = r.Repetition
	default:
		return Preview{}, Answer{}, nil, 0, 0, errors.New("unknown reference preview attribute")
	}
	p.Code = a.Value
	if a.State != "specified" || attribute == "optionality" || attribute == "length" || attribute == "conformance_length" || attribute == "repetition" {
		p.Kind = "attribute"
		p.Title, p.Definition = attributeExplanation(attribute, a)
		p.TargetKey = ""
		return p, owner, []Record{}, 0, 0, nil
	}
	for _, key := range owner.TableKeys {
		if table, ok := c.records[key]; ok && len(p.Tables) < 16 {
			p.Tables = append(p.Tables, PreviewLink{Key: key, Code: table.TableID, Name: table.Name, Kind: table.TableKind})
		}
	}
	if p.TargetKey == "" {
		if attribute == "table" && len(p.Tables) > 1 {
			p.Kind = "tables"
			p.Title = "Referenced tables"
			p.Definition = "This position references more than one table. Choose a table to open its complete reference."
			return p, owner, []Record{}, 0, 0, nil
		}
		owner.Record = nil
		owner.Status = "not_available"
		owner.Reason = "This reference is not available as one entity in the selected catalog."
		return p, owner, []Record{}, 0, 0, nil
	}
	answer, children, count, total, err := c.EntitySearch(version, p.TargetKey, offset, limit, query)
	if err != nil {
		return Preview{}, Answer{}, nil, 0, 0, err
	}
	if answer.Record != nil {
		p.Title = answer.Record.Name
		p.Definition, p.MaximumLength = definitionPreview(answer.Record.Definition)
		if attribute == "item" {
			p.Kind = "element"
			if datatype, ok := c.records["datatype/"+answer.Record.Datatype.Value]; ok && answer.Record.Datatype.State == "specified" {
				p.DatatypeName = datatype.Name
			}
			for _, key := range answer.Record.Uses[:min(5, len(answer.Record.Uses))] {
				if field, ok := c.records[key]; ok && field.Kind == "field" {
					p.Uses = append(p.Uses, PreviewLink{Key: key, Code: fmt.Sprintf("%s-%d", field.Segment, field.Field), Name: field.Name, Kind: "field"})
				} else {
					p.Uses = append(p.Uses, PreviewLink{Name: "Field reference unavailable"})
				}
			}
		}
	}
	return p, answer, children, count, total, nil
}

var maximumStatement = regexp.MustCompile(`(?im)^\s*Maximum Length:\s*([^\r\n]{1,128})`)
var nestedReferenceHeading = regexp.MustCompile(`(?m)^\s*[0-9]+[A-C]?(?:\.[A-Za-z0-9]+)+\s`)
var nextReferenceHeading = regexp.MustCompile(`(?m)\n\s*(?:Note:|Maximum Length:|[0-9]+(?:\.[A-Za-z0-9]+)+\s)`)
var printedInteger = regexp.MustCompile(`^[0-9]+$`)

// These are excerpts of supplied prose, not synthesized definitions or numeric
// limits. In particular, a datatype's Maximum Length statement is not a field
// constraint and is never copied into the field's Length attribute.
func definitionPreview(definition string) (string, string) {
	text := strings.TrimSpace(strings.ReplaceAll(definition, "\r\n", "\n"))
	// An imported datatype body can retain child subsections. Bound ownership
	// before looking for prose markers so a child's definition/limit cannot be
	// mistaken for the datatype's own introduction.
	if at := nestedReferenceHeading.FindStringIndex(text); at != nil {
		text = text[:at[0]]
	}
	maximum := ""
	if match := maximumStatement.FindStringSubmatchIndex(text); match != nil {
		maximum = strings.TrimSpace(text[match[2]:match[3]])
	}
	if at := strings.Index(text, "Definition:"); at >= 0 {
		text = strings.TrimSpace(text[at+len("Definition:"):])
	} else if match := maximumStatement.FindStringIndex(text); match != nil {
		text = strings.TrimSpace(text[match[1]:])
	}
	if at := nextReferenceHeading.FindStringIndex(text); at != nil {
		text = text[:at[0]]
	}
	if at := strings.Index(text, "\n\n"); at >= 0 {
		text = text[:at]
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 2048 {
		// Split on rune boundaries: this remains a visible, bounded excerpt.
		text = string([]rune(text)[:min(512, len([]rune(text)))]) + "…"
	}
	return text, maximum
}

func attributeExplanation(name PreviewAttribute, a Attribute) (string, string) {
	title := map[PreviewAttribute]string{"datatype": "Data type", "table": "Table", "item": "Data element", "section": "Section", "optionality": "Optionality", "length": "Field length", "conformance_length": "Conformance length", "repetition": "Repetition"}[name]
	if a.State != "specified" {
		switch a.State {
		case "not_specified":
			return title + " not specified", "The selected source does not specify this attribute. No value is inferred."
		case "not_applicable":
			return title + " not applicable", "This attribute does not apply to this reference entity."
		default:
			return title + " unavailable", "This attribute is not available in the selected reference."
		}
	}
	switch name {
	case "optionality":
		if text, ok := map[string]string{"R": "Required", "O": "Optional", "C": "Conditional", "B": "Backward compatibility", "W": "Withdrawn"}[a.Value]; ok {
			return text, "The selected reference marks this position as " + strings.ToLower(text) + ". Consult its definition for any conditions."
		}
	case "length":
		if printedInteger.MatchString(a.Value) {
			return title, "Maximum " + a.Value + " characters for this field. This is separate from the datatype's maximum length."
		}
		return title, "The selected reference's character-length notation for this field. Ranges and qualifiers are retained exactly as supplied."
	case "conformance_length":
		return title, "Conformance-length notation from the selected reference. This is separate from the field's length."
	case "repetition":
		if a.Value == "Y" || a.Value == "*" {
			return "May repeat", "This position may repeat. The selected reference does not supply a numeric maximum here."
		}
		if a.Value == "N" || a.Value == "1" {
			return "Single value", "The selected reference permits one value at this position."
		}
		return title, "The selected reference's repetition notation. Counts and qualifiers are retained exactly as supplied."
	}
	return title, "This is the selected reference's notation for this attribute."
}
