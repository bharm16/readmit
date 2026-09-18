package receiver

import (
	"bytes"
	"testing"
)

func TestEmbeddedProfileRejectsUnknownMembersAndUnsupportedOperations(t *testing.T) {
	if _, err := decodeProfile(fixtureProfileJSON); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ name, from, to string }{
		{"unknown member", "{", `{"PRIVATE-TYPO":true,`},
		{"unknown identifier member", `"value": "PID-3.1"`, `"value": "PID-3.1", "PRIVATE-TYPO": true`},
		{"unknown version", `readmit-receiver-profile/v1`, `readmit-receiver-profile/v9`},
		{"unsupported action", `"reschedule"`, `"PRIVATE-CODE"`},
		{"unsupported count operator", `"exact-count"`, `"PRIVATE-CODE"`},
		{"unsupported time operator", `"hl7-ts-whole-seconds-optional-offset"`, `"PRIVATE-CODE"`},
		{"invalid selector", `"PID-3.1"`, `"PID-0.1"`},
		{"invalid text bound", `"max_text_bytes": 1024`, `"max_text_bytes": 2048`},
	} {
		t.Run(change.name, func(t *testing.T) {
			data := bytes.Replace(fixtureProfileJSON, []byte(change.from), []byte(change.to), 1)
			if bytes.Equal(data, fixtureProfileJSON) {
				t.Fatal("profile mutation did not apply")
			}
			if _, err := decodeProfile(data); err == nil {
				t.Fatal("accepted an invalid profile")
			} else if bytes.Contains([]byte(err.Error()), []byte("PRIVATE")) {
				t.Fatal("profile error disclosed its content")
			}
		})
	}
}
