package fhirvalidator_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

func fixturePlan(t *testing.T, requirements fhirvalidator.Requirements) *fhirvalidator.Plan {
	t.Helper()
	m, assets := fixtureManifest()
	m.Packages = append(m.Packages, fixturePinnedPackage(fhirvalidator.PackageRef{ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}))
	m.ValidatorPackages = []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}, {ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}}
	for _, root := range []fhirvalidator.PackageRef{{ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}} {
		m.Packages = append(m.Packages, fixturePinnedPackage(root))
		m.ValidatorPackages = append(m.ValidatorPackages, root)
	}
	c, e := fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "cap"), m, assets)
	if e != nil {
		t.Fatal(e)
	}
	input := []byte(`{"resourceType":"Patient","active":true}`)
	request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: c.Identity(), InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{m.Profiles[0]}, Requirements: requirements, TimeoutMS: 120000, MaxOutputBytes: 1 << 20}
	raw, _ := json.Marshal(request)
	p, e := fhirvalidator.Prepare(raw, input, c)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

var required = fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error"}}

func evaluated(outcome string, exit int) fhirvalidator.WorkerResponse {
	return fhirvalidator.WorkerResponse{Schema: fhirvalidator.WorkerResponseSchema, Job: "0123456789abcdef0123456789abcdef", State: "evaluated", ExitCode: exit, Outcome: []byte(outcome)}
}

func issue(severity, code, message string) string {
	return `{"severity":"` + severity + `","code":"` + code + `","extension":[{"url":"http://hl7.org/fhir/StructureDefinition/operationoutcome-message-id","valueCode":"` + message + `"}],"details":{"text":"synthetic"},"expression":["Patient"]}`
}

func outcome(issues ...string) string {
	return `{"resourceType":"OperationOutcome","issue":[` + strings.Join(issues, ",") + `]}`
}

// The pinned validator's actual outcomes for the independently authored
// oracle, recorded by TestFHIRValidatorLiveQualification. The policy must
// reach the oracle's state from them without Java.
func TestFHIRValidatorPolicyReadsRecordedWorkerOutcomes(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fhir-validation/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Cases []liveCase `json:"cases"`
	}
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	p := fixturePlan(t, required)
	seen := 0
	for _, c := range oracle.Cases {
		name := strings.TrimSuffix(c.Input, ".json")
		if len(c.Profiles) > 0 {
			name += "+" + filepath.Base(strings.Split(c.Profiles[0], "|")[0])
		}
		recorded, err := os.ReadFile(filepath.Join("../../testdata/fhir-validation/outcomes", name+".json"))
		if os.IsNotExist(err) && c.Expected == "profile-unavailable" {
			continue // refused during preparation; the worker never ran
		}
		if err != nil {
			t.Fatal(err)
		}
		seen++
		t.Run(name, func(t *testing.T) {
			var parsed struct {
				Issue []struct {
					Severity string `json:"severity"`
				} `json:"issue"`
			}
			if err := json.Unmarshal(recorded, &parsed); err != nil {
				t.Fatal(err)
			}
			exit := 0
			if slices.ContainsFunc(parsed.Issue, func(i struct {
				Severity string `json:"severity"`
			}) bool {
				return i.Severity == "error" || i.Severity == "fatal"
			}) {
				exit = 1
			}
			r, err := p.Interpret(fhirvalidator.WorkerResponse{Schema: fhirvalidator.WorkerResponseSchema, Job: "0123456789abcdef0123456789abcdef", State: "evaluated", ExitCode: exit, Outcome: recorded})
			expected := c.Expected
			if expected == "" {
				expected = c.Evaluated
			}
			if err != nil || r.State != expected || r.Policy != fhirvalidator.Policy {
				t.Fatalf("state %s policy %s: %v; oracle %q", r.State, r.Policy, err, expected)
			}
			if (expected == "conforms") != (r.Verdict == assertion.VerdictPass) || (expected == "nonconforms") != (r.Verdict == assertion.VerdictFail) {
				t.Fatal("verdict", r.Verdict)
			}
			for _, f := range r.Findings {
				if f.InputSHA256 != p.Request().InputSHA256 || len(f.Expressions) == 0 {
					t.Fatal("finding is not linked to its input and location", f)
				}
			}
		})
	}
	if seen < 8 {
		t.Fatal("recorded outcomes missing", seen)
	}
}

func TestFHIRValidatorPolicyDistinguishesUncheckedFromDecided(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		exit          int
		requirements  fhirvalidator.Requirements
		state         string
		verdict       assertion.Verdict
	}{
		{"no issues", outcome(), 0, required, "conforms", assertion.VerdictPass},
		{"cardinality", outcome(issue("error", "structure", "Validation_VAL_Profile_Minimum")), 1, required, "nonconforms", assertion.VerdictFail},
		{"constraint key is a decided finding", outcome(issue("error", "invariant", "https://example.test/StructureDefinition/unknown-profile#inv-1")), 1, required, "nonconforms", assertion.VerdictFail},
		{"missing value set as a warning", outcome(issue("warning", "code-invalid", "Terminology_TX_ValueSet_NotFound")), 0, required, "terminology-unavailable", assertion.VerdictUndecided},
		{"expression not evaluated", outcome(issue("error", "invariant", "Problem_processing_expression__in_profile__path__")), 1, required, "invariant-unsupported", assertion.VerdictUndecided},
		{"profile not staged", outcome(issue("warning", "structure", "VALIDATION_VAL_PROFILE_UNKNOWN_NOT_POLICY")), 0, required, "profile-unavailable", assertion.VerdictUndecided},
		{"slicing not checked", outcome(issue("warning", "structure", "Validation_VAL_Profile_NoCheckMin")), 0, required, "check-unavailable", assertion.VerdictUndecided},
		{"incomplete processing", outcome(issue("warning", "incomplete", "SOMETHING_NEW")), 0, required, "check-unavailable", assertion.VerdictUndecided},
		{"decided error outranks an unchecked area", outcome(issue("warning", "code-invalid", "Terminology_TX_ValueSet_NotFound"), issue("error", "structure", "Validation_VAL_Profile_Minimum")), 1, required, "nonconforms", assertion.VerdictFail},
		{"warning gate", outcome(issue("warning", "invalid", "All_observations_should_have_a_subject")), 0, fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error", "warning"}}, "conforms", assertion.VerdictFail},
		{"terminology not requested never passes", outcome(), 0, fhirvalidator.Requirements{Terminology: "not-requested", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, "not-fully-evaluated", assertion.VerdictUndecided},
		{"terminology not requested ignores its gaps", outcome(issue("warning", "code-invalid", "Terminology_TX_ValueSet_NotFound")), 0, fhirvalidator.Requirements{Terminology: "not-requested", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, "not-fully-evaluated", assertion.VerdictUndecided},
		{"invariants not requested still fail on errors", outcome(issue("error", "structure", "Validation_VAL_Profile_Minimum")), 1, fhirvalidator.Requirements{Terminology: "required", Invariants: "not-requested", FailSeverities: []string{"fatal", "error"}}, "nonconforms", assertion.VerdictFail},
		{"exit code without an error", outcome(), 1, required, "worker-output-invalid", assertion.VerdictUndecided},
		{"error with a success exit", outcome(issue("error", "structure", "Validation_VAL_Profile_Minimum")), 0, required, "worker-output-invalid", assertion.VerdictUndecided},
		{"crash exit", outcome(), 2, required, "worker-crashed", assertion.VerdictUndecided},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := fixturePlan(t, tc.requirements).Interpret(evaluated(tc.outcome, tc.exit))
			if err != nil || r.State != tc.state || r.Verdict != tc.verdict {
				t.Fatalf("state %s verdict %s: %v", r.State, r.Verdict, err)
			}
			// An outcome the policy refuses evaluated nothing and states no finding.
			if r.State == "worker-output-invalid" || r.State == "worker-crashed" {
				for area, coverage := range r.Coverage {
					if coverage != "unavailable" {
						t.Fatalf("%s coverage %s", area, coverage)
					}
				}
				if len(r.Findings) != 0 {
					t.Fatal("findings kept from a refused outcome", r.Findings)
				}
			}
		})
	}
}

func TestFHIRValidatorPolicyRefusesAGateThatLetsErrorsPass(t *testing.T) {
	m, assets := fixtureManifest()
	for _, root := range []fhirvalidator.PackageRef{{ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}, {ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}} {
		m.Packages = append(m.Packages, fixturePinnedPackage(root))
		m.ValidatorPackages = append(m.ValidatorPackages, root)
	}
	m.ValidatorPackages = append(m.ValidatorPackages, fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"})
	c, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "cap"), m, assets)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"resourceType":"Patient"}`)
	for _, gate := range [][]string{{"fatal"}, {"error"}, {"warning", "information"}} {
		request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: c.Identity(), InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: gate}, TimeoutMS: 1000, MaxOutputBytes: 1 << 20}
		raw, _ := json.Marshal(request)
		if _, err := fhirvalidator.Prepare(raw, input, c); err == nil {
			t.Fatalf("gate %v accepted", gate)
		}
	}
}
