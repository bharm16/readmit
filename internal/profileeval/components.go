package profileeval

import (
	"encoding/json/v2"
	"strconv"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

const ProfileSchemaV3 = "readmit-local-profile/v3"
const PackSchemaV3 = "readmit-profile-pack/v3"
const ComponentOperatorVersion = "readmit-profile-evaluator/v2"

// Component metadata is explicitly versioned. It is never inferred from a type
// name in a historical pack. Codes are a finite declared local vocabulary.
type ComponentRule struct {
	Prohibited bool     `json:"prohibited,omitzero"`
	Table      string   `json:"table,omitzero"`
	Position   int      `json:"position"`
	DataType   string   `json:"datatype"`
	Required   bool     `json:"required"`
	MaxLength  int      `json:"max_length"`
	Codes      []string `json:"codes"`
}
type DatatypeRule struct {
	UsageKnown bool            `json:"usage_known"`
	Name       string          `json:"name"`
	Components []ComponentRule `json:"components"`
}
type ProfileV3 struct {
	Schema     string               `json:"schema"`
	Definition localprofile.Profile `json:"definition"`
	Structure  []Node               `json:"structure"`
	Workflows  []WorkflowV3         `json:"workflows"`
	Datatypes  []DatatypeRule       `json:"datatypes"`
}
type PackV3 struct {
	Schema    string           `json:"schema"`
	Metadata  profilepack.Pack `json:"metadata"`
	Messages  []MessageRule    `json:"messages"`
	Datatypes []DatatypeRule   `json:"datatypes"`
}

func validateDatatypes(types []DatatypeRule) error {
	if len(types) > 256 {
		return invalid
	}
	seen := map[string]bool{}
	for _, d := range types {
		if !token.MatchString(d.Name) || seen[d.Name] || len(d.Components) == 0 || len(d.Components) > 64 || datatype(d.Name, "1") != "unsupported" {
			return invalid
		}
		seen[d.Name] = true
		previous := 0
		for _, c := range d.Components {
			if c.Required && c.Prohibited || c.Position <= previous || c.Position > 64 || !token.MatchString(c.DataType) || c.MaxLength < 0 || c.MaxLength > MaxBytes || len(c.Codes) > 1024 || c.Table != "" && !token.MatchString(c.Table) {
				return invalid
			}
			previous = c.Position
			codes := map[string]bool{}
			for _, code := range c.Codes {
				if code == "" || len(code) > 1024 || codes[code] {
					return invalid
				}
				codes[code] = true
			}
		}
	}
	// Source metadata can contain recursive/withdrawn placeholders. Keep them
	// verbatim; evaluation stops at the two wire component levels as unsupported.
	return nil
}

func (e *evaluator) evaluateDatatype(kind, selector, origin string, r hl7.Reading, depth int) {
	var declaration *DatatypeRule
	for i := range e.pack.datatypes {
		if e.pack.datatypes[i].Name == kind {
			declaration = &e.pack.datatypes[i]
			break
		}
	}
	if origin != "profile" {
		for i := range e.profile.datatypes {
			if e.profile.datatypes[i].Name == kind {
				declaration = &e.profile.datatypes[i]
				break
			}
		}
	}
	if declaration == nil {
		outcome := datatype(kind, string(r.Decoded))
		if outcome != "unsupported" && e.report.Operator == ComponentOperatorVersion && !r.Literal && depth < 2 {
			delimiters := e.doc.Messages[0].Delimiters
			separators := []byte{delimiters.Subcomponent}
			if depth == 0 {
				separators = append(separators, delimiters.Component)
			}
			raw := e.doc.Bytes(r.Span)
			for _, separator := range separators {
				if encodedComponents(raw, separator, delimiters.Escape) > 1 {
					e.add("primitive-components-"+kind, origin, "fail", selector, r)
					break
				}
			}
		}
		if outcome != "pass" {
			e.add("datatype-"+kind, origin, outcome, selector, r)
		}
		return
	}
	if depth >= 2 {
		e.add("datatype-wire-depth-"+kind, origin, "unsupported", selector, r)
		return
	}
	// Count encoded separators before decoding; an escaped separator is text.
	delimiter := e.doc.Messages[0].Delimiters.Component
	if depth == 1 {
		delimiter = e.doc.Messages[0].Delimiters.Subcomponent
	}
	raw := e.doc.Bytes(r.Span)
	e.readBytes += len(raw)
	if e.readBytes > 64<<20 {
		e.err = invalid
		return
	}
	count := encodedComponents(raw, delimiter, e.doc.Messages[0].Delimiters.Escape)
	if !declaration.UsageKnown {
		e.add("component-usage-unavailable-"+kind, origin, "unsupported", selector, r)
	}
	max := declaration.Components[len(declaration.Components)-1].Position
	if count > max {
		e.add("component-cardinality", origin, "fail", selector, r)
	}
	declared := map[int]bool{}
	for _, c := range declaration.Components {
		declared[c.Position] = true
		path := selector + "." + strconv.Itoa(c.Position)
		value := e.read(path)
		if c.Prohibited {
			if value.State == hl7.Present || value.State == hl7.Null {
				e.add("prohibited-component", origin, "fail", path, value)
			}
			continue
		}
		if c.Required && value.State != hl7.Present {
			e.add("required-component", origin, "fail", path, value)
		}
		if value.State != hl7.Present {
			continue
		}
		if value.Reason != "" {
			e.add(string(value.Reason), origin, "unsupported", path, value)
			continue
		}
		if c.MaxLength > 0 && len(value.Decoded) > c.MaxLength {
			e.add("component-length", origin, "fail", path, value)
		}
		if c.Table != "" && len(c.Codes) == 0 {
			e.add("component-terminology-unavailable-"+c.Table, origin, "unsupported", path, value)
		}
		if len(c.Codes) > 0 {
			found := false
			for _, code := range c.Codes {
				if code == string(value.Decoded) {
					found = true
					break
				}
			}
			if !found {
				e.add("component-code-set", origin, "fail", path, value)
			}
		}
		e.evaluateDatatype(c.DataType, path, origin, value, depth+1)
	}
	for position := 1; position <= count && position <= max; position++ {
		if !declared[position] {
			path := selector + "." + strconv.Itoa(position)
			value := e.read(path)
			if value.State == hl7.Present || value.State == hl7.Null {
				e.add("undeclared-component", origin, "fail", path, value)
			}
		}
	}
}

func profileCanonical(p ProfileV2) ([]byte, error) {
	switch p.Schema {
	case ProfileSchemaV3:
		return json.Marshal(ProfileV3{Schema: p.Schema, Definition: p.Definition, Structure: p.Structure, Workflows: workflowsV3(p.Workflows), Datatypes: p.datatypes}, json.Deterministic(true))
	case ProfileSchema:
		return json.Marshal(p, json.Deterministic(true))
	default:
		return json.Marshal(p.Definition, json.Deterministic(true))
	}
}

// WorkflowV3 binds a repeated subject to an explicitly ordered outer-to-inner
// parent chain. An absent parent is unknown, never borrowed from another group.
type WorkflowV3 struct {
	Workflow
	ParentSegments []string `json:"parent_segments"`
}

func workflowsV3(values []Workflow) []WorkflowV3 {
	out := make([]WorkflowV3, len(values))
	for i, w := range values {
		out[i] = WorkflowV3{Workflow: w, ParentSegments: w.parents}
	}
	return out
}

func encodedComponents(raw []byte, delimiter, escape byte) int {
	count := 1
	escaped := false
	for _, b := range raw {
		if b == escape {
			escaped = !escaped
		} else if b == delimiter && !escaped {
			count++
		}
	}
	return count
}
