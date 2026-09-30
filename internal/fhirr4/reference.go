package fhirr4

import (
	"context"
	"net/url"
	"strings"
)

func resolved(matches []string) Resolution {
	state := "missing"
	if len(matches) == 1 {
		state = "resolved"
	}
	if len(matches) > 1 {
		state = "ambiguous"
	}
	return Resolution{State: state, Occurrences: matches}
}

// Resolve performs only identity lookup in retained resources. An external
// reference reports external; it has no resolver or network callback.
func (d *Document) Resolve(sourceID, reference string) Resolution {
	source := d.resource(sourceID)
	if source == nil || reference == "" || len(reference) > 4096 {
		return Resolution{State: "invalid", Occurrences: []string{}}
	}
	matches := []string{}
	if strings.HasPrefix(reference, "#") {
		container := source.resource.Occurrence
		if source.resource.Container != "" {
			container = source.resource.Container
		}
		if reference == "#" {
			if source.resource.Container != "" {
				matches = append(matches, container)
			}
			return resolved(matches)
		}
		id := strings.TrimPrefix(reference, "#")
		if !identifier.MatchString(id) {
			return Resolution{State: "invalid", Occurrences: matches}
		}
		for _, r := range d.resources {
			if r.resource.Container == container && r.resource.LogicalID == id {
				matches = append(matches, r.resource.Occurrence)
			}
		}
		return resolved(matches)
	}
	u, err := url.Parse(reference)
	if err != nil || u.User != nil || u.Fragment != "" {
		return Resolution{State: "invalid", Occurrences: matches}
	}
	if u.RawQuery != "" {
		return Resolution{State: "unsupported", Occurrences: matches}
	}
	absolute := reference
	if !u.IsAbs() {
		scope := source
		if source.resource.Container != "" {
			scope = d.resource(source.resource.Container)
		}
		if scope == nil || scope.resource.Base == "" {
			return resolved(matches)
		}
		parts := strings.Split(reference, "/")
		if len(parts) != 2 && len(parts) != 4 || !resourceType.MatchString(parts[0]) || !identifier.MatchString(parts[1]) || len(parts) == 4 && (parts[2] != "_history" || !identifier.MatchString(parts[3])) {
			return Resolution{State: "invalid", Occurrences: matches}
		}
		absolute = strings.TrimSuffix(scope.resource.Base, "/") + "/" + reference
	}
	address, version := referenceVersion(absolute)
	for _, entry := range d.resources {
		r := entry.resource
		if r.Container != "" || r.Bundle != d.bundleScope(sourceID) {
			continue
		}
		identity := r.FullURL
		if identity == "" && r.Base != "" && r.LogicalID != "" {
			identity = strings.TrimSuffix(r.Base, "/") + "/" + r.Type + "/" + r.LogicalID
		}
		if identity == address && (version == "" || r.VersionID == version) {
			matches = append(matches, r.Occurrence)
		}
	}
	result := resolved(matches)
	if len(matches) == 0 {
		parsed, _ := url.Parse(absolute)
		if parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			result.State = "external"
		}
	}
	return result
}

// Relationships lists finite Reference positions in one resource, preserving
// repeated positions and using Join's scoped literal/identifier semantics.
func (d *Document) Relationships(ctx context.Context, sourceID string) []Relationship {
	resource := d.resource(sourceID)
	if resource == nil {
		return []Relationship{}
	}
	out := []Relationship{}
	var walk func(*node, string, []Step)
	walk = func(n *node, typ string, steps []Step) {
		if n == nil || ctx.Err() != nil || len(out) >= MaxMatches {
			return
		}
		if typ == "Reference" {
			out = append(out, d.Join(ctx, sourceID, Selector{Steps: steps}, "")...)
			return
		}
		for _, member := range n.members {
			field, known := definitions[typ][member.key]
			if !known || primitive(field.typ) || field.typ == "Resource" {
				continue
			}
			if field.many {
				for i, item := range member.value.array() {
					index := i
					next := append(append([]Step(nil), steps...), Step{Field: member.key, Index: &index})
					walk(item, field.typ, next)
				}
			} else {
				next := append(append([]Step(nil), steps...), Step{Field: member.key})
				walk(member.value, field.typ, next)
			}
		}
	}
	walk(resource.node, resource.resource.Type, nil)
	return out
}
func referenceVersion(reference string) (string, string) {
	u, err := url.Parse(reference)
	if err != nil {
		return reference, ""
	}
	pieces := strings.Split(u.Path, "/")
	if len(pieces) >= 4 && pieces[len(pieces)-2] == "_history" && identifier.MatchString(pieces[len(pieces)-1]) {
		version := pieces[len(pieces)-1]
		u.Path = strings.Join(pieces[:len(pieces)-2], "/")
		u.RawPath = ""
		return u.String(), version
	}
	return reference, ""
}

// Join follows a finite Reference selector through this retained document only.
// It does not infer transitive patient/order relationships or match display text.
func (d *Document) Join(ctx context.Context, sourceID string, selector Selector, targetType string) []Relationship {
	selection := d.Select(ctx, sourceID, selector)
	out := []Relationship{}
	if selection.State == "invalid" || selection.State == "unsupported" || selection.State == "ambiguous" {
		return []Relationship{{Source: sourceID, Resolution: Resolution{State: selection.State, Occurrences: []string{}}}}
	}
	for _, reading := range selection.Readings {
		relation := Relationship{Source: sourceID, Pointer: reading.Pointer, Resolution: Resolution{State: "missing", Occurrences: []string{}}}
		if ctx.Err() != nil || reading.Datatype != "Reference" || reading.source == nil {
			relation.Resolution.State = "invalid"
			out = append(out, relation)
			continue
		}
		node := reading.source
		literal := node.field("reference").string()
		relation.Reference = literal
		business := BusinessID{System: node.field("identifier").field("system").string(), Value: node.field("identifier").field("value").string()}
		if literal != "" {
			relation.Resolution = d.Resolve(sourceID, literal)
		} else if business.System != "" && business.Value != "" {
			matches := []string{}
			for _, entry := range d.resources {
				r := entry.resource
				if r.Container != "" || r.Bundle != d.bundleScope(sourceID) || targetType != "" && r.Type != targetType {
					continue
				}
				for _, id := range r.Identifiers {
					if id == business {
						matches = append(matches, r.Occurrence)
						break
					}
				}
			}
			relation.Resolution = resolved(matches)
		}
		declared := node.field("type").string()
		if strings.HasPrefix(declared, "http://hl7.org/fhir/StructureDefinition/") {
			declared = strings.TrimPrefix(declared, "http://hl7.org/fhir/StructureDefinition/")
		}
		for _, id := range relation.Resolution.Occurrences {
			target := d.resource(id)
			if target == nil || targetType != "" && target.resource.Type != targetType || declared != "" && target.resource.Type != declared {
				relation.Resolution.State = "invalid"
			}
			if literal != "" && business.System != "" && business.Value != "" {
				matched := false
				for _, identifier := range target.resource.Identifiers {
					if identifier == business {
						matched = true
					}
				}
				if !matched {
					relation.Resolution.State = "invalid"
				}
			}
		}
		out = append(out, relation)
	}
	if len(out) == 0 {
		out = append(out, Relationship{Source: sourceID, Resolution: Resolution{State: "missing", Occurrences: []string{}}})
	}
	return out
}

func (d *Document) bundleScope(sourceID string) string {
	source := d.resource(sourceID)
	if source == nil {
		return ""
	}
	if source.resource.Type == "Bundle" {
		return source.resource.Occurrence
	}
	return source.resource.Bundle
}
