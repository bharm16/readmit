package replay

import (
	"encoding/json/v2"
	"slices"
	"testing"
)

// FuzzTargetDocument exercises the target configuration reader: the strict
// decode that refuses unknown members, and the validation each declared version
// is held to. No input may panic, an accepted configuration always reports a
// classification from the closed set, and only readmit-target/v3 may report one
// other than unclassified.
func FuzzTargetDocument(f *testing.F) {
	for _, seed := range []string{
		`{"schema":"readmit-target/v1","test_endpoint":true,"address":"127.0.0.1:2575","transport":"plain","approved_transport":false,"connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536}`,
		`{"schema":"readmit-target/v2","test_endpoint":true,"address":"127.0.0.1:2575","transport":"plain","approved_transport":false,"connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536,"credential":{"secrets_file":"secrets.json","reference":"lab-mllp"}}`,
		`{"schema":"readmit-target/v3","test_endpoint":true,"name":"lab-siu","classification":"nonproduction","address":"127.0.0.1:2575","transport":"tls","approved_transport":false,"ca_file":"ca.pem","server_name":"lab.example.invalid","connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536}`,
		`{"schema":"readmit-target/v3","test_endpoint":true,"name":"prod-siu","classification":"production","address":"10.0.0.1:2575","transport":"tls","approved_transport":true,"connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536}`,
		`{"schema":"readmit-target/v3","test_endpoint":true,"name":".","classification":"unclassified","address":"[::1]:2575","transport":"plain","approved_transport":false,"connect_timeout":"1ns","message_timeout":"5m","max_ack_bytes":1}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var config Target
		if err := json.Unmarshal(data, &config, json.RejectUnknownMembers(true)); err != nil {
			return
		}
		if err := validateTarget(config); err != nil {
			return
		}
		named := config.Environment()
		if !slices.Contains(classifications, named.Classification) {
			t.Fatalf("an accepted target reports classification %q, which is outside the closed set", named.Classification)
		}
		if config.Schema != TargetSchemaV3 && (named.Classification != Unclassified || named.Name != "") {
			t.Fatalf("%s reported the environment %+v", config.Schema, named)
		}
	})
}
