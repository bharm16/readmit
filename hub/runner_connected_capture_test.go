package hub_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testlicense"
)

const savedCaptureHorizonMS = 5000

type captureLabSession struct {
	Generation    string `json:"generation"`
	Mode          string `json:"mode"`
	Route         string `json:"route"`
	Status        string `json:"status"`
	ChannelSHA256 string `json:"channel_sha256"`
}
type captureLabConnection struct {
	MLLP struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		TLS  bool   `json:"tls"`
	} `json:"mllp"`
	Return struct {
		Host              string                    `json:"host"`
		Port              int                       `json:"port"`
		Transport         desktop.ListenerTransport `json:"transport"`
		ServerName        string                    `json:"server_name"`
		Authorities       string                    `json:"authorities"`
		Certificate       string                    `json:"certificate"`
		PrivateKey        string                    `json:"private_key"`
		ClientCertificate string                    `json:"client_certificate"`
		Route             string                    `json:"route"`
	} `json:"return_listener"`
}
type captureReturnReceipt struct {
	Schema            string `json:"schema"`
	Generation        string `json:"generation"`
	State             string `json:"state"`
	Transport         string `json:"transport"`
	Transmitted       string `json:"transmitted_base64"`
	ACK               string `json:"ack_base64"`
	PeerCertificate   string `json:"peer_certificate_sha256"`
	ClientCertificate string `json:"client_certificate_sha256"`
}
type savedCaptureRunner struct {
	app                 *desktop.App
	context             desktop.RequestContext
	test                desktop.ItemRef
	config              customerrunner.Config
	request             suite.ConnectedRequest
	authority, keyReads string
	checks              []byte
}

func readCaptureJSON(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
func captureSession(t *testing.T, state string) (captureLabSession, captureLabConnection) {
	t.Helper()
	var current captureLabSession
	var connection captureLabConnection
	readCaptureJSON(t, filepath.Join(state, "control", "session.json"), &current)
	readCaptureJSON(t, filepath.Join(state, "connection.json"), &connection)
	if current.Generation == "" || current.ChannelSHA256 == "" || current.Status != "ready" || current.Route != "v2v2" || connection.MLLP.TLS || connection.Return.Route != "owned-docker-exec-reverse" {
		t.Fatal("qualification needs a ready retained v2-to-v2 session and explicitly owned return path")
	}
	ip := net.ParseIP(connection.Return.Host)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || !ip.IsPrivate() || connection.Return.Transport != desktop.MutualTLSTransport || connection.Return.Port < 1 {
		t.Fatal("qualification requires the concrete approved private mutual-TLS return listener")
	}
	return current, connection
}

// The public session controller uses the same host flock. Keeping it in a
// dedicated Python process permits this opt-in test to compile on every OS.
func holdCaptureSession(t *testing.T, python, state string) func() {
	t.Helper()
	command := exec.Command(python, "-u", "-c", "import fcntl,sys; held=open(sys.argv[1],'a'); fcntl.flock(held,fcntl.LOCK_EX|fcntl.LOCK_NB); print('held',flush=True); sys.stdin.read()", filepath.Join(state, "control", "session.lock"))
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = input.Close()
		if err := command.Wait(); err != nil {
			t.Errorf("session lock process failed: %v %s", err, diagnostics.String())
		}
	}
	t.Cleanup(release)
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "held\n" {
		release()
		t.Fatal("persistent lab session is busy or cannot be fenced")
	}
	return release
}

func captureWitness(t *testing.T, python, state string, expected captureLabSession) {
	t.Helper()
	command := exec.CommandContext(t.Context(), python, "../tools/integration_lab.py", "session-witness", state, "--generation", expected.Generation)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("independent channel witness failed: %v\n%s", err, output)
	}
	current, _ := captureSession(t, state)
	if current != expected {
		t.Fatal("selected OIE channel changed during execution")
	}
}

func savedCaptureSuite(t *testing.T, hub *connectedHubFixture, state string, session captureLabSession, connection captureLabConnection) savedCaptureRunner {
	t.Helper()
	app := desktop.New(nil, desktop.ShellDocuments{Folder: t.TempDir()})
	if r := app.SelectOperationPolicy(testlicense.New(t)); r.State != desktop.Completed {
		t.Fatal(r)
	}
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "OIE saved receive " + session.Mode, Location: t.TempDir()})
	if created.State != desktop.Completed {
		t.Fatal(created)
	}
	ctx, root := created.Context, created.Context.Project
	save := func(kind desktop.ItemKind, intent string, draft desktop.ItemDraft) desktop.ItemRef {
		t.Helper()
		r := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: kind, IntentID: intent, Draft: draft})
		if r.Saved == nil {
			t.Fatalf("save %s: %+v", intent, r)
		}
		return *r.Saved
	}
	// Setup owns synthetic resources independently of the OIE mapping. This
	// fixture never supplies the product stimulus or its expected output.
	fixture := connectedlab.StartFixtureForProject(t, ctx.ProjectID, nil)
	fixtureProvider := filepath.Join(root, "fixture-provider")
	if err := os.WriteFile(fixtureProvider, []byte("#!/bin/sh\nprintf 'lab-%s' \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	role := func(name string, purpose sendpolicy.Operation) testisolation.Credential {
		return testisolation.Credential{Endpoint: "fixture-" + name, Reference: networkaction.Credential{Endpoint: fixture.Server().Listener.Addr().String(), Purpose: purpose, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: fixtureProvider, Arguments: []string{name}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "lab-fixture", Revision: "1", Project: ctx.ProjectID, Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "lab-tenant", Namespace: "lab-data", URL: fixture.Server().URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Server().Certificate().Raw}), Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	registryPath := filepath.Join(root, "fixture-registry.json")
	connectedlab.WriteJSON(t, registryPath, registry)
	isolation := &desktop.EnvironmentIsolation{Name: "Reserved synthetic session", RegistryFile: registryPath, Adapter: "lab-fixture", Mode: "isolated-tenant", Resources: []desktop.IsolationResource{{Name: "Patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "LAB"}}}}, Manual: []desktop.IsolationManual{}}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "OIE " + session.ChannelSHA256, Classification: replay.Nonproduction, TestEndpoint: true, Address: net.JoinHostPort(connection.MLLP.Host, strconv.Itoa(connection.MLLP.Port)), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "10s", MessageTimeout: "30s", MaxACKBytes: 65536}
	environment := save(desktop.EnvironmentItem, "oie-target", desktop.ItemDraft{Name: "Owned OIE mapping " + session.Mode, Environment: &target, SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{connection.MLLP.Host + "/32"}}, Isolation: isolation})
	provider, keyReads := filepath.Join(root, "return-key-provider"), filepath.Join(root, "return-key-reads")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\nexec /bin/cat \"$2\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	listener := desktop.ListenerSettings{BindAddress: connection.Return.Host, Port: connection.Return.Port, Transport: connection.Return.Transport, AllowRemote: true, TLSCertificate: "return-server.pem", TLSKeyReference: "return-key", ClientCA: "return-client-ca.pem", ConnectionLimit: 2, IdleTimeout: "1s", AckCode: "AA"}
	for name, original := range map[string]string{"return-server.pem": connection.Return.Certificate, "return-client-ca.pem": connection.Return.Authorities} {
		raw, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		runnerMTLSWrite(t, filepath.Join(root, name), raw)
	}
	credential := app.SaveCredential(desktop.CredentialSaveRequest{Context: ctx, Name: "return-key", Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged, Address: listener.Address(), Command: provider, Arguments: []string{keyReads, connection.Return.PrivateKey}})
	if credential.State != desktop.Completed {
		t.Fatal(credential)
	}
	source := save(desktop.SourceItem, "oie-return", desktop.ItemDraft{Name: "Approved private OIE return", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener}})
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "received-hl7", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "MSH-10", Key: true, Required: true}, {Name: "start", Type: "text", Selector: "SCH-11.4", Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 1 << 20, TimeoutMS: 5000}}
	observation := save(desktop.ObservationItem, "oie-output", desktop.ItemDraft{Name: "OIE mapped appointment", Observation: &desktop.ObservationDraft{Connected: &desktop.ConnectedObservation{Schema: desktop.ConnectedObservationSchema, Phase: "after", Namespace: "received", Baseline: "before-run", BusinessKeys: []desktop.BusinessKeyMapping{{Field: "key", Variable: "appointment-key"}}, Projection: &projection, Capture: &desktop.ConnectedCaptureObservation{Source: source, RunSelector: "MSH-4", InputKeySelector: "MSH-10", OutputKeySelector: "MSH-10", Include: []observeinterval.ScopeFilter{{Selector: "ZLG-1", Equals: session.Generation}}}, Completion: observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "stream", Freshness: "ingress", HorizonMS: savedCaptureHorizonMS, SampleMS: 100, MaxGapMS: 30000, MaxSamples: 400, MaxRecords: 10, MaxBytes: 1 << 20}}}})
	input, err := os.ReadFile(filepath.Join(state, "session-evidence", session.Generation, "stimuli", "siu-s13.hl7"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	messages, err := bundle.Write(filepath.Join(root, "messages"), []bundle.Input{{Path: "session-s13.mllp", Data: frameCapture(input)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.RegisterCase(root, "messages", operation.CaseRegistration{Title: "Session reschedule input"}); err != nil {
		t.Fatal(err)
	}
	cases := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem})
	if cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatal(cases)
	}
	one, expected := 1, dataset.Value{State: "present", Type: "text", Text: "20300102100000+0000"}
	checks := []desktop.ConnectedCheck{{Name: "One matching output", Check: assertion.DatasetAssertion{ID: "one-output", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "received", Where: []assertion.RowFilter{}}, Count: &one}}, {Name: "Mapped appointment time", Check: assertion.DatasetAssertion{ID: "mapped-start", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "received", Where: []assertion.RowFilter{}}, Column: "start", Expected: &expected}}}
	draft := desktop.ConnectedTestDraft{Schema: desktop.ConnectedTestSchema, Boundary: desktop.EngineOutputBoundary, Generation: connectedtest.Generation{Seed: 7, BaseTime: "2030-01-01T00:00:00Z"}, Variables: []desktop.ConnectedVariable{}, Steps: []desktop.ConnectedStep{{ID: "move", After: []string{}, Source: desktop.ConnectedSource{Case: cases.Page.Items[0].Ref, Identity: messages.Identity, Occurrence: messages.Events[0].ID}, V2: &desktop.ConnectedV2{RuntimeMarkerSelector: "MSH-4"}}}, Phases: []desktop.ConnectedPhase{{ID: "move", Name: "Observe OIE mapped output", Steps: []string{"move"}, After: []connectedtest.PhaseDependency{}, Observations: []desktop.ConnectedPhaseObservation{{Dataset: "received", Observation: observation, When: "after"}}, Checks: checks, Responses: []desktop.ConnectedResponseCheck{}, Validations: []desktop.ConnectedValidationCheck{}, Acks: []desktop.ConnectedAckCheck{{ID: "accepted", Name: "OIE accepted input", Step: "move", Code: "AA"}}}}}
	test := save(desktop.TestItem, "oie-saved-test", desktop.ItemDraft{Name: "Saved OIE mapped appointment", ConnectedTest: &draft, TestLinks: &desktop.TestLinks{Environment: environment.ID, Reset: desktop.ResetFromEnvironment}})
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: test})
	if reopened.Draft == nil || reopened.Draft.ConnectedTest == nil {
		t.Fatal(reopened)
	}
	actualChecks, err := json.Marshal(reopened.Draft.ConnectedTest.Phases[0].Checks, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	saved := save(desktop.SuiteItem, "oie-saved-suite", desktop.ItemDraft{Name: "Saved OIE receive suite", Suite: &desktop.SuiteDraft{Tags: []string{}, Concurrency: 1, Tests: []desktop.SuiteTestDraft{{ID: "move", Test: test, Parameter: "receiver", After: []string{}, Sequence: []string{}, Tags: []string{}}}, Datasets: []desktop.SuiteDataset{}, Environments: []desktop.SuiteEnvironment{{ID: "qa", Name: "QA", Bindings: []desktop.SuiteBinding{{Parameter: "receiver", Target: environment}}}}, Requirements: []desktop.SuiteRequirement{}, Exclusions: []desktop.SuiteExclusion{}}})
	baseline := app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{saved}})
	if baseline.Review == nil || !baseline.Review.Ready {
		t.Fatalf("baseline: %+v", baseline)
	}
	if r := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: baseline.Review.Token, IntentID: "baseline", Decisions: desktop.ReviewDecisions{Rationale: "One output with independently specified appointment time"}}); r.State != desktop.Completed {
		t.Fatal(r)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: saved})
	if opened.Suite == nil || !opened.Suite.Runnable {
		t.Fatal(opened)
	}
	path := filepath.Join(root, "runner-suite.json")
	runnerMTLSWrite(t, path, []byte(opened.Suite.Document))
	revision := session.Generation + ":" + session.ChannelSHA256
	review, err := suite.ReviewConnectedPromotion(path, "qa", revision)
	if err != nil {
		t.Fatal(err)
	}
	promotionPath := filepath.Join(root, "promotion.json")
	promotion, err := suite.ApproveConnectedPromotion(path, "qa", revision, review.Identity(), "Operator", "Exact independently deployed OIE channel and saved capture", promotionPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(root, "prepared")})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := prepared.Capabilities.Identity()
	if err != nil {
		t.Fatal(err)
	}
	authority := filepath.Join(root, "runner-authority.json")
	connectedlab.WriteJSON(t, authority, customerrunner.ConnectedAuthority{Schema: customerrunner.ConnectedAuthoritySchema, Actor: "runner", Generation: "1", Input: prepared.Identity, Promotion: promotion.Identity(), Capabilities: capability, Operations: prepared.Operations(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour), MaxSeconds: 120, MaxOccurrences: 5})
	for i := range hub.policy.Grants {
		hub.policy.Grants[i].Project = ctx.ProjectID
	}
	for i := range hub.policy.Tokens {
		hub.policy.Tokens[i].Project = ctx.ProjectID
	}
	writePolicy(t, hub.accessPath, hub.policy)
	hub.grants.Runners[0].Project, hub.grants.Runners[0].Environment = ctx.ProjectID, "test"
	hub.grants.Runners[0].Capability, hub.grants.Runners[0].MaxSeconds, hub.grants.Runners[0].MaxJobs = capability, 120, 10
	hub.writeGrants()
	config := hub.runnerConfig()
	config.Project, config.Environment = ctx.ProjectID, "test"
	request := suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(root, "execution"), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: revision, Instance: "capture-" + session.Mode}
	if _, err := os.Stat(keyReads); !os.IsNotExist(err) {
		t.Fatal("save or preparation resolved the capture private key")
	}
	return savedCaptureRunner{app: app, context: ctx, test: test, config: config, request: request, authority: authority, keyReads: keyReads, checks: actualChecks}
}
func frameCapture(payload []byte) []byte { return append(append([]byte{11}, payload...), 28, 13) }

// The native application executes the exact saved revision first. A stop file
// releases this fixture only to verify real evidence and dispatch the runner.
func handoffCaptureNative(t *testing.T, saved savedCaptureRunner, session captureLabSession, connection captureLabConnection) string {
	t.Helper()
	base := os.Getenv("READMIT_CAPTURE_RUNNER_NATIVE_DIR")
	if base == "" {
		return ""
	}
	if !filepath.IsAbs(base) {
		t.Fatal("native handoff path must be absolute")
	}
	root := filepath.Join(base, session.Mode)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	stop, home := filepath.Join(root, "stop"), filepath.Join(root, "home")
	state := filepath.Join(home, "Library", "Application Support", "readmit")
	received := filepath.Join(root, "vendor-license")
	if err := os.Mkdir(received, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := testlicense.CreateUnactivated(received); err != nil {
		t.Fatal(err)
	}
	license, trust := filepath.Join(received, "entitlement.json"), filepath.Join(received, "trust.json")
	raw, err := os.ReadFile(license)
	if err != nil {
		t.Fatal(err)
	}
	native := desktop.NewWithInstalledLicense(nil, desktop.ShellDocuments{Folder: state}, filepath.Join(state, "license"))
	if r := native.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: license, Trust: trust, Digest: connectedtest.Digest(raw), Author: "test-author", Device: "test-device", Authority: "test-runner"}); r.State != desktop.Completed {
		t.Fatal(r)
	}
	if r := native.OpenNamedProject(saved.context.Project); r.State != desktop.Completed {
		t.Fatal(r)
	}
	info := map[string]string{"home": home, "state": state, "project": saved.context.Project, "test_id": saved.test.ID, "test_revision": saved.test.Revision, "test_name": "Saved OIE mapped appointment", "mode": session.Mode, "generation": session.Generation, "channel_sha256": session.ChannelSHA256, "return_address": net.JoinHostPort(connection.Return.Host, strconv.Itoa(connection.Return.Port)), "stop_file": stop}
	connectedlab.WriteJSON(t, filepath.Join(root, "fixture-info.json"), info)
	t.Logf("OIE native handoff ready: %s", filepath.Join(root, "fixture-info.json"))
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("OIE native handoff timed out")
		case <-ticker.C:
			if _, err := os.Stat(stop); err != nil {
				continue
			}
			runs := saved.app.ListCatalog(desktop.CatalogQuery{Context: saved.context, Kind: desktop.RunItem})
			if runs.Page == nil {
				t.Fatal(runs)
			}
			for _, item := range runs.Page.Items {
				if item.Summary.Run == nil || item.Summary.Run.Test == nil || *item.Summary.Run.Test != saved.test {
					continue
				}
				read := saved.app.OpenRun(desktop.RunRequest{Context: saved.context, Run: item.Ref})
				if read.State != desktop.Completed || read.Run == nil || read.Run.Lifecycle == nil || read.Run.Report == nil {
					t.Fatal(read)
				}
				expected := desktop.RunPassed
				if session.Mode == "defective" {
					expected = desktop.RunFailed
				}
				if read.Run.Result != expected {
					t.Fatalf("native result %s expected %s", read.Run.Result, expected)
				}
				connectedlab.WriteJSON(t, filepath.Join(root, "native-result.json"), read)
				return filepath.Join(saved.context.Project, read.Run.Report.Run)
			}
			t.Fatal("native run did not retain the exact selected saved test revision")
		}
	}
}
func verifyCaptureFlow(t *testing.T, state, output string, session captureLabSession, connection captureLabConnection) connectedrun.FlowResult {
	t.Helper()
	flow, err := connectedrun.OpenFlow(t.Context(), output)
	if err != nil {
		t.Fatal("retained captured flow", err)
	}
	verdict, mapped := assertion.VerdictPass, assertion.OutcomePassed
	if session.Mode == "defective" {
		verdict, mapped = assertion.VerdictFail, assertion.OutcomeFailed
	}
	if flow.State != "complete" || flow.Verdict != verdict || flow.Boundary != "engine-output" || len(flow.Phases) != 1 || len(flow.Phases[0].Checks) != 3 || flow.Phases[0].Checks[0].ID != "typed:one-output" || flow.Phases[0].Checks[0].Outcome != assertion.OutcomePassed || flow.Phases[0].Checks[1].ID != "typed:mapped-start" || flow.Phases[0].Checks[1].Outcome != mapped || flow.Phases[0].Checks[2].ID != "wire:accepted" || flow.Phases[0].Checks[2].Outcome != assertion.OutcomePassed {
		t.Fatalf("OIE %s did not decide intended unchanged checks: %+v", session.Mode, flow)
	}
	execution, err := connectedrun.ExecutionRoot(output)
	if err != nil {
		t.Fatal("retained execution root", err)
	}
	intervalPath := filepath.Join(execution, "phases", "move", "intervals", "received")
	interval, err := observeinterval.Open(t.Context(), intervalPath)
	if err != nil {
		t.Fatal("retained full interval", err)
	}
	var ready, started, completed, final observeinterval.Record
	for _, record := range interval.Records {
		switch record.Kind {
		case "baseline":
			ready = record
		case "stimulus-started":
			started = record
		case "stimulus-finished":
			completed = record
		case "sample":
			final = record
		}
	}
	if !interval.Sufficient() || interval.Definition.HorizonMS != savedCaptureHorizonMS || ready.RecordedAt.IsZero() || started.RecordedAt.Before(ready.RecordedAt) || completed.RecordedAt.IsZero() || final.Stamp.MonotonicMS-completed.Stamp.MonotonicMS < savedCaptureHorizonMS {
		t.Fatalf("capture did not prove readiness before stimulus and full interval: %+v", interval)
	}
	_, captured, err := networkaction.OpenCapture(filepath.Join(intervalPath, "capture"))
	if err != nil {
		t.Fatal("retained capture", err)
	}
	var inbound, ack []byte
	for _, event := range captured.Events {
		raw, err := captured.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		if event.Direction == bundle.Inbound {
			if inbound != nil {
				t.Fatal("unexpected extra received output")
			}
			inbound = raw
		}
		if event.Direction == bundle.Outbound {
			if ack != nil {
				t.Fatal("unexpected extra returned ACK")
			}
			ack = raw
		}
	}
	if inbound == nil || ack == nil {
		t.Fatal("capture lost received payload or full ACK")
	}
	receipts, err := filepath.Glob(filepath.Join(state, "session-evidence", session.Generation, "return-receipts", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	matched := 0
	serverDigest, clientDigest := captureCertificateDigest(t, connection.Return.Certificate), captureCertificateDigest(t, connection.Return.ClientCertificate)
	for _, path := range receipts {
		var receipt captureReturnReceipt
		readCaptureJSON(t, path, &receipt)
		transmitted, err := base64.StdEncoding.DecodeString(receipt.Transmitted)
		if err != nil || !bytes.Equal(transmitted, inbound) {
			continue
		}
		receivedACK, err := base64.StdEncoding.DecodeString(receipt.ACK)
		if err != nil || !bytes.Equal(receivedACK, ack) || receipt.Schema != "readmit-lab-return-receipt/v1" || receipt.Generation != session.Generation || receipt.State != "acknowledged" || receipt.Transport != "mutual-tls" || receipt.PeerCertificate != serverDigest || receipt.ClientCertificate != clientDigest {
			t.Fatal("independent OIE sender did not witness exact trusted payload and full ACK", path)
		}
		matched++
	}
	if matched != 1 {
		t.Fatalf("expected one OIE sender receipt matching capture; found %d", matched)
	}
	return flow
}
func verifyCaptureReport(t *testing.T, output, packetPath string, expected assertion.Verdict) {
	t.Helper()
	packet, err := report.AssembleConnected(t.Context(), report.ConnectedInput{Current: output}, packetPath)
	if err != nil {
		t.Fatal("assemble retained capture report", err)
	}
	doc, err := report.BuildConnectedReport(packet)
	if err != nil || len(doc.Runs) != 1 || doc.Runs[0].Run.Verdict != string(expected) {
		t.Fatal("build captured report", err)
	}
	for _, run := range doc.Runs {
		for _, phase := range run.Phases {
			references := []string{}
			for _, check := range phase.Checks {
				references = append(references, check.Evidence)
			}
			for _, observation := range phase.Observations {
				references = append(references, observation.Evidence)
			}
			for _, step := range phase.Steps {
				references = append(references, step.Evidence)
			}
			for _, reference := range references {
				member, _, _ := strings.Cut(reference, "#")
				if !filepath.IsLocal(filepath.FromSlash(member)) {
					t.Fatal("report evidence reference is not a local packet member", reference)
				}
				if _, err := os.Stat(filepath.Join(packetPath, filepath.FromSlash(member))); err != nil {
					t.Fatal("report links to missing retained evidence", reference, err)
				}
			}
		}
	}
	raw, err := json.Marshal(doc, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PRIVATE KEY")) || bytes.Contains(raw, []byte("rh_")) {
		t.Fatal("capture report contains private key or runner bearer authority")
	}
	if err := os.WriteFile(packetPath+"-report.json", raw, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := report.OpenConnected(t.Context(), packetPath)
	if err != nil || reopened.Identity != packet.Identity {
		t.Fatal("offline capture report did not reopen", err)
	}
}

// Fresh-process verification also covers candidate report fixes after an
// already-running native handoff, without repeating any external stimulus.
func TestSavedConnectedCaptureRetainedReports(t *testing.T) {
	var assertionBytes []byte
	verified := []map[string]any{}
	root := os.Getenv("READMIT_CAPTURE_EVIDENCE_DIR")
	if root == "" {
		t.Skip("set READMIT_CAPTURE_EVIDENCE_DIR to verify actual retained OIE capture reports")
	}
	modes := []string{"positive", "defective", "corrected"}
	if mode := os.Getenv("READMIT_CAPTURE_REPORT_MODE"); mode != "" {
		modes = []string{mode}
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			expected := assertion.VerdictPass
			if mode == "defective" {
				expected = assertion.VerdictFail
			}
			for _, kind := range []string{"native", "runner"} {
				t.Run(kind, func(t *testing.T) {
					source := filepath.Join(root, mode, kind)
					if kind == "runner" {
						execution, err := suite.OpenConnectedExecution(t.Context(), source)
						if err != nil || len(execution.Report.Jobs) != 1 || execution.Report.Jobs[0].Flow == nil {
							t.Fatal("retained runner job identity", err)
						}
						source = filepath.Join(source, "runs", execution.Report.Jobs[0].ID)
					}
					destination, err := os.MkdirTemp(filepath.Join(root, mode), "report-"+kind+"-")
					if err != nil {
						t.Fatal(err)
					}
					state := os.Getenv("READMIT_CAPTURE_LAB_SESSION")
					if state != "" {
						var proof struct {
							Generation    string `json:"generation"`
							ChannelSHA256 string `json:"channel_sha256"`
						}
						readCaptureJSON(t, filepath.Join(root, mode, "qualification.json"), &proof)
						var connection captureLabConnection
						readCaptureJSON(t, filepath.Join(state, "connection.json"), &connection)
						verifyCaptureFlow(t, state, source, captureLabSession{Generation: proof.Generation, Mode: mode, ChannelSHA256: proof.ChannelSHA256}, connection)
					}
					verifyCaptureReport(t, source, filepath.Join(destination, "packet"), expected)
					evidence, err := connectedrun.OpenFlowEvidence(t.Context(), source)
					if err != nil {
						t.Fatal(err)
					}
					phase := evidence.Plan.Document().Test.Phases[0]
					var typed assertion.DatasetSetDocument
					if err := json.Unmarshal(evidence.Plan.Phase(phase.ID).Files()["dependencies/"+phase.Checks.SHA256], &typed); err != nil {
						t.Fatal(err)
					}
					var wire assertion.Set
					if phase.Wire == nil {
						t.Fatal("retained capture is missing authored ACK assertion")
					}
					if err := json.Unmarshal(evidence.Plan.Dependency(phase.Wire.Set), &wire); err != nil {
						t.Fatal(err)
					}
					actual, err := json.Marshal(struct {
						Typed []assertion.DatasetAssertion
						Wire  []assertion.Assertion
					}{typed.Assertions, wire.Assertions}, json.Deterministic(true))
					if err != nil {
						t.Fatal(err)
					}
					if assertionBytes == nil {
						assertionBytes = bytes.Clone(actual)
					} else if !bytes.Equal(assertionBytes, actual) {
						t.Fatal("actual retained native/runner assertion bytes changed across the defect cycle")
					}
					var declaration struct {
						Test       desktop.ItemRef `json:"test"`
						Generation string          `json:"generation"`
						Channel    string          `json:"channel_sha256"`
					}
					readCaptureJSON(t, filepath.Join(root, mode, "qualification.json"), &declaration)
					executionRoot, err := connectedrun.ExecutionRoot(source)
					if err != nil {
						t.Fatal(err)
					}
					interval, err := observeinterval.Open(t.Context(), filepath.Join(executionRoot, "phases", "move", "intervals", "received"))
					if err != nil {
						t.Fatal(err)
					}
					var stimulusFinished, observed int64
					for _, record := range interval.Records {
						if record.Kind == "stimulus-finished" {
							stimulusFinished = record.Stamp.MonotonicMS
						}
						if record.Kind == "sample" {
							observed = record.Stamp.MonotonicMS
						}
					}
					_, capture, err := networkaction.OpenCapture(filepath.Join(executionRoot, "phases", "move", "intervals", "received", "capture"))
					if err != nil {
						t.Fatal(err)
					}
					bytesProof := map[string]any{}
					for _, event := range capture.Events {
						raw, err := capture.Raw(event.ID)
						if err != nil {
							t.Fatal(err)
						}
						if event.Direction == bundle.Inbound || event.Direction == bundle.Outbound {
							bytesProof[string(event.Direction)] = map[string]any{"bytes": len(raw), "sha256": fmt.Sprintf("%x", sha256.Sum256(raw))}
						}
					}
					verified = append(verified, map[string]any{"mode": mode, "path": kind, "saved_test": declaration.Test, "generation": declaration.Generation, "channel_sha256": declaration.Channel, "instance": evidence.Result.Instance, "identity": evidence.Identity, "verdict": evidence.Result.Verdict, "state": evidence.Result.State, "horizon_ms": interval.Definition.HorizonMS, "observed_post_stimulus_ms": observed - stimulusFinished, "independent_sender_verified": state != "", "capture_bytes": bytesProof})

				})
			}
		})
	}
	if !t.Failed() {
		suffix := "all"
		if mode := os.Getenv("READMIT_CAPTURE_REPORT_MODE"); mode != "" {
			suffix = mode
		}
		receipt := map[string]any{"schema": "readmit-capture-retained-verification/v1", "verified": true, "assertions_sha256": fmt.Sprintf("%x", sha256.Sum256(assertionBytes)), "results": verified}
		raw, err := json.Marshal(receipt, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		destination, err := os.MkdirTemp(root, "verified-"+suffix+"-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, "receipt.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, "assertions.json"), assertionBytes, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("independent retained qualification receipt: %s", filepath.Join(destination, "receipt.json"))
	}
}

func captureCertificateDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("invalid qualification certificate")
	}
	return fmt.Sprintf("%x", sha256.Sum256(block.Bytes))
}
func captureTestProducer(t *testing.T) map[string]any {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Error(err)
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		t.Error(err)
		return nil
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		t.Error(err)
		return nil
	}
	build := "unavailable"
	if info, ok := debug.ReadBuildInfo(); ok {
		build = info.String()
	}
	return map[string]any{"producer": "production customer runner API in Go test binary", "binary_sha256": fmt.Sprintf("%x", digest.Sum(nil)), "binary_bytes": size, "go_build_info": build}
}

func preserveCaptureQualification(t *testing.T, state string, saved savedCaptureRunner, session captureLabSession, native *string) {
	t.Helper()
	t.Cleanup(func() {
		base := os.Getenv("READMIT_CAPTURE_EVIDENCE_DIR")
		if base == "" && !t.Failed() {
			return
		}
		if base == "" {
			var err error
			base, err = os.MkdirTemp("", "readmit-699-capture-failure-")
			if err != nil {
				t.Log(err)
				return
			}
		}
		destination := filepath.Join(base, session.Mode)
		if err := os.MkdirAll(destination, 0700); err != nil {
			t.Log(err)
			return
		}
		copies := map[string]string{"runner": saved.request.Output, "return-receipts": filepath.Join(state, "session-evidence", session.Generation, "return-receipts")}
		if *native != "" {
			copies["native"] = *native
		}
		for name, source := range copies {
			if _, err := os.Stat(source); err != nil {
				continue
			}
			if err := os.CopyFS(filepath.Join(destination, name), os.DirFS(source)); err != nil {
				t.Logf("preserve %s: %v", name, err)
			}
		}
		for _, name := range []string{"channel.xml", "readiness.json", "runtime.json", "connection.json", "source-provenance.json", "return-path.json"} {
			if raw, err := os.ReadFile(filepath.Join(state, "session-evidence", session.Generation, name)); err == nil {
				if err = os.WriteFile(filepath.Join(destination, name), raw, 0600); err != nil {
					t.Log(err)
				}
			}
		}
		producer := captureTestProducer(t)
		proof := map[string]any{"schema": "readmit-capture-qualification/v1", "test": saved.test, "generation": session.Generation, "channel_sha256": session.ChannelSHA256, "mode": session.Mode, "native_executed": *native != "", "checks_sha256": fmt.Sprintf("%x", sha256.Sum256(saved.checks)), "test_failed": t.Failed(), "runner_producer": producer}
		raw, _ := json.Marshal(proof, json.Deterministic(true))
		if err := os.WriteFile(filepath.Join(destination, "qualification.json"), raw, 0600); err != nil {
			t.Log(err)
		}
		t.Logf("retained synthetic capture qualification: %s", destination)
	})
}
func TestSavedConnectedCaptureActualOIERunnerAndNativeDefectCycle(t *testing.T) {
	state := os.Getenv("READMIT_CAPTURE_LAB_SESSION")
	if state == "" {
		t.Skip("set READMIT_CAPTURE_LAB_SESSION to opt in to persistent actual OIE capture")
	}
	if !filepath.IsAbs(state) {
		t.Fatal("READMIT_CAPTURE_LAB_SESSION must be absolute")
	}
	for _, name := range []string{"READMIT_HUB_TEST_SOCKET", "READMIT_HUB_TEST_PORT", "READMIT_HUB_TEST_USER"} {
		if os.Getenv(name) == "" {
			t.Fatalf("opted-in OIE runner qualification requires %s for an isolated PostgreSQL cluster", name)
		}
	}
	python := os.Getenv("READMIT_CAPTURE_LAB_PYTHON")
	if !filepath.IsAbs(python) {
		t.Fatal("READMIT_CAPTURE_LAB_PYTHON must name an absolute executable")
	}
	if native := os.Getenv("READMIT_CAPTURE_RUNNER_NATIVE_DIR"); native != "" {
		if err := os.MkdirAll(native, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var originalChecks []byte
	for _, mode := range []string{"positive", "defective", "corrected"} {
		if !t.Run(mode, func(t *testing.T) {
			current, connection := captureSession(t, state)
			if current.Mode != mode {
				command := exec.CommandContext(t.Context(), python, "../tools/integration_lab.py", "session-revision", state, "--generation", current.Generation, "--mode", mode)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("OIE revision failed: %v\n%s", err, output)
				}
				current, connection = captureSession(t, state)
			}
			captureWitness(t, python, state, current)
			release := holdCaptureSession(t, python, state)
			hub := newConnectedHubFixture(t)
			saved := savedCaptureSuite(t, hub, state, current, connection)
			if originalChecks == nil {
				originalChecks = bytes.Clone(saved.checks)
			} else if !bytes.Equal(originalChecks, saved.checks) {
				t.Fatal("defect cycle changed saved assertions")
			}
			native := ""
			preserveCaptureQualification(t, state, saved, current, &native)
			native = handoffCaptureNative(t, saved, current, connection)
			if actual, _ := captureSession(t, state); actual != current {
				t.Fatal("session changed while the native test held its fence")
			}
			if native != "" {
				verifyCaptureFlow(t, state, native, current, connection)
			}
			before := hub.requests.Load()
			keyBefore, _ := os.ReadFile(saved.keyReads)
			report, err := executeCustomerConnectedSuite(t, saved.config, saved.request, saved.authority)
			exit := 0
			if mode == "defective" {
				exit = 1
			}
			if err != nil || report.ExitCode() != exit || report.Executed != 1 || hub.requests.Load() <= before {
				t.Fatalf("production runner %s: %+v %v", mode, report, err)
			}
			if keyAfter, err := os.ReadFile(saved.keyReads); err != nil || len(keyAfter) <= len(keyBefore) {
				t.Fatal("runner did not resolve its own capture key", err)
			}
			if actual, _ := captureSession(t, state); actual != current {
				t.Fatal("session changed while the runner held its fence")
			}
			flow := verifyCaptureFlow(t, state, filepath.Join(saved.request.Output, "runs", report.Jobs[0].ID), current, connection)
			if native != "" {
				nativeFlow, err := connectedrun.OpenFlow(t.Context(), native)
				if err != nil || nativeFlow.Instance == flow.Instance {
					t.Fatal("native and runner borrowed runtime marker", err)
				}
			}
			retry := saved.request
			retry.Output = filepath.Join(saved.context.Project, "retry")
			if _, err := executeCustomerConnectedSuite(t, saved.config, retry, saved.authority); err == nil {
				t.Fatal("consumed runner occurrence was reused")
			}
			release()
			captureWitness(t, python, state, current)
			hub.server.Close()
			if err := os.Remove(saved.config.Key.Arguments[0]); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(saved.context.Project, "return-key-provider")); err != nil {
				t.Fatal(err)
			}
			calls := hub.requests.Load()
			keyReads, _ := os.ReadFile(saved.keyReads)
			retained, err := suite.OpenConnectedExecution(t.Context(), saved.request.Output)
			if err != nil || retained.Report.ExitCode() != exit {
				t.Fatal("offline captured runner evidence did not reopen", err)
			}
			afterReads, _ := os.ReadFile(saved.keyReads)
			if hub.requests.Load() != calls || !bytes.Equal(keyReads, afterReads) {
				t.Fatal("offline read reacquired authority or key")
			}
			t.Logf("qualified mode=%s saved_test=%s revision=%s channel=%s runtime=%s evidence=%s", mode, saved.test.ID, saved.test.Revision, current.ChannelSHA256, flow.Instance, retained.Identity)
		}) {
			break
		}
	}
}
