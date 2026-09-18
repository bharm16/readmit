package collection_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
)

func sample(t *testing.T) collection.Record {
	t.Helper()
	policy, err := collection.DecodePolicy([]byte(acceptAll))
	if err != nil {
		t.Fatal(err)
	}
	return collection.Record{
		Schema:                collection.Schema,
		SessionID:             strings.Repeat("ab", 16),
		Policy:                policy,
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{{SessionID: "c0001", SourceID: "s0001", Label: policy.SourceLabel}},
		Received:              []collection.Received{{SessionID: "c0001", OccurrenceID: "s0001-e000001", ControlID: "MSG00001", Acknowledgement: "AA"}},
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

func TestRecordRejectsUnsupportedOrIncoherentEvidence(t *testing.T) {
	valid, err := collection.Encode(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"unknown member":        strings.Replace(string(valid), `"policy"`, `"mode":"fixed","policy"`, 1),
		"unknown schema":        strings.Replace(string(valid), "readmit-collection/v1", "readmit-collection/v2", 1),
		"claimed processing":    strings.Replace(string(valid), `"application_processing":"none"`, `"application_processing":"applied"`, 1),
		"unknown session":       strings.Replace(string(valid), `"session_id":"c0001","occurrence_id"`, `"session_id":"c0002","occurrence_id"`, 1),
		"out of order sessions": strings.Replace(string(valid), `"session_id":"c0001","source_id":"s0001"`, `"session_id":"c0002","source_id":"s0002"`, 1),
		"unsupported code":      strings.Replace(string(valid), `"acknowledgement":"AA"`, `"acknowledgement":"CA"`, 1),
		"reason on acceptance":  strings.Replace(string(valid), `"reason":""`, `"reason":"rejected"`, 1),
		"anonymous acceptance":  strings.Replace(string(valid), `"control_id":"MSG00001"`, `"control_id":""`, 1),
		"malformed occurrence":  strings.Replace(string(valid), "s0001-e000001", "s1-e1", 1),
		"invalid session id":    strings.Replace(string(valid), strings.Repeat("ab", 16), "AB", 1),
		"invalid policy":        strings.Replace(string(valid), "original-mode-fixed-code", "original-mode-any-code", 1),
		"missing member":        strings.Replace(string(valid), `"sessions":[{"session_id":"c0001","source_id":"s0001","label":"downstream-test-endpoint"}],`, "", 1),
		"null received":         strings.Replace(string(valid), `"received":[{"session_id":"c0001","occurrence_id":"s0001-e000001","control_id":"MSG00001","acknowledgement":"AA","reason":""}]`, `"received":null`, 1),
		"unprintable reason":    strings.Replace(string(valid), `"reason":""`, `"reason":"a\u0007b"`, 1),
		"duplicate occurrence":  strings.Replace(string(valid), `"acknowledgement":"AA","reason":""}]`, `"acknowledgement":"AA","reason":""},{"session_id":"c0001","occurrence_id":"s0001-e000001","control_id":"MSG00001","acknowledgement":"AA","reason":""}]`, 1),
		"unlabelled source":     strings.Replace(string(valid), `"label":"downstream-test-endpoint"`, `"label":""`, 1),
		"foreign source":        strings.Replace(string(valid), `"occurrence_id":"s0001-e000001"`, `"occurrence_id":"s0002-e000001"`, 1),
	} {
		if _, err := collection.Decode([]byte(mutated)); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestRecordRetainsUnacknowledgedFramesExplicitly(t *testing.T) {
	record := sample(t)
	record.Received = append(record.Received, collection.Received{SessionID: "c0001", OccurrenceID: "s0001-e000002", Acknowledgement: collection.NotAcknowledged, Reason: "message header cannot be acknowledged safely"})
	data, err := collection.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := collection.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Received) != 2 || decoded.Received[1].Acknowledgement != collection.NotAcknowledged || decoded.Received[1].ControlID != "" {
		t.Fatalf("unacknowledged frame is not retained explicitly: %+v", decoded.Received)
	}
}

func FuzzCollection(f *testing.F) {
	valid, err := collection.Encode(collection.Record{
		Schema: collection.Schema, SessionID: strings.Repeat("ab", 16),
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{}, Received: []collection.Received{},
		Policy: collection.Policy{Schema: collection.PolicySchema, Name: "sink", SourceLabel: "downstream",
			Acknowledgement:      collection.AckRule{Operator: collection.FixedCodeOperator, Code: collection.AcceptCode},
			AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}},
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schema":"readmit-collection/v1","received":[]}`))
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
