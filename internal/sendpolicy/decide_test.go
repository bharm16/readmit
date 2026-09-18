package sendpolicy

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/netip"
	"slices"
	"testing"
)

// loopbackOnly is the policy a team records for a laboratory endpoint that
// lives on the machine running readmit.
func loopbackOnly(t *testing.T) *Policy {
	t.Helper()
	return policyFrom(t, `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8","::1/128"]}`)
}

func policyFrom(t *testing.T, document string) *Policy {
	t.Helper()
	policy, err := DecodePolicy([]byte(document))
	if err != nil {
		t.Fatalf("DecodePolicy(%s): %v", document, err)
	}
	return &policy
}

// resolvesTo answers every name with the same addresses. No test resolves a
// name outside itself, so nothing here depends on DNS or reaches a real host.
func resolvesTo(addresses ...string) Resolver {
	return func(context.Context, string) ([]netip.Addr, error) {
		parsed := make([]netip.Addr, 0, len(addresses))
		for _, address := range addresses {
			parsed = append(parsed, netip.MustParseAddr(address))
		}
		return parsed, nil
	}
}

func refusesToResolve() Resolver {
	return func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("no such host")
	}
}

// TestDecideRefusesWhatItCannotName is the substance of the rule: every
// destination readmit cannot establish as approved is denied by name.
func TestDecideRefusesWhatItCannotName(t *testing.T) {
	approvedLab := policyFrom(t, `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.0/24"]}`)
	for _, c := range []struct {
		name     string
		policy   *Policy
		request  Request
		resolve  Resolver
		want     Reason
		resolved []string
	}{
		{
			name:    "a production class is refused even with a policy approving the address",
			policy:  loopbackOnly(t),
			request: Request{Address: "127.0.0.1:2575", Classification: "production", Explicit: true},
			want:    ProductionClassification,
		},
		{
			name:    "a production class is refused with no policy at all",
			request: Request{Address: "127.0.0.1:2575", Classification: "production", Explicit: true},
			want:    ProductionClassification,
		},
		{
			name:    "a class nobody recorded is not a nonproduction one",
			policy:  loopbackOnly(t),
			request: Request{Address: "198.51.100.7:2575", Classification: "unclassified", Explicit: true},
			want:    UnrecordedClassification,
		},
		{
			name:    "an absent class reads as unclassified rather than as approval",
			policy:  loopbackOnly(t),
			request: Request{Address: "198.51.100.7:2575", Explicit: true},
			want:    UnrecordedClassification,
		},
		{
			name:    "a nonloopback address needs a policy that names it",
			request: Request{Address: "198.51.100.7:2575", Classification: "nonproduction", Explicit: true},
			want:    PolicyRequired,
		},
		{
			name:    "a name needs a policy even when it is usually loopback",
			request: Request{Address: "localhost:2575", Classification: "nonproduction", Explicit: true},
			resolve: resolvesTo("127.0.0.1"),
			want:    PolicyRequired,
		},
		{
			name:     "a name resolving to several addresses is ambiguous, not a choice",
			policy:   approvedLab,
			request:  Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true},
			resolve:  resolvesTo("198.51.100.7", "198.51.100.8"),
			want:     AmbiguousDestination,
			resolved: []string{"198.51.100.7", "198.51.100.8"},
		},
		{
			name:    "a name that does not resolve is denied, never attempted",
			policy:  approvedLab,
			request: Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true},
			resolve: refusesToResolve(),
			want:    UnresolvableDestination,
		},
		{
			name:    "a name resolving to nothing is denied",
			policy:  approvedLab,
			request: Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true},
			resolve: resolvesTo(),
			want:    UnresolvableDestination,
		},
		{
			name:     "a label does not approve what the name actually resolves to",
			policy:   approvedLab,
			request:  Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true},
			resolve:  resolvesTo("203.0.113.9"),
			want:     UnapprovedDestination,
			resolved: []string{"203.0.113.9"},
		},
		{
			name:     "a literal address outside the approved set is refused too",
			policy:   approvedLab,
			request:  Request{Address: "203.0.113.9:2575", Classification: "nonproduction", Explicit: true},
			want:     UnapprovedDestination,
			resolved: []string{"203.0.113.9"},
		},
		{
			name:    "a send nobody asked for is not sent",
			policy:  approvedLab,
			request: Request{Address: "198.51.100.7:2575", Classification: "nonproduction"},
			want:    SendNotExplicit,
		},
		{
			name:    "a loopback send nobody asked for is not sent either",
			request: Request{Address: "127.0.0.1:2575", Classification: "nonproduction"},
			want:    SendNotExplicit,
		},
		{
			name:    "an address without a port is not a destination",
			policy:  approvedLab,
			request: Request{Address: "198.51.100.7", Classification: "nonproduction", Explicit: true},
			want:    UnresolvableDestination,
		},
		{
			name:    "an empty host is not a destination",
			policy:  approvedLab,
			request: Request{Address: ":2575", Classification: "nonproduction", Explicit: true},
			want:    UnresolvableDestination,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			decision := Decide(t.Context(), c.policy, c.request, c.resolve)
			if decision.Allowed || decision.Reason != c.want {
				t.Fatalf("allowed=%t reason=%q, want a denial for %q", decision.Allowed, decision.Reason, c.want)
			}
			if c.resolved != nil && !slices.Equal(decision.ResolvedAddresses, c.resolved) {
				t.Fatalf("resolved %q, want %q", decision.ResolvedAddresses, c.resolved)
			}
			if decision.Schema != DecisionSchema {
				t.Fatalf("decision schema %q", decision.Schema)
			}
		})
	}
}

// TestDecideApproves covers the two destinations that are allowed at all.
func TestDecideApproves(t *testing.T) {
	for _, c := range []struct {
		name     string
		policy   *Policy
		request  Request
		resolve  Resolver
		want     Reason
		resolved []string
	}{
		{
			name:     "a literal loopback address needs no approved-destination document",
			request:  Request{Address: "127.0.0.1:2575", Classification: "unclassified", Explicit: true},
			want:     LoopbackDestination,
			resolved: []string{"127.0.0.1"},
		},
		{
			name:     "an IPv6 loopback address is loopback too",
			request:  Request{Address: "[::1]:2575", Classification: "unclassified", Explicit: true},
			want:     LoopbackDestination,
			resolved: []string{"::1"},
		},
		{
			name:     "a name resolving into the approved set is approved",
			policy:   policyFrom(t, `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.0/24"]}`),
			request:  Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true},
			resolve:  resolvesTo("198.51.100.7", "198.51.100.7"),
			want:     Approved,
			resolved: []string{"198.51.100.7"},
		},
		{
			name:     "a loopback literal under a policy is approved on the policy's terms",
			policy:   loopbackOnly(t),
			request:  Request{Address: "127.0.0.1:2575", Classification: "nonproduction", Explicit: true},
			want:     Approved,
			resolved: []string{"127.0.0.1"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			decision := Decide(t.Context(), c.policy, c.request, c.resolve)
			if !decision.Allowed || decision.Reason != c.want {
				t.Fatalf("allowed=%t reason=%q, want an approval for %q", decision.Allowed, decision.Reason, c.want)
			}
			if !slices.Equal(decision.ResolvedAddresses, c.resolved) {
				t.Fatalf("resolved %q, want %q", decision.ResolvedAddresses, c.resolved)
			}
		})
	}
}

// TestDecisionIsRetainableEvidence checks that a decision reads back as the
// strict document it declares, including the destinations it was decided
// against, so retained evidence answers the question without the policy file.
func TestDecisionIsRetainableEvidence(t *testing.T) {
	policy := policyFrom(t, `{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.0/24"]}`)
	decision := Decide(t.Context(), policy, Request{Address: "lab.example.invalid:2575", Classification: "nonproduction", Explicit: true}, resolvesTo("203.0.113.9"))
	data, err := EncodeDecision(decision)
	if err != nil {
		t.Fatalf("EncodeDecision: %v", err)
	}
	var reread Decision
	if err := json.Unmarshal(data, &reread, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("a retained decision does not read back strictly: %v", err)
	}
	if reread.Reason != UnapprovedDestination || reread.Allowed {
		t.Fatalf("reread %+v", reread)
	}
	if !slices.Equal(reread.ApprovedDestinations, []string{"198.51.100.0/24"}) || !slices.Equal(reread.ResolvedAddresses, []string{"203.0.113.9"}) {
		t.Fatalf("a decision does not name what it compared: %+v", reread)
	}
	if reread.DecidedAt.IsZero() || reread.Address != "lab.example.invalid:2575" {
		t.Fatalf("a decision does not name when and what it decided: %+v", reread)
	}
}

func TestDecideRejectsInvalidConstructedPolicies(t *testing.T) {
	for _, policy := range []Policy{
		{Schema: "readmit-send-policy/v2", ApprovedDestinations: []string{"127.0.0.0/8"}},
		{Schema: PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8", "invalid"}},
		{Schema: PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8", "127.0.0.0/8"}},
	} {
		decision := Decide(t.Context(), &policy, Request{Address: "127.0.0.1:2575", Explicit: true}, nil)
		if decision.Allowed {
			t.Fatalf("invalid policy approved: %+v", policy)
		}
	}
}
