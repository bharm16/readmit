package fhirr4

import (
	"net/url"
	"slices"
	"strings"
	"unicode"
)

type Canonical struct {
	URL      string `json:"url"`
	Version  string `json:"version"`
	Fragment string `json:"fragment"`
}

// ParseCanonical keeps business canonical versions separate from meta.versionId.
// It neither normalizes the URL nor contacts a registry to select a version.
func ParseCanonical(value string) (Canonical, error) {
	c := Canonical{}
	if value == "" || len(value) > 4096 || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return c, invalid
	}
	if at := strings.IndexByte(value, '#'); at >= 0 {
		c.Fragment = value[at+1:]
		value = value[:at]
		if !identifier.MatchString(c.Fragment) {
			return c, invalid
		}
	}
	if at := strings.IndexByte(value, '|'); at >= 0 {
		c.Version = value[at+1:]
		value = value[:at]
		if c.Version == "" || strings.Contains(c.Version, "|") || len(c.Version) > 256 {
			return c, invalid
		}
	}
	c.URL = value
	if c.URL == "" && c.Fragment == "" || c.URL == "" && c.Version != "" {
		return c, invalid
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.User != nil {
		return c, invalid
	}
	return c, nil
}
func canonicalResource(typ string) bool {
	return slices.Contains([]string{"ActivityDefinition", "CapabilityStatement", "ChargeItemDefinition", "CodeSystem", "CompartmentDefinition", "ConceptMap", "EffectEvidenceSynthesis", "EventDefinition", "Evidence", "EvidenceVariable", "ExampleScenario", "GraphDefinition", "ImplementationGuide", "Library", "Measure", "MessageDefinition", "NamingSystem", "OperationDefinition", "PlanDefinition", "Questionnaire", "ResearchDefinition", "ResearchElementDefinition", "RiskEvidenceSynthesis", "SearchParameter", "StructureDefinition", "StructureMap", "TerminologyCapabilities", "TestScript", "ValueSet"}, typ)
}
func (d *Document) ResolveCanonical(sourceID, value string) Resolution {
	if d.resource(sourceID) == nil {
		return Resolution{State: "invalid", Occurrences: []string{}}
	}
	c, err := ParseCanonical(value)
	if err != nil {
		return Resolution{State: "invalid", Occurrences: []string{}}
	}
	if c.URL == "" {
		return d.Resolve(sourceID, "#"+c.Fragment)
	}
	matches := []string{}
	for _, entry := range d.resources {
		r := entry.resource
		if r.Container == "" && r.Bundle == d.bundleScope(sourceID) && r.CanonicalURL == c.URL && (c.Version == "" || r.CanonicalVersion == c.Version) {
			matches = append(matches, r.Occurrence)
		}
	}
	result := resolved(matches)
	if result.State == "resolved" && c.Fragment != "" {
		return d.Resolve(matches[0], "#"+c.Fragment)
	}
	if len(matches) == 0 {
		result.State = "external"
	}
	return result
}
