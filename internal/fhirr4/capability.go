package fhirr4

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/dataset"
	"slices"
)

const ClaimsSchema = "readmit-fhir-capability-claims/v1"
const RequirementsSchema = "readmit-fhir-capability-requirements/v1"

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
type ResourceRequirement struct {
	Type              string            `json:"type"`
	Interactions      []string          `json:"interactions"`
	Search            []SearchParameter `json:"search"`
	Profiles          []string          `json:"profiles"`
	Versioning        string            `json:"versioning"`
	ConditionalCreate bool              `json:"conditional_create"`
	ConditionalRead   string            `json:"conditional_read"`
	ConditionalUpdate bool              `json:"conditional_update"`
	ConditionalDelete string            `json:"conditional_delete"`
}
type Requirements struct {
	Schema       string                `json:"schema"`
	FHIRVersion  string                `json:"fhir_version"`
	Formats      []string              `json:"formats"`
	PatchFormats []string              `json:"patch_formats"`
	Resources    []ResourceRequirement `json:"resources"`
}
type CapabilityFinding struct {
	Resource    string `json:"resource"`
	Requirement string `json:"requirement"`
	State       string `json:"state"`
}
type CapabilityCheck struct {
	SourceIdentity       string              `json:"source_identity"`
	RequirementsIdentity string              `json:"requirements_identity"`
	Schema               string              `json:"schema"`
	State                string              `json:"state"`
	Meaning              string              `json:"meaning"`
	Findings             []CapabilityFinding `json:"findings"`
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
func DecodeRequirements(raw []byte) (Requirements, error) {
	var r Requirements
	if len(raw) > 64<<10 || json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil || r.Validate() != nil {
		return r, invalid
	}
	return r, nil
}
func (r Requirements) Validate() error {
	if r.Schema != RequirementsSchema || r.FHIRVersion != Version || len(r.Resources) > 32 || len(r.Formats) > 16 || len(r.PatchFormats) > 16 || !unique(r.Formats) || !unique(r.PatchFormats) {
		return invalid
	}
	seen := map[string]bool{}
	for _, req := range r.Resources {
		if !r4ResourceTypes[req.Type] || seen[req.Type] || len(req.Interactions) > 16 || len(req.Search) > 32 || len(req.Profiles) > 32 || !unique(req.Profiles) || !unique(req.Interactions) || req.Versioning != "" && !slices.Contains(versioning, req.Versioning) || req.ConditionalRead != "" && !slices.Contains(conditionalRead, req.ConditionalRead) || req.ConditionalDelete != "" && !slices.Contains(conditionalDelete, req.ConditionalDelete) {
			return invalid
		}
		seen[req.Type] = true
		for _, op := range req.Interactions {
			if !slices.Contains(interactions, op) {
				return invalid
			}
		}
		for _, sp := range req.Search {
			if sp.Name == "" || len(sp.Name) > 128 || !slices.Contains(searchTypes, sp.Type) {
				return invalid
			}
		}
	}
	return nil
}
func (c Capabilities) Check(r Requirements) (CapabilityCheck, error) {
	out := CapabilityCheck{SourceIdentity: c.SourceIdentity, RequirementsIdentity: dataset.Digest(canonical(r)), Schema: CapabilitySchema, State: "satisfied", Meaning: "declared-claims-only-not-permission-or-workflow-success", Findings: []CapabilityFinding{}}
	if r.Validate() != nil || c.Schema != ClaimsSchema || c.FHIRVersion != Version || !digestPattern.MatchString(c.SourceIdentity) {
		return out, invalid
	}
	add := func(resource, requirement string, ok bool) {
		state := "satisfied"
		if !ok {
			state = "missing"
			out.State = "missing"
		}
		out.Findings = append(out.Findings, CapabilityFinding{resource, requirement, state})
	}
	add("", "fhir-version", c.FHIRVersion == r.FHIRVersion)
	for _, format := range r.Formats {
		add("", "format:"+format, supportsFormat(c.Formats, format))
	}
	for _, format := range r.PatchFormats {
		add("", "patch-format:"+format, slices.Contains(c.PatchFormats, format))
	}
	for _, requirement := range r.Resources {
		var declared *ResourceClaims
		for _, rest := range c.REST {
			if rest.Mode != "server" {
				continue
			}
			for i := range rest.Resources {
				if rest.Resources[i].Type == requirement.Type {
					copy := rest.Resources[i]
					declared = &copy
				}
			}
		}
		add(requirement.Type, "resource", declared != nil)
		if declared == nil {
			continue
		}
		for _, interaction := range requirement.Interactions {
			add(requirement.Type, "interaction:"+interaction, slices.Contains(declared.Interactions, interaction))
		}
		for _, parameter := range requirement.Search {
			found := false
			for _, sp := range declared.Search {
				if sp.Name == parameter.Name && sp.Type == parameter.Type && (parameter.Definition == "" || sp.Definition == parameter.Definition) {
					found = true
				}
			}
			add(requirement.Type, "search:"+parameter.Name, found)
		}
		for _, profile := range requirement.Profiles {
			add(requirement.Type, "profile:"+profile, declared.Profile == profile || slices.Contains(declared.SupportedProfiles, profile))
		}
		if requirement.Versioning != "" {
			add(requirement.Type, "versioning", declared.Versioning != "" && slices.Index(versioning, declared.Versioning) >= slices.Index(versioning, requirement.Versioning))
		}
		if requirement.ConditionalCreate {
			add(requirement.Type, "conditional-create", declared.ConditionalCreate != nil && *declared.ConditionalCreate)
		}
		if requirement.ConditionalUpdate {
			add(requirement.Type, "conditional-update", declared.ConditionalUpdate != nil && *declared.ConditionalUpdate)
		}
		if requirement.ConditionalRead != "" {
			add(requirement.Type, "conditional-read", declared.ConditionalRead == requirement.ConditionalRead || declared.ConditionalRead == "full-support" && requirement.ConditionalRead != "not-supported")
		}
		if requirement.ConditionalDelete != "" {
			add(requirement.Type, "conditional-delete", declared.ConditionalDelete == requirement.ConditionalDelete || requirement.ConditionalDelete != "not-supported" && declared.ConditionalDelete != "" && slices.Index(conditionalDelete, declared.ConditionalDelete) >= slices.Index(conditionalDelete, requirement.ConditionalDelete))
		}
	}
	return out, nil
}

func supportsFormat(formats []string, want string) bool {
	alias := func(value string) string {
		switch value {
		case "json":
			return "application/fhir+json"
		case "xml":
			return "application/fhir+xml"
		case "ttl":
			return "text/turtle"
		}
		return value
	}
	for _, format := range formats {
		if alias(format) == alias(want) {
			return true
		}
	}
	return false
}
