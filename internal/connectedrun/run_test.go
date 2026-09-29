package connectedrun_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

func write(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// The target is a separately implemented ordinary CSV-exporting application.
// It does not use readmit's fixture ledger, assertion engine or ACK generator.
type target struct {
	connectTimeout string
	output         func(int, string)
	notifications  chan int
	listener       net.Listener
	file           string
	mu             sync.Mutex
	mode           string
	rows           []string
	received       atomic.Int32
	done           sync.WaitGroup
}

func startTarget(t *testing.T, dir string) *target {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &target{listener: l, file: filepath.Join(dir, "export.csv")}
	s.reset("fixed")
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s.done.Add(1)
			go func() {
				defer s.done.Done()
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(c)
				for {
					frame, err := reader.ReadBytes(28)
					if err != nil {
						return
					}
					if _, err = reader.ReadByte(); err != nil {
						return
					}
					raw := strings.TrimPrefix(string(frame[:len(frame)-1]), string(byte(11)))
					segments := strings.Split(raw, "\r")
					header := strings.Split(segments[0], "|")
					if len(header) < 11 {
						return
					}
					control := header[9]
					status, start := "booked", "2026-01-01T12:00"
					if strings.Contains(raw, "SIU^S13") {
						status, start = "moved", "2026-01-02T12:00"
					}
					ordinal := s.received.Add(1)
					s.mu.Lock()
					row := "SAME," + status + "," + start + "\n"
					if s.mode == "defective" {
						s.rows = append(s.rows, row)
					} else {
						s.rows = []string{row}
					}
					s.persist()
					disconnect := strings.HasPrefix(s.mode, "disconnect")
					if s.mode == "disconnect-missing" {
						os.Remove(s.file)
					}
					late := s.mode == "late" && status == "moved"
					s.mu.Unlock()
					if disconnect {
						return
					}
					if output := s.getOutput(); output != nil {
						output(int(ordinal), raw)
					}
					ack := "MSH|^~\\&|TARGET|LAB|SENDER|LAB|20260101000000||ACK|ACK|P|2.5.1\rMSA|AA|" + control + "\r"
					c.Write(append(append([]byte{11}, []byte(ack)...), 28, 13))
					if s.notifications != nil {
						s.notifications <- int(ordinal)
					}
					if late {
						s.done.Add(1)
						go func() {
							defer s.done.Done()
							time.Sleep(35 * time.Millisecond)
							s.mu.Lock()
							s.rows = append(s.rows, row)
							s.persist()
							s.mu.Unlock()
						}()
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); s.done.Wait() })
	return s
}
func (s *target) persist() {
	_ = os.WriteFile(s.file+".new", []byte("appointment,status,start\n"+strings.Join(s.rows, "")), 0600)
	_ = os.Rename(s.file+".new", s.file)
}
func (s *target) reset(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
	s.rows = nil
	s.persist()
}
func prepared(t *testing.T, s *target, dir string, httpSource ...bool) *connectedrun.Prepared {
	t.Helper()
	now := time.Now()
	messages := []string{"MSH|^~\\&|SENDER|LAB|TARGET|LAB|20260101000000||SIU^S12|BOOK|P|2.5.1\rSCH|SAME\r", "MSH|^~\\&|SENDER|LAB|TARGET|LAB|20260101000001||SIU^S13|MOVE|P|2.5.1\rSCH|SAME\r"}
	wire := []byte{}
	for _, m := range messages {
		wire = append(wire, 11)
		wire = append(wire, []byte(m)...)
		wire = append(wire, 28, 13)
	}
	casePath := filepath.Join(dir, "case")
	sourceCase, err := bundle.Write(casePath, []bundle.Input{{Path: "independent.mllp", Data: wire}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	targetConfig := replay.Target{Schema: replay.TargetSchemaV3, Name: "Lab", Classification: replay.Nonproduction, TestEndpoint: true, Address: s.listener.Addr().String(), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "1s", MessageTimeout: "1s", MaxACKBytes: 4096}
	if s.connectTimeout != "" {
		targetConfig.ConnectTimeout = s.connectTimeout
	}
	write(t, filepath.Join(dir, "target.json"), targetConfig)
	rp, err := replay.Prepare(casePath, targetConfig, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(targetConfig.Address)
	number, _ := strconv.Atoi(port)
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}}
	write(t, filepath.Join(dir, "policy.json"), policy)
	policyRaw, _ := os.ReadFile(filepath.Join(dir, "policy.json"))
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.FileExport, Identity: "independent-app", Scope: "appointments"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "10s"}, File: &observesource.File{Path: s.file, MaxBytes: 65536}, Extraction: &observesource.Extraction{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 3}, RecordKey: importer.Locator{"appointment"}}}
	if len(httpSource) > 0 && httpSource[0] {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := os.ReadFile(s.file)
			if err != nil {
				w.WriteHeader(503)
				return
			}
			w.Header().Set("Age", "0")
			w.Write(raw)
		}))
		t.Cleanup(server.Close)
		caPath := filepath.Join(dir, "ca.pem")
		os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
		source.Observes.Kind = observesource.HTTPAPI
		source.File = nil
		source.HTTP = &observesource.HTTP{URL: server.URL, Classification: "nonproduction", CAFile: caPath, ServerName: "127.0.0.1", Timeout: "1s", MaxBytes: 65536, Retry: observesource.Retry{Attempts: 0, Delay: "5ms"}}
		_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
		number, _ := strconv.Atoi(port)
		for _, id := range []string{"before", "after"} {
			policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: id, Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
		}
		write(t, filepath.Join(dir, "policy.json"), policy)
		policyRaw, _ = os.ReadFile(filepath.Join(dir, "policy.json"))
	}
	if err = observesource.WriteSource(filepath.Join(dir, "source.json"), source); err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: source.Extraction.CSV}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"appointment"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}, {Name: "start", Type: "datetime", Locator: importer.Locator{"start"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 300}}
	if source.HTTP != nil {
		projection.Limits.TimeoutMS = 1000
	}
	zero, one := 0, 1
	checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "before", Namespace: "appointments", Phase: "before", Source: source.Identity(), ProjectionIdentity: projection.Identity()}, {Name: "after", Namespace: "appointments", Phase: "after", Source: source.Identity(), ProjectionIdentity: projection.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "initial", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "before"}, Count: &zero}, {ID: "one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &one}, {ID: "unique", Operator: "unique-keys", Subject: assertion.RowSelection{Dataset: "after"}}, {ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: &dataset.Value{State: "present", Type: "text", Text: "moved"}}, {ID: "time", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "start", Expected: &dataset.Value{State: "present", Type: "datetime", Text: "2026-01-02T12:00", Precision: "minute", Timezone: "absent"}}}}
	files := map[string][]byte{}
	files["checks.json"], _ = json.Marshal(checks)
	files["projection.json"], _ = json.Marshal(projection)
	ref := func(id, schema, path string) connectedtest.Reference {
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: path, SHA256: dataset.Digest(files[path])}
	}
	test := connectedtest.Test{Schema: connectedtest.TestSchemaV2, Project: "lab", ID: "reschedule", Revision: "1", Environment: connectedtest.Environment{Project: "lab", ID: "test", Revision: "1", Name: "Lab", Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: dataset.Digest(policyRaw), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Provenance: "operator-declared", Value: "independent-target"}}, Setup: connectedtest.Setup{Kind: "operator-declared", Isolation: "dedicated-target", Instructions: "Reset the independent target before each run"}, Checks: ref("checks", assertion.DatasetSchema, "checks.json"), OperatorVersion: connectedtest.OperatorVersionV2, Limits: connectedtest.Limits{MaxSteps: 10, MaxBytes: 1 << 20, DeadlineMS: 10000}}
	for i, e := range sourceCase.Events {
		path := fmt.Sprintf("input%d.hl7", i)
		files[path], _ = sourceCase.Raw(e.ID)
		test.Steps = append(test.Steps, connectedtest.Step{ID: fmt.Sprintf("step%d", i), Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: ref(fmt.Sprintf("input%d", i), "hl7", path), Occurrence: e.ID}})
	}
	for _, phase := range []string{"before", "after"} {
		r := ref("projection", dataset.ProjectionSchema, "projection.json")
		test.Datasets = append(test.Datasets, connectedtest.Dataset{ID: phase, Kind: "typed-rows", Namespace: "appointments", Phase: phase, Source: source.Identity(), Projection: &r, Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: projection.Limits.TimeoutMS, MaxRecords: 10, MaxBytes: 65536}})
	}
	raw, _ := json.Marshal(test)
	plan, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan")
	if err = plan.Write(context.Background(), planPath); err != nil {
		t.Fatal(err)
	}
	config := connectedrun.Config{Schema: connectedrun.ConfigSchema, Case: "case", Target: "target.json", Policy: "policy.json", Send: connectedrun.Grant{Path: "grant.json", Actor: "runner", Generation: "1"}, Sources: map[string]connectedrun.SourceSelection{"before": {Path: "source.json"}, "after": {Path: "source.json"}}}
	if source.HTTP != nil {
		for _, id := range []string{"before", "after"} {
			selection := config.Sources[id]
			selection.Grant = &connectedrun.Grant{Path: id + "-grant.json", Actor: "reader", Generation: "1"}
			selection.CredentialGeneration = "1"
			config.Sources[id] = selection
		}
	}
	configPath := filepath.Join(dir, "execution.json")
	write(t, configPath, config)
	p, err := connectedrun.Prepare(planPath, configPath)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "grant.json"), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: p.Bindings()["stimulus"], IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	for id, binding := range p.Bindings() {
		if id != "stimulus" {
			write(t, filepath.Join(dir, strings.TrimPrefix(id, "dataset:")+"-grant.json"), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "reader", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
		}
	}
	return p
}
func TestConnectedRuntimeDetectsDefectFixReintroductionAndLateDuplicate(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := prepared(t, target, dir)
	for i, mode := range []string{"defective", "fixed", "defective", "late"} {
		target.reset(mode)
		output := filepath.Join(dir, fmt.Sprintf("run%d", i))
		result, err := connectedrun.Execute(context.Background(), p, fmt.Sprintf("run%d", i), output)
		if err != nil {
			t.Fatal(err)
		}
		want := assertion.VerdictFail
		if mode == "fixed" {
			want = assertion.VerdictPass
		}
		if result.State != "complete" || result.Verdict != want {
			t.Fatalf("%s: %+v", mode, result)
		}
		reopened, err := connectedrun.Open(context.Background(), output)
		if err != nil || reopened.Verdict != want {
			t.Fatal("offline result", err)
		}
	}
	if target.received.Load() != 8 {
		t.Fatal("unexpected transport attempts")
	}
	os.Remove(target.file)
	os.Remove(filepath.Join(dir, "target.json"))
	os.Remove(filepath.Join(dir, "grant.json"))
	os.Rename(filepath.Join(dir, "plan"), filepath.Join(dir, "plan-removed"))
	if result, err := connectedrun.Open(context.Background(), filepath.Join(dir, "run1")); err != nil || result.Verdict != assertion.VerdictPass {
		t.Fatal("live source was required", err)
	}
	if target.received.Load() != 8 {
		t.Fatal("reopen resent")
	}
}
func TestConnectedRuntimeStartupFailureAndUncertainWriteCannotPassOrResend(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := prepared(t, target, dir)
	os.Remove(target.file)
	result, err := connectedrun.Execute(context.Background(), p, "missing", filepath.Join(dir, "missing"))
	if err != nil || result.Verdict == assertion.VerdictPass || target.received.Load() != 0 {
		t.Fatal("failed observer sent", err, result)
	}
	target.reset("disconnect")
	output := filepath.Join(dir, "uncertain")
	result, err = connectedrun.Execute(context.Background(), p, "uncertain", output)
	if err != nil || result.State != "uncertain" || result.Verdict == assertion.VerdictPass {
		t.Fatal(err, result)
	}
	count := target.received.Load()
	if _, err = connectedrun.Execute(context.Background(), p, "uncertain", output); err == nil || target.received.Load() != count {
		t.Fatal("uncertain run resent")
	}
}

func TestConnectedRuntimeUsesScopedHTTPBeforeAndAfter(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := prepared(t, target, dir, true)
	result, err := connectedrun.Execute(context.Background(), p, "http-run", filepath.Join(dir, "http-run"))
	if err != nil || result.Verdict != assertion.VerdictPass {
		t.Fatal(err, result)
	}
	if len(result.Observations) != 2 || len(result.Armed) != 2 || target.received.Load() != 2 {
		t.Fatal("pipeline did not consume scoped datasets")
	}
}
func TestConnectedRuntimeCancellationDuringHorizonRetainsNoPassingResult(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := prepared(t, target, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for target.received.Load() < 2 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
	}()
	result, err := connectedrun.Execute(ctx, p, "cancelled", filepath.Join(dir, "cancelled"))
	if err != nil || result.Verdict == assertion.VerdictPass || result.State == "complete" {
		t.Fatal(err, result)
	}
	count := target.received.Load()
	if _, err := connectedrun.Open(context.Background(), filepath.Join(dir, "cancelled")); err != nil {
		t.Fatal(err)
	}
	if count != target.received.Load() {
		t.Fatal("reopen sent")
	}
}

func TestConnectedRuntimeThroughExistingTestCommand(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	prepared(t, target, dir)
	args := []string{"--operation-policy", testlicense.New(t), "test", filepath.Join(dir, "plan"), "--connected-config", filepath.Join(dir, "execution.json")}
	var out, stderr bytes.Buffer
	if err := cli.Execute("test", args, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if target.received.Load() != 0 || !strings.Contains(out.String(), "not-evaluated") {
		t.Fatal("preview executed or implied pass")
	}
	out.Reset()
	args = append(args, "--send", "--instance", "cli-run", "--output", filepath.Join(dir, "cli-run"))
	if err := cli.Execute("test", args, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if target.received.Load() != 2 || !strings.Contains(out.String(), `"verdict":"pass"`) {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), dir) || strings.Contains(out.String(), "SAME") {
		t.Fatal("summary exposed private values")
	}
}

func TestConnectedRuntimeRejectsResealedIntentAndPhaseTimeTampering(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := prepared(t, target, dir)
	output := filepath.Join(dir, "run")
	if _, err := connectedrun.Execute(context.Background(), p, "run", output); err != nil {
		t.Fatal(err)
	}
	reseal := func() { resealDirectory(t, output, connectedrun.Schema) }
	intent := filepath.Join(output, "stimulus-intent.json")
	original, _ := os.ReadFile(intent)
	var binding networkaction.Binding
	if err := json.Unmarshal(original, &binding); err != nil {
		t.Fatal(err)
	}
	binding.Configuration = strings.Repeat("0", 64)
	write(t, intent, binding)
	reseal()
	if _, err := connectedrun.Open(context.Background(), output); err == nil {
		t.Fatal("resealed intent mismatch accepted")
	}
	os.WriteFile(intent, original, 0600)
	manifest := filepath.Join(output, "manifest.json")
	raw, _ := os.ReadFile(manifest)
	var result connectedrun.Result
	json.Unmarshal(raw, &result)
	result.CompletedAt = result.StartedAt
	write(t, manifest, result)
	reseal()
	if _, err := connectedrun.Open(context.Background(), output); err == nil {
		t.Fatal("acquisitions outside execution window accepted")
	}
}

func TestConnectedRuntimePreservesUncertaintyWhenObservationFailsOrCancels(t *testing.T) {
	for _, cancelHorizon := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelHorizon), func(t *testing.T) {
			dir := t.TempDir()
			target := startTarget(t, dir)
			p := prepared(t, target, dir)
			if cancelHorizon {
				target.reset("disconnect")
			} else {
				target.reset("disconnect-missing")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			output := filepath.Join(dir, "run")
			if cancelHorizon {
				go func() {
					for {
						if _, err := os.Stat(filepath.Join(output, "transport", "identity.sha256")); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}()
			}
			result, err := connectedrun.Execute(ctx, p, "run", output)
			if err != nil || result.State != "uncertain" || result.Verdict == assertion.VerdictPass || result.Evaluation != "" {
				t.Fatal(err, result)
			}
			reopened, err := connectedrun.Open(context.Background(), output)
			if err != nil || reopened.State != "uncertain" {
				t.Fatal(err, reopened)
			}
			result.State = "cancelled"
			write(t, filepath.Join(output, "manifest.json"), result)
			resealDirectory(t, output, connectedrun.Schema)
			if _, err := connectedrun.Open(context.Background(), output); err == nil {
				t.Fatal("resealed uncertainty downgrade accepted")
			}
		})
	}
}

func resealDirectory(t *testing.T, path, schema string) {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(path, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name, err := filepath.Rel(path, file)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(file)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(artifactdir.Identity(schema, files)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestConnectedRuntimeRejectsResealedReadinessProjectionAndNetworkScope(t *testing.T) {
	for _, network := range []bool{false, true} {
		t.Run(fmt.Sprint(network), func(t *testing.T) {
			dir := t.TempDir()
			target := startTarget(t, dir)
			p := prepared(t, target, dir, network)
			output := filepath.Join(dir, "run")
			result, err := connectedrun.Execute(context.Background(), p, "run", output)
			if err != nil || result.Verdict != assertion.VerdictPass {
				t.Fatal(err, result)
			}
			path := filepath.Join(output, "observations", "armed-after")
			if network {
				actionPath := filepath.Join(path, "network")
				raw, _ := os.ReadFile(filepath.Join(actionPath, "action.json"))
				var spec networkaction.HTTPSpec
				json.Unmarshal(raw, &spec)
				spec.Plan = strings.Repeat("0", 64)
				raw, _ = json.Marshal(spec, json.Deterministic(true))
				policy, _ := os.ReadFile(filepath.Join(actionPath, "policy.json"))
				action, err := networkaction.PrepareHTTP(raw, policy)
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(actionPath, "action.json"), spec)
				raw, _ = os.ReadFile(filepath.Join(actionPath, "result.json"))
				var receipt networkaction.Result
				json.Unmarshal(raw, &receipt)
				receipt.Binding = action.Binding()
				write(t, filepath.Join(actionPath, "result.json"), receipt)
				write(t, filepath.Join(actionPath, "intent.json"), struct {
					Binding networkaction.Binding `json:"binding"`
					State   string                `json:"state"`
				}{action.Binding(), "uncertain-until-settled"})
				resealDirectory(t, actionPath, networkaction.ResultSchema)
				if _, err := networkaction.OpenHTTP(actionPath); err != nil {
					t.Fatal("test must retain a valid action for another plan", err)
				}
				resealDirectory(t, path, observesource.ScopedDatasetAcquisitionSchema)
			} else {
				snapshot, err := observesource.OpenDataset(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				doc := snapshot.Document()
				doc.Projection.ID = "other-projection"
				raw, _ := os.ReadFile(filepath.Join(path, "dataset", "material.bin"))
				replacement, err := dataset.Build(context.Background(), doc.Binding, doc.Projection, doc.Acquisition, raw)
				if err != nil {
					t.Fatal(err)
				}
				os.RemoveAll(filepath.Join(path, "dataset"))
				replacementPath := filepath.Join(dir, "replacement")
				if err := replacement.Write(context.Background(), replacementPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacementPath, filepath.Join(path, "dataset")); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(path, "manifest.json"), struct {
					Schema   string          `json:"schema"`
					Binding  dataset.Binding `json:"binding"`
					Identity string          `json:"dataset_identity"`
				}{observesource.DatasetAcquisitionSchema, doc.Binding, replacement.Identity()})
				resealDirectory(t, path, observesource.DatasetAcquisitionSchema)
				result.Armed["after"] = replacement.Identity()
				write(t, filepath.Join(output, "manifest.json"), result)
			}
			if _, err := observesource.OpenDataset(context.Background(), path); err != nil {
				t.Fatal("substitute acquisition must be internally valid", err)
			}
			resealDirectory(t, output, connectedrun.Schema)
			if _, err := connectedrun.Open(context.Background(), output); err == nil {
				t.Fatal("substituted readiness evidence accepted")
			}
		})
	}
}

func (s *target) setOutput(output func(int, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = output
}
func (s *target) getOutput() func(int, string) { s.mu.Lock(); defer s.mu.Unlock(); return s.output }
