package redact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// deriveFixture assembles the verified inputs derive takes, from the shipped
// synthetic fixtures: the planted booking and reschedule case, the spec whose
// literals bind to it, the disclosure policy and the inventory of known
// residual values. Everything derive receives is in memory; only this
// fixture's own setup touches a temporary directory, the way the verified
// reader does.
func deriveFixture(t *testing.T) deriveInputs {
	t.Helper()
	root := t.TempDir()
	var inputs []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{Path: filepath.Join(root, name+".mllp"), Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	source, err := bundle.Write(filepath.Join(root, "original.case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
	if err != nil {
		t.Fatal(err)
	}
	policyRaw, err := os.ReadFile("../../testdata/fixtures/redact-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := DecodePolicy(policyRaw)
	if err != nil {
		t.Fatal(err)
	}
	inventoryRaw, err := os.ReadFile("../../testdata/fixtures/redact-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := DecodeInventory(inventoryRaw)
	if err != nil {
		t.Fatal(err)
	}
	return deriveInputs{
		Case:      source,
		CasePath:  filepath.Join(root, "original.case"),
		Spec:      deriveSpec(t),
		Policy:    policy,
		Inventory: inventory,
	}
}

func deriveSpec(t *testing.T) testrunner.Spec {
	t.Helper()
	spec, err := testrunner.ReadSpec("../../testdata/fixtures/redact-spec.json")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// deriveCase rewrites the planted case with every occurrence of from replaced
// by to, in every source, and returns the verified rewritten bundle.
func deriveCase(t *testing.T, from, to string) *bundle.Bundle {
	t.Helper()
	base := deriveFixture(t)
	var inputs []bundle.Input
	for _, source := range base.Case.Manifest.Sources {
		var raw []byte
		for _, event := range base.Case.Events {
			if event.SourceID != source.ID {
				continue
			}
			part, err := base.Case.Raw(event.ID)
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, part...)
		}
		inputs = append(inputs, bundle.Input{Path: "invented-source", Data: []byte(strings.ReplaceAll(string(raw), from, to)), Options: hl7.Options{Format: source.Format, Terminator: source.Terminator}})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	rewritten, err := bundle.Write(filepath.Join(t.TempDir(), "rewritten.case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
	if err != nil {
		t.Fatal(err)
	}
	return rewritten
}

// derivedBundle writes a derivation's occurrences back into a verified
// derived case, so its fields read the way any reader of the sealed case
// would read them.
func derivedBundle(t *testing.T, d derivation) *bundle.Bundle {
	t.Helper()
	derived, err := bundle.Write(filepath.Join(t.TempDir(), "derived.case"), d.Occurrences, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return derived
}

func derivedField(t *testing.T, b *bundle.Bundle, id, path string) string {
	t.Helper()
	raw, err := b.Raw(id)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	selector, err := hl7.ParseSelector(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := doc.Read(0, selector, hl7.IgnoreMSH18)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(value.Decoded)
}

func unresolved(findings []exportreview.Finding, locationPart string) bool {
	return slices.ContainsFunc(findings, func(finding exportreview.Finding) bool {
		return !finding.Resolved && strings.Contains(finding.Location, locationPart)
	})
}

// fakeProver is the prove step's test adapter: it records the one trial it is
// asked and answers with what the test staged. Nothing is swapped at package
// level; the wiring under test receives it like production receives
// [FixtureProver].
type fakeProver struct {
	called        bool
	spec          testrunner.Spec
	casePath, dir string
	required      []int
	proof         Proof
	err           error
}

func (f *fakeProver) Prove(ctx context.Context, spec testrunner.Spec, casePath, dir string, required []int) (Proof, error) {
	f.called = true
	f.spec, f.casePath, f.dir, f.required = spec, casePath, dir, required
	return f.proof, f.err
}

// Every unreviewed surface stays an unresolved finding located at the exact
// surface, so a blocked review says what to handle and nothing else. The
// inventory's original artifacts are verified facts here, so the whole table
// runs in memory.
func TestDeriveLocatesEveryUnreviewedSurface(t *testing.T) {
	for _, test := range []struct {
		name  string
		edit  func(policy *Policy)
		facts []artifactFact
		want  string
	}{
		{
			name: "pid",
			edit: func(policy *Policy) { policy.Fields = deleteRule(policy.Fields, "PID-3.1") },
			want: "PID[1]-3",
		},
		{
			name: "nte",
			edit: func(policy *Policy) { policy.Fields = deleteRule(policy.Fields, "NTE-3") },
			want: "NTE[1]-3",
		},
		{
			name: "err",
			edit: func(policy *Policy) {
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "ERR" })
			},
			want: "ERR[1]-1",
		},
		{
			name: "unknown",
			edit: func(policy *Policy) {
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "ZXX" })
			},
			want: "ZXX[1]",
		},
		{
			name: "embedded",
			edit: func(policy *Policy) {
				policy.RemoveSegments = slices.DeleteFunc(policy.RemoveSegments, func(s string) bool { return s == "OBX" })
			},
			want: "OBX[1]-5",
		},
		{
			name: "filename",
			edit: func(policy *Policy) { policy.PacketPolicies = dropPolicy(policy.PacketPolicies, Filenames) },
			want: "/path",
		},
		{
			name: "metadata",
			edit: func(policy *Policy) { policy.PacketPolicies = dropPolicy(policy.PacketPolicies, Metadata) },
			want: "provenance-and-observed-times",
		},
		{
			name: "spec",
			edit: func(policy *Policy) { policy.SpecBindings = nil },
			want: "spec/assertions/2/expected/records/1/patient_id/value",
		},
		{
			name: "diagnosis",
			edit: func(policy *Policy) { policy.PacketPolicies = dropPolicy(policy.PacketPolicies, Diagnosis) },
			facts: []artifactFact{
				{reference: sourceReference{Kind: "run", Path: "invented/run", Identity: "invented-run"}, run: &replay.Run{}},
				{reference: sourceReference{Kind: "diagnosis-json", Path: "invented/diagnosis", Identity: "invented-diagnosis"}, report: &diagnose.Report{Findings: []diagnose.Finding{{ID: "f-planted"}}}},
			},
			want: "original-artifacts/a0002/findings/",
		},
		{
			name: "run",
			edit: func(policy *Policy) { policy.PacketPolicies = dropPolicy(policy.PacketPolicies, Rerun) },
			facts: []artifactFact{
				{reference: sourceReference{Kind: "run", Path: "invented/run", Identity: "invented-run"}, run: &replay.Run{Manifest: replay.Manifest{Changes: []replay.Change{{Transformation: "rebase-control-ids", Old: []byte("OLD"), New: []byte("READMIT000001")}}}}},
			},
			want: "original-artifacts/a0001/manifest/changes/1/new",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := deriveFixture(t)
			test.edit(&inputs.Policy)
			inputs.Artifacts = test.facts
			derived, err := derive(inputs)
			if err != nil {
				t.Fatal(err)
			}
			if !unresolved(derived.Findings, test.want) {
				t.Fatalf("missing located finding for %s: %+v", test.want, derived.Findings)
			}
		})
	}
}

func deleteRule(rules []FieldRule, selector string) []FieldRule {
	return slices.DeleteFunc(rules, func(rule FieldRule) bool { return rule.Selector == selector })
}

func dropPolicy(policies []string, name string) []string {
	return slices.DeleteFunc(policies, func(policy string) bool { return policy == name })
}

// createRequest stages the same files Create reads, beside its two output
// directories, from the shipped synthetic fixtures.
func createRequest(t *testing.T) Request {
	t.Helper()
	dir := t.TempDir()
	request := Request{
		CasePath: filepath.Join(dir, "original.case"), SpecPath: filepath.Join(dir, "spec.json"),
		PolicyPath: filepath.Join(dir, "policy.json"), InventoryPath: filepath.Join(dir, "inventory.json"),
		Output: filepath.Join(dir, "review"), LocalState: filepath.Join(dir, "private"),
	}
	var inputs []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{Path: filepath.Join(dir, "PLANTED-FILENAME-CEDAR-"+name+".mllp"), Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(request.CasePath, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"spec": request.SpecPath, "policy": request.PolicyPath, "inventory": request.InventoryPath} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return request
}

// writeCanonical encodes a value the way every redaction document is written,
// so an edited policy is indistinguishable from an authored one.
func writeCanonical(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := encode(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// A populated field no rule claims is reported unresolved at its own position,
// whatever the rest of the policy handles.
func TestDeriveReportsEveryUnresolvedByteOfAForeignCase(t *testing.T) {
	inputs := deriveFixture(t)
	inputs.Policy.Fields = deleteRule(inputs.Policy.Fields, "PID-5")
	derived, err := derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !unresolved(derived.Findings, "PID[1]-5") {
		t.Fatal("an unmapped populated field was not reported unresolved")
	}
}

// A derived spec's expected literals carry the derived fields they bind to,
// and the binding findings say so. A literal that no longer matches its
// source field cannot be resolved and stays an explicit unresolved finding.
func TestDeriveBindsExpectedSpecLiteralsToTheirDerivedFields(t *testing.T) {
	inputs := deriveFixture(t)
	derived, err := derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	caseBundle := derivedBundle(t, derived)
	surrogate := derivedField(t, caseBundle, "s0002-e000001", "PID-3.1")
	record := &(*derived.Spec.Assertions[1].Expected.Records)[0]
	if record.PatientID.Value != surrogate || surrogate == "PLANTED-PATIENT-7391" {
		t.Fatalf("the expected patient literal was not bound to its derived field: %q vs %q", record.PatientID.Value, surrogate)
	}
	if record.PatientID.Namespace != "READMIT" {
		t.Fatalf("the expected namespace literal was not bound to its replacement: %q", record.PatientID.Namespace)
	}
	start := derivedField(t, caseBundle, "s0002-e000001", "SCH-11.4")
	if record.AppointmentStart != start {
		t.Fatalf("the expected appointment literal was not bound to its shifted field: %q vs %q", record.AppointmentStart, start)
	}
	if resolved := slices.ContainsFunc(derived.Findings, func(finding exportreview.Finding) bool {
		return finding.Resolved && finding.Location == "spec/assertions/2/expected/records/1/patient_id/value"
	}); !resolved {
		t.Fatal("the resolved literal binding was not recorded as resolved")
	}

	// A spec whose expected literal disagrees with its source field leaves the
	// binding unresolved instead of silently keeping the stale literal.
	unrelated := deriveFixture(t)
	spec := unrelated.Spec
	(*spec.Assertions[1].Expected.Records)[0].PatientID.Value = "UNRELATED-LITERAL"
	unrelated.Spec = spec
	derived, err = derive(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if !unresolved(derived.Findings, "spec/assertions/2/expected/records/1/patient_id/value") {
		t.Fatal("a mismatched literal was resolved")
	}
}

// The same patient's dates move by one offset, in every field and every
// occurrence, and that offset is private state rather than a second source of
// truth.
func TestDeriveShiftsEachPatientsDatesByOneConsistentOffset(t *testing.T) {
	inputs := deriveFixture(t)
	source := inputs.Case
	derived, err := derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.Local.Shifts) != 1 {
		t.Fatalf("one patient scope produced %d shifts", len(derived.Local.Shifts))
	}
	days := derived.Local.Shifts[0].Days
	if days == 0 || days < -365 || days > 364 {
		t.Fatalf("the declared testing shift is out of range: %d", days)
	}
	caseBundle := derivedBundle(t, derived)
	for _, occurrence := range []string{"s0001-e000001", "s0002-e000001"} {
		startOld, err := time.Parse("20060102150405-0700", derivedField(t, source, occurrence, "SCH-11.4"))
		if err != nil {
			t.Fatal(err)
		}
		startNew, err := time.Parse("20060102150405-0700", derivedField(t, caseBundle, occurrence, "SCH-11.4"))
		if err != nil {
			t.Fatal(err)
		}
		dobOld, err := time.Parse("20060102150405-0700", derivedField(t, source, occurrence, "PID-7"))
		if err != nil {
			t.Fatal(err)
		}
		dobNew, err := time.Parse("20060102150405-0700", derivedField(t, caseBundle, occurrence, "PID-7"))
		if err != nil {
			t.Fatal(err)
		}
		if int(startNew.Sub(startOld).Hours()/24) != days || int(dobNew.Sub(dobOld).Hours()/24) != days {
			t.Fatalf("%s did not move by the one recorded offset %d: start %v, dob %v", occurrence, days, startNew.Sub(startOld), dobNew.Sub(dobOld))
		}
	}
}

// The same decoded identifier under one authority maps to one surrogate, an
// escaped spelling of it maps to the same surrogate, and the same identifier
// under another authority maps to a different one.
func TestDerivePreservesScopedIDsAcrossEscapesWithoutMergingAuthorities(t *testing.T) {
	base := deriveFixture(t)
	var inputs []bundle.Input
	for _, authority := range []string{"AUTH-ONE", "AUTH-TWO"} {
		for _, source := range base.Case.Manifest.Sources {
			var raw []byte
			for _, event := range base.Case.Events {
				if event.SourceID != source.ID {
					continue
				}
				part, err := base.Case.Raw(event.ID)
				if err != nil {
					t.Fatal(err)
				}
				raw = append(raw, part...)
			}
			raw = []byte(strings.ReplaceAll(string(raw), "AUTH-ONE", authority))
			if source.ID == "s0002" {
				raw = []byte(strings.ReplaceAll(string(raw), "PLANTED-PATIENT-7391", `PLANTED-PATIENT-\X37333931\`))
			}
			inputs = append(inputs, bundle.Input{Path: "invented-scoped-source", Data: raw})
		}
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	scoped, err := bundle.Write(filepath.Join(t.TempDir(), "scoped.case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported})
	if err != nil {
		t.Fatal(err)
	}
	base.Case = scoped
	derived, err := derive(base)
	if err != nil {
		t.Fatal(err)
	}
	caseBundle := derivedBundle(t, derived)
	first := derivedField(t, caseBundle, "s0001-e000001", "PID-3.1")
	second := derivedField(t, caseBundle, "s0003-e000001", "PID-3.1")
	if first == second || first != derivedField(t, caseBundle, "s0002-e000001", "PID-3.1") || second != derivedField(t, caseBundle, "s0004-e000001", "PID-3.1") {
		t.Fatal("scope collapsed or equivalent decoded identifiers diverged")
	}
}

// Free text is never rewritten and never approved by an allowed literal: the
// rule that claims it stays an explicit unresolved finding.
func TestDeriveKeepsFreeTextUnresolvedWhateverItsRuleDeclares(t *testing.T) {
	inputs := deriveFixture(t)
	inputs.Policy.Fields = deleteRule(inputs.Policy.Fields, "NTE-3")
	inputs.Policy.Fields = append(inputs.Policy.Fields, FieldRule{Selector: "NTE-3", Policy: Retain, Class: "structural", Allowed: []string{"PLANTED-NTE-ALDER"}})
	derived, err := derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !unresolved(derived.Findings, "NTE[1]-3") {
		t.Fatal("free text was approved by an allowed literal")
	}
	for _, finding := range derived.Findings {
		if strings.Contains(finding.Location, "NTE[1]-3") && finding.Reason != "free-text-or-embedded-payload" {
			t.Fatalf("free text was refused for the wrong reason: %+v", finding)
		}
	}
}

// A patient scope the case cannot resolve leaves every scoped rule unresolved,
// so an unsupportable policy blocks the review instead of inventing an offset.
func TestDeriveLeavesAnUnresolvablePatientScopeUnresolved(t *testing.T) {
	inputs := deriveFixture(t)
	inputs.Policy.Patient.Selector = "PID-4"
	derived, err := derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.Local.Shifts) != 0 {
		t.Fatal("an unknown patient was shifted anyway")
	}
	if !unresolved(derived.Findings, "SCH[1]-11[1].4") || !unresolved(derived.Findings, "PID[1]-7[1]") {
		t.Fatalf("the scoped rules were not left unresolved: %+v", derived.Findings)
	}
}

// The delimiter declarations only ever retain exact literals; a rule that
// would remove one is refused by name.
func TestDeriveRefusesARemovedDelimiterDeclaration(t *testing.T) {
	inputs := deriveFixture(t)
	inputs.Policy.Fields = deleteRule(inputs.Policy.Fields, "MSH-2")
	inputs.Policy.Fields = append(inputs.Policy.Fields, FieldRule{Selector: "MSH-2", Policy: Remove, Class: "structural"})
	if _, err := derive(inputs); err == nil || !strings.Contains(err.Error(), "delimiter declarations") {
		t.Fatalf("the delimiter declaration was not refused by name: %v", err)
	}
}

// Two rules that each write into one empty position used to land there one
// after the other, a write into an empty component at the edge of a rewritten
// field landed beside it or was refused depending on rule order, and a rule
// overlapping an earlier one that did not apply was let through. Overlapping
// rules are refused whatever the position holds, whatever order the policy
// lists them in and whether or not each applied. Rules that all leave one
// empty position empty agree, so they are not refused.
func TestDeriveRefusesOverlappingRulesIncludingTwoWritesIntoOneEmptyPosition(t *testing.T) {
	replace := func(selector, value string) FieldRule {
		return FieldRule{Selector: selector, Policy: Replace, Class: "medical-record-numbers", Replacement: &value}
	}
	unmatched := FieldRule{Selector: "PID-3", Policy: Retain, Class: "structural", Allowed: []string{"NOT-THE-IDENTIFIER"}}
	removal := FieldRule{Selector: "PID-3.2", Policy: Remove, Class: "medical-record-numbers"}
	edgeRemoval := FieldRule{Selector: "PID-3.1", Policy: Remove, Class: "medical-record-numbers"}
	for name, test := range map[string]struct {
		identifier string
		rules      []FieldRule
	}{
		"an empty PID-3, the field first":                                  {"", []FieldRule{replace("PID-3", "FIELD"), replace("PID-3.1", "COMPONENT")}},
		"an empty PID-3, the component first":                              {"", []FieldRule{replace("PID-3.1", "COMPONENT"), replace("PID-3", "FIELD")}},
		"an empty PID-3.1, the field first":                                {"^^^AUTH-ONE", []FieldRule{replace("PID-3", "FIELD"), replace("PID-3.1", "COMPONENT")}},
		"an empty PID-3.1, the component first":                            {"^^^AUTH-ONE", []FieldRule{replace("PID-3.1", "COMPONENT"), replace("PID-3", "FIELD")}},
		"a rule inside one that did not apply":                             {"PLANTED-PATIENT-7391^^^AUTH-ONE", []FieldRule{unmatched, replace("PID-3.1", "COMPONENT")}},
		"an empty position left empty inside a field, the field first":     {"PLANTED-PATIENT-7391^^^AUTH-ONE", []FieldRule{replace("PID-3", "FIELD"), removal}},
		"an empty position left empty inside a field, the component first": {"PLANTED-PATIENT-7391^^^AUTH-ONE", []FieldRule{removal, replace("PID-3", "FIELD")}},
		"an empty position left empty and written into":                    {"", []FieldRule{removal, replace("PID-3.1", "COMPONENT")}},
		"an empty position left empty at a field's edge, the field first":  {"^^^AUTH-ONE", []FieldRule{replace("PID-3", "FIELD"), edgeRemoval}},
		"an empty position left empty at a field's edge, the edge first":   {"^^^AUTH-ONE", []FieldRule{edgeRemoval, replace("PID-3", "FIELD")}},
	} {
		t.Run(name, func(t *testing.T) {
			inputs := deriveFixture(t)
			inputs.Case = deriveCase(t, "PLANTED-PATIENT-7391^^^AUTH-ONE", test.identifier)
			inputs.Policy.Fields = slices.DeleteFunc(inputs.Policy.Fields, func(rule FieldRule) bool {
				return strings.HasPrefix(rule.Selector, "PID-3")
			})
			inputs.Policy.Fields = append(inputs.Policy.Fields, test.rules...)
			if _, err := derive(inputs); err == nil || !strings.Contains(err.Error(), "redaction policies overlap") {
				t.Fatalf("two rules writing into one position were not refused: %v", err)
			}
		})
	}
	// The planted policy shifts both appointment endpoints. Where the booking
	// declares no appointment timing, both rules leave the one empty position
	// empty.
	inputs := deriveFixture(t)
	inputs.Case = deriveCase(t, "^^^20260102100000+0000^20260102103000+0000", "")
	if _, err := derive(inputs); err != nil {
		t.Fatalf("two rules leaving an empty position empty were refused: %v", err)
	}
}

// A blocked review never reaches the prove step, and Export refuses it
// without creating output. The fake adapter is the proof: the wiring under
// test was handed the seam and never consulted it.
func TestABlockedReviewIsNeverProvenAndNeverExports(t *testing.T) {
	request := createRequest(t)
	policy, err := os.ReadFile(request.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	decoded.Fields = deleteRule(decoded.Fields, "PID-3.1")
	writeCanonical(t, request.PolicyPath, decoded)
	prover := &fakeProver{}
	review, err := createWithProver(context.Background(), request, prover)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "blocked" {
		t.Fatal("the unhandled identifier field was approved")
	}
	if prover.called {
		t.Fatal("a blocked review ran a fixture proof")
	}
	packet := filepath.Join(filepath.Dir(request.Output), "packet")
	if _, err := Export(context.Background(), ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: packet}); err == nil {
		t.Fatal("blocked review exported")
	}
	if _, err := os.Stat(packet); !os.IsNotExist(err) {
		t.Fatal("blocked export created output")
	}
}

// The prove seam carries the trial's inputs and its answer: a prover's
// refusal becomes the review's private explanation and the one fixed located
// finding, never a rewritten verdict.
func TestAProverRefusalBlocksTheReviewWithItsExplanation(t *testing.T) {
	request := createRequest(t)
	cause := "fixture proof did not preserve the exact agreed failures [1 2] and full fixed pass: baseline assertion_failure with failed assertions [1 2]; postfix pass"
	prover := &fakeProver{err: errors.New(cause)}
	review, err := createWithProver(context.Background(), request, prover)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "blocked" || review.OriginalProofFailure != cause {
		t.Fatalf("prover refusal did not reach the caller: %+v", review)
	}
	if !prover.called || !slices.Equal(prover.required, []int{1, 2}) || prover.casePath != request.CasePath || !strings.HasSuffix(prover.dir, "original-proof") {
		t.Fatalf("the prover was not handed the original trial: %+v", prover)
	}
	if !unresolved(review.Findings, "proof/original-assertions") {
		t.Fatalf("the proof refusal lost its located finding: %+v", review.Findings)
	}
	for _, name := range []string{filepath.Join(request.Output, "review.json"), filepath.Join(request.LocalState, "state.json")} {
		raw, err := os.ReadFile(name)
		if err != nil || strings.Contains(string(raw), "did not preserve") {
			t.Fatalf("proof failure cause entered %s: %v", filepath.Base(name), err)
		}
	}
}
