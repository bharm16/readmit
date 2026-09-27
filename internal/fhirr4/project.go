package fhirr4

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"regexp"

	"github.com/bharm16/readmit/internal/dataset"
)

var columnName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (d *Document) Identity() string {
	return dataset.Digest(canonical(struct {
		Context Context `json:"context"`
		Raw     string  `json:"raw_sha256"`
	}{d.context, dataset.Digest(d.raw)}))
}
func (p Projection) Validate() error {
	if p.Schema != ProjectionSchema || !supported(p.ResourceType) || len(p.Columns) < 1 || len(p.Columns) > 32 || p.MaxRows < 1 || p.MaxRows > MaxResources || p.MaxValues < 1 || p.MaxValues > MaxNodes {
		return invalid
	}
	seen := map[string]bool{}
	for _, column := range p.Columns {
		if !columnName.MatchString(column.Name) || seen[column.Name] || column.Selector.Validate() != nil {
			return invalid
		}
		seen[column.Name] = true
	}
	return nil
}

// Project emits the existing typed value/provenance carriers under a distinct
// FHIR contract. It never sends FHIR nulls or partial dates through dataset/v1's
// frozen conversion, validators, projection reader or temporal assumptions.
func (d *Document) Project(ctx context.Context, binding dataset.Binding, p Projection) (Dataset, error) {
	out := Dataset{Schema: DatasetSchema, Version: Version, Binding: binding, Projection: clone(p), SourceIdentity: d.Identity(), Status: "complete", Rows: []dataset.Row{}, Provenance: []FieldProvenance{}}
	if p.Validate() != nil || dataset.ValidateBinding(binding) != nil {
		return out, invalid
	}
	if d.root.field("resourceType").string() != "Bundle" && d.root.field("resourceType").string() != p.ResourceType {
		out.Status = "source-type-mismatch"
		return out, nil
	}
	values := 0
	projectedBytes := 0
	for _, resource := range d.resources {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if resource.resource.Type != p.ResourceType {
			continue
		}
		if len(out.Rows) >= p.MaxRows {
			out.Status = "row-limit"
			break
		}
		row := dataset.Row{ID: fmt.Sprintf("row-%06d", len(out.Rows)+1), Values: []dataset.Value{}, Provenance: dataset.Provenance{Record: len(out.Rows) + 1, Offset: -1, Size: resource.node.end - resource.node.start, SourceRecord: resource.resource.Occurrence}}
		for _, column := range p.Columns {
			selection := d.Select(ctx, resource.resource.Occurrence, column.Selector)
			provenance := FieldProvenance{Datatypes: []string{}, Canonicals: []*Canonical{}, Row: row.ID, Column: column.Name, Pointers: []string{}, Companions: [][]byte{}}
			value := dataset.Value{State: "absent", Type: "text"}
			if selection.State == "invalid" || selection.State == "ambiguous" || selection.State == "unsupported" {
				value.State = selection.State
				out.Status = "projection-failed"
			} else {
				for _, reading := range selection.Readings {
					values++
					projectedBytes += len(reading.Value.Text) + len(reading.Companion) + len(reading.Pointer) + len(reading.Datatype) + 256
					if projectedBytes > 32<<20 {
						out.Status = "projection-byte-limit"
						return out, nil
					}
					if values > p.MaxValues {
						out.Status = "value-limit"
						return out, nil
					}
					provenance.Datatypes = append(provenance.Datatypes, reading.Datatype)
					provenance.Canonicals = append(provenance.Canonicals, reading.Canonical)
					provenance.Pointers = append(provenance.Pointers, reading.Pointer)
					provenance.Companions = append(provenance.Companions, reading.Companion)
				}
				if column.Repeated {
					value = dataset.Value{State: "present", Items: []dataset.Value{}}
					for _, reading := range selection.Readings {
						if !primitive(reading.Datatype) {
							value.State = "unsupported"
							out.Status = "projection-failed"
							continue
						}
						if value.Type == "" {
							value.Type = reading.Value.Type
						}
						if value.Type != reading.Value.Type {
							value.State = "invalid"
							out.Status = "projection-failed"
						}
						value.Items = append(value.Items, reading.Value)
						if column.Required && reading.Value.State != "present" {
							out.Status = "projection-failed"
						}
					}
					if len(selection.Readings) == 0 {
						value.State = "absent"
						value.Type = "text"
						value.Items = nil
					}
				} else if len(selection.Readings) > 1 {
					value.State = "ambiguous"
					out.Status = "projection-failed"
				} else if len(selection.Readings) == 1 {
					if !primitive(selection.Readings[0].Datatype) {
						value.State = "unsupported"
						out.Status = "projection-failed"
					} else {
						value = selection.Readings[0].Value
					}
				}
			}
			if column.Required && value.State != "present" {
				out.Status = "projection-failed"
			}
			row.Values = append(row.Values, value)
			out.Provenance = append(out.Provenance, provenance)
		}
		out.Rows = append(out.Rows, row)
	}
	// Governing Bundle uncertainty also applies when the selected set is empty.
	if d.findingsLimited {
		out.Status = "projection-limit"
	}
	for _, r := range d.resources {
		if r.resource.Type == "Bundle" && r.unsupported {
			out.Status = "unsupported-source"
		}
	}
	// A syntactically decoded but invalid resource cannot become passing absence.
	for _, finding := range d.findings {
		if finding.State == "invalid" {
			out.Status = "invalid-source"
			break
		}
	}
	if len(canonical(out)) > 32<<20 {
		return Dataset{}, invalid
	}
	return out, nil
}

func DecodeProjection(raw []byte) (Projection, error) {
	var p Projection
	if len(raw) > 64<<10 || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Validate() != nil {
		return p, invalid
	}
	return p, nil
}
