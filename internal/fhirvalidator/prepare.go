package fhirvalidator

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/fhirr4"
)

func (s Status) Error() string { return "FHIR validation " + s.State + "; " + s.Requirement }
func unavailable(state, requirement string) error {
	return Status{State: state, Requirement: requirement}
}

type Plan struct {
	request    Request
	input      []byte
	capability *Capability
	profiles   []string
	packages   []string
	identity   string
}

func (p *Plan) Identity() string        { return p.identity }
func (p *Plan) Request() Request        { var r Request; _ = json.Unmarshal(encode(p.request), &r); return r }
func (Plan) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "FHIR validation plan (private)") }

// Check verifies the qualified validator, Java and complete offline package
// roots without a resource or worker. It names installed metadata requirements
// only; Engine.Check remains the separate local daemon and exact image check.
func (c *Capability) Check() error {
	if c == nil || c.identity == "" {
		return unavailable("worker-missing", "install the optional local validation capability")
	}
	m := c.manifest
	if m.Validator.Version != validatorVersion || m.Validator.SHA256 != validatorSHA256 || m.Runtime.Version != runtimeVersion || m.Runtime.SHA256 != runtimeSHA256 {
		return unavailable("unsupported-runtime", "install the qualified validator and Java combination")
	}
	packages := map[string]string{}
	for _, p := range m.Packages {
		packages[key(PackageRef{p.ID, p.Version})] = p.SHA256
	}
	roots := map[string]bool{}
	for _, p := range m.ValidatorPackages {
		roots[key(p)] = true
	}
	for required, sha256 := range requiredRoots {
		if packages[required] != sha256 || !roots[required] {
			return unavailable("package-unavailable", "restage the complete pinned validator package closure")
		}
	}
	return nil
}

// Prepare reads only bounded selected bytes and immutable capability metadata.
// No executable, Docker socket, credential, network or package resolver is used.
func Prepare(raw, input []byte, c *Capability) (*Plan, error) {
	if c == nil || c.identity == "" {
		return nil, unavailable("worker-missing", "install the optional local validation capability")
	}
	var r Request
	if len(raw) > 256<<10 || json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil || r.Schema != RequestSchema || r.Capability != c.identity || len(input) == 0 || len(input) > 16<<20 || r.InputSHA256 != digest(input) || r.TimeoutMS < 1 || r.TimeoutMS > 300000 || r.MaxOutputBytes < 1024 || r.MaxOutputBytes > 16<<20 || len(r.Profiles) > 32 {
		return nil, invalid
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	m := c.manifest
	if !slices.Contains([]string{"required", "not-requested"}, r.Requirements.Terminology) || !slices.Contains([]string{"required", "not-requested"}, r.Requirements.Invariants) || len(r.Requirements.FailSeverities) < 1 || len(r.Requirements.FailSeverities) > 4 {
		return nil, invalid
	}
	seenSeverity := map[string]bool{}
	for _, severity := range r.Requirements.FailSeverities {
		if !slices.Contains([]string{"fatal", "error", "warning", "information"}, severity) || seenSeverity[severity] {
			return nil, invalid
		}
		seenSeverity[severity] = true
	}
	// A gate that lets an error or fatal issue pass would turn nonconformance
	// into a pass; the author may only add warning or information to it.
	if !seenSeverity["fatal"] || !seenSeverity["error"] {
		return nil, invalid
	}
	if !boundedJSON(input) {
		return nil, unavailable("invalid-input", "select bounded FHIR R4 JSON evidence")
	}
	var resource struct {
		ResourceType string `json:"resourceType"`
		Meta         struct {
			Profile []string `json:"profile"`
		} `json:"meta"`
	}
	if json.Unmarshal(input, &resource) != nil || !fhirr4.KnownResourceType(resource.ResourceType) || len(resource.Meta.Profile) > 32 {
		return nil, unavailable("invalid-input", "select a FHIR R4 resource")
	}
	requested := []string{}
	seen := map[string]bool{}
	packageSet := map[string]bool{}
	add := func(profile Canonical) error {
		found := false
		for _, p := range m.Profiles {
			if bytes.Equal(encode(p), encode(profile)) {
				found = true
				break
			}
		}
		if !found {
			return unavailable("profile-unavailable", "stage the exact requested profile revision")
		}
		name := profile.URL
		if profile.Version != "" {
			name += "|" + profile.Version
		}
		packageSet[key(profile.Package)] = true
		if !seen[name] {
			seen[name] = true
			requested = append(requested, name)
		}
		return nil
	}
	for _, profile := range r.Profiles {
		if e := add(profile); e != nil {
			return nil, e
		}
	}
	for _, name := range resource.Meta.Profile {
		parts := strings.Split(name, "|")
		if len(parts) > 2 {
			return nil, invalid
		}
		matches := []Canonical{}
		for _, profile := range m.Profiles {
			if profile.URL == parts[0] && (len(parts) == 1 || profile.Version == parts[1]) {
				matches = append(matches, profile)
			}
		}
		if len(matches) != 1 {
			return nil, unavailable("profile-unavailable", "stage an unambiguous resource-declared profile")
		}
		if e := add(matches[0]); e != nil {
			return nil, e
		}
	}
	if len(requested) > 32 {
		return nil, invalid
	}
	slices.Sort(requested)
	selectedPackages := []string{}
	for name := range packageSet {
		selectedPackages = append(selectedPackages, name)
	}
	slices.Sort(selectedPackages)
	identity := digest(encode(struct {
		Request  Request  `json:"request"`
		Profiles []string `json:"effective_profiles"`
	}{r, requested}))
	return &Plan{request: r, input: bytes.Clone(input), capability: c, profiles: requested, packages: selectedPackages, identity: identity}, nil
}
func boundedJSON(raw []byte) bool {
	d := jsontext.NewDecoder(bytes.NewReader(raw))
	count := 0
	for {
		token, e := d.ReadToken()
		if e == io.EOF {
			return true
		}
		if e != nil {
			return false
		}
		_ = token
		count++
		if count > 200000 || d.StackDepth() > 64 {
			return false
		}
	}
}
