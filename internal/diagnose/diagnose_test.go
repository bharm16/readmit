package diagnose_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/hl7"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeCase(t *testing.T, payloads ...[]byte) string {
	t.Helper()
	inputs := make([]bundle.Input, len(payloads))
	for i, raw := range payloads {
		inputs[i] = bundle.Input{Path: "SYNTHETIC-FIXTURE", Data: raw}
	}
	return writeInputs(t, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: timePtr("2026-01-10T10:00:00Z")})
}

func writeInputs(t *testing.T, inputs []bundle.Input, p bundle.Provenance) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	if _, err := bundle.Write(path, inputs, p); err != nil {
		t.Fatal(err)
	}
	return path
}

func timePtr(text string) *time.Time {
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		panic(err)
	}
	return &v
}

func run(t *testing.T, path string, c diagnose.Config) diagnose.Report {
	t.Helper()
	r, err := diagnose.Run(path, c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func findings(r diagnose.Report, rule string) []diagnose.Finding {
	var results []diagnose.Finding
	for _, f := range r.Findings {
		if f.RuleID == rule {
			results = append(results, f)
		}
	}
	return results
}

func TestPartialCaptureStatesOnlyHypothesisAndExplicitObservedWindow(t *testing.T) {
	path := writeInputs(t, []bundle.Input{{Path: "SYNTHETIC", Data: fixture(t, "diagnose-reschedule.hl7"), Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: timePtr("2026-01-04T00:00:00Z")}}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: timePtr("2026-01-10T00:00:00Z")})
	r := run(t, path, diagnose.DefaultConfig())
	fs := findings(r, diagnose.BookingNotObserved)
	if len(fs) != 1 || fs[0].Classification != "hypothesis" || fs[0].Window != r.Window.Description || !strings.Contains(fs[0].Summary, "observed case window") || strings.Contains(fs[0].Summary, "never booked") {
		t.Fatalf("unbounded hypothesis: %+v", r)
	}
	if r.Window.UnknownObservedTimes != 0 || !r.Window.ObservedStart.Equal(*timePtr("2026-01-04T00:00:00Z")) || !r.Window.ObservedEnd.Equal(*r.Window.ObservedStart) {
		t.Fatalf("observed/declared/import times collapsed: %+v", r.Window)
	}
	if fs[0].Profile != diagnose.Profile || len(fs[0].Evidence) != 5 || fs[0].Evidence[1].Occurrence != "s0001-e000001" || fs[0].Evidence[1].Field != "SCH-2.1" {
		t.Fatal("missing hypothesis evidence/profile")
	}
}

func TestSameIDCorrelatesOnlyWithinConfiguredNamespace(t *testing.T) {
	booking := fixture(t, "diagnose-booking.hl7")
	reschedule := fixture(t, "diagnose-reschedule.hl7")
	r := run(t, writeCase(t, booking, reschedule), diagnose.DefaultConfig())
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 || r.NoFindings == "" {
		t.Fatalf("same namespace did not correlate: %+v", r)
	}
	config := diagnose.DefaultConfig()
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER", Namespace: "OTHER"})
	cancel := fixture(t, "diagnose-cancel-other-authority.hl7")
	r = run(t, writeCase(t, booking, cancel), config)
	if len(findings(r, diagnose.BookingNotObserved)) != 1 || len(r.Unsupported) != 0 {
		t.Fatalf("distinct namespaces correlated: %+v", r)
	}
	config.Namespaces[1].Key = "READMIT" // Explicitly configured alias, not inferred equality.
	r = run(t, writeCase(t, booking, cancel), config)
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 {
		t.Fatalf("configured aliases did not correlate: %+v", r)
	}
}

func TestUniversalAuthorityTupleAndEscapedIdentifiersArePreserved(t *testing.T) {
	booking := bytes.ReplaceAll(fixture(t, "diagnose-booking.hl7"), []byte("FILLER-001^READMIT"), []byte("FILLER\\F\\001^READMIT^1.2.3^ISO"))
	reschedule := bytes.ReplaceAll(fixture(t, "diagnose-reschedule.hl7"), []byte("FILLER-001^READMIT"), []byte("FILLER\\X7c\\001^READMIT^1.2.3^ISO"))
	config := diagnose.DefaultConfig()
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "LOCAL-OID", Namespace: "READMIT", UniversalID: "1.2.3", UniversalIDType: "ISO"})
	r := run(t, writeCase(t, booking, reschedule), config)
	if len(r.Findings) != 0 || len(r.Unsupported) != 0 {
		t.Fatalf("decoded namespaced ID not matched: %+v", r)
	}
	reschedule = bytes.ReplaceAll(reschedule, []byte("1.2.3"), []byte("1.2.4"))
	config.Namespaces = append(config.Namespaces, diagnose.Namespace{Key: "OTHER-OID", Namespace: "READMIT", UniversalID: "1.2.4", UniversalIDType: "ISO"})
	r = run(t, writeCase(t, booking, reschedule), config)
	if len(findings(r, diagnose.BookingNotObserved)) != 1 {
		t.Fatal("same namespace with distinct configured universal IDs was conflated")
	}
}

func TestDuplicateControlsAndDecodedACKOutcomesAreObservedFacts(t *testing.T) {
	booking := fixture(t, "diagnose-booking.hl7")
	r := run(t, writeCase(t, booking, booking, fixture(t, "diagnose-ack.hl7")), diagnose.DefaultConfig())
	for _, rule := range []string{diagnose.DuplicateControl, diagnose.ACKOutcome, diagnose.ACKError} {
		fs := findings(r, rule)
		if len(fs) != 1 || fs[0].Classification != "observed_fact" || fs[0].Ruleset != diagnose.Ruleset || len(fs[0].Evidence) < 2 {
			t.Fatalf("missing fact %s: %+v", rule, r)
		}
	}
	if !strings.Contains(findings(r, diagnose.ACKOutcome)[0].Summary, "AE (application error)") || !strings.Contains(findings(r, diagnose.ACKError)[0].Summary, "101 (required field missing)") {
		t.Fatalf("ACK not decoded: %+v", r)
	}
	data, err := diagnose.JSON(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET-", "DIAGNOSE-BOOK", "FILLER-001", "PATIENT-001", "SYNTHETIC-FIXTURE"} {
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(diagnose.Markdown(r), []byte(secret)) {
			t.Fatalf("report disclosed %s", secret)
		}
	}
}

func TestRequiredFieldsKeepNullEmptyOmittedDistinctAndNameProfile(t *testing.T) {
	for _, tc := range []struct {
		file  []byte
		state hl7.State
	}{
		{fixture(t, "diagnose-missing-patient.hl7"), hl7.Null},
		{bytes.ReplaceAll(fixture(t, "diagnose-missing-patient.hl7"), []byte(`PID|1||""|`), []byte("PID|1|||")), hl7.Empty},
		{[]byte("MSH|^~\\&|A|B|C|D|20260101120000||SIU^S12|CONTROL|P|2.5.1\nSCH|P^READMIT|F^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000\n"), hl7.Omitted},
	} {
		r := run(t, writeCase(t, tc.file), diagnose.DefaultConfig())
		found := false
		for _, f := range findings(r, diagnose.RequiredField) {
			if f.Evidence[0].Field == "PID-3.1" {
				found = true
				if f.Classification != "profile_violation" || f.Profile != diagnose.Profile || !strings.Contains(f.Summary, diagnose.Profile) || f.Evidence[0].State != tc.state {
					t.Fatalf("state/profile missing: %+v", f)
				}
			}
		}
		if !found {
			t.Fatalf("missing required field not found: %+v", r)
		}
	}
}

func TestUnknownAuthoritiesAndEncodingsDoNotCreateFalseCorrelationOrHypothesis(t *testing.T) {
	for _, filler := range []string{"FILLER-001^UNCONFIGURED", "FILLER-001", `FILLER-001^""`, `FILLER\ZSECRET\^READMIT`, "FILLER-\xff^READMIT"} {
		payload := bytes.ReplaceAll(fixture(t, "diagnose-reschedule.hl7"), []byte("FILLER-001^READMIT"), []byte(filler))
		r := run(t, writeCase(t, fixture(t, "diagnose-booking.hl7"), payload), diagnose.DefaultConfig())
		if len(r.Unsupported) == 0 || len(findings(r, diagnose.BookingNotObserved)) != 0 {
			t.Fatalf("unknown namespace/encoding treated as a known key: %+v", r)
		}
		data, _ := diagnose.JSON(r)
		if bytes.Contains(data, []byte("SECRET")) || bytes.Contains(data, []byte("UNCONFIGURED")) {
			t.Fatal("unsupported field leaked")
		}
	}
}

func TestUnsupportedMessagesVersionsProfilesAndRulesAreVisible(t *testing.T) {
	config := diagnose.DefaultConfig()
	config.Rules = append(config.Rules, "future.rule")
	r := run(t, writeCase(t, fixture(t, "adt-cr.hl7"), bytes.ReplaceAll(fixture(t, "diagnose-booking.hl7"), []byte("2.5.1"), []byte("2.4")), []byte("not a message")), config)
	codes := []string{}
	for _, u := range r.Unsupported {
		codes = append(codes, u.Code)
	}
	for _, want := range []string{"unsupported_rule", "unsupported_message_type", "unsupported_hl7_version", "unparsed_occurrence"} {
		if !slices.Contains(codes, want) {
			t.Fatalf("missing %s: %+v", want, r)
		}
	}
	config.Profile = "future-profile"
	r = run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), config)
	if len(r.Rules) != 0 || len(r.Findings) != 0 || r.Unsupported[1].Code != "unsupported_profile" {
		t.Fatalf("unsupported configured profile silently evaluated: %+v", r)
	}
	config = diagnose.DefaultConfig()
	config.Ruleset = "future-ruleset"
	r = run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), config)
	if len(r.Rules) != 0 || r.Unsupported[0].Code != "unsupported_ruleset" {
		t.Fatalf("unsupported ruleset silently evaluated: %+v", r)
	}
	path := writeInputs(t, []bundle.Input{{Data: fixture(t, "diagnose-reschedule.hl7")}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 0, BaseTime: *timePtr("2026-01-01T00:00:00Z"), GeneratorVersion: "synthetic-v1", ProfileVersion: "future-profile"}})
	r = run(t, path, diagnose.DefaultConfig())
	if r.Unsupported[0].Code != "unsupported_bundle_profile" || len(findings(r, diagnose.BookingNotObserved)) != 0 {
		t.Fatal("unsupported bundle profile silently evaluated")
	}
}

func TestNoFindingsAndRenderingsShareExactReportModel(t *testing.T) {
	r := run(t, writeCase(t, fixture(t, "diagnose-booking.hl7")), diagnose.DefaultConfig())
	if len(r.Findings) != 0 || r.Window.UnknownObservedTimes != 1 || r.Window.ObservedStart != nil || !strings.Contains(r.NoFindings, "not proof of correctness") || !strings.Contains(r.NoFindings, diagnose.Ruleset) || !strings.Contains(r.NoFindings, r.Window.Description) {
		t.Fatalf("overclaimed no-findings report: %+v", r)
	}
	for _, path := range []string{writeCase(t, fixture(t, "diagnose-reschedule.hl7")), writeCase(t, fixture(t, "diagnose-ack.hl7")), writeCase(t, fixture(t, "diagnose-missing-patient.hl7"))} {
		r = run(t, path, diagnose.DefaultConfig())
		data, err := diagnose.JSON(r)
		if err != nil {
			t.Fatal(err)
		}
		var decoded diagnose.Report
		if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
			t.Fatal(err)
		}
		md := string(diagnose.Markdown(r))
		for _, f := range decoded.Findings {
			for _, part := range []string{f.ID, f.RuleID, f.Classification, f.Profile, f.Ruleset, f.Summary, f.Window} {
				if !strings.Contains(md, part) {
					t.Fatalf("Markdown lacks JSON finding part %q", part)
				}
			}
			for _, ref := range f.Evidence {
				if !strings.Contains(md, ref.Occurrence) || !strings.Contains(md, ref.Field) || !strings.Contains(md, string(ref.State)) {
					t.Fatal("Markdown lacks JSON evidence")
				}
			}
		}
	}
}

func TestReportRefusesTamperedEvidence(t *testing.T) {
	path := writeCase(t, fixture(t, "diagnose-booking.hl7"))
	if err := os.WriteFile(filepath.Join(path, "payloads", "s0001-e000001.bin"), []byte("SECRET-TAMPERED"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := diagnose.Run(path, diagnose.DefaultConfig()); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("tampered evidence accepted or disclosed")
	}
}

func TestConfigIsStrictAndRejectsAmbiguousNamespaceMappings(t *testing.T) {
	valid, _ := json.Marshal(diagnose.DefaultConfig())
	if _, err := diagnose.ParseConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{[]byte(`{"SECRET":"value"}`), append(valid[:len(valid)-1], []byte(`,"secret":"SECRET"}`)...), []byte(`{"schema":"readmit-diagnose-config/v1","schema":"readmit-diagnose-config/v1"}`)} {
		if _, err := diagnose.ParseConfig(data); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("invalid config accepted or disclosed")
		}
	}
	for _, change := range []func(*diagnose.Config){func(c *diagnose.Config) { c.Namespaces = append(c.Namespaces, c.Namespaces[0]) }, func(c *diagnose.Config) { c.Rules = append(c.Rules, c.Rules[0]) }, func(c *diagnose.Config) { c.Namespaces[0].UniversalID = "1.2" }, func(c *diagnose.Config) { c.Profile = "" }} {
		c := diagnose.DefaultConfig()
		change(&c)
		data, _ := json.Marshal(c)
		if _, err := diagnose.ParseConfig(data); err == nil {
			t.Fatal("ambiguous config accepted")
		}
	}
}

func TestUnsupportedRepetitionEncodingAndERRTableAreNotMisinterpreted(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		rule string
		code string
	}{
		{"control repetition", bytes.ReplaceAll(fixture(t, "diagnose-booking.hl7"), []byte("|DIAGNOSE-BOOK|"), []byte("|DIAGNOSE-BOOK~EXTRA|")), diagnose.DuplicateControl, "unsupported_field_repetition"},
		{"filler repetition", bytes.ReplaceAll(fixture(t, "diagnose-reschedule.hl7"), []byte("FILLER-001^READMIT"), []byte("FILLER-001^READMIT~OTHER^READMIT")), diagnose.BookingNotObserved, "unsupported_field_repetition"},
		{"unknown ERR table", bytes.ReplaceAll(fixture(t, "diagnose-ack.hl7"), []byte("HL70357"), []byte("SECRET-CUSTOM")), diagnose.ACKError, "unsupported_err_coding_system"},
		{"non-ASCII without declaration", bytes.ReplaceAll(fixture(t, "diagnose-reschedule.hl7"), []byte("FILLER-001"), []byte("FILLER-é")), diagnose.BookingNotObserved, "unsupported_field_encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, writeCase(t, fixture(t, "diagnose-booking.hl7"), tc.raw), diagnose.DefaultConfig())
			if len(findings(r, tc.rule)) != 0 {
				t.Fatalf("unsupported content interpreted: %+v", r)
			}
			found := false
			for _, u := range r.Unsupported {
				if u.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing unsupported explanation: %+v", r)
			}
		})
	}
}

func TestACKDecoderHasExplicitBoundForRepeatedSegments(t *testing.T) {
	raw := []byte("MSH|^~\\&|A|B|C|D|20260101120000||ACK|CONTROL|P|2.5.1\n" + strings.Repeat("MSA|AA|BOOKING\n", 129))
	r := run(t, writeCase(t, raw), diagnose.DefaultConfig())
	if len(r.Findings) != 0 || len(r.Unsupported) != 1 || r.Unsupported[0].Code != "unsupported_ack_cardinality" {
		t.Fatalf("ACK resource bound silent: %+v", r)
	}
}

func TestImportedWireProfileDeclarationsAreUnsupportedByRepetition(t *testing.T) {
	declared := fixture(t, "diagnose-unsupported-profile.hl7")
	for _, tc := range []struct {
		name, decl string
		fields     []string
	}{
		{"single", "SECRET-PROFILE^SECRET-VENDOR", []string{"MSH-21[1]"}},
		{"repeated", "SECRET-FIRST^VENDOR~SECRET-SECOND^OTHER", []string{"MSH-21[1]", "MSH-21[2]"}},
		{"empty then declared", "~SECRET-SECOND^OTHER", []string{"MSH-21[2]"}},
		{"explicit null", `""`, []string{"MSH-21[1]"}},
		{"local spelling is not a wire mapping", "readmit-siu-v1^READMIT", []string{"MSH-21[1]"}},
		{"empty", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := bytes.Replace(declared, []byte("SECRET-PROFILE^SECRET-VENDOR"), []byte(tc.decl), 1)
			r := run(t, writeCase(t, raw), diagnose.DefaultConfig())
			if len(r.Findings) != 0 || len(r.Unsupported) != len(tc.fields) {
				t.Fatalf("wire declaration evaluated or missed: %+v", r)
			}
			for i, field := range tc.fields {
				if r.Unsupported[i].Code != "unsupported_message_profile" || r.Unsupported[i].Occurrence != "s0001-e000001" || r.Unsupported[i].Field != field {
					t.Fatalf("wire declaration evidence missing: %+v", r.Unsupported)
				}
			}
			data, _ := diagnose.JSON(r)
			markdown := diagnose.Markdown(r)
			if bytes.Contains(data, []byte("SECRET")) || bytes.Contains(markdown, []byte("SECRET")) {
				t.Fatal("wire profile values disclosed")
			}
		})
	}
	// Unsupported declarations are listed even when another metadata check also
	// makes the occurrence uninterpretable, and cannot generate SIU hypotheses.
	reschedule := bytes.Replace(declared, []byte("SIU^S12"), []byte("SIU^S13"), 1)
	r := run(t, writeCase(t, reschedule), diagnose.DefaultConfig())
	if len(findings(r, diagnose.BookingNotObserved)) != 0 || len(r.Unsupported) != 1 {
		t.Fatalf("unsupported wire profile entered SIU rules: %+v", r)
	}
	otherVersion := bytes.Replace(declared, []byte("2.5.1"), []byte("2.4"), 1)
	r = run(t, writeCase(t, otherVersion), diagnose.DefaultConfig())
	if len(r.Unsupported) != 2 || r.Unsupported[0].Code != "unsupported_message_profile" {
		t.Fatal("wire declaration hidden by other unsupported metadata")
	}
}
