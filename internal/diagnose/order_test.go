package diagnose_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/scenario"
)

// order, result and acknowledgement derive one occurrence each from the
// independently authored order fixtures. Expected outcomes are written in the
// tests, never computed by the rules under test.
func order(t *testing.T, control, filler, status string) []byte {
	t.Helper()
	raw := bytes.Replace(fixture(t, "diagnose-order.hl7"), []byte("DIAGNOSE-ORDER"), []byte(control), 1)
	raw = bytes.ReplaceAll(raw, []byte("FILLER-001"), []byte(filler))
	return bytes.Replace(raw, []byte("||SC"), []byte("||"+status), 1)
}

func result(t *testing.T, control, placer, filler, status string) []byte {
	t.Helper()
	raw := bytes.Replace(fixture(t, "diagnose-result.hl7"), []byte("DIAGNOSE-RESULT"), []byte(control), 1)
	raw = bytes.ReplaceAll(raw, []byte("PLACER-001"), []byte(placer))
	raw = bytes.ReplaceAll(raw, []byte("FILLER-001"), []byte(filler))
	return bytes.Replace(raw, []byte("|F\nOBX"), []byte("|"+status+"\nOBX"), 1)
}

func acknowledgement(t *testing.T, control, reference, code, location string) []byte {
	t.Helper()
	raw := bytes.Replace(fixture(t, "diagnose-order-ack.hl7"), []byte("MSA|CA|DIAGNOSE-ORDER|"), []byte("MSA|"+code+"|"+reference+"|"), 1)
	raw = bytes.Replace(raw, []byte("DIAGNOSE-ORDER-ACK"), []byte(control), 1)
	return bytes.Replace(raw, []byte("PID^1^3^1^4^1"), []byte(location), 1)
}

// stages rewrites the acknowledgement conditions an occurrence's own header
// declares. Empty conditions leave both fields present and empty; omitting them
// is original acknowledgement mode, which declares no stage request at all.
func stages(raw []byte, accept, application string) []byte {
	if accept == "" && application == "" {
		return bytes.Replace(raw, []byte("|2.5.1|||AL|AL\n"), []byte("|2.5.1\n"), 1)
	}
	return bytes.Replace(raw, []byte("|AL|AL\n"), []byte("|"+accept+"|"+application+"\n"), 1)
}

func quietOrder(t *testing.T, control, filler, status string) []byte {
	t.Helper()
	return stages(order(t, control, filler, status), "NE", "NE")
}

func TestOrderRulesetIsSelectedExplicitlyAndLeavesTheOtherContractsUnchanged(t *testing.T) {
	siu, err := os.ReadFile("profile.json")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := os.ReadFile("lifecycle-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diagnose.ProfileSnapshot(), siu) || diagnose.DefaultConfig().Profile != diagnose.Profile || diagnose.DefaultConfig().Ruleset != diagnose.Ruleset {
		t.Fatal("the named SIU contract changed")
	}
	stored, err := os.ReadFile("order-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range [][]byte{siu, lifecycle} {
		if bytes.Contains(other, []byte(diagnose.OrderProfile)) || bytes.Contains(other, []byte(diagnose.OrderRuleset)) {
			t.Fatal("the order definition is not a separate document")
		}
	}
	if !bytes.Contains(stored, []byte(diagnose.OrderProfile)) || bytes.Contains(stored, []byte(diagnose.LifecycleProfile)) {
		t.Fatal("the order document does not name its own contract")
	}
	// Neither existing contract acquires order meaning by being run over order bytes.
	for _, config := range []diagnose.Config{diagnose.DefaultConfig(), diagnose.LifecycleConfig()} {
		r := run(t, writeCase(t, fixture(t, "diagnose-order.hl7")), config)
		if len(r.Findings) != 0 || len(r.Unsupported) != 1 || r.Unsupported[0].Code != "unsupported_message_type" {
			t.Fatalf("an existing ruleset interpreted order evidence: %+v", r)
		}
	}
	r := run(t, writeCase(t, quietOrder(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")), diagnose.OrderConfig())
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 || !strings.Contains(r.NoFindings, diagnose.OrderRuleset) {
		t.Fatalf("complete order produced findings: %+v", r)
	}
	config := diagnose.OrderConfig()
	config.Profile = diagnose.LifecycleProfile
	r = run(t, writeCase(t, fixture(t, "diagnose-order.hl7")), config)
	if len(r.Rules) != 0 || len(r.Findings) != 0 || r.Unsupported[0].Code != "unsupported_profile" {
		t.Fatalf("mismatched profile and ruleset silently evaluated: %+v", r)
	}
}

func TestResultsCorrelateWithOrdersOnlyWithinOneConfiguredNamespace(t *testing.T) {
	placed := quietOrder(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	reported := result(t, "DIAGNOSE-RESULT", "PLACER-001", "FILLER-001", "F")
	r := run(t, writeCase(t, reported), diagnose.OrderConfig())
	fs := findings(r, diagnose.OrderNotObserved)
	if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Window != r.Window.Description || fs[0].Profile != diagnose.OrderProfile || fs[0].Ruleset != diagnose.OrderRuleset {
		t.Fatalf("unbounded or missing hypothesis: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, "observed case window") || !strings.Contains(fs[0].Summary, "configured namespace") || strings.Contains(fs[0].Summary, "never") {
		t.Fatalf("assumptions absent from finding: %q", fs[0].Summary)
	}
	if len(fs[0].Evidence) != 5 || fs[0].Evidence[1].Field != "OBR-2.1" || fs[0].Evidence[1].State != hl7.Present {
		t.Fatalf("missing identifier evidence: %+v", fs[0].Evidence)
	}
	if r = run(t, writeCase(t, placed, reported), diagnose.OrderConfig()); len(findings(r, diagnose.OrderNotObserved)) != 0 {
		t.Fatalf("observed order did not correlate: %+v", r)
	}
	// Capture order inside the window is never read as business chronology.
	if r = run(t, writeCase(t, reported, placed), diagnose.OrderConfig()); len(findings(r, diagnose.OrderNotObserved)) != 0 {
		t.Fatalf("capture order changed the finding: %+v", r)
	}
	// An order placed under another authority is not the same order.
	elsewhere := bytes.ReplaceAll(placed, []byte("PLACER-001^READMIT"), []byte("PLACER-001^OTHER"))
	config := diagnose.OrderConfig()
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER", Namespace: "OTHER"})
	if r = run(t, writeCase(t, elsewhere, reported), config); len(findings(r, diagnose.OrderNotObserved)) != 1 {
		t.Fatalf("identifier bytes correlated across authorities: %+v", r)
	}
	config.Namespaces[1].Key = "READMIT" // An explicit analyst assertion of equivalence, not inferred equality.
	if r = run(t, writeCase(t, elsewhere, reported), config); len(findings(r, diagnose.OrderNotObserved)) != 0 {
		t.Fatalf("configured alias did not correlate: %+v", r)
	}
}

func TestRepeatedOutputIsReportedAsAFactAndNeverAsAProgression(t *testing.T) {
	first := result(t, "DIAGNOSE-RESULT", "PLACER-001", "FILLER-001", "F")
	again := result(t, "DIAGNOSE-REPEAT", "PLACER-001", "FILLER-001", "F")
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.DuplicateOutput, diagnose.StatusProgression}
	r := run(t, writeCase(t, first, again), config)
	fs := findings(r, diagnose.DuplicateOutput)
	if len(fs) != 1 || fs[0].Classification != "observed_fact" || fs[0].Window != r.Window.Description {
		t.Fatalf("repeated output not reported as a bounded fact: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, "does not establish") || strings.Contains(fs[0].Summary, "duplicate delivery") {
		t.Fatalf("repeated output overclaimed: %q", fs[0].Summary)
	}
	if len(fs[0].Evidence) != 6 || fs[0].Evidence[1].Occurrence == fs[0].Evidence[4].Occurrence || fs[0].Evidence[2].Field != "OBR-25" {
		t.Fatalf("both occurrences are not cited: %+v", fs[0].Evidence)
	}
	if len(findings(r, diagnose.StatusProgression)) != 0 {
		t.Fatalf("one status reported as a progression conflict: %+v", r.Findings)
	}
	// A different declared status is a progression, not a repetition.
	corrected := result(t, "DIAGNOSE-CORRECT", "PLACER-001", "FILLER-001", "C")
	if r = run(t, writeCase(t, first, corrected), config); len(r.Findings) != 0 {
		t.Fatalf("distinct statuses reported as repeated output: %+v", r.Findings)
	}
	// A different identity in the same status is not the same output.
	other := result(t, "DIAGNOSE-OTHER", "PLACER-001", "FILLER-002", "F")
	if r = run(t, writeCase(t, first, other), config); len(findings(r, diagnose.DuplicateOutput)) != 0 {
		t.Fatalf("distinct identities reported as repeated output: %+v", r.Findings)
	}
	// Equal identifier bytes under another authority are not the same output.
	elsewhere := bytes.ReplaceAll(again, []byte("FILLER-001^READMIT"), []byte("FILLER-001^OTHER"))
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER", Namespace: "OTHER"})
	if r = run(t, writeCase(t, first, elsewhere), config); len(findings(r, diagnose.DuplicateOutput)) != 0 {
		t.Fatalf("outputs correlated across authorities: %+v", r.Findings)
	}
}

func TestStatusProgressionIsProfileDependentAndNeverOrdersOccurrences(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.StatusProgression}
	final := result(t, "DIAGNOSE-RESULT", "PLACER-001", "FILLER-001", "F")
	cancelled := result(t, "DIAGNOSE-CANCEL", "PLACER-001", "FILLER-001", "X")
	r := run(t, writeCase(t, final, cancelled), config)
	fs := findings(r, diagnose.StatusProgression)
	if len(fs) != 1 || fs[0].Classification != "profile_violation" || fs[0].Window != r.Window.Description || len(fs[0].Evidence) != 6 {
		t.Fatalf("conflicting final statuses not reported: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, diagnose.OrderProfile) || !strings.Contains(fs[0].Summary, "F (final)") || !strings.Contains(fs[0].Summary, "X (no results available, order cancelled)") {
		t.Fatalf("the conflict does not name the profile and its declared statuses: %q", fs[0].Summary)
	}
	if !strings.Contains(fs[0].Summary, "not which of them is correct") || !strings.Contains(fs[0].Summary, "not the order they happened in") {
		t.Fatalf("progression overclaimed: %q", fs[0].Summary)
	}
	if reversed := run(t, writeCase(t, cancelled, final), config); len(findings(reversed, diagnose.StatusProgression)) != 1 {
		t.Fatalf("capture order changed the conflict: %+v", reversed.Findings)
	}
	// A status the profile does not declare final never conflicts with one.
	for _, code := range []string{"P", "C", "A"} {
		partial := result(t, "DIAGNOSE-PARTIAL", "PLACER-001", "FILLER-001", code)
		if r = run(t, writeCase(t, final, partial), config); len(r.Findings) != 0 {
			t.Fatalf("status %s reported as a final conflict: %+v", code, r.Findings)
		}
	}
	// Order statuses are their own vocabulary and never compare with result statuses.
	completed := quietOrder(t, "DIAGNOSE-ORDER", "FILLER-001", "CM")
	if r = run(t, writeCase(t, completed, final), config); len(r.Findings) != 0 {
		t.Fatalf("two vocabularies compared as one: %+v", r.Findings)
	}
	discontinued := quietOrder(t, "DIAGNOSE-STOP", "FILLER-001", "DC")
	if r = run(t, writeCase(t, completed, discontinued), config); len(findings(r, diagnose.StatusProgression)) != 1 {
		t.Fatalf("conflicting order statuses not reported: %+v", r.Findings)
	}
}

func TestStatusesTheProfileCannotJudgeAreRecordedRatherThanPassed(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.DuplicateOutput, diagnose.StatusProgression}
	final := result(t, "DIAGNOSE-RESULT", "PLACER-001", "FILLER-001", "F")
	for _, tc := range []struct{ name, code, status string }{
		{"undeclared status", "unsupported_status_value", "Z"},
		{"empty status", "missing_output_status", ""},
		{"null status", "missing_output_status", `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, final, result(t, "DIAGNOSE-UNKNOWN", "PLACER-001", "FILLER-001", tc.status)), config)
			if len(r.Findings) != 0 {
				t.Fatalf("an unjudgeable status was compared: %+v", r.Findings)
			}
			if !contains(codes(r), tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r.Unsupported)
			}
		})
	}
}

func TestAcknowledgementStagesAreEvaluatedOnlyWhereTheHeaderAsksForThem(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.ACKStageNotObserved}
	placed := order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	commit := acknowledgement(t, "DIAGNOSE-COMMIT", "DIAGNOSE-ORDER", "CA", "PID^1^3^1^4^1")
	applied := acknowledgement(t, "DIAGNOSE-APPLIED", "DIAGNOSE-ORDER", "AA", "PID^1^3^1^4^1")
	r := run(t, writeCase(t, placed), config)
	if len(findings(r, diagnose.ACKStageNotObserved)) != 2 {
		t.Fatalf("both asked-for stages were not evaluated: %+v", r.Findings)
	}
	r = run(t, writeCase(t, placed, commit), config)
	fs := findings(r, diagnose.ACKStageNotObserved)
	if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Window != r.Window.Description {
		t.Fatalf("the accept stage did not answer its own stage: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, "application acknowledgement") || !strings.Contains(fs[0].Summary, "AA, AE or AR") || !strings.Contains(fs[0].Summary, "observed case window") {
		t.Fatalf("stage hypothesis does not name its stage and window: %q", fs[0].Summary)
	}
	if len(fs[0].Evidence) != 2 || fs[0].Evidence[0].Field != "MSH-10" || fs[0].Evidence[1].Field != "MSH-16" {
		t.Fatalf("missing stage evidence: %+v", fs[0].Evidence)
	}
	// An application acknowledgement is never read as a commit, either.
	r = run(t, writeCase(t, placed, applied), config)
	fs = findings(r, diagnose.ACKStageNotObserved)
	if len(fs) != 1 || !strings.Contains(fs[0].Summary, "accept acknowledgement") {
		t.Fatalf("an application outcome answered the accept stage: %+v", r.Findings)
	}
	if r = run(t, writeCase(t, placed, commit, applied), config); len(r.Findings) != 0 {
		t.Fatalf("both observed stages still reported: %+v", r.Findings)
	}
	// Capture order never decides whether an acknowledgement is linked.
	if r = run(t, writeCase(t, commit, applied, placed), config); len(r.Findings) != 0 {
		t.Fatalf("capture order changed the stage findings: %+v", r.Findings)
	}
	for _, tc := range []struct {
		name, accept, application, code string
		findings                        int
	}{
		{name: "never asked", accept: "NE", application: "NE"},
		{name: "original mode declares no stage"},
		{name: "empty conditions ask for nothing", accept: "", application: ""},
		{name: "conditional on error", accept: "ER", application: "NE", code: "conditional_acknowledgement_request"},
		{name: "conditional on success", accept: "NE", application: "SU", code: "conditional_acknowledgement_request"},
		{name: "undeclared condition", accept: "ZZ", application: "NE", code: "unsupported_acknowledgement_condition"},
		{name: "explicit null condition", accept: `""`, application: "NE", code: "unsupported_acknowledgement_condition"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := stages(placed, tc.accept, tc.application)
			if tc.name == "empty conditions ask for nothing" {
				raw = bytes.Replace(placed, []byte("|AL|AL\n"), []byte("||\n"), 1)
			}
			r := run(t, writeCase(t, raw), config)
			if len(findings(r, diagnose.ACKStageNotObserved)) != tc.findings {
				t.Fatalf("stage request misread: %+v", r.Findings)
			}
			if tc.code != "" && !contains(codes(r), tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r.Unsupported)
			}
			if tc.code == "" && len(r.Unsupported) != 0 {
				t.Fatalf("a stage nobody asked for was reported: %+v", r.Unsupported)
			}
		})
	}
}

func TestAcknowledgementsThatNameNoSingleOccurrenceAreNotLinked(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.ACKStageNotObserved, diagnose.ACKErrorLocation}
	placed := order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	for _, tc := range []struct {
		name, code string
		payloads   [][]byte
	}{
		{"unknown reference", "acknowledged_occurrence_not_observed", [][]byte{placed, acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ELSEWHERE", "AA", "PID^1^3^1^4^1")}},
		{"repeated control identifier", "ambiguous_acknowledged_occurrence", [][]byte{placed, order(t, "DIAGNOSE-ORDER", "FILLER-002", "SC"), acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AA", "PID^1^3^1^4^1")}},
		{"unknown acknowledgement code", "unsupported_acknowledgement_stage", [][]byte{placed, acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "ZZ", "PID^1^3^1^4^1")}},
		{"no echoed identifier", "unsupported_acknowledgement_reference", [][]byte{placed, bytes.Replace(acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AA", "PID^1^3^1^4^1"), []byte("|DIAGNOSE-ORDER|"), []byte("||"), 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, tc.payloads...), config)
			if !contains(codes(r), tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r.Unsupported)
			}
			if len(findings(r, diagnose.ACKErrorLocation)) != 0 {
				t.Fatalf("an unlinked acknowledgement located an error: %+v", r.Findings)
			}
		})
	}
}

func TestErrorLocationsPointIntoTheAcknowledgedOccurrence(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.ACKErrorLocation}
	placed := stages(order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC"), "NE", "NE")
	r := run(t, writeCase(t, placed, acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AE", "PID^1^3^1^4^1")), config)
	fs := findings(r, diagnose.ACKErrorLocation)
	if len(fs) != 1 || fs[0].Classification != "observed_fact" || fs[0].Window != r.Window.Description {
		t.Fatalf("declared error location not reported: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, "PID[1]-3[1].4.1") || !strings.Contains(fs[0].Summary, "not that the field is wrong") {
		t.Fatalf("error location overclaimed or unlocated: %q", fs[0].Summary)
	}
	if len(fs[0].Evidence) != 3 || fs[0].Evidence[2].Field != "PID[1]-3[1].4.1" || fs[0].Evidence[2].Occurrence == fs[0].Evidence[1].Occurrence {
		t.Fatalf("the located field of the acknowledged occurrence is not cited: %+v", fs[0].Evidence)
	}
	if fs[0].Evidence[2].State != hl7.Present {
		t.Fatalf("located field state not preserved: %+v", fs[0].Evidence[2])
	}
	// A location the acknowledged occurrence does not fill stays a distinct state.
	r = run(t, writeCase(t, placed, acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AE", "PID^1^8")), config)
	fs = findings(r, diagnose.ACKErrorLocation)
	if len(fs) != 1 || fs[0].Evidence[2].Field != "PID[1]-8[1]" || fs[0].Evidence[2].State != hl7.Omitted {
		t.Fatalf("absent located field not reported as omitted: %+v", r.Findings)
	}
	// An ERR that declares no location is not an unreadable one.
	r = run(t, writeCase(t, placed, bytes.Replace(acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AE", "PID^1^3^1^4^1"), []byte("ERR||PID^1^3^1^4^1|"), []byte("ERR|||"), 1)), config)
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 {
		t.Fatalf("an absent location was reported: %+v", r)
	}
	for _, location := range []string{"PID", "PID^1^0^1", "PID^one^3", "PATIENT^1^3", "PID^1^3^1^^1", "PID^1^1000"} {
		t.Run(location, func(t *testing.T) {
			r := run(t, writeCase(t, placed, acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AE", location)), config)
			if len(r.Findings) != 0 || !contains(codes(r), "unsupported_error_location") {
				t.Fatalf("an unreadable location was interpreted: %+v", r)
			}
		})
	}
}

func TestOrderDiagnosisNeverInterpretsClinicalContent(t *testing.T) {
	r := run(t, writeCase(t, order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC"), result(t, "DIAGNOSE-RESULT", "PLACER-001", "FILLER-001", "F"), acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "AE", "PID^1^3^1^4^1")), diagnose.OrderConfig())
	data, err := diagnose.JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	rendered := append(data, diagnose.Markdown(r)...)
	for _, secret := range []string{"SECRET-", "OBX", "OBR-4", "PATIENT-001", "FILLER-001", "PLACER-001", "DIAGNOSE-ORDER", "SYNTHETIC-FIXTURE"} {
		if bytes.Contains(rendered, []byte(secret)) {
			t.Fatalf("the order report disclosed %s", secret)
		}
	}
	for _, f := range r.Findings {
		for _, evidence := range f.Evidence {
			if strings.HasPrefix(evidence.Field, "OBX") {
				t.Fatalf("an observation value was interpreted: %+v", evidence)
			}
		}
	}
}

func TestEveryOrderRuleIsSelectableOnItsOwnAndReproducible(t *testing.T) {
	payloads := [][]byte{
		order(t, "DIAGNOSE-ORDER", "FILLER-001", "CM"),
		bytes.Replace(quietOrder(t, "DIAGNOSE-STOP", "FILLER-001", "DC"), []byte("ORC|NW|"), []byte("ORC||"), 1),
		result(t, "DIAGNOSE-RESULT", "PLACER-009", "FILLER-009", "F"),
		result(t, "DIAGNOSE-REPEAT", "PLACER-009", "FILLER-009", "F"),
		acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "CA", "PID^1^3^1^4^1"),
	}
	expected := map[string]int{
		diagnose.OrderRequiredField:  1,
		diagnose.OrderNotObserved:    2,
		diagnose.DuplicateOutput:     1,
		diagnose.StatusProgression:   1,
		diagnose.ACKStageNotObserved: 1,
		diagnose.ACKErrorLocation:    1,
		diagnose.DuplicateControl:    0,
		diagnose.ACKOutcome:          1,
		diagnose.ACKError:            1,
	}
	path := writeCase(t, payloads...)
	total := 0
	for rule, want := range expected {
		total += want
		config := diagnose.OrderConfig()
		config.Rules = []string{rule}
		r := run(t, path, config)
		if len(r.Rules) != 1 || r.Rules[0] != rule {
			t.Fatalf("selected rules not reported: %+v", r.Rules)
		}
		if len(findings(r, rule)) != want || len(r.Findings) != want {
			t.Fatalf("rule %s is not independent: %+v", rule, r.Findings)
		}
	}
	report := run(t, path, diagnose.OrderConfig())
	if len(report.Findings) != total {
		t.Fatalf("combined selection changed the findings: %+v", report.Findings)
	}
	// Every rule this contract adds carries the window that bounds it. The three
	// message-level rules keep the output they already had under the other two
	// rulesets, where the report-level window is the only bound they state.
	for _, f := range report.Findings {
		shared := f.RuleID == diagnose.DuplicateControl || f.RuleID == diagnose.ACKOutcome || f.RuleID == diagnose.ACKError
		if !shared && f.Window != report.Window.Description {
			t.Fatalf("finding omits the observed capture window: %+v", f)
		}
		if shared && f.Window != "" {
			t.Fatalf("a shared message rule changed its existing output: %+v", f)
		}
	}
	first, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := diagnose.JSON(run(t, writeCase(t, payloads...), diagnose.OrderConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical evidence and configuration produced different findings")
	}
}

func TestOrderRulesAreNeverEvaluatedUnderAnotherContract(t *testing.T) {
	placed := fixture(t, "diagnose-order.hl7")
	for _, config := range []diagnose.Config{diagnose.DefaultConfig(), diagnose.LifecycleConfig()} {
		config.Rules = append(config.Rules, diagnose.StatusProgression, diagnose.ACKErrorLocation)
		r := run(t, writeCase(t, placed), config)
		if !contains(codes(r), "unsupported_rule") || contains(r.Rules, diagnose.StatusProgression) || contains(r.Rules, diagnose.ACKErrorLocation) {
			t.Fatalf("an order rule was accepted by another ruleset: %+v", r)
		}
	}
	config := diagnose.OrderConfig()
	config.Rules = append(config.Rules, diagnose.BookingNotObserved, diagnose.VisitNotObserved)
	r := run(t, writeCase(t, placed), config)
	if !contains(codes(r), "unsupported_rule") || contains(r.Rules, diagnose.BookingNotObserved) {
		t.Fatalf("another contract's rule was accepted by the order ruleset: %+v", r)
	}
	// An unnamed ruleset gives no rule identifier a meaning, and hides nothing:
	// the order occurrence no registered ruleset can read is still reported.
	config = diagnose.OrderConfig()
	config.Ruleset = "future-ruleset"
	r = run(t, writeCase(t, placed), config)
	if len(r.Rules) != 0 || len(r.Findings) != 0 || !contains(codes(r), "unsupported_ruleset") || !contains(codes(r), "unsupported_message_type") {
		t.Fatalf("unnamed ruleset silently evaluated: %+v", r)
	}
}

func TestOrderProfileRulesAreOnlyRunOverTheirOwnGeneratorProfile(t *testing.T) {
	placed := order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	path := writeInputs(t, []bundle.Input{{Data: placed}, {Data: acknowledgement(t, "DIAGNOSE-ACK", "DIAGNOSE-ORDER", "CA", "PID^1^3^1^4^1")}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 0, BaseTime: *timePtr("2026-01-01T00:00:00Z"), GeneratorVersion: "synthetic-v1", ProfileVersion: diagnose.Profile}})
	r := run(t, path, diagnose.OrderConfig())
	if len(r.Unsupported) == 0 || r.Unsupported[0].Code != "unsupported_bundle_profile" || !strings.Contains(r.Unsupported[0].Detail, "order and result profile rules") {
		t.Fatalf("generator profile silently reinterpreted: %+v", r.Unsupported)
	}
	for _, rule := range []string{diagnose.OrderRequiredField, diagnose.DuplicateOutput, diagnose.StatusProgression, diagnose.OrderNotObserved} {
		if len(findings(r, rule)) != 0 {
			t.Fatalf("order profile rules ran over another profile's bundle: %+v", r.Findings)
		}
	}
	// An acknowledgement answers an occurrence, not a profile, and still links.
	if len(findings(r, diagnose.ACKErrorLocation)) != 1 || len(findings(r, diagnose.ACKStageNotObserved)) != 1 {
		t.Fatalf("acknowledgement rules depend on a generator profile: %+v", r.Findings)
	}
}

// The order and result contract reads captured evidence; the authoring
// lifecycle profiles of scenario design decide whether a step somebody wrote
// down is taken. They share no event, and this keeps it that way in both
// directions: an order trigger that any authoring profile declares, or an
// authoring event this profile starts naming, would let one contract's finding
// be read as the other's transition.
func TestOrderTriggersAreDeclaredByNoAuthoringLifecycleProfile(t *testing.T) {
	var document struct {
		Triggers []struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"triggers"`
	}
	raw, err := os.ReadFile("order-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Triggers) == 0 {
		t.Fatal("the order profile declares no trigger")
	}
	for _, trigger := range document.Triggers {
		for _, profile := range []scenario.ProfileName{scenario.ADTLifecycle, scenario.SIULifecycle} {
			t.Run(trigger.Kind+" "+trigger.Code+" "+string(profile), func(t *testing.T) {
				designed := scenario.Scenario{
					Profile:  profile,
					BaseTime: *timePtr("2026-01-01T00:00:00Z"),
					Subjects: []scenario.Subject{{ID: "p", Kind: scenario.PatientSubject, Namespace: "READMIT", Identifier: "PATIENT-001", InitialState: scenario.PatientActive}},
					Steps:    []scenario.Step{{ID: "s1", Event: scenario.Event(trigger.Code), Subject: "p", After: "0s", Expect: scenario.Accepted}},
				}
				_, err := scenario.Preview(designed)
				if err == nil || !strings.Contains(err.Error(), "declares no event") {
					t.Fatalf("the order profile names an event authoring profile %s declares: %v", profile, err)
				}
			})
		}
	}
}

func TestARepeatedControlIdentifierIsOnlyReportedWhereItStopsAnEvaluation(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.ACKStageNotObserved}
	asking := order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	r := run(t, writeCase(t, asking, order(t, "DIAGNOSE-ORDER", "FILLER-002", "SC")), config)
	if len(r.Findings) != 0 || !contains(codes(r), "ambiguous_acknowledged_occurrence") {
		t.Fatalf("a repeated control identifier was resolved to one occurrence: %+v", r)
	}
	quiet := quietOrder(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	r = run(t, writeCase(t, quiet, quietOrder(t, "DIAGNOSE-ORDER", "FILLER-002", "SC")), config)
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 {
		t.Fatalf("occurrences that ask for no stage reported an unevaluated one: %+v", r)
	}
}

func TestAnUndecodableAcknowledgementSatisfiesNothing(t *testing.T) {
	config := diagnose.OrderConfig()
	config.Rules = []string{diagnose.ACKStageNotObserved, diagnose.ACKErrorLocation}
	placed := order(t, "DIAGNOSE-ORDER", "FILLER-001", "SC")
	commit := acknowledgement(t, "DIAGNOSE-COMMIT", "DIAGNOSE-ORDER", "CA", "PID^1^3^1^4^1")
	beyond := append(bytes.Clone(commit), bytes.Repeat([]byte("ERR||PID^1^3^1^4^1|101^SECRET-ERR-TEXT^HL70357|E\n"), 128)...)
	r := run(t, writeCase(t, placed, beyond), config)
	if !contains(codes(r), "unsupported_ack_cardinality") {
		t.Fatalf("the bounded decoder did not report the acknowledgement: %+v", r.Unsupported)
	}
	if len(findings(r, diagnose.ACKErrorLocation)) != 0 {
		t.Fatalf("an undecoded acknowledgement located an error: %+v", r.Findings)
	}
	// Both asked-for stages remain unanswered: nothing readmit refused to decode
	// is allowed to satisfy a request the header made.
	if len(findings(r, diagnose.ACKStageNotObserved)) != 2 {
		t.Fatalf("an undecoded acknowledgement satisfied a stage: %+v", r.Findings)
	}
}
