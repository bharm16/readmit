// Package exportreview holds value-free coverage and bounded residual checks.
// A clean scan is one check on known values, never an identification assessment.
package exportreview

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"slices"
)

const Scope = "Testing transformations and a limited coverage checklist only. Date shifting retains date precision and does not address age inference. Uncovered categories require separate assessment. A residual scan checks known byte patterns only, not identity, inference, unlisted values, or legal status. Local execution and customer approval establish no legal status."

// Categories are the 18 categories in 45 CFR 164.514(b)(2)(i), used as a
// checklist, not as a claim that policy labels identify all sensitive content.
var Categories = []string{
	"names", "geography", "dates-and-ages", "telephone-numbers", "fax-numbers",
	"email-addresses", "social-security-numbers", "medical-record-numbers",
	"health-plan-numbers", "account-numbers", "certificate-license-numbers",
	"vehicle-identifiers", "device-identifiers", "urls", "ip-addresses",
	"biometric-identifiers", "face-images", "other-unique-identifiers",
}

type Finding struct {
	Location string `json:"location"`
	Class    string `json:"class"`
	Reason   string `json:"reason"`
	Policy   string `json:"policy"`
	Resolved bool   `json:"resolved"`
}

type Coverage struct {
	Class      string `json:"class"`
	Status     string `json:"status"`
	Handled    int    `json:"handled_locations"`
	Unresolved int    `json:"unresolved_locations"`
}

func Checklist(findings []Finding) ([]Coverage, []string) {
	coverage := make([]Coverage, 0, len(Categories))
	uncovered := []string{}
	for _, category := range Categories {
		item := Coverage{Class: category, Status: "not-assessed"}
		for _, finding := range findings {
			if finding.Class != category {
				continue
			}
			if finding.Resolved {
				item.Handled++
			} else {
				item.Unresolved++
			}
		}
		if item.Handled > 0 {
			item.Status = "limited-policy-coverage"
		}
		if category == "dates-and-ages" && item.Handled > 0 {
			item.Status = "testing-only-age-and-precision-unresolved"
		}
		if item.Handled == 0 || item.Unresolved > 0 || category == "dates-and-ages" {
			uncovered = append(uncovered, category)
		}
		coverage = append(coverage, item)
	}
	return coverage, uncovered
}

type Scan struct {
	Status      string   `json:"status"`
	Files       int      `json:"files_checked"`
	KnownValues int      `json:"known_values_checked"`
	Locations   []string `json:"unresolved_locations"`
	Limitations string   `json:"limitations"`
}

// Residual checks file names and bytes, including JSON escapes and standalone
// base64 encodings. It deliberately reports locations without leaking a value.
func Residual(files map[string][]byte, terms [][]byte) Scan {
	result := Scan{Status: "passed", Files: len(files), KnownValues: len(terms), Locations: []string{}, Limitations: "Exact known bytes, their JSON escapes, and standalone base64 only; substrings of differently encoded values, unknown identifiers, images, semantics, and inference are not assessed. This is one check, not proof."}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		found := false
		for _, term := range terms {
			if len(term) == 0 {
				continue
			}
			patterns := [][]byte{term, []byte(base64.StdEncoding.EncodeToString(term))}
			if encoded, err := json.Marshal(string(term)); err == nil && len(encoded) > 2 {
				patterns = append(patterns, encoded[1:len(encoded)-1])
			}
			for _, pattern := range patterns {
				if bytes.Contains([]byte(name), pattern) || bytes.Contains(files[name], pattern) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			result.Locations = append(result.Locations, name)
		}
	}
	if len(result.Locations) > 0 {
		result.Status = "blocked"
	}
	return result
}
