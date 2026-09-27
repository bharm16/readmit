package connectedrun_test

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/jackc/pgx/v5/pgproto3"
)

// This independently implements PostgreSQL wire responses over TLS. It proves
// real driver/collector composition, not PostgreSQL deployment qualification.
func flowSourcesPostgres(t *testing.T, certificate tls.Certificate, booked *atomic.Bool) (string, *atomic.Int32, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	queries := &atomic.Int32{}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				backend := pgproto3.NewBackend(conn, conn)
				first, err := backend.ReceiveStartupMessage()
				if err != nil {
					return
				}
				if _, ok := first.(*pgproto3.SSLRequest); !ok {
					t.Error("database connection omitted TLS negotiation")
					return
				}
				if _, err = conn.Write([]byte("S")); err != nil {
					return
				}
				secured := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				backend = pgproto3.NewBackend(secured, secured)
				if _, err = backend.ReceiveStartupMessage(); err != nil {
					return
				}
				backend.Send(&pgproto3.AuthenticationOk{})
				backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
				backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0"})
				backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				if backend.Flush() != nil {
					return
				}
				for {
					message, err := backend.Receive()
					if err != nil {
						return
					}
					switch m := message.(type) {
					case *pgproto3.Parse:
						if !strings.HasPrefix(m.Query, `SELECT "appointment", "status" FROM "public"."observed" WHERE "status" = $1 LIMIT `) {
							t.Errorf("unexpected SELECT shape: %q", m.Query)
							return
						}
						backend.Send(&pgproto3.ParseComplete{})
					case *pgproto3.Bind:
						if len(m.Parameters) != 1 || string(m.Parameters[0]) != "ready" {
							t.Error("database filter was not separately bound")
							return
						}
						backend.Send(&pgproto3.BindComplete{})
					case *pgproto3.Describe:
						backend.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("appointment"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}, {Name: []byte("status"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}}})
					case *pgproto3.Execute:
						queries.Add(1)
						if booked.Load() {
							backend.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("DB-42"), []byte("ready")}})
						}
						backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
					case *pgproto3.Sync:
						backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
					case *pgproto3.Terminate:
						return
					}
					if backend.Flush() != nil {
						return
					}
				}
			}()
		}
	}()
	var once sync.Once
	close := func() { once.Do(func() { listener.Close(); workers.Wait() }) }
	t.Cleanup(close)
	return listener.Addr().String(), queries, close
}

func TestFlowSourcesContractEvaluatesFileHTTPDatabaseAndCaptureTogether(t *testing.T) {
	flowSourcesContract(t, false, false, false, false)
}
func TestFlowSourcesContractCancellationRetainsReadableUnresolvedResult(t *testing.T) {
	flowSourcesContract(t, true, false, false, false)
}
func TestFlowSourcesContractLostCoverageRetainsReadableUnresolvedResult(t *testing.T) {
	flowSourcesContract(t, false, true, false, false)
}
func TestFlowSourcesContractWrongRunCaptureRemainsExcludedEvidence(t *testing.T) {
	flowSourcesContract(t, false, false, true, false)
}
func TestFlowSourcesContractHTTPDeadlineRetainsReadableUnresolvedResult(t *testing.T) {
	flowSourcesContract(t, false, false, false, true)
}
func flowSourcesContract(t *testing.T, cancelRead, shortGap, wrongNamespace, timeoutRead bool) {
	runContext, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()
	h := newFlowContractHarness(t)
	h.fixture.target.notifications = make(chan int, 1)
	d, files := flowWireDependencies(t, h)
	d.Phases = d.Phases[:1]
	d.Steps = d.Steps[:1]
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	booking := config.Phases["booking"]
	config.Phases = map[string]connectedrun.ConfigV2{"booking": booking}
	phase := &d.Phases[0]
	// File evidence here is an explicitly selected immutable pair of exports,
	// not a claim that stimulus updated the file. HTTP, database and capture
	// state below change only when the independent target receives the input.
	// Atomic replacement during a live file read is legitimately refused by
	// the collector and belongs in the separate file-churn negative tests.
	var originalFileSource observesource.Source
	flowContractRead(t, filepath.Join(h.root, "source.json"), &originalFileSource)
	immutableExports := []string{}
	// This test measures adapter composition, not the maximum-gap boundary.
	// Keep that explicit safety bound within the runner deadline even under
	// race instrumentation verifying four independent evidence streams.
	for i := range phase.Datasets {
		ds := &phase.Datasets[i]
		source := originalFileSource
		selected := *originalFileSource.File
		selected.Path = filepath.Join(h.root, "immutable-"+ds.ID+".csv")
		source.File = &selected
		source.Observes.Identity = "immutable-" + ds.ID
		body := "appointment,status,start\n"
		if ds.Phase == "after" {
			body += "SAME,booked,2026-01-01T12:00\n"
		}
		if err := os.WriteFile(selected.Path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		immutableExports = append(immutableExports, selected.Path)
		sourceName := "immutable-" + ds.ID + "-source.json"
		if err := observesource.WriteSource(filepath.Join(h.root, sourceName), source); err != nil {
			t.Fatal(err)
		}
		booking.Definition.Sources[ds.ID] = connectedrun.SourceSelection{Path: sourceName}
		ds.Source = source.Identity()
		var interval observeinterval.Definition
		if err := json.Unmarshal(files[ds.Completion.Policy.File], &interval); err != nil {
			t.Fatal(err)
		}
		interval.Source = ds.Source
		interval.MaxGapMS = 10000
		ref := flowWireReference(t, files, "mixed-"+ds.ID+"-window", observeinterval.Schema, interval)
		ds.Completion.Policy = &ref
		var projection dataset.Projection
		if err := json.Unmarshal(files[ds.Projection.File], &projection); err != nil {
			t.Fatal(err)
		}
		projection.Limits.TimeoutMS = 5000
		projectionRef := flowWireReference(t, files, "mixed-"+ds.ID+"-projection", dataset.ProjectionSchema, projection)
		ds.Projection = &projectionRef
	}
	var checks assertion.DatasetSetDocument
	if err := json.Unmarshal(files[phase.Checks.File], &checks); err != nil {
		t.Fatal(err)
	}
	for i := range checks.Bindings {
		for _, ds := range phase.Datasets {
			if checks.Bindings[i].Name == ds.ID {
				checks.Bindings[i].ProjectionIdentity = ds.Projection.SHA256
				checks.Bindings[i].Source = ds.Source
			}
		}
	}

	var policy sendpolicy.ScopedPolicy
	flowContractRead(t, filepath.Join(h.root, "policy.json"), &policy)
	var booked atomic.Bool
	var httpReads atomic.Int32
	var cancellationReached atomic.Bool
	httpDeadlineReached := make(chan struct{})
	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readNumber := httpReads.Add(1)
		if (cancelRead || timeoutRead) && booked.Load() || shortGap && readNumber > 1 && !cancellationReached.Load() {
			// The independent target has already written the actual ACK and
			// the mapper received the collector's ACK. The selected fault occurs
			// while a required post-stimulus source read is in flight.
			select {
			case <-h.fixture.target.notifications:
			case <-r.Context().Done():
				return
			}
			cancellationReached.Store(true)
			if timeoutRead {
				// No response and no root cancellation: only the declared
				// HTTP request deadline can finish this acquisition.
				<-r.Context().Done()
				close(httpDeadlineReached)
				return
			}
			if cancelRead {
				cancelRun()
				<-r.Context().Done()
				return
			}
			// The response is deliberately held beyond the declared 20ms gap.
			// This is a lower-bound timer after an explicit protocol handshake,
			// not a scheduling race or a sleep used to hope an event happened.
			timer := time.NewTimer(40 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Age", "0")
		w.Header().Set("Content-Type", "text/csv")
		body := "appointment,status\n"
		if booked.Load() {
			body += "HTTP-42,ready\n"
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(httpServer.Close)
	caPath := filepath.Join(h.root, "mixed-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: httpServer.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	dbAddress, dbQueries, closeDB := flowSourcesPostgres(t, httpServer.TLS.Certificates[0], &booked)
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	captureAddress := reserve.Addr().String()
	reserve.Close()
	allow := func(id, address string, operation sendpolicy.Operation) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal(err)
		}
		number, err := strconv.Atoi(port)
		if err != nil {
			t.Fatal(err)
		}
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: id, Operation: operation, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
	}
	allow("api", httpServer.Listener.Addr().String(), sendpolicy.ObservationRead)
	allow("database", dbAddress, sendpolicy.ObservationRead)
	allow("captured", captureAddress, sendpolicy.CaptureListen)
	write(t, filepath.Join(h.root, "policy.json"), policy)
	policyRaw, err := os.ReadFile(filepath.Join(h.root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	d.Environment.AddressPolicyIdentity = dataset.Digest(policyRaw)
	csv := &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 2}
	httpSource := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.HTTPAPI, Identity: "independent-http", Scope: "appointments"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "10s"}, Extraction: &observesource.Extraction{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8, CSV: csv, RecordKey: importer.Locator{"appointment"}}, HTTP: &observesource.HTTP{URL: httpServer.URL, Classification: "nonproduction", CAFile: caPath, ServerName: "example.com", Timeout: "5s", MaxBytes: 65536, Retry: observesource.Retry{Attempts: 0, Delay: "5ms"}}}
	if timeoutRead {
		httpSource.HTTP.Timeout = "2s"
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dbSource := observesource.Source{Schema: observesource.SchemaDatabase, Observes: observewindow.Source{Kind: observesource.DatabaseQuery, Identity: "independent-db", Scope: "appointments"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "10s"}, Database: &observesource.Database{Driver: "postgresql", Address: dbAddress, Classification: "nonproduction", Name: "synthetic", Username: "observer", CAFile: caPath, ServerName: "example.com", Credential: observesource.DatabaseCredential{Store: "customer-managed", Address: dbAddress, Purpose: "database-observation", Command: executable, Arguments: []string{"-test.run=^TestFlowContractCredentialProvider$", "--", "--flow-contract-credential", "database"}}, View: []string{"public", "observed"}, RecordKey: "appointment", KeyType: "text", Filters: []observesource.DatabaseFilter{{Column: "status", Value: "ready"}}, Limits: &observesource.DatabaseLimits{Timeout: "5s", MaxRows: 10, MaxBytes: 65536}}}
	for name, source := range map[string]observesource.Source{"api": httpSource, "database": dbSource} {
		if err = observesource.WriteSource(filepath.Join(h.root, name+"-source.json"), source); err != nil {
			t.Fatal(err)
		}
	}
	capture := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchema, Address: captureAddress, ReceiverPolicy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "mixed-source-output", SourceLabel: "independent-output", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}, TimeoutMS: 10000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: 10, MaxConnections: 2, MaxSessions: 10, RunSelector: "ZRN-1"}
	write(t, filepath.Join(h.root, "captured-source.json"), capture)
	captureRaw, err := os.ReadFile(filepath.Join(h.root, "captured-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "mixed-table", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: csv}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"appointment"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 5000}}
	dbProjection := projection
	dbProjection.ID = "mixed-database"
	dbProjection.Format = "database"
	dbProjection.Order = "unordered"
	dbProjection.Envelope = nil
	if timeoutRead {
		projection.Limits.TimeoutMS = 2000
	}
	captureProjection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "mixed-capture", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "SCH-1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 5000}}
	for _, item := range []struct {
		id, source, key string
		projection      dataset.Projection
		stream          bool
	}{{"api", httpSource.Identity(), "HTTP-42", projection, false}, {"database", dbSource.Identity(), "DB-42", dbProjection, false}, {"captured", dataset.Digest(captureRaw), "CAPTURE-42", captureProjection, true}} {
		ref := flowWireReference(t, files, item.id+"-projection", dataset.ProjectionSchema, item.projection)
		interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: item.source, Namespace: "appointments", Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 120, SampleMS: 20, MaxGapMS: 10000, MaxSamples: 100, MaxRecords: 10, MaxBytes: 65536}
		if shortGap && item.id == "api" {
			interval.MaxGapMS = 20
		}
		if item.stream {
			interval.Mode = "stream"
			interval.Freshness = "ingress"
		}
		window := flowWireReference(t, files, item.id+"-window", observeinterval.Schema, interval)
		phase.Datasets = append(phase.Datasets, connectedtest.Dataset{ID: item.id, Kind: "typed-rows", Phase: "after", Source: item.source, Namespace: "appointments", Projection: &ref, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: 120, MaxRecords: 10, MaxBytes: 65536, Policy: &window}})
		checks.Bindings = append(checks.Bindings, assertion.DatasetBinding{Name: item.id, Source: item.source, Namespace: "appointments", Phase: "after", ProjectionIdentity: item.projection.Identity()})
		checks.Assertions = append(checks.Assertions, assertion.DatasetAssertion{ID: item.id + "-one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: item.id}, Count: flowWirePtr(1)}, assertion.DatasetAssertion{ID: item.id + "-value", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: item.id}, Column: "key", Expected: &dataset.Value{State: "present", Type: "text", Text: item.key}})
		selection := connectedrun.SourceSelection{Path: item.id + "-source.json", Grant: &connectedrun.Grant{Path: item.id + "-grant.json", Actor: "reader", Generation: "1"}, CredentialGeneration: "1"}
		if item.stream {
			selection.CredentialGeneration = ""
		}
		booking.Definition.Sources[item.id] = selection
	}
	config.Phases["booking"] = booking
	phase.Checks = flowWireReference(t, files, "mixed-checks", assertion.DatasetSchema, checks)
	flowWireInstall(t, h, d, files, config)
	originalOutput := h.fixture.target.getOutput()
	outputErrors := make(chan error, 1)
	h.fixture.target.setOutput(func(n int, raw string) {
		originalOutput(n, raw)
		booked.Store(true)
		captureRun := "mixed-run"
		if wrongNamespace {
			captureRun = "another-run"
		}
		outputErrors <- flowWireSendMapped(captureAddress, captureRun, "CAPTURE-42", 1)
	})
	p := flowWirePrepare(t, h, "mixed-run")
	if httpReads.Load() != 0 || dbQueries.Load() != 0 || booked.Load() {
		t.Fatal("preparation performed source or stimulus effects")
	}
	output := filepath.Join(h.root, "mixed-run")
	result, err := connectedrun.ExecuteFlow(runContext, p, output, testisolation.Confirmation{})
	if cancelRead || shortGap || timeoutRead {
		if timeoutRead {
			if !cancellationReached.Load() {
				t.Fatalf("post-stimulus deadline fixture was not reached: sends=%d result=%+v error=%v", h.fixture.target.received.Load(), result, err)
			}
			select {
			case <-httpDeadlineReached:
			case <-t.Context().Done():
				t.Fatal("HTTP request deadline did not reach target")
			}
			if runContext.Err() != nil {
				t.Fatal("root cancellation masqueraded as a source deadline", runContext.Err())
			}
		}
		var retained connectedrun.FlowResult
		flowContractRead(t, filepath.Join(output, "manifest.json"), &retained)
		var interval observeinterval.Result
		flowContractRead(t, filepath.Join(output, "phases", "booking", "intervals", "api", "manifest.json"), &interval)
		if !cancellationReached.Load() || h.fixture.target.received.Load() != 1 || retained.Verdict != assertion.VerdictUndecided || retained.State == "complete" || interval.Sufficient() || shortGap && interval.Reason != "lost-coverage" {
			t.Fatalf("source interruption did not retain the expected unresolved boundary: run=%+v interval=%+v", retained, interval)
		}
		if timeoutRead {
			var sourceSpec networkaction.HTTPSpec
			flowContractRead(t, filepath.Join(output, "phases", "booking", "observations", "api-0001", "network", "action.json"), &sourceSpec)
			if sourceSpec.TimeoutMS != 2000 {
				t.Fatal("wrong timeout policy retained", sourceSpec.TimeoutMS)
			}
		}
		if err != nil {
			child, childErr := connectedrun.OpenEvidence(t.Context(), filepath.Join(output, "phases", "booking"))
			t.Logf("child OpenEvidence error=%v evidence=%+v; retained phases=%+v", childErr, child, retained.Phases)

			t.Fatalf("honest retained source interruption rejected: state=%s cleanup=%s interval=%s error=%v", retained.State, retained.Cleanup, interval.Reason, err)
		}
		replayed, replayErr := replay.Open(filepath.Join(output, "phases", "booking", "transport", "run"))
		if replayErr != nil || len(replayed.Events) != 1 || len(retained.Phases) != 1 || len(retained.Phases[0].Steps) != 1 {
			t.Fatal("actual attempted stimulus was not retained", replayErr)
		}
		attempt := retained.Phases[0].Steps[0]
		if attempt.Outcome == "not-attempted" {
			t.Fatal("observer failure relabelled actual sent stimulus as not attempted")
		}
		if replayed.Events[0].Delivery == "acknowledged" && (attempt.Outcome != "complete" || attempt.Uncertain) {
			t.Fatal("retained ACK was not reflected in attempt", attempt)
		}
		if replayed.Events[0].Delivery == "uncertain" && (attempt.Outcome != "unknown" || !attempt.Uncertain) {
			t.Fatal("retained uncertainty was not reflected in attempt", attempt)
		}
		reopened, readErr := connectedrun.OpenFlow(t.Context(), output)
		if readErr != nil || !reflect.DeepEqual(reopened, result) {
			t.Fatal("cancelled flow did not reopen without effects", readErr)
		}
		return
	}
	if err != nil {
		_ = filepath.WalkDir(output, func(path string, e os.DirEntry, walkErr error) error {
			relative, _ := filepath.Rel(output, path)
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if walkErr == nil && !e.IsDir() && e.Name() == "manifest.json" && (len(parts) == 1 || len(parts) == 3 || len(parts) == 5 && parts[2] == "intervals") {
				raw, _ := os.ReadFile(path)
				var summary struct {
					Schema  string `json:"schema"`
					State   string `json:"state"`
					Reason  string `json:"reason"`
					Phase   string `json:"phase"`
					Setup   string `json:"setup"`
					Cleanup string `json:"cleanup"`
					Verdict string `json:"verdict"`
					Summary struct {
						State   string `json:"state"`
						Phase   string `json:"phase"`
						Verdict string `json:"verdict"`
					} `json:"summary"`
				}
				if json.Unmarshal(raw, &summary) == nil {
					t.Log(relative, summary)
				}
			}
			return nil
		})
		t.Fatal(err)
	}
	select {
	case err = <-outputErrors:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("independent mapped output not produced")
	}
	if wrongNamespace {
		interval, readErr := observeinterval.Open(t.Context(), filepath.Join(output, "phases", "booking", "intervals", "captured"))
		if readErr != nil || interval.Sufficient() || interval.Reason != "wrong-scope" || result.State == "complete" || result.Verdict != assertion.VerdictUndecided || h.fixture.target.received.Load() != 1 || len(result.Phases) != 1 || len(result.Phases[0].Checks) != 11 {
			t.Fatal("wrong run scope became usable evidence", result, interval, readErr)
		}
		last := interval.Records[len(interval.Records)-1]
		if last.Excluded["s0001-e000001"] != "wrong-run" {
			t.Fatal("excluded occurrence reason was lost", last.Excluded)
		}
		_, captured, readErr := networkaction.OpenCapture(filepath.Join(output, "phases", "booking", "intervals", "captured", "capture"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		raw, readErr := captured.Raw("s0001-e000001")
		if readErr != nil || !strings.Contains(string(raw), "ZRN|another-run") {
			t.Fatal("excluded original occurrence was removed", readErr)
		}
		for _, check := range result.Phases[0].Checks {
			if (check.ID == "typed:captured-one" || check.ID == "typed:captured-value") && check.Outcome == assertion.OutcomePassed {
				t.Fatal("wrong-run capture satisfied typed check", check)
			}
		}
		reopened, readErr := connectedrun.OpenFlow(t.Context(), output)
		if readErr != nil || !reflect.DeepEqual(reopened, result) || h.fixture.target.received.Load() != 1 {
			t.Fatal("wrong-run result did not reopen without resend", readErr)
		}
		return
	}
	if result.State != "complete" || result.Cleanup != "complete" || result.Verdict != assertion.VerdictPass || len(result.Phases) != 1 || len(result.Phases[0].Checks) != 11 {
		t.Fatalf("sources did not form one evaluated phase: %+v", result)
	}
	for _, check := range result.Phases[0].Checks {
		if check.Outcome != assertion.OutcomePassed {
			t.Fatal("typed source check not evaluated", check)
		}
	}
	child, err := connectedrun.Open(t.Context(), filepath.Join(output, "phases", "booking"))
	if err != nil {
		t.Fatal(err)
	}
	for id, kind := range map[string]string{"after": "file", "api": "http", "database": "database", "captured": "capture"} {
		snapshot, err := dataset.Open(t.Context(), filepath.Join(output, "phases", "booking", child.Observations[id]))
		if err != nil {
			t.Fatal(id, err)
		}
		doc := snapshot.Document()
		if doc.Acquisition.Kind != kind || !snapshot.Usable() || len(doc.Rows) != 1 {
			t.Fatal("source-specific typed provenance lost", id, doc)
		}
		if kind == "database" && doc.Material.Meaning != "typed-driver-result" || kind == "capture" && doc.Material.Meaning != "verified-capture-occurrence-bytes" {
			t.Fatal("invented source provenance", id, doc.Material)
		}
	}
	if httpReads.Load() < 2 || dbQueries.Load() < 2 || h.fixture.target.received.Load() != 1 {
		t.Fatal("source arming/collection or single stimulus missing")
	}
	httpServer.Close()
	closeDB()
	h.server.Close()
	h.fixture.target.listener.Close()
	for _, path := range append(immutableExports, h.fixture.target.file) {
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	httpCount, dbCount := httpReads.Load(), dbQueries.Load()
	reopened, err := connectedrun.OpenFlow(t.Context(), output)
	if err != nil || !reflect.DeepEqual(reopened, result) || httpReads.Load() != httpCount || dbQueries.Load() != dbCount {
		t.Fatal("offline reopen contacted a source or changed the verdict", err)
	}
}
