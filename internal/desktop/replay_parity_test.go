package desktop_test

// `readmit replay` previews or explicitly sends selected messages of a case,
// and the replay screen is that command in the window: both prepare through
// operation.PrepareReplay and a send goes through replay.ExecuteWithPolicy.
// The window's answers are held here to what the command prints and retains,
// in process through cli.Execute, over the same case, target configuration and
// send policy: the dry run line for line, the per-message summary of a send,
// the run and the decision each retains, the bytes the receiver was sent, and
// every refusal in the command's own words. Every send reaches only the
// loopback receiver the test started.

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

// replayFlags are the command's flags for what request selects, naming the
// same documents inside workspace.
func replayFlags(workspace string, request desktop.ReplayRequest) []string {
	args := []string{"replay", filepath.Join(workspace, request.Case), "--target", filepath.Join(workspace, request.Target)}
	if request.Policy != "" {
		args = append(args, "--policy", filepath.Join(workspace, request.Policy))
	}
	for _, message := range request.Messages {
		args = append(args, "--message", message)
	}
	for _, transformation := range request.Transformations {
		args = append(args, "--transform", transformation.Name)
		if transformation.Shift != "" {
			args = append(args, "--shift", transformation.Shift)
		}
	}
	return args
}

// replayDryRun is the dry run `readmit replay` prints, composed from what the
// window's preview answered.
func replayDryRun(result desktop.ReplayResult) string {
	preview, decision := result.Preview, result.Decision
	var text strings.Builder
	fmt.Fprintf(&text, "Dry run: no connection opened\nTarget: %q (%s)\n", preview.Target.Address, preview.Target.Transport)
	name := preview.Target.Name
	if name == "" {
		name = "none"
	}
	fmt.Fprintf(&text, "Environment: %s\nClassification: %s (recorded by a person; readmit did not establish it and never reads it as permission)\n", name, preview.Target.Classification)
	verdict := "denied"
	if decision.Allowed {
		verdict = "allowed"
	}
	listed := func(values []string) string {
		if len(values) == 0 {
			return "none"
		}
		return strings.Join(values, ", ")
	}
	fmt.Fprintf(&text, "Send policy: %s (%s)\nDestination: %s resolved to %s\n", verdict, decision.Reason, decision.Address, listed(decision.ResolvedAddresses))
	if decision.PolicySelected {
		fmt.Fprintf(&text, "Approved destinations: %s\n", listed(decision.ApprovedDestinations))
	} else {
		text.WriteString("Approved destinations: no policy was selected, so only a literal loopback address may be sent to\n")
	}
	if decision.Reason == sendpolicy.SendNotExplicit {
		text.WriteString("Nothing about this destination refuses a send; an explicit --send is what would request one.\n")
	}
	text.WriteString("A decision is reached before any byte leaves: it can stop a send, and it cannot retract bytes already sent.\n")
	fmt.Fprintf(&text, "Messages: %d\n", len(preview.Messages))
	if len(preview.Transformations) == 0 {
		text.WriteString("Transformations: none; message payload bytes unchanged\n")
	}
	for _, transformation := range preview.Transformations {
		fmt.Fprintf(&text, "Transformation: %s", transformation.Name)
		if transformation.Shift != "" {
			fmt.Fprintf(&text, " shift=%s", transformation.Shift)
		}
		text.WriteString("\n")
	}
	for _, message := range preview.Messages {
		fmt.Fprintf(&text, "  %s source=%s wire_bytes=%d\n", message.Outbound, message.Source, message.WireBytes)
	}
	return text.String()
}

// readDecision reads one retained send decision.
func readDecision(t *testing.T, path string) sendpolicy.Decision {
	t.Helper()
	var decision sendpolicy.Decision
	if err := json.Unmarshal(mustRead(t, path), &decision); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return decision
}

// sameDecision reports whether two decisions decided the same thing about the
// same destination; only when each was decided differs.
func sameDecision(a, b sendpolicy.Decision) bool {
	return a.Schema == b.Schema && a.Allowed == b.Allowed && a.Reason == b.Reason && a.Address == b.Address &&
		a.Classification == b.Classification && a.ExplicitSend == b.ExplicitSend && a.PolicySelected == b.PolicySelected &&
		slices.Equal(a.ApprovedDestinations, b.ApprovedDestinations) && slices.Equal(a.ResolvedAddresses, b.ResolvedAddresses)
}

// The window's preview is the command's dry run, line for line, and neither
// sends anything. The window's approved send then retains what the command's
// send retains: a run that verifies through the same reader with the same
// mappings, transformations, recorded changes and target record, the same
// bytes sent for every message, the same per-message outcome the command
// prints, and a decision beside the run that decided what the command's
// decision decided.
func TestTheWindowPreviewsAndSendsAReplayExactlyAsReadmitReplayDoes(t *testing.T) {
	receiver := newReplayReceiver(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	request := bothMessages(workspace, identity)

	preview := app.PreviewReplay(request)
	if preview.State != desktop.Completed || preview.Preview == nil || preview.Decision == nil {
		t.Fatalf("preview: %+v", preview)
	}
	decisionPath := filepath.Join(t.TempDir(), "preview.decision.json")
	stdout, stderr, err := commandLine(t, append(replayFlags(workspace, request), "--decision", decisionPath)...)
	if err != nil {
		t.Fatalf("readmit replay: %v\n%s", err, stderr)
	}
	if want := replayDryRun(preview); stdout != want {
		t.Fatalf("the window's preview is not the command's dry run.\ncommand:\n%s\nwindow:\n%s", stdout, want)
	}
	if retained := readDecision(t, decisionPath); !sameDecision(retained, *preview.Decision) {
		t.Fatalf("the command's preview decided %+v, the window's %+v", retained, *preview.Decision)
	}
	if receiver.reached() != 0 {
		t.Fatalf("a preview reached the target %d times", receiver.reached())
	}

	sent := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: preview.Preview.Identity, Approved: true})
	if sent.State != desktop.Completed || sent.Run == nil || !sent.Run.Successful || sent.Decision == nil || !sent.Decision.Allowed {
		t.Fatalf("the window's send: %+v", sent)
	}
	cliOutput := filepath.Join(workspace, "cli-run")
	printed, stderr, err := commandLine(t, append([]string{"--operation-policy", testlicense.New(t)}, append(replayFlags(workspace, request), "--send", "--output", cliOutput)...)...)
	if err != nil {
		t.Fatalf("readmit replay --send: %v\n%s", err, stderr)
	}
	if !strings.Contains(printed, fmt.Sprintf("Schema: %s\n", sent.Run.Schema)) ||
		!strings.Contains(printed, fmt.Sprintf("Messages: %d\nContains source values: %t (%s)\n", len(sent.Run.Messages), sent.Run.ContainsSourceValues, sent.Run.ExportPolicy)) {
		t.Fatalf("the command's summary differs from the window's run: %s\n%+v", printed, sent.Run)
	}
	for _, message := range sent.Run.Messages {
		line := fmt.Sprintf("  %s outcome=%s delivery=%s sent_bytes=%d received_bytes=%d ack=%s correlation=%s elapsed=",
			message.Outbound, message.Outcome, message.Delivery, message.SentBytes, message.ReceivedBytes, message.ACK, message.Correlation)
		if !strings.Contains(printed, line) {
			t.Errorf("the command printed no %q for the window's %+v:\n%s", line, message, printed)
		}
	}

	windowRun, err := replay.Open(filepath.Join(workspace, sent.Run.Output))
	if err != nil {
		t.Fatalf("the window's run does not verify: %v", err)
	}
	commandRun, err := replay.Open(cliOutput)
	if err != nil {
		t.Fatal(err)
	}
	if windowRun.Identity != sent.Run.Identity {
		t.Fatalf("the window reported run %s and retained %s", sent.Run.Identity, windowRun.Identity)
	}
	mine, theirs := windowRun.Manifest, commandRun.Manifest
	for name, same := range map[string]bool{
		"source":          mine.SourceBundleIdentity == theirs.SourceBundleIdentity && mine.SourceBundleIdentity == identity,
		"target":          mine.Target == theirs.Target,
		"mappings":        slices.Equal(mine.Mappings, theirs.Mappings),
		"transformations": slices.Equal(mine.Transformations, theirs.Transformations),
		"changes": slices.EqualFunc(mine.Changes, theirs.Changes, func(a, b replay.Change) bool {
			return a.Transformation == b.Transformation && a.SourceOccurrence == b.SourceOccurrence && a.OutboundOccurrence == b.OutboundOccurrence &&
				a.Selector == b.Selector && a.OldState == b.OldState && a.NewState == b.NewState && bytes.Equal(a.Old, b.Old) && bytes.Equal(a.New, b.New)
		}),
		"counts": mine.MessageCount == theirs.MessageCount && mine.ContainsSourceValues == theirs.ContainsSourceValues && mine.ExportPolicy == theirs.ExportPolicy,
	} {
		if !same {
			t.Errorf("the window's run and the command's differ in their %s", name)
		}
	}
	for i := range windowRun.Events {
		a, b := windowRun.Events[i], commandRun.Events[i]
		mineSent, _ := windowRun.Raw(a.Sent)
		theirSent, _ := commandRun.Raw(b.Sent)
		if !bytes.Equal(mineSent, theirSent) || a.Outcome != b.Outcome || a.Delivery != b.Delivery {
			t.Errorf("message %s: the window sent %q (%s, %s), the command %q (%s, %s)", a.OutboundOccurrence, mineSent, a.Outcome, a.Delivery, theirSent, b.Outcome, b.Delivery)
		}
	}
	if retained := readDecision(t, filepath.Join(workspace, sent.Run.DecisionFile)); !sameDecision(retained, *sent.Decision) ||
		!sameDecision(retained, readDecision(t, cliOutput+".decision.json")) {
		t.Fatalf("the window retained decision %+v; the command %+v", retained, readDecision(t, cliOutput+".decision.json"))
	}

	// The receiver, which shares no code with either, got the same two
	// messages twice: rebased and shifted a day.
	received := receiver.received()
	if len(received) != 4 || received[0] != received[2] || received[1] != received[3] {
		t.Fatalf("the receiver got %d messages: %q", len(received), received)
	}
	if !strings.Contains(received[0], "|SIU^S12|READMIT000001|") || !strings.Contains(received[0], "^^^20260103100000+0000^20260103103000+0000") ||
		!strings.Contains(received[1], "|SIU^S13|READMIT000002|") || !strings.Contains(received[1], "^^^20260104110000+0000^20260104113000+0000") {
		t.Fatalf("the messages were not rebased and shifted as previewed: %q", received[:2])
	}
}

// What the command refuses, the window refuses in the command's words and
// shows nothing beside: a selection or transformation the command rejects, a
// production environment, a client certificate this transport cannot present,
// a target of a version that never declared a name, and a policy that is not
// canonical. A send the send policy refuses is refused by both before any
// connection, and both retain the same denial beside the run folder they
// named; nothing reaches the receiver.
func TestTheWindowRefusesTheReplaysReadmitReplayRefusesInItsWords(t *testing.T) {
	receiver := newReplayReceiver(t)
	app := workspaceApp(t)
	workspace, identity := replayWorkspace(t, app, receiver.address)
	writeDocument(t, workspace, "production.json", replayTarget(receiver.address, "production"))
	writeDocument(t, workspace, "certificate.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), `"name"`, `"client_certificate":"client.pem","name"`, 1))
	writeDocument(t, workspace, "named-v1.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), "readmit-target/v3", "readmit-target/v1", 1))
	writeDocument(t, workspace, "masked.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/8"]}`)
	writeDocument(t, workspace, "elsewhere.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["10.1.0.0/16"]}`)

	for name, change := range map[string]func(*desktop.ReplayRequest){
		"an unknown occurrence":     func(r *desktop.ReplayRequest) { r.Messages = []string{"s0001-e000404"} },
		"a duplicate selection":     func(r *desktop.ReplayRequest) { r.Messages = []string{"s0001-e000001", "s0001-e000001"} },
		"an unknown transformation": func(r *desktop.ReplayRequest) { r.Transformations = []replay.Transformation{{Name: "rename-patients"}} },
		"a shift of part of a second": func(r *desktop.ReplayRequest) {
			r.Transformations = []replay.Transformation{{Name: "shift-timestamps", Shift: "1500ms"}}
		},
		"a production environment":       func(r *desktop.ReplayRequest) { r.Target = "production.json" },
		"a client certificate":           func(r *desktop.ReplayRequest) { r.Target = "certificate.json" },
		"a version that declares names":  func(r *desktop.ReplayRequest) { r.Target = "named-v1.json" },
		"a policy that is not canonical": func(r *desktop.ReplayRequest) { r.Policy = "masked.json" },
	} {
		t.Run(name, func(t *testing.T) {
			request := bothMessages(workspace, identity)
			change(&request)
			window := app.PreviewReplay(request)
			if window.State != desktop.Failed || window.Preview != nil {
				t.Fatalf("the window previewed it: %+v", window)
			}
			_, stderr, err := commandLine(t, append(replayFlags(workspace, request), "--decision", filepath.Join(t.TempDir(), "decision.json"))...)
			if err == nil || stderr != "readmit: "+window.Reason+"\n" {
				t.Fatalf("the window refused with %q; the command %q", window.Reason, stderr)
			}
		})
	}

	for name, change := range map[string]func(*desktop.ReplayRequest){
		"a destination the policy does not approve": func(r *desktop.ReplayRequest) { r.Policy = "elsewhere.json" },
		"a production environment":                  func(r *desktop.ReplayRequest) { r.Target = "production.json" },
	} {
		t.Run("sending to "+name, func(t *testing.T) {
			request := bothMessages(workspace, identity)
			change(&request)
			request.Output = "window-" + strings.ReplaceAll(name, " ", "-")
			expected := strings.Repeat("0", 64)
			if preview := app.PreviewReplay(request); preview.Preview != nil {
				if preview.Preview.Sendable || preview.Decision == nil || preview.Decision.Allowed {
					t.Fatalf("the preview offers the send: %+v", preview)
				}
				expected = preview.Preview.Identity
			}
			window := app.SendReplay(desktop.ReplaySendRequest{Replay: request, Expected: expected, Approved: true})
			if window.State != desktop.Failed || window.Run != nil || window.Decision == nil || window.Decision.Allowed {
				t.Fatalf("the window's send: %+v", window)
			}
			output := filepath.Join(workspace, "command-"+strings.ReplaceAll(name, " ", "-"))
			_, stderr, err := commandLine(t, append([]string{"--operation-policy", testlicense.New(t)}, append(replayFlags(workspace, request), "--send", "--output", output)...)...)
			if err == nil || stderr != "readmit: "+window.Reason+"\n" {
				t.Fatalf("the window refused with %q; the command %q", window.Reason, stderr)
			}
			if mine, theirs := readDecision(t, filepath.Join(workspace, request.Output+".decision.json")), readDecision(t, output+".decision.json"); !sameDecision(mine, theirs) || !sameDecision(mine, *window.Decision) {
				t.Fatalf("the window retained %+v; the command %+v", mine, theirs)
			}
			for _, run := range []string{filepath.Join(workspace, request.Output), output} {
				if _, err := os.Lstat(run); !os.IsNotExist(err) {
					t.Errorf("a refused send created %s", filepath.Base(run))
				}
			}
		})
	}
	if receiver.reached() != 0 {
		t.Fatalf("a refused replay reached the target %d times", receiver.reached())
	}
}
