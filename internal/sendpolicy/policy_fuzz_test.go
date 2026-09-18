package sendpolicy

import "testing"

// FuzzSendPolicyDocument exercises the approved-destination reader: the strict
// decode that refuses unknown members and the bounds every declared destination
// is held to. No input may panic, and every destination an accepted policy
// declares must be a canonical prefix that the decision path can apply, so the
// reader and the rule can never disagree about what was approved.
func FuzzSendPolicyDocument(f *testing.F) {
	for _, seed := range []string{
		`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"]}`,
		`{"schema":"readmit-send-policy/v1","approved_destinations":["10.1.0.0/16","::1/128","198.51.100.0/24"]}`,
		`{"schema":"readmit-send-policy/v1","approved_destinations":[]}`,
		`{"schema":"readmit-send-policy/v1","approved_destinations":["198.51.100.7/24"]}`,
		`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"],"allow_production":true}`,
		`{"schema":"readmit-send-policy/v2","approved_destinations":["127.0.0.0/8"]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		policy, err := DecodePolicy(data)
		if err != nil {
			return
		}
		if policy.Schema != PolicySchema || len(policy.ApprovedDestinations) == 0 || len(policy.ApprovedDestinations) > maxDestinations {
			t.Fatalf("accepted %+v", policy)
		}
		for _, destination := range policy.ApprovedDestinations {
			prefix, err := parsePrefix(destination)
			if err != nil {
				t.Fatalf("accepted destination %q that the rule cannot apply: %v", destination, err)
			}
			if !policy.approves(prefix.Addr()) {
				t.Fatalf("accepted destination %q that approves nothing, not even itself", destination)
			}
		}
	})
}
