package transform_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/transform"
)

// Every message below is synthetic. Identifiers are invented tokens and no
// namespace, authority or person here refers to anything real.
const (
	booking = "MSH|^~\\&|SCHEDULE|SITE-A|RECEIVER|LAB|20260101120000+0000||SIU^S12|MSG-001|P|2.5.1\r" +
		"SCH|APPT-001^READMIT|FILL-001^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000+0000^20260102103000+0000\r" +
		"PID|1||PATIENT-001^^^READMIT\r"
	reschedule = "MSH|^~\\&|SCHEDULE|SITE-A|RECEIVER|LAB|20260101120100+0000||SIU^S13|MSG-002|P|2.5.1\r" +
		"SCH|APPT-001^READMIT|FILL-001^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260103110000+0000^20260103113000+0000\r" +
		"PID|1||PATIENT-001^^^READMIT\r"
	unqualified = "MSH|^~\\&|SCHEDULE|SITE-C|RECEIVER|LAB|20260101120200+0000||SIU^S13|MSG-003|P|2.5.1\r" +
		"PID|1||PATIENT-001\r"
	accepted = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-001|P|2.5.1\rMSA|AA|MSG-001\r"
	unknown  = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-002|P|2.5.1\rMSA|AA|MSG-999\r"
	coarse   = "MSH|^~\\&|SCHEDULE|SITE-A|RECEIVER|LAB|202601011200||SIU^S12|MSG-004|P|2.5.1\r"

	controlInSource = `{"schema":"readmit-correlation-rules/v1","rules":[` +
		`{"id":"same-message","operator":"control-id","scope":"source"}]}`
	controlAcross = `{"schema":"readmit-correlation-rules/v1","rules":[` +
		`{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0001","s0002"]}]}`
	patientRules = `{"schema":"readmit-correlation-rules/v1","authorities":[` +
		`{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}],"rules":[` +
		`{"id":"patient","operator":"identifier","scope":"source","value":"PID-3.1",` +
		`"authority":["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}]}`
	controlAndPatientPosition = `{"schema":"readmit-correlation-rules/v1","authorities":[` +
		`{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}],"rules":[` +
		`{"id":"same-message","operator":"control-id","scope":"source"},` +
		`{"id":"overlapping","operator":"identifier","scope":"source","value":"MSH-10",` +
		`"authority":["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}]}`
	acknowledgeRules = `{"schema":"readmit-correlation-rules/v1","rules":[` +
		`{"id":"acks","operator":"acknowledges","scope":"source"}]}`
)

func framed(messages ...string) []byte {
	var wire []byte
	for _, message := range messages {
		wire = append(wire, 0x0b)
		wire = append(wire, message...)
		wire = append(wire, 0x1c, '\r')
	}
	return wire
}

// writeCase gives each payload its own case source, exactly as capture does
// with one positional file per source.
func writeCase(t *testing.T, payloads ...[]byte) string {
	t.Helper()
	imported := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	inputs := make([]bundle.Input, len(payloads))
	for i, payload := range payloads {
		inputs[i] = bundle.Input{Path: "SYNTHETIC-FIXTURE", Data: payload}
	}
	path := filepath.Join(t.TempDir(), "case")
	if _, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	return path
}

func declaredRules(t *testing.T, document string) correlate.Rules {
	t.Helper()
	rules, err := correlate.ParseRules([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

// authored binds a plan to the evidence and the declarations it is about to be
// applied to, which is what the two identities in a plan are for.
func authored(t *testing.T, path string, rules correlate.Rules, steps ...transform.Step) transform.Plan {
	t.Helper()
	return pinned(t, path, rules, profilepack.Identity{}, steps...)
}

// pinned writes one plan as a document and reads it back through the shipped
// reader, so every test below binds its plan to the evidence and the
// declarations exactly as a plan on disk does.
func pinned(t *testing.T, path string, rules correlate.Rules, pack profilepack.Identity, steps ...transform.Step) transform.Plan {
	t.Helper()
	report, err := correlate.Run(path, rules)
	if err != nil {
		t.Fatal(err)
	}
	if steps == nil {
		steps = []transform.Step{}
	}
	document, err := json.Marshal(transform.Plan{
		Schema: transform.PlanSchema, Case: report.CaseIdentity, Rules: report.RulesSHA256,
		Profile: pack, Steps: steps,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := transform.DecodePlan(document)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func previewed(t *testing.T, path string, rules correlate.Rules, steps ...transform.Step) transform.Preview {
	t.Helper()
	preview, err := transform.Run(path, authored(t, path, rules, steps...), rules, nil)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func refused(t *testing.T, path string, rules correlate.Rules, steps ...transform.Step) error {
	t.Helper()
	_, err := transform.Run(path, authored(t, path, rules, steps...), rules, nil)
	if err == nil {
		t.Fatal("a transformation this release cannot stand behind was not refused")
	}
	return err
}

func rename(rule string) transform.Step {
	return transform.Step{Operator: transform.RebaseIdentifiers, Rule: rule}
}

// changesOf indexes the preview by entry and selector, which is how a person
// reads it: one position of one entry, and the relation it was assigned.
func changesOf(preview transform.Preview) map[string]transform.Change {
	changes := make(map[string]transform.Change, len(preview.Changes))
	for _, change := range preview.Changes {
		changes[change.Entry+" "+change.Selector] = change
	}
	return changes
}

func codes(preview transform.Preview) map[string]transform.Unsupported {
	reported := make(map[string]transform.Unsupported, len(preview.Unsupported))
	for _, item := range preview.Unsupported {
		reported[item.Code] = item
	}
	return reported
}

// Renaming is assigned per relation: occurrences the declared rules related
// receive one surrogate, so the relation survives, and occurrences they related
// to nothing receive their own, so no relation is invented. Nothing outside the
// rule's declared scope is renamed at all.
func TestRenameKeepsRelatedOccurrencesTogetherAndUnrelatedApart(t *testing.T) {
	path := writeCase(t, framed(booking, reschedule), framed(booking), framed(reschedule))
	rules := declaredRules(t, controlAcross)
	preview := previewed(t, path, rules, rename("same-message"))

	changes := changesOf(preview)
	first, second, repeat := changes["t000001 MSH[1]-10[1]"], changes["t000002 MSH[1]-10[1]"], changes["t000003 MSH[1]-10[1]"]
	if first.Group == 0 || repeat.Group != first.Group {
		t.Fatalf("one message observed in two sources was not kept related: %+v %+v", first, repeat)
	}
	if second.Group == first.Group {
		t.Fatalf("two messages the rules related to nothing were given one value: %+v %+v", first, second)
	}
	if _, renamed := changes["t000004 MSH[1]-10[1]"]; renamed {
		t.Fatal("an occurrence outside the rule's declared scope was renamed")
	}
	if len(preview.Relations) != 1 || !preview.Relations[0].Preserved {
		t.Fatalf("the relation the rules produced was not reported as preserved: %+v", preview.Relations)
	}
	// A preview states locations, never content: no surrogate, no original
	// value and no identifier byte reaches the rendered report.
	rendered := string(transform.Terminal(preview))
	for _, value := range []string{"MSG-001", "MSG-002", "PATIENT-001", "READMIT000001"} {
		if strings.Contains(rendered, value) {
			t.Fatalf("a rendered preview carried the value %q", value)
		}
	}
}

// Renaming a control ID without rewriting the acknowledgement that names it
// would strand the acknowledgement. The case's own correlation is what repairs
// it, and the repaired reference receives the message's own surrogate.
func TestRenameRewritesTheAcknowledgementOfARenamedMessage(t *testing.T) {
	path := writeCase(t, framed(booking, accepted))
	preview := previewed(t, path, declaredRules(t, controlInSource), rename("same-message"))

	changes := changesOf(preview)
	message, reference := changes["t000001 MSH[1]-10[1]"], changes["t000002 MSA[1]-2[1]"]
	if message.Group == 0 || reference.Group != message.Group {
		t.Fatalf("the acknowledgement was not repaired onto its message: %+v %+v", message, reference)
	}
	if reference.Length != message.Length {
		t.Fatalf("the repaired reference does not receive the message's own value: %+v %+v", message, reference)
	}
	// The acknowledgement's own control ID is a separate identifier and is not
	// merged into the message's.
	if own := changes["t000002 MSH[1]-10[1]"]; own.Group == message.Group {
		t.Fatalf("an acknowledgement's own control ID was merged into its message's: %+v", own)
	}
}

// An acknowledgement this case could not tie to one message cannot be rewritten
// onto one of them. Renaming would leave it naming a control ID that is nowhere,
// so the rename is refused rather than performed and reported afterwards.
func TestRenameIsRefusedWhenAnAcknowledgementOfARenamedMessageIsAmbiguous(t *testing.T) {
	path := writeCase(t, framed(booking, booking, accepted))
	err := refused(t, path, declaredRules(t, controlInSource), rename("same-message"))
	if !strings.Contains(err.Error(), "several candidate messages") {
		t.Fatalf("the ambiguous acknowledgement was refused for the wrong reason: %v", err)
	}
}

// An acknowledgement naming no message of this case refers to nothing a rename
// can move, so it is left exactly as it is and reported rather than refused.
func TestAnAcknowledgementNamingNoMessageOfThisCaseIsReported(t *testing.T) {
	path := writeCase(t, framed(booking, unknown))
	preview := previewed(t, path, declaredRules(t, controlInSource), rename("same-message"))
	if _, reported := codes(preview)[transform.UnmatchedAcknowledgement]; !reported {
		t.Fatalf("an acknowledgement naming no message here was not reported: %+v", preview.Unsupported)
	}
	if _, rewritten := changesOf(preview)["t000002 MSA[1]-2[1]"]; rewritten {
		t.Fatal("an acknowledgement naming no message here was rewritten anyway")
	}
}

// The case says the same control ID was observed twice inside one source. That
// is a relation too, so both occurrences receive one surrogate and an
// intentional duplicate is still a duplicate afterwards.
func TestAnIntentionalDuplicateControlIDStaysADuplicate(t *testing.T) {
	path := writeCase(t, framed(booking, booking))
	preview := previewed(t, path, declaredRules(t, controlInSource), rename("same-message"))
	changes := changesOf(preview)
	first, second := changes["t000001 MSH[1]-10[1]"], changes["t000002 MSH[1]-10[1]"]
	if first.Group == 0 || first.Group != second.Group {
		t.Fatalf("an intentional duplicate was split by renaming: %+v %+v", first, second)
	}
}

// Equal identifier bytes under no configured assigning authority are equality
// readmit could not stand behind. Renaming them apart would break a relation
// nobody established and renaming them together would assert one, so the
// position is left exactly as it is and reported.
func TestUnqualifiedIdentifiersAreLeftAloneAndReported(t *testing.T) {
	path := writeCase(t, framed(unqualified, unqualified))
	preview := previewed(t, path, declaredRules(t, patientRules), rename("patient"))
	if _, reported := codes(preview)[transform.UnqualifiedIdentifier]; !reported {
		t.Fatalf("an unqualified identifier was not reported: %+v", preview.Unsupported)
	}
	if len(preview.Changes) != 0 {
		t.Fatalf("an unqualified identifier was renamed anyway: %+v", preview.Changes)
	}
}

// A configured authority is what makes two identifier strings one identifier.
// Both occurrences of it receive one surrogate, so a reproducer still describes
// one patient after the rename.
func TestAQualifiedIdentifierIsRenamedOnceAcrossTheRelation(t *testing.T) {
	path := writeCase(t, framed(booking, reschedule))
	preview := previewed(t, path, declaredRules(t, patientRules), rename("patient"))
	changes := changesOf(preview)
	first, second := changes["t000001 PID[1]-3[1].1"], changes["t000002 PID[1]-3[1].1"]
	if first.Group == 0 || first.Group != second.Group {
		t.Fatalf("one identifier under one authority was renamed into two: %+v %+v", first, second)
	}
}

// An acknowledges rule declares no value of its own. Renaming the messages is
// what repairs the references to them, so naming one is refused by name.
func TestRenamingAnAcknowledgesRuleIsRefusedByName(t *testing.T) {
	path := writeCase(t, framed(booking, accepted))
	err := refused(t, path, declaredRules(t, acknowledgeRules), rename("acks"))
	if !strings.Contains(err.Error(), "declares no value of its own") {
		t.Fatalf("renaming an acknowledges rule was refused for the wrong reason: %v", err)
	}
	if err := refused(t, path, declaredRules(t, controlInSource), rename("no-such-rule")); !strings.Contains(err.Error(), "do not hold") {
		t.Fatalf("renaming a rule nobody declared was refused for the wrong reason: %v", err)
	}
}

// Two rules addressing one position are the same rename written twice. The
// selectors differ and the position does not, so applying both would splice two
// values where the message declares one place to put them.
func TestTwoRenamesOfOnePositionAreRefused(t *testing.T) {
	path := writeCase(t, framed(booking))
	rules := declaredRules(t, controlAndPatientPosition)
	err := refused(t, path, rules, rename("same-message"), rename("overlapping"))
	if !strings.Contains(err.Error(), "same or overlapping bytes") {
		t.Fatalf("two renames of one position were refused for the wrong reason: %v", err)
	}
	if err := refused(t, path, rules, rename("same-message"), rename("same-message")); !strings.Contains(err.Error(), "renames each rule once") {
		t.Fatalf("renaming one rule twice was refused for the wrong reason: %v", err)
	}
}

// Every occurrence moves by the same explicit duration, so the interval between
// two messages and the duration of one appointment are exactly what they were.
func TestDateShiftMovesEverySupportedTimestampEqually(t *testing.T) {
	path := writeCase(t, framed(booking, reschedule))
	preview := previewed(t, path, declaredRules(t, controlInSource),
		transform.Step{Operator: transform.ShiftDates, Shift: "24h"})
	shifted := map[string]bool{}
	for _, change := range preview.Changes {
		if change.Operator != transform.ShiftDates || change.Group != 0 {
			t.Fatalf("a shift was recorded as a rename: %+v", change)
		}
		shifted[change.Entry+" "+change.Selector] = true
	}
	for _, position := range []string{
		"t000001 MSH[1]-7[1]", "t000001 SCH[1]-11[1].4", "t000001 SCH[1]-11[1].5",
		"t000002 MSH[1]-7[1]", "t000002 SCH[1]-11[1].4", "t000002 SCH[1]-11[1].5",
	} {
		if !shifted[position] {
			t.Fatalf("%s was not shifted: %+v", position, preview.Changes)
		}
	}
	if len(preview.Changes) != len(shifted) {
		t.Fatalf("a shift touched a position outside MSH-7 and SCH-11.4/5: %+v", preview.Changes)
	}
	// What a shift leaves alone is recorded in the preview, not only in prose:
	// a moved MSH-7 beside an unmoved date elsewhere is a fact a reader needs.
	if _, reported := codes(preview)[transform.UnshiftedPositions]; !reported {
		t.Fatalf("a shift did not record the positions it does not move: %+v", preview.Unsupported)
	}
}

// A timestamp this release cannot read back as whole seconds is refused rather
// than shifted into a form nobody can interpret.
func TestDateShiftRefusesATimestampItCannotMove(t *testing.T) {
	path := writeCase(t, framed(coarse))
	err := refused(t, path, declaredRules(t, controlInSource),
		transform.Step{Operator: transform.ShiftDates, Shift: "24h"})
	if !strings.Contains(err.Error(), "cannot move") {
		t.Fatalf("an unsupported timestamp was refused for the wrong reason: %v", err)
	}
}

// Reordering, duplicating and dropping edit the sequence, never the evidence.
// Every entry still names the occurrence and the case source it came from, so
// an edited sequence maps back onto what was captured.
func TestSequenceMutationsKeepTheirSourceMappings(t *testing.T) {
	path := writeCase(t, framed(booking, reschedule), framed(booking))
	rules := declaredRules(t, controlAcross)
	preview := previewed(t, path, rules,
		transform.Step{Operator: transform.DuplicateOccurrence, Entry: "t000001"},
		transform.Step{Operator: transform.ReorderOccurrence, Entry: "t000004", Position: 1},
		transform.Step{Operator: transform.DropOccurrence, Entry: "t000002"},
	)
	got := make([]string, 0, len(preview.Sequence))
	for position, entry := range preview.Sequence {
		if entry.Position != position+1 {
			t.Fatalf("the sequence is not numbered in order: %+v", preview.Sequence)
		}
		got = append(got, entry.ID+"="+entry.Parent+"/"+entry.Source)
	}
	expected := []string{
		"t000004=s0001-e000001/s0001",
		"t000001=s0001-e000001/s0001",
		"t000003=s0002-e000001/s0002",
	}
	if strings.Join(got, " ") != strings.Join(expected, " ") {
		t.Fatalf("an edited sequence lost its order or its source mapping: %v", got)
	}
	copies := 0
	for _, entry := range preview.Sequence {
		if entry.Copy {
			copies++
		}
	}
	if copies != 1 || preview.Summary.Copies != 1 {
		t.Fatalf("a repeated occurrence was not reported as a copy: %+v", preview.Sequence)
	}
	if _, err := transform.Run(path, authored(t, path, rules,
		transform.Step{Operator: transform.DropOccurrence, Entry: "t000009"}), rules, nil); err == nil {
		t.Fatal("a step naming an entry the sequence does not hold was accepted")
	}
}

// A copy is a second entry over one occurrence, so a rename gives both the same
// surrogate: repeating a message does not make it a different message.
func TestACopyReceivesTheSameRenamedValueAsItsOriginal(t *testing.T) {
	path := writeCase(t, framed(booking))
	preview := previewed(t, path, declaredRules(t, controlInSource),
		transform.Step{Operator: transform.DuplicateOccurrence, Entry: "t000001"},
		rename("same-message"))
	changes := changesOf(preview)
	first, copied := changes["t000001 MSH[1]-10[1]"], changes["t000002 MSH[1]-10[1]"]
	if first.Group == 0 || first.Group != copied.Group || first.Parent != copied.Parent {
		t.Fatalf("a copy was renamed away from the occurrence it repeats: %+v %+v", first, copied)
	}
}

// A relation whose occurrences are not all in the sequence is reported as
// broken, not as the part of it that remains.
func TestDroppingAnOccurrenceSeversTheRelationItBelongedTo(t *testing.T) {
	path := writeCase(t, framed(booking), framed(booking))
	rules := declaredRules(t, controlAcross)
	preview := previewed(t, path, rules, transform.Step{Operator: transform.DropOccurrence, Entry: "t000002"})
	if len(preview.Relations) != 1 {
		t.Fatalf("the declared relation was not reported: %+v", preview.Relations)
	}
	if relation := preview.Relations[0]; relation.Preserved || relation.Reason != transform.SeveredByDrop {
		t.Fatalf("a severed relation was not reported as severed: %+v", relation)
	}
	if preview.Summary.Preserved != 0 {
		t.Fatalf("a severed relation was counted as preserved: %+v", preview.Summary)
	}
	if err := refused(t, path, rules,
		transform.Step{Operator: transform.DropOccurrence, Entry: "t000001"},
		transform.Step{Operator: transform.DropOccurrence, Entry: "t000002"}); !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("dropping every occurrence was refused for the wrong reason: %v", err)
	}
}

// Only a supported outcome passes. A plan that pins no pack is answered by a
// pack that declares nothing, and unknown is not a verdict.
func TestProfileValidationReportsDeclaredSupportAndNeverPassesUnknown(t *testing.T) {
	path := writeCase(t, framed(booking))
	rules := declaredRules(t, controlInSource)
	unpinned := previewed(t, path, rules)
	if len(unpinned.Profile) != 1 || unpinned.Profile[0].Version != "2.5.1" || unpinned.Profile[0].Family != "SIU" {
		t.Fatalf("the sequence's declared combination was not reported: %+v", unpinned.Profile)
	}
	if unpinned.Profile[0].Parse != profilepack.OutcomeUnknown || unpinned.Profile[0].Parse.Passing() {
		t.Fatalf("an unpinned preview answered with something other than unknown: %+v", unpinned.Profile)
	}
	if _, reported := codes(unpinned)[transform.UnverifiedCombination]; !reported {
		t.Fatalf("a combination nothing verified was not recorded as unverified: %+v", unpinned.Unsupported)
	}

	pack := fixturePack(t)
	preview, err := transform.Run(path, pinned(t, path, rules, pack.Identity), rules, &pack)
	if err != nil {
		t.Fatal(err)
	}
	declared := preview.Profile[0]
	if declared.Parse != profilepack.OutcomeSupported || declared.Labels != profilepack.OutcomeSupported {
		t.Fatalf("the pinned pack's declared support was not reported: %+v", declared)
	}
	if declared.Structural.Passing() || declared.Workflow.Passing() {
		t.Fatalf("an unsupported level was reported as passing: %+v", declared)
	}
	if declared.Entries != 1 {
		t.Fatalf("the entries of a combination were not counted: %+v", declared)
	}
	if _, reported := codes(preview)[transform.UnverifiedCombination]; reported {
		t.Fatalf("a combination the pack declares supported was recorded as unverified: %+v", preview.Unsupported)
	}
}

// A pack is validated against only when a plan pinned it, and only when it is
// the pack that was pinned. A different version of the same pack is a different
// pack.
func TestAProfilePackIsUsedOnlyWhenThePlanPinnedThatExactPack(t *testing.T) {
	path := writeCase(t, framed(booking))
	rules := declaredRules(t, controlInSource)
	pack := fixturePack(t)
	if _, err := transform.Run(path, authored(t, path, rules), rules, &pack); err == nil {
		t.Fatal("a pack the plan did not pin was used anyway")
	}
	if _, err := transform.Run(path, pinned(t, path, rules, pack.Identity), rules, nil); err == nil {
		t.Fatal("a pinned pack that was not supplied was not refused")
	}
	other := pinned(t, path, rules, profilepack.Identity{ID: pack.Identity.ID, Version: "99"})
	if _, err := transform.Run(path, other, rules, &pack); err == nil {
		t.Fatal("a pack other than the pinned one was accepted")
	}
}

// A plan names the evidence and the declarations it was authored against, so
// replaying it somewhere else is refused rather than applied to whatever
// happens to be there.
func TestAPlanIsRefusedAgainstOtherEvidenceOrOtherRules(t *testing.T) {
	path := writeCase(t, framed(booking))
	other := writeCase(t, framed(reschedule))
	rules := declaredRules(t, controlInSource)
	plan := authored(t, path, rules)
	if _, err := transform.Run(other, plan, rules, nil); err == nil {
		t.Fatal("a plan authored against other evidence was applied")
	}
	if _, err := transform.Run(path, plan, declaredRules(t, patientRules), nil); err == nil {
		t.Fatal("a plan authored against other rules was applied")
	}
}

// A preview writes nothing. The case it read is byte-for-byte what it was, and
// no derived evidence is produced anywhere.
func TestAPreviewLeavesTheCaseExactlyAsItFoundIt(t *testing.T) {
	path := writeCase(t, framed(booking, accepted))
	before, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	previewed(t, path, declaredRules(t, controlInSource), rename("same-message"),
		transform.Step{Operator: transform.ShiftDates, Shift: "-2h"})
	after, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Identity != before.Identity {
		t.Fatal("a preview changed the evidence it read")
	}
	written, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(entries) {
		t.Fatalf("a preview wrote into the case: %d entries became %d", len(entries), len(written))
	}
}

// Unknown members and unknown versions are errors; there is no migration and no
// repair.
func TestDecodePlanRefusesUnknownMembersAndUnknownVersions(t *testing.T) {
	valid := `{"schema":"readmit-transform-plan/v1","case":"` + strings.Repeat("a", 64) +
		`","rules":"` + strings.Repeat("b", 64) + `","steps":[{"operator":"drop-occurrence/v1","entry":"t000001"}]}`
	plan, err := transform.DecodePlan([]byte(valid))
	if err != nil || len(plan.Steps) != 1 {
		t.Fatalf("a valid plan was not read: %v", err)
	}
	step := func(members string) string {
		return strings.Replace(valid, `{"operator":"drop-occurrence/v1","entry":"t000001"}`, members, 1)
	}
	for name, document := range map[string]string{
		"an unknown member":              strings.Replace(valid, `"steps"`, `"reduce":true,"steps"`, 1),
		"an unknown version":             strings.Replace(valid, "plan/v1", "plan/v2", 1),
		"no case identity":               `{"schema":"readmit-transform-plan/v1","rules":"` + strings.Repeat("b", 64) + `","steps":[]}`,
		"no rules identity":              `{"schema":"readmit-transform-plan/v1","case":"` + strings.Repeat("a", 64) + `","steps":[]}`,
		"a truncated identity":           strings.Replace(valid, strings.Repeat("a", 64), "abc", 1),
		"an unnamed profile pin":         strings.Replace(valid, `"steps"`, `"profile":{"id":"","version":"1"},"steps"`, 1),
		"an operator nobody implemented": step(`{"operator":"reduce/v1"}`),
		"a drop with a shift":            step(`{"operator":"drop-occurrence/v1","entry":"t000001","shift":"24h"}`),
		"a duplicate with a position":    step(`{"operator":"duplicate-occurrence/v1","entry":"t000001","position":2}`),
		"a reorder with no position":     step(`{"operator":"reorder-occurrence/v1","entry":"t000001"}`),
		"a reorder of no entry":          step(`{"operator":"reorder-occurrence/v1","position":1}`),
		"a rename naming an entry":       step(`{"operator":"rebase-identifiers/v1","rule":"same","entry":"t000001"}`),
		"a rename naming no rule":        step(`{"operator":"rebase-identifiers/v1"}`),
		"a shift naming a rule":          step(`{"operator":"shift-dates/v1","shift":"24h","rule":"same"}`),
		"a shift of no whole seconds":    step(`{"operator":"shift-dates/v1","shift":"90ms"}`),
		"a shift of nothing":             step(`{"operator":"shift-dates/v1","shift":"0s"}`),
		"a shift beyond ten years":       step(`{"operator":"shift-dates/v1","shift":"90000h"}`),
		"an entry outside the grammar":   step(`{"operator":"drop-occurrence/v1","entry":"s0001-e000001"}`),
	} {
		if _, err := transform.DecodePlan([]byte(document)); err == nil {
			t.Fatalf("%s was read as a plan", name)
		}
	}
}

// The JSON preview is the same model the terminal renders, declares its own
// contract, and states its boundary rather than leaving a reader to assume one.
func TestTheEncodedPreviewDeclaresItsContractAndItsBoundary(t *testing.T) {
	path := writeCase(t, framed(booking, accepted))
	preview := previewed(t, path, declaredRules(t, controlInSource), rename("same-message"))
	data, err := transform.JSON(preview)
	if err != nil {
		t.Fatal(err)
	}
	document := string(data)
	if !strings.Contains(document, `"schema":"readmit-transform-preview/v1"`) {
		t.Fatalf("the preview did not declare its contract: %s", document)
	}
	for _, promise := range []string{"writes nothing", "never pass"} {
		if !strings.Contains(document, promise) {
			t.Fatalf("the preview did not state its boundary: %s", document)
		}
	}
	for _, value := range []string{"MSG-001", "ACK-001", "PATIENT-001", "READMIT0"} {
		if strings.Contains(document, value) {
			t.Fatalf("an encoded preview carried the value %q", value)
		}
	}
}

func fixturePack(t *testing.T) profilepack.Pack {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := profilepack.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}
