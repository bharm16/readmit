package redact

import (
	"encoding/json/v2"
	"os"
	"strings"
	"testing"
)

// fixturePolicyBytes reads the shipped synthetic policy document.
func fixturePolicyBytes(t *testing.T) ([]byte, error) {
	t.Helper()
	return os.ReadFile("../../testdata/fixtures/redact-policy.json")
}

// decodePolicyFrom is the document-level setup for the decoder's table tests:
// it marshals an edited fixture policy the way an authored document is
// written, then decodes it through the one reader every caller shares.
func decodePolicyFrom(t *testing.T, edit func(policy *Policy)) error {
	t.Helper()
	policy, err := decodeFixturePolicy(t)
	if err != nil {
		t.Fatal(err)
	}
	edit(&policy)
	raw, err := json.Marshal(policy, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodePolicy(append(raw, '\n'))
	return err
}

func decodeFixturePolicy(t *testing.T) (Policy, error) {
	t.Helper()
	raw, err := fixturePolicyBytes(t)
	if err != nil {
		t.Fatal(err)
	}
	return DecodePolicy(raw)
}

// Every refusal names the declaration that failed, so an unsupported policy
// says which of its rules to fix, and the acceptance set is unchanged.
func TestDecodePolicyNamesTheRuleThatFailed(t *testing.T) {
	accepted, err := decodeFixturePolicy(t)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Schema != PolicySchema || len(accepted.Fields) == 0 {
		t.Fatal("the shipped fixture policy must be the accepted baseline")
	}
	for _, test := range []struct {
		name string
		edit func(policy *Policy)
		want string
	}{
		{
			name: "schema",
			edit: func(policy *Policy) { policy.Schema = "readmit-redact-policy/v2" },
			want: "schema must be readmit-redact-policy/v1",
		},
		{
			name: "unknown-member",
			edit: func(policy *Policy) {},
			want: "holding only known members",
		},
		{
			name: "no-required-failures",
			edit: func(policy *Policy) { policy.RequiredFailures = nil },
			want: "required_failures declares no assertion position",
		},
		{
			name: "required-failure-below-one",
			edit: func(policy *Policy) { policy.RequiredFailures = []int{0, 1} },
			want: "required_failures declares 0, which is below the first assertion position",
		},
		{
			name: "required-failure-beyond-256",
			edit: func(policy *Policy) { policy.RequiredFailures = []int{257} },
			want: "required_failures declares 257, which is beyond the last supported assertion position",
		},
		{
			name: "duplicate-required-failure",
			edit: func(policy *Policy) { policy.RequiredFailures = []int{1, 1} },
			want: "required_failures declares 1 more than once",
		},
		{
			name: "patient-selector",
			edit: func(policy *Policy) { policy.Patient.Selector = "not a selector" },
			want: "the patient selector is not a supported field selector",
		},
		{
			name: "patient-authority",
			edit: func(policy *Policy) {
				policy.Patient.Authority = append(policy.Patient.Authority, "PID-3.4", "PID-3.4")
			},
			want: "the patient authority declares an unsupported field selector or more than 4 selectors",
		},
		{
			name: "field-selector",
			edit: func(policy *Policy) { policy.Fields[0].Selector = "PID" },
			want: "a field rule does not name a supported field selector",
		},
		{
			name: "duplicate-field-rule",
			edit: func(policy *Policy) { policy.Fields[0].Selector = policy.Fields[len(policy.Fields)-1].Selector },
			want: "more than once",
		},
		{
			name: "field-class",
			edit: func(policy *Policy) { policy.Fields[0].Class = "not-a-category" },
			want: "declares a class outside the coverage checklist",
		},
		{
			name: "unknown-field-policy",
			edit: func(policy *Policy) { policy.Fields[0].Policy = "unsupported/v1" },
			want: "the field rule for MSH[1]-1[1] declares an unsupported policy",
		},
		{
			name: "surrogate-scope",
			edit: func(policy *Policy) {
				for i := range policy.Fields {
					if policy.Fields[i].Policy == Surrogate {
						policy.Fields[i].Scope = "Not A Scope"
						return
					}
				}
			},
			want: "declares a scope that is not a lowercase named scope",
		},
		{
			name: "surrogate-replacement",
			edit: func(policy *Policy) {
				value := "X"
				for i := range policy.Fields {
					if policy.Fields[i].Policy == Surrogate {
						policy.Fields[i].Replacement = &value
						return
					}
				}
			},
			want: "must not declare a replacement",
		},
		{
			name: "surrogate-structural",
			edit: func(policy *Policy) {
				for i := range policy.Fields {
					if policy.Fields[i].Policy == Surrogate {
						policy.Fields[i].Class = "structural"
						return
					}
				}
			},
			want: "must not declare the structural class",
		},
		{
			name: "date-shift-class",
			edit: func(policy *Policy) {
				for i := range policy.Fields {
					if policy.Fields[i].Policy == DateShift {
						policy.Fields[i].Class = "names"
						return
					}
				}
			},
			want: "must declare the dates-and-ages class",
		},
		{
			name: "date-shift-scope",
			edit: func(policy *Policy) {
				for i := range policy.Fields {
					if policy.Fields[i].Policy == DateShift {
						policy.Fields[i].Scope = "patient"
						return
					}
				}
			},
			want: "must not declare a scope",
		},
		{
			name: "remove-with-replacement",
			edit: func(policy *Policy) {
				value := "X"
				policy.Fields[0].Policy = Remove
				policy.Fields[0].Replacement = &value
			},
			want: "declares a replacement, which only replace-field/v1 may carry",
		},
		{
			name: "replace-unsafe-replacement",
			edit: func(policy *Policy) {
				value := "injected\x01control"
				policy.Fields[0].Policy = Replace
				policy.Fields[0].Allowed = nil
				policy.Fields[0].Replacement = &value
			},
			want: "the field rule for MSH[1]-1[1] (replace-field/v1) must declare a replacement that is bounded printable UTF-8 text",
		},
		{
			name: "retain-without-allowed",
			edit: func(policy *Policy) { policy.Fields[0].Allowed = nil },
			want: "must declare between 1 and 64 allowed literals",
		},
		{
			name: "retain-non-structural",
			edit: func(policy *Policy) { policy.Fields[0].Class = "names" },
			want: "must declare the structural class",
		},
		{
			name: "remove-with-allowed",
			edit: func(policy *Policy) { policy.Fields[0].Policy = Remove; policy.Fields[0].Allowed = []string{"|"} },
			want: "declares allowed literals, which only retain-literal/v1 may carry",
		},
		{
			name: "remove-segment-msh",
			edit: func(policy *Policy) { policy.RemoveSegments = append(policy.RemoveSegments, "MSH") },
			want: "remove_segments declares MSH, which every message requires",
		},
		{
			name: "remove-segment-token",
			edit: func(policy *Policy) { policy.RemoveSegments = append(policy.RemoveSegments, "msh") },
			want: "is not a segment identifier",
		},
		{
			name: "remove-segment-duplicate",
			edit: func(policy *Policy) { policy.RemoveSegments = append(policy.RemoveSegments, policy.RemoveSegments[0]) },
			want: "remove_segments declares ERR more than once",
		},
		{
			name: "packet-policy-unknown",
			edit: func(policy *Policy) { policy.PacketPolicies[0] = "invented/v1" },
			want: "packet_policies entry 1 is not a supported packet policy",
		},
		{
			name: "packet-policy-duplicate",
			edit: func(policy *Policy) { policy.PacketPolicies[0] = policy.PacketPolicies[1] },
			want: "more than once",
		},
		{
			name: "binding-location-length",
			edit: func(policy *Policy) {
				policy.SpecBindings = append(policy.SpecBindings, LiteralBinding{Location: strings.Repeat("x", 129)})
			},
			want: "a spec binding must name a location of 1 to 128 characters",
		},
		{
			name: "binding-constant-with-selector",
			edit: func(policy *Policy) { policy.SpecBindings[8].Occurrence = "s0001-e000001" },
			want: "pins a constant, so it must not declare an occurrence or selector",
		},
		{
			name: "binding-constant-unknown-code",
			edit: func(policy *Policy) { value := "XX"; policy.SpecBindings[8].Constant = &value },
			want: "pins a constant outside the acknowledged protocol codes",
		},
		{
			name: "binding-selector",
			edit: func(policy *Policy) { policy.SpecBindings[0].Selector = "invented" },
			want: "does not name a supported source selector",
		},
		{
			name: "binding-occurrence",
			edit: func(policy *Policy) { policy.SpecBindings[0].Occurrence = "e000001" },
			want: "does not name an occurrence like s0001-e000001",
		},
		{
			name: "binding-duplicate",
			edit: func(policy *Policy) { policy.SpecBindings[1].Location = policy.SpecBindings[0].Location },
			want: "spec binding 2 repeats a location",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "unknown-member" {
				if _, err := DecodePolicy([]byte(`{"schema":"readmit-redact-policy/v1","invented":true}`)); err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("an unknown member was not refused by name: %v", err)
				}
				return
			}
			err := decodePolicyFrom(t, test.edit)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "invalid redaction policy: ") {
				t.Fatalf("refusal did not name the rule %q: %v", test.want, err)
			}
		})
	}
}

func TestPolicyRefusalsDoNotEchoUnvalidatedText(t *testing.T) {
	const private = "PRIVATE-PATIENT-NAME\n\x1b[31m"
	for name, edit := range map[string]func(*Policy){
		"segment": func(p *Policy) { p.RemoveSegments = []string{private} },
		"packet":  func(p *Policy) { p.PacketPolicies = []string{private} },
		"binding selector": func(p *Policy) {
			p.SpecBindings = []LiteralBinding{{Location: private, Selector: "invalid"}}
		},
		"duplicate location": func(p *Policy) {
			code := "AA"
			p.SpecBindings = []LiteralBinding{{Location: private, Constant: &code}, {Location: private, Constant: &code}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := decodePolicyFrom(t, edit)
			if err == nil || strings.Contains(err.Error(), "PRIVATE-PATIENT-NAME") || strings.ContainsAny(err.Error(), "\n\x1b") {
				t.Fatalf("policy refusal disclosed unvalidated text: %v", err)
			}
		})
	}
}
