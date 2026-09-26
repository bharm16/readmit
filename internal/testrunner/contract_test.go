package testrunner_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestOpenRejectsSeparatelyValidTransformedRunSubstitution(t *testing.T) {
	for _, tc := range []struct {
		transformation replay.Transformation
		controlID      string
	}{
		{replay.Transformation{Name: "rebase-control-ids"}, "READMIT000001"},
		{replay.Transformation{Name: "shift-timestamps", Shift: "24h"}, "LISTEN-BOOK"},
	} {
		t.Run(tc.transformation.Name, func(t *testing.T) {
			dir, path, spec := setup(t)
			spec = ackSpec(t, path, spec, "AA")
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			target(t, dir, listener.Addr().String())
			done := make(chan error, 1)
			go func() {
				// Both executions use this same explicit endpoint. The independently
				// expected control IDs keep ACK correlation valid for each replay.
				for _, controlID := range []string{"LISTEN-BOOK", tc.controlID} {
					conn, err := listener.Accept()
					if err != nil {
						done <- err
						return
					}
					_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
					reader, _ := mllp.NewReader(conn, 1<<20)
					if _, err = reader.ReadFrame(); err == nil {
						_, err = conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AA|" + controlID + "\r")))
					}
					_ = conn.Close()
					if err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			output := filepath.Join(dir, "result")
			original := execute(t, path, output)
			if original.Result.Status != testrunner.Pass {
				t.Fatal("unchanged-input baseline did not pass")
			}
			targetConfig, err := replay.ReadTarget(filepath.Join(dir, "test-target.json"))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := replay.Prepare(filepath.Join(dir, "test-case"), targetConfig, replay.Options{
				Occurrences: spec.Input.Messages, Transformations: []replay.Transformation{tc.transformation},
			})
			if err != nil {
				t.Fatal(err)
			}
			replacementPath := filepath.Join(dir, "transformed-run")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := replay.Send(ctx, plan, replacementPath, replay.SendOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			replacement, err := replay.Open(replacementPath)
			if err != nil || !replacement.Successful() {
				t.Fatalf("replacement must be a separately verified successful replay: %v", err)
			}
			if replacement.Manifest.SourceBundleIdentity != original.Result.InputBundleIdentity || !bytes.Equal(jsonBytes(t, replacement.Manifest.Target), jsonBytes(t, *original.Result.Target)) {
				t.Fatal("regression must preserve both source and target identities")
			}
			if err := os.RemoveAll(filepath.Join(output, "run")); err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(filepath.Join(output, "run"), os.DirFS(replacementPath)); err != nil {
				t.Fatal(err)
			}
			original.Result.Run.Identity = replacement.Identity
			write(t, filepath.Join(output, "result.json"), jsonBytes(t, original.Result))
			rehash(t, output)
			if _, err := testrunner.Open(output); err == nil {
				t.Fatal("v1 test result accepted a transformed replacement run")
			}
			original.Result.Status = testrunner.ExecutionError
			original.Result.ErrorClass = "run_contract"
			for i := range original.Result.Assertions {
				original.Result.Assertions[i].Status = "not_evaluated"
				original.Result.Assertions[i].Observed = nil
			}
			write(t, filepath.Join(output, "result.json"), jsonBytes(t, original.Result))
			rehash(t, output)
			if _, err := testrunner.Open(output); err == nil {
				t.Fatal("changing the verdict must not authorize a forbidden transformed run")
			}
		})
	}
}

func TestSpecRequiresExactExpectedValueMemberPresenceAndTypes(t *testing.T) {
	for _, tc := range []struct {
		name, operator, expected string
		valid                    bool
	}{
		{"present", "ack_field_equals", `{"field":{"state":"present","text":"AA"}}`, true},
		{"null-state", "ack_field_equals", `{"field":{"state":"null"}}`, true},
		{"empty-state", "ack_field_equals", `{"field":{"state":"empty"}}`, true},
		{"omitted-state", "ack_field_equals", `{"field":{"state":"omitted"}}`, true},
		{"zero-count", "ledger_count", `{"count":0}`, true},
		{"empty-records", "ledger_equals", `{"records":[]}`, true},
		{"null-irrelevant-members", "ack_field_equals", `{"count":null,"records":null,"field":{"state":"present","text":"AA"}}`, false},
		{"null-count-with-records", "ledger_equals", `{"count":null,"records":[]}`, false},
		{"null-records-with-count", "ledger_count", `{"count":0,"records":null}`, false},
		{"null-field-with-count", "ledger_count", `{"count":0,"field":null}`, false},
		{"null-count", "ledger_count", `{"count":null}`, false},
		{"string-count", "ledger_count", `{"count":"0"}`, false},
		{"fraction-count", "ledger_count", `{"count":0.5}`, false},
		{"null-records", "ledger_equals", `{"records":null}`, false},
		{"object-records", "ledger_equals", `{"records":{}}`, false},
		{"null-field", "ack_field_equals", `{"field":null}`, false},
		{"unknown-member", "ledger_count", `{"count":0,"ignored":null}`, false},
		{"duplicate-member", "ledger_count", `{"count":0,"count":0}`, false},
		{"null-union", "ledger_count", `null`, false},
		{"empty-union", "ledger_count", `{}`, false},
		{"present-null-text", "ack_field_equals", `{"field":{"state":"present","text":null}}`, false},
		{"present-number-text", "ack_field_equals", `{"field":{"state":"present","text":3}}`, false},
		{"empty-null-text", "ack_field_equals", `{"field":{"state":"empty","text":null}}`, false},
		{"null-null-text", "ack_field_equals", `{"field":{"state":"null","text":null}}`, false},
		{"omitted-null-text", "ack_field_equals", `{"field":{"state":"omitted","text":null}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initialState, observed := "empty-ledger", `{"boundary":"appointment-ledger","path":"observation.json"}`
			selection := ""
			if tc.operator == "ack_field_equals" {
				initialState, observed = "operator-declared", `{"boundary":"ack-contract"}`
				selection = `"message":"s0001-e000001","selector":"MSA-1",`
			}
			raw := []byte(fmt.Sprintf(`{
  "schema":"readmit-test/v1","name":"Synthetic strict value test",
  "input":{"case":"test-case","messages":["s0001-e000001"]},"target":"target.json",
  "setup":{"initial_state":%q,"reset_instructions":"Start a fresh empty synthetic fixture."},
  "observation":%s,
  "assertions":[{"id":"check","operator":%q,%s"expected":%s}]
}`, initialState, observed, tc.operator, selection, tc.expected))
			spec, err := testrunner.DecodeSpec(raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, want %v: %v", err == nil, tc.valid, err)
			}
			if !tc.valid {
				return
			}
			value := spec.Assertions[0].Expected
			if tc.name == "zero-count" && (value.Count == nil || *value.Count != 0) {
				t.Fatal("explicit zero count was lost")
			}
			if tc.name == "empty-records" && (value.Records == nil || len(*value.Records) != 0) {
				t.Fatal("explicit empty record array was lost")
			}
			if tc.name == "null-state" && (value.Field == nil || value.Field.State != "null" || value.Field.Text != nil) {
				t.Fatal("HL7 null state was conflated with JSON null")
			}
		})
	}
}
