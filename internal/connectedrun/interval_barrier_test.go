package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestBarrierReadCommitsBusinessUpdateBeforeFinalBusinessAcquisition(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	target.notifications = make(chan int, 2)
	intervalPrepared(t, dir, target)
	var commit atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Age", "0")
		if commit.Load() {
			target.mu.Lock()
			target.rows = []string{"SAME,moved,2026-01-02T12:00\n"}
			target.persist()
			target.mu.Unlock()
			w.Write([]byte(`{"barriers":[{"run":"run-one","work":"change-one","destination":"application","state":"complete"}],"next":""}`))
		} else {
			w.Write([]byte(`{"barriers":[],"next":""}`))
		}
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	caPath := filepath.Join(dir, "barrier-ca.pem")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.HTTPAPI, Identity: "processor", Scope: "work"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "1s"}, HTTP: &observesource.HTTP{URL: server.URL, Classification: "nonproduction", CAFile: caPath, ServerName: "example.com", Timeout: "1s", MaxBytes: 65536, Retry: observesource.Retry{Attempts: 0, Delay: "5ms"}}, Extraction: &observesource.Extraction{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"barriers"}}, RecordKey: importer.Locator{"run"}}}
	if err := observesource.WriteSource(filepath.Join(dir, "barrier-source.json"), source); err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "barrier", Format: "json", Order: "source", Continuation: importer.Locator{"next"}, Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: source.Extraction.JSON}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	for _, name := range []string{"run", "work", "destination", "state"} {
		projection.Columns = append(projection.Columns, dataset.Column{Name: name, Type: "text", Locator: importer.Locator{name}, Required: true, Key: name == "run"})
	}
	old, err := connectedtest.OpenPlan(filepath.Join(dir, "interval-plan"))
	if err != nil {
		t.Fatal(err)
	}
	d := old.Document().Test
	members := old.Files()
	files := map[string][]byte{}
	retain := func(ref connectedtest.Reference) { files[ref.File] = members["dependencies/"+ref.SHA256] }
	retain(d.Checks)
	for _, step := range d.Steps {
		retain(step.V2.Input)
	}
	for i := range d.Datasets {
		ds := &d.Datasets[i]
		retain(*ds.Projection)
		retain(*ds.Completion.Policy)
		if ds.ID == "after" {
			def, e := observeinterval.Decode(files[ds.Completion.Policy.File])
			if e != nil {
				t.Fatal(e)
			}
			def.Barrier = &observeinterval.Barrier{Source: source.Identity(), Destination: "application", Work: "change-one", Projection: projection}
			files[ds.Completion.Policy.File], _ = json.Marshal(def)
			ds.Completion.Policy.SHA256 = dataset.Digest(files[ds.Completion.Policy.File])
			ds.Completion.Kind = "processing-barrier"
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "policy.json"))
	var policy sendpolicy.ScopedPolicy
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: "after-barrier", Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
	write(t, filepath.Join(dir, "policy.json"), policy)
	raw, _ = os.ReadFile(filepath.Join(dir, "policy.json"))
	d.Environment.AddressPolicyIdentity = dataset.Digest(raw)
	raw, _ = json.Marshal(d)
	plan, err := connectedtest.Compile(raw, files, old.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "barrier-plan")
	if err = plan.Write(context.Background(), planPath); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "interval-execution.json"))
	var config connectedrun.ConfigV2
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config.Barriers = map[string]connectedrun.SourceSelection{"after": {Path: "barrier-source.json", Grant: &connectedrun.Grant{Path: "barrier-grant.json", Actor: "reader", Generation: "1"}, CredentialGeneration: "1"}}
	configPath := filepath.Join(dir, "barrier-execution.json")
	write(t, configPath, config)
	p, err := connectedrun.Prepare(planPath, configPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, binding := range p.Bindings() {
		file, actor := "grant.json", "runner"
		if name == "barrier:after" {
			file, actor = "barrier-grant.json", "reader"
		}
		write(t, filepath.Join(dir, file), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: actor, Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type answer struct {
		r   connectedrun.Result
		err error
	}
	done := make(chan answer, 1)
	output := filepath.Join(dir, "run")
	go func() { r, e := connectedrun.ExecuteWithClock(ctx, p, "run-one", output, clock); done <- answer{r, e} }()
	waits := 0
	for {
		select {
		case advance := <-clock.waits:
			waits++
			if waits == 1 {
				for i := 0; i < 2; i++ {
					select {
					case <-target.notifications:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				waitStimulusFinish(t, ctx, output)
				target.mu.Lock()
				target.rows = nil
				target.persist()
				target.mu.Unlock()
				commit.Store(true)
			}
			advance <- 10 * time.Millisecond
		case got := <-done:
			if got.err != nil || got.r.State != "complete" || got.r.Verdict != assertion.VerdictPass || got.r.Boundaries()["after"] != "independently-observed-processing-barrier" {
				t.Fatal(got.r, got.err)
			}
			if _, err = connectedrun.Open(ctx, output); err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("barrier runtime did not complete", ctx.Err())
		}
	}
}
