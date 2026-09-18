package capturejournal

import (
	"encoding/json/v2"
	"testing"
)

// FuzzCapturePlan exercises the capture plan reader on arbitrary bytes. A plan
// is read back from disk after an interruption, so it is attacker-reachable in
// exactly the way any other artifact reader is: it must refuse, never panic and
// never accept a document it cannot validate.
func FuzzCapturePlan(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-capture-journal/v1","created_at":"2026-09-18T00:00:00Z","session_id":"0123456789abcdef0123456789abcdef","policy_name":"sink","policy_schema":"readmit-receiver-policy/v2","policy_sha256":"` + digest(nil) + `","limits":{"max_connections":1,"max_sessions":0,"max_messages":0,"max_capture_bytes":0,"max_frame_bytes":1024},"transport":{"tls":false,"client_certificate":false}}`))
	f.Add([]byte(`{"schema":"readmit-capture-journal/v1"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var capture Capture
		if json.Unmarshal(data, &capture) != nil {
			return
		}
		if err := capture.validate(); err != nil {
			return
		}
		if capture.Schema != Schema || capture.Limits.MaxConnections < 1 || capture.Limits.MaxFrameBytes < 1 {
			t.Fatalf("a validated plan declared an unusable capacity: %+v", capture)
		}
	})
}

// FuzzJournalRecord exercises one journal line. Recovery reads these after a
// crash, so an unreadable or unknown record must be refused rather than
// partially believed.
func FuzzJournalRecord(f *testing.F) {
	f.Add([]byte(`{"sequence":1,"previous":"` + digest(nil) + `","at":"2026-09-18T00:00:00Z","kind":"ready"}`))
	f.Add([]byte(`{"sequence":3,"previous":"` + digest(nil) + `","at":"2026-09-18T00:00:00Z","kind":"received","session":"c0001","occurrence":"s0001-e000001","frame":{"path":"received/s0001-e000001.bin","size":1,"sha256":"` + digest(nil) + `"}}`))
	f.Add([]byte(`{"kind":"finished"}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var record entry
		if json.Unmarshal(data, &record) != nil {
			return
		}
		key, ok := stageKey(record)
		if !ok {
			return
		}
		// A record recovery can pair as an intent with a completion must name
		// one connection, one occurrence and one of the two declared stages,
		// and must carry neither a retained frame nor a terminal summary.
		if record.Stage != AcceptStage && record.Stage != ApplicationStage {
			t.Fatalf("a record was keyed to a stage outside the declared two: %q", record.Stage)
		}
		if !connectionPattern.MatchString(record.Session) || !occurrencePattern.MatchString(record.Occurrence) {
			t.Fatalf("a record was keyed without naming its connection and occurrence: %q", key)
		}
		if record.Frame != nil || record.Final != nil || record.ControlID != "" {
			t.Fatalf("an acknowledgement record carried evidence of another kind: %q", key)
		}
	})
}
