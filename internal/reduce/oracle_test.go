package reduce_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The fixture receiver below rejects the rescheduling message, and only after
// the booking that created the appointment reached it in the same session. That
// is the defect a reduction has to keep: one message, and the setup dependency
// it needs to fail at all.
const (
	bookingControl    = "LISTEN-BOOK"
	reschedulControl  = "LISTEN-MOVE"
	chosenAssertion   = "reschedule-accepted"
	environmentName   = "lab-siu"
	rejectedByFixture = "AE"
)

// receiver accepts one connection per trial and answers every message it is
// framed, so a reduction can spend a real budget of durable runs against it.
type receiver struct {
	listener net.Listener
	mu       sync.Mutex
	sessions int
}

func listen(t *testing.T) *receiver {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &receiver{listener: listener}
	go r.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return r
}

func (r *receiver) serve() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return
		}
		r.mu.Lock()
		r.sessions++
		r.mu.Unlock()
		go r.session(conn)
	}
}

// session answers one run. The rejection is stateful on purpose: the fixture
// only refuses the rescheduling message when the booking preceded it here.
func (r *receiver) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
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
		control := controlID(frame)
		code := "AA"
		switch control {
		case bookingControl:
			booked = true
		case reschedulControl:
			if booked {
				code = rejectedByFixture
			}
		}
		ack := "MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|FIXTURE-ACK|P|2.5.1\rMSA|" + code + "|" + control + "\r"
		if _, err := conn.Write(mllp.Frame([]byte(ack))); err != nil {
			return
		}
	}
}

// controlID reads MSH-10 out of one framed message. The fixture answers what it
// was sent, which is what makes the acknowledgement match the message.
func controlID(frame []byte) string {
	header, _, _ := bytes.Cut(frame, []byte("\r"))
	fields := strings.Split(string(header), "|")
	if len(fields) < 10 {
		return ""
	}
	return fields[9]
}

func (r *receiver) address() string { return r.listener.Addr().String() }

func (r *receiver) connections() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func nonproductionTarget(address string) replay.Target {
	return replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Name: environmentName,
		Classification: replay.Nonproduction, Address: address, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "3s", MaxACKBytes: 4096,
	}
}

// ackSpec declares the whole four-message sequence and one acknowledgement
// expectation per message. The rescheduling expectation is the failure a
// reduction is asked to keep.
func ackSpec(t *testing.T, directory, casePath string) string {
	t.Helper()
	accepted := "AA"
	assertions := make([]testrunner.Assertion, 0, len(sequence))
	for i, id := range sequence {
		name := []string{"booking-accepted", chosenAssertion, "admit-accepted", "other-accepted"}[i]
		assertions = append(assertions, testrunner.Assertion{
			ID: name, Operator: "ack_field_equals", Message: id, Selector: "MSA-1",
			Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}},
		})
	}
	spec := testrunner.Spec{
		Schema: testrunner.SpecSchema, Name: "Rescheduling is acknowledged",
		Input:  testrunner.Input{Case: casePath, Messages: slices.Clone(sequence)},
		Target: filepath.Join(directory, "target.json"),
		Setup: testrunner.Setup{
			InitialState:      testrunner.OperatorDeclared,
			ResetInstructions: "Restart the fixture receiver and confirm the reset action by name.",
		},
		Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary},
		Assertions:  assertions,
	}
	path := filepath.Join(directory, "spec.json")
	writeJSON(t, path, spec)
	return path
}

// resetRequest is the explicitly selected reset every trial runs first. It
// performs nothing: a person confirms the named action, which is exactly what
// operator-assisted means, and no trial runs without that confirmation.
func resetRequest(t *testing.T, directory, address string, confirm bool) fixturereset.Request {
	t.Helper()
	document := `{"schema":"readmit-reset-plan/v1","environment":"` + environmentName +
		`","actions":[{"id":"restart-listener","operator":"operator_confirms","authority":"none",` +
		`"instructions":"Restart the fixture receiver on an empty ledger."}]}`
	request := fixturereset.Request{
		Target: nonproductionTarget(address), PlanBytes: []byte(document), PlanDirectory: directory,
	}
	if confirm {
		request.Confirmed = []string{"restart-listener"}
	}
	return request
}

func durableSetup(t *testing.T, confirm bool) (reduce.Request, *reduce.DurableOracle, *receiver) {
	t.Helper()
	casePath, identity := fourMessages(t)
	directory := t.TempDir()
	fixture := listen(t)
	writeJSON(t, filepath.Join(directory, "target.json"), nonproductionTarget(fixture.address()))
	specPath := ackSpec(t, directory, casePath)

	declared := plan(identity, 64, 1)
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: filepath.Join(directory, "reduction"),
		Signature: declared.Signature, Reset: resetRequest(t, directory, fixture.address(), confirm),
	})
	if err != nil {
		t.Fatal(err)
	}
	return reduce.Request{
		Case: casePath, Plan: declared, Messages: oracle.Messages(), Required: oracle.Required(),
	}, oracle, fixture
}

// TestReducingThroughDurableRunsRetainsTheSetupDependency is the delivery end
// to end: real durable runs against a real fixture receiver, one confirmed
// reset before each of them, and a bounded budget. The four-message sequence
// reduces to the rescheduling message and the booking it needs, and the same
// chosen assertion fails before and after.
func TestReducingThroughDurableRunsRetainsTheSetupDependency(t *testing.T) {
	request, oracle, fixture := durableSetup(t, true)

	report, err := reduce.Run(t.Context(), request, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != reduce.OutcomeReduced || report.Minimality != reduce.GroupOneMinimal {
		t.Fatalf("a reduction that ran to a fixpoint claims 1-minimality over its grouping: %s %s (%s)",
			report.Outcome, report.Minimality, report.Reason)
	}
	if !slices.Equal(report.Retained, sequence[:2]) {
		t.Fatalf("the rescheduling message and the booking it depends on are retained: %v", report.Retained)
	}
	if !slices.Equal(report.Removed, sequence[2:]) {
		t.Fatalf("the messages the failure did not need are removed: %v", report.Removed)
	}
	// The same assertion failed before and after, and nothing else did.
	first, last := report.Trials[0], report.Trials[len(report.Trials)-1]
	for _, trial := range []reduce.Trial{first, last} {
		if trial.State != durablerun.AssertionFailed || !slices.Equal(trial.Failed, []string{chosenAssertion}) {
			t.Fatalf("the chosen failure is the one that survived: %+v", trial)
		}
	}
	if first.Purpose != reduce.Calibration || last.Purpose != reduce.Confirmation {
		t.Fatalf("a reduction calibrates first and confirms last: %s then %s", first.Purpose, last.Purpose)
	}
	if fixture.connections() != len(report.Trials) {
		t.Fatalf("each of %d trials is one durable run against the fixture: %d connections",
			len(report.Trials), fixture.connections())
	}
	for _, trial := range report.Trials {
		if trial.Reset != fixturereset.Confirmed {
			t.Fatalf("every trial resets first: %+v", trial)
		}
	}
	// The removal trials really did stop reproducing when the booking left.
	if !slices.ContainsFunc(report.Trials, func(trial reduce.Trial) bool {
		return trial.Purpose == reduce.Removal && trial.Removed == report.Groups[0].ID && trial.Verdict == reduce.NotReproduced
	}) {
		t.Fatal("removing the setup dependency must lose the failure, otherwise it was not a dependency")
	}
}

// TestAnUnconfirmedResetSendsNothing is the reset strategy at the live boundary:
// a reset nobody confirmed stops the reduction before a single message is sent.
func TestAnUnconfirmedResetSendsNothing(t *testing.T) {
	request, oracle, fixture := durableSetup(t, false)

	report, err := reduce.Run(t.Context(), request, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.ResetNotConfirmed {
		t.Fatalf("an unconfirmed reset stops the reduction: %s (%s)", report.Outcome, report.Reason)
	}
	if fixture.connections() != 0 {
		t.Fatalf("nothing was sent to the fixture: %d connections", fixture.connections())
	}
	if report.Trials[0].ResetReason != fixturereset.AwaitingOperator {
		t.Fatalf("the trial records what the reset was waiting for: %+v", report.Trials[0])
	}
}

// TestStoppingALiveReductionLeavesNoVerdict is the cancel path against real
// runs: an interrupted reduction claims nothing, and the trials it did spend
// remain readable durable runs rather than being resumed or repeated.
func TestStoppingALiveReductionLeavesNoVerdict(t *testing.T) {
	request, oracle, _ := durableSetup(t, true)
	ctx, stop := context.WithCancel(t.Context())
	stop()

	report, err := reduce.Run(ctx, request, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != reduce.OutcomeUndecided || report.Reason != reduce.OperatorStopped {
		t.Fatalf("an interrupted reduction is undecided: %s (%s)", report.Outcome, report.Reason)
	}
	if len(report.Trials) != 0 || report.Minimality != reduce.NoClaim {
		t.Fatalf("a reduction stopped before its first trial claims nothing: %+v", report.Summary)
	}
}

// TestADurableOracleRefusesWhatItCannotAnswer keeps a budget from being spent
// establishing that an assertion nobody declared never fails, and keeps one
// reduction's working files out of an existing directory.
func TestADurableOracleRefusesWhatItCannotAnswer(t *testing.T) {
	casePath, _ := fourMessages(t)
	directory := t.TempDir()
	fixture := listen(t)
	writeJSON(t, filepath.Join(directory, "target.json"), nonproductionTarget(fixture.address()))
	specPath := ackSpec(t, directory, casePath)
	reset := resetRequest(t, directory, fixture.address(), true)

	absent := reduce.Signature{State: durablerun.AssertionFailed, Assertions: []string{"no-such-assertion"}}
	if _, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: filepath.Join(directory, "a"), Signature: absent, Reset: reset,
	}); err == nil {
		t.Fatal("an oracle accepted a signature its spec cannot ever produce")
	}
	chosen := reduce.Signature{State: durablerun.AssertionFailed, Assertions: []string{chosenAssertion}}
	if _, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: directory, Signature: chosen, Reset: reset,
	}); err == nil {
		t.Fatal("an oracle wrote its trials into a directory that already existed")
	}
	if _, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: filepath.Join(directory, "absent.json"), Workspace: filepath.Join(directory, "b"),
		Signature: chosen, Reset: reset,
	}); err == nil {
		t.Fatal("an oracle read a spec that is not there")
	}
	// The one that is well formed knows which occurrence its signature is about.
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: filepath.Join(directory, "c"), Signature: chosen, Reset: reset,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(oracle.Required(), []string{sequence[1]}) {
		t.Fatalf("the occurrence the signature is stated about is pinned: %v", oracle.Required())
	}
	if !slices.Equal(oracle.Messages(), sequence) {
		t.Fatalf("the unreduced sequence is the spec's own: %v", oracle.Messages())
	}
}
