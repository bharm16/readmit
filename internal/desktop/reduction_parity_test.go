package desktop_test

// The controlled reduction panel's two operations, held to the engine
// internal/reduce runs for everyone. There is no `readmit reduce` command, so
// the shared operation is the package itself: a preview is the partition
// reduce.PreviewPlan reads over the same documents, and a run is the report
// reduce.Run reaches with a durable oracle built over the same spec, target
// and reset plan against the same receiver. Every trial resets first under the
// reset plan's own authorization, a reset nobody authorised stops the
// reduction before anything is sent, a person who stops a reduction mid-trial
// is answered cancelled and nothing is resent, a budget spent part way stays
// a claim of nothing, and a refused reduction leaves no working folder.

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A booking, the reschedule that moves it and an unrelated update, sent on one
// connection. The receiver below refuses the reschedule only after the booking
// reached it on the same connection, so the failure a reduction holds needs
// the booking and does not need the update. Every value is synthetic.
const (
	rdBooking    = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|RD-BOOK|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT\rPID|1||RD-MRN-1^^^READMIT^MR||SYNTHETIC^ONLY\r"
	rdReschedule = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|RD-MOVE|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT\rPID|1||RD-MRN-1^^^READMIT^MR||SYNTHETIC^ONLY\r"
	rdUpdate     = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120200||ADT^A08|RD-UPDATE|P|2.5.1\rPID|1||RD-MRN-2^^^READMIT^MR||SYNTHETIC^ONLY\r"

	rdEnvironment = "lab-siu"
	rdAction      = "restart-listener"
	rdAssertion   = "reschedule-accepted"
)

// rdSequence is the three occurrences the case holds, in the order it holds
// them.
var rdSequence = []string{"s0001-e000001", "s0001-e000002", "s0001-e000003"}

// reductionReceiver is the independent synthetic target every trial really
// sends to. It answers each frame on the connection it arrived on, holds an
// acknowledgement back while told to, and counts what it received.
type reductionReceiver struct {
	listener net.Listener
	mu       sync.Mutex
	frames   int
	sessions int
	holding  bool
	release  chan struct{}
}

func newReductionReceiver(t *testing.T) *reductionReceiver {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &reductionReceiver{listener: listener, release: make(chan struct{})}
	t.Cleanup(func() {
		r.releaseAcknowledgement()
		_ = listener.Close()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			r.sessions++
			r.mu.Unlock()
			go r.session(conn)
		}
	}()
	return r
}

func (r *reductionReceiver) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	reader, err := mllp.NewReader(conn, 1<<20)
	if err != nil {
		return
	}
	booked := false
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			return
		}
		header, _, _ := bytes.Cut(frame, []byte("\r"))
		fields := strings.Split(string(header), "|")
		control := ""
		if len(fields) > 9 {
			control = fields[9]
		}
		r.mu.Lock()
		r.frames++
		holding, release := r.holding, r.release
		r.mu.Unlock()
		if holding {
			<-release
		}
		code := "AA"
		switch control {
		case "RD-BOOK":
			booked = true
		case "RD-MOVE":
			if booked {
				code = "AE"
			}
		}
		ack := "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120000||ACK|ACK-" + control + "|P|2.5.1\rMSA|" + code + "|" + control + "\r"
		if _, err := conn.Write(mllp.Frame([]byte(ack))); err != nil {
			return
		}
	}
}

func (r *reductionReceiver) address() string { return r.listener.Addr().String() }

func (r *reductionReceiver) received() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frames
}

func (r *reductionReceiver) connections() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions
}

// holdAcknowledgements withholds every acknowledgement from now on, as a
// stalled system does, until releaseAcknowledgement.
func (r *reductionReceiver) holdAcknowledgements() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holding = true
}

func (r *reductionReceiver) releaseAcknowledgement() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holding {
		r.holding = false
		close(r.release)
		r.release = make(chan struct{})
	}
}

// reductionWorkspace is one open workspace holding the three-message case,
// the nonproduction environment the receiver is recorded as, the regression
// test whose reschedule expectation fails there, and the reviewed reset plan
// whose one action a person performs and confirms. It returns the root and
// the identity the window verified.
func reductionWorkspace(t *testing.T, app *desktop.App, address string) (string, string) {
	t.Helper()
	root := resolved(t, t.TempDir())
	writeCase(t, root, "incident", framed(rdBooking)+framed(rdReschedule)+framed(rdUpdate))
	target, err := json.Marshal(replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Name: rdEnvironment,
		Classification: replay.Nonproduction, Address: address, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "10s", MaxACKBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "target.json", string(target))
	accepted := "AA"
	assertions := make([]testrunner.Assertion, 0, len(rdSequence))
	for i, occurrence := range rdSequence {
		assertions = append(assertions, testrunner.Assertion{
			ID: []string{"booking-accepted", rdAssertion, "update-accepted"}[i], Operator: "ack_field_equals",
			Message: occurrence, Selector: "MSA-1",
			Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7Present, Text: &accepted}},
		})
	}
	spec, err := json.Marshal(testrunner.Spec{
		Schema: testrunner.SpecSchema, Name: "Rescheduling is acknowledged",
		Input:  testrunner.Input{Case: "incident", Messages: slices.Clone(rdSequence)},
		Target: "target.json",
		Setup: testrunner.Setup{InitialState: testrunner.OperatorDeclared,
			ResetInstructions: "Restart the receiver on an empty ledger and confirm the reset action by name."},
		Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary},
		Assertions:  assertions,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "spec.json", string(spec))
	writeDocument(t, root, "reset.json", `{"schema":"readmit-reset-plan/v1","environment":"`+rdEnvironment+
		`","actions":[{"id":"`+rdAction+`","operator":"operator_confirms","authority":"none",`+
		`"instructions":"Restart the receiver on an empty ledger."}]}`)
	opened := app.OpenCase(root, "incident")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the case a reduction reduces: %+v", opened)
	}
	return root, opened.Case.Identity
}

// reductionRequest is what the panel sends: the case, the test whose
// reschedule failure is held, per-occurrence grouping, the budget, the
// environment and reset plan every trial uses with its one action confirmed,
// and a new working folder.
func reductionRequest(root, identity string, trials int) desktop.ReductionRequest {
	return desktop.ReductionRequest{
		Workspace: root, Case: "incident", Identity: identity, Spec: "spec.json",
		Grouping: reduce.GroupPerOccurrence, Assertions: []string{rdAssertion},
		Trials: trials, Confirmations: 1, ResetPlan: "reset.json", Target: "target.json",
		Confirmed: []string{rdAction}, Work: "trials",
	}
}

// engineReduction is the same reduction asked of internal/reduce directly:
// the plan the request declares, and a durable oracle over the same spec,
// environment and reset plan whose trials go into a folder of the test's own.
func engineReduction(t *testing.T, root string, request desktop.ReductionRequest) (reduce.Request, *reduce.DurableOracle) {
	t.Helper()
	target, err := operation.ReadTarget(filepath.Join(root, request.Target))
	if err != nil {
		t.Fatal(err)
	}
	resetPlan, err := os.ReadFile(filepath.Join(root, request.ResetPlan))
	if err != nil {
		t.Fatal(err)
	}
	plan := reduce.Plan{
		Schema: reduce.PlanSchema, Case: request.Identity, Grouping: request.Grouping,
		Signature: reduce.Signature{State: durablerun.AssertionFailed, Assertions: request.Assertions},
		Trials:    request.Trials, Confirmations: request.Confirmations,
	}
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: filepath.Join(root, request.Spec), Workspace: filepath.Join(t.TempDir(), "trials"),
		Signature: plan.Signature, Resolve: sendpolicy.SystemResolver,
		Reset: fixturereset.Request{Target: target, PlanBytes: resetPlan, PlanDirectory: root, Confirmed: request.Confirmed},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reduce.Request{
		Case: filepath.Join(root, request.Case), Plan: plan,
		Messages: oracle.Messages(), Required: oracle.Required(),
	}, oracle
}

// sameDocument fails the test unless the two values encode to the same JSON.
func sameDocument(t *testing.T, what string, window, engine any) {
	t.Helper()
	got, err := json.Marshal(window, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(engine, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the window's %s is not the engine's:\nwindow %s\nengine %s", what, got, want)
	}
}

// started runs one reduction in the window and fails the test unless it
// answered with a report.
func started(t *testing.T, app *desktop.App, request desktop.ReductionRequest) desktop.ReductionResult {
	t.Helper()
	result := app.StartReduction(request)
	if result.Reduction == nil || result.Reduction.Report == nil {
		t.Fatalf("the reduction reported nothing: %+v", result)
	}
	return result
}

// A preview is the partition the engine reads over the same documents, and it
// is only a read: the receiver sees nothing, and the workspace — including an
// entry of the person's own under the name the preview once used as scratch —
// is exactly as it was.
func TestAReductionPreviewIsThePartitionTheEngineReadsAndWritesNothing(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())
	if err := os.Mkdir(filepath.Join(root, ".readmit-reduction-preview"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, ".readmit-reduction-preview/notes.txt", "the person's own notes")
	before := bytesUnder(t, root)
	request := reductionRequest(root, identity, 16)

	previewed := app.PreviewReduction(request)
	if previewed.State != desktop.Completed || previewed.Reduction == nil || previewed.Reduction.Preview == nil {
		t.Fatalf("the preview was refused: %+v", previewed)
	}
	engine, _ := engineReduction(t, root, request)
	want, err := reduce.PreviewPlan(engine)
	if err != nil {
		t.Fatal(err)
	}
	sameDocument(t, "preview", previewed.Reduction.Preview, want)
	// The reschedule is what the signature is stated about, so its group is
	// pinned and the other two are the removal candidates.
	groups := previewed.Reduction.Preview.Groups
	if len(groups) != 3 || !groups[1].Required || groups[0].Required || groups[2].Required {
		t.Fatalf("the reschedule's group is the one the signature pins: %+v", groups)
	}
	if receiver.connections() != 0 {
		t.Fatalf("a preview reached the receiver: %d connections", receiver.connections())
	}
	if after := bytesUnder(t, root); !maps.EqualFunc(before, after, bytes.Equal) {
		t.Fatalf("a preview changed the workspace: %v", entriesOf(t, root))
	}
}

// A run in the window is the reduction the engine reaches: the same trials,
// the same verdicts, the same retained and removed occurrences and the same
// claim, one durable run per trial. The failure keeps the booking it depends
// on and loses the unrelated update, and the result claims only
// 1-minimality over the declared grouping.
func TestAReductionRunInTheWindowReachesTheReportTheEngineReaches(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())
	request := reductionRequest(root, identity, 16)

	result := started(t, app, request)
	if result.State != desktop.Completed {
		t.Fatalf("a reduction that reached a fixpoint is completed: %+v", result)
	}
	report := result.Reduction.Report
	if report.Outcome != reduce.OutcomeReduced || report.Minimality != reduce.GroupOneMinimal ||
		!slices.Equal(report.Retained, rdSequence[:2]) || !slices.Equal(report.Removed, rdSequence[2:]) {
		t.Fatalf("the reschedule keeps its booking and loses the update: %s %s retained %v removed %v",
			report.Outcome, report.Minimality, report.Retained, report.Removed)
	}
	sent := receiver.connections()
	if sent != len(report.Trials) {
		t.Fatalf("each of %d trials is one durable run against the receiver: %d connections", len(report.Trials), sent)
	}
	for _, trial := range report.Trials {
		if trial.Reset != fixturereset.Confirmed || trial.ResetReason != fixturereset.EveryActionConfirmed {
			t.Fatalf("every trial resets first under the confirmed action: %+v", trial)
		}
		if _, err := os.Stat(filepath.Join(root, "trials", fmt.Sprintf("t%04d", trial.Index), "journal.jsonl")); err != nil {
			t.Fatalf("trial %d left no durable run in the working folder: %v", trial.Index, err)
		}
	}

	engine, oracle := engineReduction(t, root, request)
	want, err := reduce.Run(t.Context(), engine, oracle)
	if err != nil {
		t.Fatal(err)
	}
	sameDocument(t, "report", report, want)
	if receiver.connections() != 2*sent {
		t.Fatalf("the engine's own run spent the same trials: %d connections after %d", receiver.connections(), sent)
	}
}

// A reset nobody authorised stops the reduction before its first send. A
// confirmation withheld leaves the reset unconfirmed; a confirmation naming
// something the plan does not ask a person to do is refused. Either way the
// reduction establishes nothing, the receiver sees nothing, the working
// folder no trial ever wrote into is not left behind, and the engine answers
// the same.
func TestAReductionWhoseResetIsNotAuthorisedSendsNothingAndLeavesNoWorkingFolder(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())

	for _, authorisation := range []struct {
		name      string
		confirmed []string
		outcome   fixturereset.Outcome
		reason    fixturereset.Reason
	}{
		{"withheld", nil, fixturereset.Unconfirmed, fixturereset.AwaitingOperator},
		{"naming no action a person performs", []string{"send-anything"}, fixturereset.Refused, fixturereset.ApprovalNamesMachineAction},
	} {
		request := reductionRequest(root, identity, 16)
		request.Confirmed = authorisation.confirmed
		result := started(t, app, request)
		report := result.Reduction.Report
		if result.State != desktop.Completed || report.Outcome != reduce.OutcomeUndecided ||
			report.Reason != reduce.ResetNotConfirmed || report.Minimality != reduce.NoClaim {
			t.Fatalf("a reset %s stops the reduction undecided: %+v", authorisation.name, report)
		}
		if len(report.Trials) != 1 || report.Trials[0].Reset != authorisation.outcome ||
			report.Trials[0].ResetReason != authorisation.reason || report.Trials[0].State != "" {
			t.Fatalf("a reset %s is recorded as it was and runs nothing: %+v", authorisation.name, report.Trials)
		}
		if !slices.Equal(report.Retained, rdSequence) || len(report.Removed) != 0 {
			t.Fatalf("a reduction that established nothing removed something: %+v", report)
		}
		if result.Reduction.Work != "" {
			t.Fatalf("the window names a working folder it removed: %q", result.Reduction.Work)
		}
		if _, err := os.Lstat(filepath.Join(root, "trials")); !os.IsNotExist(err) {
			t.Fatalf("a reset %s left the working folder behind: %v", authorisation.name, err)
		}
		engine, oracle := engineReduction(t, root, request)
		want, err := reduce.Run(t.Context(), engine, oracle)
		if err != nil {
			t.Fatal(err)
		}
		sameDocument(t, "report", report, want)
	}
	if receiver.connections() != 0 {
		t.Fatalf("an unauthorised reset let a trial reach the receiver: %d connections", receiver.connections())
	}
}

// Stopping a reduction while a trial waits on its acknowledgement answers
// cancelled, whatever that trial then recorded. The trial the stop
// interrupted is an uncertain delivery, nothing more is sent, and the
// command line recovers that trial's durable run as uncertain and never safe
// to repeat.
func TestAStoppedReductionIsCancelledAndItsInterruptedTrialIsNeverResent(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())
	receiver.holdAcknowledgements()

	answered := make(chan desktop.ReductionResult, 1)
	go func() { answered <- app.StartReduction(reductionRequest(root, identity, 16)) }()
	deadline := time.Now().Add(20 * time.Second)
	for receiver.received() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first trial never reached the receiver")
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.Cancel("reduction")
	var result desktop.ReductionResult
	select {
	case result = <-answered:
	case <-time.After(20 * time.Second):
		t.Fatal("a stopped reduction never answered")
	}
	if result.State != desktop.Cancelled || result.Reduction == nil || result.Reduction.Report == nil {
		t.Fatalf("a reduction the person stopped is cancelled: %+v", result)
	}
	report := result.Reduction.Report
	if report.Outcome != reduce.OutcomeUndecided || report.Minimality != reduce.NoClaim || len(report.Removed) != 0 {
		t.Fatalf("a stopped reduction claims nothing: %+v", report)
	}
	if len(report.Trials) != 1 || report.Trials[0].Verdict != reduce.Undecided || report.Trials[0].Reason == "" {
		t.Fatalf("the interrupted trial is undecided: %+v", report.Trials)
	}
	receiver.releaseAcknowledgement()
	time.Sleep(200 * time.Millisecond)
	if receiver.received() != 1 || receiver.connections() != 1 {
		t.Fatalf("a stopped reduction sent again: %d frames on %d connections", receiver.received(), receiver.connections())
	}

	var stdout, stderr bytes.Buffer
	status := commandStatus(t, cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "run", "status",
		filepath.Join(root, "trials", "t0001"), "--recovery", "--json"}, &stdout, &stderr))
	if status != 2 {
		t.Fatalf("the command line reads the interrupted trial as settled: %d %s %s", status, stdout.String(), stderr.String())
	}
	var recovery struct {
		Run struct {
			DeliveryUncertain bool `json:"delivery_uncertain"`
		} `json:"run"`
		SafeToRepeat bool `json:"safe_to_repeat"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &recovery); err != nil {
		t.Fatalf("run status: %v %s", err, stdout.String())
	}
	if !recovery.Run.DeliveryUncertain || recovery.SafeToRepeat {
		t.Fatalf("the interrupted trial is uncertain and never safe to repeat: %s", stdout.String())
	}
	// Reading it back sent nothing either.
	if receiver.received() != 1 {
		t.Fatalf("recovery sent again: %d frames", receiver.received())
	}
}

// A budget spent after a removal was committed leaves a smaller sequence that
// still reproduced the last time it was asked, and nothing more: the report
// is bounded, claims no minimality and names the budget, and the engine
// reaches the same report.
func TestABoundedReductionIsReportedAsTheIncompleteSearchItWas(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())
	// One calibration, the booking tried and kept, the update tried and
	// removed: the fourth trial the restarted pass needs is not in the budget.
	request := reductionRequest(root, identity, 3)

	result := started(t, app, request)
	report := result.Reduction.Report
	if result.State != desktop.Completed || report.Outcome != reduce.OutcomeBounded ||
		report.Minimality != reduce.NoClaim || report.Reason != reduce.BudgetSpent {
		t.Fatalf("a budget spent part way is bounded and claims nothing: %+v", report)
	}
	if !slices.Equal(report.Retained, rdSequence[:2]) || !slices.Equal(report.Removed, rdSequence[2:]) {
		t.Fatalf("the committed removal is what a bounded result holds: %+v", report)
	}
	engine, oracle := engineReduction(t, root, request)
	want, err := reduce.Run(t.Context(), engine, oracle)
	if err != nil {
		t.Fatal(err)
	}
	sameDocument(t, "report", report, want)
}

// A reduction the engine or the license refuses sends nothing and leaves no
// working folder, so the same name is free for the next attempt.
func TestARefusedReductionSendsNothingAndLeavesNoWorkingFolder(t *testing.T) {
	receiver := newReductionReceiver(t)
	app := workspaceApp(t)
	root, identity := reductionWorkspace(t, app, receiver.address())

	for _, refusal := range []struct {
		change func(*desktop.ReductionRequest)
		reason string
	}{
		{func(r *desktop.ReductionRequest) { r.Trials = 0 }, "a reduction spends between 1 and 512 trials"},
		{func(r *desktop.ReductionRequest) { r.Confirmations = 9 }, "a reduction confirms its oracle between 1 and 8 times"},
		{func(r *desktop.ReductionRequest) { r.Assertions = []string{"no-such-assertion"} },
			"this signature names an assertion the test spec does not declare, so no sequence can reproduce it"},
	} {
		request := reductionRequest(root, identity, 16)
		refusal.change(&request)
		result := app.StartReduction(request)
		if result.State != desktop.Failed || result.Reason != refusal.reason {
			t.Fatalf("the engine's refusal is the window's: %+v, want %q", result, refusal.reason)
		}
		if _, err := os.Lstat(filepath.Join(root, "trials")); !os.IsNotExist(err) {
			t.Fatalf("a refused reduction left its working folder: %v", err)
		}
	}

	// Execution is admitted by the license: an activation that admits only
	// authoring refuses the reduction before anything is read or created.
	authorOnly := windowWith(t, authorOnlyPolicy(t))
	if opened := authorOnly.OpenCase(root, "incident"); opened.State != desktop.Completed {
		t.Fatalf("open the case: %+v", opened)
	}
	if denied := authorOnly.StartReduction(reductionRequest(root, identity, 16)); denied.State != desktop.PermissionDenied || denied.Reduction != nil {
		t.Fatalf("an activation without execution admitted a reduction: %+v", denied)
	}
	if _, err := os.Lstat(filepath.Join(root, "trials")); !os.IsNotExist(err) {
		t.Fatalf("a refused reduction left its working folder: %v", err)
	}
	if receiver.connections() != 0 {
		t.Fatalf("a refused reduction reached the receiver: %d connections", receiver.connections())
	}

	// The name is free: the same request, corrected, runs.
	if result := started(t, app, reductionRequest(root, identity, 16)); result.State != desktop.Completed {
		t.Fatalf("the corrected reduction did not run: %+v", result)
	}
}
