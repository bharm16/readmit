package desktop_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/testlicense"
)

type mtlsCertificate struct {
	cert         *x509.Certificate
	key          *ecdsa.PrivateKey
	pem, private []byte
}

func mtlsCert(t *testing.T, parent *mtlsCertificate, ca, expired bool, usage x509.ExtKeyUsage) mtlsCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Synthetic saved-test peer " + serial.String()}, DNSNames: []string{"receiver.test"}, NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: ca, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	if ca {
		template.KeyUsage |= x509.KeyUsageCertSign
		template.ExtKeyUsage = nil
	}
	if expired {
		template.NotAfter = time.Now().Add(-time.Hour)
	}
	issuer, signing := template, key
	if parent != nil {
		issuer, signing = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, key.Public(), signing)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return mtlsCertificate{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), private: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})}
}
func mtlsWrite(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func savedMTLSEnvironment(t *testing.T, f *connectedAuthoring, address string, ca, client mtlsCertificate) (desktop.ItemRef, string) {
	t.Helper()
	folder := t.TempDir()
	marker := filepath.Join(folder, "provider-used")
	provider := filepath.Join(folder, "provider")
	key := filepath.Join(folder, "client-key.pem")
	mtlsWrite(t, key, client.private)
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf invoked >> \"$1\"\nexec /bin/cat \"$2\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	caPath, clientPath := filepath.Join(folder, "ca.pem"), filepath.Join(folder, "client.pem")
	mtlsWrite(t, caPath, ca.pem)
	mtlsWrite(t, clientPath, client.pem)
	registered := f.app.SaveCredential(desktop.CredentialSaveRequest{Context: f.context, Name: "mllp-client", Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged, Address: address, Command: provider, Arguments: []string{marker, key}})
	if registered.State != desktop.Completed {
		t.Fatalf("register: %+v", registered)
	}
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.v2})
	if opened.Draft == nil {
		t.Fatal(opened)
	}
	draft := *opened.Draft
	draft.Name = "Mutual TLS receiver"
	draft.Environment.Address = address
	draft.Environment.Transport = "tls"
	draft.Environment.ServerName = "receiver.test"
	draft.Environment.CAFile = caPath
	draft.Environment.ClientCertificate = clientPath
	draft.Environment.Credential = replay.Credential{Reference: "mllp-client"}
	return saveEnvironment(t, f.app, f.context, desktop.SaveItemRequest{IntentID: "mtls-environment", Draft: draft}), marker
}
func mtlsDraft(f *connectedAuthoring) desktop.ConnectedTestDraft {
	d := f.reschedule()
	zero := 0
	d.Phases[0].Checks = []desktop.ConnectedCheck{{Name: "No downstream workflow claimed", Check: assertion.DatasetAssertion{ID: "no-downstream", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments"}, Count: &zero}}}
	return d
}
func TestSavedConnectedMutualTLSPreparesWithoutResolvingKey(t *testing.T) {
	f := newConnectedAuthoring(t)
	ca := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
	client := mtlsCert(t, &ca, false, false, x509.ExtKeyUsageClientAuth)
	env, marker := savedMTLSEnvironment(t, f, "127.0.0.1:25777", ca, client)
	test := f.save(t, "Saved mutual TLS", "mtls-save", mtlsDraft(f), env.ID)
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: test})
	if opened.Draft == nil || opened.Draft.ConnectedTest == nil {
		t.Fatal(opened)
	}
	reviewed := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	if reviewed.Run == nil || reviewed.Run.Environment == nil || reviewed.Run.Environment.ID != env.ID || reviewed.Run.MessageCount == nil || *reviewed.Run.MessageCount != 2 || reviewed.Run.Lifecycle.Transport != "mtls" {
		t.Fatalf("review: %+v", reviewed)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("passive authoring/preparation resolved client key")
	}
}

type savedMTLSPeer struct {
	root, address string
	ca, client    mtlsCertificate
}

func startSavedMTLSPeer(t *testing.T) *savedMTLSPeer {
	t.Helper()
	python := os.Getenv("READMIT_MTLS_PYTHON")
	if python == "" {
		t.Skip("opt in with READMIT_MTLS_PYTHON selecting an independent Python SSL receiver runtime")
	}
	if !filepath.IsAbs(python) {
		t.Fatal("READMIT_MTLS_PYTHON must be absolute")
	}
	p := &savedMTLSPeer{root: t.TempDir()}
	p.ca = mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
	p.client = mtlsCert(t, &p.ca, false, false, x509.ExtKeyUsageClientAuth)
	server := mtlsCert(t, &p.ca, false, false, x509.ExtKeyUsageServerAuth)
	for name, raw := range map[string][]byte{"ca.pem": p.ca.pem, "server.pem": server.pem, "server-key.pem": server.private} {
		mtlsWrite(t, filepath.Join(p.root, name), raw)
	}
	command := exec.Command(python, "testdata/mtls_peer.py", p.root)
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		if diagnostic.Len() > 0 {
			t.Log(diagnostic.String())
		}
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
				t.Log("independent runtime:", string(raw))
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("independent peer did not become ready")
	return nil
}
func (p *savedMTLSPeer) events(t *testing.T) []map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.root, "peer.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]string{}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event map[string]string
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		out = append(out, event)
	}
	return out
}
func (p *savedMTLSPeer) accepted(t *testing.T) []map[string]string {
	out := []map[string]string{}
	for _, e := range p.events(t) {
		if e["event"] == "accepted" {
			out = append(out, e)
		}
	}
	return out
}
func mtlsRetain(t *testing.T, p *savedMTLSPeer, result desktop.ReviewedActionResult) {
	t.Helper()
	root := os.Getenv("READMIT_MTLS_EVIDENCE_DIR")
	if root == "" {
		return
	}
	root = filepath.Join(root, strings.ReplaceAll(t.Name(), "/", "-"))
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ready.json", "peer.jsonl", "ca.pem", "server.pem"} {
		if raw, err := os.ReadFile(filepath.Join(p.root, name)); err == nil {
			mtlsWrite(t, filepath.Join(root, name), raw)
		}
	}
	raw, err := json.Marshal(result, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	mtlsWrite(t, filepath.Join(root, "result.json"), raw)
	if result.Lifecycle != nil && result.Lifecycle.Output != "" {
		if err := os.CopyFS(filepath.Join(root, "readmit-evidence"), os.DirFS(filepath.Join(result.Context.Project, result.Lifecycle.Output))); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSavedConnectedMutualTLSIndependentPeer(t *testing.T) {
	for _, name := range []string{"trusted", "repeated-step", "missing-client", "expired-client", "untrusted-client", "wrong-key", "key-unavailable", "server-name", "server-ca", "drop-before-ack"} {
		t.Run(name, func(t *testing.T) {
			p := startSavedMTLSPeer(t)
			f := newConnectedAuthoring(t)
			client := p.client
			if name == "expired-client" {
				client = mtlsCert(t, &p.ca, false, true, x509.ExtKeyUsageClientAuth)
			}
			if name == "untrusted-client" {
				other := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
				client = mtlsCert(t, &other, false, false, x509.ExtKeyUsageClientAuth)
			}
			if name == "wrong-key" {
				other := mtlsCert(t, &p.ca, false, false, x509.ExtKeyUsageClientAuth)
				client.private = other.private
			}
			env, marker := savedMTLSEnvironment(t, f, p.address, p.ca, client)
			if name == "key-unavailable" {
				if err := os.Remove(filepath.Join(filepath.Dir(marker), "client-key.pem")); err != nil {
					t.Fatal(err)
				}
			}
			if name == "missing-client" || name == "server-name" || name == "server-ca" {
				opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: env})
				draft := *opened.Draft
				switch name {
				case "missing-client":
					draft.Environment.ClientCertificate = ""
					draft.Environment.Credential = replay.Credential{}
				case "server-name":
					draft.Environment.ServerName = "wrong.test"
				case "server-ca":
					other := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
					ca := filepath.Join(t.TempDir(), "other.pem")
					mtlsWrite(t, ca, other.pem)
					draft.Environment.CAFile = ca
				}
				saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.EnvironmentItem, Item: env.ID, BaseRevision: env.Revision, IntentID: "negative-target", Draft: draft})
				if saved.Saved == nil {
					t.Fatal(saved)
				}
				env = *saved.Saved
			}
			if name == "drop-before-ack" {
				mtlsWrite(t, filepath.Join(p.root, "mode"), []byte("drop"))
			}
			draft := mtlsDraft(f)
			if name == "repeated-step" {
				draft.Steps[1].Source = draft.Steps[0].Source
				first, second := draft.Phases[0], draft.Phases[0]
				first.ID, first.Steps = "first", []string{"step-1"}
				first.Acks = []desktop.ConnectedAckCheck{{ID: "first-accepted", Name: "First accepted", Step: "step-1", Code: "AA"}}
				second.ID, second.Steps = "repeat", []string{"step-2"}
				second.After = []connectedtest.PhaseDependency{{Phase: "first", Requires: "pass"}}
				draft.Phases = []desktop.ConnectedPhase{first, second}
			}
			test := f.save(t, "Mutual TLS qualification", "peer-test", draft, env.ID)
			review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
			if len(p.events(t)) != 0 {
				t.Fatal("passive preparation contacted peer")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("passive preparation resolved key")
			}
			result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "actual-send"})
			mtlsRetain(t, p, result)
			t.Logf("retained outcome: state=%s outcome=%s lifecycle=%+v", result.State, result.Outcome, result.Lifecycle)
			if name == "wrong-key" || name == "key-unavailable" {
				if result.Outcome != desktop.ActionRefused || result.Lifecycle != nil || len(p.events(t)) != 0 {
					t.Fatal("wrong key did not refuse before dial", result, p.events(t))
				}
				partial := filepath.Join(f.root, "job-001", "phases", "reschedule", "transport")
				refusal, err := connectedtransport.ReadRefusal(partial)
				want := "client-key-pair-invalid"
				if name == "key-unavailable" {
					want = "client-key-unavailable"
				}
				if err != nil || refusal.Reason != want {
					t.Fatalf("wrong key lost actual refusal diagnostic: %+v %v", refusal, err)
				}
				if _, err := connectedtransport.OpenEvidence(partial); err == nil {
					t.Fatal("partial refusal became sealed execution evidence")
				}
				if root := os.Getenv("READMIT_MTLS_EVIDENCE_DIR"); root != "" {
					raw, _ := json.Marshal(refusal)
					mtlsWrite(t, filepath.Join(root, strings.ReplaceAll(t.Name(), "/", "-"), "refusal.json"), raw)
				}

				calls, err := os.ReadFile(marker)
				if err != nil || string(calls) != "invoked" {
					t.Fatal("wrong-key control did not exercise the registered key provider")
				}
				return
			}
			if result.Lifecycle == nil || result.Run == nil {
				t.Fatalf("execution omitted retained result: %+v", result)
			}
			accepted := p.accepted(t)
			connections := 0
			diagnostics := ""
			for _, event := range p.events(t) {
				if event["event"] == "connection" {
					connections++
				}
				diagnostics += " " + event["reason"]
			}
			wantConnections := 1
			if name == "repeated-step" {
				wantConnections = 2
			}
			if connections != wantConnections {
				t.Fatalf("unexpected reconnect/fallback: %d", connections)
			}
			reason := map[string]string{"missing-client": "peer did not return a certificate", "expired-client": "certificate has expired", "untrusted-client": "unable to get local issuer certificate", "drop-before-ack": "deliberate drop after acceptance before ACK"}[name]
			if reason != "" && !strings.Contains(diagnostics, reason) {
				t.Fatalf("negative did not exercise its declared refusal: %s", diagnostics)
			}
			switch name {
			case "trusted", "repeated-step":
				if result.Lifecycle.Verdict != assertion.VerdictPass || len(accepted) != 2 {
					t.Fatalf("trusted result/peer disagreement: %+v %+v", result, accepted)
				}
				messages := []string{bookMessage, rescheduleMessage}
				if name == "repeated-step" {
					messages[1] = bookMessage
				}
				for i, want := range messages {
					raw, err := base64.StdEncoding.DecodeString(accepted[i]["payload_base64"])
					if err != nil || string(raw) != want || accepted[i]["client_sha256"] != connectedtest.Digest(client.cert.Raw) {
						t.Fatalf("independent peer payload/certificate mismatch: %+v", accepted[i])
					}
				}
				detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run})
				if detail.Run == nil || detail.Run.Lifecycle == nil || len(detail.Run.Lifecycle.Steps) != 2 || detail.Run.Lifecycle.Steps[0].ACK != "AA" || detail.Run.Lifecycle.Steps[1].ACK != "AA" {
					t.Fatal("correlated ACK evidence missing", detail)
				}
			case "drop-before-ack":
				if len(accepted) != 1 || result.Outcome != desktop.ActionUncertain || result.Lifecycle.State != "uncertain" {
					t.Fatal("lost uncertainty or resent accepted message", result, accepted)
				}
			default:
				if len(accepted) != 0 || result.Lifecycle.Verdict == assertion.VerdictPass {
					t.Fatal("negative admitted payload or passed", result, accepted)
				}
			}
			before := len(p.events(t))
			keyBefore, _ := os.ReadFile(marker)
			if name == "trusted" {
				mtlsShareOffline(t, f, result, client.private)
			}
			_ = f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run})
			repeated := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "actual-send"})
			if !repeated.Replayed {
				t.Fatal("repeat intent did not return retained result")
			}
			if len(p.events(t)) != before {
				t.Fatal("offline readback or repeated consent reconnected")
			}
			keyAfter, _ := os.ReadFile(marker)
			if !bytes.Equal(keyBefore, keyAfter) {
				t.Fatal("readback resolved key")
			}
		})
	}
}
func TestSavedConnectedMutualTLSRejectsChangedReviewBeforeEffects(t *testing.T) {
	for _, name := range []string{"credential-generation", "credential-purpose", "credential-address", "credential-provider", "client-certificate", "server-ca", "environment"} {
		t.Run(name, func(t *testing.T) {
			f := newConnectedAuthoring(t)
			ca := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
			client := mtlsCert(t, &ca, false, false, x509.ExtKeyUsageClientAuth)
			env, marker := savedMTLSEnvironment(t, f, "127.0.0.1:25777", ca, client)
			test := f.save(t, "Review fences", "fence-test", mtlsDraft(f), env.ID)
			review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
			opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: env})
			target := opened.Draft.Environment
			switch name {
			case "client-certificate":
				replacement := mtlsCert(t, &ca, false, false, x509.ExtKeyUsageClientAuth)
				mtlsWrite(t, target.ClientCertificate, replacement.pem)
			case "server-ca":
				replacement := mtlsCert(t, nil, true, false, x509.ExtKeyUsageServerAuth)
				mtlsWrite(t, target.CAFile, replacement.pem)
			case "environment":
				target.ServerName = "changed.test"
				saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.EnvironmentItem, Item: env.ID, BaseRevision: env.Revision, IntentID: "changed-environment", Draft: *opened.Draft})
				if saved.Saved == nil {
					t.Fatal(saved)
				}
			default:
				store, err := secret.ReadStore(filepath.Join(f.root, desktop.ProjectSecrets))
				if err != nil {
					t.Fatal(err)
				}
				ref := &store.References[0]
				switch name {
				case "credential-generation":
					ref.Generation++
				case "credential-purpose":
					ref.Purpose = secret.SourceEndpoint
				case "credential-address":
					ref.Address = "127.0.0.1:25778"
				case "credential-provider":
					ref.Arguments = append(ref.Arguments, "changed")
				}
				connectedlab.WriteJSON(t, filepath.Join(f.root, desktop.ProjectSecrets), store)
			}
			result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "stale-send"})
			if result.Outcome != desktop.ActionStale {
				t.Fatalf("changed authority not stale: %+v", result)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("stale review resolved key")
			}
		})
	}
}
func TestSavedConnectedMutualTLSNativeFixture(t *testing.T) {
	root := os.Getenv("READMIT_MTLS_NATIVE_DIR")
	if root == "" {
		t.Skip("explicit native qualification handoff only")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("native fixture directory must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	p := startSavedMTLSPeer(t)
	f := newConnectedAuthoring(t)
	env, _ := savedMTLSEnvironment(t, f, p.address, p.ca, p.client)
	test := f.save(t, "Mutual TLS qualification", "native-test", mtlsDraft(f), env.ID)
	home := filepath.Join(root, "home")
	state := filepath.Join(home, "Library", "Application Support", "readmit")
	received := filepath.Join(root, "vendor-license")
	if err := os.MkdirAll(received, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := testlicense.CreateUnactivated(received); err != nil {
		t.Fatal(err)
	}
	license := filepath.Join(received, "entitlement.json")
	trust := filepath.Join(received, "trust.json")
	document, err := os.ReadFile(license)
	if err != nil {
		t.Fatal(err)
	}
	app := desktop.NewWithInstalledLicense(&chooser{}, desktop.ShellDocuments{Folder: state}, filepath.Join(state, "license"))
	activated := app.ActivateLicense(desktop.LicenseActivateRequest{Entitlement: license, Trust: trust, Digest: connectedtest.Digest(document), Author: "test-author", Device: "test-device", Authority: "test-runner"})
	if activated.State != desktop.Completed {
		t.Fatal(activated)
	}
	if opened := app.OpenNamedProject(f.root); opened.State != desktop.Completed {
		t.Fatal(opened)
	}
	stop := filepath.Join(root, "stop")
	raw, _ := json.Marshal(map[string]string{"home": home, "state": state, "project": f.root, "peer_root": p.root, "stop_file": stop, "test_name": "Mutual TLS qualification", "test_id": test.ID})
	mtlsWrite(t, filepath.Join(root, "fixture-info.json"), raw)
	t.Log("Native fixture ready:", string(raw))
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("native handoff timed out")
		case <-time.After(time.Second):
			if _, err := os.Stat(stop); err == nil {
				mtlsRetain(t, p, desktop.ReviewedActionResult{})
				fmt.Println("native peer events", p.events(t))
				return
			}
		}
	}
}

func mtlsShareOffline(t *testing.T, f *connectedAuthoring, actual desktop.ReviewedActionResult, key []byte) {
	t.Helper()
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ReportItem, IntentID: "mtls-report", Draft: desktop.ItemDraft{Name: "Mutual TLS evidence", Report: &desktop.ReportDraft{Title: "Mutual TLS evidence", Run: *actual.Run}}})
	if saved.Saved == nil {
		t.Fatal(saved)
	}
	dialogs := &chooser{folder: f.root}
	exporter := newApp(t, dialogs)
	out := filepath.Join(t.TempDir(), "mtls-extract")
	reviewed := prepareShare(t, exporter, dialogs, f.context, *saved.Saved, desktop.ReportShareOptions{Format: "json", ConnectedMode: "value-free-extract"}, out)
	if reviewed.Review == nil || !reviewed.Review.Ready {
		t.Fatal(reviewed)
	}
	result := exporter.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: reviewed.Review.Token, IntentID: "mtls-share"})
	if result.Outcome != desktop.ActionCompleted {
		t.Fatal(result)
	}
	if _, err := report.OpenExtract(out); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{filepath.Join(f.root, actual.Lifecycle.Output), out} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(raw, key) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(key))) || bytes.Contains(raw, []byte("connected-read")) || bytes.Contains(raw, []byte("connected-setup")) || bytes.Contains(raw, []byte("connected-cleanup")) {
				t.Fatal("private key escaped into retained evidence or sharing")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
