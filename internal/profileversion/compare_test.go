package profileversion_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profileversion"
)

// revised is the fixture profile evolved into version 2. It relaxes a
// constrained position, stops constraining another, constrains a new one,
// tightens a date rule and adds a site-defined segment, each where it stands
// rather than in canonical order: Compare holds the profile to its whole
// contract and orders it before anything is compared.
func revised(t *testing.T) localprofile.Profile {
	t.Helper()
	changed := relaxed(t)
	changed.Identity.Version = "2"
	zpd := constrained(t, &changed, "ZPD")
	zpd.Fields = slices.DeleteFunc(zpd.Fields, func(field localprofile.Field) bool { return field.Position == 4 })
	zpd.Fields = append(zpd.Fields, localprofile.Field{
		Position: 6,
		Name:     "Local escalation contact",
		Usage:    localprofile.UsageRequiredOrEmpty,
		Type:     "XCN",
	})
	changed.Dates = []localprofile.DateHandling{{
		ID:          "appointment-instant",
		Description: "Appointment times are sent to the minute and always carry an offset.",
		Precision:   localprofile.PrecisionSecond,
		TimeZone:    localprofile.TimeZoneRequired,
	}}
	changed.Segments = append(changed.Segments, localprofile.Segment{
		ID:          "ZIN",
		Description: "The site-defined insurance extension this interface added.",
		Cardinality: &localprofile.Cardinality{Min: 0, Max: "1"},
		Fields: []localprofile.Field{{
			Position: 1,
			Name:     "Local plan identifier",
			Usage:    localprofile.UsageRequired,
		}},
	})
	return changed
}

// revisedPin is the sealed pin of that version 2, so a test upgrades a saved
// test onto a version something actually sealed.
func revisedPin(t *testing.T) profileversion.Pin {
	t.Helper()
	sealed, err := profileversion.Seal(revised(t))
	if err != nil {
		t.Fatalf("seal the revised profile: %v", err)
	}
	return sealed.Pin()
}

// TestCompareNamesEveryPartThatDiffers is the comparison half of the delivery
// read as one assertion: every part that changed is named once, in one order,
// with the members that differ stated rather than left to be diffed by eye.
func TestCompareNamesEveryPartThatDiffers(t *testing.T) {
	comparison, err := profileversion.Compare(profile(t), revised(t))
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if comparison.Profile != "fixture-local-siu" || comparison.From != "1" || comparison.To != "2" {
		t.Fatalf("compared %+v", comparison)
	}
	expected := []profileversion.Change{
		{Part: profileversion.PartField, Kind: profileversion.KindChanged, Subject: "SCH-1", Detail: "the usage and the cardinality differ"},
		{Part: profileversion.PartSegment, Kind: profileversion.KindAdded, Subject: "ZIN", Detail: "a site-defined segment constraining 1 position"},
		{Part: profileversion.PartField, Kind: profileversion.KindRemoved, Subject: "ZPD-4", Detail: "usage O"},
		{Part: profileversion.PartField, Kind: profileversion.KindAdded, Subject: "ZPD-6", Detail: "usage RE"},
		{Part: profileversion.PartDate, Kind: profileversion.KindChanged, Subject: "appointment-instant", Detail: "the precision differs"},
	}
	if len(comparison.Changes) != len(expected) {
		t.Fatalf("compared %d changes: %+v", len(comparison.Changes), comparison.Changes)
	}
	for i, change := range comparison.Changes {
		if change != expected[i] {
			t.Fatalf("change %d is %+v, not %+v", i, change, expected[i])
		}
	}

	// The same pair always compares the same way, so a report of a change is
	// reproducible rather than dependent on map order.
	again, err := profileversion.Compare(profile(t), revised(t))
	if err != nil {
		t.Fatalf("compare again: %v", err)
	}
	for i, change := range again.Changes {
		if change != comparison.Changes[i] {
			t.Fatalf("comparing twice produced %+v then %+v", comparison.Changes[i], change)
		}
	}
}

// TestCompareNamesTheRemainingParts covers the parts the fixture pair does not
// exercise: the pinned base, a segment's own rules, a removed segment, a local
// code table and an assigning authority.
func TestCompareNamesTheRemainingParts(t *testing.T) {
	for _, change := range []struct {
		name     string
		revise   func(t *testing.T, changed *localprofile.Profile)
		expected profileversion.Change
	}{
		{"a segment's own cardinality", func(t *testing.T, changed *localprofile.Profile) {
			t.Helper()
			constrained(t, changed, "ZPD").Cardinality = &localprofile.Cardinality{Min: 1, Max: "2"}
		}, profileversion.Change{Part: profileversion.PartSegment, Kind: profileversion.KindChanged, Subject: "ZPD", Detail: "the cardinality differs"}},
		{"a removed segment", func(t *testing.T, changed *localprofile.Profile) {
			t.Helper()
			changed.Segments = slices.DeleteFunc(changed.Segments, func(segment localprofile.Segment) bool { return segment.ID == "SCH" })
		}, profileversion.Change{Part: profileversion.PartSegment, Kind: profileversion.KindRemoved, Subject: "SCH", Detail: "a standard segment constraining 3 positions"}},
		{"a local code table", func(t *testing.T, changed *localprofile.Profile) {
			t.Helper()
			changed.Terminology = []localprofile.TerminologySet{{
				ID:          "local-visit-reason",
				Description: "The reason codes this site sends, hand-authored for the fixture.",
				Binding:     localprofile.BindingSuggested,
				Codes:       []localprofile.Code{{Code: "ROUTINE", Display: "Routine appointment"}},
			}}
		}, profileversion.Change{Part: profileversion.PartTerminology, Kind: profileversion.KindChanged, Subject: "local-visit-reason", Detail: "the binding and the codes differ"}},
		{"an assigning authority", func(t *testing.T, changed *localprofile.Profile) {
			t.Helper()
			changed.Authorities = []localprofile.Authority{{
				ID:          "local-mrn-authority",
				Description: "The assigning authority this site issues its medical record numbers under.",
				Namespace:   "FIXTURECARE2",
			}}
		}, profileversion.Change{Part: profileversion.PartAuthority, Kind: profileversion.KindChanged, Subject: "local-mrn-authority", Detail: "the namespace, the universal id and the universal id type differ"}},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := profile(t)
			changed.Identity.Version = "2"
			change.revise(t, &changed)
			comparison, err := profileversion.Compare(profile(t), changed)
			if err != nil {
				t.Fatalf("compare: %v", err)
			}
			if !contains(comparison.Changes, change.expected) {
				t.Fatalf("the comparison does not report %+v: %+v", change.expected, comparison.Changes)
			}
		})
	}

	// A repin is not a rule of any segment, so it is reported against the base.
	repinned := profile(t)
	repinned.Identity.Version = "2"
	repinned.Base.Pack = profilepack.Identity{ID: "fixture-siu", Version: "2"}
	repinned.Base.Family = "ADT"
	comparison, err := profileversion.Compare(profile(t), repinned)
	if err != nil {
		t.Fatalf("compare the repinned profile: %v", err)
	}
	expected := profileversion.Change{
		Part: profileversion.PartBase, Kind: profileversion.KindChanged,
		Detail: "the pinned pack and the message family differ",
	}
	if len(comparison.Changes) != 1 || comparison.Changes[0] != expected {
		t.Fatalf("the repin compared as %+v", comparison.Changes)
	}
}

// TestCompareRefusesWhatIsNotAComparison is the immutability rule stated from
// the comparison side: two profiles are not two versions of one, a version is
// not compared with itself, and one version never stands for two documents.
func TestCompareRefusesWhatIsNotAComparison(t *testing.T) {
	other := profile(t)
	other.Identity.ID = "fixture-local-adt"
	if _, err := profileversion.Compare(profile(t), other); err == nil {
		t.Fatal("two different profiles compared as two versions of one")
	}

	_, err := profileversion.Compare(profile(t), profile(t))
	if err == nil {
		t.Fatal("a version compared with itself")
	}
	if !strings.Contains(err.Error(), "not with itself") {
		t.Fatalf("the refusal does not say what happened: %v", err)
	}

	// The same version carrying two different documents is the state a saved
	// test's pin could not survive, so it is refused by name rather than
	// reported as a list of differences.
	_, err = profileversion.Compare(profile(t), relaxed(t))
	if err == nil {
		t.Fatal("one version standing for two documents compared")
	}
	if !strings.Contains(err.Error(), "carries a new version") {
		t.Fatalf("the refusal does not say what to do: %v", err)
	}

	// A profile a reader would have refused is never compared.
	incomplete := profile(t)
	incomplete.Identity.Version = "2"
	incomplete.Segments = nil
	if _, err := profileversion.Compare(profile(t), incomplete); err == nil {
		t.Fatal("a profile constraining nothing compared")
	}
	if _, err := profileversion.Compare(incomplete, profile(t)); err == nil {
		t.Fatal("a profile constraining nothing compared")
	}
}

// constrained is the segment p constrains under id, to change where it stands.
func constrained(t *testing.T, p *localprofile.Profile, id string) *localprofile.Segment {
	t.Helper()
	for i := range p.Segments {
		if p.Segments[i].ID == id {
			return &p.Segments[i]
		}
	}
	t.Fatalf("the profile does not constrain %s", id)
	return nil
}

func contains(changes []profileversion.Change, wanted profileversion.Change) bool {
	for _, change := range changes {
		if change == wanted {
			return true
		}
	}
	return false
}
