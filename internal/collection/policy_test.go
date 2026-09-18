package collection_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
)

const acceptAll = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func TestPolicyDecodesDeclaredOperatorsAndLabels(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(acceptAll))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Name != "downstream-sink" || policy.SourceLabel != "downstream-test-endpoint" || policy.Acknowledgement.Code != "AA" {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if !policy.Accepts("ADT", "A01") || !policy.Accepts("SIU", "S12") {
		t.Fatal("any-message-type must accept every declared type")
	}
}

func TestPolicyRestrictsAcceptedMessageTypes(t *testing.T) {
	data := `{"schema":"readmit-receiver-policy/v1","name":"adt-only","source_label":"lab-route","acknowledgement":{"operator":"original-mode-fixed-code","code":"AE"},"accepted_message_types":{"operator":"message-type-in","values":["ADT^A01","ORU"]}}`
	policy, err := collection.DecodePolicy([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, accepted := range [][2]string{{"ADT", "A01"}, {"ORU", "R01"}, {"ORU", ""}} {
		if !policy.Accepts(accepted[0], accepted[1]) {
			t.Fatalf("declared type %v rejected", accepted)
		}
	}
	for _, rejected := range [][2]string{{"ADT", "A08"}, {"ADT", ""}, {"SIU", "S12"}, {"", ""}} {
		if policy.Accepts(rejected[0], rejected[1]) {
			t.Fatalf("undeclared type %v accepted", rejected)
		}
	}
}

func TestPolicyRejectsUnsupportedAndIncompleteDeclarations(t *testing.T) {
	for name, data := range map[string]string{
		"unknown member":       strings.Replace(acceptAll, `"name"`, `"mode":"fixed","name"`, 1),
		"unknown schema":       strings.Replace(acceptAll, "readmit-receiver-policy/v1", "readmit-receiver-policy/v2", 1),
		"unsupported operator": strings.Replace(acceptAll, "original-mode-fixed-code", "original-mode-random-code", 1),
		"unsupported ack mode": strings.Replace(acceptAll, `"code":"AA"`, `"code":"CA"`, 1),
		"unsupported matcher":  strings.Replace(acceptAll, "any-message-type", "message-type-matches", 1),
		"values without match": strings.Replace(acceptAll, `"values":[]`, `"values":["ADT^A01"]`, 1),
		"match without values": strings.Replace(acceptAll, `"operator":"any-message-type","values":[]`, `"operator":"message-type-in","values":[]`, 1),
		"duplicate value":      strings.Replace(acceptAll, `"operator":"any-message-type","values":[]`, `"operator":"message-type-in","values":["ADT^A01","ADT^A01"]`, 1),
		"malformed value":      strings.Replace(acceptAll, `"operator":"any-message-type","values":[]`, `"operator":"message-type-in","values":["adt|a01"]`, 1),
		"missing member":       strings.Replace(acceptAll, `"source_label":"downstream-test-endpoint",`, "", 1),
		"null member":          strings.Replace(acceptAll, `"source_label":"downstream-test-endpoint"`, `"source_label":null`, 1),
		"empty label":          strings.Replace(acceptAll, `"source_label":"downstream-test-endpoint"`, `"source_label":""`, 1),
		"unprintable label":    strings.Replace(acceptAll, "downstream-test-endpoint", "down\\u0007stream", 1),
		"separator in label":   strings.Replace(acceptAll, "downstream-test-endpoint", "down|stream", 1),
		"not an object":        `[]`,
		"trailing content":     acceptAll + "{}",
	} {
		if _, err := collection.DecodePolicy([]byte(data)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	oversized := strings.Replace(acceptAll, "downstream-sink", strings.Repeat("a", 65<<10), 1)
	if _, err := collection.DecodePolicy([]byte(oversized)); err == nil {
		t.Error("accepted oversized policy")
	}
}

func FuzzPolicy(f *testing.F) {
	f.Add([]byte(acceptAll))
	f.Add([]byte(`{"schema":"readmit-receiver-policy/v1"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		policy, err := collection.DecodePolicy(data)
		if err != nil {
			return
		}
		if policy.Validate() != nil {
			t.Fatal("decoded policy is not valid")
		}
		// An accepted policy must not change meaning when it is re-encoded.
		encoded, err := collection.EncodePolicy(policy)
		if err != nil {
			t.Fatal(err)
		}
		again, err := collection.DecodePolicy(encoded)
		if err != nil {
			t.Fatalf("policy round-trip changed the declaration: %v", err)
		}
		repeated, err := collection.EncodePolicy(again)
		if err != nil || !bytes.Equal(encoded, repeated) {
			t.Fatal("policy round-trip changed the declaration")
		}
	})
}
