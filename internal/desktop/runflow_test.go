package desktop_test

// The connected run flow: preflight reads the exact plan a send would
// execute, execution is bound to the identity the preflight fixed, progress
// reads the journal the run is writing, and retained evidence reopens
// read-only through the readers the command line uses. Completion, assertion
// failure, execution error and delivery uncertainty stay separate facts
// through every step, and no path here sends twice.

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ackingPeer answers every frame with one acknowledgement carrying the code it
// was given, after an optional delay that holds one exchange open. It is the
// independent synthetic target a send is really delivered to in these tests,
// never a loop the facade answers itself.
type ackingPeer struct {
	address string
	delay   time.Duration
	mu      sync.Mutex
	frames  int
}

func newAckingPeer(t *testing.T, code string) *ackingPeer {
	t.Helper()
	return newDelayedAckingPeer(t, code, 0)
}

func newDelayedAckingPeer(t *testing.T, code string, delay time.Duration) *ackingPeer {
	t.Helper()
	peer := &ackingPeer{address: "", delay: delay}
	_ = peer
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer.address = listener.Addr().String()
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				// A frame is counted when it arrives, before the exchange is
				// held open, so a test that sees the delivery acts while the
				// sender is still waiting for its acknowledgement.
				peer.mu.Lock()
				peer.frames++
				frame := peer.frames
				peer.mu.Unlock()
				if delay > 0 {
					time.Sleep(delay)
				}
				_, _ = fmt.Fprintf(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-%d|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", frame, code)
			}()
		}
	}()
	return peer
}

func (p *ackingPeer) deliveries() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frames
}

// ackWorkspace is one workspace holding one case, one spec that sends its one
// message at the acking peer, and the target configuration naming the peer.
func ackWorkspace(t *testing.T, address string) string {
	t.Helper()
	workspace := t.TempDir()
	writeCase(t, workspace, "case", framed(string(listenFrame(t))))
	documents := map[string]any{
		"target.json": replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address,
			Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "2s", MaxACKBytes: 4096},
	}
	encoded, err := json.Marshal(documents["target.json"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "target.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// preflighted is the identity a preflight of the request fixes. Every start
// names the one its preflight showed, as the run panel does.
func preflighted(t testing.TB, app *desktop.App, request desktop.RunPreflightRequest) string {
	t.Helper()
	preflight := app.PreflightRun(request)
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight %s: %+v", request.Spec, preflight)
	}
	return preflight.Preflight.Identity
}

func writeAckSpec(t *testing.T, workspace, name, code string) {
	t.Helper()
	value := code
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK regression",
		Input:       testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}},
		Target:      "target.json",
		Setup:       testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset the fixture deliberately"},
		Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary},
		Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001",
			Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7Present, Text: &value}}}}}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, name), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

const hl7Present = "present"

// listenFrame is the framed fixture message the shared test data holds; the
// case it builds is the one every run here sends.
func listenFrame(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The preflight reports the exact plan a send would execute: the selected
// input, the sealed target and the environment it records, the boundary and
// reset requirements, the pinned versions, the deadline, a generated fresh
// destination and the backend's own admission decision. None of it is a
// verdict and none of it touched the network.
func TestPreflightShowsTheExactPlanWithoutSendingOrVerdict(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := workspaceApp(t)

	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight: %+v", preflight)
	}
	plan := preflight.Preflight
	if plan.Kind != "test" || plan.Name != "ACK regression" || plan.Schema != testrunner.SpecSchema {
		t.Fatalf("the preflight did not name the test it would run: %+v", plan)
	}
	if plan.Identity == "" || len(plan.Selected) != 1 || plan.Selected[0].Source != "s0001-e000001" {
		t.Fatalf("the preflight did not fix the exact selected input: %+v", plan)
	}
	if plan.Target.Address != peer.address || plan.Target.TestEndpoint != true || plan.Target.Classification != "unclassified" {
		t.Fatalf("the preflight did not report the sealed target: %+v", plan.Target)
	}
	if plan.Boundary != testrunner.ACKBoundary || plan.Reset == "" || plan.InitialState != "operator-declared" {
		t.Fatalf("the preflight did not report the observation and reset requirements: %+v", plan)
	}
	if plan.Engine.Spec != testrunner.SpecSchema || plan.Engine.Profile == "" || plan.Deadline == "" {
		t.Fatalf("the preflight did not pin the versions and the deadline: %+v", plan.Engine)
	}
	if !plan.Destination.Generated || !plan.Destination.Fresh || plan.Destination.Name == "" {
		t.Fatalf("the preflight did not propose a fresh destination: %+v", plan.Destination)
	}
	if !plan.Admission.Admitted {
		t.Fatalf("an activated operation term was not admitted in preflight: %+v", plan.Admission)
	}
	if peer.deliveries() != 0 {
		t.Fatal("preflight sent something")
	}
}

// A destination that already exists is refused before anything is created,
// and a name that is not one entry of the workspace is refused the same way.
func TestPreflightRefusesAnExistingOrInvalidDestination(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	if err := os.Mkdir(filepath.Join(workspace, "taken"), 0700); err != nil {
		t.Fatal(err)
	}
	app := workspaceApp(t)

	exists := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json", Output: "taken"})
	if exists.State != desktop.Failed || strings.Contains(exists.Reason, workspace) {
		t.Fatalf("an existing destination was not refused: %+v", exists)
	}
	escape := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json", Output: "../outside"})
	if escape.State != desktop.Failed {
		t.Fatalf("a workspace escape was not refused: %+v", escape)
	}
	notASpec := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "target.json"})
	if notASpec.State != desktop.Failed {
		t.Fatalf("a non-spec entry was preflighted as a test: %+v", notASpec)
	}
	absent := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "absent.json"})
	if absent.State != desktop.Failed {
		t.Fatalf("an absent spec was preflighted: %+v", absent)
	}
}

// Without an activated operation term the preflight still reports the plan,
// and the admission it reports is denied — the same decision execution would
// get, shown before anything is created rather than after.
func TestPreflightReportsDeniedAdmissionAndExecutionRefuses(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(t.TempDir(), "drafts.json"))

	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight: %+v", preflight)
	}
	if preflight.Preflight.Admission.Admitted || preflight.Preflight.Admission.Reason == "" {
		t.Fatalf("an unconfigured term was admitted in preflight: %+v", preflight.Preflight.Admission)
	}
	executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: preflight.Preflight.Identity})
	if executed.State != desktop.PermissionDenied || executed.Run != nil {
		t.Fatalf("execution without admission did anything: %+v", executed)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "job-001")); !os.IsNotExist(err) {
		t.Fatal("a refused execution created a run folder")
	}
	if peer.deliveries() != 0 {
		t.Fatal("a refused execution sent something")
	}
}

// Changed inputs invalidate the preflight: a spec rewritten after the
// preflight is refused by the send, naming why, and nothing reaches the
// receiver.
func TestAChangedSpecAfterPreflightIsRefusedByTheSend(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := workspaceApp(t)

	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})
	if preflight.State != desktop.Completed {
		t.Fatalf("preflight: %+v", preflight)
	}
	writeAckSpec(t, workspace, "reschedule.json", "AE")
	executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: preflight.Preflight.Identity})
	if executed.State != desktop.Failed || executed.Run != nil {
		t.Fatalf("a changed spec was executed as though nothing changed: %+v", executed)
	}
	if !strings.Contains(executed.Reason, "changed after the preflight") {
		t.Fatalf("the refusal did not name the stale preflight: %q", executed.Reason)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "job-001")); !os.IsNotExist(err) {
		t.Fatal("a refused send left a run folder behind")
	}
	if peer.deliveries() != 0 {
		t.Fatal("a refused send delivered a message")
	}
}

// The preflight pins every input the send would consume, not only the spec: a
// target configuration edited after the preflight — the spec's bytes
// unchanged — is refused by the send, and nothing reaches the receiver.
func TestAChangedTargetConfigurationAfterPreflightIsRefusedByTheSend(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := workspaceApp(t)

	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: peer.address,
		Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "3s", MaxACKBytes: 4096}
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "target.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: identity})
	if executed.State != desktop.Failed || executed.Run != nil ||
		executed.Reason != "the selected test changed after the preflight; preflight it again before executing" {
		t.Fatalf("a changed target configuration was executed as though nothing changed: %+v", executed)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "job-001")); !os.IsNotExist(err) {
		t.Fatal("a refused send left a run folder behind")
	}
	if peer.deliveries() != 0 {
		t.Fatal("a refused send delivered a message")
	}
	// Preflighted again, the edited configuration is what executes.
	again := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})
	if again == identity {
		t.Fatal("the preflight identity did not change with the target configuration")
	}
	if executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: again}); executed.State != desktop.Completed || executed.Run == nil {
		t.Fatalf("the preflighted configuration did not execute: %+v", executed)
	}
}

// A suite rewritten after its preflight is refused by the send before its
// output exists: the suite compiles only the document the preflight
// identified, checked on the bytes it compiles.
func TestASuiteRewrittenAfterPreflightIsRefusedByTheSend(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "booking.json", "AA")
	writeSuite(t, workspace, "nightly.json")
	app := workspaceApp(t)

	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "nightly.json", Environment: "east"})
	writeSuiteRows(t, workspace, "nightly.json", "one", "two")
	executed := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: workspace, Suite: "nightly.json", Environment: "east", Output: "suite-run", Expected: identity})
	if executed.State != desktop.Failed || executed.Report != nil ||
		executed.Reason != "the selected suite changed after the preflight; preflight it again before executing" {
		t.Fatalf("a rewritten suite was executed as though nothing changed: %+v", executed)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "suite-run")); !os.IsNotExist(err) {
		t.Fatal("a refused suite left its output behind")
	}
	if peer.deliveries() != 0 {
		t.Fatal("a refused suite delivered a message")
	}
}

// The whole connected journey over a real independent synthetic target:
// preflight, send once, live progress, read-only evidence with linked
// assertion detail under deliberate reveal, and the same job the command
// line's own readers report.
func TestTheRunJourneyFromPreflightToLinkedAssertionEvidence(t *testing.T) {
	app, workspace := ledgerWorkspace(t)
	listener, fixture := ledgerReceiver(t, workspace)
	defer listener.Close()

	specName := authorLedgerSpec(t, app, workspace)
	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: specName})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight: %+v", preflight)
	}
	plan := preflight.Preflight
	if plan.Boundary != testrunner.LedgerBoundary || plan.Observation == "" {
		t.Fatalf("the ledger boundary was not preflighted: %+v", plan)
	}
	if plan.Engine.Engine == "" || plan.Engine.Profile != observation.Profile {
		t.Fatalf("the engine pin was not preflighted: %+v", plan.Engine)
	}
	served := make(chan error, 1)
	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()
	go func() { _, err := fixture.Serve(serveCtx, listener); served <- err }()

	executed := app.StartDurableRun(desktop.DurableRunRequest{
		Workspace: workspace, Spec: specName, Output: plan.Destination.Name, Expected: plan.Identity,
	})
	if executed.State != desktop.Completed || executed.Run == nil || executed.Run.State != durablerun.Passed {
		t.Fatalf("the run did not pass: %+v", executed)
	}

	// The progress read reports the journal's own state and counts, and never
	// claims the operation slot.
	progress := app.DurableRunProgress(workspace, plan.Destination.Name)
	if progress.State != desktop.Completed || progress.Progress == nil {
		t.Fatalf("progress: %+v", progress)
	}
	if progress.Progress.Executing || progress.Progress.Phase != string(durablerun.Passed) {
		t.Fatalf("a finished run was not reported as finished: %+v", progress.Progress)
	}
	if progress.Progress.Acknowledged != 2 || progress.Progress.NotAttempted != 0 || progress.Progress.Uncertain != 0 {
		t.Fatalf("the journal's delivery counts were not reported: %+v", progress.Progress)
	}

	// Read-only evidence, values hidden until they are deliberately revealed.
	hidden := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: plan.Destination.Name, Reveal: false})
	if hidden.State != desktop.Completed || hidden.Evidence == nil {
		t.Fatalf("evidence: %+v", hidden)
	}
	evidence := hidden.Evidence
	if !evidence.Durable || evidence.RunState != string(durablerun.Passed) || evidence.Status != string(testrunner.Pass) {
		t.Fatalf("completion, verdict and error were not separate facts: %+v", evidence)
	}
	if evidence.Pin == nil || evidence.Pin.Spec != testrunner.SpecSchema || !evidence.PinRecorded {
		t.Fatalf("the retained engine pin was not reported: %+v", evidence.Pin)
	}
	if evidence.SpecName == "" || evidence.SourceCase != guide.CaseName || evidence.SourceIdentity == "" {
		t.Fatalf("the source case was not linked: %+v", evidence)
	}
	if evidence.FinalRecords != 1 || evidence.InitialRecords != 0 {
		t.Fatalf("the observation counts were not reported: %+v", evidence)
	}
	if len(evidence.Assertions) == 0 || evidence.Assertions[0].Status != string(testrunner.Passed) {
		t.Fatalf("the assertion inventory was not reported: %+v", evidence.Assertions)
	}
	if evidence.Assertions[0].Expected != "" || evidence.Assertions[0].Observed != "" {
		t.Fatalf("values crossed the boundary without the deliberate reveal: %+v", evidence.Assertions[0])
	}
	if evidence.Elapsed == "" || evidence.StartedAt == "" || evidence.CompletedAt == "" {
		t.Fatalf("the run timings were not reported: %+v", evidence)
	}

	revealed := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: plan.Destination.Name, Reveal: true})
	if revealed.State != desktop.Completed || !revealed.Evidence.Revealed {
		t.Fatalf("reveal: %+v", revealed)
	}
	if revealed.Evidence.Assertions[0].Expected == "" {
		t.Fatalf("the revealed expectation was empty: %+v", revealed.Evidence.Assertions[0])
	}

	// The command line's own readers report the same run.
	var stdout, stderr strings.Builder
	if err := cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "run", "status", filepath.Join(workspace, plan.Destination.Name), "--json"}, &stdout, &stderr); err != nil {
		t.Fatalf("run status: %v %s", err, stderr.String())
	}
	var cliSummary durablerun.Summary
	if err := json.Unmarshal([]byte(stdout.String()), &cliSummary); err != nil {
		t.Fatalf("run status output: %q %v", stdout.String(), err)
	}
	if cliSummary != *executed.Run {
		t.Fatalf("the command line and the window disagree about the run: %+v vs %+v", cliSummary, executed.Run)
	}

}

// ledgerWorkspace is the guided sample workspace: the regression case the
// fixture receiver understands, opened the way the window opens it.
func ledgerWorkspace(t *testing.T) (*desktop.App, string) {
	t.Helper()
	app := newApp(t, &chooser{folder: t.TempDir()})
	return app, sample(t, app).Workspace.Root
}

// ledgerReceiver binds the fixture receiver on a loopback port and writes its
// observation into the workspace, the way an operator's nonproduction
// environment would. It is a real independent process-facing target: the
// facade never answers itself.
func ledgerReceiver(t *testing.T, workspace string) (net.Listener, *receiver.Receiver) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := receiver.New(receiver.Config{
		Mode: observation.Fixed, OutputPath: filepath.Join(workspace, "receiver"),
		ObservationPath: filepath.Join(workspace, "observation.json"),
		MaxMessages:     2, MaxFrameBytes: 1 << 20, IdleTimeout: 5 * time.Second,
	})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	target := guide.Target()
	target.Address = listener.Addr().String()
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "durable-target.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	return listener, fixture
}

// authorLedgerSpec walks the same authoring stages the window walks and saves
// the spec, so the durable run executes a test this facade authored.
func authorLedgerSpec(t *testing.T, app *desktop.App, workspace string) string {
	t.Helper()
	opened := app.OpenCase(workspace, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	request := desktop.TestRequest{Workspace: workspace, Case: guide.CaseName, Identity: opened.Case.Identity}
	appointments := 1
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: "durable-target.json"},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh fixture receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &appointments},
		}},
	} {
		request.Answer = answer
		authored := app.AuthorTest(request)
		if (authored.State != desktop.Completed && authored.State != desktop.Empty) || authored.Test == nil {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		request.Draft, request.Answer = authored.Test.Draft, testauthor.Answer{}
	}
	request.Output = "durable-reschedule.json"
	saved := app.SaveTest(request)
	if saved.State != desktop.Completed || saved.Test == nil {
		t.Fatalf("save the authored test: %+v", saved)
	}
	return "durable-reschedule.json"
}

// A cancel action names its own operation. Cancelling the comparison while a
// run is executing does nothing to the run; cancelling the run stops it.
func TestACancelActionCannotTargetADifferentOperation(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := workspaceApp(t)
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})

	done := make(chan desktop.DurableRunResult, 1)
	go func() {
		done <- app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: identity})
	}()
	// The other panel's cancel, pressed while the run holds the slot.
	deadline := time.Now().Add(10 * time.Second)
	for peer.deliveries() == 0 && time.Now().Before(deadline) {
		app.Cancel("run-comparison")
		time.Sleep(5 * time.Millisecond)
	}
	if peer.deliveries() == 0 {
		t.Fatal("nothing was sent")
	}
	app.Cancel("durable-run")
	result := <-done
	if result.State != desktop.Completed || result.Run == nil {
		t.Fatalf("the run did not finish: %+v", result)
	}
	if result.Run.State == durablerun.Cancelled {
		t.Log("the run completed before its own cancel arrived")
	}
	// Cancelling when nothing runs is a no-op, not an error.
	app.Cancel("durable-run")
	app.Cancel("")
}

// Duplicate clicks cannot start two runs: while one execution holds the slot
// a second start of the same spec is refused busy, and the receiver sees
// exactly the deliveries of one execution.
func TestDuplicateClicksCannotStartTwoRuns(t *testing.T) {
	peer := newDelayedAckingPeer(t, "AA", 750*time.Millisecond)
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "reschedule.json", "AA")
	app := workspaceApp(t)
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "reschedule.json"})

	done := make(chan desktop.DurableRunResult, 1)
	go func() {
		done <- app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: identity})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for peer.deliveries() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if peer.deliveries() == 0 {
		t.Fatal("nothing was sent to the test endpoint")
	}
	// The exchange is held open, so the first execution still holds the slot:
	// the duplicate click of the same button is refused busy.
	second := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "reschedule.json", Output: "job-001", Expected: identity})
	if second.State != desktop.Busy || second.Run != nil {
		t.Fatalf("a duplicate click was not refused busy: %+v", second)
	}
	first := <-done
	if first.State != desktop.Completed {
		t.Fatalf("the first run did not finish: %+v", first)
	}
	if deliveries := peer.deliveries(); deliveries != 1 {
		t.Fatalf("the duplicate click delivered more messages: %d", deliveries)
	}
}

// A run whose delivery nobody can confirm stays uncertain and completion,
// assertion failure and execution error remain separate facts in the evidence
// view: no verdict is invented for an unfinished exchange.
func TestUncertainDeliveryNeverBecomesAPassInTheEvidenceView(t *testing.T) {
	peer := newSilentPeer(t)
	workspace, _, _ := crashFixture(t, peer.address)
	app := workspaceApp(t)
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: workspace, Spec: "spec.json"})

	done := make(chan desktop.DurableRunResult, 1)
	go func() {
		done <- app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: "spec.json", Output: "job-001", Expected: identity})
	}()
	select {
	case <-peer.received:
	case <-time.After(20 * time.Second):
		t.Fatal("nothing was sent to the test endpoint")
	}
	app.Cancel("durable-run")
	result := <-done
	if result.State != desktop.Completed {
		t.Fatalf("the cancelled run did not report: %+v", result)
	}

	progress := app.DurableRunProgress(workspace, "job-001")
	if progress.State != desktop.Completed || progress.Progress == nil {
		t.Fatalf("progress: %+v", progress)
	}
	if progress.Progress.Phase != string(durablerun.DeliveryUncertain) || progress.Progress.Uncertain != 1 {
		t.Fatalf("an uncertain delivery was flattened: %+v", progress.Progress)
	}

	evidence := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: "job-001"})
	if evidence.State != desktop.Completed || evidence.Evidence == nil {
		t.Fatalf("evidence: %+v", evidence)
	}
	view := evidence.Evidence
	if view.DeliveryUncertain != true || view.StopReason != string(durablerun.Cancelled) {
		t.Fatalf("the stop reason and the uncertainty were not both retained: %+v", view)
	}
	if view.Status != string(testrunner.ExecutionError) {
		t.Fatalf("an uncertain delivery produced a verdict: %+v", view)
	}
	if view.Acknowledged != 0 || view.Uncertain != 1 {
		t.Fatalf("the delivery counts were not retained: %+v", view)
	}
}

// A suite is selected, preflighted read-only at one of the environments it
// declares, and executed once through the existing durable queue into a fresh
// destination; its jobs land in the run history and reopen read-only.
func TestASuiteIsPreflightedAndExecutedThroughTheExistingQueue(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "booking.json", "AA")
	writeSuite(t, workspace, "nightly.json")
	app := workspaceApp(t)

	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "nightly.json", Environment: "east"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("suite preflight: %+v", preflight)
	}
	plan := preflight.Preflight
	if plan.Kind != "suite" || plan.Suite == nil || plan.Identity == "" {
		t.Fatalf("the suite was not preflighted as a suite: %+v", plan)
	}
	if plan.Suite.Environment != "east" || len(plan.Suite.Jobs) != 1 || plan.Suite.Jobs[0].ID != "booking" {
		t.Fatalf("the suite's jobs were not reported: %+v", plan.Suite)
	}
	if len(plan.Suite.Targets) != 1 || plan.Suite.Targets[0].Address != peer.address {
		t.Fatalf("the environment's bound target was not reported: %+v", plan.Suite.Targets)
	}
	if peer.deliveries() != 0 {
		t.Fatal("the suite preflight sent something")
	}
	// Selecting an environment the suite does not declare is a validation
	// refusal, not an execution.
	absent := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: "nightly.json", Environment: "west"})
	if absent.State != desktop.Failed {
		t.Fatalf("an undeclared environment was preflighted: %+v", absent)
	}

	executed := app.StartSuiteRun(desktop.SuiteRunRequest{
		Workspace: workspace, Suite: "nightly.json", Environment: "east",
		Output: "suite-run", Expected: plan.Identity,
	})
	if executed.State != desktop.Completed || executed.Report == nil {
		t.Fatalf("suite run: %+v", executed)
	}
	if executed.Report.Executed != 1 || len(executed.Report.Jobs) != 1 {
		t.Fatalf("the queue did not execute the suite's one job: %+v", executed.Report)
	}
	job := executed.Report.Jobs[0]
	if job.Admission != "executed" || job.Run == nil || job.Run.State != durablerun.Passed {
		t.Fatalf("the suite's job did not pass: %+v", job)
	}
	// The job reopens read-only from the run history, named through the
	// suite's retained runs directory.
	evidence := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: "suite-run/runs/booking-one"})
	if evidence.State != desktop.Completed || evidence.Evidence == nil {
		t.Fatalf("suite job evidence: %+v", evidence)
	}
	if evidence.Evidence.Status != string(testrunner.Pass) || !evidence.Evidence.Durable {
		t.Fatalf("the suite's job was not reopened as a passed durable run: %+v", evidence.Evidence)
	}
	// A stale suite identity is refused by the send, exactly as a test's is.
	stale := app.StartSuiteRun(desktop.SuiteRunRequest{
		Workspace: workspace, Suite: "nightly.json", Environment: "east",
		Output: "suite-run-2", Expected: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if stale.State != desktop.Failed || !strings.Contains(stale.Reason, "changed after the preflight") {
		t.Fatalf("a changed suite was executed as though nothing changed: %+v", stale)
	}
}

// writeSuite authors one small suite document: one environment, one data row,
// one test whose template is the workspace's saved spec.
func writeSuite(t *testing.T, workspace, name string) {
	t.Helper()
	writeSuiteRows(t, workspace, name, "one")
}

// writeSuiteRows is writeSuite with one data row, and so one job, per row id,
// executed in turn.
func writeSuiteRows(t *testing.T, workspace, name string, ids ...string) {
	t.Helper()
	rows := []any{}
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "case": "case"})
	}
	document := map[string]any{
		"schema":       "readmit-suite/v1",
		"id":           "nightly",
		"owner":        "interop-team",
		"tags":         []string{"smoke"},
		"parallelism":  1,
		"environments": []any{map[string]any{"id": "east", "site": "hospital-a", "bindings": []any{map[string]any{"parameter": "scheduling", "target": "target.json"}}}},
		"tables":       []any{map[string]any{"id": "patients", "rows": rows}},
		"tests": []any{map[string]any{
			"id": "booking", "spec": "booking.json", "owner": "interop-team", "tags": []string{"smoke"},
			"parameter": "scheduling", "table": "patients", "isolation": "shared", "sequence": []string{"s0001-e000001"},
		}},
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, name), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

// The evidence view refuses an entry that is not a retained execution, and a
// nested name that is not a suite job, rather than guessing from a file name.
func TestOpenRunEvidenceRefusesWhatItDoesNotVerify(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	workspace := ackWorkspace(t, peer.address)
	writeAckSpec(t, workspace, "booking.json", "AA")
	app := workspaceApp(t)

	notARun := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: "booking.json"})
	if notARun.State != desktop.Failed {
		t.Fatalf("a spec file was opened as an execution: %+v", notARun)
	}
	nested := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: "booking.json/runs/other"})
	if nested.State != desktop.Failed {
		t.Fatalf("a nested non-run was opened as an execution: %+v", nested)
	}
	absent := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: workspace, Entry: "absent"})
	if absent.State != desktop.Failed {
		t.Fatalf("an absent entry was opened as an execution: %+v", absent)
	}
}

// The progress read of a folder with no run yet is empty, not a failure, and
// a folder that is not a run at all is refused.
func TestDurableRunProgressReportsEmptyAndRefusesWhatIsNotARun(t *testing.T) {
	workspace := ackWorkspace(t, "127.0.0.1:1")
	app := workspaceApp(t)

	empty := app.DurableRunProgress(workspace, "job-001")
	if empty.State != desktop.Empty {
		t.Fatalf("a folder with no run was not empty: %+v", empty)
	}
	specFolder := app.DurableRunProgress(workspace, "case")
	if specFolder.State != desktop.Failed {
		t.Fatalf("a case directory was read as a run: %+v", specFolder)
	}
}

// A production-classified environment and an unbindable credential reference
// are refused by the local validation itself: no socket is opened, nothing is
// created, and the refusal is not a verdict about anything.
func TestProductionAndCredentialRefusalsHappenBeforeAnySend(t *testing.T) {
	app := workspaceApp(t)

	production := ackWorkspace(t, "127.0.0.1:1")
	writeDocument(t, production, "target.json", `{"schema":"readmit-target/v3","name":"prod","classification":"production","test_endpoint":true,"address":"127.0.0.1:1","transport":"plain","connect_timeout":"2s","message_timeout":"2s","max_ack_bytes":4096}`)
	writeAckSpec(t, production, "reschedule.json", "AA")
	refused := app.PreflightRun(desktop.RunPreflightRequest{Workspace: production, Spec: "reschedule.json"})
	if refused.State != desktop.Failed || refused.Preflight != nil {
		t.Fatalf("a production-classified environment was preflighted: %+v", refused)
	}

	credentialed := ackWorkspace(t, "127.0.0.1:1")
	writeDocument(t, credentialed, "target.json", `{"schema":"readmit-target/v3","name":"lab","classification":"nonproduction","test_endpoint":true,"address":"127.0.0.1:1","transport":"plain","connect_timeout":"2s","message_timeout":"2s","max_ack_bytes":4096,"credential":{"secrets_file":"absent-secrets.json","reference":"mllp"}}`)
	writeAckSpec(t, credentialed, "reschedule.json", "AA")
	unbound := app.PreflightRun(desktop.RunPreflightRequest{Workspace: credentialed, Spec: "reschedule.json"})
	if unbound.State != desktop.Failed || unbound.Preflight != nil {
		t.Fatalf("an unbindable credential reference was preflighted: %+v", unbound)
	}
	if executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: credentialed, Spec: "reschedule.json", Output: "job-001", Expected: strings.Repeat("0", 64)}); executed.State == desktop.Completed {
		t.Fatalf("a credentialed send executed against a broken reference: %+v", executed)
	}
	if _, err := os.Lstat(filepath.Join(credentialed, "job-001")); !os.IsNotExist(err) {
		t.Fatal("a refused configuration left a run folder behind")
	}
}
