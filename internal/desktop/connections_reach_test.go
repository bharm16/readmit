package desktop_test

// An operation reaching outside the window is listed as the object it
// reaches: the saved environment, observation or runner when it acts on one,
// or else the operation itself named by what it carries, with the
// destination it reaches. These tests read the inventory at the moment each
// operation records what it reaches, before it reaches it, and hold every
// reading to exactly that row.

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/mllp"
)

// reachedRow is the one active row an operation is listed as.
type reachedRow struct {
	ref, name, destination, operation string
}

// activeWhileReaching runs call and returns the active rows the inventory
// listed each time the operation recorded what it reaches, the declared
// program row aside.
func activeWhileReaching(t *testing.T, app *desktop.App, context desktop.RequestContext, call func()) [][]desktop.ConnectionRow {
	t.Helper()
	var mu sync.Mutex
	var readings [][]desktop.ConnectionRow
	desktop.OnReachForTest(app, func() {
		active := []desktop.ConnectionRow{}
		for _, row := range app.ListConnections(context).Rows {
			if row.State == desktop.ConnectionActive && row.Ref != "program" {
				active = append(active, row)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		readings = append(readings, active)
	})
	defer desktop.OnReachForTest(app, nil)
	call()
	mu.Lock()
	defer mu.Unlock()
	return readings
}

func TestEveryActiveOperationNamesTheObjectItReaches(t *testing.T) {
	countLookups(t)
	for _, operation := range []struct {
		name  string
		start func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow)
	}{
		{"reviewed collection", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, context := namedProject(t)
			writeDocument(t, context.Project, "export.csv", "appointment,status\nA1,booked\n")
			saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
				Draft: desktop.ItemDraft{Name: "Appointments", Observation: observationDraft(t, "appointments")}, IntentID: "first"})
			if saved.Saved == nil {
				t.Fatalf("save: %+v", saved)
			}
			prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}})
			if prepared.Review == nil || !prepared.Review.Ready {
				t.Fatalf("review: %+v", prepared)
			}
			return app, context, func() {
					app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "collect"})
				},
				reachedRow{"observation:" + saved.Saved.ID, "Appointments", "export.csv", "observation"}
		}},
		{"reviewed reset", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, context := namedProject(t)
			writeDocument(t, context.Project, "ledger.json", emptyReceiverSnapshot)
			plan, links := resetDraft()
			environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab", Draft: desktop.ItemDraft{Name: "Lab",
				Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain"), ResetPlan: plan, Links: links}})
			prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ResetEnvironmentAction, Items: []desktop.ItemRef{environment}})
			if prepared.Review == nil || !prepared.Review.Ready {
				t.Fatalf("review: %+v", prepared)
			}
			return app, context, func() {
					app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "reset",
						Decisions: desktop.ReviewDecisions{Confirmed: []string{prepared.Review.Reset.Actions[0].ID}}})
				},
				reachedRow{"environment:" + environment.ID, "Lab", "127.0.0.1:2575", "target-reset"}
		}},
		{"connectivity check of a target file", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, dir, address := targetWorkspace(t)
			return app, desktop.RequestContext{}, func() {
				app.CheckTarget(desktop.TargetCheckRequest{Workspace: dir, TargetFile: "target.json", PolicyFile: "policy.json"})
			}, reachedRow{"operation:target-check", "lab-mllp", address, "target-check"}
		}},
		{"connectivity check of a saved environment's target", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, context := namedProject(t)
			address := newCountingEndpoint(t, false).address
			environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab", Draft: desktop.ItemDraft{Name: "Lab",
				Environment: environmentDraft(address, "nonproduction", "plain")}})
			target := memberEntry(t, context.Project, environment, "target")
			return app, context, func() {
				app.CheckTarget(desktop.TargetCheckRequest{Workspace: context.Project, TargetFile: target})
			}, reachedRow{"environment:" + environment.ID, "Lab", address, "target-check"}
		}},
		{"fixture reset of a target file", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, dir, address := targetWorkspace(t)
			return app, desktop.RequestContext{}, func() {
				app.ResetTarget(desktop.TargetResetRequest{Workspace: dir, TargetFile: "target.json", PlanFile: "reset-plan.json",
					OutcomeFile: "reset-outcome.json", PolicyFile: "policy.json"})
			}, reachedRow{"operation:target-reset", "lab-mllp", address, "target-reset"}
		}},
		{"controlled reduction", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			address := newCountingEndpoint(t, true).address
			workspace := ackWorkspace(t, address)
			if err := os.Remove(filepath.Join(workspace, "target.json")); err != nil {
				t.Fatal(err)
			}
			recorded := app.ReadTarget(workspace, "target.json")
			if recorded.Target == nil {
				t.Fatalf("new target: %+v", recorded)
			}
			environment := *recorded.Target
			environment.Name, environment.Classification, environment.ApprovedTransport, environment.Address = "lab-mllp", "nonproduction", true, address
			if saved := app.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "target.json", Target: environment}); saved.State != desktop.Completed {
				t.Fatalf("target: %+v", saved)
			}
			writeAckSpec(t, workspace, "booking.json", "AE")
			writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
			writeDocument(t, workspace, "plan.json", `{"schema":"readmit-reset-plan/v1","environment":"lab-mllp","actions":[`+
				`{"id":"confirm","operator":"operator_confirms","authority":"none","instructions":"Empty the ledger"}]}`)
			opened := app.OpenCase(workspace, "case")
			if opened.Case == nil {
				t.Fatalf("case: %+v", opened)
			}
			return app, desktop.RequestContext{}, func() {
				app.StartReduction(desktop.ReductionRequest{Workspace: workspace, Case: "case", Identity: opened.Case.Identity,
					Spec: "booking.json", Assertions: []string{"ack"}, Trials: 1, Confirmations: 1, ResetPlan: "plan.json",
					Target: "target.json", Policy: "policy.json", Confirmed: []string{"confirm"}, Work: "reduction"})
			}, reachedRow{"operation:reduction", "lab-mllp", address, "reduction"}
		}},
		{"durable run", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			address := newCountingEndpoint(t, true).address
			workspace := ackWorkspace(t, address)
			writeAckSpec(t, workspace, "booking.json", "AA")
			identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "booking.json", Output: "run"})
			return app, desktop.RequestContext{}, func() {
				app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "booking.json", Output: "run", Expected: identity})
			}, reachedRow{"operation:durable-run", "ACK regression", address, "durable-run"}
		}},
		{"suite run", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			address := newCountingEndpoint(t, true).address
			workspace := ackWorkspace(t, address)
			writeAckSpec(t, workspace, "booking.json", "AA")
			writeSuite(t, workspace, "nightly.json")
			identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "nightly.json", Environment: "east"})
			return app, desktop.RequestContext{}, func() {
				app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: workspace, Suite: "nightly.json", Environment: "east",
					Output: "suite-run", Expected: identity})
			}, reachedRow{"operation:durable-run", "nightly", address, "durable-run"}
		}},
		{"replay send", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			receiver := newReplayReceiver(t)
			workspace, identity := replayWorkspace(t, app, receiver.address)
			request := bothMessages(workspace, identity)
			preview := replayPreviewed(t, app, request)
			return app, desktop.RequestContext{}, func() {
				app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true})
			}, reachedRow{"operation:replay", "lab-replay", receiver.address, "replay"}
		}},
		{"observation collection of source files", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			dir := t.TempDir()
			writeDocument(t, dir, "window.json", facadeWindowDocument)
			writeDocument(t, dir, "source.json", facadeSourceDocument)
			writeDocument(t, dir, "export.csv", "appointment,status\nA1,booked\n")
			return app, desktop.RequestContext{}, func() {
				app.CollectObservation(desktop.ObservationCollectFacadeRequest{Workspace: dir, SourceFile: "source.json", WindowFile: "window.json",
					OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: true, Produced: []string{"A1"}})
			}, reachedRow{"operation:observation", "scheduling-archive", "export.csv", "observation"}
		}},
		{"source access check", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, root := sourceWorkspace(t)
			return app, desktop.RequestContext{}, func() {
				app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json"})
			}, reachedRow{"operation:source-diagnosis", "exports", "exports.example.test:22", "source-diagnosis"}
		}},
		{"source collection", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app, root := sourceWorkspace(t)
			return app, desktop.RequestContext{}, func() {
				app.CollectSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json",
					OutputName: "staged", ReceiptName: "receipt.json"})
			}, reachedRow{"operation:collect", "exports", "exports.example.test:22", "collect"}
		}},
		{"capture", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			app := workspaceApp(t)
			root := t.TempDir()
			policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
			if err != nil {
				t.Fatal(err)
			}
			if saved := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: root, PolicyFile: "receiver.json", Policy: policy}); saved.State != desktop.Completed {
				t.Fatalf("receiver policy: %+v", saved)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			return app, desktop.RequestContext{}, func() {
				done := make(chan desktop.CaptureSessionResult, 1)
				go func() {
					done <- app.StartCapture(desktop.CaptureRequest{Workspace: root, Kind: "collect", Address: address,
						PolicyFile: "receiver.json", OutputName: "case", JournalName: "journal", MaxMessages: 1, IdleTimeout: "5s"})
				}()
				var conn net.Conn
				for deadline := time.Now().Add(5 * time.Second); conn == nil && time.Now().Before(deadline); {
					if conn, err = net.DialTimeout("tcp", address, 50*time.Millisecond); err != nil {
						conn = nil
						time.Sleep(10 * time.Millisecond)
					}
				}
				if conn == nil {
					app.Cancel("capture")
					<-done
					t.Error("the capture never listened on its port")
					return
				}
				_, _ = conn.Write(mllp.Frame([]byte("MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r")))
				_ = conn.Close()
				<-done
			}, reachedRow{"operation:capture", "case", address, "capture"}
		}},
		{"runner enrollment", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
			configPath := fixture.gateRunnerConfig(t)
			return fixture.app, desktop.RequestContext{}, func() { fixture.app.EnrollRunner(configPath) },
				reachedRow{"runner:config", "Runner lab", hostOf(t, fixture.serverURL), "runner-enrollment"}
		}},
		{"runner execution", func(t *testing.T) (*desktop.App, desktop.RequestContext, func(), reachedRow) {
			fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
			configPath := fixture.gateRunnerConfig(t)
			jobPath := filepath.Join(t.TempDir(), "job.json")
			if saved := fixture.app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: writableSpec(t, t.TempDir()), Output: jobPath}); saved.State != desktop.Completed {
				t.Fatalf("job: %+v", saved)
			}
			return fixture.app, desktop.RequestContext{}, func() {
				fixture.app.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath})
			}, reachedRow{"runner:config", "Runner lab", hostOf(t, fixture.serverURL), "runner"}
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			app, context, call, want := operation.start(t)
			readings := activeWhileReaching(t, app, context, call)
			if len(readings) == 0 {
				t.Fatal("the operation never recorded what it reaches")
			}
			for _, active := range readings {
				if len(active) != 1 {
					t.Fatalf("while it reached outside the inventory listed %d active rows, want its one: %+v", len(active), active)
				}
				row := active[0]
				if got := (reachedRow{row.Ref, row.Name, row.Destination, row.Detail.Operation}); got != want {
					t.Errorf("while it reached outside it was listed as %+v, want %+v", got, want)
				}
			}
			for _, row := range app.ListConnections(context).Rows {
				if row.State == desktop.ConnectionActive {
					t.Errorf("after it ended the inventory still lists it active: %+v", row)
				}
			}
		})
	}
}

// hostOf is the host and port of an HTTPS origin.
func hostOf(t *testing.T, origin string) string {
	t.Helper()
	parsed, err := url.Parse(origin)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Host
}

// targetWorkspace is a workspace holding a nonproduction target named
// lab-mllp at a loopback listener that accepts and closes, a policy
// approving loopback and a reset plan whose one action checks the endpoint
// is quiet.
func targetWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	app := workspaceApp(t)
	dir := t.TempDir()
	recorded := app.ReadTarget(dir, "target.json")
	if recorded.Target == nil {
		t.Fatalf("new target: %+v", recorded)
	}
	environment := *recorded.Target
	environment.Name, environment.Classification, environment.ApprovedTransport = "lab-mllp", "nonproduction", true
	environment.Address = newCountingEndpoint(t, false).address
	if saved := app.SaveTarget(desktop.TargetSaveRequest{Workspace: dir, TargetFile: "target.json", Target: environment}); saved.State != desktop.Completed {
		t.Fatalf("target: %+v", saved)
	}
	writeDocument(t, dir, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	writeDocument(t, dir, "reset-plan.json", `{"schema":"readmit-reset-plan/v1","environment":"lab-mllp","actions":[`+
		`{"id":"quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"Confirm the endpoint is quiet"}]}`)
	return app, dir, environment.Address
}

// sourceWorkspace is a workspace registering a transfer source named exports
// at a name the test's resolver refuses, and a policy for it.
func sourceWorkspace(t *testing.T) (*desktop.App, string) {
	t.Helper()
	app := workspaceApp(t)
	root := t.TempDir()
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Transfer, Scope: "appointments",
		Address: "exports.example.test:22", Classification: "nonproduction", Command: "/bin/cat",
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: source}); saved.State != desktop.Completed {
		t.Fatalf("source: %+v", saved)
	}
	writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["192.0.2.0/24"]}`)
	return app, root
}

// The runner a window last read, saved or enrolled with is listed from what
// it recorded then: removing its configuration file does not change the row,
// because listing never reads it; and while a runner operation holds the slot
// the runner row itself is active, with no separate activity row.
func TestListConnectionsListsTheRunnerTheWindowLastConfigured(t *testing.T) {
	app := workspaceApp(t)
	if listed := app.ListConnections(desktop.RequestContext{}); listed.State != desktop.Empty || len(listed.Rows) != 0 {
		t.Fatalf("a window that configured no runner lists: %+v", listed)
	}
	request := runnerConfigInput(t, "/var/lib/readmit-runner/runs")
	request.Output = filepath.Join(t.TempDir(), "runner.json")
	if saved := app.SaveRunnerConfig(request); saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	if err := os.Remove(request.Output); err != nil {
		t.Fatal(err)
	}
	row, ok := connectionRows(t, app, desktop.RequestContext{})["runner:config"]
	if !ok || row.Name != "Runner lab" || row.Kind != desktop.ConnectionRunner || row.Destination != "hub.example:8443" ||
		row.State != desktop.ConnectionNotChecked || row.CheckedAt != nil || row.Owner.Kind != desktop.OwnerRunner ||
		row.Disclosure != "runner" || len(row.Actions) != 1 || row.Actions[0] != desktop.ConnectionEdit || row.Detail.ConfigPath != request.Output {
		t.Fatalf("the runner the window saved: %+v", row)
	}

	// Reading another configuration makes it the one listed.
	request.Environment, request.Output = "night-lab", filepath.Join(t.TempDir(), "night.json")
	if saved := app.SaveRunnerConfig(request); saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	if read := app.ReadRunnerConfig(request.Output); read.State != desktop.Completed {
		t.Fatalf("read: %+v", read)
	}
	rows := connectionRows(t, app, desktop.RequestContext{})
	if row := rows["runner:config"]; row.Name != "Runner night-lab" || row.Detail.ConfigPath != request.Output || len(rows) != 1 {
		t.Fatalf("the runner the window read last: %+v", rows)
	}

	for _, operation := range []string{"runner-enrollment", "runner"} {
		release, held := desktop.HoldSlotForTest(app, operation)
		if !held {
			t.Fatal("the slot was not free")
		}
		active := app.ListConnections(desktop.RequestContext{}).Rows
		release()
		if len(active) != 1 || active[0].Ref != "runner:config" || active[0].State != desktop.ConnectionActive || active[0].Detail.Operation != operation {
			t.Errorf("while %s holds the slot: %+v", operation, active)
		}
	}
}

// A team hub session this window connects reads Connected, with when it was
// last seen and Disconnect offered; once the window ends it, the row reads
// Disconnected, last seen when it ended, and offers only Edit.
func TestHubSessionMarksReadConnectedThenDisconnected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	app := newConnectedHubApp(t, mux, "scheduling-lead", []string{"evidence.read"}, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no sign-in in this test", http.StatusBadRequest)
	}).app

	connected := connectionRows(t, app, desktop.RequestContext{})["hub:client"]
	if connected.State != desktop.ConnectionConnected || connected.LastSeen == nil || connected.CheckedAt != nil ||
		!slices.Equal(connected.Actions, []desktop.ConnectionAction{desktop.ConnectionEdit, desktop.ConnectionDisconnect}) {
		t.Fatalf("a connected session: %+v", connected)
	}

	ended := time.Date(2026, 9, 27, 14, 30, 5, 0, time.UTC)
	desktop.SetClockForTest(app, func() time.Time { return ended })
	if result := app.DisconnectHub(); result.State != desktop.Completed {
		t.Fatalf("disconnect: %+v", result)
	}
	disconnected := connectionRows(t, app, desktop.RequestContext{})["hub:client"]
	if disconnected.State != desktop.ConnectionDisconnected || disconnected.LastSeen == nil || *disconnected.LastSeen != catalog.Stamp(ended) ||
		!slices.Equal(disconnected.Actions, []desktop.ConnectionAction{desktop.ConnectionEdit}) {
		t.Fatalf("an ended session: %+v", disconnected)
	}
}
