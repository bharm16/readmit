package fhirr4

import (
	"slices"
)

const ClaimsSchema = "readmit-fhir-capability-claims/v1"

type SearchParameter struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Definition string `json:"definition"`
}
type ResourceClaims struct {
	Type              string            `json:"type"`
	Profile           string            `json:"profile"`
	SupportedProfiles []string          `json:"supported_profiles"`
	Interactions      []string          `json:"interactions"`
	Search            []SearchParameter `json:"search"`
	Versioning        string            `json:"versioning"`
	ConditionalCreate *bool             `json:"conditional_create"`
	ConditionalRead   string            `json:"conditional_read"`
	ConditionalUpdate *bool             `json:"conditional_update"`
	ConditionalDelete string            `json:"conditional_delete"`
}
type RESTClaims struct {
	Mode         string           `json:"mode"`
	Resources    []ResourceClaims `json:"resources"`
	Interactions []string         `json:"interactions"`
}
type Capabilities struct {
	SourceIdentity string       `json:"source_identity"`
	Schema         string       `json:"schema"`
	Occurrence     string       `json:"occurrence"`
	FHIRVersion    string       `json:"fhir_version"`
	Formats        []string     `json:"formats"`
	PatchFormats   []string     `json:"patch_formats"`
	REST           []RESTClaims `json:"rest"`
}

func init() {
	definitions["CapabilityStatement"] = merge(domainResource, map[string]field{"url": one("uri"), "version": one("string"), "name": one("string"), "title": one("string"), "status": required("code"), "experimental": one("boolean"), "date": required("dateTime"), "publisher": one("string"), "contact": many("ContactDetail"), "description": one("markdown"), "useContext": many("UsageContext"), "jurisdiction": many("CodeableConcept"), "purpose": one("markdown"), "copyright": one("markdown"), "kind": required("code"), "instantiates": many("canonical"), "imports": many("canonical"), "software": one("Software"), "implementation": one("Implementation"), "fhirVersion": required("code"), "format": {typ: "code", many: true, required: true}, "patchFormat": many("code"), "implementationGuide": many("canonical"), "rest": many("REST"), "messaging": many("Messaging"), "document": many("CapabilityDocument")})
	definitions["Software"] = merge(backbone, map[string]field{"name": required("string"), "version": one("string"), "releaseDate": one("dateTime")})
	definitions["Implementation"] = merge(backbone, map[string]field{"description": required("string"), "url": one("url"), "custodian": one("Reference")})
	definitions["REST"] = merge(backbone, map[string]field{"mode": required("code"), "documentation": one("markdown"), "security": one("RESTSecurity"), "resource": many("RESTResource"), "interaction": many("Interaction"), "searchParam": many("SearchParameter"), "operation": many("Operation"), "compartment": many("canonical")})
	definitions["RESTSecurity"] = merge(backbone, map[string]field{"cors": one("boolean"), "service": many("CodeableConcept"), "description": one("markdown")})
	definitions["RESTResource"] = merge(backbone, map[string]field{"type": required("code"), "profile": one("canonical"), "supportedProfile": many("canonical"), "documentation": one("markdown"), "interaction": many("Interaction"), "versioning": one("code"), "readHistory": one("boolean"), "updateCreate": one("boolean"), "conditionalCreate": one("boolean"), "conditionalRead": one("code"), "conditionalUpdate": one("boolean"), "conditionalDelete": one("code"), "referencePolicy": many("code"), "searchInclude": many("string"), "searchRevInclude": many("string"), "searchParam": many("SearchParameter"), "operation": many("Operation")})
	definitions["Interaction"] = merge(backbone, map[string]field{"code": required("code"), "documentation": one("markdown")})
	definitions["SearchParameter"] = merge(backbone, map[string]field{"name": required("string"), "definition": one("canonical"), "type": required("code"), "documentation": one("markdown")})
	definitions["Operation"] = merge(backbone, map[string]field{"name": required("string"), "definition": required("canonical"), "documentation": one("markdown")})
	definitions["ContactDetail"] = merge(baseElement, map[string]field{"name": one("string"), "telecom": many("ContactPoint")})
}
func stringsOf(n *node) []string {
	out := []string{}
	for _, item := range n.array() {
		out = append(out, item.string())
	}
	return out
}
func boolOf(n *node) *bool {
	if n == nil || n.kind != 't' && n.kind != 'f' {
		return nil
	}
	v := n.kind == 't'
	return &v
}

var interactions = []string{"read", "vread", "update", "patch", "delete", "history-instance", "history-type", "create", "search-type"}
var searchTypes = []string{"number", "date", "string", "token", "reference", "composite", "quantity", "uri", "special"}
var versioning = []string{"no-version", "versioned", "versioned-update"}
var conditionalRead = []string{"not-supported", "modified-since", "not-match", "full-support"}
var conditionalDelete = []string{"not-supported", "single", "multiple"}

func unique(values []string) bool {
	seen := map[string]bool{}
	for _, v := range values {
		if v == "" || len(v) > 4096 || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

// Capabilities returns finite recorded claims, not tested compatibility,
// permission, or profile conformance. It never fetches /metadata or profiles.
func (d *Document) Capabilities(occurrenceID string) (Capabilities, error) {
	r := d.resource(occurrenceID)
	if r == nil || r.resource.Type != "CapabilityStatement" || r.invalid || r.unsupported {
		return Capabilities{}, invalid
	}
	n := r.node
	c := Capabilities{SourceIdentity: d.Identity(), Schema: ClaimsSchema, Occurrence: occurrenceID, FHIRVersion: n.field("fhirVersion").string(), Formats: stringsOf(n.field("format")), PatchFormats: stringsOf(n.field("patchFormat")), REST: []RESTClaims{}}
	if c.FHIRVersion != Version || len(c.Formats) == 0 || !unique(c.Formats) || !unique(c.PatchFormats) || !slices.Contains([]string{"draft", "active", "retired", "unknown"}, n.field("status").string()) || !slices.Contains([]string{"instance", "capability", "requirements"}, n.field("kind").string()) {
		return Capabilities{}, invalid
	}
	modes := map[string]bool{}
	for _, rest := range n.field("rest").array() {
		mode := rest.field("mode").string()
		if mode != "server" && mode != "client" || modes[mode] {
			return Capabilities{}, invalid
		}
		modes[mode] = true
		claims := RESTClaims{Mode: mode, Resources: []ResourceClaims{}, Interactions: []string{}}
		types := map[string]bool{}
		for _, interaction := range rest.field("interaction").array() {
			v := interaction.field("code").string()
			if !slices.Contains([]string{"transaction", "batch", "search-system", "history-system"}, v) {
				return Capabilities{}, invalid
			}
			claims.Interactions = append(claims.Interactions, v)
		}
		if !unique(claims.Interactions) || len(rest.field("resource").array()) > 256 {
			return Capabilities{}, invalid
		}
		for _, resource := range rest.field("resource").array() {
			typ := resource.field("type").string()
			if !r4ResourceTypes[typ] || types[typ] {
				return Capabilities{}, invalid
			}
			types[typ] = true
			rc := ResourceClaims{Type: typ, Profile: resource.field("profile").string(), SupportedProfiles: stringsOf(resource.field("supportedProfile")), Interactions: []string{}, Search: []SearchParameter{}, Versioning: resource.field("versioning").string(), ConditionalCreate: boolOf(resource.field("conditionalCreate")), ConditionalRead: resource.field("conditionalRead").string(), ConditionalUpdate: boolOf(resource.field("conditionalUpdate")), ConditionalDelete: resource.field("conditionalDelete").string()}
			if rc.Versioning != "" && !slices.Contains(versioning, rc.Versioning) || rc.ConditionalRead != "" && !slices.Contains(conditionalRead, rc.ConditionalRead) || rc.ConditionalDelete != "" && !slices.Contains(conditionalDelete, rc.ConditionalDelete) || !unique(rc.SupportedProfiles) {
				return Capabilities{}, invalid
			}
			for _, interaction := range resource.field("interaction").array() {
				code := interaction.field("code").string()
				if !slices.Contains(interactions, code) {
					return Capabilities{}, invalid
				}
				rc.Interactions = append(rc.Interactions, code)
			}
			if !unique(rc.Interactions) {
				return Capabilities{}, invalid
			}
			searches := map[string]bool{}
			for _, parameter := range resource.field("searchParam").array() {
				sp := SearchParameter{Name: parameter.field("name").string(), Type: parameter.field("type").string(), Definition: parameter.field("definition").string()}
				if sp.Name == "" || len(sp.Name) > 128 || searches[sp.Name] || !slices.Contains(searchTypes, sp.Type) {
					return Capabilities{}, invalid
				}
				searches[sp.Name] = true
				rc.Search = append(rc.Search, sp)
			}
			claims.Resources = append(claims.Resources, rc)
		}
		c.REST = append(c.REST, claims)
	}
	return c, nil
}
