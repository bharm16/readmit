package reproducer_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/reproducer"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// capture imports fixture files as one case the way `readmit capture` does, so
// every test below reads evidence a verifying reader accepted.
func capture(t testing.TB, names ...string) (string, *bundle.Bundle) {
	t.Helper()
	inputs := make([]bundle.Input, 0, len(names))
	for _, name := range names {
		inputs = append(inputs, bundle.Input{Path: "/evidence/" + name, Data: fixture(t, name)})
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "incident.case")
	source, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	return path, source
}

func plan(t testing.TB, identity string, steps ...reproducer.Step) reproducer.Plan {
	t.Helper()
	authored, err := reproducer.NewPlan(identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		authored, err = reproducer.Append(authored, step)
		if err != nil {
			t.Fatal(err)
		}
	}
	return authored
}

func selectOne(id string) reproducer.Step {
	return reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: id}
}

func reasons(resolution reproducer.Resolution) map[string]reproducer.Retained {
	retained := make(map[string]reproducer.Retained, len(resolution.Occurrences))
	for _, entry := range resolution.Occurrences {
		retained[entry.Parent] = entry
	}
	return retained
}

// A plan is an ordered list of typed operators. Undo removes the last one and
// nothing else: the plan is replayed from what remains, so a step never leaves
// anything behind.
func TestPlanRecordsStepsCanonicallyAndUndoRemovesOnlyTheLast(t *testing.T) {
	identity := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	authored := plan(t, identity,
		selectOne("s0001-e000001"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: "MRN-TEST"},
	)
	if authored.Schema != reproducer.PlanSchema || authored.Case != identity || len(authored.Steps) != 2 {
		t.Fatal("a plan did not record the steps it was given")
	}
	// The recorded selector writes out the segment occurrence and the field
	// repetition, so the position a step edits is explicit rather than default.
	if authored.Steps[1].Selector != "PID[1]-3[1].1" {
		t.Fatalf("selector was not recorded canonically: %q", authored.Steps[1].Selector)
	}
	undone, err := reproducer.Undo(authored)
	if err != nil {
		t.Fatal(err)
	}
	if len(undone.Steps) != 1 || undone.Steps[0].Operator != reproducer.SelectOccurrence || len(authored.Steps) != 2 {
		t.Fatal("undo did not remove exactly the last step of a copy of the plan")
	}
	empty, err := reproducer.Undo(undone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reproducer.Undo(empty); err == nil {
		t.Fatal("undo invented a step to remove")
	}
	if _, err := reproducer.NewPlan("not-an-identity"); err == nil {
		t.Fatal("a plan was authored against something that is not a verified identity")
	}
}

// A step means exactly one thing. Every member another operator uses, every
// value that could restructure a message, and every operator this release does
// not perform is refused where the step is authored.
func TestPlanRefusesStepsThatCouldMeanSomethingElse(t *testing.T) {
	identity := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := plan(t, identity)
	for name, step := range map[string]reproducer.Step{
		"unknown operator":                  {Operator: "reorder-occurrence/v1", Occurrence: "s0001-e000001"},
		"selection with a selector":         {Operator: reproducer.SelectOccurrence, Occurrence: "s0001-e000001", Selector: "PID-3"},
		"selection with no occurrence":      {Operator: reproducer.SelectOccurrence},
		"malformed occurrence":              {Operator: reproducer.SelectOccurrence, Occurrence: "s1-e1"},
		"acknowledgements with a value":     {Operator: reproducer.IncludeAcknowledgements, Value: "x"},
		"identity with no selector":         {Operator: reproducer.IncludePriorIdentity},
		"identity naming a field twice":     {Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH[1]-2[1].1"}},
		"identity that is not a selector":   {Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "not a selector"}},
		"edit with no value":                {Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1"},
		"edit declaring a delimiter":        {Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: "A^B"},
		"edit declaring an escape":          {Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: `A\F\B`},
		"edit declaring an explicit null":   {Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: `""`},
		"edit carrying a control byte":      {Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: "A\rMSH"},
		"clearing a field with a value":     {Operator: reproducer.ClearField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: "x"},
		"edit of an unaddressable position": {Operator: reproducer.ClearField, Occurrence: "s0001-e000001", Selector: "PID-0"},
	} {
		if _, err := reproducer.Append(base, step); err == nil {
			t.Errorf("a plan accepted a step it cannot state: %s", name)
		}
	}
}

// A plan is an ordinary strict-JSON contract: unknown members and unknown
// versions are errors, and a step that was tampered with after it was written
// is refused by the same rule that refused it when it was authored.
func TestDecodePlanRefusesUnknownMembersVersionsAndTamperedSteps(t *testing.T) {
	identity := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	authored := plan(t, identity, selectOne("s0001-e000001"))
	document, err := json.Marshal(authored, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := reproducer.DecodePlan(document)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Case != identity || len(decoded.Steps) != 1 {
		t.Fatal("a plan did not survive being written and read back")
	}
	for name, change := range map[string][2]string{
		"unknown member":      {`"schema":`, `"retention":"forever","schema":`},
		"unknown step member": {`"operator":`, `"script":"rm -rf /","operator":`},
		"unknown version":     {reproducer.PlanSchema, "readmit-reproducer-plan/v2"},
		"unknown operator":    {reproducer.SelectOccurrence, "exec-shell/v1"},
		"unverified case":     {identity, "not-an-identity"},
	} {
		if _, err := reproducer.DecodePlan(bytes.Replace(document, []byte(change[0]), []byte(change[1]), 1)); err == nil {
			t.Errorf("a tampered plan was accepted: %s", name)
		}
	}
	if _, err := reproducer.DecodePlan([]byte(`{"schema":"readmit-reproducer-plan/v1","case":"` + identity + `"}`)); err == nil {
		t.Fatal("a plan that declares no steps was accepted")
	}
}

// Including acknowledgements retains what the case itself correlated and
// reports everything it could not settle. The ambiguous acknowledgement of this
// fixture names two candidate messages; selecting one of them here would be
// choosing an answer the evidence does not hold.
func TestIncludingAcknowledgementsRetainsCorrelationsAndReportsTheRest(t *testing.T) {
	_, source := capture(t, "case-evidence.mllp")
	acknowledgements := reproducer.Step{Operator: reproducer.IncludeAcknowledgements}
	resolution, err := reproducer.Resolve(source, plan(t, source.Identity, selectOne("s0001-e000004"), acknowledgements))
	if err != nil {
		t.Fatal(err)
	}
	retained := reasons(resolution)
	if retained["s0001-e000004"].Reason != reproducer.Selected {
		t.Fatal("a selected message lost its reason")
	}
	if retained["s0001-e000005"].Reason != reproducer.Acknowledgement || retained["s0001-e000005"].RequiredBy != "s0001-e000004" {
		t.Fatal("the acknowledgement of a selected message was not retained with what required it")
	}
	if len(resolution.Occurrences) != 2 {
		t.Fatalf("retained %d occurrences for one correlated pair", len(resolution.Occurrences))
	}
	for _, unsettled := range []struct{ occurrence, reason string }{
		{"s0001-e000003", reproducer.AmbiguousACK},
		{"s0001-e000006", reproducer.UnacknowledgedMsg},
		{"s0001-e000007", reproducer.UnmatchedACK},
	} {
		resolution, err := reproducer.Resolve(source, plan(t, source.Identity, selectOne(unsettled.occurrence), acknowledgements))
		if err != nil {
			t.Fatal(err)
		}
		if len(resolution.Occurrences) != 1 {
			t.Fatalf("%s retained a counterpart the case did not correlate", unsettled.occurrence)
		}
		if !slices.Contains(resolution.Unresolved, reproducer.Unresolved{Occurrence: unsettled.occurrence, Reason: unsettled.reason}) {
			t.Fatalf("%s was not reported as %s", unsettled.occurrence, unsettled.reason)
		}
	}
}

// A reschedule cannot be reproduced without the booking it refers to. That
// dependency is the identity an operator declares, compared by state and
// decoded value over the whole tuple, and only an earlier occurrence of the
// same source can be a setup dependency of a later one.
func TestPriorIdentityRetainsTheEarlierOccurrenceAboutTheSameSubject(t *testing.T) {
	_, source := capture(t, "synth-v1-regression.mllp")
	filler := reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}}
	resolution, err := reproducer.Resolve(source, plan(t, source.Identity, selectOne("s0001-e000002"), filler))
	if err != nil {
		t.Fatal(err)
	}
	retained := reasons(resolution)
	if retained["s0001-e000001"].Reason != reproducer.PriorIdentity || retained["s0001-e000001"].RequiredBy != "s0001-e000002" {
		t.Fatal("the booking a reschedule depends on was not retained as its setup dependency")
	}
	// The later occurrence is not a setup dependency of the earlier one.
	earlier, err := reproducer.Resolve(source, plan(t, source.Identity, selectOne("s0001-e000001"), filler))
	if err != nil {
		t.Fatal(err)
	}
	if len(earlier.Occurrences) != 1 {
		t.Fatal("a later occurrence was retained as a setup dependency of an earlier one")
	}
	// An occurrence that declares none of the identity fields, and one nothing
	// decoded, match nothing: unknown is not the same identity.
	_, other := capture(t, "case-evidence.mllp")
	unmatched, err := reproducer.Resolve(other, plan(t, other.Identity, selectOne("s0001-e000002"), selectOne("s0001-e000008"), filler))
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatched.Occurrences) != 2 {
		t.Fatal("an occurrence with no declared identity was matched to something")
	}
	if !slices.Contains(unmatched.Unresolved, reproducer.Unresolved{Occurrence: "s0001-e000002", Reason: reproducer.NoIdentity}) ||
		!slices.Contains(unmatched.Unresolved, reproducer.Unresolved{Occurrence: "s0001-e000008", Reason: reproducer.Undecodable}) {
		t.Fatalf("what carried no identity was not reported: %v", unmatched.Unresolved)
	}
}

// Dropping an occurrence removes it and everything the plan said about it. The
// plan is replayed from its steps, so undoing that drop resolves the edits
// again exactly as they were.
func TestDroppingAnOccurrenceRemovesItsEditsAndUndoRestoresThem(t *testing.T) {
	_, source := capture(t, "synth-v1-regression.mllp")
	edited := plan(t, source.Identity,
		selectOne("s0001-e000001"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3.1", Value: "MRN-TEST"},
		reproducer.Step{Operator: reproducer.DropOccurrence, Occurrence: "s0001-e000001"},
	)
	dropped, err := reproducer.Resolve(source, edited)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped.Occurrences) != 0 || len(dropped.Edits) != 0 {
		t.Fatal("dropping an occurrence left it or its edits in the reproducer")
	}
	restored, err := reproducer.Undo(edited)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := reproducer.Resolve(source, restored)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Occurrences) != 1 || len(resolution.Edits) != 1 {
		t.Fatal("undoing a drop did not resolve the edits of that occurrence again")
	}
	if _, err := reproducer.Resolve(source, plan(t, source.Identity, reproducer.Step{Operator: reproducer.DropOccurrence, Occurrence: "s0001-e000001"})); err == nil {
		t.Fatal("a plan dropped an occurrence it never retained")
	}
}

// Every refusal the editor promises, asserted where a person would meet it.
func TestEditsAreRefusedWhereThisReleaseCannotApplyThem(t *testing.T) {
	_, source := capture(t, "synth-v1-regression.mllp", "case-evidence.mllp")
	selected := selectOne("s0001-e000001")
	edit := func(operator, occurrence, selector, value string) reproducer.Step {
		return reproducer.Step{Operator: operator, Occurrence: occurrence, Selector: selector, Value: value}
	}
	for name, steps := range map[string][]reproducer.Step{
		"an occurrence the reproducer does not retain": {selected, edit(reproducer.SetField, "s0001-e000002", "PID-3.1", "MRN")},
		"an occurrence this case does not hold":        {selectOne("s0003-e000001")},
		"an occurrence nothing decoded":                {selectOne("s0002-e000008"), edit(reproducer.SetField, "s0002-e000008", "PID-3.1", "MRN")},
		"the same position twice":                      {selected, edit(reproducer.SetField, "s0001-e000001", "PID-3.1", "A"), edit(reproducer.SetField, "s0001-e000001", "PID[1]-3[1].1", "B")},
		"an omitted position":                          {selected, edit(reproducer.SetField, "s0001-e000001", "PID-99", "MRN")},
		"the field delimiter declaration":              {selected, edit(reproducer.SetField, "s0001-e000001", "MSH-1", "X")},
		"the encoding character declaration":           {selected, edit(reproducer.SetField, "s0001-e000001", "MSH-2", "X")},
		"overlapping positions":                        {selected, edit(reproducer.SetField, "s0001-e000001", "PID-3", "A"), edit(reproducer.SetField, "s0001-e000001", "PID-3.2", "B")},
		"selecting the same occurrence twice":          {selected, selected},
	} {
		if _, err := reproducer.Resolve(source, plan(t, source.Identity, steps...)); err == nil {
			t.Errorf("the editor applied something it cannot: %s", name)
		}
	}
	// A plan authored against other evidence is never applied to what happens
	// to be here instead.
	_, other := capture(t, "case-evidence.mllp")
	if _, err := reproducer.Resolve(other, plan(t, source.Identity, selected)); err == nil {
		t.Fatal("a plan was applied to evidence it was not authored against")
	}
	// An occurrence declaring other delimiters is retained exactly as it is and
	// refused for editing, rather than rewritten against an assumption about
	// what its separators mean.
	for _, name := range []string{"custom-delimiters.hl7", "reduced-delimiters.hl7"} {
		_, declared := capture(t, name)
		retained := plan(t, declared.Identity, selected)
		if _, err := reproducer.Resolve(declared, retained); err != nil {
			t.Fatalf("%s could not be retained unedited: %v", name, err)
		}
		edited := plan(t, declared.Identity, selected, edit(reproducer.ClearField, "s0001-e000001", "PID-3", ""))
		if _, err := reproducer.Resolve(declared, edited); err == nil {
			t.Fatalf("%s was edited under delimiters this release does not rewrite", name)
		}
	}
}

// The whole delivery through its public interface: select the reschedule,
// retain the booking it depends on, edit a supported field, and write a
// separate revision whose manifest says exactly what happened.
func TestCreateWritesASeparateRevisionAndNeverChangesTheOriginal(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	before := directoryFiles(t, casePath)
	authored := plan(t, source.Identity,
		selectOne("s0001-e000002"),
		reproducer.Step{Operator: reproducer.IncludePriorIdentity, Identity: []string{"SCH-2.1", "SCH-2.2"}},
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000002", Selector: "PID-3.1", Value: "MRN-REPRODUCER"},
		reproducer.Step{Operator: reproducer.ClearField, Occurrence: "s0001-e000002", Selector: "SCH-7"},
	)
	output := filepath.Join(t.TempDir(), "reproducer")
	manifest, err := reproducer.Create(source, casePath, authored, output)
	if err != nil {
		t.Fatal(err)
	}
	if !equalFiles(before, directoryFiles(t, casePath)) {
		t.Fatal("building a reproducer changed the evidence it was derived from")
	}
	if manifest.Parent.Identity != source.Identity || manifest.Parent.Schema != source.Manifest.Schema {
		t.Fatal("the manifest does not name the evidence this was derived from")
	}
	derived, err := bundle.Open(filepath.Join(output, reproducer.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	if derived.Manifest.Schema != bundle.DerivedSchema || derived.Manifest.Provenance.Derivation != reproducer.Derivation ||
		derived.Manifest.Provenance.Mode != bundle.Derived || derived.Manifest.Sources[0].Path != "" {
		t.Fatal("the reproducer did not declare itself as derived testing evidence")
	}
	if manifest.Derived.Identity != derived.Identity || len(manifest.Occurrences) != 2 {
		t.Fatal("the manifest does not describe the derived case beside it")
	}
	if manifest.Occurrences[0].Parent != "s0001-e000001" || manifest.Occurrences[0].Reason != reproducer.PriorIdentity ||
		manifest.Occurrences[1].Parent != "s0001-e000002" || manifest.Occurrences[1].Reason != reproducer.Selected {
		t.Fatalf("the reproducer did not retain the booking before the reschedule: %+v", manifest.Occurrences)
	}
	// Every edit names where it landed in the derived occurrence, and the bytes
	// there are the bytes the plan asked for.
	if len(manifest.Edits) != 2 {
		t.Fatalf("recorded %d edits for two steps", len(manifest.Edits))
	}
	// Edits are recorded in the order they occur in the occurrence, so the
	// cleared field is looked up by the position it names rather than by when it
	// was authored.
	placed := make(map[string]reproducer.Edit, len(manifest.Edits))
	for _, edit := range manifest.Edits {
		placed[edit.Selector] = edit
	}
	raw, err := derived.Raw(manifest.Edits[0].Derived)
	if err != nil {
		t.Fatal(err)
	}
	set := placed["PID[1]-3[1].1"]
	if set.Operator != reproducer.SetField || set.State != hl7.Present ||
		string(raw[set.Offset:set.Offset+set.Length]) != "MRN-REPRODUCER" {
		t.Fatalf("the manifest does not locate the value it wrote: %+v", set)
	}
	cleared := placed["SCH[1]-7[1]"]
	if cleared.Operator != reproducer.ClearField || cleared.Length != 0 || cleared.State != hl7.Present {
		t.Fatalf("clearing a field was not recorded as leaving an explicit empty position: %+v", cleared)
	}
	if cleared.Offset >= set.Offset {
		t.Fatal("edits were not recorded in the order they occur in the occurrence")
	}
	if bytes.Contains(raw, []byte("SYNTH-0000000000000000")) {
		t.Fatal("the derived occurrence retained the value the plan replaced")
	}
	// Nothing that was said about the original travels inside the derived
	// bundle: the lineage is in the manifest beside it.
	for name, content := range directoryFiles(t, filepath.Join(output, reproducer.CaseName)) {
		if bytes.Contains(content, []byte(source.Identity)) {
			t.Fatalf("derived evidence carries the identity of its parent in %s", name)
		}
	}
	reopened, err := reproducer.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Derived.Identity != manifest.Derived.Identity || len(reopened.Plan.Steps) != 4 {
		t.Fatal("a written reproducer did not read back as what was written")
	}
}

// A reproducer is refused wherever it would overwrite, reach into, or contradict
// retained evidence.
func TestCreateAndOpenRefuseWhatTheyCannotStandBehind(t *testing.T) {
	casePath, source := capture(t, "synth-v1-regression.mllp")
	authored := plan(t, source.Identity, selectOne("s0001-e000001"))
	workspace := t.TempDir()
	if _, err := reproducer.Create(source, casePath, plan(t, source.Identity), filepath.Join(workspace, "empty")); err == nil {
		t.Fatal("a reproducer retaining nothing was written")
	}
	if _, err := reproducer.Create(source, casePath, authored, filepath.Join(casePath, "inside")); err == nil {
		t.Fatal("a reproducer was written inside the evidence it reads")
	}
	output := filepath.Join(workspace, "reproducer")
	if _, err := reproducer.Create(source, casePath, authored, output); err != nil {
		t.Fatal(err)
	}
	if _, err := reproducer.Create(source, casePath, authored, output); err == nil {
		t.Fatal("an existing reproducer was replaced in place")
	}
	document := filepath.Join(output, reproducer.ManifestName)
	original, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string][2]string{
		"unknown member":                               {`"schema":`, `"approved":true,"schema":`},
		"unknown version":                              {reproducer.ManifestSchema, "readmit-reproducer/v2"},
		"a different derived identity":                 {`"derived":{"schema":"readmit-case/v3","identity":"`, `"derived":{"schema":"readmit-case/v3","identity":"0000000000000000000000000000000000000000000000000000000000000000`},
		"an occurrence the derived case does not hold": {`"derived":"s0001-e000001"`, `"derived":"s0001-e000009"`},
		"a plan authored against another case":         {`"plan":{"schema":"readmit-reproducer-plan/v1","case":"`, `"plan":{"schema":"readmit-reproducer-plan/v1","case":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff`},
	} {
		modified := bytes.Replace(original, []byte(change[0]), []byte(change[1]), 1)
		if bytes.Equal(modified, original) {
			t.Fatalf("the tampering %q changed nothing; the assertion would be vacuous", name)
		}
		if err := os.WriteFile(document, modified, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := reproducer.Open(output); err == nil {
			t.Errorf("a manifest that disagrees with its evidence was accepted: %s", name)
		}
	}
	if err := os.WriteFile(document, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(output, reproducer.CaseName, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if _, err := reproducer.Open(output); err == nil {
		t.Fatal("a reproducer whose derived case is incomplete was accepted")
	}
	if err := os.Remove(document); err != nil {
		t.Fatal(err)
	}
	if _, err := reproducer.Open(output); err == nil {
		t.Fatal("a directory with no manifest was read as a finished reproducer")
	}
}

func directoryFiles(t testing.TB, path string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			for nested, content := range directoryFiles(t, name) {
				files[filepath.Join(entry.Name(), nested)] = content
			}
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = data
	}
	return files
}

func equalFiles(before, after map[string][]byte) bool {
	if len(before) != len(after) {
		return false
	}
	for name, content := range before {
		if !bytes.Equal(content, after[name]) {
			return false
		}
	}
	return true
}

// An edit addresses one repetition, one component, or a whole field, and leaves
// every other position of the occurrence exactly as it was. Empty and explicit
// null are positions the message declares, so both can be edited, and both are
// recorded as the state they were.
func TestEditsAddressOneRepetitionAndLeaveTheRestOfTheOccurrenceAlone(t *testing.T) {
	const wire = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR~ALT-1^^^OTHER^MR||\"\"|\r"
	path := filepath.Join(t.TempDir(), "incident.case")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source, err := bundle.Write(path, []bundle.Input{{Path: "/evidence/inline.hl7", Data: []byte(wire)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	authored := plan(t, source.Identity,
		selectOne("s0001-e000001"),
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-3[2].1", Value: "ALT-REPLACED"},
		reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: "PID-4", Value: "FILLED"},
		reproducer.Step{Operator: reproducer.ClearField, Occurrence: "s0001-e000001", Selector: "PID-5"},
	)
	output := filepath.Join(t.TempDir(), "reproducer")
	manifest, err := reproducer.Create(source, path, authored, output)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := bundle.Open(filepath.Join(output, reproducer.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := derived.Raw("s0001-e000001")
	if err != nil {
		t.Fatal(err)
	}
	// The second repetition changed; the first, its assigning authority and the
	// rest of the message did not.
	if !bytes.Contains(raw, []byte("MRN-1^^^READMIT^MR~ALT-REPLACED^^^OTHER^MR")) {
		t.Fatalf("editing one repetition did not leave the others alone: %q", raw)
	}
	if !bytes.Contains(raw, []byte("~ALT-REPLACED^^^OTHER^MR|FILLED|")) || bytes.Contains(raw, []byte(`""`)) {
		t.Fatalf("the empty and null positions were not edited as declared: %q", raw)
	}
	states := map[string]hl7.State{}
	for _, edit := range manifest.Edits {
		states[edit.Selector] = edit.State
		if string(raw[edit.Offset:edit.Offset+edit.Length]) != wanted(edit.Selector) {
			t.Errorf("%s does not locate the bytes it wrote", edit.Selector)
		}
	}
	if states["PID[1]-3[2].1"] != hl7.Present || states["PID[1]-4[1]"] != hl7.Empty || states["PID[1]-5[1]"] != hl7.Null {
		t.Fatalf("the state each position was before its edit was not recorded: %v", states)
	}
}

// wanted is the bytes each selector of the test above should now address.
func wanted(selector string) string {
	switch selector {
	case "PID[1]-3[2].1":
		return "ALT-REPLACED"
	case "PID[1]-4[1]":
		return "FILLED"
	}
	return ""
}

// inline writes one case from bytes a test declares, so a message can be shaped
// for exactly the position under test.
func inline(t testing.TB, wire string) (string, *bundle.Bundle) {
	t.Helper()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "incident.case")
	source, err := bundle.Write(path, []bundle.Input{{Path: "/evidence/inline.hl7", Data: []byte(wire)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	return path, source
}

// Two selectors can name one position. A field with a single component is its
// own first component, and every position below an empty or explicit-null
// ancestor resolves to that ancestor's span, so a plan naming both is refused
// rather than splicing two values where the message declares one place.
func TestTwoEditsCannotAddressTheSameBytesUnderDifferentNames(t *testing.T) {
	// SCH-3 is declared and empty; PID-3 is present with one component.
	const wire = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rSCH|PLACER-1|FILLER-1||\"\"|\rPID|1||MRN-1|\r"
	_, source := inline(t, wire)
	edit := func(selector, value string) reproducer.Step {
		return reproducer.Step{Operator: reproducer.SetField, Occurrence: "s0001-e000001", Selector: selector, Value: value}
	}
	for name, selectors := range map[string][2]string{
		"two components of an empty field":       {"SCH-3.1", "SCH-3.2"},
		"a component and its empty field":        {"SCH-3", "SCH-3.1"},
		"two components of an explicit null":     {"SCH-4.1", "SCH-4.2"},
		"a single-component field and component": {"PID-3", "PID-3.1"},
	} {
		refused := plan(t, source.Identity, selectOne("s0001-e000001"), edit(selectors[0], "A"), edit(selectors[1], "B"))
		if _, err := reproducer.Resolve(source, refused); err == nil {
			t.Errorf("two edits spliced into one position: %s", name)
		}
	}
	// One edit of such a position is still exactly what the message can hold.
	accepted := plan(t, source.Identity, selectOne("s0001-e000001"), edit("SCH-3.1", "ONLY"))
	resolution, err := reproducer.Resolve(source, accepted)
	if err != nil || len(resolution.Edits) != 1 || resolution.Edits[0].State != hl7.Empty {
		t.Fatalf("one edit of a declared empty position was refused: %v %+v", err, resolution)
	}
}

// Unsupported syntax stays visible. An occurrence nothing decoded is retained
// byte for byte by a build, and the derived case records it as undecoded rather
// than dropping it or repairing it.
func TestABuildRetainsAnUndecodedOccurrenceByteForByte(t *testing.T) {
	casePath, source := capture(t, "case-evidence.mllp")
	raw, err := source.Raw("s0001-e000008")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "reproducer")
	manifest, err := reproducer.Create(source, casePath, plan(t, source.Identity, selectOne("s0001-e000008")), output)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := bundle.Open(filepath.Join(output, reproducer.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.Events) != 1 || derived.Events[0].Kind != bundle.Unparsed || derived.Events[0].ParseError == "" {
		t.Fatalf("the reproducer did not record what it could not decode: %+v", derived.Events)
	}
	retained, err := derived.Raw(manifest.Occurrences[0].Derived)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(retained, raw) {
		t.Fatalf("an undecoded occurrence was not retained byte for byte: %q against %q", retained, raw)
	}
}
