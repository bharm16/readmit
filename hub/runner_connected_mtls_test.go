package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testlicense"
)

const runnerMTLSMessage = "MSH|^~\\&|SCHED|LAB|ENGINE|LAB|20260301090000||SIU^S12|BOOK-1|P|2.5.1\rSCH|APT-1||||||||||2026-03-02T09:00:00Z\r"

type runnerMTLSPeer struct {
	root, address, certificate, key, clientDigest string
	command                                       *exec.Cmd
}

func startRunnerMTLSPeer(t *testing.T) *runnerMTLSPeer {
	t.Helper()
	python := os.Getenv("READMIT_MTLS_PYTHON")
	if python == "" {
		t.Skip("set READMIT_MTLS_PYTHON to opt in to independent Python SSL qualification")
	}
	if !filepath.IsAbs(python) {
		t.Fatal("READMIT_MTLS_PYTHON must name an absolute executable")
	}
	var host hub.Config
	client := certificates(t, &host)
	p := &runnerMTLSPeer{root: filepath.Dir(host.Certificate), certificate: filepath.Join(filepath.Dir(host.Certificate), "client.pem"), key: filepath.Join(filepath.Dir(host.Certificate), "client-key.pem"), clientDigest: fmt.Sprintf("%x", sha256.Sum256(client.Certificate[0]))}
	private, err := x509.MarshalPKCS8PrivateKey(client.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	runnerMTLSWrite(t, p.certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: client.Certificate[0]}))
	runnerMTLSWrite(t, p.key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
	logPath := filepath.Join(p.root, "peer-process.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	p.command = exec.Command(python, "../internal/desktop/testdata/mtls_peer.py", p.root)
	p.command.Stdout, p.command.Stderr = log, log
	if err = p.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if p.command.Process != nil {
			_ = p.command.Process.Kill()
			_ = p.command.Wait()
		}
		_ = log.Close()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(p.root, "ready.json"))
		if err == nil {
			var ready struct {
				Address string `json:"address"`
			}
			if json.Unmarshal(raw, &ready) == nil && ready.Address != "" {
				p.address = ready.Address
				t.Logf("independent peer runtime: %s", raw)
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("independent receiver did not start: %s", raw)
	return nil
}
func runnerMTLSWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func (p *runnerMTLSPeer) accepted(t *testing.T) []map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.root, "peer.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]string
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var event map[string]string
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "accepted" {
			events = append(events, event)
		}
	}
	return events
}

// savedRunnerMTLSSuite uses the same saved environment, saved test, named suite
// and baseline approval that the native authoring facade publishes. Only the
// administrator's finite runner installation is supplied below that facade.
func savedRunnerMTLSSuite(t *testing.T, f *connectedHubFixture, p *runnerMTLSPeer) (customerrunner.Config, suite.ConnectedRequest, string, string, *desktop.App, desktop.RequestContext, desktop.ItemRef) {
	t.Helper()
	app := desktop.New(nil, desktop.ShellDocuments{Folder: t.TempDir()})
	if r := app.SelectOperationPolicy(testlicense.New(t)); r.State != desktop.Completed {
		t.Fatal(r.Reason)
	}
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Saved mTLS qualification", Location: t.TempDir()})
	if created.State != desktop.Completed {
		t.Fatal(created.Reason)
	}
	ctx, root := created.Context, created.Context.Project
	save := func(kind desktop.ItemKind, id string, d desktop.ItemDraft) desktop.ItemRef {
		t.Helper()
		r := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: kind, IntentID: id, Draft: d})
		if r.Saved == nil {
			t.Fatalf("save %s: %+v", id, r)
		}
		return *r.Saved
	}
	lab := connectedlab.StartFHIRLab(t)
	fixture := connectedlab.StartFixtureForProject(t, ctx.ProjectID, nil)
	provider := filepath.Join(t.TempDir(), "provider")
	marker := provider + "-used"
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\nexec /bin/cat \"$2\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fixtureProvider := filepath.Join(t.TempDir(), "fixture-provider")
	if err := os.WriteFile(fixtureProvider, []byte("#!/bin/sh\nprintf 'lab-%s' \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	role := func(name string, op sendpolicy.Operation) testisolation.Credential {
		return testisolation.Credential{Endpoint: "fixture-" + name, Reference: networkaction.Credential{Endpoint: fixture.Server().Listener.Addr().String(), Purpose: op, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: fixtureProvider, Arguments: []string{name}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "lab-fixture", Revision: "1", Project: ctx.ProjectID, Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "lab-tenant", Namespace: "lab-data", URL: fixture.Server().URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Server().Certificate().Raw}), Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	registryPath := filepath.Join(root, "fixture-registry.json")
	connectedlab.WriteJSON(t, registryPath, registry)
	isolation := func() *desktop.EnvironmentIsolation {
		return &desktop.EnvironmentIsolation{Name: "Synthetic reserved lab", RegistryFile: registryPath, Adapter: "lab-fixture", Mode: "isolated-tenant", Resources: []desktop.IsolationResource{{Name: "Patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "LAB"}}}}, Manual: []desktop.IsolationManual{}}
	}
	approved := &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}
	credential := app.SaveCredential(desktop.CredentialSaveRequest{Context: ctx, Name: "runner-mllp-client", Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged, Address: p.address, Command: provider, Arguments: []string{marker, p.key}})
	if credential.State != desktop.Completed {
		t.Fatal(credential)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "Independent mutual TLS peer", Classification: replay.Nonproduction, TestEndpoint: true, Address: p.address, Transport: "tls", ApprovedTransport: true, ServerName: "127.0.0.1", CAFile: filepath.Join(p.root, "ca.pem"), ClientCertificate: p.certificate, Credential: replay.Credential{Reference: "runner-mllp-client"}, ConnectTimeout: "10s", MessageTimeout: "10s", MaxACKBytes: 4096}
	environment := save(desktop.EnvironmentItem, "mtls-environment", desktop.ItemDraft{Name: "Independent mutual TLS peer", Environment: &target, SendPolicy: approved, Isolation: isolation()})
	caPath := filepath.Join(root, "observation-ca.pem")
	runnerMTLSWrite(t, caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: lab.Server().Certificate().Raw}))
	fhir := save(desktop.EnvironmentItem, "observation-environment", desktop.ItemDraft{Name: "Separate empty observation store", SendPolicy: approved, Isolation: isolation(), FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: lab.Base(), ServerName: "example.com", CAFile: caPath, Classification: replay.Nonproduction, Authentication: "none"}})
	capReview := app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.CheckFHIRCapabilitiesAction, Items: []desktop.ItemRef{fhir}})
	if capReview.Review == nil {
		t.Fatal(capReview)
	}
	if r := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: capReview.Review.Token, IntentID: "observation-capabilities"}); r.State != desktop.Completed {
		t.Fatal(r)
	}
	observation := save(desktop.ObservationItem, "empty-observation", desktop.ItemDraft{Name: "No downstream workflow claimed", Observation: &desktop.ObservationDraft{Connected: &desktop.ConnectedObservation{Environment: fhir.ID, Namespace: "appointments", Phase: "both", Baseline: "before-run", BusinessKeys: []desktop.BusinessKeyMapping{{Field: "key", Variable: "appointment-key"}}, Completion: observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 3000, SampleMS: 100, MaxGapMS: 30000, MaxSamples: 40, MaxRecords: 50, MaxBytes: 512 << 10}, FHIR: &desktop.FHIRSearchDraft{Resource: "Appointment", Boundary: "reference-fhir-store", Criteria: []desktop.FHIRCriterion{{Parameter: "identifier", Type: "token", System: connectedlab.AppointmentSystem, Value: "APT-1"}}, Fields: []desktop.FHIRFieldProjection{{Name: "key", Field: "identifier[0].value", Key: true, Required: true}}, Budget: fhirrest.Budget{Pages: 4, Rows: 50, Bytes: 256 << 10, TimeoutMS: 30000}}}}})
	now := time.Now()
	source, err := bundle.Write(filepath.Join(root, "messages"), []bundle.Input{{Path: "synthetic.mllp", Data: append(append([]byte{11}, []byte(runnerMTLSMessage)...), 28, 13)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.RegisterCase(root, "messages", operation.CaseRegistration{Title: "Synthetic booking"}); err != nil {
		t.Fatal(err)
	}
	cases := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem})
	if cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatal(cases)
	}
	zero := 0
	draft := desktop.ConnectedTestDraft{Schema: desktop.ConnectedTestSchema, Boundary: desktop.ApplicationBoundary, Server: fhir.ID, Generation: connectedtest.Generation{Seed: 7, BaseTime: "2026-03-01T00:00:00Z"}, Variables: []desktop.ConnectedVariable{}, Steps: []desktop.ConnectedStep{{ID: "book", After: []string{}, Source: desktop.ConnectedSource{Case: cases.Page.Items[0].Ref, Identity: source.Identity, Occurrence: source.Events[0].ID}, V2: &desktop.ConnectedV2{}}}, Phases: []desktop.ConnectedPhase{{ID: "book", Name: "Booking transport", Steps: []string{"book"}, After: []connectedtest.PhaseDependency{}, Observations: []desktop.ConnectedPhaseObservation{{Dataset: "appointments", Observation: observation, When: "after"}}, Checks: []desktop.ConnectedCheck{{Name: "No downstream workflow claimed", Check: assertion.DatasetAssertion{ID: "empty", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments"}, Count: &zero}}}, Responses: []desktop.ConnectedResponseCheck{}, Validations: []desktop.ConnectedValidationCheck{}, Acks: []desktop.ConnectedAckCheck{{ID: "accepted", Name: "Peer accepted booking", Step: "book", Code: "AA"}}}}}
	test := save(desktop.TestItem, "saved-mtls-test", desktop.ItemDraft{Name: "Saved mutual TLS booking", ConnectedTest: &draft, TestLinks: &desktop.TestLinks{Environment: environment.ID, Reset: desktop.ResetFromEnvironment}})
	saved := save(desktop.SuiteItem, "saved-mtls-suite", desktop.ItemDraft{Name: "Saved mutual TLS suite", Suite: &desktop.SuiteDraft{Tags: []string{}, Concurrency: 1, Tests: []desktop.SuiteTestDraft{{ID: "book", Test: test, Parameter: "receiver", After: []string{}, Sequence: []string{}, Tags: []string{}}}, Datasets: []desktop.SuiteDataset{}, Environments: []desktop.SuiteEnvironment{{ID: "qa", Name: "QA", Bindings: []desktop.SuiteBinding{{Parameter: "receiver", Target: environment, Server: &fhir}}}}, Requirements: []desktop.SuiteRequirement{}, Exclusions: []desktop.SuiteExclusion{}}})
	baseline := app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.ApproveSuiteBaselineAction, Items: []desktop.ItemRef{saved}})
	if baseline.Review == nil || !baseline.Review.Ready {
		t.Fatal(baseline)
	}
	if r := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: baseline.Review.Token, IntentID: "baseline", Decisions: desktop.ReviewDecisions{Rationale: "Independently authored peer ACK and transport-only qualification"}}); r.State != desktop.Completed {
		t.Fatal(r)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: saved})
	if opened.Suite == nil || !opened.Suite.Runnable {
		t.Fatal(opened)
	}
	path := filepath.Join(root, "runner-suite.json")
	runnerMTLSWrite(t, path, []byte(opened.Suite.Document))
	review, err := suite.ReviewConnectedPromotion(path, "qa", "independent-python-mtls")
	if err != nil {
		t.Fatal(err)
	}
	promotionPath := filepath.Join(root, "promotion.json")
	promotion, err := suite.ApproveConnectedPromotion(path, "qa", "independent-python-mtls", review.Identity(), "Operator", "Exact saved mutual TLS definition", promotionPath)
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
	for i := range f.policy.Grants {
		f.policy.Grants[i].Project = ctx.ProjectID
	}
	for i := range f.policy.Tokens {
		f.policy.Tokens[i].Project = ctx.ProjectID
	}
	writePolicy(t, f.accessPath, f.policy)
	f.grants.Runners[0].Project, f.grants.Runners[0].Environment = ctx.ProjectID, "test"
	f.grants.Runners[0].Capability, f.grants.Runners[0].MaxSeconds, f.grants.Runners[0].MaxJobs = capability, 120, 10
	f.writeGrants()
	config := f.runnerConfig()
	config.Project, config.Environment = ctx.ProjectID, "test"
	request := suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(root, "execution"), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: "independent-python-mtls", Instance: "trusted-runner"}
	if _, err := os.Stat(marker); !os.IsNotExist(err) || len(p.accepted(t)) != 0 {
		t.Fatal("saving and preparing resolved an mTLS key or sent a payload")
	}
	return config, request, authority, marker, app, ctx, test
}

// handoffSavedRunnerMTLS exposes this exact saved definition to an installed
// native application only when an operator explicitly requests qualification.
// Waiting creates no grant and invokes no desktop execution callback.
func handoffSavedRunnerMTLS(t *testing.T, app *desktop.App, request desktop.RequestContext, test desktop.ItemRef, peer *runnerMTLSPeer) string {
	t.Helper()
	root := os.Getenv("READMIT_MTLS_RUNNER_NATIVE_DIR")
	if root == "" {
		return ""
	}
	if !filepath.IsAbs(root) {
		t.Fatal("native runner handoff directory must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	stop := filepath.Join(root, "stop")
	if _, err := os.Stat(stop); !os.IsNotExist(err) {
		t.Fatal("native handoff requires a fresh directory without a stop file")
	}
	home := filepath.Join(root, "home")
	state := filepath.Join(home, "Library", "Application Support", "readmit")
	received := filepath.Join(root, "vendor-license")
	if err := os.MkdirAll(received, 0700); err != nil {
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
	if activated := native.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: license, Trust: trust, Digest: connectedtest.Digest(raw), Author: "test-author", Device: "test-device", Authority: "test-runner"}); activated.State != desktop.Completed {
		t.Fatal(activated)
	}
	if opened := native.OpenNamedProject(request.Project); opened.State != desktop.Completed {
		t.Fatal(opened)
	}
	info := map[string]string{"home": home, "state": state, "project": request.Project, "test_id": test.ID, "test_revision": test.Revision, "test_name": "Saved mutual TLS booking", "peer_root": peer.root, "stop_file": stop}
	connectedlab.WriteJSON(t, filepath.Join(root, "fixture-info.json"), info)
	t.Logf("Same saved test native handoff ready: %s", filepath.Join(root, "fixture-info.json"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("same saved test native handoff timed out")
		case <-ticker.C:
			if _, err := os.Stat(stop); err == nil {
				events := peer.accepted(t)
				if len(events) != 1 || events[0]["control_id"] != "BOOK-1" || events[0]["client_sha256"] != peer.clientDigest || events[0]["payload_base64"] != base64.StdEncoding.EncodeToString([]byte(runnerMTLSMessage)) {
					t.Fatalf("native execution did not send this saved payload exactly once: %+v", events)
				}
				runs := app.ListCatalog(desktop.CatalogQuery{Context: request, Kind: desktop.RunItem})
				if runs.Page == nil {
					t.Fatal(runs)
				}
				for _, item := range runs.Page.Items {
					read := app.OpenRun(desktop.RunRequest{Context: request, Run: item.Ref})
					if read.State != desktop.Completed || read.Run == nil || read.Run.Lifecycle == nil || read.Run.Report == nil || item.Summary.Run == nil || item.Summary.Run.Test == nil || *item.Summary.Run.Test != test {
						continue
					}
					if read.Run.Result != desktop.RunPassed || len(read.Run.Lifecycle.Steps) != 1 || read.Run.Lifecycle.Steps[0].ACK != "AA" || read.Run.Lifecycle.Steps[0].Uncertain {
						t.Fatal("native run did not retain passed correlated ACK", read)
					}
					connectedlab.WriteJSON(t, filepath.Join(root, "native-result.json"), read)
					t.Logf("Native same saved test verified: test=%s revision=%s evidence=%s", test.ID, test.Revision, read.Run.Lifecycle.Identity)
					return filepath.Join(request.Project, read.Run.Report.Run)
				}
				t.Fatal("native execution was not retained against this exact saved test revision")
			}
		}
	}
}

func TestSavedConnectedMutualTLSActualRunnerUsesOwnAuthorityAndProvider(t *testing.T) {
	p := startRunnerMTLSPeer(t)
	f := newConnectedHubFixture(t)
	config, request, authority, marker, app, ctx, savedTest := savedRunnerMTLSSuite(t, f, p)
	nativeOutput := ""
	t.Cleanup(func() {
		destination := os.Getenv("READMIT_MTLS_EVIDENCE_DIR")
		if destination == "" && !t.Failed() {
			return
		}
		if destination == "" {
			var err error
			destination, err = os.MkdirTemp("", "readmit-700-runner-failure-")
			if err != nil {
				t.Log(err)
				return
			}
		}
		destination = filepath.Join(destination, "runner")
		if err := os.MkdirAll(destination, 0700); err != nil {
			t.Log(err)
			return
		}
		for _, name := range []string{"ready.json", "peer.jsonl", "ca.pem", "client.pem", "server.pem"} {
			if raw, err := os.ReadFile(filepath.Join(p.root, name)); err == nil {
				if err := os.WriteFile(filepath.Join(destination, name), raw, 0600); err != nil {
					t.Log(err)
				}
			}
		}
		if err := os.CopyFS(filepath.Join(destination, "execution"), os.DirFS(request.Output)); err != nil {
			t.Logf("execution preservation: %v", err)
		}
		if _, err := os.Stat(filepath.Join(ctx.Project, "dropped")); err == nil {
			if err := os.CopyFS(filepath.Join(destination, "dropped"), os.DirFS(filepath.Join(ctx.Project, "dropped"))); err != nil {
				t.Log(err)
			}
		}
		if nativeOutput != "" {
			if err := os.CopyFS(filepath.Join(destination, "native"), os.DirFS(nativeOutput)); err != nil {
				t.Log(err)
			}
		}
		t.Logf("synthetic runner qualification evidence: %s", destination)
	})
	// A genuine Desktop one-action token cannot replace the installed document.
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.RunTestAction, Items: []desktop.ItemRef{savedTest}})
	if review.Review == nil || !review.Review.Ready {
		t.Fatal(review)
	}
	tokenPath := filepath.Join(ctx.Project, "desktop-token.json")
	runnerMTLSWrite(t, tokenPath, []byte(review.Review.Token))
	if _, err := executeCustomerConnectedSuite(t, config, request, tokenPath); err == nil {
		t.Fatal("Desktop token substituted for finite runner authority")
	}
	if _, err := executeCustomerConnectedSuite(t, config, request, p.key); err == nil {
		t.Fatal("exported private key substituted for finite runner authority")
	}

	raw, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := customerrunner.DecodeConnectedAuthority(raw)
	if err != nil {
		t.Fatal(err)
	}
	expired := grant
	expired.IssuedAt = time.Now().Add(-2 * time.Minute)
	expired.Expires = time.Now().Add(-time.Minute)
	connectedlab.WriteJSON(t, authority, expired)
	if _, err := executeCustomerConnectedSuite(t, config, request, authority); err == nil {
		t.Fatal("expired finite authority permitted execution")
	} else {
		t.Logf("expired authority refusal: %v", err)
	}
	changed := grant
	changed.Input = strings.Repeat("a", 64)
	connectedlab.WriteJSON(t, authority, changed)
	if _, err := executeCustomerConnectedSuite(t, config, request, authority); err == nil {
		t.Fatal("changed installed input authority permitted execution")
	}
	runnerMTLSWrite(t, authority, raw)
	grants := f.grants.Runners
	f.grants.Runners = []runnerprotocol.ConnectedGrant{}
	f.writeGrants()
	refused := request
	refused.Instance = "revoked-runner"
	refused.Output = filepath.Join(ctx.Project, "revoked")
	refusedReport, refusedErr := executeCustomerConnectedSuite(t, config, refused, authority)
	t.Logf("revoked runner grant: exit=%d error=%v", refusedReport.ExitCode(), refusedErr)
	if refusedErr == nil && refusedReport.ExitCode() == 0 {
		t.Fatal("revoked hub grant permitted execution")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) || len(p.accepted(t)) != 0 {
		t.Fatal("refused authority resolved endpoint key or reached independent peer")
	}
	f.grants.Runners = grants
	f.writeGrants()
	nativeOutput = handoffSavedRunnerMTLS(t, app, ctx, savedTest, p)
	nativeAccepted := 0
	if nativeOutput != "" {
		nativeAccepted = 1
	}
	providerBefore, _ := os.ReadFile(marker)
	before := f.requests.Load()
	report, err := executeCustomerConnectedSuite(t, config, request, authority)
	if err != nil || report.ExitCode() != 0 || report.Executed != 1 || f.requests.Load() <= before {
		for _, job := range report.Jobs {
			if job.Flow != nil {
				t.Logf("flow: %+v", *job.Flow)
			}
		}
		t.Fatalf("actual saved runner execution: %+v %v", report, err)
	}
	events := p.accepted(t)
	if len(events) != 1+nativeAccepted || events[nativeAccepted]["control_id"] != "BOOK-1" || events[nativeAccepted]["client_sha256"] != p.clientDigest || events[nativeAccepted]["payload_base64"] != base64.StdEncoding.EncodeToString([]byte(runnerMTLSMessage)) {
		t.Fatalf("independent receiver did not verify exact client/payload: %+v", events)
	}
	if providerAfter, err := os.ReadFile(marker); err != nil || len(providerAfter) <= len(providerBefore) {
		t.Fatal("runner did not resolve its own endpoint key", err)
	}
	if len(report.Jobs) != 1 || report.Jobs[0].Flow == nil || len(report.Jobs[0].Flow.Phases) != 1 || report.Jobs[0].Flow.Phases[0].Wire == nil || report.Jobs[0].Flow.Phases[0].Wire.Verdict != "pass" {
		t.Fatal("runner lost independently expected correlated ACK", report)
	}
	retry := request
	retry.Output = filepath.Join(ctx.Project, "retry")
	if _, err := executeCustomerConnectedSuite(t, config, retry, authority); err == nil || len(p.accepted(t)) != 1+nativeAccepted {
		t.Fatal("claimed saved run silently resent", err)
	}

	// A separate intentional occurrence loses its ACK after acceptance. The
	// receiver's record is the independent witness; neither recovery nor the
	// same dispatch identity may send again.
	runnerMTLSWrite(t, filepath.Join(p.root, "mode"), []byte("drop"))
	dropped := request
	dropped.Instance = "dropped-runner"
	dropped.Output = filepath.Join(ctx.Project, "dropped")
	lost, err := executeCustomerConnectedSuite(t, config, dropped, authority)
	if err != nil || lost.ExitCode() != 2 || len(lost.Jobs) != 1 || lost.Jobs[0].Flow == nil || lost.Jobs[0].Flow.State != "uncertain" || len(p.accepted(t)) != 2+nativeAccepted {
		t.Fatalf("accepted payload without ACK lost uncertainty: %+v %v", lost, err)
	}
	duplicate := dropped
	duplicate.Output = filepath.Join(ctx.Project, "drop-retry")
	if _, err := executeCustomerConnectedSuite(t, config, duplicate, authority); err == nil || len(p.accepted(t)) != 2+nativeAccepted {
		t.Fatal("uncertain runner occurrence resent", err)
	}
	_ = p.command.Process.Kill()
	_ = p.command.Wait()
	p.command.Process = nil
	f.server.Close()
	if err := os.Remove(p.key); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(config.Key.Arguments[0]); err != nil {
		t.Fatal(err)
	}
	calls := f.requests.Load()
	keyReads, _ := os.ReadFile(marker)

	lostRetained, err := suite.OpenConnectedExecution(t.Context(), dropped.Output)
	if err != nil || lostRetained.Report.ExitCode() != 2 {
		t.Fatal("offline reading lost accepted-but-unacknowledged uncertainty", err)
	}
	retained, err := suite.OpenConnectedExecution(t.Context(), request.Output)
	if err != nil || retained.Report.ExitCode() != 0 {
		t.Fatal("offline saved execution needs a peer or secret", err)
	}
	afterReads, _ := os.ReadFile(marker)
	if f.requests.Load() != calls || !bytes.Equal(keyReads, afterReads) {
		t.Fatal("offline reading renewed network or credential activity")
	}
	sanitized, err := json.Marshal(retained.Report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sanitized, []byte("PRIVATE KEY")) || bytes.Contains(sanitized, []byte(f.token)) || bytes.Contains(sanitized, []byte(review.Review.Token)) {
		t.Fatal("retained report contains key material or bearer authority")
	}
	t.Logf("qualified saved test=%s revision=%s client_certificate_sha256=%s execution=%s", savedTest.ID, savedTest.Revision, p.clientDigest, retained.Identity)
}
