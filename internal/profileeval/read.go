package profileeval

import (
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
	"regexp"
	"slices"
	"strconv"
)

var token = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var segment = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)
var invalid = errors.New("invalid profile evaluation contract")

func decodeProfile(raw []byte) (ProfileV2, error) {
	var head struct {
		Schema string `json:"schema"`
	}
	if len(raw) > localprofile.MaxProfileBytes || json.Unmarshal(raw, &head) != nil {
		return ProfileV2{}, invalid
	}
	var p ProfileV2
	switch head.Schema {
	case localprofile.Schema:
		d, err := localprofile.Decode(raw)
		if err != nil {
			return p, err
		}
		p = ProfileV2{Schema: localprofile.Schema, Definition: d}
	case ProfileSchemaV3:
		var v ProfileV3
		if json.Unmarshal(raw, &v, json.RejectUnknownMembers(true)) != nil || validateDatatypes(v.Datatypes, false) != nil {
			return p, invalid
		}
		p = ProfileV2{Schema: v.Schema, Definition: v.Definition, Structure: v.Structure, datatypes: v.Datatypes}
		for _, w := range v.Workflows {
			w.Workflow.parents = w.ParentSegments
			p.Workflows = append(p.Workflows, w.Workflow)
		}
		b, err := json.Marshal(v.Definition)
		if err != nil {
			return p, err
		}
		if _, err = localprofile.Decode(b); err != nil {
			return p, err
		}
	case ProfileSchema:
		if json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil {
			return p, invalid
		}
		b, err := json.Marshal(p.Definition)
		if err != nil {
			return p, err
		}
		d, err := localprofile.Decode(b)
		if err != nil {
			return p, err
		}
		p.Definition = d
	default:
		return p, errors.New("unsupported local profile version")
	}
	count := 0
	if err := validateNodes(p.Structure, 0, &count, false); err != nil {
		return p, err
	}
	if len(p.Workflows) > 32 {
		return p, invalid
	}
	seen := map[string]bool{}
	for _, w := range p.Workflows {
		if !token.MatchString(w.ID) || !token.MatchString(w.Version) || seen[w.ID] || !slices.Contains([]string{"identity-visit", "scheduling", "order-result"}, w.Kind) || len(w.Identity) < 1 || len(w.Identity) > 8 || len(w.Initial) < 1 || len(w.Initial) > 64 || len(w.Transitions) < 1 || len(w.Transitions) > 256 {
			return p, invalid
		}
		seen[w.ID] = true
		if len(w.parents) > 8 || len(w.parents) > 0 && w.RepeatSegment == "" {
			return p, invalid
		}
		parents := map[string]bool{}
		for _, parent := range w.parents {
			if !segment.MatchString(parent) || parents[parent] || parent == w.RepeatSegment {
				return p, invalid
			}
			parents[parent] = true
		}

		for _, s := range append(slices.Clone(w.Identity), w.Status) {
			if _, err := hl7.ParseSelector(s); err != nil {
				return p, err
			}
		}
		if w.RepeatSegment != "" && !segment.MatchString(w.RepeatSegment) {
			return p, invalid
		}
		for _, v := range w.Initial {
			if !token.MatchString(v) {
				return p, invalid
			}
		}
		for _, tr := range w.Transitions {
			if !token.MatchString(tr.From) || !token.MatchString(tr.To) {
				return p, invalid
			}
		}
	}
	return p, nil
}
func decodePack(raw []byte) (PackV2, error) {
	var head struct {
		Schema string `json:"schema"`
	}
	if len(raw) > MaxBytes || json.Unmarshal(raw, &head) != nil {
		return PackV2{}, invalid
	}
	var p PackV2
	switch head.Schema {
	case profilepack.Schema:
		d, err := profilepack.Decode(raw)
		if err != nil {
			return p, err
		}
		p = PackV2{Schema: profilepack.Schema, Metadata: d}
	case PackSchemaV3, PackSchemaV4, PackSchemaV5:
		var v PackV3
		if json.Unmarshal(raw, &v, json.RejectUnknownMembers(true)) != nil || validateDatatypes(v.Datatypes, v.Schema == PackSchemaV5) != nil {
			return p, invalid
		}
		p = PackV2{Schema: v.Schema, Metadata: v.Metadata, Messages: v.Messages, datatypes: v.Datatypes}
		b, err := json.Marshal(v.Metadata)
		if err != nil {
			return p, err
		}
		if _, err = profilepack.Decode(b); err != nil {
			return p, err
		}
	case PackSchema:
		if json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil {
			return p, invalid
		}
		b, err := json.Marshal(p.Metadata)
		if err != nil {
			return p, err
		}
		d, err := profilepack.Decode(b)
		if err != nil {
			return p, err
		}
		p.Metadata = d
	default:
		return p, errors.New("unsupported profile pack version")
	}
	if len(p.Messages) > 1024 {
		return p, invalid
	}
	seen := map[string]bool{}
	count := 0
	for _, m := range p.Messages {
		k := m.HL7Version + "/" + m.Structure
		if seen[k] || !slices.Contains(profilepack.HL7Versions(), m.HL7Version) || !slices.Contains(profilepack.Families(), m.Family) || !token.MatchString(m.Structure) || len(m.Sequence) == 0 || len(m.Segments) > 256 {
			return p, invalid
		}
		seen[k] = true
		if err := validateNodes(m.Sequence, 0, &count, p.Schema == PackSchemaV4 || p.Schema == PackSchemaV5); err != nil {
			return p, err
		}
		ss := map[string]bool{}
		for _, s := range m.Segments {
			if !segment.MatchString(s.ID) || ss[s.ID] || len(s.Fields) > 999 {
				return p, invalid
			}
			ss[s.ID] = true
			positions := map[int]bool{}
			for _, f := range s.Fields {
				v5 := p.Schema == PackSchemaV5
				if f.Position < 1 || f.Position > 999 || positions[f.Position] || f.MaxRepetitions < 0 || f.MaxRepetitions > 9999 || f.MaxLength < 0 || f.MaxLength > MaxBytes || !token.MatchString(f.DataType) ||
					validateUsage(f.Usage, f.Condition, f.Length, v5, f.Position, 999) != nil || v5 && (f.Required || f.MaxLength != 0) ||
					!v5 && (f.Table != "" || f.TableKind != "" || f.Policy != "" || f.Codes != nil) || v5 && (validateBinding(f.Table, f.TableKind, f.Policy, f.Codes) != nil || f.Table != "" && !token.MatchString(f.Table)) {
					return p, invalid
				}
				positions[f.Position] = true
			}
		}
	}
	return p, nil
}
func validateNodes(nodes []Node, depth int, count *int, choice bool) error {
	if depth > 16 {
		return invalid
	}
	for _, n := range nodes {
		*count++
		if *count > 20000 || !token.MatchString(n.Name) || n.Min < 0 || n.Min > 9999 || (n.Segment == "") == (len(n.Children) == 0) {
			return invalid
		}
		if n.Choice {
			// An alternative that matches nothing would hide optionality the
			// choice's own cardinality must state.
			if !choice || len(n.Children) < 2 || slices.ContainsFunc(n.Children, func(c Node) bool { return c.Min < 1 }) {
				return invalid
			}
		}
		if n.Max != "*" {
			max, err := strconv.Atoi(n.Max)
			if err != nil || max < n.Min || max > 9999 {
				return invalid
			}
		}
		if n.Segment != "" && !segment.MatchString(n.Segment) {
			return invalid
		}
		if err := validateNodes(n.Children, depth+1, count, choice); err != nil {
			return err
		}
	}
	return nil
}

// DecodeProfile and DecodePack expose the same strict contract boundary used by
// evaluation so connected compilation can validate pins without evaluating them.
func DecodeProfile(raw []byte) (ProfileV2, error) { return decodeProfile(raw) }
func DecodePack(raw []byte) (PackV2, error)       { return decodePack(raw) }
