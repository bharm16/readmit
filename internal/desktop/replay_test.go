package desktop_test

// The replay screen previews selected messages of the verified case and sends
// them once, only after the person approved that exact preview. Every send
// here reaches only a loopback receiver the test starts, which answers each
// message on the one connection a replay opens and records what it was sent,
// so what a refusal kept from leaving is counted rather than assumed.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testlicense"
)

// replayReceiver is a loopback MLLP receiver that answers every message on a
// connection with an accepting acknowledgement correlated to that message's
// own control ID, as a downstream system does. While holding, it records a
// message and keeps its acknowledgement back until released.
type replayReceiver struct {
	address     string
	mu          sync.Mutex
	payloads    []string
	connections int
	holding     bool
	release     chan struct{}
}

func newReplayReceiver(t *testing.T) *replayReceiver {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	receiver := &replayReceiver{address: listener.Addr().String(), release: make(chan struct{})}
	t.Cleanup(func() {
		listener.Close()
		receiver.mu.Lock()
		if receiver.holding {
			receiver.holding = false
			close(receiver.release)
		}
		receiver.mu.Unlock()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			receiver.mu.Lock()
			receiver.connections++
			receiver.mu.Unlock()
			go receiver.serve(conn)
		}
	}()
	return receiver
}

func (r *replayReceiver) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader, _ := mllp.NewReader(conn, 1<<20)
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			return
		}
		payload := string(frame[1 : len(frame)-2])
		r.mu.Lock()
		r.payloads = append(r.payloads, payload)
		holding, release := r.holding, r.release
		r.mu.Unlock()
		if holding {
			<-release
		}
		control := ""
		if header, _, _ := strings.Cut(payload, "\r"); header != "" {
			if fields := strings.Split(header, "|"); len(fields) > 9 {
				control = fields[9]
			}
		}
		if _, err := fmt.Fprintf(conn, "\x0bMSH|^~\\&|RECEIVER|LAB|READMIT|SYNTHETIC|20260101120000||ACK|ACK-%s|P|2.5.1\rMSA|AA|%s\r\x1c\r", control, control); err != nil {
			return
		}
	}
}

// hold keeps back the acknowledgement of every message received from now on.
func (r *replayReceiver) hold() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holding, r.release = true, make(chan struct{})
}

// releaseHeld sends every acknowledgement held back and stops holding.
func (r *replayReceiver) releaseHeld() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holding {
		r.holding = false
		close(r.release)
	}
}

// received is every message payload that arrived, in order.
func (r *replayReceiver) received() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.payloads)
}

// reached is how many connections were opened to the receiver.
func (r *replayReceiver) reached() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connections
}

// replayTarget is a readmit-target/v3 configuration of a named environment at
// address, with the class a person recorded for it.
func replayTarget(address, classification string) string {
	return fmt.Sprintf(`{"schema":"readmit-target/v3","test_endpoint":true,"address":%q,"transport":"plain","approved_transport":false,`+
		`"connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536,"name":"lab-replay","classification":%q}`, address, classification)
}

// replayWorkspace is an open workspace holding a verified case of a synthetic
// booking and its reschedule, a target configuration at address and a send
// policy approving only loopback's one address. It returns the workspace and
// the identity the window verified the case under.
func replayWorkspace(t *testing.T, app *desktop.App, address string) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	writeCase(t, workspace, "case", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	writeDocument(t, workspace, "target.json", replayTarget(address, "nonproduction"))
	writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	opened := app.OpenCase(workspace, "case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the case: %+v", opened)
	}
	return workspace, opened.Case.Identity
}

// bothMessages rebased and shifted a day, against the target under the policy.
func bothMessages(workspace, identity string) desktop.ReplayRequest {
	return desktop.ReplayRequest{Workspace: workspace, Case: "case", Identity: identity, Target: "target.json", Policy: "policy.json",
		Messages:        []string{"s0001-e000001", "s0001-e000002"},
		Transformations: []replay.Transformation{{Name: "rebase-control-ids"}, {Name: "shift-timestamps", Shift: "24h"}},
		Output:          "run"}
}

// replayPreviewed previews request and fails unless the preview completed.
func replayPreviewed(t *testing.T, app *desktop.App, request desktop.ReplayRequest) *desktop.ReplayPreview {
	t.Helper()
	result := app.PreviewReplay(request)
	if result.State != desktop.Completed || result.Preview == nil {
		t.Fatalf("preview: %+v", result)
	}
	return result.Preview
}

// A preview is the plan a send would execute and nothing more: every selected
// message and the bytes it would put on the wire, every field a named
// transformation changes, the target, the fresh run folder, the decision
// file beside it and the backend's admission. It opens no connection and
// writes nothing, and the values a transformation changes appear only on
// purpose.
func TestAReplayPreviewSendsAndWritesNothingAndRevealsValuesOnlyOnPurpose(t *testing.T) {
	receiver := newReplayReceiver(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	before := bytesUnder(t, workspace)
	request := bothMessages(workspace, identity)
	request.Output = ""

	result := app.PreviewReplay(request)
	if result.State != desktop.Completed || result.Preview == nil || result.Decision == nil {
		t.Fatalf("preview: %+v", result)
	}
	preview := result.Preview
	if !preview.Sendable || preview.Refusal != "" || !preview.Admission.Admitted {
		t.Fatalf("a preview nothing refuses is not sendable: %+v", preview)
	}
	if result.Decision.Allowed || result.Decision.Reason != "send_not_explicit" || result.Decision.ExplicitSend || !result.Decision.PolicySelected {
		t.Fatalf("a preview requested a send: %+v", result.Decision)
	}
	if preview.Destination != (desktop.RunDestination{Name: "replay-001", Generated: true, Fresh: true}) || preview.DecisionFile != "replay-001.decision.json" {
		t.Fatalf("destination: %+v %q", preview.Destination, preview.DecisionFile)
	}
	if preview.Target.Name != "lab-replay" || preview.Target.Classification != "nonproduction" || preview.Target.Address != receiver.address {
		t.Fatalf("target: %+v", preview.Target)
	}
	if len(preview.Messages) != 2 || preview.Messages[0].Source != "s0001-e000001" || preview.Messages[0].Outbound != "o000001" ||
		preview.Messages[1].Source != "s0001-e000002" || preview.Messages[0].WireBytes == 0 {
		t.Fatalf("messages: %+v", preview.Messages)
	}
	var selectors []string
	for _, change := range preview.Changes {
		selectors = append(selectors, change.Source+" "+change.Transformation+" "+change.Selector)
		if change.Old != "" || change.New != "" {
			t.Errorf("a preview nobody asked to reveal carries values: %+v", change)
		}
	}
	for _, want := range []string{"s0001-e000001 rebase-control-ids MSH[1]-10[1]", "s0001-e000002 rebase-control-ids MSH[1]-10[1]",
		"s0001-e000001 shift-timestamps MSH[1]-7[1]", "s0001-e000001 shift-timestamps SCH[1]-11[1].4", "s0001-e000002 shift-timestamps SCH[1]-11[1].5"} {
		if !slices.Contains(selectors, want) {
			t.Errorf("the preview does not name the change %q: %v", want, selectors)
		}
	}
	if receiver.reached() != 0 {
		t.Fatalf("a preview reached the target %d times", receiver.reached())
	}
	if after := bytesUnder(t, workspace); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatalf("a preview wrote into the workspace: %v", entriesOf(t, workspace))
	}

	request.Reveal = true
	revealed := replayPreviewed(t, app, request)
	if !revealed.Revealed || revealed.Identity != preview.Identity {
		t.Fatalf("revealing changed what would be sent: %+v", revealed)
	}
	values := map[string]string{}
	for _, change := range revealed.Changes {
		values[change.Source+" "+change.Selector] = change.Old + " -> " + change.New
	}
	if values["s0001-e000001 MSH[1]-10[1]"] != "LISTEN-BOOK -> READMIT000001" || values["s0001-e000002 MSH[1]-10[1]"] != "LISTEN-MOVE -> READMIT000002" {
		t.Fatalf("rebased control IDs: %v", values)
	}
	if values["s0001-e000001 MSH[1]-7[1]"] != "20260101120000+0000 -> 20260102120000+0000" ||
		values["s0001-e000002 SCH[1]-11[1].4"] != "20260103110000+0000 -> 20260104110000+0000" {
		t.Fatalf("shifted timestamp: %v", values)
	}

	// A refused send retains its decision without a run, so the next name
	// proposed is one free for both.
	writeDocument(t, workspace, "replay-001.decision.json", "{}")
	if next := replayPreviewed(t, app, bothMessages(workspace, identity)); next.Destination.Name != "run" {
		t.Fatalf("a named output was replaced: %+v", next.Destination)
	}
	request.Output, request.Reveal = "", false
	if next := replayPreviewed(t, app, request); next.Destination.Name != "replay-002" {
		t.Fatalf("the proposed run folder collides with a retained decision: %+v", next.Destination)
	}
	request.Output = "replay-001"
	if taken := replayPreviewed(t, app, request); taken.Sendable || taken.Destination.Fresh || !strings.Contains(taken.Refusal, "already exists") {
		t.Fatalf("a taken run folder is offered for a send: %+v", taken)
	}
}

// A send happens only after the person approved the exact preview: without
// approval, without the preview's identity, into a folder a preview did not
// name, or after anything the preview identified changed, nothing is
// admitted, decided, written or sent.
func TestAReplaySendIsRefusedWithoutApprovalOrTheExactPreview(t *testing.T) {
	receiver := newReplayReceiver(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	request := bothMessages(workspace, identity)
	preview := replayPreviewed(t, app, request)
	listed := entriesOf(t, workspace)

	refused := map[string]desktop.ReplaySendRequest{
		"not approved":    {Replay: request, Expected: preview.Identity},
		"never previewed": {Replay: request, Approved: true},
		"no run folder":   {Replay: func() desktop.ReplayRequest { r := request; r.Output = ""; return r }(), Expected: preview.Identity, Approved: true},
		"other messages":  {Replay: func() desktop.ReplayRequest { r := request; r.Messages = []string{"s0001-e000001"}; return r }(), Expected: preview.Identity, Approved: true},
		"no rebasing":     {Replay: func() desktop.ReplayRequest { r := request; r.Transformations = r.Transformations[1:]; return r }(), Expected: preview.Identity, Approved: true},
		"another shift": {Replay: func() desktop.ReplayRequest {
			r := request
			r.Transformations = []replay.Transformation{{Name: "rebase-control-ids"}, {Name: "shift-timestamps", Shift: "48h"}}
			return r
		}(), Expected: preview.Identity, Approved: true},
		"no policy":        {Replay: func() desktop.ReplayRequest { r := request; r.Policy = ""; return r }(), Expected: preview.Identity, Approved: true},
		"another identity": {Replay: request, Expected: strings.Repeat("0", 64), Approved: true},
	}
	for name, send := range refused {
		result := app.SendReplay(send)
		if result.State != desktop.Failed || !strings.Contains(result.Reason, "othing was sent") || result.Run != nil {
			t.Errorf("%s: %+v", name, result)
		}
	}
	// A policy that changed after the preview is not the policy it showed.
	writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32","10.1.0.0/16"]}`)
	if result := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true}); result.State != desktop.Failed || !strings.Contains(result.Reason, "changed after its preview") {
		t.Errorf("a changed policy: %+v", result)
	}
	writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	// Neither is a target that changed, nor a case replaced under the same name.
	writeDocument(t, workspace, "target.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), `"5s"`, `"4s"`, 1))
	if result := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true}); result.State != desktop.Failed || !strings.Contains(result.Reason, "changed after its preview") {
		t.Errorf("a changed target: %+v", result)
	}
	writeDocument(t, workspace, "target.json", replayTarget(receiver.address, "nonproduction"))
	// Nor is a certificate authority replaced under the same file name.
	writeDocument(t, workspace, "ca.pem", string(newHubTestAuthority(t, "replay-lab").pem))
	writeDocument(t, workspace, "tls-target.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), `"transport":"plain"`, `"transport":"tls","ca_file":"ca.pem"`, 1))
	secured := request
	secured.Target = "tls-target.json"
	securedPreview := replayPreviewed(t, app, secured)
	writeDocument(t, workspace, "ca.pem", string(newHubTestAuthority(t, "another-lab").pem))
	if result := app.SendReplay(desktop.ReplaySendRequest{Replay: secured, Expected: securedPreview.Identity, Approved: true}); result.State != desktop.Failed || !strings.Contains(result.Reason, "changed after its preview") {
		t.Errorf("a replaced certificate authority: %+v", result)
	}
	listed = entriesOf(t, workspace)
	if err := os.RemoveAll(filepath.Join(workspace, "case")); err != nil {
		t.Fatal(err)
	}
	writeCase(t, workspace, "case", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s12.hl7")))
	if result := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true}); result.State != desktop.Failed || result.Reason != "the case identity changed; reopen the case" {
		t.Errorf("a replaced case: %+v", result)
	}
	if receiver.reached() != 0 {
		t.Fatalf("a refused send reached the target %d times", receiver.reached())
	}
	if now := entriesOf(t, workspace); !slices.Equal(now, listed) {
		t.Fatalf("a refused send wrote into the workspace: %v, was %v", now, listed)
	}
}

// Sending reserves a runner instance exactly as `readmit replay --send` does,
// and previewing needs no admission at all: an unactivated window and one
// with no runner authority preview, say a send would be refused, and are
// refused before anything is decided, written or reached.
func TestAReplaySendIsAdmittedAsExecutionAndAPreviewIsNot(t *testing.T) {
	receiver := newReplayReceiver(t)
	full := windowWith(t, testlicense.New(t))
	workspace, identity := replayWorkspace(t, full, receiver.address)
	request := bothMessages(workspace, identity)
	for policy, reason := range map[string]string{"": operationguard.ErrUnavailable.Error(), authorOnlyPolicy(t): entitlement.ErrAuthorityNotNamed.Error()} {
		window := windowWith(t, policy)
		preview := replayPreviewed(t, window, request)
		if preview.Sendable || preview.Admission.Admitted || preview.Refusal != preview.Admission.Reason {
			t.Errorf("a window without runner authority offers a send: %+v", preview)
		}
		result := window.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true})
		if result.State != desktop.PermissionDenied || result.Reason != reason {
			t.Errorf("sent without a runner instance (policy %q): %+v", filepath.Base(policy), result)
		}
	}
	if receiver.reached() != 0 || slices.Contains(entriesOf(t, workspace), "run") || slices.Contains(entriesOf(t, workspace), "run.decision.json") {
		t.Fatalf("a refused send reached the target or wrote: %d, %v", receiver.reached(), entriesOf(t, workspace))
	}
}

// Cancel names the send's own operation and stops at the message in flight:
// it is recorded as cancelled with its delivery uncertain, the next message is
// recorded as never attempted, and nothing is ever sent again — not when the
// held acknowledgement arrives, not by another panel's cancel, and not by
// sending the same approval again, which finds its run folder taken.
func TestACancelledReplayStopsAtTheMessageInFlightAndNeverSendsAgain(t *testing.T) {
	receiver := newReplayReceiver(t)
	receiver.hold()
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	request := bothMessages(workspace, identity)
	preview := replayPreviewed(t, app, request)
	send := desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true}

	answered := make(chan desktop.ReplayResult, 1)
	go func() { answered <- app.SendReplay(send) }()
	deadline := time.Now().Add(10 * time.Second)
	for len(receiver.received()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first message never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if busy := app.PreviewReplay(request); busy.State != desktop.Busy {
		t.Fatalf("a preview during a send: %+v", busy)
	}
	app.Cancel("durable-run")
	select {
	case result := <-answered:
		t.Fatalf("another operation's cancel stopped the replay: %+v", result)
	case <-time.After(200 * time.Millisecond):
	}
	app.Cancel("replay")
	result := <-answered
	if result.State != desktop.Cancelled || result.Run == nil {
		t.Fatalf("cancelled send: %+v", result)
	}
	run := result.Run
	if run.Uncertain != 1 || run.Successful || len(run.Messages) != 2 ||
		run.Messages[0].Outcome != "cancelled" || run.Messages[0].Delivery != "uncertain" ||
		run.Messages[1].Outcome != "not_attempted" || run.Messages[1].Delivery != "not_sent" {
		t.Fatalf("the retained run: %+v", run)
	}
	if run.Output != "run" || run.DecisionFile != "run.decision.json" {
		t.Fatalf("where the run was retained: %+v", run)
	}
	if _, err := replay.Open(filepath.Join(workspace, "run")); err != nil {
		t.Fatalf("the cancelled run does not verify: %v", err)
	}
	receiver.releaseHeld()
	time.Sleep(200 * time.Millisecond)
	again := app.SendReplay(send)
	if again.State != desktop.Failed || !strings.Contains(again.Reason, "already exists") {
		t.Fatalf("the same approval sent again: %+v", again)
	}
	if got := len(receiver.received()); got != 1 || receiver.reached() != 1 {
		t.Fatalf("after a cancelled send the target received %d messages over %d connections, want the one in flight", got, receiver.reached())
	}
}

// Another operation holding the slot is answered busy, and neither a preview
// nor a send reaches outside the open workspace: a case, target, policy or
// run folder named by a link or a path is refused before anything is read.
func TestAReplayIsBusyWhileTheSlotIsHeldAndReachesOnlyEntriesOfTheWorkspace(t *testing.T) {
	receiver := newReplayReceiver(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	request := bothMessages(workspace, identity)
	preview := replayPreviewed(t, app, request)

	release, held := desktop.HoldSlotForTest(app, "another")
	if !held {
		t.Fatal("the slot was not free")
	}
	if result := app.PreviewReplay(request); result.State != desktop.Busy {
		t.Errorf("preview while busy: %+v", result)
	}
	if result := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true}); result.State != desktop.Busy {
		t.Errorf("send while busy: %+v", result)
	}
	release()

	outside := t.TempDir()
	writeDocument(t, outside, "target.json", replayTarget(receiver.address, "nonproduction"))
	if err := os.Symlink(filepath.Join(outside, "target.json"), filepath.Join(workspace, "linked-target.json")); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*desktop.ReplayRequest){
		"linked target":      func(r *desktop.ReplayRequest) { r.Target = "linked-target.json" },
		"target outside":     func(r *desktop.ReplayRequest) { r.Target = "../" + filepath.Base(outside) + "/target.json" },
		"policy by path":     func(r *desktop.ReplayRequest) { r.Policy = filepath.Join(workspace, "policy.json") },
		"case by path":       func(r *desktop.ReplayRequest) { r.Case = filepath.Join(workspace, "case") },
		"run folder by path": func(r *desktop.ReplayRequest) { r.Output = "nested/run" },
	} {
		changed := request
		change(&changed)
		if result := app.PreviewReplay(changed); result.State != desktop.Failed || result.Preview != nil {
			t.Errorf("%s previewed: %+v", name, result)
		}
		if result := app.SendReplay(desktop.ReplaySendRequest{Replay: changed, Expected: preview.Identity, Approved: true}); result.State != desktop.Failed || result.Run != nil {
			t.Errorf("%s sent: %+v", name, result)
		}
	}
	if receiver.reached() != 0 {
		t.Fatalf("a refused replay reached the target %d times", receiver.reached())
	}
}

// While a replay sends, the privacy status reads the run activity active, and
// while a preview decides a send policy for a named host, the lookups it makes
// are read under the environment's send-policy evaluations; neither leaves its
// activity active afterwards.
func TestDisclosureStatusReportsAReplayActiveWhileItSendsOrResolves(t *testing.T) {
	w := &witness{}
	witnessLookups(t, w)
	app := workspaceApp(t)
	w.app.Store(app)
	workspace, identity := replayWorkspace(t, app, witnessPeer(t, w, true))
	// One message, sent as captured: the witness answers it with the
	// acknowledgement its control ID draws.
	request := desktop.ReplayRequest{Workspace: workspace, Case: "case", Identity: identity, Target: "target.json", Messages: []string{"s0001-e000001"}, Output: "run"}
	preview := replayPreviewed(t, app, request)
	w.during(t, "a replay", "run", func() {
		result := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true})
		if result.State != desktop.Completed || result.Run == nil || !result.Run.Successful {
			t.Errorf("send: %+v", result)
		}
	})
	// A named host needs its transport approved; the resolver refuses every
	// lookup, so nothing leaves this machine.
	writeDocument(t, workspace, "named-target.json", strings.Replace(replayTarget("scheduling.example.test:2575", "nonproduction"),
		`"approved_transport":false`, `"approved_transport":true`, 1))
	request.Target, request.Policy, request.Output = "named-target.json", "policy.json", ""
	w.during(t, "a replay preview", "environment", func() {
		result := app.PreviewReplay(request)
		if result.State != desktop.Completed || result.Decision == nil || result.Decision.Reason != "unresolvable_destination" || result.Preview.Sendable {
			t.Errorf("a preview whose name did not resolve: %+v", result)
		}
	})
}

// blockingLookups replaces name resolution with a resolver that refuses at
// once until block is set, and then holds each lookup until the operation that
// asked it is cancelled, signalling on started as each one begins. Nothing
// leaves this machine either way.
func blockingLookups(t *testing.T) (*atomic.Bool, chan context.Context) {
	t.Helper()
	block, started := &atomic.Bool{}, make(chan context.Context, 8)
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if !block.Load() {
			return nil, errors.New("lookup refused")
		}
		started <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	return block, started
}

// A cancellation that interrupts the name lookup a policy decision is waiting
// on is the person's cancellation, never an unresolvable destination: a
// preview answers cancelled and shows no plan, and a send answers cancelled,
// sends nothing and keeps the decision it had reached beside the run folder.
// Each cancel names its own operation.
func TestAReplayCancelledWhileItsPolicyWaitsOnALookupIsCancelledNotRefused(t *testing.T) {
	block, started := blockingLookups(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, "127.0.0.1:1")
	writeDocument(t, workspace, "named-target.json", strings.Replace(replayTarget("scheduling.example.test:2575", "nonproduction"),
		`"approved_transport":false`, `"approved_transport":true`, 1))
	request := desktop.ReplayRequest{Workspace: workspace, Case: "case", Identity: identity, Target: "named-target.json", Policy: "policy.json", Output: "run"}
	preview := replayPreviewed(t, app, request)
	if preview.Sendable {
		t.Fatalf("a name that did not resolve is offered for a send: %+v", preview)
	}

	block.Store(true)
	cancelled := func(operation string, call func() desktop.ReplayResult) desktop.ReplayResult {
		t.Helper()
		// A lookup asks for each address family, so a signal can arrive from
		// the last operation's lookup after that operation was cancelled.
		// Only a lookup that is still waiting is this one's.
		answered := make(chan desktop.ReplayResult, 1)
		go func() { answered <- call() }()
		deadline := time.After(10 * time.Second)
	waiting:
		for {
			select {
			case lookup := <-started:
				if lookup.Err() == nil {
					break waiting
				}
			case <-deadline:
				t.Fatalf("%s never asked for the name", operation)
			}
		}
		app.Cancel("durable-run")
		app.Cancel(operation)
		return <-answered
	}
	previewed := cancelled("replay-preview", func() desktop.ReplayResult { return app.PreviewReplay(request) })
	if previewed.State != desktop.Cancelled || previewed.Preview != nil {
		t.Fatalf("a cancelled preview: %+v", previewed)
	}
	sent := cancelled("replay", func() desktop.ReplayResult {
		return app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Identity, Approved: true})
	})
	if sent.State != desktop.Cancelled || sent.Run != nil || sent.Decision == nil || sent.Decision.Allowed {
		t.Fatalf("a cancelled send: %+v", sent)
	}
	if slices.Contains(entriesOf(t, workspace), "run") || !slices.Contains(entriesOf(t, workspace), "run.decision.json") {
		t.Fatalf("a send cancelled before anything was sent wrote %v", entriesOf(t, workspace))
	}
}
