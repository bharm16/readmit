package hl7reference

import "errors"

// Origin distinguishes chapter notation, schema fallback, identifier display
// and explicit availability. Source names an exact declared source role; the
// source's file and content hash are retained by the catalog. Locator names the
// concrete chapter member or schema entity, never an inferred parent citation.
// Value semantics keep detached answers from mutating the catalog.
type Origin struct {
	Kind    string `json:"kind"`
	Source  string `json:"source,omitzero"`
	Locator string `json:"locator,omitzero"`
}

func validOrigin(o Origin, sources map[string]bool) bool {
	if len(o.Locator) > 512 {
		return false
	}
	switch o.Kind {
	case "not_available", "not_applicable":
		return o.Source == "" && o.Locator == ""
	case "identity":
		return o.Source == "" && o.Locator != ""
	case "normative", "schema", "derived":
		return sources[o.Source] && o.Locator != ""
	}
	return false
}
func validateRecordOrigins(r Record, sources map[string]bool) error {
	if !validOrigin(r.NameOrigin, sources) || r.NameOrigin.Kind == "not_available" || r.NameOrigin.Kind == "not_applicable" || !validOrigin(r.DefinitionOrigin, sources) || (r.Definition == "") != (r.DefinitionOrigin.Kind == "not_available") {
		return errors.New("v5 reference requires truthful name and definition origins")
	}
	for _, a := range []Attribute{r.Datatype, r.Optionality, r.Length, r.ConformanceLength, r.Repetition, r.Item, r.Table, r.Section} {
		if !validOrigin(a.Origin, sources) || a.State == "not_applicable" && a.Origin.Kind != "not_applicable" || a.State == "not_available" && a.Origin.Kind != "not_available" || a.State == "specified" && (a.Origin.Kind == "not_available" || a.Origin.Kind == "not_applicable") || a.State == "not_specified" && (a.Origin.Kind == "not_available" || a.Origin.Kind == "not_applicable") {
			return errors.New("v5 attribute origin disagrees with availability or declared source")
		}
	}
	return nil
}
