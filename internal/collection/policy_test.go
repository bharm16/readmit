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
		"unknown schema":       strings.Replace(acceptAll, "readmit-receiver-policy/v1", "readmit-receiver-policy/v3", 1),
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
	f.Add([]byte(faultPolicy))
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

const enhancedAll = `{"schema":"readmit-receiver-policy/v2","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false}}`

const enhancedUnsupported = `{"schema":"readmit-receiver-policy/v2","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"unsupported","accept_code":"","application_code":"","application_delivery":"","application_endpoint":"","approved_transport":false}}`

func TestPolicyV1KeepsItsFrozenMemberSetAndMeaning(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(acceptAll))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Schema != collection.PolicySchemaV1 || policy.Enhanced != nil || policy.SupportsEnhanced() {
		t.Fatalf("v1 policy declared an enhanced rule: %+v", policy)
	}
	// A v1 policy must re-encode byte-identically: no member is added to v1.
	encoded, err := collection.EncodePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != acceptAll {
		t.Fatalf("v1 policy did not round-trip byte-identically:\n got %s\nwant %s", encoded, acceptAll)
	}
}

func TestPolicyV2DeclaresStageSpecificCodesAndDelivery(t *testing.T) {
	policy, err := collection.DecodePolicy([]byte(enhancedAll))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Schema != collection.PolicySchema || !policy.SupportsEnhanced() {
		t.Fatalf("v2 policy does not support enhanced mode: %+v", policy)
	}
	rule := *policy.Enhanced
	if rule.AcceptCode != collection.CommitAcceptCode || rule.ApplicationCode != collection.AcceptCode || rule.ApplicationDelivery != collection.SameConnection {
		t.Fatalf("unexpected enhanced rule: %+v", rule)
	}
	encoded, err := collection.EncodePolicy(policy)
	if err != nil || string(encoded) != enhancedAll {
		t.Fatalf("v2 policy did not round-trip byte-identically: %v %s", err, encoded)
	}
	declined, err := collection.DecodePolicy([]byte(enhancedUnsupported))
	if err != nil {
		t.Fatal(err)
	}
	if declined.SupportsEnhanced() || declined.Enhanced.Operator != collection.EnhancedUnsupported {
		t.Fatalf("an explicit refusal was read as support: %+v", declined.Enhanced)
	}
}

func TestPolicySeparatesAcceptCodesFromApplicationCodes(t *testing.T) {
	// Commit-accept is not application-accept. Neither stage may borrow the
	// other's vocabulary, in either direction.
	for name, data := range map[string]string{
		"application code in accept stage": strings.Replace(enhancedAll, `"accept_code":"CA"`, `"accept_code":"AA"`, 1),
		"accept code in application stage": strings.Replace(enhancedAll, `"application_code":"AA"`, `"application_code":"CA"`, 1),
		"accept error as application code": strings.Replace(enhancedAll, `"application_code":"AA"`, `"application_code":"CE"`, 1),
		"application error as accept code": strings.Replace(enhancedAll, `"accept_code":"CA"`, `"accept_code":"AE"`, 1),
	} {
		if _, err := collection.DecodePolicy([]byte(data)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestPolicyRefusesIncoherentEnhancedDeclarations(t *testing.T) {
	separate := strings.Replace(strings.Replace(enhancedAll,
		`"application_delivery":"same-connection"`, `"application_delivery":"separate-endpoint"`, 1),
		`"application_endpoint":""`, `"application_endpoint":"127.0.0.1:2576"`, 1)
	if _, err := collection.DecodePolicy([]byte(separate)); err != nil {
		t.Fatalf("a declared loopback application endpoint was refused: %v", err)
	}
	for name, data := range map[string]string{
		"v1 schema with an enhanced rule": strings.Replace(enhancedAll, collection.PolicySchema, collection.PolicySchemaV1, 1),
		"v1 schema with a null rule": strings.Replace(strings.Replace(enhancedAll, collection.PolicySchema, collection.PolicySchemaV1, 1),
			`{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false}`, "null", 1),
		"v2 schema without an enhanced rule": strings.Replace(acceptAll, collection.PolicySchemaV1, collection.PolicySchema, 1),
		"unsupported enhanced operator":      strings.Replace(enhancedAll, "enhanced-mode-fixed-codes", "enhanced-mode-script", 1),
		"unsupported delivery":               strings.Replace(enhancedAll, `"application_delivery":"same-connection"`, `"application_delivery":"other-process"`, 1),
		"endpoint without separate delivery": strings.Replace(enhancedAll, `"application_endpoint":""`, `"application_endpoint":"127.0.0.1:2576"`, 1),
		"separate delivery without endpoint": strings.Replace(enhancedAll, `"application_delivery":"same-connection"`, `"application_delivery":"separate-endpoint"`, 1),
		"unapproved nonloopback endpoint":    strings.Replace(separate, "127.0.0.1:2576", "10.0.0.7:2576", 1),
		"unapproved hostname endpoint":       strings.Replace(separate, "127.0.0.1:2576", "downstream.invalid:2576", 1),
		"endpoint without a port":            strings.Replace(separate, "127.0.0.1:2576", "127.0.0.1", 1),
		"endpoint with a zero port":          strings.Replace(separate, "127.0.0.1:2576", "127.0.0.1:0", 1),
		"approval without an endpoint":       strings.Replace(enhancedAll, `"approved_transport":false`, `"approved_transport":true`, 1),
		"codes on an unsupported rule":       strings.Replace(enhancedUnsupported, `"accept_code":""`, `"accept_code":"CA"`, 1),
		"delivery on an unsupported rule":    strings.Replace(enhancedUnsupported, `"application_delivery":""`, `"application_delivery":"same-connection"`, 1),
		"missing enhanced member":            strings.Replace(enhancedAll, `"accept_code":"CA",`, "", 1),
		"unknown enhanced member":            strings.Replace(enhancedAll, `"accept_code":"CA"`, `"accept_code":"CA","delay":"1s"`, 1),
		"null enhanced member":               strings.Replace(enhancedAll, `"application_endpoint":""`, `"application_endpoint":null`, 1),
	} {
		if _, err := collection.DecodePolicy([]byte(data)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

// A declared condition decides whether a stage is answered at all. An unknown
// condition is never treated as a request, and never as a silent pass.
func TestAcknowledgementConditionsAreDeclaredNotGuessed(t *testing.T) {
	for _, valid := range []string{collection.Always, collection.Never, collection.OnError, collection.OnSuccess} {
		if !collection.ValidCondition(valid) {
			t.Errorf("declared condition %q refused", valid)
		}
	}
	for _, invalid := range []string{"", "al", "XX", "AL^", "NEVER"} {
		if collection.ValidCondition(invalid) {
			t.Errorf("undeclared condition %q accepted", invalid)
		}
	}
	for _, c := range []struct {
		condition       string
		success, wanted bool
	}{
		{collection.Always, true, true}, {collection.Always, false, true},
		{collection.Never, true, false}, {collection.Never, false, false},
		{collection.OnError, true, false}, {collection.OnError, false, true},
		{collection.OnSuccess, true, true}, {collection.OnSuccess, false, false},
		{"", true, false}, {"XX", false, false},
	} {
		if got := collection.Requested(c.condition, c.success); got != c.wanted {
			t.Errorf("condition %q success=%t asked for %t, want %t", c.condition, c.success, got, c.wanted)
		}
	}
}
