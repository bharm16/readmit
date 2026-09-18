package collection_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
)

// The exact readmit-collection/v1 bytes a released collector wrote. This
// literal is the frozen v1 contract: a reader must still accept it and an
// encoder must reproduce it byte for byte.
const recordV1 = `{"schema":"readmit-collection/v1","session_id":"abababababababababababababababab","policy":` + acceptAll +
	`,"application_processing":"none","sessions":[{"session_id":"c0001","source_id":"s0001","label":"downstream-test-endpoint"}],` +
	`"received":[{"session_id":"c0001","occurrence_id":"s0001-e000001","control_id":"MSG00001","acknowledgement":"AA","reason":""}]}` + "\n"

func policy(t *testing.T, declared string) collection.Policy {
	t.Helper()
	p, err := collection.DecodePolicy([]byte(declared))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sample(t *testing.T) collection.Record {
	t.Helper()
	declared := policy(t, enhancedAll)
	return collection.Record{
		Schema:                collection.Schema,
		SessionID:             strings.Repeat("ab", 16),
		Policy:                declared,
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{{SessionID: "c0001", SourceID: "s0001", Label: declared.SourceLabel}},
		Received: []collection.Received{{
			SessionID: "c0001", OccurrenceID: "s0001-e000001", ControlID: "MSG00001",
			Mode:        collection.EnhancedMode,
			Accept:      collection.Stage{Code: collection.CommitAcceptCode, ControlID: "READMITACC000001", Destination: collection.SameConnection},
			Application: collection.Stage{Code: collection.AcceptCode, ControlID: "READMITAPP000001", Destination: collection.SeparateEndpoint},
		}},
	}
}

func TestRecordRoundTripsExactlyAndNamesReceiptOnly(t *testing.T) {
	record := sample(t)
	data, err := collection.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"application_processing":"none"`)) {
		t.Fatalf("record does not state that no application processing occurred: %s", data)
	}
	decoded, err := collection.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := collection.Encode(decoded)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("record round-trip changed the collected evidence")
	}
}

// readmit-collection/v1 is frozen. It must still decode, and re-encode to the
// same bytes, with no v2 member added to it.
func TestRecordV1RemainsReadableAndUnchanged(t *testing.T) {
	decoded, err := collection.Decode([]byte(recordV1))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != collection.SchemaV1 || len(decoded.Received) != 1 {
		t.Fatalf("v1 record did not decode as itself: %+v", decoded)
	}
	// A v1 record could express one original-mode application acknowledgement
	// on the receiving connection, and nothing else.
	entry := decoded.Received[0]
	if entry.Mode != collection.OriginalMode || entry.Application.Code != collection.AcceptCode || entry.Application.Destination != collection.SameConnection {
		t.Fatalf("v1 receipt was not read as an original-mode application acknowledgement: %+v", entry)
	}
	if entry.Accept.Code != collection.NotAcknowledged || entry.Accept.Destination != collection.NoDestination {
		t.Fatalf("v1 receipt invented an accept stage: %+v", entry.Accept)
	}
	encoded, err := collection.Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != recordV1 {
		t.Fatalf("v1 record did not re-encode byte-identically:\n got %s\nwant %s", encoded, recordV1)
	}
	for _, added := range []string{`"mode"`, `"accept"`, `"application"`} {
		if bytes.Contains(encoded, []byte(added)) {
			t.Errorf("v1 record acquired the v2 member %s", added)
		}
	}
}

// A v1 record cannot carry anything v1 could not express, and cannot carry a
// v2 member even when the value would otherwise be valid.
func TestRecordV1RefusesV2Evidence(t *testing.T) {
	for name, mutated := range map[string]string{
		"stage members":            strings.Replace(recordV1, `"acknowledgement":"AA","reason":""`, `"mode":"enhanced","accept":{"code":"CA","control_id":"X","destination":"same-connection","reason":""},"application":{"code":"AA","control_id":"Y","destination":"same-connection","reason":""}`, 1),
		"added mode member":        strings.Replace(recordV1, `"acknowledgement":"AA"`, `"mode":"original","acknowledgement":"AA"`, 1),
		"v2 policy in a v1 record": strings.Replace(recordV1, acceptAll, enhancedAll, 1),
	} {
		if _, err := collection.Decode([]byte(mutated)); err == nil {
			t.Errorf("accepted %s in a v1 record", name)
		}
	}
	decoded, err := collection.Decode([]byte(recordV1))
	if err != nil {
		t.Fatal(err)
	}
	decoded.Received[0].Accept = collection.Stage{Code: collection.CommitAcceptCode, ControlID: "X", Destination: collection.SameConnection}
	if _, err := collection.Encode(decoded); err == nil {
		t.Error("encoded an accept stage into a v1 record")
	}
}

// Commit-accept is not application-accept. The record has no way to write a
// commit code into the application stage, or an application code into the
// accept stage, in either direction.
func TestRecordKeepsAcceptAndApplicationStagesApart(t *testing.T) {
	valid, err := collection.Encode(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"application code in the accept stage":  strings.Replace(string(valid), `"accept":{"code":"CA"`, `"accept":{"code":"AA"`, 1),
		"commit code in the application stage":  strings.Replace(string(valid), `"application":{"code":"AA"`, `"application":{"code":"CA"`, 1),
		"commit error as an application result": strings.Replace(string(valid), `"application":{"code":"AA"`, `"application":{"code":"CE"`, 1),
		"application error as a commit result":  strings.Replace(string(valid), `"accept":{"code":"CA"`, `"accept":{"code":"AE"`, 1),
		"accept stage on a separate endpoint":   strings.Replace(string(valid), `"control_id":"READMITACC000001","destination":"same-connection"`, `"control_id":"READMITACC000001","destination":"separate-endpoint"`, 1),
	} {
		if _, err := collection.Decode([]byte(mutated)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestRecordRejectsUnsupportedOrIncoherentEvidence(t *testing.T) {
	valid, err := collection.Encode(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"unknown member":                strings.Replace(string(valid), `"policy"`, `"mode":"fixed","policy"`, 1),
		"unknown schema":                strings.Replace(string(valid), "readmit-collection/v2", "readmit-collection/v3", 1),
		"claimed processing":            strings.Replace(string(valid), `"application_processing":"none"`, `"application_processing":"applied"`, 1),
		"unknown session":               strings.Replace(string(valid), `"session_id":"c0001","occurrence_id"`, `"session_id":"c0002","occurrence_id"`, 1),
		"out of order sessions":         strings.Replace(string(valid), `"session_id":"c0001","source_id":"s0001"`, `"session_id":"c0002","source_id":"s0002"`, 1),
		"unsupported mode":              strings.Replace(string(valid), `"mode":"enhanced"`, `"mode":"assumed"`, 1),
		"accept stage in original mode": strings.Replace(string(valid), `"mode":"enhanced"`, `"mode":"original"`, 1),
		"separate endpoint in original mode": strings.Replace(strings.Replace(string(valid), `"mode":"enhanced"`, `"mode":"original"`, 1),
			`"accept":{"code":"CA","control_id":"READMITACC000001","destination":"same-connection","reason":""}`, `"accept":{"code":"none","control_id":"","destination":"none","reason":""}`, 1),
		"stages under an unknown mode":  strings.Replace(string(valid), `"mode":"enhanced"`, `"mode":"unknown"`, 1),
		"anonymous acknowledgement":     strings.Replace(string(valid), `"control_id":"READMITAPP000001"`, `"control_id":""`, 1),
		"undelivered acknowledgement":   strings.Replace(string(valid), `"destination":"separate-endpoint"`, `"destination":"none"`, 1),
		"unsupported destination":       strings.Replace(string(valid), `"destination":"separate-endpoint"`, `"destination":"another-process"`, 1),
		"reason on acceptance":          strings.Replace(string(valid), `"code":"AA","control_id":"READMITAPP000001","destination":"separate-endpoint","reason":""`, `"code":"AA","control_id":"READMITAPP000001","destination":"separate-endpoint","reason":"rejected"`, 1),
		"reason on commit acceptance":   strings.Replace(string(valid), `"code":"CA","control_id":"READMITACC000001","destination":"same-connection","reason":""`, `"code":"CA","control_id":"READMITACC000001","destination":"same-connection","reason":"refused"`, 1),
		"answered without a control ID": strings.Replace(string(valid), `"control_id":"MSG00001"`, `"control_id":""`, 1),
		"malformed occurrence":          strings.Replace(string(valid), "s0001-e000001", "s1-e1", 1),
		"invalid session id":            strings.Replace(string(valid), strings.Repeat("ab", 16), "AB", 1),
		"invalid policy":                strings.Replace(string(valid), "original-mode-fixed-code", "original-mode-any-code", 1),
		"missing member":                strings.Replace(string(valid), `"sessions":[{"session_id":"c0001","source_id":"s0001","label":"downstream-test-endpoint"}],`, "", 1),
		"missing stage member":          strings.Replace(string(valid), `"accept":{"code":"CA","control_id":"READMITACC000001","destination":"same-connection","reason":""}`, `"accept":{"code":"CA","control_id":"READMITACC000001","destination":"same-connection"}`, 1),
		"null stage":                    strings.Replace(string(valid), `"accept":{"code":"CA","control_id":"READMITACC000001","destination":"same-connection","reason":""}`, `"accept":null`, 1),
		"unprintable reason":            strings.Replace(string(valid), `"destination":"separate-endpoint","reason":""`, `"destination":"separate-endpoint","reason":"a`+``+`b"`, 1),
		"unlabelled source":             strings.Replace(string(valid), `"label":"downstream-test-endpoint"`, `"label":""`, 1),
		"foreign source":                strings.Replace(string(valid), `"occurrence_id":"s0001-e000001"`, `"occurrence_id":"s0002-e000001"`, 1),
	} {
		if _, err := collection.Decode([]byte(mutated)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

// A frame that could not be read, or a sender that asked for no answer at all,
// is retained explicitly. Neither is a pass and neither is a negative result.
func TestRecordRetainsUnansweredFramesExplicitly(t *testing.T) {
	record := sample(t)
	record.Received = append(record.Received,
		collection.Received{SessionID: "c0001", OccurrenceID: "s0001-e000002", Mode: collection.UnknownMode,
			Accept:      collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: "frame is not supported HL7 syntax"},
			Application: collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: "frame is not supported HL7 syntax"}},
		collection.Received{SessionID: "c0001", OccurrenceID: "s0001-e000003", ControlID: "MSG00003", Mode: collection.EnhancedMode,
			Accept:      collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: "sender declared no accept acknowledgement"},
			Application: collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: "sender declared no application acknowledgement"}})
	data, err := collection.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := collection.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Received) != 3 {
		t.Fatalf("unanswered frames were not retained: %+v", decoded.Received)
	}
	unreadable := decoded.Received[1]
	if unreadable.Mode != collection.UnknownMode || unreadable.ControlID != "" || unreadable.Application.Code != collection.NotAcknowledged {
		t.Fatalf("an unreadable frame is not retained explicitly: %+v", unreadable)
	}
	declined := decoded.Received[2]
	if declined.Mode != collection.EnhancedMode || declined.Application.Code != collection.NotAcknowledged || declined.Application.Reason == "" {
		t.Fatalf("a declined stage is not retained explicitly: %+v", declined)
	}
}

func FuzzCollection(f *testing.F) {
	base := collection.Record{
		Schema: collection.Schema, SessionID: strings.Repeat("ab", 16),
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{}, Received: []collection.Received{},
		Policy: collection.Policy{Schema: collection.PolicySchema, Name: "sink", SourceLabel: "downstream",
			Acknowledgement:      collection.AckRule{Operator: collection.FixedCodeOperator, Code: collection.AcceptCode},
			AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}},
			Enhanced: &collection.EnhancedRule{Operator: collection.EnhancedFixedCodes, AcceptCode: collection.CommitAcceptCode,
				ApplicationCode: collection.AcceptCode, ApplicationDelivery: collection.SameConnection}},
	}
	valid, err := collection.Encode(base)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	faultDeclared, err := collection.DecodePolicy([]byte(faultPolicy))
	if err != nil {
		f.Fatal(err)
	}
	base.Schema, base.Policy = collection.FaultSchema, faultDeclared
	faultRecord, err := collection.Encode(base)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(faultRecord)
	f.Add([]byte(recordV1))
	f.Add([]byte(`{"schema":"readmit-collection/v2","received":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		record, err := collection.Decode(data)
		if err != nil {
			return
		}
		if record.Validate() != nil {
			t.Fatal("decoded collection record is not valid")
		}
		// An accepted record must not change meaning when it is re-encoded.
		encoded, err := collection.Encode(record)
		if err != nil {
			t.Fatal(err)
		}
		again, err := collection.Decode(encoded)
		if err != nil {
			t.Fatalf("collection round-trip changed the record: %v", err)
		}
		repeated, err := collection.Encode(again)
		if err != nil || !bytes.Equal(encoded, repeated) {
			t.Fatal("collection round-trip changed the record")
		}
	})
}
