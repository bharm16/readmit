package profileversion_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileversion"
)

// TestAssessListsTheSavedTestsAVersionReaches is the impact half of the
// delivery read as one assertion: the tests written against the version that
// changed are listed, the ones already on the new version and the ones on
// another version are told apart from them, another profile's tests are not in
// the report at all, and not one pin moved.
func TestAssessListsTheSavedTestsAVersionReaches(t *testing.T) {
	comparison, err := profileversion.Compare(profile(t), revised(t))
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	index := references(t)
	assessment, err := profileversion.Assess(comparison, index)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}

	if assessment.Profile != "fixture-local-siu" || assessment.From != "1" || assessment.To != "2" {
		t.Fatalf("assessed %+v", assessment)
	}
	if len(assessment.Changes) != len(comparison.Changes) {
		t.Fatalf("the assessment carries %d of %d changes", len(assessment.Changes), len(comparison.Changes))
	}
	expected := map[string]profileversion.Impact{
		"test-reschedule.json":     profileversion.ImpactAffected,
		"test-cancellation.json":   profileversion.ImpactCurrent,
		"test-retired-window.json": profileversion.ImpactUnrelated,
	}
	if len(assessment.Tests) != len(expected) {
		t.Fatalf("assessed %d saved tests: %+v", len(assessment.Tests), assessment.Tests)
	}
	for _, assessed := range assessment.Tests {
		if assessed.Impact != expected[assessed.Test] {
			t.Fatalf("%s was assessed %q, not %q", assessed.Test, assessed.Impact, expected[assessed.Test])
		}
		if assessed.Pinned != pinned(t, index, assessed.Test) {
			t.Fatalf("%s is reported as pinning %+v", assessed.Test, assessed.Pinned)
		}
		if assessed.Test == "test-other-interface.json" {
			t.Fatal("a saved test pinning another profile is in the report")
		}
	}

	// The affected list is the one an operator acts on, and it names the case
	// each affected test replays.
	affected := assessment.Affected()
	if len(affected) != 1 || affected[0].Test != "test-reschedule.json" || affected[0].Case != "test-case" {
		t.Fatalf("the affected saved tests are %+v", affected)
	}

	// Assessing moves nothing. A saved test reported as affected still pins the
	// version it was written against, so a historical contract only changes
	// when somebody upgrades it by name.
	if pinned(t, index, "test-reschedule.json").Identity() != (localprofile.Identity{ID: "fixture-local-siu", Version: "1"}) {
		t.Fatal("assessing moved a pin")
	}
	after, err := profileversion.Assess(comparison, index)
	if err != nil {
		t.Fatalf("assess again: %v", err)
	}
	if len(after.Tests) != len(assessment.Tests) {
		t.Fatal("assessing twice produced a different report")
	}
	for i, assessed := range after.Tests {
		if assessed != assessment.Tests[i] {
			t.Fatalf("assessing twice reported %+v then %+v", assessment.Tests[i], assessed)
		}
	}
}

// TestAssessReportsNoSavedTestForAProfileNothingPins is the honest empty case:
// a profile no saved test references produces an empty list rather than a
// report about somebody else's tests.
func TestAssessReportsNoSavedTestForAProfileNothingPins(t *testing.T) {
	unreferenced := profile(t)
	unreferenced.Identity.ID = "fixture-local-oru"
	later := revised(t)
	later.Identity.ID = "fixture-local-oru"
	comparison, err := profileversion.Compare(unreferenced, later)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	assessment, err := profileversion.Assess(comparison, references(t))
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if len(assessment.Tests) != 0 || len(assessment.Affected()) != 0 {
		t.Fatalf("assessed %+v", assessment.Tests)
	}
	if len(assessment.Changes) == 0 {
		t.Fatal("the assessment dropped the changes it was built from")
	}
}

// TestAssessDoesNotCallAnUnchangedContractAffected is the overclaim this report
// must not make. A version bumped without changing one rule is a comparison that
// found nothing, and a saved test written against the earlier version was not
// affected by it — saying otherwise would be a statement the comparison did not
// make.
func TestAssessDoesNotCallAnUnchangedContractAffected(t *testing.T) {
	renumbered := profile(t)
	renumbered.Identity.Version = "2"
	comparison, err := profileversion.Compare(profile(t), renumbered)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(comparison.Changes) != 0 {
		t.Fatalf("a version bumped alone compared as %+v", comparison.Changes)
	}
	assessment, err := profileversion.Assess(comparison, references(t))
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	if len(assessment.Affected()) != 0 {
		t.Fatalf("an unchanged contract affected %+v", assessment.Affected())
	}
	for _, assessed := range assessment.Tests {
		if assessed.Test == "test-reschedule.json" && assessed.Impact != profileversion.ImpactUnaffected {
			t.Fatalf("a saved test on the earlier version was assessed %q", assessed.Impact)
		}
	}
}

// TestAssessRefusesAnIndexNoReaderWouldAccept keeps the assessment from being
// the one entry point that answers over references a reader would have refused.
func TestAssessRefusesAnIndexNoReaderWouldAccept(t *testing.T) {
	comparison, err := profileversion.Compare(profile(t), revised(t))
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	doubled := references(t)
	doubled.Tests = append(doubled.Tests, doubled.Tests[2])
	assessment, err := profileversion.Assess(comparison, doubled)
	if err == nil {
		t.Fatalf("assessed an index recording one saved test twice: %+v", assessment.Tests)
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}
