package observeinterval_test

import (
	"context"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestRegisteredCaptureInputsRecheckBeforeBindAndDecodeOffline(t *testing.T) {
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address, port := reserve.Addr().String(), reserve.Addr().(*net.TCPAddr).Port
	reserve.Close()
	root := t.TempDir()
	s := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchemaV3, Address: address, RunSelector: "MSH-4", OutputKeySelector: "MSH-10", PhaseKeys: []string{"BOOK-1"}, MaxConnections: 1, MaxSessions: 4, TimeoutMS: 5000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: 10, ReceiverPolicy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "return", SourceLabel: "engine", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}}
	for _, role := range []string{"listener", "responder"} {
		p := filepath.Join(root, role+".json")
		raw := []byte("reviewed " + role)
		if err := os.WriteFile(p, raw, 0600); err != nil {
			t.Fatal(err)
		}
		s.Inputs = append(s.Inputs, observeinterval.CaptureInput{Role: role, Path: p, SHA256: dataset.Digest(raw)})
	}
	raw, _ := json.Marshal(s, json.Deterministic(true))
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "return", Operation: sendpolicy.CaptureListen, Port: port, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	p, err := observeinterval.PrepareCapture(raw, policy, networkaction.Binding{Plan: dataset.Digest([]byte("plan")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "return"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Inputs[0].Path, []byte("changed listener"), 0600); err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "received", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "MSH-10", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	bound := dataset.Binding{Run: "run-one", Phase: "after", Namespace: "received", Source: dataset.Digest(raw)}
	started, err := observeinterval.ArmCapture(context.Background(), raw, p, authority{p.Binding()}, bound, projection, filepath.Join(root, "run"), nil)
	if err == nil {
		started.Close()
		t.Fatal("changed listener bound after preparation")
	}
	if _, err := observeinterval.PrepareCapture(raw, policy, p.Binding()); err == nil {
		t.Fatal("changed live input prepared")
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := observeinterval.DecodeCapture(raw); err != nil {
		t.Fatalf("offline read followed an original host path: %v", err)
	}
	for _, legacy := range []string{observeinterval.CaptureSourceSchema, observeinterval.CaptureSourceSchemaV2} {
		s.Schema = legacy
		old, _ := json.Marshal(s)
		if _, err := observeinterval.DecodeCapture(old); err == nil {
			t.Fatal("frozen capture accepted new live-input members")
		}
	}
}
