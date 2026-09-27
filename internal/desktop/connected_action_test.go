package desktop_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/secret"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func connectedReview(t *testing.T, app *desktop.App, ctx desktop.RequestContext, address, planEntry string, configure ...func(string, *replay.Target)) desktop.PrepareActionRequest {
	t.Helper()
	incident, lab := sendProject(t, app, ctx, address)
	root := ctx.Project
	targetFile := filepath.Join(root, "lab.json")
	rawTarget, _ := os.ReadFile(targetFile)
	var selected replay.Target
	if err := json.Unmarshal(rawTarget, &selected); err != nil {
		t.Fatal(err)
	}
	selected.ApprovedTransport = true
	for _, f := range configure {
		f(root, &selected)
	}
	if err := replay.WriteTarget(targetFile, selected); err != nil {
		t.Fatal(err)
	}
	lab = listed(t, app, root, desktop.EnvironmentItem)["lab-replay"].Ref
	target, err := replay.ReadTarget(filepath.Join(root, "lab.json"))
	if err != nil {
		t.Fatal(err)
	}
	rp, err := replay.PrepareScoped(filepath.Join(root, "incident"), target, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(address)
	n, _ := strconv.Atoi(port)
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: n, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	os.WriteFile(filepath.Join(root, "scoped.json"), policy, 0600)
	source, err := bundle.Open(filepath.Join(root, "incident"))
	if err != nil {
		t.Fatal(err)
	}
	checks := []byte(`{"schema":"readmit-assertion-set/v1","name":"ACK","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"s0001-e000001","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`)
	files := map[string][]byte{"checks.json": checks}
	ref := func(id, schema, name string) connectedtest.Reference {
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: name, SHA256: connectedtest.Digest(files[name])}
	}
	d := connectedtest.Test{Schema: connectedtest.TestSchema, Project: "lab", ID: "connected", Revision: "1", Environment: connectedtest.Environment{Project: "lab", ID: "test", Revision: "1", Name: target.Name, Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: connectedtest.Digest(policy), TLS: connectedtest.TLS{Mode: target.Transport, ServerName: target.ServerName}, TargetRevision: connectedtest.TargetRevision{Provenance: "unknown"}}, Setup: connectedtest.Setup{Kind: "operator-declared", Isolation: "dedicated", Instructions: "Independent synthetic fixture"}, Checks: ref("checks", "readmit-assertion-set/v1", "checks.json"), Datasets: []connectedtest.Dataset{{ID: "acks", Kind: "v2-messages", Phase: "after", Source: "legacy-ack", Completion: connectedtest.Completion{Kind: "ack-responses", HorizonMS: 3000, MaxRecords: 10, MaxBytes: 65536}}}, Bindings: connectedtest.Bindings{Observed: "acks"}, OperatorVersion: connectedtest.OperatorVersion, Limits: connectedtest.Limits{MaxSteps: 10, MaxBytes: 1 << 20, DeadlineMS: 5000}}
	for i, e := range source.Events {
		raw, err := source.Raw(e.ID)
		if err != nil {
			t.Fatal(err)
		}
		name := "input" + strconv.Itoa(i) + ".hl7"
		files[name] = raw
		d.Steps = append(d.Steps, connectedtest.Step{ID: "step" + strconv.Itoa(i), Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: ref("input"+strconv.Itoa(i), "hl7", name), Occurrence: e.ID}})
	}
	if target.ClientCertificate != "" {
		d.Environment.TLS.Mode = "mtls"
	}
	raw, _ := json.Marshal(d)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Write(context.Background(), filepath.Join(root, planEntry)); err != nil {
		t.Fatal(err)
	}
	return desktop.PrepareActionRequest{Context: ctx, Action: desktop.ReplaySendAction, Items: []desktop.ItemRef{incident}, Destination: &lab, Replay: &desktop.ReplayActionOptions{Connected: &desktop.ConnectedActionOptions{Plan: planEntry, Policy: "scoped.json", Credential: func() string {
		if target.ClientCertificate != "" {
			return "credential-scope.json"
		}
		return ""
	}()}}}
}
func TestConnectedReviewUsesOneConsentAndReopensWithoutEffects(t *testing.T) {
	receiver := newReplayReceiver(t)
	app, ctx := namedProject(t)
	request := connectedReview(t, app, ctx, receiver.address, "compiled")
	review := prepared(t, app, request)
	if receiver.reached() != 0 {
		t.Fatal("review connected")
	}
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Token, IntentID: "connected-click"})
	if result.State != desktop.Completed || result.Replay == nil {
		t.Fatalf("%+v", result)
	}
	count := receiver.reached()
	if count != 1 || len(result.Replay.Messages) != 2 {
		t.Fatalf("sent %d", count)
	}
	if again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Token, IntentID: "connected-click"}); !again.Replayed || receiver.reached() != count {
		t.Fatal("click resent")
	}
	if again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Token, IntentID: "other-click"}); again.Outcome != desktop.ActionRefused {
		t.Fatal("review reused")
	}
	evidence := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: ctx.Project, Entry: result.Replay.Output})
	if evidence.State != desktop.Completed || evidence.Evidence.Status != "not-evaluated" || receiver.reached() != count {
		t.Fatalf("read: %+v", evidence)
	}
	if rows := listed(t, app, ctx.Project, desktop.RunItem); len(rows) == 0 {
		t.Fatal("connected receipt not cataloged")
	}
}
func TestConnectedCompilationNavigationAndReviewDoNotResolveNames(t *testing.T) {
	app, ctx := namedProject(t)
	var calls atomic.Int32
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) { calls.Add(1); return nil, context.Canceled }}
	t.Cleanup(func() { net.DefaultResolver = previous })
	request := connectedReview(t, app, ctx, "receiver.invalid:2575", "compiled")
	prepared(t, app, request)
	listed(t, app, ctx.Project, desktop.CaseItem)
	listed(t, app, ctx.Project, desktop.EnvironmentItem)
	if calls.Load() != 0 {
		t.Fatal("offline boundary performed DNS")
	}
	net.DefaultResolver.LookupHost(context.Background(), "receiver.invalid")
	if calls.Load() == 0 {
		t.Fatal("DNS instrumentation did not fire")
	}
}

func TestConnectedReviewNeverInvokesSelectedPrivateKeyProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX marker provider")
	}
	app, ctx := namedProject(t)
	marker := filepath.Join(ctx.Project, "provider-called")
	request := connectedReview(t, app, ctx, "receiver.invalid:2575", "compiled-mtls", func(root string, target *replay.Target) {
		target.Transport = "tls"
		target.ServerName = "receiver.invalid"
		target.ClientCertificate = filepath.Join(root, "public.pem")
		os.WriteFile(target.ClientCertificate, []byte("public certificate checked at explicit execution"), 0600)
		provider := filepath.Join(root, "provider")
		os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\nexit 1\n"), 0700)
		target.Credential = replay.Credential{SecretsFile: filepath.Join(root, "secrets.json"), Reference: "client"}
		if err := secret.WriteStore(target.Credential.SecretsFile, secret.Document{Schema: secret.Schema, References: []secret.Reference{{Name: "client", Store: secret.CustomerManaged, Purpose: secret.MLLPEndpoint, Address: target.Address, Command: provider, Arguments: []string{marker}, Generation: 1, RotatedAt: time.Now()}}}); err != nil {
			t.Fatal(err)
		}
		scope := map[string]any{"schema": "readmit-connected-credential/v1", "project": "lab", "environment": "test", "endpoint": "receiver", "operation": "v2-stimulus", "address": target.Address, "generation": 1, "reference": "client"}
		raw, _ := json.Marshal(scope)
		os.WriteFile(filepath.Join(root, "credential-scope.json"), raw, 0600)
	})
	prepared(t, app, request)
	listed(t, app, ctx.Project, desktop.EnvironmentItem)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("offline review invoked credential provider")
	}
	_, _ = (secret.Locator{Command: filepath.Join(ctx.Project, "provider"), Arguments: []string{marker}}).Read(context.Background())
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("provider instrumentation did not fire", err)
	}
}
