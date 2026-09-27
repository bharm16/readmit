package sendpolicy_test

import (
	"context"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net/netip"
	"testing"
)

func TestScopedPolicySeparatesOperationsAndValidatesEveryDNSAnswer(t *testing.T) {
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: 2575, Destinations: []string{"10.2.0.0/16"}, Selection: "lowest-address"}}}
	request := sendpolicy.ScopedRequest{Project: "lab", Environment: "test", Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Classification: "nonproduction", Address: "test.invalid:2575"}
	calls := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("10.2.0.9"), netip.MustParseAddr("10.2.0.3")}, nil
	}
	d := sendpolicy.DecideScoped(context.Background(), policy, request, resolve)
	if !d.Allowed || d.SelectedAddress != "10.2.0.3" || calls != 1 {
		t.Fatalf("unexpected scoped decision: %+v", d)
	}
	request.Operation = sendpolicy.ObservationRead
	calls = 0
	if d := sendpolicy.DecideScoped(context.Background(), policy, request, resolve); d.Allowed || calls != 0 {
		t.Fatal("send grant admitted an observation or resolved before purpose admission")
	}
	request.Operation = sendpolicy.V2Stimulus
	mixed := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.2.0.3"), netip.MustParseAddr("198.51.100.8")}, nil
	}
	if d := sendpolicy.DecideScoped(context.Background(), policy, request, mixed); d.Allowed {
		t.Fatal("unapproved alternate answer was hidden by selection")
	}
}

func TestScopedPolicyIPv6AndFiniteAddressSelection(t *testing.T) {
	p := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.ObservationRead, Port: 443, Destinations: []string{"fd00::/64"}, Selection: "lowest-address"}}}
	r := sendpolicy.ScopedRequest{Project: "lab", Environment: "test", Endpoint: "receiver", Classification: "nonproduction", Address: "receiver.invalid:443", Operation: sendpolicy.ObservationRead}
	for _, tc := range []struct {
		name      string
		addresses []string
		allowed   bool
	}{
		{"ipv6", []string{"fd00::2", "fd00::1"}, true},
		{"mixed", []string{"fd00::1", "2001:db8::1"}, false},
		{"mapped", []string{"::ffff:127.0.0.1"}, false},
		{"scoped", []string{"fd00::1%zone"}, false},
		{"empty", nil, false},
		{"too-many", []string{"fd00::1", "fd00::1", "fd00::1", "fd00::1", "fd00::1", "fd00::1", "fd00::1", "fd00::1", "fd00::1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sendpolicy.DecideScoped(context.Background(), p, r, func(context.Context, string) ([]netip.Addr, error) {
				ips := []netip.Addr{}
				for _, a := range tc.addresses {
					ips = append(ips, netip.MustParseAddr(a))
				}
				return ips, nil
			})
			if d.Allowed != tc.allowed {
				t.Fatalf("%+v", d)
			}
			if d.Allowed && d.SelectedAddress != "fd00::1" {
				t.Fatal("nondeterministic selection")
			}
		})
	}
	for _, class := range []string{"production", "unclassified", "test", ""} {
		r.Classification = class
		if d := sendpolicy.DecideScoped(context.Background(), p, r, func(context.Context, string) ([]netip.Addr, error) {
			t.Fatal("DNS for refused classification")
			return nil, nil
		}); d.Allowed {
			t.Fatal("classification granted access")
		}
	}
}
