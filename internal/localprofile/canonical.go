package localprofile

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
)

// Canonical is the one way a local profile becomes bytes. It holds the profile
// to the whole contract, puts its segments, fields and declared sets in the one
// order the contract writes them in, and writes it deterministically with
// members in the order the contract declares them, so the same rules are always
// the same bytes whatever order their parts were added or written in. A code
// table's codes and a condition's values keep the order they were written in.
// It answers the profile in that order beside its document; the profile it was
// given is not changed and shares no slice with the one it answers.
//
// Sealing a version, packaging a profile and saving one from the desktop all
// write a profile through here, so what a version was sealed over, what a
// package carries and what a window saved are the same document. Indentation
// can write a profile a reader accepted past MaxProfileBytes; sealing refuses
// such a profile, so nothing is written that could not be read back.
func Canonical(profile Profile) (Profile, []byte, error) {
	if err := profile.Validate(); err != nil {
		return Profile{}, nil, err
	}
	ordered := profile.clone()
	ordered.canonicalize()
	data, err := json.Marshal(ordered, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return Profile{}, nil, errors.New("a local profile could not be written as JSON")
	}
	return ordered, append(data, '\n'), nil
}

// canonicalize sorts every list in place into the one order the contract
// writes it in: segments by identifier, fields by position, and every declared
// set by its id. Ordering is not meaning here — this contract version
// constrains named segments and says nothing about where they appear in a
// message — so the same profile is always the same bytes whatever order its
// parts were added in.
func (p *Profile) canonicalize() {
	slices.SortFunc(p.Segments, func(a, b Segment) int { return cmp.Compare(a.ID, b.ID) })
	for i := range p.Segments {
		slices.SortFunc(p.Segments[i].Fields, func(a, b Field) int { return cmp.Compare(a.Position, b.Position) })
	}
	slices.SortFunc(p.Terminology, func(a, b TerminologySet) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(p.Authorities, func(a, b Authority) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(p.Dates, func(a, b DateHandling) int { return cmp.Compare(a.ID, b.ID) })
}

// clone copies every list, so a canonical profile and the profile it came from
// never share a slice.
func (p Profile) clone() Profile {
	copied := p
	copied.Terminology = slices.Clone(p.Terminology)
	for i, set := range copied.Terminology {
		copied.Terminology[i].Codes = slices.Clone(set.Codes)
	}
	copied.Authorities = slices.Clone(p.Authorities)
	copied.Dates = slices.Clone(p.Dates)
	copied.Segments = slices.Clone(p.Segments)
	for i, segment := range copied.Segments {
		copied.Segments[i].Cardinality = cloneCardinality(segment.Cardinality)
		copied.Segments[i].Fields = slices.Clone(segment.Fields)
		for j, field := range copied.Segments[i].Fields {
			copied.Segments[i].Fields[j].Cardinality = cloneCardinality(field.Cardinality)
			if field.Condition != nil {
				condition := *field.Condition
				condition.Values = slices.Clone(field.Condition.Values)
				copied.Segments[i].Fields[j].Condition = &condition
			}
		}
	}
	return copied
}

func cloneCardinality(cardinality *Cardinality) *Cardinality {
	if cardinality == nil {
		return nil
	}
	copied := *cardinality
	return &copied
}
