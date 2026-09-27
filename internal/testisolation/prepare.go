package testisolation

import (
	"bytes"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Prepare reads separately selected local registration/policy files. It does
// not discover capabilities, invoke providers, resolve names or connect.
func Prepare(contract []byte, registryPath, policyPath string, options Options) (*Prepared, error) {
	registry, err := (artifactdir.Document{MaxBytes: MaxBytes}).Read(registryPath)
	if err != nil {
		return nil, refused
	}
	policy, err := (artifactdir.Document{MaxBytes: MaxBytes}).Read(policyPath)
	if err != nil {
		return nil, refused
	}
	p, err := prepare(contract, registry, policy, options)
	if err != nil {
		return nil, err
	}
	p.registryPath, p.policyPath = registryPath, policyPath
	p.registryDigest, p.policyDigest = networkaction.Digest(registry), networkaction.Digest(policy)
	return p, nil
}
func prepare(raw, registryRaw, policy []byte, options Options) (*Prepared, error) {
	var c Contract
	var registry Registry
	if decode(raw, &c) != nil || decode(registryRaw, &registry) != nil || c.Schema != ContractSchema || registry.Schema != RegistrySchema || len(registry.Adapters) == 0 || len(registry.Adapters) > 32 || !networkaction.ValidDigest(options.ParentPlan) || !token.MatchString(options.Instance) {
		return nil, refused
	}
	for _, id := range []string{c.Project, c.Environment, c.Revision, c.Adapter, c.Tenant, c.Namespace} {
		if !token.MatchString(id) {
			return nil, refused
		}
	}
	if !slices.Contains([]string{"isolated-tenant", "reserved-namespace", "recorded-baseline"}, c.Mode) || c.Concurrency != "exclusive-target-lease" || len(c.Resources) > 32 || len(c.Resources) == 0 || len(c.Manual) > 16 || c.Mode == "recorded-baseline" && !networkaction.ValidDigest(c.Baseline) || c.Mode != "recorded-baseline" && c.Baseline != "" {
		return nil, refused
	}
	var registration *Registration
	ids := map[string]bool{}
	for i := range registry.Adapters {
		a := &registry.Adapters[i]
		if !token.MatchString(a.ID) || ids[a.ID] {
			return nil, refused
		}
		ids[a.ID] = true
		if a.ID == c.Adapter {
			registration = a
		}
	}
	if registration == nil {
		return nil, refused
	}
	a := *registration
	if a.Project != c.Project || a.Environment != c.Environment || a.EnvironmentRevision != c.Revision || a.Classification != "nonproduction" || a.Tenant != c.Tenant || a.Namespace != c.Namespace || !token.MatchString(a.Revision) || a.TimeoutMS < 1 || a.TimeoutMS > 30000 || len(a.Templates) == 0 || len(a.Templates) > 32 {
		return nil, refused
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || len(a.URL) > 2048 {
		return nil, refused
	}
	a.URL = strings.TrimSuffix(a.URL, "/")
	endpointIDs := map[string]bool{}
	locators := map[string]bool{}
	for i, role := range []Credential{a.Read, a.Setup, a.Cleanup} {
		purpose := sendpolicy.SetupAction
		if i == 0 {
			purpose = sendpolicy.ObservationRead
		}
		if !token.MatchString(role.Endpoint) || endpointIDs[role.Endpoint] || role.Reference.Purpose != purpose {
			return nil, refused
		}
		endpointIDs[role.Endpoint] = true
		key := string(canonical(role.Reference.Locator))
		if locators[key] {
			return nil, refused
		}
		locators[key] = true
	}
	templates := map[string]Template{}
	for _, t := range a.Templates {
		if !token.MatchString(t.ID) || !kind(t.Kind) || len(t.Attributes) > 32 {
			return nil, refused
		}
		if _, ok := templates[t.ID]; ok {
			return nil, refused
		}
		seen := map[string]bool{}
		for _, field := range t.Attributes {
			if !token.MatchString(field) || seen[field] {
				return nil, refused
			}
			seen[field] = true
		}
		templates[t.ID] = t
	}
	p := &Prepared{registration: a, document: planDocument{Schema: PlanSchema, Contract: c, Registry: registry, Policy: bytes.Clone(policy), Options: options}}
	owner := networkaction.Digest(canonical(struct {
		Contract string
		Options  Options
	}{networkaction.Digest(canonical(c)), options}))
	ns := c.Namespace
	if c.Mode == "reserved-namespace" {
		if len(ns) > 40 {
			return nil, refused
		}
		ns += "-" + owner[:16]
	}
	// Serialize the entire tenant even across namespace and URL aliases.
	// The registered target must enforce this tenant boundary itself.
	leaseScope := "readmit-fixture-tenant/v1/" + c.Tenant
	p.document.Scope = Scope{Project: c.Project, Environment: c.Environment, Revision: c.Revision, AdapterRevision: a.Revision, Tenant: c.Tenant, Namespace: ns, Owner: owner, LeaseKey: networkaction.Digest([]byte(leaseScope))}
	seen := map[string]bool{}
	references := map[string]Reference{}
	physical := map[string]bool{}
	for _, r := range c.Resources {
		t, ok := templates[r.Template]
		if !ok || t.Kind != r.Kind || !token.MatchString(r.ID) || seen[r.ID] || !slices.Contains([]string{"create", "claim", "select"}, r.Ownership) || len(r.DependsOn) > 32 || len(r.Attributes) > 32 || len(r.Identifiers) > 16 {
			return nil, refused
		}
		seen[r.ID] = true
		if r.Ownership == "create" && (r.LogicalID != "" || r.Version != "") || r.Ownership != "create" && (!token.MatchString(r.LogicalID) || !token.MatchString(r.Version)) {
			return nil, refused
		}
		for k, v := range r.Attributes {
			if !slices.Contains(t.Attributes, k) || len(v) > 1024 {
				return nil, refused
			}
		}
		id := r.LogicalID
		if r.Ownership == "create" {
			id = "r-" + networkaction.Digest([]byte(owner + "/" + r.Kind + "/" + r.ID))[:32]
		}
		if physical[r.Kind+"/"+id] {
			return nil, refused
		}
		physical[r.Kind+"/"+id] = true
		resource := Resource{Alias: r.ID, Kind: r.Kind, ID: id, Version: r.Version, Template: r.Template, Attributes: clone(r.Attributes), Identifiers: clone(r.Identifiers), References: []Reference{}}
		for i, identifier := range resource.Identifiers {
			if !kind(identifier.Scope) || !token.MatchString(identifier.Namespace) || len(identifier.Value) > 256 {
				return nil, refused
			}
			if identifier.Value == "" {
				resource.Identifiers[i].Value = "b-" + networkaction.Digest([]byte(owner + "/" + r.ID + "/" + identifier.Scope + "/" + strconv.Itoa(i)))[:24]
			}
		}
		dependencies := map[string]bool{}
		for _, dependency := range r.DependsOn {
			ref, ok := references[dependency]
			if !ok || dependencies[dependency] {
				return nil, refused
			}
			dependencies[dependency] = true
			resource.References = append(resource.References, ref)
		}
		references[r.ID] = Reference{Kind: r.Kind, ID: id}
		p.document.Resources = append(p.document.Resources, resource)
	}
	manual := map[string]bool{}
	for _, m := range c.Manual {
		if !token.MatchString(m.ID) || manual[m.ID] || len(m.Instructions) < 1 || len(m.Instructions) > 4096 {
			return nil, refused
		}
		manual[m.ID] = true
	}
	if len(canonical(p.document.Resources)) > 64<<10 || len(canonical(p.document)) > MaxBytes {
		return nil, refused
	}
	p.identity = networkaction.Digest(canonical(p.document))
	// Preparing each role also validates provider references and policy bindings.
	for _, phase := range []string{"read", "setup", "cleanup"} {
		action := "capabilities"
		if phase != "read" {
			action = "lease-acquire"
		}
		if _, err := p.httpPlan(phase, action, nil); err != nil {
			return nil, err
		}
	}
	return p, nil
}
func kind(k string) bool {
	return slices.Contains([]string{"patient", "visit", "appointment", "order", "logical-resource", "business-identifier", "practitioner", "location", "reference"}, k)
}
func (p *Prepared) current() error {
	if p.registryPath == "" || p.policyPath == "" {
		return refused
	}
	for _, pin := range []struct{ path, digest string }{{p.registryPath, p.registryDigest}, {p.policyPath, p.policyDigest}} {
		raw, err := (artifactdir.Document{MaxBytes: MaxBytes}).Read(pin.path)
		if err != nil || networkaction.Digest(raw) != pin.digest {
			return refused
		}
	}
	return nil
}
func (p *Prepared) role(phase string) Credential {
	switch phase {
	case "setup":
		return p.registration.Setup
	case "cleanup":
		return p.registration.Cleanup
	}
	return p.registration.Read
}
func (p *Prepared) Review(phase string) Review {
	role := p.role(phase)
	configuration := p.identity
	if p.recovery != "" {
		configuration = networkaction.Digest([]byte(p.identity + "/" + p.recovery))
	}
	r := Review{Schema: ReviewSchema, Identity: p.identity, Phase: phase, Scope: p.document.Scope, Manual: clone(p.document.Contract.Manual), Effects: []Effect{}, Binding: networkaction.Binding{Plan: p.document.Options.ParentPlan, Source: networkaction.Digest(canonical(p.document.Contract)), Configuration: configuration, Policy: networkaction.Digest(p.document.Policy), Credentials: networkaction.Digest(canonical(role.Reference)), Project: p.document.Contract.Project, Environment: p.document.Contract.Environment, Revision: p.document.Contract.Revision, Endpoint: role.Endpoint, Operation: role.Reference.Purpose}}
	for i, resource := range p.document.Resources {
		operation := p.document.Contract.Resources[i].Ownership
		if phase == "read" {
			operation = "observe"
		} else if phase == "cleanup" {
			if operation == "select" {
				continue
			}
			operation = "delete-owned-version"
		}
		r.Effects = append(r.Effects, Effect{Alias: resource.Alias, Kind: resource.Kind, ID: resource.ID, Operation: operation, Expected: clone(resource.Attributes)})
	}
	if phase == "cleanup" {
		slices.Reverse(r.Effects)
	}
	return r
}
