package fhirr4

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

func (d *Document) index(ctx context.Context, n *node, base, fullURL, container, bundle string) error {
	if ctx.Err() != nil || len(d.resources) >= MaxResources {
		return invalid
	}
	if n == nil || n.kind != '{' {
		d.add("resource-object", "invalid", n)
		return nil
	}
	typ := n.field("resourceType").string()
	support := "unsupported"
	if supported(typ) {
		support = "finite-r4-projection"
	}
	r := Resource{Bundle: bundle, Occurrence: fmt.Sprintf("r%06d", len(d.resources)+1), Type: typ, Base: base, FullURL: fullURL, LogicalID: n.field("id").string(), VersionID: n.field("meta").field("versionId").string(), CanonicalURL: n.field("url").string(), CanonicalVersion: n.field("version").string(), Container: container, Pointer: n.pointer, ProjectionSupport: support, Identifiers: []BusinessID{}}
	if !canonicalResource(typ) {
		r.CanonicalURL = ""
		r.CanonicalVersion = ""
	}
	if fullURL != "" {
		r.Base = restBase(fullURL, typ, r.LogicalID)
	}
	for _, id := range n.field("identifier").array() {
		r.Identifiers = append(r.Identifiers, BusinessID{System: id.field("system").string(), Value: id.field("value").string()})
	}
	d.resources = append(d.resources, occurrence{resource: r, node: n})
	if typ == "Bundle" {
		for _, entry := range n.field("entry").array() {
			if child := entry.field("resource"); child != nil {
				if err := d.index(ctx, child, "", entry.field("fullUrl").string(), "", r.Occurrence); err != nil {
					return err
				}
			}
			if child := entry.field("response").field("outcome"); child != nil {
				if err := d.index(ctx, child, "", "", "", r.Occurrence); err != nil {
					return err
				}
			}
		}
	}
	for _, child := range n.field("contained").array() {
		if container != "" {
			d.add("nested-contained", "invalid", child)
			continue
		}
		if err := d.index(ctx, child, "", "", r.Occurrence, bundle); err != nil {
			return err
		}
	}
	return nil
}
func restBase(fullURL, typ, id string) string {
	u, err := url.Parse(fullURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || id == "" {
		return ""
	}
	suffix := "/" + typ + "/" + id
	if !strings.HasSuffix(u.Path, suffix) {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, suffix)
	u.RawPath = ""
	return strings.TrimSuffix(u.String(), "/")
}
func (d *Document) validate() {
	for i := range d.resources {
		r := &d.resources[i]
		start := len(d.findings)
		if !resourceType.MatchString(r.resource.Type) {
			d.add("resource-type", "invalid", r.node)
		}
		if r.resource.LogicalID != "" && !identifier.MatchString(r.resource.LogicalID) {
			d.add("logical-id", "invalid", r.node.field("id"))
		}
		if r.resource.Container != "" {
			meta := r.node.field("meta")
			for _, key := range []string{"versionId", "lastUpdated", "security"} {
				if n := meta.field(key); n != nil {
					d.add("contained-meta", "invalid", n)
				}
			}
		}
		d.validateObject(r.node, r.resource.Type)
		for j := start; j < len(d.findings); j++ {
			d.findings[j].Occurrence = r.resource.Occurrence
			if d.findings[j].State == "invalid" {
				r.invalid = true
			}
			if d.findings[j].Code == "uninterpreted-modifier-extension" || d.findings[j].Code == "uninterpreted-implicit-rules" {
				r.unsupported = true
			}
		}
		r.resource.State = "parsed"
		if r.invalid {
			r.resource.State = "invalid"
		} else if r.unsupported || !supported(r.resource.Type) {
			r.resource.State = "unsupported"
		}
	}
	for i := range d.findings {
		if d.findings[i].Occurrence != "" {
			continue
		}
		best := -1
		for j, r := range d.resources {
			p := r.resource.Pointer
			if (d.findings[i].Pointer == p || strings.HasPrefix(d.findings[i].Pointer, p+"/")) && (best < 0 || len(p) > len(d.resources[best].resource.Pointer)) {
				best = j
			}
		}
		if best >= 0 {
			d.findings[i].Occurrence = d.resources[best].resource.Occurrence
			if d.findings[i].State == "invalid" {
				d.resources[best].invalid = true
				d.resources[best].resource.State = "invalid"
			}
		}
	}
	if d.findingsLimited {
		for i := range d.resources {
			d.resources[i].unsupported = true
			if !d.resources[i].invalid {
				d.resources[i].resource.State = "unsupported"
			}
		}
	}
	byID := map[string]*occurrence{}
	for i := range d.resources {
		byID[d.resources[i].resource.Occurrence] = &d.resources[i]
	}
	for i := range d.resources {
		r := &d.resources[i]
		for parent := byID[r.resource.Bundle]; parent != nil; parent = byID[parent.resource.Bundle] {
			r.invalid = r.invalid || parent.invalid
			r.unsupported = r.unsupported || parent.unsupported
		}
		if r.invalid {
			r.resource.State = "invalid"
		} else if r.unsupported {
			r.resource.State = "unsupported"
		}
	}
	slices.SortFunc(d.findings, func(a, b Finding) int {
		for _, pair := range [][2]string{{a.Occurrence, b.Occurrence}, {a.Pointer, b.Pointer}, {a.Code, b.Code}, {a.State, b.State}} {
			if order := strings.Compare(pair[0], pair[1]); order != 0 {
				return order
			}
		}
		return 0
	})
}
func (d *Document) validateObject(n *node, typ string) {
	if n == nil || n.kind != '{' {
		d.add("object-required", "invalid", n)
		return
	}
	if len(n.members) == 0 {
		d.add("empty-object", "invalid", n)
		return
	}
	def := definitions[typ]
	if def == nil {
		d.add("unsupported-type", "unsupported", n)
		return
	}
	choices := map[string]int{}
	for key, f := range def {
		value, metadata := n.field(key), n.field("_"+key)
		if f.choice != "" && (value != nil || metadata != nil) {
			choices[f.choice]++
		}
		if f.required && value == nil && metadata == nil {
			d.add("required-element", "invalid", &node{pointer: pointer(n.pointer, key)})
		}
	}
	for _, count := range choices {
		if count > 1 {
			d.add("multiple-choice-values", "invalid", n)
		}
	}
	for _, m := range n.members {
		key := m.key
		if key == "modifierExtension" {
			d.add("uninterpreted-modifier-extension", "unsupported", m.value)
		}
		if key == "implicitRules" {
			d.add("uninterpreted-implicit-rules", "unsupported", m.value)
		}
		if strings.HasPrefix(key, "_") {
			base := strings.TrimPrefix(key, "_")
			f, known := def[base]
			if !known || !primitive(f.typ) {
				d.add("unexpected-primitive-companion", "invalid", m.value)
			}
			continue
		}
		f, known := def[key]
		if !known {
			d.add("unsupported-element", "unsupported", m.value)
			d.validateUnknown(m.value, false)
			continue
		}
		metadata := n.field("_" + key)
		d.validateElement(m.value, metadata, f)
		if values := knownCodes[typ+"."+key]; len(values) > 0 && m.value.kind == '"' && !slices.Contains(values, m.value.text) {
			d.add("code-value", "invalid", m.value)
		}
	}
	// Extension-only primitive values are represented by only the companion.
	for key, f := range def {
		if n.field(key) == nil && n.field("_"+key) != nil && primitive(f.typ) {
			d.validateElement(nil, n.field("_"+key), f)
		}
	}
	if typ == "Extension" {
		values := 0
		for key := range def {
			if strings.HasPrefix(key, "value") && (n.field(key) != nil || n.field("_"+key) != nil) {
				values++
			}
		}
		children := n.field("extension")
		if values > 0 && children != nil || values == 0 && children == nil {
			d.add("extension-content", "invalid", n)
		}
	}
}
func (d *Document) validateElement(value, metadata *node, f field) {
	if f.many {
		if value != nil && value.kind != '[' || metadata != nil && metadata.kind != '[' {
			d.add("array-required", "invalid", first(value, metadata))
			return
		}
		if value != nil && len(value.items) == 0 || metadata != nil && len(metadata.items) == 0 {
			d.add("empty-array", "invalid", first(value, metadata))
			return
		}
		length := max(len(value.array()), len(metadata.array()))
		for i := 0; i < length; i++ {
			v, m := item(value, i), item(metadata, i)
			if !primitive(f.typ) {
				if v == nil || v.kind == 'n' {
					d.add("illegal-null", "invalid", first(v, metadata))
					continue
				}
				d.validateElement(v, nil, field{typ: f.typ})
				continue
			}
			// Null padding is legal only when the aligned slot carries a value or metadata.
			if v != nil && v.kind == 'n' {
				v = nil
			}
			if m != nil && m.kind == 'n' {
				m = nil
			}
			if v == nil && m == nil {
				d.add("empty-primitive-slot", "invalid", first(item(value, i), item(metadata, i)))
				continue
			}
			d.validateElement(v, m, field{typ: f.typ})
		}
		return
	}
	if primitive(f.typ) {
		if value != nil {
			v := scalar(f.typ, value)
			if v.State != "present" {
				d.add("primitive-"+f.typ, v.State, value)
			}
		}
		if metadata != nil {
			if metadata.kind != '{' || len(metadata.members) == 0 {
				d.add("primitive-companion", "invalid", metadata)
			} else {
				for _, m := range metadata.members {
					if m.key != "id" && m.key != "extension" {
						d.add("primitive-companion-member", "invalid", m.value)
					}
				}
				d.validateObject(metadata, "PrimitiveMetadata")
			}
		}
		return
	}
	if metadata != nil {
		d.add("nonprimitive-companion", "invalid", metadata)
	}
	if value == nil || value.kind == 'n' {
		d.add("illegal-null", "invalid", value)
		return
	}
	if f.typ == "Resource" {
		if value.kind != '{' || value.field("resourceType").string() == "" {
			d.add("resource-object", "invalid", value)
		}
		return
	}
	d.validateObject(value, f.typ)
}
func (d *Document) validateUnknown(n *node, allowNull bool) {
	if n == nil {
		return
	}
	switch n.kind {
	case 'n':
		if !allowNull {
			d.add("illegal-null", "invalid", n)
		}
	case '"':
		if n.text == "" {
			d.add("empty-string", "invalid", n)
		}
	case '[':
		if len(n.items) == 0 {
			d.add("empty-array", "invalid", n)
		}
		for _, item := range n.items {
			d.validateUnknown(item, false)
		}
	case '{':
		if len(n.members) == 0 {
			d.add("empty-object", "invalid", n)
		}
		for _, m := range n.members {
			d.validateUnknown(m.value, false)
		}
	}
}
func first(a, b *node) *node {
	if a != nil {
		return a
	}
	return b
}
func item(n *node, index int) *node {
	if n == nil || n.kind != '[' || index >= len(n.items) {
		return nil
	}
	return n.items[index]
}
