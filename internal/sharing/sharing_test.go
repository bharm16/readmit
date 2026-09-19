package sharing_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/sharing"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyRefusesMissingUnknownAndNetworkDestinations(t *testing.T) {
	for _, raw := range []string{`{}`, `{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["https://PLANTED.invalid"],"max_bytes":4096}`, `{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096,"extra":true}`, `{"schema":"readmit-sharing-policy/v1","destinations":["local-file"],"max_bytes":4096}`} {
		if _, err := sharing.DecodePolicy([]byte(raw)); err == nil {
			t.Fatal("unsafe policy accepted")
		}
	}
	if _, err := sharing.DecodePolicy([]byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}`)); err != nil {
		t.Fatal(err)
	}
}

func FuzzSharingPolicy(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}`))
	f.Add([]byte(`{"schema":"readmit-sharing-policy/v1","support":null}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, e := sharing.DecodePolicy(raw)
		if e == nil && (p.Allows("https://example.invalid", 0) || p.Allows("local-file", 65537)) {
			t.Fatal("policy escaped closed bounds")
		}
	})
}
func TestSummaryRejectsInjectedMembersClaimsAndMissingBindings(t *testing.T) {
	raw := []byte(`{"schema":"readmit-support-summary/v1","source_kind":"retained-packet","source_identity":"` + strings.Repeat("a", 64) + `","input_commitment":"` + strings.Repeat("b", 64) + `","spec_identity":"` + strings.Repeat("c", 64) + `","policy_identity":"` + strings.Repeat("d", 64) + `","outcome":"pass","external_equivalence":"declined","scope":` + strconv.Quote(sharing.Scope) + `}`)
	var doc map[string]any
	if e := json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	var summary sharing.Summary
	if e := json.Unmarshal(raw, &summary); e != nil {
		t.Fatal(e)
	}
	canonical, _ := json.Marshal(summary, json.Deterministic(true))
	if _, e := sharing.Decode(canonical); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"source_identity", "input_commitment", "spec_identity", "policy_identity", "scope"} {
		saved := doc[field]
		delete(doc, field)
		b, _ := json.Marshal(doc, json.Deterministic(true))
		if _, e := sharing.Decode(b); e == nil {
			t.Fatal("missing binding accepted")
		}
		doc[field] = saved
	}
	for field, value := range map[string]string{"external_equivalence": "proven", "outcome": "<script>PLANTED</script>", "source_kind": "logs", "scope": "Safe Harbor certified", "source_identity": "PLANTED"} {
		saved := doc[field]
		doc[field] = value
		b, _ := json.Marshal(doc, json.Deterministic(true))
		if _, e := sharing.Decode(b); e == nil {
			t.Fatal("unsupported claim accepted")
		}
		doc[field] = saved
	}
	doc["notes"] = "PLANTED"
	b, _ := json.Marshal(doc, json.Deterministic(true))
	if _, e := sharing.Decode(b); e == nil {
		t.Fatal("extra surface accepted")
	}
}
