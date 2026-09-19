package localprofile_test

import (
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

func pinnedPack(t *testing.T) profilepack.Pack {
	t.Helper()
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatalf("decode the pinned pack: %v", err)
	}
	return pack
}

func resolvedField(t *testing.T, resolution localprofile.Resolution, id string, position int) localprofile.ResolvedField {
	t.Helper()
	for _, segment := range resolution.Segments {
		if segment.ID != id {
			continue
		}
		for _, field := range segment.Fields {
			if field.Position == position {
				return field
			}
		}
	}
	t.Fatalf("the resolution does not carry %s-%d", id, position)
	return localprofile.ResolvedField{}
}

func kinds(resolution localprofile.Resolution) []localprofile.FindingKind {
	found := make([]localprofile.FindingKind, 0, len(resolution.Findings))
	for _, finding := range resolution.Findings {
		found = append(found, finding.Kind)
	}
	return found
}

// TestResolveSeparatesProfileOriginFromLocalRules is the delivery read as one
// assertion: a name the pinned pack supplies, a name that replaced one, a name
// that exists only here, a position nobody names, and every other rule stated
// as local rather than inherited.
func TestResolveSeparatesProfileOriginFromLocalRules(t *testing.T) {
	resolution := localprofile.Resolve(decoded(t), pinnedPack(t))
	if !resolution.Pinned {
		t.Fatal("the profile does not resolve against the pack it pins")
	}
	if resolution.Support.Parse != profilepack.OutcomeSupported || resolution.Support.Labels != profilepack.OutcomeSupported ||
		resolution.Support.Structural != profilepack.OutcomeUnsupported || resolution.Support.Workflow != profilepack.OutcomeUnsupported {
		t.Fatalf("support %+v", resolution.Support)
	}

	// The pack labels SCH-1 "Placer Appointment ID"; this profile renames it,
	// and what was replaced stays visible beside the local name.
	placer := resolvedField(t, resolution, "SCH", 1)
	if placer.NameOrigin != localprofile.OriginOverridden ||
		placer.Name != "Placer appointment number" || placer.PackName != "Placer Appointment ID" {
		t.Fatalf("the overridden name resolved as %+v", placer)
	}
	// SCH-2 is left alone, so its name is the pack's and says so.
	filler := resolvedField(t, resolution, "SCH", 2)
	if filler.NameOrigin != localprofile.OriginProfile || filler.Name != "Filler Appointment ID" || filler.PackName != filler.Name {
		t.Fatalf("the profile-origin name resolved as %+v", filler)
	}
	// SCH-11 is constrained and nobody names it.
	timing := resolvedField(t, resolution, "SCH", 11)
	if timing.NameOrigin != localprofile.OriginUndeclared || timing.Name != "" || timing.PackName != "" {
		t.Fatalf("an unnamed position resolved as %+v", timing)
	}
	// A Z-segment is site-defined, so every name on it is local by nature.
	local := resolvedField(t, resolution, "ZPD", 2)
	if local.NameOrigin != localprofile.OriginLocal || local.PackName != "" {
		t.Fatalf("a site-defined name resolved as %+v", local)
	}

	// No readmit-profile-pack/v1 pack carries structural content, so no rule
	// other than a name may ever resolve with profile origin.
	for _, segment := range resolution.Segments {
		if segment.CardinalityOrigin != localprofile.OriginLocal && segment.CardinalityOrigin != localprofile.OriginUndeclared {
			t.Fatalf("segment %s cardinality resolved as %q", segment.ID, segment.CardinalityOrigin)
		}
		for _, field := range segment.Fields {
			for name, origin := range map[string]localprofile.Origin{
				"usage":       field.UsageOrigin,
				"condition":   field.ConditionOrigin,
				"cardinality": field.CardinalityOrigin,
				"type":        field.TypeOrigin,
				"terminology": field.TerminologyOrigin,
				"authority":   field.AuthorityOrigin,
				"date":        field.DateOrigin,
			} {
				if origin != localprofile.OriginLocal && origin != localprofile.OriginUndeclared {
					t.Errorf("%s-%d %s resolved as %q, which no v1 pack can back", segment.ID, field.Position, name, origin)
				}
			}
		}
	}

	// A declared rule resolves to the set it names, and an undeclared one to
	// nothing at all.
	reason := resolvedField(t, resolution, "ZPD", 3)
	if reason.TerminologyOrigin != localprofile.OriginLocal || reason.Terminology == nil ||
		reason.Terminology.Binding != localprofile.BindingRequired || len(reason.Terminology.Codes) != 2 {
		t.Fatalf("the bound code table resolved as %+v", reason.Terminology)
	}
	if reason.Condition == nil || reason.ConditionOrigin != localprofile.OriginLocal ||
		reason.Condition.Operator != localprofile.ConditionValueIn ||
		!slices.Equal(reason.Condition.Values, []string{"BOOKED", "RESCHEDULED"}) {
		t.Fatalf("the conditional requirement resolved as %q %+v", reason.ConditionOrigin, reason.Condition)
	}
	if reason.AuthorityOrigin != localprofile.OriginUndeclared || reason.Authority != nil ||
		reason.DateOrigin != localprofile.OriginUndeclared || reason.Date != nil {
		t.Fatalf("an undeclared rule resolved as %+v", reason)
	}
	if note := resolvedField(t, resolution, "ZPD", 4); note.ConditionOrigin != localprofile.OriginUndeclared || note.Condition != nil {
		t.Fatalf("a field with no condition resolved as %q %+v", note.ConditionOrigin, note.Condition)
	}
	mrn := resolvedField(t, resolution, "ZPD", 2)
	if mrn.Authority == nil || mrn.Authority.UniversalIDType != "ISO" {
		t.Fatalf("the bound authority resolved as %+v", mrn.Authority)
	}
	appointment := resolvedField(t, resolution, "SCH", 11)
	if appointment.Date == nil || appointment.Date.Precision != localprofile.PrecisionMinute ||
		appointment.Date.TimeZone != localprofile.TimeZoneRequired {
		t.Fatalf("the bound date rule resolved as %+v", appointment.Date)
	}

	found := kinds(resolution)
	for _, kind := range []localprofile.FindingKind{
		localprofile.FindingNoStructuralBacking,
		localprofile.FindingLabelOverridden,
		localprofile.FindingPositionUnlabelled,
	} {
		if !slices.Contains(found, kind) {
			t.Errorf("the resolution does not state %q: %v", kind, found)
		}
	}
	for _, kind := range []localprofile.FindingKind{
		localprofile.FindingPackNotPinned,
		localprofile.FindingCombinationUnknown,
		localprofile.FindingLabelsNotSupported,
	} {
		if slices.Contains(found, kind) {
			t.Errorf("the resolution states %q about a pinned, supported combination", kind)
		}
	}
}

// A pack that is not the pinned one contributes nothing at all: the pin is
// checked first, no label is read, and every name is local or undeclared.
func TestAnUnpinnedPackContributesNothing(t *testing.T) {
	profile := decoded(t)
	profile.Base.Pack = profilepack.Identity{ID: "some-other-pack", Version: "1"}
	resolution := localprofile.Resolve(profile, pinnedPack(t))
	if resolution.Pinned {
		t.Fatal("a pack that is not the pinned one was read")
	}
	for _, outcome := range []profilepack.Outcome{
		resolution.Support.Parse, resolution.Support.Labels,
		resolution.Support.Structural, resolution.Support.Workflow,
	} {
		if outcome != profilepack.OutcomeUnknown {
			t.Fatalf("an unpinned pack answered %q", outcome)
		}
	}
	for _, segment := range resolution.Segments {
		for _, field := range segment.Fields {
			if field.NameOrigin == localprofile.OriginProfile || field.NameOrigin == localprofile.OriginOverridden || field.PackName != "" {
				t.Fatalf("%s-%d took a name from an unpinned pack: %+v", segment.ID, field.Position, field)
			}
		}
	}
	if !slices.Contains(kinds(resolution), localprofile.FindingPackNotPinned) {
		t.Fatalf("the resolution does not state that the pack is not the pinned one: %v", kinds(resolution))
	}
}

// A pack assembled in Go without going through its own reader satisfies no
// pin, so the pack reader's refusals cannot be walked around from this side.
func TestAPackThatDidNotComeThroughItsReaderIsNotPinned(t *testing.T) {
	assembled := profilepack.Pack{
		Schema:   profilepack.Schema,
		Identity: profilepack.Identity{ID: "fixture-siu", Version: "1"},
		Coverage: []profilepack.Coverage{{
			HL7Version: "2.5.1", Family: "SIU",
			Parse: profilepack.Supported, Labels: profilepack.Supported,
			Structural: profilepack.Supported, Workflow: profilepack.Supported,
		}},
		Labels: []profilepack.Labels{{
			HL7Version: "2.5.1",
			Segments:   map[string]map[int]string{"SCH": {2: "Invented Label"}},
		}},
	}
	resolution := localprofile.Resolve(decoded(t), assembled)
	if resolution.Pinned {
		t.Fatal("a pack that did not come through its reader was treated as pinned")
	}
	if name := resolvedField(t, resolution, "SCH", 2).Name; name != "" {
		t.Fatalf("a name reached the resolution from an unread pack: %q", name)
	}
}

// The support level a combination carries is stated, never borrowed. Labels
// that are untested, and a combination the pack never declares, each leave
// every name local or undeclared and say so.
func TestACombinationWithoutLabelSupportOriginatesNoName(t *testing.T) {
	for _, tc := range []struct {
		name    string
		family  string
		version string
		want    localprofile.FindingKind
		labels  profilepack.Outcome
	}{
		{"labels the pack declares untested", "ADT", "2.5.1", localprofile.FindingLabelsNotSupported, profilepack.OutcomeUntested},
		{"a combination the pack never declares", "ORU", "2.6", localprofile.FindingCombinationUnknown, profilepack.OutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := decoded(t)
			profile.Base.Family, profile.Base.HL7Version = tc.family, tc.version
			if err := profile.Validate(); err != nil {
				t.Fatalf("the reshaped profile is not valid: %v", err)
			}
			resolution := localprofile.Resolve(profile, pinnedPack(t))
			if !resolution.Pinned {
				t.Fatal("the pinned pack was not read")
			}
			if resolution.Support.Labels != tc.labels {
				t.Fatalf("labels support %q", resolution.Support.Labels)
			}
			if !slices.Contains(kinds(resolution), tc.want) {
				t.Fatalf("the resolution does not state %q: %v", tc.want, kinds(resolution))
			}
			for _, segment := range resolution.Segments {
				for _, field := range segment.Fields {
					if field.PackName != "" || field.NameOrigin == localprofile.OriginProfile || field.NameOrigin == localprofile.OriginOverridden {
						t.Fatalf("%s-%d took a name from a combination whose labels are %q", segment.ID, field.Position, resolution.Support.Labels)
					}
				}
			}
		})
	}
}

// Structural backing is stated for every profile, because no v1 pack can
// declare it: the statement is what stops a locally invented cardinality from
// being read as a profile-backed rule.
func TestEveryResolutionStatesThatNoPackBacksItsConstraints(t *testing.T) {
	resolution := localprofile.Resolve(decoded(t), pinnedPack(t))
	for _, finding := range resolution.Findings {
		if finding.Kind == localprofile.FindingNoStructuralBacking {
			if finding.Detail == "" {
				t.Fatal("the structural statement says nothing")
			}
			return
		}
	}
	t.Fatalf("no resolution finding states the missing structural backing: %v", kinds(resolution))
}

// What a resolution hands out is a copy of the profile's own rules.
func TestAResolutionDoesNotShareSlicesWithTheProfile(t *testing.T) {
	profile := decoded(t)
	resolution := localprofile.Resolve(profile, pinnedPack(t))
	reason := resolvedField(t, resolution, "ZPD", 3)
	reason.Terminology.Codes[0].Code = "REWRITTEN"
	reason.Condition.Values[0] = "REWRITTEN"
	if profile.Terminology[0].Codes[0].Code == "REWRITTEN" {
		t.Fatal("a resolved code table shares its codes with the profile")
	}
	if profile.Segments[1].Fields[2].Condition.Values[0] == "REWRITTEN" {
		t.Fatal("a resolved condition shares its values with the profile")
	}
}
