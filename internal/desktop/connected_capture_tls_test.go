package desktop_test

import (
	"crypto/x509"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/suite"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/secret"
)

func savedTLSCaptureSource(t *testing.T, f *connectedAuthoring, listener desktop.ListenerSettings) (desktop.ItemRef, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "key-read")
	if listener.Transport != desktop.PlainTransport {
		ca := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
		server := mtlsCert(t, &ca, false, false, x509.ExtKeyUsageServerAuth)
		key := filepath.Join(t.TempDir(), "server-key.pem")
		provider := filepath.Join(t.TempDir(), "key-provider")
		mtlsWrite(t, key, server.private)
		if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\nexec /bin/cat \"$2\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		mtlsWrite(t, filepath.Join(f.root, "return-cert.pem"), server.pem)
		listener.TLSCertificate, listener.TLSKeyReference = "return-cert.pem", "return-key"
		if listener.Transport == desktop.MutualTLSTransport {
			mtlsWrite(t, filepath.Join(f.root, "return-clients.pem"), ca.pem)
			listener.ClientCA = "return-clients.pem"
		}
		registered := f.app.SaveCredential(desktop.CredentialSaveRequest{Context: f.context, Name: listener.TLSKeyReference, Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged, Address: listener.Address(), Command: provider, Arguments: []string{marker, key}})
		if registered.State != desktop.Completed {
			t.Fatal(registered)
		}
	}
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SourceItem, IntentID: "secure-return-source", Draft: desktop.ItemDraft{Name: "Engine return", Source: &desktop.CaptureSourceDraft{Type: desktop.MLLPListenerSource, Listener: &listener}}})
	if saved.Saved == nil {
		t.Fatalf("source: %+v", saved)
	}
	return *saved.Saved, marker
}

func TestSavedReceivedHL7ApprovedTransportsPrepareWithoutEffects(t *testing.T) {
	for _, mode := range []desktop.ListenerTransport{desktop.PlainTransport, desktop.TLSTransport, desktop.MutualTLSTransport} {
		t.Run(string(mode), func(t *testing.T) {
			f := newConnectedAuthoring(t)
			listener := desktop.ListenerSettings{BindAddress: "192.0.2.10", Port: 25788, Transport: mode, AllowRemote: true, ConnectionLimit: 2, IdleTimeout: "1s", AckCode: "AA"}
			source, marker := savedTLSCaptureSource(t, f, listener)
			opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: source})
			if opened.Draft == nil || opened.Draft.Source.Listener.Transport != mode || !opened.Draft.Source.Listener.AllowRemote {
				t.Fatalf("source did not reopen: %+v", opened)
			}
			observation := captureObservation(t, f, source)
			saved := savedCaptureTest(t, f, observation)
			issued := f.app.IssueExchangeRuntimeMarker(f.context)
			review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{saved}, Run: &desktop.RunActionOptions{RuntimeMarker: issued.Marker}})
			found := false
			for _, endpoint := range review.Run.Lifecycle.Endpoints {
				if endpoint.Address == net.JoinHostPort(listener.BindAddress, "25788") {
					found = true
				}
			}
			if !found {
				t.Fatal("review omitted actual bind endpoint")
			}
			collectors := review.Run.Lifecycle.Collectors
			if len(collectors) != 1 || collectors[0].Capture == nil || collectors[0].Capture.Name != "Engine return" || collectors[0].Capture.Revision != source.Revision || collectors[0].Capture.Transport != mode || !collectors[0].Capture.Remote || collectors[0].Address != listener.Address() {
				t.Fatalf("receive review missing exact source: %+v", collectors)
			}
			if mode != desktop.PlainTransport && (collectors[0].Credential != "return-key" || collectors[0].Generation != "1") {
				t.Fatal("review omitted key reference generation")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("passive source/test preparation resolved a private key")
			}
			if f.lab.Creates.Load() != 0 {
				t.Fatal("preparation executed setup")
			}
		})
	}
}

func TestSavedCaptureNeverRetainsAPrivateKeySelectedAsPublicCertificate(t *testing.T) {
	f := newConnectedAuthoring(t)
	source, _ := savedTLSCaptureSource(t, f, desktop.ListenerSettings{BindAddress: "127.0.0.1", Port: 25789, Transport: desktop.TLSTransport, ConnectionLimit: 1, IdleTimeout: "1s", AckCode: "AA"})
	observation := captureObservation(t, f, source)
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: observation})
	if opened.Draft == nil {
		t.Fatal(opened)
	}
	private := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth).private
	mtlsWrite(t, filepath.Join(f.root, "return-cert.pem"), private)
	opened.Draft.Name = "Wrong certificate selection"
	result := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, IntentID: "misfiled-private-key", Draft: *opened.Draft})
	if result.State != desktop.Failed || result.Saved != nil {
		t.Fatal("private key bytes accepted as retained public certificate", result)
	}
}

func TestSavedCaptureSuitePublishesRuntimeTemplateWithStableApproval(t *testing.T) {
	f := newConnectedAuthoring(t)
	source, marker := savedTLSCaptureSource(t, f, desktop.ListenerSettings{BindAddress: "192.0.2.10", Port: 25790, AllowRemote: true, Transport: desktop.MutualTLSTransport, ConnectionLimit: 1, IdleTimeout: "1s", AckCode: "AA"})
	observation := captureObservation(t, f, source)
	test := savedCaptureTest(t, f, observation)
	draft := desktop.SuiteDraft{Tags: []string{}, Concurrency: 1, Tests: []desktop.SuiteTestDraft{{ID: "capture", Test: test, Parameter: "engine", After: []string{}, Sequence: []string{}, Tags: []string{}}}, Datasets: []desktop.SuiteDataset{}, Environments: []desktop.SuiteEnvironment{{ID: "qa", Name: "QA", Bindings: []desktop.SuiteBinding{{Parameter: "engine", Target: f.v2}}}}, Requirements: []desktop.SuiteRequirement{}, Exclusions: []desktop.SuiteExclusion{}}
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SuiteItem, IntentID: "capture-suite", Draft: desktop.ItemDraft{Name: "Captured output suite", Suite: &draft}})
	if saved.Saved == nil {
		t.Fatalf("save: %+v", saved)
	}
	if approved := f.approve(t, desktop.ApproveSuiteBaselineAction, *saved.Saved, "capture-baseline", nil); approved.State != desktop.Completed {
		t.Fatal(approved)
	}
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	if opened.Suite == nil || !opened.Suite.Runnable {
		t.Fatal(opened)
	}
	var document suite.ConnectedDocument
	if err := json.Unmarshal([]byte(opened.Suite.Document), &document); err != nil {
		t.Fatal(err)
	}
	binding := document.Environments[0].Bindings[0]
	planPath, configPath := filepath.Join(f.root, binding.Plan), filepath.Join(f.root, binding.Config)
	plan, err := connectedtest.OpenFlowPlan(planPath)
	if err != nil || !plan.RuntimeScoped() {
		t.Fatal("suite omitted explicit runtime template", err)
	}
	first, err := connectedrun.PrepareFlow(planPath, configPath, "runner-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := connectedrun.PrepareFlow(planPath, configPath, "runner-second")
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.InputIdentity()
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.InputIdentity()
	if err != nil || a != b {
		t.Fatal("new occurrence changed approved inputs", err)
	}
	if first.Bindings()["reschedule:stimulus"] == second.Bindings()["reschedule:stimulus"] {
		t.Fatal("two occurrences share concrete stimulus")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("suite preparation resolved capture key")
	}
}

func TestSavedReceivedTLSCaptureRefusesChangedReviewBeforeEffects(t *testing.T) {
	for _, change := range []string{"key-generation", "key-purpose", "key-provider", "certificate", "client-ca", "listener-revision"} {
		t.Run(change, func(t *testing.T) {
			f := newConnectedAuthoring(t)
			source, marker := savedTLSCaptureSource(t, f, desktop.ListenerSettings{BindAddress: "192.0.2.10", Port: 25791, AllowRemote: true, Transport: desktop.MutualTLSTransport, ConnectionLimit: 1, IdleTimeout: "1s", AckCode: "AA"})
			observation := captureObservation(t, f, source)
			saved := savedCaptureTest(t, f, observation)
			issued := f.app.IssueExchangeRuntimeMarker(f.context)
			review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{saved}, Run: &desktop.RunActionOptions{RuntimeMarker: issued.Marker}})
			switch change {
			case "certificate", "client-ca":
				name := "return-cert.pem"
				if change == "client-ca" {
					name = "return-clients.pem"
				}
				mtlsWrite(t, filepath.Join(f.root, name), mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth).pem)
			case "listener-revision":
				opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: source})
				if opened.Draft == nil {
					t.Fatal(opened)
				}
				opened.Draft.Source.Listener.ConnectionLimit = 3
				updated := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SourceItem, Item: source.ID, BaseRevision: source.Revision, IntentID: "changed-listener", Draft: *opened.Draft})
				if updated.Saved == nil {
					t.Fatal(updated)
				}
			default:
				path := filepath.Join(f.root, desktop.ProjectSecrets)
				document, err := secret.ReadStore(path)
				if err != nil {
					t.Fatal(err)
				}
				for i := range document.References {
					if document.References[i].Name == "return-key" {
						switch change {
						case "key-generation":
							document.References[i].Generation++
						case "key-purpose":
							document.References[i].Purpose = secret.SourceEndpoint
						case "key-provider":
							document.References[i].Arguments = append(document.References[i].Arguments, "changed")
						}
					}
				}
				if err := secret.WriteStore(path, document); err != nil {
					t.Fatal(err)
				}
			}
			actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "stale-capture"})
			if actual.Outcome != desktop.ActionStale {
				t.Fatalf("changed input kept old review: %+v", actual)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("stale capture resolved key")
			}
			if f.lab.Creates.Load() != 0 {
				t.Fatal("stale capture ran setup")
			}
		})
	}
}
