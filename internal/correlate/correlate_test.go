package correlate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/observation"
)

// Every message below is synthetic. Identifiers are invented tokens and no
// namespace, authority or person here refers to anything real.
const (
	bookingReadmit  = "MSH|^~\\&|SCHEDULE|SITE-A|RECEIVER|LAB|20260101120000+0000||SIU^S12|MSG-001|P|2.5.1\rPID|1||PATIENT-001^^^READMIT\r"
	rescheduleOther = "MSH|^~\\&|SCHEDULE|SITE-B|RECEIVER|LAB|20260101120100+0000||SIU^S13|MSG-002|P|2.5.1\rPID|1||PATIENT-001^^^OTHER\r"
	rescheduleOID   = "MSH|^~\\&|SCHEDULE|SITE-B|RECEIVER|LAB|20260101120100+0000||SIU^S13|MSG-002|P|2.5.1\rPID|1||PATIENT-001^^^READMIT-OLD&1.2.3&ISO\r"
	noAuthority     = "MSH|^~\\&|SCHEDULE|SITE-C|RECEIVER|LAB|20260101120200+0000||SIU^S13|MSG-003|P|2.5.1\rPID|1||PATIENT-001\r"
	nullAuthority   = "MSH|^~\\&|SCHEDULE|SITE-C|RECEIVER|LAB|20260101120200+0000||SIU^S13|MSG-004|P|2.5.1\rPID|1||PATIENT-001^^^\"\"\r"
	acceptMSG001    = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-001|P|2.5.1\rMSA|AA|MSG-001\r"
	acceptNothing   = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-002|P|2.5.1\rMSA|AA|\r"
	acceptTwice     = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-003|P|2.5.1\rMSA|AA|MSG-001\rMSA|AA|MSG-002\r"
	acceptUnknown   = "MSH|^~\\&|RECEIVER|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-004|P|2.5.1\rMSA|AA|MSG-999\r"
	patientSelector = `{"id":"patient","operator":"identifier","scope":"declared","sources":["s0001","s0002"],"value":"PID-3.1","authority":["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}`
)

func mllp(messages ...string) []byte {
	var framed []byte
	for _, message := range messages {
		framed = append(framed, 0x0b)
		framed = append(framed, message...)
		framed = append(framed, 0x1c, '\r')
	}
	return framed
}

// writeCase gives each payload its own source, exactly as capture does with
// one positional file per source.
func writeCase(t *testing.T, payloads ...[]byte) string {
	t.Helper()
	imported := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	return writeProvenance(t, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}, payloads...)
}

// writeRecorded gives the case a declared session, which only a recorded or
// collected receiver session has.
func writeRecorded(t *testing.T, payloads ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	snapshot := observation.Snapshot{
		Schema: observation.Schema, Profile: observation.Profile,
		SessionID: strings.Repeat("a", 32), Mode: observation.Fixed, Consistent: true,
	}
	if _, err := bundle.WriteRecorded(path, recordedInputs(payloads), time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), snapshot); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeProvenance(t *testing.T, provenance bundle.Provenance, payloads ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	if _, err := bundle.Write(path, inputs(payloads), provenance); err != nil {
		t.Fatal(err)
	}
	return path
}

func inputs(payloads [][]byte) []bundle.Input {
	declared := make([]bundle.Input, len(payloads))
	for i, payload := range payloads {
		declared[i] = bundle.Input{Path: "SYNTHETIC-FIXTURE", Data: payload}
	}
	return declared
}

// Recorded evidence has no original file path, so its sources declare none.
func recordedInputs(payloads [][]byte) []bundle.Input {
	declared := make([]bundle.Input, len(payloads))
	for i, payload := range payloads {
		declared[i] = bundle.Input{Data: payload}
	}
	return declared
}

func rules(t *testing.T, authorities string, declarations ...string) correlate.Rules {
	t.Helper()
	document := `{"schema":"readmit-correlation-rules/v1",`
	if authorities != "" {
		document += `"authorities":[` + authorities + `],`
	}
	document += `"rules":[` + strings.Join(declarations, ",") + `]}`
	parsed, err := correlate.ParseRules([]byte(document))
	if err != nil {
		t.Fatalf("rules refused: %v", err)
	}
	return parsed
}

func report(t *testing.T, path string, declared correlate.Rules) correlate.Report {
	t.Helper()
	result, err := correlate.Run(path, declared)
	if err != nil {
		t.Fatalf("correlate: %v", err)
	}
	if result.Schema != correlate.ReportSchema || result.Scope == "" {
		t.Fatalf("report does not declare its contract and boundary: %+v", result)
	}
	return result
}

func reasons(result correlate.Report) []string {
	var found []string
	for _, collision := range result.Collisions {
		found = append(found, collision.Reason)
	}
	return found
}

func codes(result correlate.Report) []string {
	var found []string
	for _, item := range result.Unsupported {
		found = append(found, item.Code)
	}
	return found
}

func TestAcknowledgementResolvesInSourceAndBecomesAmbiguousWhenTheScopeWidens(t *testing.T) {
	one := mllp(bookingReadmit, acceptMSG001)
	path := writeCase(t, one, one)

	inSource := report(t, path, rules(t, "", `{"id":"ack","operator":"acknowledges","scope":"source"}`))
	if len(inSource.Links) != 2 || len(inSource.Collisions) != 0 {
		t.Fatalf("same-source acknowledgements did not resolve: %+v", inSource)
	}
	for _, link := range inSource.Links {
		if link.Linkage != correlate.Observed || len(link.Occurrences) != 2 {
			t.Fatalf("an acknowledgement's own declaration was not recorded as observed: %+v", link)
		}
		if link.Occurrences[0].SourceID != link.Occurrences[1].SourceID {
			t.Fatalf("a source-scoped rule crossed a source boundary: %+v", link)
		}
	}

	// The same evidence, with the operator declaring both sources one scope:
	// each acknowledgement now has two candidates and none is chosen.
	widened := report(t, path, rules(t, "", `{"id":"ack","operator":"acknowledges","scope":"declared","sources":["s0001","s0002"]}`))
	if len(widened.Links) != 0 || len(widened.Collisions) != 2 {
		t.Fatalf("a widened scope selected a candidate: %+v", widened)
	}
	for _, collision := range widened.Collisions {
		if collision.Reason != correlate.AmbiguousAcknowledgement || collision.Declaring == nil || len(collision.Occurrences) != 2 {
			t.Fatalf("ambiguity did not name its declaration and candidates: %+v", collision)
		}
	}
	if widened.Rules[0].Linked != 0 || widened.Rules[0].Unlinked != widened.Rules[0].Considered {
		t.Fatalf("occurrences held back by a collision were counted as linked: %+v", widened.Rules[0])
	}
}

func TestAcknowledgementsWithoutOneUsableReferenceAreNeverResolved(t *testing.T) {
	path := writeCase(t, mllp(bookingReadmit, rescheduleOther, acceptNothing, acceptTwice, acceptUnknown))
	result := report(t, path, rules(t, "", `{"id":"ack","operator":"acknowledges","scope":"source"}`))
	if len(result.Links) != 0 || len(result.Collisions) != 0 {
		t.Fatalf("an unusable reference produced a correlation: %+v", result)
	}
	for _, want := range []string{"unusable_declared_reference", "multiple_declared_references"} {
		if !containsString(codes(result), want) {
			t.Fatalf("missing %s in %+v", want, result.Unsupported)
		}
	}
	// An acknowledgement of a control ID nothing carries is not an error and
	// not a link: it is one more occurrence this case does not explain.
	if result.Rules[0].Linked != 0 || result.Rules[0].Considered != 3 {
		t.Fatalf("unmatched acknowledgement accounting: %+v", result.Rules[0])
	}
}

func TestEqualControlIDsLinkAcrossDeclaredSourcesAndCollideInsideOne(t *testing.T) {
	across := writeCase(t, mllp(bookingReadmit), mllp(bookingReadmit))
	declared := rules(t, "", `{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0001","s0002"]}`)
	result := report(t, across, declared)
	if len(result.Links) != 1 || result.Links[0].Linkage != correlate.Inferred || len(result.Links[0].Occurrences) != 2 {
		t.Fatalf("two observations of one control ID were not linked as inferred: %+v", result)
	}
	if len(result.Collisions) != 0 {
		t.Fatalf("distinct sources collided: %+v", result.Collisions)
	}

	// Nothing crosses a scope boundary: the same evidence under source scope
	// leaves both occurrences alone.
	perSource := report(t, across, rules(t, "", `{"id":"same-message","operator":"control-id","scope":"source"}`))
	if len(perSource.Links) != 0 || len(perSource.Collisions) != 0 {
		t.Fatalf("a source-scoped rule compared across sources: %+v", perSource)
	}

	duplicated := writeCase(t, mllp(bookingReadmit, bookingReadmit), mllp(bookingReadmit))
	collided := report(t, duplicated, declared)
	if len(collided.Links) != 0 || len(collided.Collisions) != 1 {
		t.Fatalf("a duplicated control ID was merged: %+v", collided)
	}
	if collided.Collisions[0].Reason != correlate.DuplicateControlID || len(collided.Collisions[0].Occurrences) != 3 {
		t.Fatalf("collision does not list every candidate: %+v", collided.Collisions[0])
	}
	// The rule read every one of those keys and reached nothing with them, so
	// all three occurrences are counted as unlinked rather than as linked.
	if rule := collided.Rules[0]; rule.Considered != 3 || rule.Linked != 0 || rule.Unlinked != 3 {
		t.Fatalf("collision accounting: %+v", rule)
	}
}

func TestIdentifiersCorrelateOnlyUnderAConfiguredAuthority(t *testing.T) {
	readmit := `{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}`
	other := `{"key":"OTHER-MR","namespace":"OTHER","universal_id":"","universal_id_type":""}`
	alias := `{"key":"READMIT-MR","namespace":"OTHER","universal_id":"","universal_id_type":""}`
	oldOID := `{"key":"READMIT-MR","namespace":"READMIT-OLD","universal_id":"1.2.3","universal_id_type":"ISO"}`

	sameAuthority := writeCase(t, mllp(bookingReadmit), mllp(bookingReadmit))
	linked := report(t, sameAuthority, rules(t, readmit, patientSelector))
	if len(linked.Links) != 1 || linked.Links[0].Authority != "READMIT-MR" || len(linked.Links[0].Occurrences) != 2 {
		t.Fatalf("one configured authority did not correlate: %+v", linked)
	}
	if linked.Links[0].Linkage != correlate.Inferred {
		t.Fatal("an identifier correlation was reported as directly observed linkage")
	}

	// Equal identifier strings under two configured authorities stay apart,
	// and the report says the equality was seen and refused.
	differentPath := writeCase(t, mllp(bookingReadmit), mllp(rescheduleOther))
	distinct := report(t, differentPath, rules(t, readmit+","+other, patientSelector))
	if len(distinct.Links) != 0 || !containsString(reasons(distinct), correlate.DistinctAuthorities) {
		t.Fatalf("different namespaces were conflated: %+v", distinct)
	}

	// The same two authorities mapped to one key is an explicit customer
	// assertion of equivalence, not an inference from the identifier.
	aliased := report(t, differentPath, rules(t, readmit+","+alias, patientSelector))
	if len(aliased.Links) != 1 || len(aliased.Collisions) != 0 {
		t.Fatalf("explicitly aliased authorities did not correlate: %+v", aliased)
	}

	// An authority nobody configured cannot qualify an identifier, so equal
	// strings remain a collision rather than becoming one patient.
	unconfigured := report(t, differentPath, rules(t, readmit, patientSelector))
	if len(unconfigured.Links) != 0 || !containsString(reasons(unconfigured), correlate.UnqualifiedIdentifier) ||
		!containsString(codes(unconfigured), "unconfigured_assigning_authority") {
		t.Fatalf("an unconfigured authority passed: %+v", unconfigured)
	}

	// A complete universal-ID authority is matched as the whole tuple.
	universal := writeCase(t, mllp(bookingReadmit), mllp(rescheduleOID))
	byOID := report(t, universal, rules(t, readmit+","+oldOID, patientSelector))
	if len(byOID.Links) != 1 || len(byOID.Collisions) != 0 {
		t.Fatalf("a universal-ID authority did not correlate: %+v", byOID)
	}
}

func TestMissingAndExplicitlyNullAuthoritiesAreUnsupportedRatherThanMerged(t *testing.T) {
	readmit := `{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}`
	for name, payload := range map[string]string{"omitted": noAuthority, "explicit null": nullAuthority} {
		t.Run(name, func(t *testing.T) {
			path := writeCase(t, mllp(bookingReadmit), mllp(payload))
			result := report(t, path, rules(t, readmit, patientSelector))
			if len(result.Links) != 0 {
				t.Fatalf("an unqualified identifier was merged: %+v", result)
			}
			if !containsString(codes(result), "unknown_assigning_authority") {
				t.Fatalf("missing authority was not reported: %+v", result.Unsupported)
			}
			if !containsString(reasons(result), correlate.UnqualifiedIdentifier) {
				t.Fatalf("equal strings with no authority were not recorded as colliding: %+v", result)
			}
			// Only the occurrence whose authority was configured gave the rule
			// a usable key. The unqualified one is counted neither as
			// considered nor as unlinked: claiming otherwise would say the rule
			// reached evidence it could not read.
			if rule := result.Rules[0]; !rule.Applied || rule.Considered != 1 || rule.Linked != 0 || rule.Unlinked != 1 {
				t.Fatalf("unqualified accounting: %+v", rule)
			}
		})
	}

	// One unqualified identifier that collides with nothing is unsupported
	// evidence, not a collision: there is no second occurrence to confuse.
	alone := writeCase(t, mllp(noAuthority))
	result := report(t, alone, rules(t, readmit, `{"id":"patient","operator":"identifier","scope":"source","value":"PID-3.1","authority":["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}`))
	if len(result.Collisions) != 0 || !containsString(codes(result), "unknown_assigning_authority") {
		t.Fatalf("a lone unqualified identifier: %+v", result)
	}
	if rule := result.Rules[0]; rule.Considered != 0 || rule.Unlinked != 0 {
		t.Fatalf("an unreadable key was counted as evidence the rule reached: %+v", rule)
	}
}

func TestSessionScopeNeedsADeclaredSessionAndNeverInventsOne(t *testing.T) {
	declared := rules(t, "", `{"id":"same-message","operator":"control-id","scope":"session"}`)
	imported := report(t, writeCase(t, mllp(bookingReadmit), mllp(bookingReadmit)), declared)
	if imported.SessionDeclared || imported.Rules[0].Applied || len(imported.Links) != 0 {
		t.Fatalf("an imported case acquired a session: %+v", imported)
	}
	if !containsString(codes(imported), "no_declared_session") {
		t.Fatalf("the unapplied rule was not reported: %+v", imported.Unsupported)
	}

	recorded := report(t, writeRecorded(t, mllp(bookingReadmit), mllp(bookingReadmit)), declared)
	if !recorded.SessionDeclared || !recorded.Rules[0].Applied || len(recorded.Links) != 1 {
		t.Fatalf("a recorded session did not correlate its sources: %+v", recorded)
	}
}

func TestDeclaredScopeReportsSourcesTheCaseDoesNotHave(t *testing.T) {
	path := writeCase(t, mllp(bookingReadmit, bookingReadmit))
	partial := report(t, path, rules(t, "", `{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0001","s0009"]}`))
	if !partial.Rules[0].Applied || !containsString(codes(partial), "unknown_source") {
		t.Fatalf("an unknown source was not reported: %+v", partial)
	}
	if len(partial.Collisions) != 1 || partial.Collisions[0].Reason != correlate.DuplicateControlID {
		t.Fatalf("the known source was not correlated: %+v", partial)
	}

	absent := report(t, path, rules(t, "", `{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0009"]}`))
	if absent.Rules[0].Applied || absent.Rules[0].Considered != 0 || len(absent.Links) != 0 {
		t.Fatalf("a rule reaching no source claimed to have run: %+v", absent)
	}

	// Every absent source is named, not just the first: one rule missing two of
	// them reports both.
	both := report(t, path, rules(t, "", `{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0008","s0009"]}`))
	named := 0
	for _, item := range both.Unsupported {
		if item.Code == "unknown_source" {
			named++
		}
	}
	if named != 2 {
		t.Fatalf("only some absent sources were reported: %+v", both.Unsupported)
	}
}

func TestPreservedButUnparsedEvidenceTakesPartInNothingAndSaysSoOnce(t *testing.T) {
	path := writeCase(t, mllp(bookingReadmit, "NOT-HL7-AT-ALL"))
	result := report(t, path, rules(t, "",
		`{"id":"ack","operator":"acknowledges","scope":"source"}`,
		`{"id":"same-message","operator":"control-id","scope":"source"}`))
	unparsed := 0
	for _, item := range result.Unsupported {
		if item.Code == "unparsed_occurrence" {
			unparsed++
		}
	}
	if unparsed != 1 {
		t.Fatalf("unparsed evidence reported %d times: %+v", unparsed, result.Unsupported)
	}
	for _, link := range result.Links {
		for _, reference := range link.Occurrences {
			if reference.Kind == string(bundle.Unparsed) {
				t.Fatalf("unparsed evidence entered a link: %+v", link)
			}
		}
	}
}

func TestReportCarriesNoFieldBytesAndLeavesTheCaseUnchanged(t *testing.T) {
	path := writeCase(t, mllp(bookingReadmit, acceptMSG001), mllp(rescheduleOther))
	before, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	declared := rules(t,
		`{"key":"READMIT-MR","namespace":"READMIT","universal_id":"","universal_id_type":""}`,
		`{"id":"ack","operator":"acknowledges","scope":"source"}`,
		`{"id":"same-message","operator":"control-id","scope":"declared","sources":["s0001","s0002"]}`,
		patientSelector)
	result := report(t, path, declared)
	encoded, err := correlate.JSON(result)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded) + string(correlate.Terminal(result))
	for _, secret := range []string{"PATIENT-001", "MSG-001", "MSG-002", "ACK-001", "SCHEDULE", "SITE-A", path} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("the report disclosed %q", secret)
		}
	}
	after, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil || string(after) != string(before) {
		t.Fatal("correlation changed the case it read")
	}

	// The report names the exact declarations it ran, so a link can be traced
	// back to the rules that produced it.
	repeat := report(t, path, declared)
	if repeat.RulesSHA256 != result.RulesSHA256 || repeat.CaseIdentity != result.CaseIdentity {
		t.Fatal("identical rules over identical evidence produced a different report identity")
	}
	changed := report(t, path, rules(t, "", `{"id":"ack","operator":"acknowledges","scope":"source"}`))
	if changed.RulesSHA256 == result.RulesSHA256 {
		t.Fatal("different rules produced the same rules digest")
	}
}

func TestRunRefusesRulesItWasNotGivenThroughTheReader(t *testing.T) {
	path := writeCase(t, mllp(bookingReadmit))
	// A Rules value assembled in Go has never been through ParseRules, so Run
	// applies the same refusals rather than trusting the caller.
	if _, err := correlate.Run(path, correlate.Rules{}); err == nil {
		t.Fatal("ran a rules value that declares no contract")
	}
	unchecked := correlate.Rules{Schema: correlate.RulesSchema, Rules: []correlate.Rule{{ID: "a", Operator: "shell", Scope: correlate.SourceScope}}}
	if _, err := correlate.Run(path, unchecked); err == nil {
		t.Fatal("ran an operator outside the closed set")
	}
	valid := rules(t, "", `{"id":"ack","operator":"acknowledges","scope":"source"}`)
	if _, err := correlate.Run(filepath.Join(t.TempDir(), "absent"), valid); err == nil {
		t.Fatal("correlated a case that is not there")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
