package desktop_test

import (
	"context"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/secret"
)

// One disposable TLS cluster and least-privilege observation principal, using
// the same explicit binary selection as the existing PostgreSQL fixture owner.
// Version evidence comes from the actual selected binary, never a requested pin.
type connectedPostgres struct {
	bin, root, port, address string
	t                        *testing.T
}

func startConnectedPostgres(t *testing.T, f *connectedAuthoring) *connectedPostgres {
	t.Helper()
	bin := os.Getenv("READMIT_POSTGRES_BIN")
	if bin == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("READMIT_POSTGRES_BIN required in CI")
		}
		t.Skip("select a real PostgreSQL bin folder")
	}
	version, err := exec.Command(filepath.Join(bin, "postgres"), "--version").Output()
	if err != nil {
		t.Fatal("selected PostgreSQL cannot run")
	}
	t.Log(strings.TrimSpace(string(version)))
	root, err := os.MkdirTemp("", "readmit-connected-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	p := &connectedPostgres{bin: bin, root: root, t: t}
	data := filepath.Join(root, "data")
	p.run("initdb", "-D", data, "--auth-local=trust", "--auth-host=reject", "-U", "lab_owner", "--no-locale", "--encoding=UTF8")
	certificate := f.fixture.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), "server.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), "pg_hba.conf": []byte("local all all trust\nhostssl application observer 127.0.0.1/32 trust\nhost all all 127.0.0.1/32 reject\n")} {
		if err := os.WriteFile(filepath.Join(data, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.address = socket.Addr().String()
	_, p.port, _ = net.SplitHostPort(p.address)
	socket.Close()
	p.run("pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-w", "-t", "20", "-o", "-h 127.0.0.1 -p "+p.port+" -k "+root+" -c ssl=on", "start")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		exec.CommandContext(ctx, filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "immediate", "-w", "stop").Run()
	})
	p.run("psql", "-h", root, "-p", p.port, "-U", "lab_owner", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "CREATE ROLE observer LOGIN")
	p.run("createdb", "-h", root, "-p", p.port, "-U", "lab_owner", "application")
	p.sql("CREATE TABLE private_appointments (appointment text,status text); CREATE VIEW observed AS SELECT appointment,status FROM private_appointments; GRANT SELECT ON observed TO observer;")
	return p
}
func (p *connectedPostgres) run(name string, args ...string) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, filepath.Join(p.bin, name), args...).CombinedOutput(); err != nil {
		p.t.Fatalf("independent synthetic PostgreSQL %s failed: %v %s", name, err, out)
	}
}
func (p *connectedPostgres) sql(statement string) {
	p.run("psql", "-h", p.root, "-p", p.port, "-U", "lab_owner", "-d", "application", "-v", "ON_ERROR_STOP=1", "-c", statement)
}

func saveDatabaseObservation(t *testing.T, f *connectedAuthoring, p *connectedPostgres) desktop.ItemRef {
	t.Helper()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.fixture.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(f.root, "database-ca.pem"), ca, 0o600); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(t.TempDir(), "database-reference.sh")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'synthetic-database-reference-secret'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	registered := f.app.SaveCredential(desktop.CredentialSaveRequest{Context: f.context, Name: "database-observer", Purpose: secret.SourceEndpoint, Store: secret.CustomerManaged, Address: p.address, Command: provider, Arguments: []string{}})
	if registered.State != desktop.Completed {
		t.Fatalf("reference registration: %+v", registered)
	}
	scope := observewindow.Source{Kind: observesource.DatabaseQuery, Identity: "independent-postgres", Scope: "appointments"}
	source := observesource.Source{Schema: observesource.SchemaDatabase, Observes: scope, Enabled: true, Freshness: observesource.Freshness{MaxAge: "30s"}, Database: &observesource.Database{Driver: "postgresql", Address: p.address, Classification: "nonproduction", Name: "application", Username: "observer", CAFile: filepath.Join(f.root, "database-ca.pem"), ServerName: "example.com", Credential: observesource.DatabaseCredential{}, View: []string{"public", "observed"}, RecordKey: "appointment", KeyType: "text", Filters: []observesource.DatabaseFilter{}, Limits: &observesource.DatabaseLimits{Timeout: "5s", MaxRows: 10, MaxBytes: 65536}}}
	window := observewindow.Window{Schema: observewindow.WindowSchema, Source: scope, Watermark: observewindow.Watermark{Kind: "none"}, PreExisting: observewindow.PreExisting{Declaration: observewindow.DeclaredEmpty}, Completion: observewindow.Rule{Deadline: "3s", QuietPeriod: "10ms", StableSamples: 2, MaxRecords: 10, MaxSamples: 20}}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "database-appointments", Format: "database", Order: "unordered", Columns: []dataset.Column{{Name: "appointment", Type: "text", Locator: importer.Locator{"appointment"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 5000}}
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, IntentID: "database-observation", Draft: desktop.ItemDraft{Name: "Actual database appointments", Observation: &desktop.ObservationDraft{Source: source, Window: window, Credential: "database-observer", Connected: &desktop.ConnectedObservation{Schema: desktop.ConnectedObservationSchema, Environment: f.v2.ID, Namespace: "appointments", Phase: "both", Baseline: "before-run", BusinessKeys: []desktop.BusinessKeyMapping{{Field: "appointment", Variable: "appointment-key"}}, Projection: &projection, Completion: observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 100, SampleMS: 100, MaxGapMS: 1000, MaxSamples: 400, MaxRecords: 10, MaxBytes: 65536}}}}})
	if saved.Saved == nil {
		t.Fatalf("database observation: %+v", saved)
	}
	reopened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	if reopened.Draft == nil || reopened.Draft.Observation == nil || reopened.Draft.Observation.Connected == nil {
		t.Fatalf("saved database setup unavailable: %+v", reopened)
	}
	held := reopened.Draft.Observation
	if held.Connected.Projection.Identity() != projection.Identity() || held.Connected.Completion.MaxGapMS != 1000 || len(held.Connected.BusinessKeys) != 1 || held.Connected.BusinessKeys[0].Variable != "appointment-key" || held.Credential != "database-observer" {
		t.Fatalf("database choices changed through save/reopen: %+v", held)
	}
	return *saved.Saved
}
func TestSavedConnectedDatabaseNormalRunUsesRegisteredReferenceAndActualRows(t *testing.T) {
	f := newConnectedAuthoring(t)
	p := startConnectedPostgres(t, f)
	observation := saveDatabaseObservation(t, f, p)

	draft := captureTestDraft(f, observation)
	draft.Boundary = desktop.ApplicationBoundary
	draft.Steps[0].V2 = &desktop.ConnectedV2{}
	draft.Phases[0].Checks = draft.Phases[0].Checks[:2]
	draft.Phases[0].Checks[1].Check.Column = "status"
	test := f.save(t, "Database appointment status", "database-test", draft, f.v2.ID)

	var mode atomic.Value
	mode.Store("wrong")
	databaseErrors := make(chan error, 10)
	f.engine.SetAfter(func() {
		status := mode.Load().(string)
		statement := "INSERT INTO private_appointments VALUES ('APT-1','" + status + "')"
		if status == "quiet" {
			statement = "SELECT 1"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := exec.CommandContext(ctx, filepath.Join(p.bin, "psql"), "-h", p.root, "-p", p.port, "-U", "lab_owner", "-d", "application", "-v", "ON_ERROR_STOP=1", "-c", statement).CombinedOutput(); err != nil {
			databaseErrors <- err
		}
	})
	for index, status := range []string{"wrong", "booked", "quiet", "denied"} {
		mode.Store(status)
		p.sql("DELETE FROM private_appointments")
		if status == "denied" {
			p.sql("REVOKE SELECT ON observed FROM observer")
		}
		review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
		if review.Run.Lifecycle == nil || len(review.Run.Lifecycle.Collectors) != 1 || review.Run.Lifecycle.Collectors[0].Credential != "database-observer" || review.Run.Lifecycle.Collectors[0].Generation != "1" || review.Run.Lifecycle.Collectors[0].Address != p.address {
			t.Fatalf("actual collector authority omitted: %+v", review.Run.Lifecycle)
		}
		actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: fmt.Sprintf("database-run-%d", index)})
		select {
		case err := <-databaseErrors:
			t.Fatal(err)
		default:
		}
		if actual.Lifecycle == nil || actual.Run == nil {
			t.Fatalf("real database result: %+v", actual)
		}
		rows := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *actual.Run, Phase: "reschedule", Dataset: "received", Reveal: true})
		if status == "denied" {
			if rows.Available || actual.Lifecycle.Verdict == assertion.VerdictPass {
				t.Fatalf("denied collection became empty/pass: %+v", rows)
			}
			p.sql("GRANT SELECT ON observed TO observer")
			continue
		}
		wanted := assertion.VerdictFail
		if status == "booked" {
			wanted = assertion.VerdictPass
		}
		if actual.Lifecycle.Verdict != wanted || actual.Lifecycle.Setup != "ready" || actual.Lifecycle.Cleanup != "complete" {
			proof, proofErr := connectedrun.OpenFlowEvidence(context.Background(), filepath.Join(f.root, actual.Lifecycle.Output))
			t.Fatalf("actual %s database verdict/setup: %+v; proof=%+v proofError=%v", status, actual.Lifecycle, proof.Phases, proofErr)
		}
		if status == "quiet" {
			if !rows.Available || rows.Total != 0 || len(rows.Rows) != 0 {
				t.Fatalf("real healthy zero: %+v", rows)
			}
		} else if !rows.Available || rows.Total != 1 || len(rows.Rows) != 1 || rows.Rows[0].Values[1].Text != status {
			t.Fatalf("actual retained driver rows: %+v", rows)
		}
		public, err := json.Marshal(actual)
		if err != nil || strings.Contains(string(public), "synthetic-database-reference-secret") {
			t.Fatal("resolved credential entered facade evidence")
		}
	}
}

func TestSavedConnectedDatabaseReviewRefusesChangedCredentialGenerationBeforeEffects(t *testing.T) {
	f := newConnectedAuthoring(t)
	p := startConnectedPostgres(t, f)
	observation := saveDatabaseObservation(t, f, p)
	draft := captureTestDraft(f, observation)
	draft.Boundary = desktop.ApplicationBoundary
	draft.Steps[0].V2 = &desktop.ConnectedV2{}
	draft.Phases[0].Checks = draft.Phases[0].Checks[:2]
	test := f.save(t, "Database reference fence", "database-reference-test", draft, f.v2.ID)
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	before := f.lab.Creates.Load()
	rotated := f.app.RecordCredentialRotation(desktop.CredentialRequest{Context: f.context, Name: "database-observer"})
	if rotated.State != desktop.Completed {
		t.Fatalf("registered rotation: %+v", rotated)
	}
	refused := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "changed-database-reference"})
	if refused.Outcome != desktop.ActionStale || f.lab.Creates.Load() != before {
		t.Fatalf("changed reference reached effects: %+v", refused)
	}
}

func TestSavedConnectedDatabaseFreshAuthorityRejectsChangedInputsDuringExecution(t *testing.T) {
	for _, mutation := range []string{"registry", "target", "projection", "input"} {
		t.Run(mutation, func(t *testing.T) {
			f := newConnectedAuthoring(t)
			p := startConnectedPostgres(t, f)
			observation := saveDatabaseObservation(t, f, p)
			draft := captureTestDraft(f, observation)
			draft.Boundary = desktop.ApplicationBoundary
			draft.Steps[0].V2 = &desktop.ConnectedV2{}
			draft.Phases[0].Checks = draft.Phases[0].Checks[:2]
			test := f.save(t, "Fresh database authority", "database-fresh-inputs", draft, f.v2.ID)
			store, err := catalog.Open(f.root)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := store.Read()
			if err != nil {
				t.Fatal(err)
			}
			member := func(ref desktop.ItemRef, role string) string {
				item := document.Items[document.Find(ref.ID)]
				for _, m := range item.Current().Members {
					if m.Role == role {
						return filepath.Join(f.root, m.Path)
					}
				}
				t.Fatalf("missing %s", role)
				return ""
			}
			path := f.registryAt
			switch mutation {
			case "target":
				path = member(f.v2, "target")
			case "projection":
				path = member(observation, "projection")
			case "input":
				path = filepath.Join(f.root, f.messages.Summary.Case.Entry, "payloads", f.bookAt+".bin")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reached := make(chan error, 1)
			f.engine.SetAfter(func() {
				changed := append([]byte(nil), raw...)
				if mutation == "registry" {
					changed = []byte(strings.Replace(string(raw), "minimize", "changed-scope", 1))
				} else {
					changed = append(changed, ' ')
				}
				reached <- os.WriteFile(path, changed, 0o600)
			})
			review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
			actual := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "database-changed-" + mutation})
			select {
			case err := <-reached:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("independent stimulus did not reach mutation witness")
			}
			if actual.Lifecycle == nil || actual.Run == nil || actual.Lifecycle.Verdict == assertion.VerdictPass {
				t.Fatalf("changed exact input credited: %+v", actual)
			}
			rows := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *actual.Run, Phase: "reschedule", Dataset: "received"})
			if rows.Available {
				t.Fatalf("changed authority credited actual zero: %+v", rows)
			}
		})
	}
}

func TestSavedConnectedDatabaseCancellationRetainsInsufficientScope(t *testing.T) {
	f := newConnectedAuthoring(t)
	p := startConnectedPostgres(t, f)
	observation := saveDatabaseObservation(t, f, p)
	draft := captureTestDraft(f, observation)
	draft.Boundary = desktop.ApplicationBoundary
	draft.Steps[0].V2 = &desktop.ConnectedV2{}
	draft.Phases[0].Checks = draft.Phases[0].Checks[:2]
	test := f.save(t, "Cancel actual database run", "database-cancel", draft, f.v2.ID)
	held := make(chan struct{})
	defer close(held)
	reached := make(chan struct{}, 1)
	f.engine.SetAfter(func() { reached <- struct{}{}; <-held })
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	click := desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "database-cancel"}
	done := make(chan desktop.ReviewedActionResult, 1)
	go func() { done <- f.app.ExecuteReviewedAction(click) }()
	select {
	case <-reached:
	case <-time.After(30 * time.Second):
		t.Fatal("independent stimulus absent")
	}
	f.app.CancelOperation(click.IntentID)
	var actual desktop.ReviewedActionResult
	select {
	case actual = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("database cancel did not settle")
	}
	if actual.Run == nil || actual.Lifecycle == nil || actual.Lifecycle.Verdict == assertion.VerdictPass {
		t.Fatalf("cancel credited outcome: %+v", actual)
	}
	rows := f.app.ReadConnectedObservation(desktop.ConnectedObservationRequest{Context: f.context, Run: *actual.Run, Phase: "reschedule", Dataset: "received"})
	if rows.Available || !strings.Contains(rows.Reason, "no absence") {
		t.Fatalf("cancel lost insufficient retained scope: %+v", rows)
	}
	comparison, err := runcompare.CompareFlows(context.Background(), filepath.Join(f.root, actual.Lifecycle.Output), filepath.Join(f.root, actual.Lifecycle.Output))
	if err != nil || len(comparison.Records) != 1 || comparison.Records[0].State != "not_compared" || !strings.Contains(comparison.Records[0].Reason, "coverage") {
		t.Fatalf("cancelled table became empty comparison: %+v %v", comparison.Records, err)
	}

}
