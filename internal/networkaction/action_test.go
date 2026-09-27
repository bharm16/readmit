package networkaction_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type authority struct {
	binding networkaction.Binding
	actor   networkaction.Actor
	revoked *atomic.Bool
}

func (a authority) Check(_ context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if a.revoked.Load() || b != a.binding {
		return networkaction.Actor{}, context.Canceled
	}
	return a.actor, nil
}
func approved(b networkaction.Binding) authority {
	return authority{binding: b, revoked: new(atomic.Bool), actor: networkaction.Actor{Kind: "runner", ID: "runner", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("grant")), Expires: time.Now().Add(time.Hour)}}
}
func policy(t *testing.T, address string, operation sendpolicy.Operation) []byte {
	t.Helper()
	host, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	ip := netip.MustParseAddr(host)
	bits := 128
	if ip.Is4() {
		bits = 32
	}
	raw, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "endpoint", Operation: operation, Port: number, Destinations: []string{netip.PrefixFrom(ip, bits).String()}, Selection: "single-address"}}})
	return raw
}
func TestEveryHTTPPurposeExecutesOnlyItsExactBoundOperation(t *testing.T) {
	for _, op := range []sendpolicy.Operation{sendpolicy.ObservationRead, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.SMARTToken, sendpolicy.SetupAction} {
		t.Run(string(op), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Set-Cookie", "sensitive-cookie")
				w.Header().Set("Authorization", "sensitive-header")
				w.Write([]byte(`{"value":"synthetic"}`))
			}))
			defer server.Close()
			method := "GET"
			if op == sendpolicy.FHIRAction || op == sendpolicy.SMARTToken || op == sendpolicy.SetupAction {
				method = "POST"
			}
			spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("compiled")), Source: networkaction.Digest([]byte("data")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: op, Method: method, URL: server.URL + "/resource", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}
			raw, _ := json.Marshal(spec)
			p, err := networkaction.PrepareHTTP(raw, policy(t, server.Listener.Addr().String(), op))
			if err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 0 {
				t.Fatal("preparation connected")
			}
			a := approved(p.Binding())
			wrong := a
			wrong.binding.Operation = sendpolicy.CaptureListen
			if _, _, err := p.Execute(context.Background(), wrong, filepath.Join(t.TempDir(), "denied"), nil); err == nil || requests.Load() != 0 {
				t.Fatal("different purpose authorized request")
			}
			output := filepath.Join(t.TempDir(), "result")
			response, result, err := p.Execute(context.Background(), a, output, nil)
			if err != nil || response.Status != 200 || result.State != "responded" || requests.Load() != 1 {
				t.Fatal(err, result)
			}
			if _, err := networkaction.OpenHTTP(output); err != nil {
				t.Fatal("reopen", err)
			}
			if strings.Contains(fmt.Sprintf("%+v %#v", response, response), "sensitive-") {
				t.Fatal("response formatting exposed headers")
			}
			if op == sendpolicy.SMARTToken && result.ResponseRetained {
				t.Fatal("token response retained")
			}
			a.revoked.Store(true)
			if _, _, err = p.Execute(context.Background(), a, filepath.Join(t.TempDir(), "revoked"), nil); err == nil || requests.Load() != 1 {
				t.Fatal("revoked actor reused")
			}
		})
	}
}
func TestScopedCaptureUsesExistingCollectorAndRetainsOccurrenceBytes(t *testing.T) {
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	spec := networkaction.CaptureSpec{Schema: networkaction.CaptureActionSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("capture")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Address: address, TimeoutMS: 3000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: 1, Policy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "independent", SourceLabel: "synthetic", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}}
	raw, _ := json.Marshal(spec)
	p, err := networkaction.PrepareCapture(raw, policy(t, address, sendpolicy.CaptureListen))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "capture")
	session, err := p.Start(context.Background(), approved(p.Binding()), output, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Stop()
	conn, err := net.Dial("tcp", session.Address())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	wire := mllp.Frame([]byte("MSH|^~\\&|SYNTH|LAB|TARGET|LAB|20260101000000||ADT^A01|SAME|P|2.5.1\rPID|1\r"))
	conn.Write(wire)
	reader, _ := mllp.NewReader(conn, 4096)
	if _, err = reader.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	result, err := session.Wait()
	if err != nil || result == nil || len(result.Events) < 1 {
		t.Fatal(err)
	}
	if _, reopened, err := networkaction.OpenCapture(output); err != nil || reopened.Identity != result.Identity {
		t.Fatal("capture authority reopen", err)
	}
}

type revokeAfterDecision struct {
	base  authority
	calls int
}

func (a *revokeAfterDecision) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	a.calls++
	if a.calls >= 3 {
		return networkaction.Actor{}, context.Canceled
	}
	return a.base.Check(ctx, b)
}
func TestAllowedDestinationThenRevokedAuthorityRetainsReadableRefusal(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("revoked action connected") }))
	defer server.Close()
	spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("data")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRSearch, Method: "GET", URL: server.URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}
	raw, _ := json.Marshal(spec)
	p, err := networkaction.PrepareHTTP(raw, policy(t, server.Listener.Addr().String(), sendpolicy.FHIRSearch))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "refused")
	_, r, err := p.Execute(context.Background(), &revokeAfterDecision{base: approved(p.Binding())}, output, nil)
	if err == nil || r.State != "refused" {
		t.Fatal("revocation was not refused")
	}
	if _, err := networkaction.OpenHTTP(output); err != nil {
		t.Fatal("refusal unreadable", err)
	}
	// Re-sealing a contradictory decision cannot make it real approval.
	b, _ := os.ReadFile(filepath.Join(output, "decision.json"))
	var d sendpolicy.ScopedDecision
	json.Unmarshal(b, &d)
	d.Address = "192.0.2.1:443"
	b, _ = json.Marshal(d, json.Deterministic(true))
	os.WriteFile(filepath.Join(output, "decision.json"), b, 0600)
	files := map[string][]byte{}
	entries, _ := os.ReadDir(output)
	for _, e := range entries {
		if !e.IsDir() {
			files[e.Name()], _ = os.ReadFile(filepath.Join(output, e.Name()))
		}
	}
	identity := artifactdir.Identity(networkaction.ResultSchema, files)
	os.WriteFile(filepath.Join(output, "identity.sha256"), []byte(identity+"\n"), 0600)
	if _, err := networkaction.OpenHTTP(output); err == nil {
		t.Fatal("contradictory decision accepted")
	}
}

type revokeBeforeDecision struct {
	base  authority
	calls int
}

func (a *revokeBeforeDecision) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	a.calls++
	if a.calls >= 2 {
		return networkaction.Actor{}, context.Canceled
	}
	return a.base.Check(ctx, b)
}
func TestRevocationBeforeDestinationDecisionLeavesNoCompletionMarker(t *testing.T) {
	spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("data")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRSearch, Method: "GET", URL: "https://127.0.0.1:9443", ServerName: "example.com", TimeoutMS: 1000, MaxBytes: 4096}
	raw, _ := json.Marshal(spec)
	p, err := networkaction.PrepareHTTP(raw, policy(t, "127.0.0.1:9443", sendpolicy.FHIRSearch))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "interrupted")
	if _, _, err = p.Execute(context.Background(), &revokeBeforeDecision{base: approved(p.Binding())}, output, nil); err == nil {
		t.Fatal("revoked action accepted")
	}
	if _, err := os.Stat(filepath.Join(output, "identity.sha256")); !os.IsNotExist(err) {
		t.Fatal("pre-decision refusal incorrectly sealed")
	}
}
