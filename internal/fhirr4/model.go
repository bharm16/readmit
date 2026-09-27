// Package fhirr4 interprets explicitly selected FHIR R4 4.0.1 JSON offline.
// Resource bytes, identity scope and primitive metadata are retained separately
// from projections. Nothing in this package resolves a network destination.
package fhirr4

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"regexp"

	"github.com/bharm16/readmit/internal/dataset"
)

const Version = "4.0.1"
const EvidenceSchema = "readmit-fhir-evidence/v1"
const ProjectionSchema = "readmit-fhir-projection/v1"
const DatasetSchema = "readmit-fhir-dataset/v1"
const CapabilitySchema = "readmit-fhir-capability-check/v1"
const MaxBytes = 16 << 20
const MaxNodes = 100000
const MaxDepth = 64
const MaxResources = 4096
const MaxMatches = 10000
const MaxFindings = 4096

var invalid = errors.New("invalid or unsupported FHIR R4 contract")
var identifier = regexp.MustCompile(`^[A-Za-z0-9.-]{1,64}$`)
var resourceType = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)
var name = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)

type Context struct {
	Version   string `json:"version"`
	Base      string `json:"base"`
	MediaType string `json:"media_type"`
}
type Finding struct {
	Code       string `json:"code"`
	State      string `json:"state"`
	Pointer    string `json:"pointer"`
	Occurrence string `json:"occurrence,omitzero"`
}
type BusinessID struct {
	System string `json:"system"`
	Value  string `json:"value"`
}
type Resource struct {
	Bundle            string       `json:"bundle"`
	State             string       `json:"state"`
	Occurrence        string       `json:"occurrence"`
	Type              string       `json:"type"`
	Base              string       `json:"base"`
	LogicalID         string       `json:"logical_id"`
	VersionID         string       `json:"version_id"`
	FullURL           string       `json:"full_url"`
	CanonicalURL      string       `json:"canonical_url"`
	CanonicalVersion  string       `json:"canonical_version"`
	Identifiers       []BusinessID `json:"identifiers"`
	Container         string       `json:"container"`
	Pointer           string       `json:"pointer"`
	ProjectionSupport string       `json:"projection_support"`
}
type member struct {
	key   string
	value *node
}
type node struct {
	kind       byte
	text       string
	members    []member
	items      []*node
	start, end int
	pointer    string
}

func (n *node) field(key string) *node {
	if n == nil {
		return nil
	}
	for _, m := range n.members {
		if m.key == key {
			return m.value
		}
	}
	return nil
}
func (n *node) string() string {
	if n == nil || n.kind != '"' {
		return ""
	}
	return n.text
}
func (n *node) array() []*node {
	if n == nil || n.kind != '[' {
		return nil
	}
	return n.items
}

type occurrence struct {
	resource             Resource
	node                 *node
	invalid, unsupported bool
}
type Document struct {
	findingsLimited bool
	raw             []byte
	context         Context
	root            *node
	resources       []occurrence
	findings        []Finding
}

func (d *Document) Raw() []byte      { return bytes.Clone(d.raw) }
func (d *Document) Context() Context { return d.context }
func (d *Document) Resources() []Resource {
	values := make([]Resource, len(d.resources))
	for i, r := range d.resources {
		values[i] = clone(r.resource)
	}
	return values
}
func (d *Document) Findings() []Finding { return clone(d.findings) }
func (d *Document) ResourceBytes(id string) ([]byte, error) {
	r := d.resource(id)
	if r == nil {
		return nil, invalid
	}
	return bytes.Clone(d.raw[r.node.start:r.node.end]), nil
}
func (d *Document) resource(id string) *occurrence {
	for i := range d.resources {
		if d.resources[i].resource.Occurrence == id {
			return &d.resources[i]
		}
	}
	return nil
}
func canonical(value any) []byte {
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return raw
}
func clone[T any](v T) T { var out T; _ = json.Unmarshal(canonical(v), &out); return out }
func (d *Document) add(code, state string, n *node) {
	if len(d.findings) >= MaxFindings-1 {
		if !d.findingsLimited {
			d.findingsLimited = true
			d.findings = append(d.findings, Finding{Code: "finding-limit", State: "unsupported"})
		}
		return
	}
	pointer := ""
	if n != nil {
		pointer = n.pointer
	}
	d.findings = append(d.findings, Finding{Code: code, State: state, Pointer: pointer})
}

type Step struct {
	Field string `json:"field"`
	Index *int   `json:"index,omitzero"`
	Each  bool   `json:"each"`
}
type Selector struct {
	Steps []Step `json:"steps"`
}
type Reading struct {
	Canonical *Canonical `json:"canonical,omitzero"`
	source    *node
	Datatype  string        `json:"datatype"`
	Value     dataset.Value `json:"value"`
	Pointer   string        `json:"pointer"`
	Companion []byte        `json:"companion,omitzero"`
	Raw       []byte        `json:"raw,omitzero"`
}
type Selection struct {
	State    string    `json:"state"`
	Readings []Reading `json:"readings"`
}
type Resolution struct {
	State       string   `json:"state"`
	Occurrences []string `json:"occurrences"`
}
type Relationship struct {
	Source     string     `json:"source"`
	Pointer    string     `json:"pointer"`
	Reference  string     `json:"reference"`
	Resolution Resolution `json:"resolution"`
}

type Column struct {
	Name     string   `json:"name"`
	Selector Selector `json:"selector"`
	Required bool     `json:"required"`
	Repeated bool     `json:"repeated"`
}
type Projection struct {
	Schema       string   `json:"schema"`
	ResourceType string   `json:"resource_type"`
	Columns      []Column `json:"columns"`
	MaxRows      int      `json:"max_rows"`
	MaxValues    int      `json:"max_values"`
}
type FieldProvenance struct {
	Datatypes  []string     `json:"datatypes"`
	Canonicals []*Canonical `json:"canonicals"`
	Row        string       `json:"row"`
	Column     string       `json:"column"`
	Pointers   []string     `json:"pointers"`
	Companions [][]byte     `json:"companions"`
}
type Dataset struct {
	Schema         string            `json:"schema"`
	Version        string            `json:"version"`
	Binding        dataset.Binding   `json:"binding"`
	Projection     Projection        `json:"projection"`
	SourceIdentity string            `json:"source_identity"`
	Status         string            `json:"status"`
	Rows           []dataset.Row     `json:"rows"`
	Provenance     []FieldProvenance `json:"provenance"`
}

// Decode consumes bounded original JSON. Syntax/limit failures refuse decoding;
// legal JSON with illegal FHIR representations remains inspectable with findings.
func Decode(ctx context.Context, raw []byte, c Context) (*Document, error) {
	if c.Version != Version || len(raw) == 0 || len(raw) > MaxBytes || validateContext(c) != nil {
		return nil, invalid
	}
	root, err := parse(ctx, raw)
	if err != nil {
		return nil, err
	}
	d := &Document{raw: bytes.Clone(raw), context: c, root: root, findings: []Finding{}, resources: []occurrence{}}
	if root.kind != '{' {
		return nil, invalid
	}
	if err := d.index(ctx, root, c.Base, "", "", ""); err != nil {
		return nil, err
	}
	d.validate()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return d, nil
}
