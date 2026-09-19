package reproducer_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/reproducer"
)

// build writes one reproducer of a case and returns the folder holding it.
func build(t testing.TB, source *bundle.Bundle, casePath, name string, steps ...reproducer.Step) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), name)
	if _, err := reproducer.Create(source, casePath, plan(t, source.Identity, steps...), output); err != nil {
		t.Fatal(err)
	}
	return output
}

// changedRetention indexes a comparison by the occurrence each change names.
func changedRetention(comparison reproducer.Comparison) map[string]reproducer.RetentionChange {
	changes := make(map[string]reproducer.RetentionChange, len(comparison.Retention))
	for _, change := range comparison.Retention {
		changes[change.Parent] = change
	}
	return changes
}

// The whole delivery through its public interface: two revisions of one
// incident, the lineage that relates them, what the second one does
// differently, and the setup dependency it stopped retaining.
func TestComparingTwoRevisionsNamesTheirLineageAndTheDependencyOneDropped(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	// The first revision keeps the reschedule and the booking it refers to, and
	// replaces one identifier. The second keeps only the reschedule.
	first := build(t, source, casePath, "first",
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
		reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}},
	)
	// The second revision is the first one with its last step undone, which is
	// the only way this editor changes a plan at all.
	second := build(t, source, casePath, "second",
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
	)
	comparison, err := reproducer.CompareRevisions(reproducer.Revision{Path: first}, reproducer.Revision{Path: second})
	if err != nil {
		t.Fatal(err)
	}
	// Both were built from the same case and neither from the other.
	if comparison.Lineage != reproducer.SiblingOf {
		t.Fatalf("two revisions of one case were related as %q", comparison.Lineage)
	}
	if comparison.Left.Parent.Identity != source.Identity || comparison.Right.Parent.Identity != source.Identity {
		t.Fatal("a compared revision does not name the evidence it was derived from")
	}
	// The provenance and the derivation are read from the evidence itself, so a
	// transformation of customer evidence is reported as the transformation it
	// declares and never as generated material.
	if comparison.Left.Provenance != string(bundle.Derived) || comparison.Left.Derivation != reproducer.Derivation ||
		comparison.Right.Provenance != string(bundle.Derived) || comparison.Right.Derivation != reproducer.Derivation {
		t.Fatalf("a revision was not reported as the derived evidence it declares: %+v", comparison.Left)
	}
	// The setup dependency the second revision stopped retaining is named as a
	// dropped prerequisite rather than as an occurrence that merely went away.
	changes := changedRetention(comparison)
	dropped, found := changes["s0001-e000001"]
	if !found || dropped.Change != reproducer.DroppedPrerequisite || dropped.LeftReason != reproducer.PriorIdentity {
		t.Fatalf("the dropped setup dependency was not reported as one: %+v", comparison.Retention)
	}
	if _, moved := changes["s0001-e000002"]; moved {
		t.Fatal("the occurrence both revisions select was reported as a retention change")
	}
	// The authored change is the step the second plan no longer holds, at its
	// place in the plan that held it.
	if len(comparison.Steps) != 1 || comparison.Steps[0].Change != reproducer.Removed ||
		comparison.Steps[0].Step.Operator != reproducer.IncludePriorIdentity || comparison.Steps[0].Position != 3 {
		t.Fatalf("the plans were not compared as one ordered transformation: %+v", comparison.Steps)
	}
	// The edit both revisions make is the same edit, so it is not a change.
	if len(comparison.Edits) != 0 {
		t.Fatalf("an unchanged edit was reported as a difference: %+v", comparison.Edits)
	}
	// Neither revision names a retained run, so nothing is claimed about either.
	if comparison.Proof.State != reproducer.ProofNotAttempted || len(comparison.Proof.Assertions) != 0 ||
		comparison.Proof.Left != nil || comparison.Proof.Right != nil {
		t.Fatalf("proof was reported for revisions nobody has run: %+v", comparison.Proof)
	}
}

// Lineage is read from the identities the manifests name. A reproducer built
// from another reproducer's derived case is its child, the same pair compared
// the other way round is its parent, and a reproducer of unrelated evidence is
// neither rather than a guess.
func TestLineageIsReadFromTheIdentitiesAndIsNeverGuessed(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	parent := build(t, source, casePath, "parent", selectOne("s0001-e000001"), selectOne("s0001-e000002"))
	parentCase := filepath.Join(parent, reproducer.CaseName)
	derived, err := bundle.Open(parentCase)
	if err != nil {
		t.Fatal(err)
	}
	child := build(t, derived, parentCase, "child", selectOne("s0001-e000001"))
	for name, expected := range map[string]struct {
		left, right, lineage string
	}{
		"child":  {parent, child, reproducer.ChildOf},
		"parent": {child, parent, reproducer.ParentOf},
		"same":   {parent, parent, reproducer.SameEvidence},
	} {
		comparison, err := reproducer.CompareRevisions(reproducer.Revision{Path: expected.left}, reproducer.Revision{Path: expected.right})
		if err != nil {
			t.Fatal(err)
		}
		if comparison.Lineage != expected.lineage {
			t.Errorf("%s lineage was reported as %q", name, comparison.Lineage)
		}
		if name == "same" && (len(comparison.Steps) != 0 || len(comparison.Retention) != 0 || len(comparison.Edits) != 0) {
			t.Errorf("one revision compared with itself reported a difference: %+v", comparison)
		}
	}
	otherPath, other := capture(t, "case-evidence.mllp")
	unrelated := build(t, other, otherPath, "unrelated", selectOne("s0001-e000001"))
	comparison, err := reproducer.CompareRevisions(reproducer.Revision{Path: parent}, reproducer.Revision{Path: unrelated})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Lineage != reproducer.Unrelated {
		t.Fatalf("reproducers of different evidence were related as %q", comparison.Lineage)
	}
}

// A comparison of two revisions compares what they do, never the evidence they
// hold. That is an evidence rule rather than an omission: where one revision
// edits a position and the other leaves it alone, the bytes the other holds
// there are the original's own value, and this contract refuses to record the
// bytes an edit replaced.
func TestAComparisonNeverReportsTheBytesAnEditReplaced(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	original, err := bundle.Open(casePath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.Raw("s0001-e000002")
	if err != nil {
		t.Fatal(err)
	}
	// The identifier the first revision replaces and the second leaves exactly
	// as the incident recorded it.
	replaced := ""
	for _, field := range strings.Split(string(raw), "|") {
		if strings.HasPrefix(field, "SYNTH-") {
			replaced = strings.Split(field, "^")[0]
			break
		}
	}
	if replaced == "" {
		t.Fatal("the fixture no longer carries the identifier this test replaces")
	}
	edited := build(t, source, casePath, "edited",
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
	)
	untouched := build(t, source, casePath, "untouched", selectOne("s0001-e000002"))
	comparison, err := reproducer.CompareRevisions(reproducer.Revision{Path: edited}, reproducer.Revision{Path: untouched})
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Edits) != 1 || comparison.Edits[0].Change != reproducer.Removed ||
		comparison.Edits[0].Selector != "PID[1]-3[1].1" || comparison.Edits[0].LeftOperator != reproducer.SetField {
		t.Fatalf("an edit only one revision applies was not reported: %+v", comparison.Edits)
	}
	reported, err := json.Marshal(comparison, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reported), replaced) {
		t.Fatal("the comparison reported the bytes an edit replaced")
	}
}

// Two revisions that treat one position differently are compared at that
// position. The offsets are deliberately not compared: an edit lands somewhere
// else simply because an earlier edit of the same occurrence changed length.
func TestOnePositionEditedTwoWaysIsReportedAsOneChangedPosition(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	replaced := build(t, source, casePath, "replaced",
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "SCH-7", Value: "REPLACED"},
	)
	cleared := build(t, source, casePath, "cleared",
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
		reproducer.Step{Operator: reproducer.ClearField, Occurrence: "s0001-e000002", Selector: "SCH-7"},
	)
	comparison, err := reproducer.CompareRevisions(reproducer.Revision{Path: replaced}, reproducer.Revision{Path: cleared})
	if err != nil {
		t.Fatal(err)
	}
	changes := make(map[string]reproducer.EditChange, len(comparison.Edits))
	for _, change := range comparison.Edits {
		changes[change.Selector] = change
	}
	if len(comparison.Edits) != 2 {
		t.Fatalf("expected one changed position and one added one: %+v", comparison.Edits)
	}
	if moved := changes["SCH[1]-7[1]"]; moved.Change != reproducer.Changed ||
		moved.LeftOperator != reproducer.SetField || moved.RightOperator != reproducer.ClearField {
		t.Fatalf("one position edited two ways was not reported as changed: %+v", moved)
	}
	if added := changes["PID[1]-3[1].1"]; added.Change != reproducer.Added || added.LeftOperator != "" {
		t.Fatalf("a position only the second revision edits was not reported as added: %+v", added)
	}
}

// A comparison stands behind both sides or produces nothing. Anything that is
// not a finished reproducer of this transformation is refused, so evidence of
// another provenance can never be compared as a revision of this one.
func TestComparisonRefusesAnythingThatIsNotAFinishedReproducer(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	revision := build(t, source, casePath, "revision", selectOne("s0001-e000001"))
	// Synthetically generated evidence is exactly what a reproducer must never
	// be read as, and the case it was derived from is not a revision either.
	// Generated and derived bundles carry no source path, exactly as the writers
	// of each mode produce them.
	evidence := []bundle.Input{{Data: fixture(t, "synth-v1-regression.mllp")}}
	generated := filepath.Join(t.TempDir(), "generated")
	if _, err := bundle.Write(generated, evidence, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{
		BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1",
	}}); err != nil {
		t.Fatal(err)
	}
	// A redaction is derived testing evidence of another transformation, and a
	// reproducer revision is not something it can be read as either.
	redaction := filepath.Join(t.TempDir(), "redaction")
	if err := os.Mkdir(redaction, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Write(filepath.Join(redaction, reproducer.CaseName), evidence,
		bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"}); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "absent")
	noManifest := filepath.Join(t.TempDir(), "no-manifest")
	if err := os.MkdirAll(filepath.Join(noManifest, reproducer.CaseName), 0700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"generated evidence":           generated,
		"the case it was derived from": casePath,
		"a folder that does not exist": missing,
		"a build with no manifest":     noManifest,
		"the derived case on its own":  filepath.Join(revision, reproducer.CaseName),
	} {
		if _, err := reproducer.CompareRevisions(reproducer.Revision{Path: revision}, reproducer.Revision{Path: path}); err == nil {
			t.Errorf("a comparison accepted %s as a reproducer revision", name)
		}
		if _, err := reproducer.CompareRevisions(reproducer.Revision{Path: path}, reproducer.Revision{Path: revision}); err == nil {
			t.Errorf("a comparison accepted %s as the revision on the other side", name)
		}
	}
	// A run this release cannot verify is not proof of anything.
	if _, err := reproducer.CompareRevisions(
		reproducer.Revision{Path: revision, Result: casePath},
		reproducer.Revision{Path: revision, Result: casePath},
	); err == nil {
		t.Fatal("a directory that is not a retained test result was accepted as proof")
	}
}
