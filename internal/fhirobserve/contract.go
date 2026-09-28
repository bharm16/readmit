// Package fhirobserve adapts one reviewed FHIR R4 search into the shared typed
// table carrier. It never contacts a server itself: acquisition runs through
// fhirrest's scoped HTTP executor, and reading a sample uses retained bytes only.
// A complete search is observed state at its declared boundary, never proof that
// an upstream scheduling, laboratory or EHR workflow occurred.
package fhirobserve

import (
	"encoding/json/v2"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/fhirrest"
)

const Schema = "readmit-fhir-observation/v1"
const ProjectionSchema = "readmit-fhir-typed-projection/v1"

var invalid = errors.New("invalid FHIR observation contract or retained sample")
var token = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var parameter = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)
var placeholder = regexp.MustCompile(`\{([a-z][a-z0-9-]{0,63})\}`)

// Boundaries name what an observation reads. The report meaning is fixed per
// boundary; nothing observed can widen it.
var boundaries = map[string]string{
	"authoritative-application-api": "State read from the application's authoritative API when it was observed.",
	"delayed-replica":               "State read from a delayed replica or export; staleness or absence can be replication delay, not application state.",
	"reference-fhir-store":          "State of a reference FHIR store; it is not evidence that an EHR, scheduling or laboratory workflow occurred.",
}

// Meaning is the fixed qualification sentence for a declared boundary.
func Meaning(boundary string) string { return boundaries[boundary] }

// Value selects a column's content. A field reads a typed primitive; identity
// is the matched resource's own Type/id; reference normalizes a selected
// Reference.reference to Type/id within the declared base, never fetching it.
type Value struct {
	Kind     string           `json:"kind"`
	Selector *fhirr4.Selector `json:"selector,omitzero"`
}
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	CodeSystem string `json:"code_system,omitzero"`
	Key        bool   `json:"key"`
	Required   bool   `json:"required"`
	Repeated   bool   `json:"repeated"`
	Value      Value  `json:"value"`
}

// Observation is an authored, reviewed search. Query values may name compiled
// test variables as {id}; they are resolved from the plan, never from a response.
type Observation struct {
	Schema    string          `json:"schema"`
	ID        string          `json:"id"`
	Server    string          `json:"server"`
	Resource  string          `json:"resource"`
	Query     string          `json:"query"`
	Boundary  string          `json:"boundary"`
	Columns   []Column        `json:"columns"`
	MaxRows   int             `json:"max_rows"`
	MaxValues int             `json:"max_values"`
	Budget    fhirrest.Budget `json:"budget"`
	Retry     fhirrest.Retry  `json:"retry"`
}

func canonical(v any) []byte { b, _ := json.Marshal(v, json.Deterministic(true)); return b }

func Decode(raw []byte) (Observation, error) {
	var o Observation
	if len(raw) > 64<<10 || json.Unmarshal(raw, &o, json.RejectUnknownMembers(true)) != nil || o.Validate() != nil {
		return Observation{}, invalid
	}
	return o, nil
}

func (o Observation) Validate() error {
	if o.Schema != Schema || !token.MatchString(o.ID) || !token.MatchString(o.Server) || boundaries[o.Boundary] == "" || len(o.Columns) < 1 || len(o.Columns) > 32 || o.MaxRows < 1 || o.MaxRows > fhirr4.MaxResources || o.MaxValues < 1 || o.MaxValues > fhirr4.MaxNodes || !o.Budget.Valid() || !o.Retry.Valid() {
		return invalid
	}
	values, err := url.ParseQuery(o.Query)
	if err != nil || len(o.Query) > 4096 || strings.ContainsAny(o.Query, "#\r\n") {
		return invalid
	}
	for name, v := range values {
		// Partial views make a projection unusable; refuse them before review.
		if !parameter.MatchString(name) && name != "_include" && name != "_revinclude" || name == "_summary" || name == "_elements" || name == "_total" || len(v) > 8 {
			return invalid
		}
	}
	seen, keys, fields := map[string]bool{}, 0, 0
	for _, c := range o.Columns {
		if !token.MatchString(c.Name) || seen[c.Name] || !slices.Contains([]string{"text", "decimal", "boolean", "date", "datetime", "code"}, c.Type) || c.Type == "code" && (c.CodeSystem == "" || len(c.CodeSystem) > 256) || c.Type != "code" && c.CodeSystem != "" || c.Key && c.Repeated {
			return invalid
		}
		seen[c.Name] = true
		switch c.Value.Kind {
		case "field":
			if c.Value.Selector == nil {
				return invalid
			}
			fields++
		case "reference":
			if c.Value.Selector == nil || c.Type != "text" || c.Repeated {
				return invalid
			}
			fields++
		case "identity":
			if c.Value.Selector != nil || c.Type != "text" || c.Repeated {
				return invalid
			}
		default:
			return invalid
		}
		if c.Key {
			keys++
		}
	}
	if keys < 1 || fields < 1 || o.projection().Validate() != nil {
		return invalid
	}
	return nil
}

// Identity is the observation source identity bound into datasets and intervals.
func (o Observation) Identity() string { return dataset.Digest(canonical(o)) }

// ProjectionIdentity binds checks to exactly these typed columns.
func (o Observation) ProjectionIdentity() string {
	return dataset.Digest(canonical(struct {
		Schema   string   `json:"schema"`
		ID       string   `json:"id"`
		Resource string   `json:"resource"`
		Columns  []Column `json:"columns"`
	}{ProjectionSchema, o.ID, o.Resource, o.Columns}))
}

// TypedColumns are the declared evaluator columns. They carry no locator: the
// FHIR selector remains in this contract, not in dataset/v1's frozen projection.
func (o Observation) TypedColumns() []dataset.Column {
	out := []dataset.Column{}
	for _, c := range o.Columns {
		out = append(out, dataset.Column{Name: c.Name, Type: c.Type, CodeSystem: c.CodeSystem, Key: c.Key, Required: c.Required, Repeated: c.Repeated})
	}
	return out
}

// Variables are the compiled test variables the query names.
func (o Observation) Variables() []string {
	out := []string{}
	for _, m := range placeholder.FindAllStringSubmatch(o.Query, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// URL is the exact search address under base. Every placeholder must resolve
// from the supplied compiled values; the encoding is canonical.
func (o Observation) URL(base string, resolution map[string]string) (string, error) {
	values, err := url.ParseQuery(o.Query)
	if err != nil || !fhirrequest.ValidBase(base) {
		return "", invalid
	}
	for name, list := range values {
		for i, v := range list {
			resolved := placeholder.ReplaceAllStringFunc(v, func(m string) string {
				value, ok := resolution[m[1:len(m)-1]]
				if !ok {
					err = invalid
				}
				return value
			})
			if strings.ContainsAny(resolved, "{}") {
				err = invalid
			}
			list[i] = resolved
		}
		values[name] = list
	}
	if err != nil {
		return "", invalid
	}
	address := base + "/" + o.Resource
	if encoded := values.Encode(); encoded != "" {
		address += "?" + encoded
	}
	return address, nil
}

func (o Observation) projection() fhirr4.Projection {
	p := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: o.Resource, Columns: []fhirr4.Column{}, MaxRows: o.MaxRows, MaxValues: o.MaxValues}
	for _, c := range o.Columns {
		if c.Value.Kind == "identity" {
			continue
		}
		p.Columns = append(p.Columns, fhirr4.Column{Name: c.Name, Selector: *c.Value.Selector, Required: c.Required, Repeated: c.Repeated})
	}
	return p
}
