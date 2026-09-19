package localprofile_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

// FuzzLocalProfileDocument exercises the local profile reader, the editor that
// writes what the reader reads, and the resolution that says where every rule
// came from.
//
// No input may panic, and no bytes at all may produce a profile that carries a
// rule outside the closed sets, a reference to a set it does not declare, a
// constraint on a field it declares unsupported, or a document the editor
// cannot write back unchanged. No bytes may produce a resolution in which a
// rule other than a field name has profile origin, or in which a pack that is
// not the pinned one contributed anything: that is the ADR-0009 rule stated as
// a property, so a locally invented constraint can never acquire the standing
// of one a pack declares.
func FuzzLocalProfileDocument(f *testing.F) {
	positive, err := os.ReadFile(fixtureRoot + "local-profile.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	negative, err := os.ReadFile(fixtureRoot + "local-profile-refused.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	packed, err := os.ReadFile(fixtureRoot + "profile-pack.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	pack, err := profilepack.Decode(packed)
	if err != nil {
		f.Fatalf("decode the pinned pack: %v", err)
	}
	unread := profilepack.Pack{
		Schema:   profilepack.Schema,
		Identity: profilepack.Identity{ID: "fixture-siu", Version: "1"},
		Labels: []profilepack.Labels{{
			HL7Version: "2.5.1",
			Segments:   map[string]map[int]string{"SCH": {1: "Invented Label"}, "ZPD": {1: "Invented Label"}},
		}},
	}
	fixture := string(positive)
	for _, seed := range []string{
		fixture,
		string(negative),
		strings.Replace(fixture, `"usage": "X"`, `"usage": "R"`, 1),
		strings.Replace(fixture, `"operator": "value_in"`, `"operator": "present"`, 1),
		strings.Replace(fixture, `"terminology": "local-visit-reason"`, `"terminology": "absent-set"`, 1),
		strings.Replace(fixture, `"family": "SIU"`, `"family": "ORU"`, 1),
		strings.Replace(fixture, `"max": "*"`, `"max": "0"`, 1),
		strings.Replace(fixture, `"segments": [`, `"command": "sh -c true", "segments": [`, 1),
		strings.Replace(fixture, "readmit-local-profile/v1", "readmit-local-profile/v2", 1),
		`{"schema":"readmit-local-profile/v1"}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		profile, err := localprofile.Decode(data)
		if err != nil {
			return
		}
		if profile.Schema != localprofile.Schema || len(profile.Segments) == 0 {
			t.Fatalf("accepted %+v", profile)
		}
		declared := declaredSets(t, profile)
		segments := map[string]bool{}
		for _, segment := range profile.Segments {
			if segments[segment.ID] {
				t.Fatalf("accepted the segment %s twice", segment.ID)
			}
			segments[segment.ID] = true
			checkSegment(t, segment, declared)
		}

		// The editor writes exactly what the reader reads, and writing again
		// changes nothing, so a profile has one canonical document.
		editor, err := localprofile.Open(profile)
		if err != nil {
			t.Fatalf("a decoded profile could not be opened: %v", err)
		}
		written, err := editor.Encode()
		if err != nil {
			t.Fatalf("a decoded profile could not be written: %v", err)
		}
		again, err := localprofile.Decode(written)
		if err != nil {
			t.Fatalf("the editor wrote a document its own reader refuses: %v", err)
		}
		rewritten, err := localprofile.Open(again)
		if err != nil {
			t.Fatalf("a written profile could not be opened: %v", err)
		}
		if second, err := rewritten.Encode(); err != nil || string(second) != string(written) {
			t.Fatalf("writing a profile twice produced different bytes: %v", err)
		}

		// A pack that did not come through its own reader is never the pinned
		// pack, whatever it claims, so nothing in it reaches a resolution.
		if unpinned := localprofile.Resolve(profile, unread); unpinned.Pinned {
			t.Fatal("a pack that did not come through its reader was treated as pinned")
		} else {
			checkOrigins(t, unpinned, false)
		}
		checkOrigins(t, localprofile.Resolve(profile, pack), true)
	})
}

func declaredSets(t *testing.T, profile localprofile.Profile) map[string]bool {
	t.Helper()
	declared := map[string]bool{}
	for _, set := range profile.Terminology {
		if !slices.Contains(localprofile.Bindings(), set.Binding) || len(set.Codes) == 0 {
			t.Fatalf("accepted the terminology set %+v", set)
		}
		declared["terminology:"+set.ID] = true
	}
	for _, authority := range profile.Authorities {
		if authority.Namespace == "" && authority.UniversalID == "" {
			t.Fatalf("accepted the authority %+v", authority)
		}
		if (authority.UniversalID == "") != (authority.UniversalIDType == "") {
			t.Fatalf("accepted the authority %+v", authority)
		}
		declared["authority:"+authority.ID] = true
	}
	for _, date := range profile.Dates {
		if !slices.Contains(localprofile.Precisions(), date.Precision) || !slices.Contains(localprofile.TimeZoneRules(), date.TimeZone) {
			t.Fatalf("accepted the date rule %+v", date)
		}
		declared["date:"+date.ID] = true
	}
	return declared
}

func checkSegment(t *testing.T, segment localprofile.Segment, declared map[string]bool) {
	t.Helper()
	if len(segment.Fields) == 0 {
		t.Fatalf("accepted the segment %s, which constrains nothing", segment.ID)
	}
	positions := map[int]bool{}
	for _, field := range segment.Fields {
		if positions[field.Position] {
			t.Fatalf("accepted %s-%d twice", segment.ID, field.Position)
		}
		positions[field.Position] = true
		if !slices.Contains(localprofile.Usages(), field.Usage) {
			t.Fatalf("accepted the usage %q", field.Usage)
		}
		if field.Type != "" && !slices.Contains(localprofile.DataTypes(), string(field.Type)) {
			t.Fatalf("accepted the data type %q", field.Type)
		}
		if field.Usage == localprofile.UsageNotSupported &&
			(field.Condition != nil || field.Cardinality != nil || field.Type != "" ||
				field.Terminology != "" || field.Authority != "" || field.Date != "") {
			t.Fatalf("accepted a constraint on the unsupported field %s-%d", segment.ID, field.Position)
		}
		if (field.Usage == localprofile.UsageConditional) != (field.Condition != nil) {
			t.Fatalf("accepted %s-%d with usage %q and condition %v", segment.ID, field.Position, field.Usage, field.Condition)
		}
		if field.Condition != nil && !slices.Contains(localprofile.ConditionOperators(), field.Condition.Operator) {
			t.Fatalf("accepted the condition operator %q", field.Condition.Operator)
		}
		for kind, reference := range map[string]string{
			"terminology": field.Terminology, "authority": field.Authority, "date": field.Date,
		} {
			if reference != "" && !declared[kind+":"+reference] {
				t.Fatalf("accepted %s-%d naming the %s %q, which the profile does not declare", segment.ID, field.Position, kind, reference)
			}
		}
	}
}

// checkOrigins is the rule this package keeps: a field name is the only rule a
// readmit-profile-pack/v1 pack can originate, and an unpinned pack originates
// nothing at all.
func checkOrigins(t *testing.T, resolution localprofile.Resolution, pinned bool) {
	t.Helper()
	fromPack := []localprofile.Origin{localprofile.OriginProfile, localprofile.OriginOverridden}
	for _, segment := range resolution.Segments {
		if slices.Contains(fromPack, segment.CardinalityOrigin) {
			t.Fatalf("the cardinality of %s resolved as %q", segment.ID, segment.CardinalityOrigin)
		}
		for _, field := range segment.Fields {
			for _, origin := range []localprofile.Origin{
				field.UsageOrigin, field.ConditionOrigin, field.CardinalityOrigin,
				field.TypeOrigin, field.TerminologyOrigin, field.AuthorityOrigin, field.DateOrigin,
			} {
				if slices.Contains(fromPack, origin) {
					t.Fatalf("a rule of %s-%d resolved as %q, which no v1 pack can back", segment.ID, field.Position, origin)
				}
			}
			if !pinned && (slices.Contains(fromPack, field.NameOrigin) || field.PackName != "") {
				t.Fatalf("%s-%d took a name from a pack that is not pinned: %+v", segment.ID, field.Position, field)
			}
			if field.NameOrigin == localprofile.OriginProfile && field.Name != field.PackName {
				t.Fatalf("%s-%d claims profile origin for a name the pack does not give: %+v", segment.ID, field.Position, field)
			}
			if field.PackName != "" && !resolution.Support.Labels.Passing() {
				t.Fatalf("%s-%d took a name from a combination whose labels are %q", segment.ID, field.Position, resolution.Support.Labels)
			}
		}
	}
	if !slices.Contains(kinds(resolution), localprofile.FindingNoStructuralBacking) {
		t.Fatal("a resolution did not state that no pack backs its constraints")
	}
}
