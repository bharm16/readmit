package testauthor_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// workspace captures one case the way `readmit capture` does and puts a target
// configuration beside it, so every test below answers a draft against
// evidence a verifying reader accepted and a target the shared reader reads.
func workspace(t testing.TB) (string, *bundle.Bundle) {
	t.Helper()
	root := t.TempDir()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	source, err := bundle.Write(filepath.Join(root, "incident"),
		[]bundle.Input{{Path: "/evidence/case-evidence.mllp", Data: fixture(t, "case-evidence.mllp")}},
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "test-target.json"), fixture(t, "test-target.json"))
	return root, source
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func draft(t testing.TB, source *bundle.Bundle, answers ...testauthor.Answer) testauthor.Draft {
	t.Helper()
	authored, err := testauthor.NewDraft("incident", source.Identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range answers {
		authored, err = authored.Answer(answer)
		if err != nil {
			t.Fatalf("stage %s was refused: %v", answer.Stage, err)
		}
	}
	return authored
}

func ledgerAnswers() []testauthor.Answer {
	count := 1
	return []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: "test-target.json"},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Stop the prior listener and start a fresh one with an empty ledger."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &count},
			{ID: "booking-ack", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
		}},
	}
}

func present(text string) *testrunner.FieldValue {
	return &testrunner.FieldValue{State: hl7.Present, Text: &text}
}

// The whole flow, through its public interface. A draft is answered one stage
// at a time, each answer narrows what is still missing, and the bytes it
// generates are a spec the reader that executes one accepts unchanged.
func TestADraftIsAnsweredOneStageAtATimeAndGeneratesASpecTheRunnerReads(t *testing.T) {
	root, source := workspace(t)
	authored := draft(t, source)

	if _, err := testauthor.Generate(authored); err == nil {
		t.Fatal("an unanswered draft generated a test spec")
	}
	// Every stage is asked in its own order, and answering one removes exactly
	// that stage from what is still missing.
	remaining := []string{
		testauthor.StageName, testauthor.StageMessages, testauthor.StageTarget, testauthor.StageBoundary,
		testauthor.StageObservation, testauthor.StageReset, testauthor.StageExpectations,
	}
	for i, answer := range ledgerAnswers() {
		resolution, err := testauthor.Resolve(root, source, authored)
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Stage != remaining[i] || !slices.Equal(resolution.Missing, remaining[i:]) {
			t.Fatalf("the flow asked %q with %v missing; expected %q with %v", resolution.Stage, resolution.Missing, remaining[i], remaining[i:])
		}
		if authored, err = authored.Answer(answer); err != nil {
			t.Fatalf("stage %s was refused: %v", answer.Stage, err)
		}
	}
	resolution, err := testauthor.Resolve(root, source, authored)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Stage != "" || len(resolution.Missing) != 0 {
		t.Fatalf("an answered draft still reported %q missing %v", resolution.Stage, resolution.Missing)
	}
	// Choosing the boundary fixes the initial state; it is never answered twice.
	if resolution.Setup != testrunner.EmptyLedger {
		t.Fatalf("the ledger boundary reported the initial state %q", resolution.Setup)
	}
	// The occurrences are reported in the order the case records them, which is
	// the order a run sends them in.
	if !slices.Equal(resolution.Messages, []string{"s0001-e000001", "s0001-e000002"}) {
		t.Fatalf("selected messages were not reported in evidence order: %v", resolution.Messages)
	}

	data, err := testauthor.Generate(authored)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.DecodeSpec(data)
	if err != nil {
		t.Fatalf("the generated document is not a spec this release reads: %v", err)
	}
	if spec.Schema != testrunner.SpecSchema || spec.Input.Case != "incident" || spec.Target != "test-target.json" {
		t.Fatalf("the generated spec does not name what the draft answered: %+v", spec)
	}
	if spec.Setup.InitialState != testrunner.EmptyLedger || spec.Observation.Boundary != testrunner.LedgerBoundary || spec.Observation.Path != "test-observation.json" {
		t.Fatalf("the generated spec did not carry the boundary the draft chose: %+v", spec)
	}
	if len(spec.Assertions) != 2 || spec.Assertions[0].Operator != testauthor.LedgerCount || *spec.Assertions[0].Expected.Count != 1 {
		t.Fatalf("the generated spec did not carry the expectations the draft made: %+v", spec.Assertions)
	}
	if spec.Assertions[1].Selector != "MSA-1" || spec.Assertions[1].Expected.Field.State != hl7.Present || *spec.Assertions[1].Expected.Field.Text != "AA" {
		t.Fatalf("the acknowledgement expectation did not survive generation: %+v", spec.Assertions[1])
	}
	// Nothing the draft did not answer reaches the document: readmit-test/v1
	// gains no member here and a draft never invents one.
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil || len(members) != 7 {
		t.Fatalf("the generated spec declares members readmit-test/v1 does not: %v", members)
	}
}

// The ACK boundary observes correlated acknowledgements. It reads no
// observation document, its initial state is the one an operator declares, and
// it makes no ledger claim at all.
func TestTheACKBoundaryReadsNoObservationDocumentAndMakesNoLedgerClaim(t *testing.T) {
	root, source := workspace(t)
	authored := draft(t, source,
		testauthor.Answer{Stage: testauthor.StageName, Name: "The receiver accepts both messages"},
		testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001"}},
		testauthor.Answer{Stage: testauthor.StageTarget, Target: "test-target.json"},
		testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary},
		testauthor.Answer{Stage: testauthor.StageReset, Reset: "The operator declares the starting state of the receiver."},
		testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "accepted", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
		}},
	)
	resolution, err := testauthor.Resolve(root, source, authored)
	if err != nil {
		t.Fatal(err)
	}
	// The observation stage does not apply, so it is never reported as missing.
	if slices.Contains(resolution.Missing, testauthor.StageObservation) || resolution.Setup != testrunner.OperatorDeclared {
		t.Fatalf("the ACK boundary asked for an observation document: %+v", resolution)
	}
	data, err := testauthor.Generate(authored)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.DecodeSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Observation.Path != "" || spec.Setup.InitialState != testrunner.OperatorDeclared {
		t.Fatalf("an ACK-only spec carried an observation source: %+v", spec)
	}
	// An observation source and a ledger count are both refused at this
	// boundary, rather than generated and refused by the runner later.
	count := 0
	for name, answer := range map[string]testauthor.Answer{
		"an observation source": {Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		"a ledger count": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "nothing-booked", Operator: testauthor.LedgerCount, Count: &count},
		}},
	} {
		if _, err := authored.Answer(answer); err == nil {
			t.Errorf("the ACK boundary accepted %s", name)
		}
	}
}

// An answer means exactly one thing. Every member another stage declares, and
// every stage this flow does not ask, is refused where the answer is given.
func TestAnAnswerCarriesOnlyTheMemberItsOwnStageDeclares(t *testing.T) {
	_, source := workspace(t)
	base := draft(t, source, ledgerAnswers()[:4]...)
	count := 1
	for name, answer := range map[string]testauthor.Answer{
		"no stage":                                   {Name: "unnamed"},
		"a stage this flow never asks":               {Stage: "deadline", Name: "later"},
		"a name with another member":                 {Stage: testauthor.StageName, Name: "one", Target: "test-target.json"},
		"a name that is empty":                       {Stage: testauthor.StageName},
		"messages with a target":                     {Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001"}, Target: "test-target.json"},
		"an occurrence that is not one":              {Stage: testauthor.StageMessages, Messages: []string{"e1"}},
		"the same occurrence twice":                  {Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000001"}},
		"a target that is a path":                    {Stage: testauthor.StageTarget, Target: "../test-target.json"},
		"a boundary this release does not decide at": {Stage: testauthor.StageBoundary, Boundary: "database-state"},
		"reset instructions carrying a control byte": {Stage: testauthor.StageReset, Reset: "stop\athe listener"},
		"an expectation with no operator":            {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{{ID: "a", Count: &count}}},
		"an expectation this flow does not author": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "exact-ledger", Operator: "ledger_equals"},
		}},
		"an identifier that is not one": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "One Appointment", Operator: testauthor.LedgerCount, Count: &count},
		}},
		"the same identifier twice": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one", Operator: testauthor.LedgerCount, Count: &count},
			{ID: "one", Operator: testauthor.LedgerCount, Count: &count},
		}},
		"a count carrying a selector": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one", Operator: testauthor.LedgerCount, Count: &count, Selector: "MSA-1"},
		}},
		"a count that is not declared": {Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one", Operator: testauthor.LedgerCount},
		}},
	} {
		if _, err := base.Answer(answer); err == nil {
			t.Errorf("the flow accepted %s", name)
		}
	}
}

// An acknowledgement expectation reads one position of the acknowledgement of
// one message this test sends. Every other shape of it is refused where it is
// authored, not discovered by the runner afterwards.
func TestAnAcknowledgementExpectationIsHeldToWhatTheTestSends(t *testing.T) {
	_, source := workspace(t)
	base := draft(t, source, ledgerAnswers()[:4]...)
	empty := ""
	for name, expectation := range map[string]testauthor.Expectation{
		"a message this test does not send":        {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000006", Selector: "MSA-1", Field: present("AA")},
		"no message at all":                        {ID: "a", Operator: testauthor.ACKFieldEquals, Selector: "MSA-1", Field: present("AA")},
		"a position outside MSA and ERR":           {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "PID-3.1", Field: present("AA")},
		"a selector this release does not address": {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-*", Field: present("AA")},
		"no expected value":                        {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1"},
		"a present value with no text":             {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: &testrunner.FieldValue{State: hl7.Present}},
		"a present value with empty text":          {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: &testrunner.FieldValue{State: hl7.Present, Text: &empty}},
		"an omitted value carrying text":           {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: &testrunner.FieldValue{State: hl7.Omitted, Text: &empty}},
		"a state that is not one of the four":      {ID: "a", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: &testrunner.FieldValue{State: "missing"}},
	} {
		if _, err := base.Answer(testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{expectation}}); err == nil {
			t.Errorf("the flow accepted %s", name)
		}
	}
	// The four states stay four answers: absent, empty and explicit null are
	// each expectations a person can make about an acknowledgement position.
	for _, state := range []hl7.State{hl7.Empty, hl7.Null, hl7.Omitted} {
		expectation := testauthor.Expectation{ID: "text-id", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-3", Field: &testrunner.FieldValue{State: state}}
		if _, err := base.Answer(testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{expectation}}); err != nil {
			t.Errorf("the flow refused an expectation that a value is %s: %v", state, err)
		}
	}
}

// An answer replaces one stage, so correcting a mistake is the same operation
// as answering. A replacement that would contradict another answer is refused
// and leaves the draft exactly as it was: a half-answered draft is a state this
// flow has, a contradictory one is not.
func TestReplacingAnAnswerNeverLeavesAContradictoryDraft(t *testing.T) {
	root, source := workspace(t)
	authored := draft(t, source, ledgerAnswers()...)

	// The acknowledgement expectation reads a message this test sends, so
	// withdrawing that message is refused rather than silently dropping it.
	withdrawn, err := authored.Answer(testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000002"}})
	if err == nil {
		t.Fatalf("withdrawing a message an expectation reads left %v", withdrawn.Expectations)
	}
	// And the boundary cannot be changed under an expectation it cannot make.
	if _, err := authored.Answer(testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary}); err == nil {
		t.Fatal("the ledger count survived a change to the ACK boundary")
	}
	if len(authored.Messages) != 2 || len(authored.Expectations) != 2 || authored.Boundary != testrunner.LedgerBoundary {
		t.Fatalf("a refused answer changed the draft: %+v", authored)
	}
	// A refused answer is not a dead end: the flow continues from exactly the
	// draft that was there, and the stage can be answered again.
	renamed, err := authored.Answer(testauthor.Answer{Stage: testauthor.StageName, Name: "Rescheduling keeps one appointment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testauthor.Generate(renamed); err != nil {
		t.Fatalf("the flow did not recover from a refused answer: %v", err)
	}
	// Withdrawing the expectation first is what makes the boundary answerable,
	// and choosing the ACK boundary then withdraws the observation source the
	// ledger boundary had asked for rather than generating a spec with both.
	cleared, err := renamed.Answer(testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
		{ID: "booking-ack", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	switched, err := cleared.Answer(testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary})
	if err != nil {
		t.Fatal(err)
	}
	// Removing the last expectation and withdrawing the last selection are the
	// same operation as answering, so neither is a state the flow cannot leave.
	emptied, err := switched.Answer(testauthor.Answer{Stage: testauthor.StageExpectations})
	if err != nil || len(emptied.Expectations) != 0 {
		t.Fatalf("the last expectation could not be removed: %v", err)
	}
	emptied, err = emptied.Answer(testauthor.Answer{Stage: testauthor.StageMessages})
	if err != nil || len(emptied.Messages) != 0 {
		t.Fatalf("the last selected message could not be withdrawn: %v", err)
	}
	if missing := testauthor.Missing(emptied); !slices.Contains(missing, testauthor.StageMessages) || !slices.Contains(missing, testauthor.StageExpectations) {
		t.Fatalf("a withdrawn answer was not reported as missing again: %v", missing)
	}
	if switched.Observation != "" {
		t.Fatalf("the ACK boundary kept an observation source: %q", switched.Observation)
	}
	if _, err := testauthor.Resolve(root, source, switched); err != nil {
		t.Fatal(err)
	}
}

// Resolve reads the evidence rather than trusting the answer that named it.
func TestResolveChecksEverySelectionAgainstTheVerifiedCase(t *testing.T) {
	root, source := workspace(t)
	base := draft(t, source, ledgerAnswers()[:1]...)
	for name, selection := range map[string][]string{
		"an acknowledgement":                    {"s0001-e000003"},
		"an occurrence nothing decoded":         {"s0001-e000008"},
		"an occurrence this case does not hold": {"s0009-e000001"},
	} {
		answered, err := base.Answer(testauthor.Answer{Stage: testauthor.StageMessages, Messages: selection})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := testauthor.Resolve(root, source, answered); err == nil {
			t.Errorf("a test was authored to send %s", name)
		}
	}
	// A draft authored against other evidence is refused rather than resolved
	// against whatever case happens to be open.
	other, err := testauthor.NewDraft("incident", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testauthor.Resolve(root, source, other); err == nil {
		t.Fatal("a draft resolved against evidence it was not authored against")
	}
}

// The targets a workspace offers are read by the same reader that resolves a
// spec's target, and the classification a configuration records is reported
// before anyone chooses it. A production-classified environment refuses every
// send, so it is refused while it is being chosen rather than after a test is
// saved that can never run.
func TestTargetsAreOfferedAsTheSharedReaderReadsThem(t *testing.T) {
	root, source := workspace(t)
	writeFile(t, filepath.Join(root, "broken-target.json"), []byte(`{"schema":"readmit-target/v1","test_endpoint":false}`))
	writeFile(t, filepath.Join(root, "notes.json"), []byte(`{"schema":"something-else/v1"}`))
	production := strings.Replace(string(fixture(t, "test-target.json")),
		`"schema": "readmit-target/v1"`,
		`"schema": "readmit-target/v3", "name": "prod-siu", "classification": "production"`, 1)
	writeFile(t, filepath.Join(root, "production-target.json"), []byte(production))

	targets, err := testauthor.Targets(root)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]testauthor.Target, len(targets))
	for _, target := range targets {
		found[target.Name] = target
	}
	if len(found) != 3 {
		t.Fatalf("the workspace offered %v", found)
	}
	if found["test-target.json"].Classification != "unclassified" || found["test-target.json"].Reason != "" {
		t.Fatalf("a readable target was not offered as read: %+v", found["test-target.json"])
	}
	// A file declaring the contract that the reader refuses is reported with
	// the reason rather than hidden from the person looking for it.
	if found["broken-target.json"].Reason == "" || found["broken-target.json"].Classification != "" {
		t.Fatalf("an unreadable target was offered as usable: %+v", found["broken-target.json"])
	}
	if _, declared := found["notes.json"]; declared {
		t.Fatal("a document that declares no target contract was offered as a target")
	}

	base := draft(t, source, ledgerAnswers()[:2]...)
	for name, target := range map[string]string{
		"a production-classified environment":   "production-target.json",
		"a configuration the reader refuses":    "broken-target.json",
		"an entry this workspace does not hold": "absent-target.json",
	} {
		answered, err := base.Answer(testauthor.Answer{Stage: testauthor.StageTarget, Target: target})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := testauthor.Resolve(root, source, answered); err == nil {
			t.Errorf("a test was authored against %s", name)
		}
	}
}

// Save writes one new entry of the workspace and reads back exactly what it
// wrote. A destination that already exists, one that is not a single entry, and
// one inside retained evidence are each refused, and a refused save leaves the
// draft answerable.
func TestSaveWritesOneNewEntryOfTheWorkspaceAndReadsItBack(t *testing.T) {
	root, source := workspace(t)
	authored := draft(t, source, ledgerAnswers()...)

	saved, err := testauthor.Save(root, source, authored, "reschedule-test.json")
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, "reschedule-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(written)
	if saved.Output != "reschedule-test.json" || saved.Identity != hex.EncodeToString(sum[:]) {
		t.Fatalf("the reported identity is not the identity of the bytes on disk: %+v", saved)
	}
	// The spec on disk is the one the command line prepares, resolving the case
	// and the target it names relative to itself.
	plan, err := testrunner.Prepare(filepath.Join(root, "reschedule-test.json"))
	if err != nil {
		t.Fatalf("the command line refused the spec this flow saved: %v", err)
	}
	if plan.Count() != 2 || plan.SpecIdentity() != saved.Identity || plan.SourceIdentity() != source.Identity {
		t.Fatalf("the prepared plan does not match what was authored: %d messages", plan.Count())
	}

	for name, output := range map[string]string{
		"an entry that already exists":  "reschedule-test.json",
		"a name that is not one entry":  "specs/reschedule-test.json",
		"a parent of the workspace":     "..",
		"a destination inside the case": "incident",
	} {
		if _, err := testauthor.Save(root, source, authored, output); err == nil {
			t.Errorf("a spec was written to %s", name)
		}
	}
	// A refused save wrote nothing, so saving again under a new name is all the
	// recovery there is.
	if _, err := testauthor.Save(root, source, authored, "reschedule-test-2.json"); err != nil {
		t.Fatalf("the flow did not recover from a refused save: %v", err)
	}
}

// Save resolves the draft against the evidence and the workspace again before
// it writes anything, so a draft assembled without going through the flow
// cannot put a spec on disk that the flow itself would have refused.
func TestSaveMakesEveryRefusalAgainBeforeItWritesAnything(t *testing.T) {
	root, source := workspace(t)
	production := strings.Replace(string(fixture(t, "test-target.json")),
		`"schema": "readmit-target/v1"`,
		`"schema": "readmit-target/v3", "name": "prod-siu", "classification": "production"`, 1)
	writeFile(t, filepath.Join(root, "production-target.json"), []byte(production))

	for name, assembled := range map[string]testauthor.Draft{
		"an acknowledgement as a sent message": {
			Schema: testauthor.Schema,
			Case:   testauthor.Evidence{Entry: "incident", Identity: source.Identity},
			Name:   "A regression", Messages: []string{"s0001-e000003"}, Target: "test-target.json",
			Boundary: testrunner.LedgerBoundary, Observation: "test-observation.json", Reset: "Restart the listener.",
			Expectations: []testauthor.Expectation{{ID: "none", Operator: testauthor.LedgerCount, Count: new(int)}},
		},
		"a production-classified target": {
			Schema: testauthor.Schema,
			Case:   testauthor.Evidence{Entry: "incident", Identity: source.Identity},
			Name:   "A regression", Messages: []string{"s0001-e000001"}, Target: "production-target.json",
			Boundary: testrunner.LedgerBoundary, Observation: "test-observation.json", Reset: "Restart the listener.",
			Expectations: []testauthor.Expectation{{ID: "none", Operator: testauthor.LedgerCount, Count: new(int)}},
		},
	} {
		if _, err := testauthor.Save(root, source, assembled, "assembled.json"); err == nil {
			t.Errorf("a spec naming %s was written", name)
		}
		if _, err := os.Stat(filepath.Join(root, "assembled.json")); !os.IsNotExist(err) {
			t.Fatalf("a refused save wrote a spec for %s", name)
		}
	}
}

// Generate names the stage a draft has not answered, so a person is told which
// question is still open rather than that a document is invalid.
func TestGenerateNamesTheStageADraftHasNotAnswered(t *testing.T) {
	_, source := workspace(t)
	answers := ledgerAnswers()
	// Every prefix of the flow is a draft that has not answered the stage the
	// flow asks next, and none of them generates a spec.
	for next := range answers {
		authored := draft(t, source, answers[:next]...)
		if _, err := testauthor.Generate(authored); err == nil {
			t.Errorf("a draft that has not answered %s generated a spec", answers[next].Stage)
		}
	}
	// A test at the ledger boundary that expects nothing of the ledger has
	// answered the expectations stage and still decides nothing, so the flow
	// asks for the count rather than generating a spec the runner refuses.
	partial := draft(t, source, slices.Concat(answers[:6], []testauthor.Answer{{
		Stage: testauthor.StageExpectations,
		Expectations: []testauthor.Expectation{
			{ID: "booking-ack", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
		},
	}})...)
	if !slices.Contains(testauthor.Missing(partial), testauthor.StageExpectations) {
		t.Fatal("a ledger test with no ledger expectation reported nothing missing")
	}
	if _, err := testauthor.Generate(partial); err == nil {
		t.Fatal("a ledger test with no ledger expectation generated a spec")
	}
}

// A draft is read strictly: unknown members, unknown versions and an omitted
// declaration are errors, and there is no migration and no repair.
func TestDecodeDraftReadsOnlyThisContract(t *testing.T) {
	_, source := workspace(t)
	authored := draft(t, source, ledgerAnswers()...)
	data, err := json.Marshal(authored)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := testauthor.DecodeDraft(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != authored.Name || len(decoded.Expectations) != len(authored.Expectations) {
		t.Fatalf("a draft did not survive being read back: %+v", decoded)
	}
	for name, document := range map[string]string{
		"an unknown member":           `{"schema":"readmit-test-draft/v1","case":{"entry":"incident","identity":"` + source.Identity + `"},"name":"n","messages":[],"target":"","boundary":"","observation":"","reset":"","expectations":[],"deadline":"5s"}`,
		"an unknown version":          strings.Replace(string(data), testauthor.Schema, "readmit-test-draft/v2", 1),
		"an omitted declaration":      `{"schema":"readmit-test-draft/v1","case":{"entry":"incident","identity":"` + source.Identity + `"}}`,
		"an identity that is not one": `{"schema":"readmit-test-draft/v1","case":{"entry":"incident","identity":"nope"},"name":"","messages":[],"target":"","boundary":"","observation":"","reset":"","expectations":[]}`,
		"a document that is not JSON": "{",
	} {
		if _, err := testauthor.DecodeDraft([]byte(document)); err == nil {
			t.Errorf("the reader accepted %s", name)
		}
	}
	if _, err := testauthor.DecodeDraft(make([]byte, testauthor.MaxDraftBytes+1)); err == nil {
		t.Error("the reader accepted a draft past its size limit")
	}
}
