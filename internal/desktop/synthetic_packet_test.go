package desktop_test

// The synthetic demonstration packets of the investigation-packet panels are
// `readmit report`, `report verify` and `report prepare`: the window generates
// the committed scenario into a new folder named in the save dialog, verifies
// a packet offline and prepares runnable copies outside it through the same
// operations and readers the command line uses, and the command line reads
// everything the window wrote exactly as it reads its own. A packet stays
// labelled synthetic in every view, a changed or unsupported packet is refused
// by both entry points with the same sentence, and none of it is admitted
// work: a window without any activation does all of it, as the command line
// does.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

// unactivatedWindow is a window with no operation policy selected, over fresh
// state files: the synthetic walkthrough is ungated, so nothing here may need
// one.
func unactivatedWindow(t *testing.T, c *chooser) *desktop.App {
	t.Helper()
	state := t.TempDir()
	return desktop.New(c, desktop.ShellDocuments{Folder: state})
}

// generated generates one synthetic packet through the window into a new
// folder of parent and requires it to complete.
func generated(t *testing.T, app *desktop.App, parent, name string) (string, *desktop.SyntheticPacketView) {
	t.Helper()
	destination := filepath.Join(parent, name)
	result := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: destination})
	if result.State != desktop.Completed || result.Packet == nil {
		t.Fatalf("generate: %+v", result)
	}
	return destination, result.Packet
}

// refusedWordForWord requires the window's refusal to be the sentence the command
// line prints for the same input.
func refusedWordForWord(t *testing.T, what string, state desktop.State, reason string, stderr string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("the command line accepted %s", what)
	}
	if state != desktop.Failed || "readmit: "+reason+"\n" != stderr {
		t.Fatalf("%s: the window answered %s %q, the command line %q", what, state, reason, stderr)
	}
}

func TestSyntheticPacketGeneratedByTheWindowIsTheOneReadmitReportVerifies(t *testing.T) {
	workspace := t.TempDir()
	app := unactivatedWindow(t, &chooser{})
	destination, packet := generated(t, app, workspace, "demo-packet")

	// The view is the packet's own verified manifest: the committed scenario,
	// synthetic-only provenance, a defective baseline that fails its ledger
	// assertion and a fixed post-fix that passes, over one unchanged input.
	if packet.Folder != "demo-packet" || packet.Schema != report.Schema || packet.State != "complete" || packet.Scenario != report.Scenario {
		t.Fatalf("the generated packet lost its identity: %+v", packet)
	}
	if packet.Provenance != "synthetic-only" || packet.InputChanged || !packet.ReceiverBehaviorChanged || packet.ObservationBoundary != testrunner.LedgerBoundary {
		t.Fatalf("the generated packet lost its synthetic label or boundary: %+v", packet)
	}
	want := []desktop.SyntheticRunView{
		{Path: "baseline", ReceiverMode: "defective", Receiver: "readmit built-in SIU fixture", Status: string(testrunner.AssertionFailure), LedgerCount: 2},
		{Path: "post-fix", ReceiverMode: "fixed", Receiver: "readmit built-in SIU fixture", Status: string(testrunner.Pass), LedgerCount: 1},
	}
	if len(packet.Runs) != len(want) {
		t.Fatalf("runs: %+v", packet.Runs)
	}
	for i, run := range packet.Runs {
		if run.ResultIdentity == "" {
			t.Fatalf("run %s has no result identity", run.Path)
		}
		run.ResultIdentity = ""
		if run != want[i] {
			t.Fatalf("run %d: %+v, want %+v", i, run, want[i])
		}
	}
	if len(packet.Limitations) == 0 || !strings.HasPrefix(packet.Limitations[0], "Synthetic-only evidence; no patient or customer-derived input.") {
		t.Fatalf("the packet's own synthetic limitation is not carried: %+v", packet.Limitations)
	}

	// `readmit report verify` accepts the folder the window wrote, under the
	// identity the window showed; the window reads it back the same.
	stdout, stderr, err := commandLine(t, "report", "verify", destination)
	if err != nil || stdout != "Packet verified: "+packet.Identity+"\nSynthetic-only; input unchanged; appointment-ledger boundary\n" {
		t.Fatalf("report verify: %q %q %v", stdout, stderr, err)
	}
	reopened := app.OpenSyntheticPacket(destination)
	if reopened.State != desktop.Completed || reopened.Packet == nil || reopened.Packet.Identity != packet.Identity || reopened.Packet.Files != packet.Files {
		t.Fatalf("reopen: %+v", reopened)
	}

	// A packet `readmit report` generated opens in the window with the same
	// frozen input, historical specification, labels and limitations; only its
	// fresh executions' identities differ.
	commandPacket := filepath.Join(workspace, "command-packet")
	stdout, stderr, err = commandLine(t, "report", "--scenario", report.Scenario, "--output", commandPacket)
	if err != nil {
		t.Fatalf("readmit report: %q %v", stderr, err)
	}
	written := bytesUnder(t, commandPacket)
	opened := app.OpenSyntheticPacket(commandPacket)
	if opened.State != desktop.Completed || opened.Packet == nil || !strings.Contains(stdout, "Packet: "+opened.Packet.Identity+"\n") {
		t.Fatalf("the command line's packet: %+v, printed %q", opened, stdout)
	}
	if read := bytesUnder(t, commandPacket); !maps.EqualFunc(written, read, func(a, b []byte) bool { return string(a) == string(b) }) {
		t.Fatal("verifying the command line's packet in the window changed it")
	}
	theirs, ours := *opened.Packet, *packet
	for _, view := range []*desktop.SyntheticPacketView{&theirs, &ours} {
		view.Folder, view.Identity = "", ""
		for i := range view.Runs {
			view.Runs[i].ResultIdentity = ""
		}
	}
	if theirs.InputIdentity != ours.InputIdentity || theirs.SpecIdentity != ours.SpecIdentity || theirs.Files != ours.Files ||
		!slices.Equal(theirs.Runs, ours.Runs) || !slices.Equal(theirs.Limitations, ours.Limitations) || theirs.Provenance != ours.Provenance {
		t.Fatalf("the window's packet and the command line's differ beyond their executions:\n%+v\n%+v", ours, theirs)
	}

	// The listing names both folders as synthetic demonstration packets,
	// never as investigation packets of the person's own evidence.
	for _, name := range []string{"demo-packet", "command-packet"} {
		if kind := listingKind(t, app, workspace, name); kind != string(desktop.SyntheticPacketArtifact) {
			t.Fatalf("%s lists as %q", name, kind)
		}
	}

	// Refusals are the operation's own, word for word: an existing folder, a
	// scenario that is not the committed one. Neither writes anything.
	existing := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: destination})
	_, stderr, err = commandLine(t, "report", "--scenario", report.Scenario, "--output", destination)
	refusedWordForWord(t, "an existing destination", existing.State, existing.Reason, stderr, err)
	unknown := filepath.Join(workspace, "unknown-scenario")
	other := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: "customer-derived", Destination: unknown})
	_, stderr, err = commandLine(t, "report", "--scenario", "customer-derived", "--output", unknown)
	refusedWordForWord(t, "another scenario", other.State, other.Reason, stderr, err)
	if _, err := os.Lstat(unknown); !os.IsNotExist(err) {
		t.Fatal("a refused scenario wrote its destination")
	}
	// A destination the save dialog did not name is refused before anything.
	if relative := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: "relative-packet"}); relative.State != desktop.Failed {
		t.Fatalf("a relative destination was generated into: %+v", relative)
	}
}

func TestSyntheticPacketsChangedIncompleteOrUnsupportedAreRefusedByTheWindowAndTheCommandLine(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)
	source, _ := generated(t, app, t.TempDir(), "packet")
	copyOf := func(name string) string {
		t.Helper()
		copied := filepath.Join(t.TempDir(), name)
		if err := os.CopyFS(copied, os.DirFS(source)); err != nil {
			t.Fatal(err)
		}
		return copied
	}
	refused := func(what, path string) {
		t.Helper()
		opened := app.OpenSyntheticPacket(path)
		if opened.Packet != nil {
			t.Fatalf("%s was shown as verified: %+v", what, opened)
		}
		_, stderr, err := commandLine(t, "report", "verify", path)
		refusedWordForWord(t, what, opened.State, opened.Reason, stderr, err)
		// Nothing is prepared from it either, by either entry point.
		rerun := filepath.Join(t.TempDir(), "rerun")
		prepared := app.PrepareSyntheticRerun(desktop.SyntheticRerunRequest{Packet: path, Destination: rerun, Address: "127.0.0.1:2575"})
		_, stderr, err = commandLine(t, "report", "prepare", path, "--output", rerun, "--address", "127.0.0.1:2575")
		refusedWordForWord(t, what+" prepared", prepared.State, prepared.Reason, stderr, err)
		if _, err := os.Lstat(rerun); !os.IsNotExist(err) {
			t.Fatalf("preparing %s wrote the rerun folder", what)
		}
	}

	// A changed derived file, even one a person might think harmless.
	changed := copyOf("changed")
	if err := os.WriteFile(filepath.Join(changed, "SUMMARY.md"), []byte("# Customer evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused("a changed packet", changed)

	// A packet whose completion record is missing never reads as complete.
	incomplete := copyOf("incomplete")
	if err := os.Remove(filepath.Join(incomplete, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	refused("an incomplete packet", incomplete)

	// An unknown contract version is unsupported, never read as v1, even with
	// its completion record recomputed over the rewritten manifest.
	future := copyOf("future")
	manifest, err := os.ReadFile(filepath.Join(future, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten := []byte(strings.Replace(string(manifest), `"schema":"`+report.Schema+`"`, `"schema":"readmit-report/v2"`, 1))
	if string(rewritten) == string(manifest) {
		t.Fatal("the manifest did not declare its contract where expected")
	}
	sum := sha256.Sum256(rewritten)
	if os.WriteFile(filepath.Join(future, "manifest.json"), rewritten, 0o600) != nil ||
		os.WriteFile(filepath.Join(future, "identity.sha256"), []byte(hex.EncodeToString(sum[:])+"\n"), 0o600) != nil {
		t.Fatal("cannot rewrite the manifest")
	}
	refused("an unsupported packet version", future)

	// The person's own retained evidence is not a synthetic packet.
	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("assemble: %+v", assembled)
	}
	refused("a retained investigation packet", filepath.Join(root, assembled.Packet.Entry))

	// Nor is a synthetic packet ever verified, exported or reviewed as the
	// person's own evidence: the retained-packet panels refuse it exactly as
	// `readmit report verify-retained` does.
	if err := os.CopyFS(filepath.Join(root, "synthetic"), os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	if kind := listingKind(t, app, root, "synthetic"); kind != string(desktop.SyntheticPacketArtifact) {
		t.Fatalf("a synthetic packet lists as %q", kind)
	}
	asRetained := app.OpenPacket(root, "synthetic")
	_, stderr, err := commandLine(t, "report", "verify-retained", filepath.Join(root, "synthetic"))
	refusedWordForWord(t, "a synthetic packet verified as retained evidence", asRetained.State, asRetained.Reason, stderr, err)
	review := filepath.Join(t.TempDir(), "review")
	if exported := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: "synthetic", Destination: review}); exported.State == desktop.Completed {
		t.Fatalf("a synthetic packet was exported as a portable review: %+v", exported)
	}
	if _, _, err := commandLine(t, "report", "review", review); err == nil {
		t.Fatal("a portable review of a synthetic packet verified")
	}

	// Refusing changed nothing the refusals read.
	if opened := app.OpenSyntheticPacket(source); opened.State != desktop.Completed {
		t.Fatalf("the source packet no longer verifies: %+v", opened)
	}
}

func TestSyntheticRerunPreparedByTheWindowIsTheCommandLinesPreparation(t *testing.T) {
	folder := t.TempDir()
	app := unactivatedWindow(t, &chooser{})
	packet, view := generated(t, app, folder, "packet")
	before := bytesUnder(t, packet)

	windowRerun := filepath.Join(folder, "window-rerun")
	prepared := app.PrepareSyntheticRerun(desktop.SyntheticRerunRequest{Packet: packet, Destination: windowRerun, Address: "127.0.0.1:2575"})
	if prepared.State != desktop.Completed || prepared.Rerun == nil {
		t.Fatalf("prepare: %+v", prepared)
	}
	rerun := prepared.Rerun
	if rerun.Folder != "window-rerun" || rerun.Address != "127.0.0.1:2575" || rerun.PacketIdentity != view.Identity ||
		rerun.InputIdentity != view.InputIdentity || rerun.HistoricalSpecIdentity != view.SpecIdentity {
		t.Fatalf("the preparation lost its bindings to the packet: %+v", rerun)
	}
	if !slices.Equal(rerun.ChangedBindings, []string{"input.case", "target", "observation.path"}) || len(rerun.Specs) != 3 {
		t.Fatalf("the runnable copies: %+v", rerun)
	}

	// `readmit report prepare` from the same packet and address writes the
	// same workspace, byte for byte, and the window reported what it wrote.
	commandRerun := filepath.Join(folder, "command-rerun")
	stdout, stderr, err := commandLine(t, "report", "prepare", packet, "--output", commandRerun, "--address", "127.0.0.1:2575")
	if err != nil || !strings.Contains(stdout, "Packet: "+view.Identity+"\n") {
		t.Fatalf("report prepare: %q %q %v", stdout, stderr, err)
	}
	windowFiles, commandFiles := bytesUnder(t, windowRerun), bytesUnder(t, commandRerun)
	if !maps.EqualFunc(windowFiles, commandFiles, func(a, b []byte) bool { return string(a) == string(b) }) {
		t.Fatalf("the window's workspace differs from the command line's: %v and %v", slices.Sorted(maps.Keys(windowFiles)), slices.Sorted(maps.Keys(commandFiles)))
	}
	for _, spec := range rerun.Specs {
		sum := sha256.Sum256(windowFiles[spec.Path])
		if hex.EncodeToString(sum[:]) != spec.SHA256 {
			t.Fatalf("the window reported %s as %s", spec.Path, spec.SHA256)
		}
	}
	// The copies keep the synthetic case: its generated provenance is the
	// label the runnable copies carry out of the packet.
	if !strings.Contains(string(windowFiles["reproducer/manifest.json"]), `"mode":"generated"`) {
		t.Fatal("the runnable copies lost the case's generated provenance")
	}

	// Refusals are the operation's own, word for word, and write nothing: an
	// address that is not numeric loopback, a folder inside the sealed
	// packet, and one that exists.
	for what, request := range map[string]desktop.SyntheticRerunRequest{
		"a nonloopback address":        {Packet: packet, Destination: filepath.Join(folder, "wide"), Address: "192.0.2.10:2575"},
		"a host name":                  {Packet: packet, Destination: filepath.Join(folder, "named"), Address: "localhost:2575"},
		"a folder inside the packet":   {Packet: packet, Destination: filepath.Join(packet, "rerun"), Address: "127.0.0.1:2575"},
		"an existing folder":           {Packet: packet, Destination: windowRerun, Address: "127.0.0.1:2575"},
		"a folder in a missing folder": {Packet: packet, Destination: filepath.Join(folder, "missing", "rerun"), Address: "127.0.0.1:2575"},
	} {
		answered := app.PrepareSyntheticRerun(request)
		_, stderr, err := commandLine(t, "report", "prepare", request.Packet, "--output", request.Destination, "--address", request.Address)
		refusedWordForWord(t, what, answered.State, answered.Reason, stderr, err)
	}
	for _, name := range []string{"wide", "named", "missing"} {
		if _, err := os.Lstat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("a refused preparation wrote %s", name)
		}
	}
	// A folder the dialogs did not name is refused before anything is read
	// or written.
	for _, relative := range []desktop.SyntheticRerunRequest{
		{Packet: packet, Destination: "rerun", Address: "127.0.0.1:2575"},
		{Packet: "packet", Destination: filepath.Join(folder, "relative-packet"), Address: "127.0.0.1:2575"},
	} {
		if answered := app.PrepareSyntheticRerun(relative); answered.State != desktop.Failed || answered.Rerun != nil {
			t.Fatalf("a relative folder was prepared: %+v", answered)
		}
	}
	if opened := app.OpenSyntheticPacket("packet"); opened.State != desktop.Failed || opened.Packet != nil {
		t.Fatalf("a relative packet was verified: %+v", opened)
	}
	if _, err := os.Lstat(filepath.Join(folder, "relative-packet")); !os.IsNotExist(err) {
		t.Fatal("a refused preparation wrote relative-packet")
	}

	// Preparing never edits the sealed packet.
	if after := bytesUnder(t, packet); !maps.EqualFunc(before, after, func(a, b []byte) bool { return string(a) == string(b) }) {
		t.Fatal("preparing runnable copies changed the sealed packet")
	}
	if opened := app.OpenSyntheticPacket(packet); opened.State != desktop.Completed || opened.Packet.Identity != view.Identity {
		t.Fatalf("the packet no longer verifies after preparation: %+v", opened)
	}
}

func TestWorkspaceNamesPreparedRerunsAndExplainsTheirWindowAction(t *testing.T) {
	workspace := t.TempDir()
	app := unactivatedWindow(t, &chooser{})
	packet, _ := generated(t, app, workspace, "packet")
	destination := filepath.Join(workspace, "prepared-rerun")
	if prepared := app.PrepareSyntheticRerun(desktop.SyntheticRerunRequest{Packet: packet, Destination: destination, Address: "127.0.0.1:2575"}); prepared.State != desktop.Completed {
		t.Fatalf("prepare: %+v", prepared)
	}
	listed := app.OpenWorkspace(workspace)
	if listed.State != desktop.Completed || listed.Workspace == nil {
		t.Fatalf("list prepared workspace: %+v", listed)
	}
	var rerun desktop.Artifact
	for _, artifact := range listed.Workspace.Artifacts {
		if artifact.Name == "prepared-rerun" {
			rerun = artifact
		}
	}
	if rerun.Kind != desktop.PreparedRerunArtifact || !strings.Contains(rerun.Reason, "RERUN.md") || !strings.Contains(rerun.Reason, "no window action") {
		t.Fatalf("the prepared folder had no truthful kind or action reason: %+v", rerun)
	}
	// A marker that only looks like a preparation cannot give an unprepared
	// folder the prepared kind. It must pass the preparation reader itself.
	marker := filepath.Join(destination, "preparation.json")
	if err := os.WriteFile(marker, []byte(`{"schema":"readmit-report-preparation/v1","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	listed = app.OpenWorkspace(workspace)
	for _, artifact := range listed.Workspace.Artifacts {
		if artifact.Name == "prepared-rerun" && artifact.Kind != desktop.UnsupportedArtifact {
			t.Fatalf("an unreadable preparation was listed as runnable: %+v", artifact)
		}
	}
}

// A cancelled generation answers cancelled, never a failed or completed
// packet, and whatever it left in the new folder verifies as a packet to
// neither entry point.
func TestSyntheticPacketGenerationCancelledLeavesAnIncompleteFolder(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := desktop.GenerateSyntheticPacketWithinForTest(ctx, desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: destination})
	if result.State != desktop.Cancelled || result.Packet != nil || !strings.Contains(result.Reason, "cannot be verified as complete") {
		t.Fatalf("a cancelled generation answered %+v", result)
	}
	app := unactivatedWindow(t, &chooser{})
	if opened := app.OpenSyntheticPacket(destination); opened.State == desktop.Completed {
		t.Fatalf("a cancelled generation's folder verified: %+v", opened)
	}
	if _, _, err := commandLine(t, "report", "verify", destination); err == nil {
		t.Fatal("the command line verified a cancelled generation's folder")
	}
}

// Generation is interruptible under its own name alone: the packet panels'
// other operation and a durable run's cancel leave it running to a packet
// that verifies, and its own cancel, from the moment it holds the slot, stops
// the fixture executions and leaves a folder neither entry point verifies.
func TestSyntheticPacketGenerationIsCancelledOnlyByItsOwnName(t *testing.T) {
	app := unactivatedWindow(t, &chooser{})
	generate := func(destination string, cancelling ...string) desktop.SyntheticPacketResult {
		t.Helper()
		done := make(chan desktop.SyntheticPacketResult, 1)
		go func() {
			done <- app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: destination})
		}()
		// A cancel before the generation holds the slot does nothing, so each
		// name is sent again until the generation answers.
		for {
			select {
			case result := <-done:
				return result
			case <-time.After(time.Millisecond):
				for _, name := range cancelling {
					app.Cancel(name)
				}
			}
		}
	}
	folder := t.TempDir()
	other := generate(filepath.Join(folder, "others-cancelled"), "packet", "durable-run")
	if other.State != desktop.Completed || other.Packet == nil {
		t.Fatalf("another operation's cancel stopped the generation: %+v", other)
	}
	if _, _, err := commandLine(t, "report", "verify", filepath.Join(folder, "others-cancelled")); err != nil {
		t.Fatalf("the generation another cancel could not reach does not verify: %v", err)
	}
	own := filepath.Join(folder, "cancelled")
	cancelled := generate(own, desktop.SyntheticPacketOperationForTest)
	if cancelled.State != desktop.Cancelled || cancelled.Packet != nil || !strings.Contains(cancelled.Reason, "cannot be verified as complete") {
		t.Fatalf("the generation's own cancel answered %+v", cancelled)
	}
	if opened := app.OpenSyntheticPacket(own); opened.State == desktop.Completed {
		t.Fatalf("a cancelled generation's folder verified: %+v", opened)
	}
	if _, _, err := commandLine(t, "report", "verify", own); err == nil {
		t.Fatal("the command line verified a cancelled generation's folder")
	}
	// Recovery is a new folder: the window is free again and generates there.
	if again := app.GenerateSyntheticPacket(desktop.SyntheticPacketRequest{Scenario: report.Scenario, Destination: filepath.Join(folder, "again")}); again.State != desktop.Completed {
		t.Fatalf("generating again into a new folder: %+v", again)
	}
}

// Each folder is named in the dialog that can name it: a new packet or rerun
// folder in the save dialog, which creates nothing, and an existing packet in
// the folder dialog. A dismissed dialog is a cancellation that hands nothing on.
func TestChooseSyntheticPacketPathKinds(t *testing.T) {
	for kind, want := range map[string]struct{ dialog, title string }{
		"packet-destination": {"save", "Choose a new folder for the synthetic demonstration packet"},
		"packet":             {"folder", "Choose a synthetic demonstration packet to verify"},
		"rerun-destination":  {"save", "Choose a new folder for the runnable copies"},
	} {
		named := &chooser{folder: t.TempDir(), destination: filepath.Join(t.TempDir(), "named-here")}
		chosen := unactivatedWindow(t, named).ChooseSyntheticPacketPath(kind)
		path := named.destination
		if want.dialog == "folder" {
			path = named.folder
		}
		if chosen.State != desktop.Completed || chosen.Path != path || !slices.Equal(named.opened, []string{want.dialog}) || !slices.Equal(named.titles, []string{want.title}) {
			t.Fatalf("%s: %+v through %v %v", kind, chosen, named.opened, named.titles)
		}
		if dismissed := unactivatedWindow(t, &chooser{}).ChooseSyntheticPacketPath(kind); dismissed.State != desktop.Cancelled || dismissed.Path != "" {
			t.Fatalf("%s: a dismissed dialog answered %+v", kind, dismissed)
		}
	}
	unknown := &chooser{folder: t.TempDir(), destination: filepath.Join(t.TempDir(), "named-here")}
	if chosen := unactivatedWindow(t, unknown).ChooseSyntheticPacketPath("case"); chosen.State != desktop.Failed || len(unknown.opened) != 0 {
		t.Fatalf("an unknown kind opened a dialog: %+v %v", chosen, unknown.opened)
	}
}
