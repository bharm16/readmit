package connectedtransport_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func write(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func prepared(t *testing.T, address string, configure ...func(string, *replay.Target, *connectedtransport.Selection)) (*connectedtransport.Prepared, connectedtransport.Selection, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "case")
	now := time.Now()
	raw := []byte("MSH|^~\\&|SYNTH|LAB|TARGET|LAB|20260101120000+0000||SIU^S12|DUPLICATE|P|2.5.1\rSCH|APT1\r")
	b, err := bundle.Write(source, []bundle.Input{{Path: "synthetic-fixture.hl7", Data: append(mllp.Frame(raw), mllp.Frame(raw)...)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "Lab", Classification: replay.Nonproduction, TestEndpoint: true, Address: address, Transport: "plain", ApprovedTransport: true, ConnectTimeout: "2s", MessageTimeout: "2s", MaxACKBytes: 4096}
	s := connectedtransport.Selection{Case: source, Target: filepath.Join(dir, "target.json"), Policy: filepath.Join(dir, "policy.json")}
	for _, configure := range configure {
		configure(dir, &target, &s)
	}
	write(t, s.Target, target)
	rp, err := replay.PrepareScoped(source, target, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	ip, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		ip = netip.MustParseAddr("127.0.0.1")
	}
	bits := 128
	if ip.Is4() {
		bits = 32
	}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: n, Destinations: []string{netip.PrefixFrom(ip, bits).String()}, Selection: "single-address"}}}
	write(t, s.Policy, policy)
	policyRaw, _ := os.ReadFile(s.Policy)
	checks := []byte(`{"schema":"readmit-assertion-set/v1","name":"Independent check","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"s0001-e000001","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`)
	files := map[string][]byte{"input.hl7": raw, "checks.json": checks, "policy.json": policyRaw}
	ref := func(id, schema, file string) connectedtest.Reference {
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: file, SHA256: connectedtest.Digest(files[file])}
	}
	d := connectedtest.Test{Schema: connectedtest.TestSchema, Project: "lab", ID: "duplicate", Revision: "1", Environment: connectedtest.Environment{Project: "lab", ID: "test", Revision: "1", Name: "Lab", Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: connectedtest.Digest(policyRaw), Grants: []connectedtest.Reference{ref("policy", sendpolicy.ScopedPolicySchema, "policy.json")}, TLS: connectedtest.TLS{Mode: target.Transport, ServerName: target.ServerName}, TargetRevision: connectedtest.TargetRevision{Provenance: "unknown"}}, Setup: connectedtest.Setup{Kind: "operator-declared", Isolation: "dedicated", Instructions: "Independent receiver"}, Checks: ref("checks", "readmit-assertion-set/v1", "checks.json"), Datasets: []connectedtest.Dataset{{ID: "acks", Kind: "v2-messages", Phase: "after", Source: "receiver", Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: 3000, MaxRecords: 10, MaxBytes: 65536}}}, Bindings: connectedtest.Bindings{Observed: "acks"}, OperatorVersion: connectedtest.OperatorVersion, Limits: connectedtest.Limits{MaxSteps: 10, MaxBytes: 1 << 20, DeadlineMS: 5000}}
	for i, e := range b.Events {
		d.Steps = append(d.Steps, connectedtest.Step{ID: fmt.Sprintf("step%d", i), Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: ref("input", "hl7", "input.hl7"), Occurrence: e.ID}})
	}
	if target.ClientCertificate != "" {
		d.Environment.TLS.Mode = "mtls"
	}
	testRaw, _ := json.Marshal(d)
	plan, err := connectedtest.Compile(testRaw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := connectedtransport.Prepare(plan, s)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, dir
}
func grant(t *testing.T, p *connectedtransport.Prepared, dir string) connectedtransport.FileAuthority {
	t.Helper()
	path := filepath.Join(dir, "grant.json")
	write(t, path, connectedtransport.RunnerGrant{Schema: connectedtransport.GrantSchema, Actor: "runner", Generation: "1", Binding: p.Binding(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	return connectedtransport.FileAuthority{Path: path, Actor: "runner", Generation: "1"}
}
func serve(t *testing.T, listener net.Listener, disconnect bool) *atomic.Int32 {
	t.Helper()
	count := new(atomic.Int32)
	t.Cleanup(func() { listener.Close() })
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(c, 65536)
		for {
			_, err := reader.ReadFrame()
			if err != nil {
				return
			}
			count.Add(1)
			if disconnect {
				return
			}
			_, err = c.Write(mllp.Frame([]byte("MSH|^~\\&|TARGET|LAB|SYNTH|LAB|20260101120000+0000||ACK^S12|ACK1|P|2.5.1\rMSA|AA|DUPLICATE\r")))
			if err != nil {
				return
			}
		}
	}()
	return count
}
func TestConnectedTransportSettlesExplicitDuplicatesAndReopensOffline(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := serve(t, l, false)
	p, _, dir := prepared(t, l.Addr().String())
	authority := grant(t, p, dir)
	output := filepath.Join(dir, "result")
	r, err := connectedtransport.Execute(context.Background(), p, authority, "run1", output, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "settled" || r.ApplicationVerdict != "not-evaluated" || count.Load() != 2 {
		t.Fatalf("receipt=%+v sends=%d", r, count.Load())
	}
	l.Close()
	os.Remove(authority.Path)
	reopened, err := connectedtransport.Open(output)
	if err != nil || reopened.RunIdentity != r.RunIdentity {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := connectedtransport.Execute(context.Background(), p, authority, "run1", output, nil); err == nil {
		t.Fatal("resent without grant")
	}
}
func TestConnectedTransportDisconnectRemainsUncertainWithoutResend(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := serve(t, l, true)
	p, _, dir := prepared(t, l.Addr().String())
	authority := grant(t, p, dir)
	out := filepath.Join(dir, "result")
	r, err := connectedtransport.Execute(context.Background(), p, authority, "run1", out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "uncertain" || count.Load() != 1 {
		t.Fatalf("%+v count=%d", r, count.Load())
	}
	if _, err = connectedtransport.Open(out); err != nil {
		t.Fatal(err)
	}
	if _, err = connectedtransport.Execute(context.Background(), p, authority, "run1", out, nil); err == nil {
		t.Fatal("reused output")
	}
	if count.Load() != 1 {
		t.Fatal("automatic resend")
	}
}
func TestConnectedTransportRejectsChangedPolicyAndStaleActorBeforeEffects(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := serve(t, l, false)
	_, port, _ := net.SplitHostPort(l.Addr().String())
	p, s, dir := prepared(t, net.JoinHostPort("receiver.invalid", port))
	a := grant(t, p, dir)
	calls := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	a.Generation = "2"
	if _, err := connectedtransport.Execute(context.Background(), p, a, "run1", filepath.Join(dir, "denied"), resolve); err == nil {
		t.Fatal("stale grant accepted")
	}
	a.Generation = "1"
	policy, _ := os.ReadFile(s.Policy)
	os.WriteFile(s.Policy, []byte("{}"), 0600)
	if _, err := connectedtransport.Execute(context.Background(), p, a, "run2", filepath.Join(dir, "changed"), resolve); err == nil {
		t.Fatal("changed policy accepted")
	}
	if calls != 0 || count.Load() != 0 {
		t.Fatal("effects before approval")
	}
	os.WriteFile(s.Policy, policy, 0600)
	r, err := connectedtransport.Execute(context.Background(), p, a, "run3", filepath.Join(dir, "approved"), resolve)
	if err != nil || r.State != "settled" || calls != 1 || count.Load() != 2 {
		t.Fatalf("authorized control: %v state=%s DNS=%d messages=%d", err, r.State, calls, count.Load())
	}
}

func TestConnectedTransportPrivateNetworkListenerRequiresExplicitPolicy(t *testing.T) {
	if os.Getenv("READMIT_PRIVATE_LISTENER_TEST") != "1" {
		t.Skip("explicit isolated private-network qualification")
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var host string
	for _, a := range addresses {
		p, err := netip.ParsePrefix(a.String())
		if err == nil && p.Addr().Is4() && p.Addr().IsPrivate() && !p.Addr().IsLoopback() {
			host = p.Addr().String()
			break
		}
	}
	if host == "" {
		t.Skip("no private IPv4 interface available")
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	count := serve(t, l, false)
	p, s, dir := prepared(t, l.Addr().String())
	a := grant(t, p, dir)
	original, _ := os.ReadFile(s.Policy)
	os.WriteFile(s.Policy, []byte("{}"), 0600)
	if _, err := connectedtransport.Execute(context.Background(), p, a, "denied", filepath.Join(dir, "denied"), nil); err == nil {
		t.Fatal("private send without policy")
	}
	if count.Load() != 0 {
		t.Fatal("unapproved connection")
	}
	os.WriteFile(s.Policy, original, 0600)
	r, err := connectedtransport.Execute(context.Background(), p, a, "approved", filepath.Join(dir, "approved"), nil)
	if err != nil || r.State != "settled" || count.Load() != 2 {
		t.Fatalf("private listener: %v %+v count=%d", err, r, count.Load())
	}
	t.Log("authorized real private-network listener received two authored messages")
}
