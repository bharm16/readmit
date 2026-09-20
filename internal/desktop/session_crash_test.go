package desktop_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The environment the re-executed child reads. It is a test-only switch: the
// child is this same test binary, so no build runs for it and nothing about the
// facade changes to make the crash reachable.
const (
	crashChild     = "READMIT_DESKTOP_CRASH_CHILD"
	crashSession   = "READMIT_DESKTOP_CRASH_SESSION"
	crashWorkspace = "READMIT_DESKTOP_CRASH_WORKSPACE"
	crashSpec      = "READMIT_DESKTOP_CRASH_SPEC"
	crashOutput    = "READMIT_DESKTOP_CRASH_OUTPUT"
)

// draftBody is the unstored working text the crash has to return. It is only
// ever in the session document and in this test.
const draftBody = "MSA-1 came back AE on the reschedule; still checking whether"

// draftLoopName is the note the write-racing-the-kill child replaces over and
// over, so the parent can wait until a replacement has actually landed.
const draftLoopName = "growing-draft"

// silentPeer accepts connections and reads frames without ever acknowledging
// one, so a send stays in flight until the sender is killed. It counts what it
// accepted, which is how this test proves nothing was sent a second time.
type silentPeer struct {
	address  string
	received chan struct{}

	mu       sync.Mutex
	accepted int
	frames   int
}

func (p *silentPeer) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted, p.frames
}

// A real socket would let the kernel's send buffer decide how much of a write
// the peer has, so this reads whole frames and counts them rather than bytes.
func newSilentPeer(t *testing.T) *silentPeer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	peer := &silentPeer{address: listener.Addr().String(), received: make(chan struct{})}
	var once sync.Once
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			peer.mu.Lock()
			peer.accepted++
			peer.mu.Unlock()
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(20 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					return
				}
				peer.mu.Lock()
				peer.frames++
				peer.mu.Unlock()
				once.Do(func() { close(peer.received) })
				// Never acknowledge: the delivery stays unresolved, which is
				// the state this ticket has to keep honest.
				io.Copy(io.Discard, conn)
			}()
		}
	}()
	return peer
}

// crashFixture is one verified case, one target naming the silent peer, and one
// spec that sends exactly one message to it.
func crashFixture(t *testing.T, address string) (workspace, spec, output string) {
	t.Helper()
	workspace = t.TempDir()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(workspace, "case"),
		[]bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}},
		bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{
			BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	accepted := "AA"
	documents := map[string]any{
		// A long message timeout keeps the send in flight, so the process is
		// killed while the delivery is genuinely unresolved.
		"target.json": replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address,
			Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "1m", MaxACKBytes: 4096},
		"spec.json": testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK",
			Input:       testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}},
			Target:      "target.json",
			Setup:       testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"},
			Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary},
			Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001",
				Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}},
	}
	for name, document := range documents {
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, name), encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return workspace, filepath.Join(workspace, "spec.json"), filepath.Join(workspace, "job")
}

// The scenario this ticket exists for. A person is writing a note and watching
// a run, the process is killed mid-send, and the window is opened again.
//
// What must come back: the unstored note, and where they were. What must not:
// a verdict. The delivery whose effect nobody knows stays uncertain, the run
// whose completion was never recorded stays interrupted, and nothing is
// resumed, restarted or resent to resolve either one.
func TestControlledCrashRestoresUnstoredWorkAndKeepsTheSendUncertain(t *testing.T) {
	if os.Getenv(crashChild) == "1" {
		crashChildRun(t)
		return
	}
	peer := newSilentPeer(t)
	workspace, spec, output := crashFixture(t, peer.address)
	state := t.TempDir()
	session := filepath.Join(state, "session.json")

	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(),
		crashChild+"=1", crashSession+"="+session, crashWorkspace+"="+workspace,
		crashSpec+"="+spec, crashOutput+"="+output)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill() })

	select {
	case <-peer.received:
	case <-time.After(20 * time.Second):
		t.Fatal("the child never sent anything to the test endpoint")
	}
	journal := filepath.Join(output, "journal.jsonl")
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, _ := os.ReadFile(journal)
		if bytes.Contains(raw, []byte(`"kind":"sent"`)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sent prefix was not retained before the crash")
		}
		time.Sleep(time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()

	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	accepted, frames := peer.counts()
	if accepted != 1 || frames != 1 {
		t.Fatalf("the killed process sent %d frames over %d connections", frames, accepted)
	}

	// Restarting the shell is a new facade over the same local state.
	restarted := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), session)
	restored := restarted.RecoverSession()
	if restored.State != desktop.Completed || restored.Session == nil {
		t.Fatalf("the restarted shell restored nothing: %+v", restored)
	}
	if len(restored.Session.Drafts) != 1 {
		t.Fatalf("the unstored note did not survive the crash: %+v", restored.Session.Drafts)
	}
	if body := restored.Session.Drafts[0].Note.Body; body != draftBody {
		t.Fatalf("the restored draft is not what was typed: %q", body)
	}
	if restored.Session.Drafts[0].Project != workspace {
		t.Fatalf("the restored draft names another project: %q", restored.Session.Drafts[0].Project)
	}
	want := desktop.View{Workspace: workspace, Region: "evidence", Case: "case", Run: output}
	if restored.Session.View != want {
		t.Fatalf("restored view %+v, recorded %+v", restored.Session.View, want)
	}

	// The run is recovered as what it is: interrupted, with a delivery whose
	// effect is unknown. Neither becomes a pass because the window reopened.
	if restored.Run == nil {
		t.Fatalf("the run the session was watching was not recovered: %q", restored.RunReason)
	}
	if restored.Run.State != durablerun.DeliveryUncertain {
		t.Fatalf("an interrupted send was reported as %q", restored.Run.State)
	}
	if restored.Run.StopReason != durablerun.Interrupted || !restored.Run.DeliveryUncertain || !restored.Run.Recovered {
		t.Fatalf("recovery did not keep the run interrupted and uncertain: %+v", restored.Run)
	}
	if restored.Run.ResultIdentity != "" {
		t.Fatalf("an interrupted run reported a result identity: %+v", restored.Run)
	}
	// Bytes already written stay retained; cancellation and a crash cannot
	// retract them, and recovery must not pretend they were never sent.
	if sent, err := os.ReadFile(filepath.Join(output, "sent", "o000001.bin")); err != nil || len(sent) == 0 {
		t.Fatalf("the bytes already sent were not retained: %v", err)
	}

	// Recovery reads. Repeating it changes no retained byte and opens no
	// connection, so the receiver never sees the message a second time.
	if again := restarted.RecoverSession(); again.Run == nil || again.Run.State != durablerun.DeliveryUncertain {
		t.Fatalf("repeated recovery did not report the same uncertain run: %+v", again)
	}
	if after, err := os.ReadFile(journal); err != nil || !bytes.Equal(before, after) {
		t.Fatal("recovery changed the retained journal")
	}
	if accepted, frames = peer.counts(); accepted != 1 || frames != 1 {
		t.Fatalf("recovery resent: %d frames over %d connections", frames, accepted)
	}

	// Executing again is a deliberate act that needs a new output. The retained
	// run is never reused, so no recovery path can turn into a resend.
	if reused := restarted.StartDurableRun(spec, output); reused.State != desktop.Failed {
		t.Fatalf("an interrupted run was executed again in place: %+v", reused)
	}
	if accepted, frames = peer.counts(); accepted != 1 || frames != 1 {
		t.Fatalf("starting over a retained run resent: %d frames over %d connections", frames, accepted)
	}
}

// A write racing the kill. The document is replaced whenever a person moves
// through the window, so the process is killed while it is writing one: what
// comes back must be a complete document the shell can read, never a torn one,
// and never an invented state.
func TestAWriteRacingTheKillLeavesACompleteDocument(t *testing.T) {
	if os.Getenv(crashChild) == "2" {
		crashDraftLoop(t)
		return
	}
	state := t.TempDir()
	session := filepath.Join(state, "session.json")
	workspace := t.TempDir()

	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(),
		crashChild+"=2", crashSession+"="+session, crashWorkspace+"="+workspace)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if raw, err := os.ReadFile(session); err == nil && bytes.Contains(raw, []byte(draftLoopName)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child retained nothing to race against")
		}
		time.Sleep(time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()

	restored := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), session).RecoverSession()
	if restored.State != desktop.Completed || restored.Session == nil {
		t.Fatalf("a kill during a write left an unreadable session: %+v", restored)
	}
	if len(restored.Session.Drafts) != 1 {
		t.Fatalf("a kill during a write left %d drafts: %+v", len(restored.Session.Drafts), restored.Session.Drafts)
	}
	// Whatever it is, it is one of the complete bodies the child wrote: the
	// replacement is whole or it never happened, never half of each.
	body := restored.Session.Drafts[0].Note.Body
	if body == "" || strings.Trim(body, "x") != "" {
		t.Fatalf("a kill during a write left a torn body: %q", body)
	}
}

// crashDraftLoop retains a growing note until it is killed, so the kill lands
// somewhere inside the sequence of replacements.
func crashDraftLoop(t *testing.T) {
	state := filepath.Dir(os.Getenv(crashSession))
	app := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), os.Getenv(crashSession))
	workspace := os.Getenv(crashWorkspace)
	// The body stays inside the bound a note is held to, so the loop never ends
	// by being refused: the only thing that stops it is the kill.
	for i := 1; ; i++ {
		retained := app.SaveDraft(desktop.Draft{Project: workspace,
			Note: project.Note{Name: draftLoopName, Title: "Growing", Body: strings.Repeat("x", 1+i%512)}})
		if retained.State != desktop.Completed {
			t.Fatalf("child could not retain its draft: %+v", retained)
		}
	}
}

// crashChildRun is the process that gets killed. It records where it is and the
// note it is writing through the public facade, then starts the one send that
// never completes.
func crashChildRun(t *testing.T) {
	state := filepath.Dir(os.Getenv(crashSession))
	app := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), os.Getenv(crashSession))
	workspace := os.Getenv(crashWorkspace)
	view := desktop.View{Workspace: workspace, Region: "evidence", Case: "case", Run: os.Getenv(crashOutput)}
	if recorded := app.RecordView(view); recorded.State != desktop.Completed {
		t.Fatalf("child could not record its view: %+v", recorded)
	}
	retained := app.SaveDraft(desktop.Draft{Project: workspace,
		Note: project.Note{Name: "triage", Subject: "case", Title: "Reschedule triage", Body: draftBody}})
	if retained.State != desktop.Completed {
		t.Fatalf("child could not retain its draft: %+v", retained)
	}
	app.StartDurableRun(os.Getenv(crashSpec), os.Getenv(crashOutput))
}

// Cancelling is not the same interruption as a crash, and recovery must not
// flatten the two. A run cancelled after bytes reached the socket stopped
// because a person asked, and its delivery is still unresolved: recovery has to
// report both of those, and still never resend.
func TestRecoveringACancelledRunKeepsItsStopReasonAndUncertainty(t *testing.T) {
	peer := newSilentPeer(t)
	workspace, spec, output := crashFixture(t, peer.address)
	state := t.TempDir()
	session := filepath.Join(state, "session.json")
	app := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), session)

	if recorded := app.RecordView(desktop.View{Workspace: workspace, Region: "evidence", Run: output}); recorded.State != desktop.Completed {
		t.Fatalf("record view: %+v", recorded)
	}
	executed := make(chan desktop.DurableRunResult, 1)
	go func() { executed <- app.StartDurableRun(spec, output) }()
	select {
	case <-peer.received:
	case <-time.After(20 * time.Second):
		t.Fatal("nothing was sent to the test endpoint")
	}
	app.Cancel()
	select {
	case result := <-executed:
		if result.State != desktop.Completed || result.Run == nil {
			t.Fatalf("the cancelled run did not finish: %+v", result)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cancelling did not stop the run")
	}

	restored := app.RecoverSession()
	if restored.State != desktop.Completed || restored.Run == nil {
		t.Fatalf("the cancelled run was not recovered: %+v", restored)
	}
	if restored.Run.StopReason != durablerun.Cancelled {
		t.Fatalf("recovery lost why the run stopped: %+v", restored.Run)
	}
	if restored.Run.State != durablerun.DeliveryUncertain || !restored.Run.DeliveryUncertain {
		t.Fatalf("cancelling was reported as though nothing reached the receiver: %+v", restored.Run)
	}
	if accepted, frames := peer.counts(); accepted != 1 || frames != 1 {
		t.Fatalf("recovering a cancelled run resent: %d frames over %d connections", frames, accepted)
	}
}
