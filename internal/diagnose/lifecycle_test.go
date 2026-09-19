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

// adt derives one ADT occurrence from the independently authored admission
// fixture. Expected outcomes are written in the tests, never computed by the
// rules under test.
func adt(t *testing.T, trigger, control, visit string) []byte {
	t.Helper()
	raw := bytes.ReplaceAll(fixture(t, "diagnose-admit.hl7"), []byte("A01"), []byte(trigger))
	raw = bytes.ReplaceAll(raw, []byte("DIAGNOSE-ADMIT"), []byte(control))
	return bytes.ReplaceAll(raw, []byte("VISIT-001"), []byte(visit))
}

func siu(t *testing.T, trigger, control string) []byte {
	t.Helper()
	raw := bytes.ReplaceAll(fixture(t, "diagnose-reschedule.hl7"), []byte("S13"), []byte(trigger))
	return bytes.ReplaceAll(raw, []byte("DIAGNOSE-RESCHEDULE"), []byte(control))
}

func codes(r diagnose.Report) []string {
	list := []string{}
	for _, u := range r.Unsupported {
		list = append(list, u.Code)
	}
	return list
}

func TestLifecycleRulesetIsSelectedExplicitlyAndLeavesTheSIUContractUnchanged(t *testing.T) {
	stored, err := os.ReadFile("profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(diagnose.ProfileSnapshot(), stored) || diagnose.DefaultConfig().Ruleset != diagnose.Ruleset || diagnose.DefaultConfig().Profile != diagnose.Profile {
		t.Fatal("the named SIU contract changed")
	}
	lifecycle, err := os.ReadFile("lifecycle-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(lifecycle, []byte(diagnose.LifecycleProfile)) || bytes.Contains(stored, []byte(diagnose.LifecycleProfile)) {
		t.Fatal("the lifecycle definition is not a separate document")
	}
	// The default contract never acquires ADT meaning by being run over ADT bytes.
	r := run(t, writeCase(t, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")), diagnose.DefaultConfig())
	if len(r.Findings) != 0 || len(r.Unsupported) != 1 || r.Unsupported[0].Code != "unsupported_message_type" {
		t.Fatalf("SIU ruleset interpreted ADT evidence: %+v", r)
	}
	r = run(t, writeCase(t, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")), diagnose.LifecycleConfig())
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 || !strings.Contains(r.NoFindings, diagnose.LifecycleRuleset) {
		t.Fatalf("complete admission produced findings: %+v", r)
	}
	config := diagnose.LifecycleConfig()
	config.Profile = diagnose.Profile
	r = run(t, writeCase(t, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")), config)
	if len(r.Rules) != 0 || len(r.Findings) != 0 || r.Unsupported[0].Code != "unsupported_profile" {
		t.Fatalf("mismatched profile and ruleset silently evaluated: %+v", r)
	}
}

func TestVisitEventsCorrelateOnlyWithinOneConfiguredNamespace(t *testing.T) {
	admit := adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")
	register := adt(t, "A04", "DIAGNOSE-REGISTER", "VISIT-002")
	for _, trigger := range []string{"A02", "A03", "A08", "A11", "A13"} {
		t.Run(trigger, func(t *testing.T) {
			dependent := adt(t, trigger, "DIAGNOSE-"+trigger, "VISIT-001")
			r := run(t, writeCase(t, admit, dependent), diagnose.LifecycleConfig())
			if len(r.Findings) != 0 || len(r.Unsupported) != 0 {
				t.Fatalf("observed admission did not correlate: %+v", r)
			}
			r = run(t, writeCase(t, register, dependent), diagnose.LifecycleConfig())
			fs := findings(r, diagnose.VisitNotObserved)
			if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Window != r.Window.Description {
				t.Fatalf("unbounded or missing hypothesis: %+v", r)
			}
			if !strings.Contains(fs[0].Summary, "observed case window") || !strings.Contains(fs[0].Summary, "configured namespace") || strings.Contains(fs[0].Summary, "never") {
				t.Fatalf("assumptions absent from finding: %q", fs[0].Summary)
			}
			if len(fs[0].Evidence) != 5 || fs[0].Evidence[1].Field != "PV1-19.1" || fs[0].Evidence[1].State != hl7.Present {
				t.Fatalf("missing identifier evidence: %+v", fs[0].Evidence)
			}
			// A different assigning authority is never the same visit.
			other := bytes.ReplaceAll(adt(t, trigger, "DIAGNOSE-OTHER", "VISIT-001"), []byte("VISIT-001^^^READMIT"), []byte("VISIT-001^^^OTHER"))
			config := diagnose.LifecycleConfig()
			config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER", Namespace: "OTHER"})
			if r = run(t, writeCase(t, admit, other), config); len(findings(r, diagnose.VisitNotObserved)) != 1 {
				t.Fatalf("distinct authorities correlated: %+v", r)
			}
			config.Namespaces[1].Key = "READMIT" // An explicit analyst assertion of equivalence, not inferred equality.
			if r = run(t, writeCase(t, admit, other), config); len(r.Findings) != 0 {
				t.Fatalf("configured alias did not correlate: %+v", r)
			}
		})
	}
}

func TestCancelAndNoShowEventsAreCorrelatedFromEvidenceAlone(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		opening    []byte
		cancel     []byte
	}{
		{"cancel admit", diagnose.VisitNotObserved, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001"), adt(t, "A11", "DIAGNOSE-CANCEL", "VISIT-001")},
		{"cancel discharge", diagnose.VisitNotObserved, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001"), adt(t, "A13", "DIAGNOSE-RECOVER", "VISIT-001")},
		{"cancel appointment", diagnose.AppointmentNotObserved, fixture(t, "diagnose-booking.hl7"), siu(t, "S15", "DIAGNOSE-CANCEL")},
		{"appointment no show", diagnose.AppointmentNotObserved, fixture(t, "diagnose-booking.hl7"), siu(t, "S26", "DIAGNOSE-NOSHOW")},
		{"appointment modification", diagnose.AppointmentNotObserved, fixture(t, "diagnose-booking.hl7"), siu(t, "S14", "DIAGNOSE-MODIFY")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, tc.cancel), diagnose.LifecycleConfig())
			fs := findings(r, tc.rule)
			if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Ruleset != diagnose.LifecycleRuleset || fs[0].Profile != diagnose.LifecycleProfile {
				t.Fatalf("isolated cancellation not reported as a bounded hypothesis: %+v", r)
			}
			if r = run(t, writeCase(t, tc.opening, tc.cancel), diagnose.LifecycleConfig()); len(findings(r, tc.rule)) != 0 {
				t.Fatalf("observed opening did not suppress the hypothesis: %+v", r)
			}
			// Order inside the window is never read as business chronology.
			if r = run(t, writeCase(t, tc.cancel, tc.opening), diagnose.LifecycleConfig()); len(findings(r, tc.rule)) != 0 {
				t.Fatalf("capture order changed the finding: %+v", r)
			}
		})
	}
}

func TestMergeReportsOnlyAnUnobservedPriorIdentifier(t *testing.T) {
	merge := fixture(t, "diagnose-merge.hl7")
	r := run(t, writeCase(t, merge), diagnose.LifecycleConfig())
	fs := findings(r, diagnose.MergeIdentifierNotObserved)
	if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Window != r.Window.Description || len(fs[0].Evidence) != 5 || fs[0].Evidence[1].Field != "MRG-1.1" {
		t.Fatalf("merge hypothesis missing or unbounded: %+v", r)
	}
	prior := bytes.ReplaceAll(adt(t, "A01", "DIAGNOSE-PRIOR", "VISIT-009"), []byte("PATIENT-001"), []byte("PATIENT-000"))
	if r = run(t, writeCase(t, prior, merge), diagnose.LifecycleConfig()); len(findings(r, diagnose.MergeIdentifierNotObserved)) != 0 {
		t.Fatalf("observed prior identifier did not correlate: %+v", r)
	}
	// An identifier from another authority is not the same identifier.
	elsewhere := bytes.ReplaceAll(prior, []byte("PATIENT-000^^^READMIT"), []byte("PATIENT-000^^^OTHER"))
	config := diagnose.LifecycleConfig()
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER", Namespace: "OTHER"})
	if r = run(t, writeCase(t, elsewhere, merge), config); len(findings(r, diagnose.MergeIdentifierNotObserved)) != 1 {
		t.Fatalf("identifier bytes correlated across authorities: %+v", r)
	}
}

func TestEventTypeMismatchIsReportedWithoutDisclosingEitherValue(t *testing.T) {
	mismatch := bytes.Replace(adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001"), []byte("EVN|A01"), []byte("EVN|A02"), 1)
	r := run(t, writeCase(t, mismatch), diagnose.LifecycleConfig())
	fs := findings(r, diagnose.EventTypeMismatch)
	if len(fs) != 1 || fs[0].Classification != "profile_violation" || len(fs[0].Evidence) != 2 || fs[0].Evidence[1].Field != "EVN-1" {
		t.Fatalf("intra-occurrence disagreement not reported: %+v", r)
	}
	if !strings.Contains(fs[0].Summary, "not which value is correct") || strings.Contains(fs[0].Summary, "A02") {
		t.Fatalf("mismatch overclaimed or disclosed a value: %q", fs[0].Summary)
	}
	if r = run(t, writeCase(t, adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")), diagnose.LifecycleConfig()); len(findings(r, diagnose.EventTypeMismatch)) != 0 {
		t.Fatalf("agreeing declarations reported: %+v", r)
	}
	// An absent declaration belongs to the required-field rule, not this one.
	absent := bytes.Replace(adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001"), []byte("EVN|A01|"), []byte("EVN||"), 1)
	r = run(t, writeCase(t, absent), diagnose.LifecycleConfig())
	if len(findings(r, diagnose.EventTypeMismatch)) != 0 || len(findings(r, diagnose.LifecycleRequiredField)) != 1 {
		t.Fatalf("absent event type misclassified: %+v", r)
	}
	// SIU appointment occurrences declare no event type and are never compared.
	if r = run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), diagnose.LifecycleConfig()); len(findings(r, diagnose.EventTypeMismatch)) != 0 {
		t.Fatalf("appointment occurrence compared against an absent declaration: %+v", r)
	}
}

func TestLifecycleRequiredFieldsKeepNullEmptyAndOmittedDistinct(t *testing.T) {
	base := adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")
	for _, tc := range []struct {
		name, field string
		raw         []byte
		state       hl7.State
	}{
		{"null patient class", "PV1-2", bytes.Replace(base, []byte("PV1|1|I|"), []byte(`PV1|1|""|`), 1), hl7.Null},
		{"empty patient class", "PV1-2", bytes.Replace(base, []byte("PV1|1|I|"), []byte("PV1|1||"), 1), hl7.Empty},
		{"omitted visit number", "PV1-19.1", bytes.Replace(base, []byte("||||||||||||||||VISIT-001^^^READMIT"), nil, 1), hl7.Omitted},
		{"null merge identifier", "MRG-1.1", bytes.Replace(fixture(t, "diagnose-merge.hl7"), []byte("MRG|PATIENT-000^^^READMIT"), []byte(`MRG|""`), 1), hl7.Null},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, tc.raw), diagnose.LifecycleConfig())
			found := false
			for _, f := range findings(r, diagnose.LifecycleRequiredField) {
				if f.Evidence[0].Field != tc.field {
					continue
				}
				found = true
				if f.Classification != "profile_violation" || f.Profile != diagnose.LifecycleProfile || !strings.Contains(f.Summary, diagnose.LifecycleProfile) || f.Evidence[0].State != tc.state || f.Window == "" {
					t.Fatalf("state, profile or window missing: %+v", f)
				}
			}
			if !found {
				t.Fatalf("required field not reported: %+v", r)
			}
		})
	}
}

func TestLifecycleUnsupportedEvidenceIsNeverInterpreted(t *testing.T) {
	admit := adt(t, "A01", "DIAGNOSE-ADMIT", "VISIT-001")
	for _, tc := range []struct {
		name, code, rule string
		raw              []byte
	}{
		{"repeated visit number", "unsupported_field_repetition", diagnose.VisitNotObserved, bytes.ReplaceAll(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("VISIT-001^^^READMIT"), []byte("VISIT-001^^^READMIT~SECRET-OTHER^^^READMIT"))},
		{"repeated merge identifier", "unsupported_field_repetition", diagnose.MergeIdentifierNotObserved, bytes.ReplaceAll(fixture(t, "diagnose-merge.hl7"), []byte("MRG|PATIENT-000^^^READMIT"), []byte("MRG|PATIENT-000^^^READMIT~SECRET-OTHER^^^READMIT"))},
		{"unconfigured authority", "unconfigured_assigning_authority", diagnose.VisitNotObserved, bytes.ReplaceAll(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("VISIT-001^^^READMIT"), []byte("VISIT-001^^^SECRET-UNCONFIGURED"))},
		{"explicit null authority", "unknown_assigning_authority", diagnose.VisitNotObserved, bytes.ReplaceAll(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("VISIT-001^^^READMIT"), []byte(`VISIT-001^^^""`))},
		{"repeated visit segment", "unsupported_segment_cardinality", diagnose.VisitNotObserved, append(bytes.Clone(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001")), "PV1|1|I|WARD^ROOM^BED||||||||||||||||SECRET-VISIT^^^READMIT\n"...)},
		{"unsupported trigger", "unsupported_message_type", diagnose.VisitNotObserved, adt(t, "A31", "DIAGNOSE-UPDATE", "VISIT-001")},
		{"unsupported version", "unsupported_hl7_version", diagnose.VisitNotObserved, bytes.Replace(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("2.5.1"), []byte("2.4"), 1)},
		{"undeclared non-ASCII", "unsupported_field_encoding", diagnose.VisitNotObserved, bytes.ReplaceAll(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("VISIT-001"), []byte("VISIT-é"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, admit, tc.raw), diagnose.LifecycleConfig())
			if len(findings(r, tc.rule)) != 0 {
				t.Fatalf("unsupported evidence interpreted: %+v", r)
			}
			if !contains(codes(r), tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r.Unsupported)
			}
			data, err := diagnose.JSON(r)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("SECRET")) || bytes.Contains(diagnose.Markdown(r), []byte("SECRET")) {
				t.Fatal("unsupported evidence disclosed")
			}
		})
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestEveryLifecycleRuleIsSelectableOnItsOwnAndReproducible(t *testing.T) {
	payloads := [][]byte{
		bytes.Replace(adt(t, "A03", "DIAGNOSE-DISCHARGE", "VISIT-001"), []byte("EVN|A03"), []byte("EVN|A01"), 1),
		bytes.Replace(fixture(t, "diagnose-merge.hl7"), []byte("PID|1||PATIENT-001^^^READMIT"), []byte("PID|1||"), 1),
		siu(t, "S15", "DIAGNOSE-CANCEL"),
	}
	expected := map[string]int{diagnose.VisitNotObserved: 1, diagnose.MergeIdentifierNotObserved: 1, diagnose.AppointmentNotObserved: 1, diagnose.EventTypeMismatch: 1, diagnose.LifecycleRequiredField: 2}
	path := writeCase(t, payloads...)
	for rule, want := range expected {
		config := diagnose.LifecycleConfig()
		config.Rules = []string{rule}
		r := run(t, path, config)
		if len(r.Rules) != 1 || r.Rules[0] != rule {
			t.Fatalf("selected rules not reported: %+v", r.Rules)
		}
		if len(findings(r, rule)) != want || len(r.Findings) != want {
			t.Fatalf("rule %s is not independent: %+v", rule, r.Findings)
		}
	}
	first, err := diagnose.JSON(run(t, path, diagnose.LifecycleConfig()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := diagnose.JSON(run(t, writeCase(t, payloads...), diagnose.LifecycleConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical evidence and configuration produced different findings")
	}
	total := 0
	for _, want := range expected {
		total += want
	}
	report := run(t, path, diagnose.LifecycleConfig())
	if len(report.Findings) != total {
		t.Fatalf("combined selection changed the findings: %+v", report.Findings)
	}
	for _, f := range report.Findings {
		if f.Window == "" {
			t.Fatalf("finding omits the observed capture window: %+v", f)
		}
	}
}

func TestRulesOfOneContractAreNeverEvaluatedUnderAnother(t *testing.T) {
	config := diagnose.DefaultConfig()
	config.Rules = append(config.Rules, diagnose.VisitNotObserved)
	r := run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), config)
	if !contains(codes(r), "unsupported_rule") || contains(r.Rules, diagnose.VisitNotObserved) {
		t.Fatalf("a lifecycle rule was accepted by the SIU ruleset: %+v", r)
	}
	config = diagnose.LifecycleConfig()
	config.Rules = append(config.Rules, diagnose.BookingNotObserved)
	r = run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), config)
	if !contains(codes(r), "unsupported_rule") || contains(r.Rules, diagnose.BookingNotObserved) {
		t.Fatalf("an SIU rule was accepted by the lifecycle ruleset: %+v", r)
	}
	// An unnamed ruleset gives no rule identifier a meaning.
	config = diagnose.LifecycleConfig()
	config.Ruleset = "future-ruleset"
	r = run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), config)
	if len(r.Rules) != 0 || len(r.Findings) != 0 || len(r.Unsupported) != 1 || r.Unsupported[0].Code != "unsupported_ruleset" {
		t.Fatalf("unnamed ruleset silently evaluated: %+v", r)
	}
}

func TestGeneratedBundlesAreOnlyInterpretedByTheirOwnProfile(t *testing.T) {
	path := writeInputs(t, []bundle.Input{{Data: fixture(t, "diagnose-reschedule.hl7")}, {Data: fixture(t, "diagnose-reschedule.hl7")}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 0, BaseTime: *timePtr("2026-01-01T00:00:00Z"), GeneratorVersion: "synthetic-v1", ProfileVersion: diagnose.Profile}})
	r := run(t, path, diagnose.LifecycleConfig())
	if len(r.Unsupported) == 0 || r.Unsupported[0].Code != "unsupported_bundle_profile" || !strings.Contains(r.Unsupported[0].Detail, "lifecycle profile rules") {
		t.Fatalf("generator profile silently reinterpreted: %+v", r.Unsupported)
	}
	if len(findings(r, diagnose.AppointmentNotObserved)) != 0 || len(findings(r, diagnose.LifecycleRequiredField)) != 0 {
		t.Fatalf("lifecycle profile rules ran over another profile's bundle: %+v", r.Findings)
	}
	if len(findings(r, diagnose.DuplicateControl)) != 1 {
		t.Fatalf("message rules depend on a profile: %+v", r.Findings)
	}
}

func TestFurtherPatientIdentifierRepetitionsAreRecordedAsUncompared(t *testing.T) {
	repeated := bytes.ReplaceAll(adt(t, "A01", "DIAGNOSE-PRIOR", "VISIT-009"), []byte("PATIENT-001^^^READMIT"), []byte("PATIENT-001^^^READMIT~PATIENT-000^^^READMIT"))
	r := run(t, writeCase(t, repeated, fixture(t, "diagnose-merge.hl7")), diagnose.LifecycleConfig())
	if !contains(codes(r), "partial_identifier_repetition") {
		t.Fatalf("uncompared identifier repetitions left implied: %+v", r.Unsupported)
	}
	if len(findings(r, diagnose.MergeIdentifierNotObserved)) != 1 {
		t.Fatalf("a later repetition was silently correlated: %+v", r.Findings)
	}
	if !strings.Contains(findings(r, diagnose.MergeIdentifierNotObserved)[0].Summary, "first patient identifier repetition") {
		t.Fatal("the hypothesis does not name its first-repetition assumption")
	}
	// A single identifier repetition is complete coverage and says nothing.
	r = run(t, writeCase(t, fixture(t, "diagnose-merge.hl7")), diagnose.LifecycleConfig())
	if contains(codes(r), "partial_identifier_repetition") {
		t.Fatalf("complete coverage reported as partial: %+v", r.Unsupported)
	}
}

func TestMessageRulesDecodeACKsUnderTheLifecycleRulesetToo(t *testing.T) {
	r := run(t, writeCase(t, fixture(t, "diagnose-admit.hl7"), fixture(t, "diagnose-ack.hl7")), diagnose.LifecycleConfig())
	for _, rule := range []string{diagnose.ACKOutcome, diagnose.ACKError} {
		fs := findings(r, rule)
		if len(fs) != 1 || fs[0].Classification != "observed_fact" || fs[0].Ruleset != diagnose.LifecycleRuleset || fs[0].Profile != diagnose.LifecycleProfile {
			t.Fatalf("missing acknowledgement fact %s: %+v", rule, r)
		}
	}
	if !strings.Contains(findings(r, diagnose.ACKOutcome)[0].Summary, "AE (application error)") || !strings.Contains(findings(r, diagnose.ACKError)[0].Summary, "101 (required field missing)") {
		t.Fatalf("acknowledgement not decoded: %+v", r)
	}
	data, err := diagnose.JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET-", "DIAGNOSE-ADMIT", "PATIENT-001", "VISIT-001", "SYNTHETIC-FIXTURE"} {
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(diagnose.Markdown(r), []byte(secret)) {
			t.Fatalf("lifecycle report disclosed %s", secret)
		}
	}
	config := diagnose.LifecycleConfig()
	config.Rules = []string{diagnose.DuplicateControl}
	r = run(t, writeCase(t, fixture(t, "diagnose-admit.hl7"), fixture(t, "diagnose-admit.hl7")), config)
	if len(findings(r, diagnose.DuplicateControl)) != 1 || len(r.Findings) != 1 {
		t.Fatalf("message rules are not selectable alone: %+v", r.Findings)
	}
}

// The diagnosis contract reads captured evidence and the scenario contract
// decides authored steps, but both name the same trigger events. This keeps the
// diagnosis profile from inventing a trigger the authoring lifecycle does not
// declare, without either contract consulting the other's transitions.
func TestLifecycleTriggersAreTheEventsTheAuthoringProfilesDeclare(t *testing.T) {
	var document struct {
		Triggers []struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"triggers"`
	}
	raw, err := os.ReadFile("lifecycle-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Triggers) == 0 {
		t.Fatal("the lifecycle profile declares no trigger")
	}
	for _, trigger := range document.Triggers {
		t.Run(trigger.Kind+" "+trigger.Code, func(t *testing.T) {
			designed := scenario.Scenario{
				Profile:  scenario.ADTLifecycle,
				BaseTime: *timePtr("2026-01-01T00:00:00Z"),
				Subjects: []scenario.Subject{{ID: "p", Kind: scenario.PatientSubject, Namespace: "READMIT", Identifier: "PATIENT-001", InitialState: scenario.PatientActive}},
				Steps:    []scenario.Step{{ID: "s1", Event: scenario.Event(trigger.Code), Subject: "s", After: "0s", Expect: scenario.Accepted}},
			}
			subject := scenario.Subject{ID: "s", Kind: scenario.VisitSubject, Namespace: "READMIT", Identifier: "VISIT-001", Patient: "p", InitialState: scenario.VisitNone}
			switch {
			case trigger.Kind == "SIU":
				designed.Profile = scenario.SIULifecycle
				subject.Kind, subject.Identifier, subject.InitialState = scenario.AppointmentSubject, "FILLER-001", scenario.AppointmentNone
			case trigger.Code == "A40":
				designed.Steps[0].Subject, designed.Steps[0].Into = "p", "s"
				subject.Kind, subject.Identifier, subject.InitialState = scenario.PatientSubject, "PATIENT-002", scenario.PatientActive
				subject.Patient = ""
			}
			designed.Subjects = append(designed.Subjects, subject)
			// Whether the lifecycle takes this step is the scenario contract's
			// question; only the vocabulary is shared.
			if _, err := scenario.Preview(designed); err != nil && strings.Contains(err.Error(), "declares no event") {
				t.Fatalf("the diagnosis profile names a trigger no authoring profile declares: %v", err)
			}
		})
	}
}
