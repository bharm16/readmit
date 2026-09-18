package sendpolicy

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestDecodePolicyRefusals holds the reader to what it declares. A policy that
// cannot be read exactly as written is refused, never read approximately: it is
// the document that decides where evidence is allowed to be sent.
func TestDecodePolicyRefusals(t *testing.T) {
	for _, c := range []struct{ name, document string }{
		{"an empty document", ``},
		{"a document that is not an object", `["127.0.0.0/8"]`},
		{"no schema", `{"approved_destinations":["127.0.0.0/8"]}`},
		{"another contract", `{"schema":"readmit-send-policy/v2","approved_destinations":["127.0.0.0/8"]}`},
		{"a member this contract never declared", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"],"allow_production":true}`},
		{"no approved destination at all", `{"schema":"readmit-send-policy/v1","approved_destinations":[]}`},
		{"an absent destination member", `{"schema":"readmit-send-policy/v1"}`},
		{"a bare address rather than a prefix", `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.7"]}`},
		{"a prefix carrying bits outside its mask", `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.7/24"]}`},
		{"a prefix wider than its family allows", `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.0/33"]}`},
		{"an IPv4-mapped IPv6 prefix, which is two spellings of one address", `{"schema":"readmit-send-policy/v1","approved_destinations":["::ffff:127.0.0.0/104"]}`},
		{"a zoned address", `{"schema":"readmit-send-policy/v1","approved_destinations":["fe80::1%eth0/128"]}`},
		{"the same destination twice", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8","127.0.0.0/8"]}`},
		{"a name rather than a prefix", `{"schema":"readmit-send-policy/v1","approved_destinations":["lab.example.invalid/32"]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if policy, err := DecodePolicy([]byte(c.document)); err == nil {
				t.Fatalf("read %+v from a document that must be refused", policy)
			}
		})
	}
}

func TestDecodePolicyReadsWhatItDeclares(t *testing.T) {
	policy, err := DecodePolicy([]byte(`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8","10.1.0.0/16","::1/128"]}`))
	if err != nil {
		t.Fatalf("DecodePolicy: %v", err)
	}
	if policy.Schema != PolicySchema || !slices.Equal(policy.ApprovedDestinations, []string{"127.0.0.0/8", "10.1.0.0/16", "::1/128"}) {
		t.Fatalf("read %+v", policy)
	}
}

// TestDecodePolicyBoundsTheApprovedSet checks the refusal past the bound rather
// than a silent truncation, which would approve fewer destinations than the
// operator declared without saying so.
func TestDecodePolicyBoundsTheApprovedSet(t *testing.T) {
	destinations := make([]string, 0, maxDestinations+1)
	for i := range maxDestinations + 1 {
		destinations = append(destinations, `"10.`+strconv.Itoa(i)+`.0.0/16"`)
	}
	document := `{"schema":"readmit-send-policy/v1","approved_destinations":[` + strings.Join(destinations, ",") + `]}`
	if _, err := DecodePolicy([]byte(document)); err == nil {
		t.Fatal("read a policy declaring more destinations than the bound allows")
	}
	if _, err := DecodePolicy([]byte(strings.Replace(document, `,"10.`+strconv.Itoa(maxDestinations)+`.0.0/16"`, "", 1))); err != nil {
		t.Fatalf("refused a policy at the bound: %v", err)
	}
}

// TestPolicyApprovesNothingWhenNothingWasValidated guards the direction an
// assembled value fails in: a prefix that never passed the reader approves no
// address rather than widening the approved set.
func TestPolicyApprovesNothingWhenNothingWasValidated(t *testing.T) {
	assembled := Policy{Schema: PolicySchema, ApprovedDestinations: []string{"198.51.100.7/24", "not-a-prefix"}}
	decision := Decide(t.Context(), &assembled, Request{Address: "198.51.100.7:2575", Classification: "nonproduction", Explicit: true}, nil)
	if decision.Allowed || decision.Reason != UnapprovedDestination {
		t.Fatalf("allowed=%t reason=%q", decision.Allowed, decision.Reason)
	}
}
