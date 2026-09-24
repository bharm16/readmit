package tests

// Reexecution from the privacy review is one operation with two entry points.
// The window previews and sends through the same `redact reexecute`
// preparation and execution the command line runs, so the assessment one
// writes is the assessment the other writes, the job one retains is the job
// the other's recovery reads, and every refusal is the same refusal. Every
// send here reaches only a loopback receiver this test started on the address
// the actual original execution recorded; nothing else is ever contacted.

import (
	"context"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

// reexecution is one workspace both entry points reexecute from: a review the
// window derived and its private state, the retained packet whose current run
// is the actual original failing execution, the authorized target that
// execution recorded, and a rebound specification naming the derived case,
// that target and the observation a receiver writes — each one entry.
type reexecution struct {
	workspace, identity, address string
	app                          *desktop.App
}

func reexecutionWorkspace(t *testing.T) reexecution {
	t.Helper()
	workspace := privacyParityWorkspace(t)
	app := desktopApp(t, workspace)
	derived := app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json",
		Output: "review", LocalState: "review-private",
	})
	if derived.State != desktop.Completed || derived.Outcome == nil || derived.Outcome.State != "ready-for-approval" {
		t.Fatalf("the window did not derive a ready review: %+v", derived)
	}
	// The actual original phase: the failing execution the derivation's
	// private proof retained, assembled into a customer-local packet as its
	// current run, exactly as `report assemble` retains one.
	originalRun := filepath.Join(workspace, "review-private", "original-proof", "baseline")
	if _, err := report.Assemble(context.Background(), report.RetainedInput{
		Case: filepath.Join(workspace, "original.case"), Current: filepath.Join(originalRun, "result"),
		Spec: filepath.Join(originalRun, "result", "spec.json"),
	}, filepath.Join(workspace, "original-packet")); err != nil {
		t.Fatal(err)
	}
	original, err := testrunner.Open(filepath.Join(originalRun, "result"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.ReadFile(filepath.Join(originalRun, "target.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, workspace, "authorized-target.json", string(target))
	writeRebound(t, workspace, "rebound.json", "authorized-target.json", "observation.json")
	return reexecution{workspace: workspace, identity: derived.Outcome.Identity, address: original.Result.Target.Address, app: app}
}

// writeRebound copies the reviewed specification and rebinds only its case,
// target and observation, relative to the workspace it is written into.
func writeRebound(t *testing.T, workspace, name, target, observationPath string) {
	t.Helper()
	spec, err := testrunner.ReadSpec(filepath.Join(workspace, "review", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Case, spec.Target, spec.Observation.Path = "review/case", target, observationPath
	redactJSON(t, filepath.Join(workspace, name), spec)
}

func (r reexecution) request(approval, phase string) desktop.ReexecutionRequest {
	return desktop.ReexecutionRequest{Workspace: r.workspace, Review: "review", LocalState: "review-private",
		OriginalPacket: "original-packet", Spec: "rebound.json", Phase: phase, Approval: approval}
}

func (r reexecution) send(request desktop.ReexecutionRequest, expected, output string) desktop.ReexecutionResult {
	return r.app.ReexecuteReviewedEvidence(desktop.ReexecutionSendRequest{Workspace: request.Workspace, Review: request.Review,
		LocalState: request.LocalState, OriginalPacket: request.OriginalPacket, Spec: request.Spec, Phase: request.Phase,
		Approval: request.Approval, Output: output, Expected: expected, Authorize: true})
}

// cli is `readmit redact reexecute` over the same entries: a preview, or a
// send into output when one is named.
func (r reexecution) cli(t *testing.T, request desktop.ReexecutionRequest, output string) (string, string, error) {
	t.Helper()
	args := []string{"redact", "reexecute", filepath.Join(r.workspace, request.Review),
		"--local-state", filepath.Join(r.workspace, request.LocalState), "--approve", request.Approval,
		"--original-packet", filepath.Join(r.workspace, request.OriginalPacket),
		"--spec", filepath.Join(r.workspace, request.Spec), "--phase", request.Phase}
	if output != "" {
		args = append(args, "--send", "--output", filepath.Join(r.workspace, output))
	}
	return run(t, args...)
}

// serveLedger starts the built-in appointment-ledger receiver on the address
// the original execution recorded, writing the observation the rebound
// specification names, for two messages. It closes its listener once it has
// received them; the function it returns waits for that.
func (r reexecution) serveLedger(t *testing.T, mode observation.Mode) func() {
	t.Helper()
	listener, err := net.Listen("tcp4", r.address)
	if err != nil {
		t.Fatal(err)
	}
	server, err := receiver.New(receiver.Config{Mode: mode, OutputPath: filepath.Join(t.TempDir(), "received"),
		ObservationPath: filepath.Join(r.workspace, "observation.json"), MaxFrameBytes: 1 << 20, IdleTimeout: 10 * time.Second, MaxMessages: 2})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = server.Serve(ctx, listener); listener.Close(); close(done) }()
	t.Cleanup(func() { cancel(); listener.Close(); <-done })
	return func() {
		t.Helper()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("the receiver did not finish its two messages")
		}
	}
}

// counted is a listener standing on the original address that counts every
// connection reaching it. When it holds, it reads one framed message and never
// acknowledges it, so a send waits on it until cancelled.
type counted struct {
	accepted, framed atomic.Int64
	listener         net.Listener
}

func countingListener(t *testing.T, address string, hold bool) *counted {
	t.Helper()
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	c := &counted{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			c.accepted.Add(1)
			go func() {
				defer conn.Close()
				if !hold {
					return
				}
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err == nil {
					c.framed.Add(1)
				}
				_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
				_, _ = conn.Read(make([]byte, 1))
			}()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return c
}

// The window's preview is the command's preview, byte for byte, and neither
// connects to anything. The window's send is the command's send: it matches
// the reviewed failure set, declines external equivalence, and retains a job
// `readmit run status --recovery` reads as the result the window assessed;
// the command line's own send assesses the same inputs the same way. A fixed
// receiver changes the selected criteria, and both refuse that phase with the
// same assessment.
func TestTheWindowReexecutesReviewedEvidenceExactlyAsReadmitRedactReexecuteDoes(t *testing.T) {
	r := reexecutionWorkspace(t)
	request := r.request(r.identity, "failure")

	// The two previews, while a counting listener holds the address.
	counter := countingListener(t, r.address, false)
	preview := r.app.PreviewReexecution(request)
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("the window did not preview: %+v", preview)
	}
	stdout, stderr, err := r.cli(t, request, "")
	if err != nil {
		t.Fatalf("the command line did not preview: %v %s", err, stderr)
	}
	if cli := readStrictOutput[redact.ReexecutionAssessment](t, stdout); cli != preview.Preview.Assessment {
		t.Fatalf("the previews disagree:\nwindow %+v\ncli    %+v", preview.Preview.Assessment, cli)
	}
	shown := preview.Preview
	if shown.Assessment.Criteria != "not-executed" || shown.Assessment.ExternalEquivalence != "declined" ||
		shown.Target.Address != r.address || !shown.Target.TestEndpoint || len(shown.Selected) != 2 ||
		shown.InitialState != "empty-ledger" || shown.Destination.Name != "reexecution-001" || !shown.Destination.Fresh || !shown.Admission.Admitted {
		t.Fatalf("the preview does not show what a send would do: %+v", shown)
	}
	if n := counter.accepted.Load(); n != 0 {
		t.Fatalf("previewing reached the target %d times", n)
	}
	if _, err := os.Lstat(filepath.Join(r.workspace, "reexecution-001")); !os.IsNotExist(err) {
		t.Fatal("previewing wrote a job")
	}
	counter.listener.Close()

	// The window sends once, into the job its preview proposed.
	received := r.serveLedger(t, observation.Defective)
	sent := r.send(request, shown.Identity, shown.Destination.Name)
	if sent.State != desktop.Completed || sent.Outcome == nil || sent.Outcome.Job != "reexecution-001" {
		t.Fatalf("the window did not reexecute: %+v", sent)
	}
	assessed := sent.Outcome.Assessment
	if assessed.Criteria != "matched" || assessed.ExternalEquivalence != "declined" || assessed.ResultIdentity == "" ||
		sent.Outcome.Retained == nil || sent.Outcome.Retained.Run.State != durablerun.AssertionFailed || sent.Outcome.Retained.Uncertain != 0 {
		t.Fatalf("the window's reexecution was not assessed as the command assesses it: %+v", sent.Outcome)
	}
	// The command line's recovery reads the job the window retained.
	stdout, stderr, err = run(t, "run", "status", filepath.Join(r.workspace, "reexecution-001"), "--recovery", "--json")
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("recovery of the window's job exited %d: %s", code, stderr)
	}
	if recovery := readStrictOutput[durablerun.Recovery](t, stdout); !recovery.Terminal || recovery.Run.ResultIdentity != assessed.ResultIdentity {
		t.Fatalf("the command line recovers another result than the window assessed: %+v", recovery)
	}

	// The command line sends the same inputs after the operator's reset.
	received()
	if err := os.Remove(filepath.Join(r.workspace, "observation.json")); err != nil {
		t.Fatal(err)
	}
	received = r.serveLedger(t, observation.Defective)
	stdout, stderr, err = r.cli(t, request, "cli-job")
	if err != nil {
		t.Fatalf("the command line did not reexecute: %v %s", err, stderr)
	}
	cli := readStrictOutput[redact.ReexecutionAssessment](t, stdout)
	cli.ResultIdentity, assessed.ResultIdentity = "", ""
	if cli != assessed {
		t.Fatalf("the two sends are assessed differently:\nwindow %+v\ncli    %+v", assessed, cli)
	}

	// A fixed receiver changes the selected failure set: both entry points
	// retain the job and refuse it, the command line with its refusal status
	// and the assessment's reason, the window with that reason.
	var windowChanged redact.ReexecutionAssessment
	for _, output := range []string{"window-changed", "cli-changed"} {
		received()
		if err := os.Remove(filepath.Join(r.workspace, "observation.json")); err != nil {
			t.Fatal(err)
		}
		received = r.serveLedger(t, observation.Fixed)
		if output == "window-changed" {
			changed := r.send(request, shown.Identity, output)
			if changed.State != desktop.Failed || changed.Outcome == nil || changed.Reason != changed.Outcome.Assessment.Reason ||
				changed.Outcome.Assessment.Criteria != "changed" || changed.Outcome.Retained == nil || changed.Outcome.Retained.Run.State != durablerun.Passed {
				t.Fatalf("a changed phase was not refused as the command refuses it: %+v", changed)
			}
			windowChanged = changed.Outcome.Assessment
			continue
		}
		stdout, stderr, err = r.cli(t, request, output)
		if code := exitCode(t, err); code != exitRefused || stderr != "" {
			t.Fatalf("the command line's changed phase exited %d: %q", code, stderr)
		}
		cli := readStrictOutput[redact.ReexecutionAssessment](t, stdout)
		cli.ResultIdentity, windowChanged.ResultIdentity = "", ""
		if cli != windowChanged {
			t.Fatalf("the two changed phases are assessed differently:\nwindow %+v\ncli    %+v", windowChanged, cli)
		}
	}
}

// Every refusal the command line makes is the window's refusal, in the same
// sentence, before anything is written or sent: an unapproved or blocked
// review, a private linkage from another derivation, a phase the original run
// does not meet, a target that is not the one the original execution
// recorded, a hostname nobody approved, and a production-classified target.
// Only the window's own gates have their own sentences: a send nobody
// authorized, a send nobody previewed, and a preview that no longer describes
// the inputs.
func TestTheWindowRefusesTheReexecutionsReadmitRedactReexecuteRefuses(t *testing.T) {
	r := reexecutionWorkspace(t)
	blocked := r.app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: r.workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json", Output: "review-2", LocalState: "review-2-private",
	})
	if blocked.State != desktop.Completed || blocked.Outcome == nil {
		t.Fatalf("second derivation: %+v", blocked)
	}
	writeDocument(t, r.workspace, "blocked-policy.json", string(mustRead(t, "../testdata/fixtures/redact-policy-blocked.json")))
	unready := r.app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: r.workspace, Case: "original.case", Spec: "spec.json",
		Policy: "blocked-policy.json", Inventory: "inventory.json", Output: "blocked-review", LocalState: "blocked-private",
	})
	if unready.State != desktop.Completed || unready.Outcome == nil || unready.Outcome.State != "blocked" {
		t.Fatalf("blocked derivation: %+v", unready)
	}
	var target map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(r.workspace, "authorized-target.json")), &target); err != nil {
		t.Fatal(err)
	}
	variant := func(name string, change func(map[string]any)) string {
		copied := map[string]any{}
		for key, value := range target {
			copied[key] = value
		}
		change(copied)
		redactJSON(t, filepath.Join(r.workspace, name+".json"), copied)
		writeRebound(t, r.workspace, name+"-spec.json", name+".json", "observation.json")
		return name + "-spec.json"
	}
	elsewhere := variant("elsewhere", func(document map[string]any) { document["address"] = freeLoopbackAddress(t, "tcp4") })
	hostname := variant("hostname", func(document map[string]any) { document["address"] = "lab.invalid:2575" })
	production := variant("production", func(document map[string]any) {
		document["schema"], document["name"], document["classification"] = "readmit-target/v3", "scheduling-lab", "production"
	})

	counter := countingListener(t, r.address, false)
	for _, refused := range []struct {
		name    string
		request desktop.ReexecutionRequest
		reason  string
	}{
		{"a wrong approval", r.request(strings.Repeat("0", 64), "failure"), "reexecution requires the exact complete disclosure review; changed artifacts require review again"},
		{"a blocked review", func() desktop.ReexecutionRequest {
			request := r.request(unready.Outcome.Identity, "failure")
			request.Review, request.LocalState = "blocked-review", "blocked-private"
			return request
		}(), "reexecution requires the exact complete disclosure review; changed artifacts require review again"},
		{"another derivation's private state", func() desktop.ReexecutionRequest {
			request := r.request(r.identity, "failure")
			request.LocalState = "review-2-private"
			return request
		}(), "private source linkage differs from approved review"},
		{"a phase the original run does not meet", r.request(r.identity, "pass"), "actual original execution does not meet the selected failure/pass criteria"},
		{"a target the original execution did not record", func() desktop.ReexecutionRequest {
			request := r.request(r.identity, "failure")
			request.Spec = elsewhere
			return request
		}(), "reexecution target configuration differs from the selected original execution"},
		{"a hostname nobody approved", func() desktop.ReexecutionRequest {
			request := r.request(r.identity, "failure")
			request.Spec = hostname
			return request
		}(), "nonloopback targets and hostnames require approved_transport true"},
		{"a production-classified target", func() desktop.ReexecutionRequest {
			request := r.request(r.identity, "failure")
			request.Spec = production
			return request
		}(), "this configuration records the production classification; readmit does not replay to a production-classified environment"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			_, stderr, err := r.cli(t, refused.request, "cli-refused")
			if exitCode(t, err) == 0 || !strings.Contains(stderr, refused.reason) {
				t.Fatalf("the command line: %v %q", err, stderr)
			}
			if preview := r.app.PreviewReexecution(refused.request); preview.State != desktop.Failed || preview.Reason != refused.reason || preview.Preview != nil {
				t.Fatalf("the window's preview: %+v", preview)
			}
			if sent := r.send(refused.request, strings.Repeat("a", 64), "window-refused"); sent.State != desktop.Failed || sent.Reason != refused.reason || sent.Outcome != nil {
				t.Fatalf("the window's send: %+v", sent)
			}
			for _, output := range []string{"cli-refused", "window-refused"} {
				if _, err := os.Lstat(filepath.Join(r.workspace, output)); !os.IsNotExist(err) {
					t.Fatalf("a refused reexecution wrote %s", output)
				}
			}
		})
	}

	// The window's own gates: no authorization, no preview, and a stale one.
	request := r.request(r.identity, "failure")
	preview := r.app.PreviewReexecution(request)
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("preview: %+v", preview)
	}
	unauthorized := r.app.ReexecuteReviewedEvidence(desktop.ReexecutionSendRequest{Workspace: r.workspace, Review: "review",
		LocalState: "review-private", OriginalPacket: "original-packet", Spec: "rebound.json", Phase: "failure",
		Approval: r.identity, Output: "window-refused", Expected: preview.Preview.Identity})
	if unauthorized.State != desktop.Failed || !strings.Contains(unauthorized.Reason, "explicit authorization") {
		t.Fatalf("an unauthorized send: %+v", unauthorized)
	}
	if unpreviewed := r.send(request, "", "window-refused"); unpreviewed.State != desktop.Failed || !strings.Contains(unpreviewed.Reason, "preview the reexecution first") {
		t.Fatalf("a send nobody previewed: %+v", unpreviewed)
	}
	_, stderr, err := run(t, "redact", "reexecute", filepath.Join(r.workspace, "review"), "--local-state", filepath.Join(r.workspace, "review-private"),
		"--approve", r.identity, "--original-packet", filepath.Join(r.workspace, "original-packet"), "--spec", filepath.Join(r.workspace, "rebound.json"),
		"--phase", "failure", "--output", filepath.Join(r.workspace, "cli-refused"))
	if exitCode(t, err) == 0 || !strings.Contains(stderr, "reexecute requires --send and --output together") {
		t.Fatalf("the command line sent without --send: %v %s", err, stderr)
	}
	writeRebound(t, r.workspace, "rebound.json", "authorized-target.json", "observation-after-preview.json")
	if stale := r.send(request, preview.Preview.Identity, "window-refused"); stale.State != desktop.Failed || !strings.Contains(stale.Reason, "changed after the preview") {
		t.Fatalf("a stale preview was sent: %+v", stale)
	}
	for _, output := range []string{"cli-refused", "window-refused"} {
		if _, err := os.Lstat(filepath.Join(r.workspace, output)); !os.IsNotExist(err) {
			t.Fatalf("a refused reexecution wrote %s", output)
		}
	}
	if n := counter.accepted.Load(); n != 0 {
		t.Fatalf("refused reexecutions reached the target %d times", n)
	}
}

// Without an activation neither entry point sends. The command line refuses
// the whole command; the window's preview states the same refusal as its
// admission, and its send is refused with it, before anything is written.
func TestAnUnlicensedWindowIsRefusedTheReexecutionTheCommandLineRefuses(t *testing.T) {
	r := reexecutionWorkspace(t)
	isolateInstalledLicense(t)
	unlicensed := unlicensedDesktopApp(t, r.workspace)
	request := r.request(r.identity, "failure")
	counter := countingListener(t, r.address, false)
	_, stderr, err := withoutPolicy(t, "redact", "reexecute", filepath.Join(r.workspace, "review"), "--local-state", filepath.Join(r.workspace, "review-private"),
		"--approve", r.identity, "--original-packet", filepath.Join(r.workspace, "original-packet"), "--spec", filepath.Join(r.workspace, "rebound.json"),
		"--phase", "failure", "--send", "--output", filepath.Join(r.workspace, "cli-refused"))
	if exitCode(t, err) == 0 {
		t.Fatalf("the command line reexecuted without an activation: %s", stderr)
	}
	preview := unlicensed.PreviewReexecution(request)
	if preview.State != desktop.Completed || preview.Preview == nil || preview.Preview.Admission.Admitted || preview.Preview.Admission.Reason == "" ||
		!strings.Contains(stderr, preview.Preview.Admission.Reason) {
		t.Fatalf("the preview does not state the command line's refusal %q: %+v", stderr, preview)
	}
	sent := unlicensed.ReexecuteReviewedEvidence(desktop.ReexecutionSendRequest{Workspace: r.workspace, Review: "review",
		LocalState: "review-private", OriginalPacket: "original-packet", Spec: "rebound.json", Phase: "failure",
		Approval: r.identity, Output: "window-refused", Expected: preview.Preview.Identity, Authorize: true})
	if sent.State != desktop.PermissionDenied || sent.Reason != preview.Preview.Admission.Reason || sent.Outcome != nil {
		t.Fatalf("an unlicensed window sent: %+v", sent)
	}
	for _, output := range []string{"cli-refused", "window-refused"} {
		if _, err := os.Lstat(filepath.Join(r.workspace, output)); !os.IsNotExist(err) {
			t.Fatalf("a refused reexecution wrote %s", output)
		}
	}
	if n := counter.accepted.Load(); n != 0 {
		t.Fatalf("an unlicensed reexecution reached the target %d times", n)
	}
}

// A send cancelled while the target holds its acknowledgement stops, is
// reported cancelled rather than as any verdict, and retains a job whose
// delivery stays uncertain — the recovery the command line reads. Nothing is
// resent: a second send into the same job is refused, and the target was
// reached exactly once.
func TestACancelledReexecutionIsRetainedUncertainAndNeverResent(t *testing.T) {
	r := reexecutionWorkspace(t)
	request := r.request(r.identity, "failure")
	preview := r.app.PreviewReexecution(request)
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("preview: %+v", preview)
	}
	// The operator's reset left the empty ledger the target reports before
	// the first message; the held target writes nothing after it.
	initial := mustRead(t, filepath.Join(r.workspace, "review-private", "original-proof", "baseline", "result", "initial-observation.json"))
	writeDocument(t, r.workspace, "observation.json", string(initial))
	held := countingListener(t, r.address, true)
	done := make(chan desktop.ReexecutionResult, 1)
	go func() { done <- r.send(request, preview.Preview.Identity, "held") }()
	deadline := time.Now().Add(20 * time.Second)
	for held.framed.Load() == 0 {
		select {
		case early := <-done:
			t.Fatalf("the send ended before it reached the held target: %+v", early)
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the send never reached the held target")
		}
	}
	r.app.Cancel("reexecution")
	var cancelled desktop.ReexecutionResult
	select {
	case cancelled = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the cancelled send did not return")
	}
	if cancelled.State != desktop.Cancelled || cancelled.Outcome == nil || cancelled.Outcome.Job != "held" ||
		cancelled.Outcome.Assessment.Criteria != "unavailable-or-unstable" || cancelled.Outcome.Retained == nil ||
		cancelled.Outcome.Retained.Uncertain == 0 || !strings.Contains(cancelled.Reason, "never resent") {
		t.Fatalf("a cancelled send was not retained uncertain: %+v", cancelled)
	}
	stdout, stderr, err := run(t, "run", "status", filepath.Join(r.workspace, "held"), "--recovery", "--json")
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("recovery of the cancelled job exited %d: %s", code, stderr)
	}
	recovery := readStrictOutput[durablerun.Recovery](t, stdout)
	if recovery.Uncertain != cancelled.Outcome.Retained.Uncertain || recovery.SafeToRepeat {
		t.Fatalf("the command line recovers the cancelled job differently: %+v", recovery)
	}
	again := r.send(request, preview.Preview.Identity, "held")
	if again.State != desktop.Failed || again.Outcome != nil || !strings.Contains(again.Reason, "fresh destination") {
		t.Fatalf("the cancelled job was sent into again: %+v", again)
	}
	if n := held.accepted.Load(); n != 1 {
		t.Fatalf("the held target was reached %d times, want exactly once", n)
	}
}
