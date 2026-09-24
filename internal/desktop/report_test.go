package desktop_test

// The investigation-packet journeys: a packet is previewed from the exact
// actual evidence it will copy, assembled by the existing retained-packet
// operation into one new protected destination and read back by identity,
// sealed into a portable review of five inert offline renderings through the
// existing export operation, and reopened read-only through the same
// verifiers the command line uses. A missing baseline stays absent, a
// historical specification is never substituted, and opening a packet or a
// review acquires no authority at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

// packetWorkspace is one workspace holding actual retained evidence: one case,
// the exact historical specification that names it, and two distinct durable
// executions against an independent acking peer — a baseline and a current —
// exactly as the durable-run panels retain them.
func packetWorkspace(t *testing.T, app *desktop.App) (root, spec, baseline, current string) {
	t.Helper()
	peer := newAckingPeer(t, "AA")
	root = ackWorkspace(t, peer.address)
	writeAckSpec(t, root, "reschedule.json", "AA")
	identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: root, Spec: "reschedule.json"})
	for _, name := range []string{"baseline-run", "current-run"} {
		executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: "reschedule.json", Output: name, Expected: identity})
		if executed.State != desktop.Completed || executed.Run == nil || executed.Run.State != durablerun.Passed {
			t.Fatalf("retained execution %s: %+v", name, executed)
		}
	}
	return root, "reschedule.json", "baseline-run", "current-run"
}

func packetRequest(root, spec, baseline, current string) desktop.PacketRequest {
	request := desktop.PacketRequest{Workspace: root, Case: "case", Spec: spec, Current: current}
	if baseline != "" {
		request.Baseline = baseline
		request.BaselineCase = "case"
	}
	return request
}

func shaOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// reseal recomputes a packet's manifest index, canonical manifest bytes and
// completion record from the files on disk, the way a coherent re-seal would.
// Verification must still refuse what the re-seal cannot make true.
func reseal(t *testing.T, packet string, mutate func(*report.RetainedManifest)) {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(packet, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(packet, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(name)
		if slash == "manifest.json" || slash == "identity.sha256" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[slash] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(packet, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest report.RetainedManifest
	if json.Unmarshal(raw, &manifest) != nil {
		t.Fatal("the stored manifest could not be read back")
	}
	if mutate != nil {
		mutate(&manifest)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	manifest.Files = make([]bundle.Payload, 0, len(names))
	for _, name := range names {
		manifest.Files = append(manifest.Files, bundle.Payload{Path: name, Size: len(files[name]), SHA256: shaOf(files[name])})
	}
	encoded, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(packet, "manifest.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packet, "identity.sha256"), []byte(shaOf(encoded)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPacketPreviewReportsInputsBoundariesAndProblems(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)

	preview := app.PreviewPacket(packetRequest(root, spec, baseline, current))
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("preview: %+v", preview)
	}
	p := preview.Preview
	if !p.BaselineSupplied || p.Case == nil || p.Spec == nil || p.Current == nil || p.Baseline == nil {
		t.Fatalf("the preview lost an input: %+v", p)
	}
	if !p.Case.Found || !p.Case.CaseMatch || p.Case.Identity == "" {
		t.Fatalf("the case was not verified: %+v", p.Case)
	}
	if !p.Spec.Found || !p.Spec.SpecMatch {
		t.Fatalf("the historical specification did not match what the run retained: %+v", p.Spec)
	}
	if !p.Current.Found || p.Current.Status != string(testrunner.Pass) || p.Current.Boundary != testrunner.ACKBoundary {
		t.Fatalf("the current result was not reported with its boundary: %+v", p.Current)
	}
	if !p.Current.Durable || p.Current.RunState != string(durablerun.Passed) || p.Current.DeliveryUncertain {
		t.Fatalf("the current lifecycle was not reported: %+v", p.Current)
	}
	if p.Baseline.ResultIdentity == p.Current.ResultIdentity {
		t.Fatalf("the baseline was not a distinct retained execution: %+v", p)
	}
	if !p.Destination.Generated || !p.Destination.Fresh || !strings.HasPrefix(p.Destination.Name, "packet-") {
		t.Fatalf("the preview did not propose a fresh packet destination: %+v", p.Destination)
	}
	if !p.ContainsSourceValues || p.ExportPolicy != "customer-local-only" {
		t.Fatalf("the sensitivity was not stated: %+v", p)
	}
	if len(p.Problems) != 0 || len(p.Limitations) == 0 {
		t.Fatalf("problems and limitations: %+v", p)
	}
	if _, err := os.Lstat(filepath.Join(root, p.Destination.Name)); !os.IsNotExist(err) {
		t.Fatal("the preview wrote a destination")
	}

	single := app.PreviewPacket(packetRequest(root, spec, "", current))
	if single.State != desktop.Completed || single.Preview == nil || single.Preview.BaselineSupplied {
		t.Fatalf("single-run preview: %+v", single)
	}
	stated := false
	for _, limitation := range single.Preview.Limitations {
		stated = stated || strings.Contains(limitation, "No observed baseline")
	}
	if !stated {
		t.Fatalf("an absent baseline was not stated as absent: %+v", single.Preview.Limitations)
	}
}

func TestPacketPreviewShowsMissingAndMismatchedInputs(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)

	// A missing baseline is shown as missing, never invented.
	absent := app.PreviewPacket(packetRequest(root, spec, "absent-run", current))
	if absent.State != desktop.Completed || absent.Preview == nil || absent.Preview.Baseline == nil {
		t.Fatalf("absent baseline preview: %+v", absent)
	}
	if absent.Preview.Baseline.Found || len(absent.Preview.Baseline.Problems) == 0 {
		t.Fatalf("a missing baseline was not shown: %+v", absent.Preview.Baseline)
	}
	if len(absent.Preview.Problems) == 0 {
		t.Fatalf("the missing baseline carried no problem: %+v", absent.Preview)
	}

	// A specification rewritten after the runs is not silently substituted.
	writeAckSpec(t, root, spec, "AE")
	changedPreview := app.PreviewPacket(packetRequest(root, spec, baseline, current))
	if changedPreview.State != desktop.Completed || changedPreview.Preview == nil {
		t.Fatalf("changed spec preview: %+v", changedPreview)
	}
	if changedPreview.Preview.Spec.SpecMatch || len(changedPreview.Preview.Spec.Problems) == 0 {
		t.Fatalf("a mismatched historical specification was not named: %+v", changedPreview.Preview.Spec)
	}
	if !strings.Contains(strings.Join(changedPreview.Preview.Spec.Problems, " "), "never substitutes") {
		t.Fatalf("the refusal did not say the spec is never substituted: %+v", changedPreview.Preview.Spec.Problems)
	}
	if len(changedPreview.Preview.Problems) == 0 {
		t.Fatalf("the mismatch did not surface as a problem: %+v", changedPreview.Preview)
	}

	// An unrelated case is named, not accepted.
	writeCase(t, root, "other", framed("MSH|^~\\&|OTHER|SITE|||20260101000000||ADT^A01|OTHER|P|2.5.1\r"))
	other := packetRequest(root, spec, baseline, current)
	other.Case = "other"
	otherPreview := app.PreviewPacket(other)
	if otherPreview.State != desktop.Completed || otherPreview.Preview == nil {
		t.Fatalf("other case preview: %+v", otherPreview)
	}
	if otherPreview.Preview.Case.CaseMatch || len(otherPreview.Preview.Case.Problems) == 0 {
		t.Fatalf("an unrelated case was not named: %+v", otherPreview.Preview.Case)
	}

	// The current run relabelled as the baseline is refused before assembly.
	samePreview := app.PreviewPacket(packetRequest(root, spec, current, current))
	if samePreview.State != desktop.Completed || samePreview.Preview == nil {
		t.Fatalf("same-run preview: %+v", samePreview)
	}
	found := false
	for _, problem := range samePreview.Preview.Problems {
		found = found || strings.Contains(problem, "distinct retained execution")
	}
	if !found {
		t.Fatalf("the current result stood in for a baseline: %+v", samePreview.Preview.Problems)
	}

	// Entries that are not executions are named as such, and a workspace
	// escape is refused outright.
	notARun := app.PreviewPacket(packetRequest(root, spec, "", "case"))
	if notARun.State != desktop.Completed || notARun.Preview == nil || notARun.Preview.Current.Found {
		t.Fatalf("a case was previewed as an execution: %+v", notARun)
	}
	escape := app.PreviewPacket(packetRequest(root, spec, "", "../outside"))
	if escape.State != desktop.Failed {
		t.Fatalf("a workspace escape was previewed: %+v", escape)
	}
}

func TestPacketAssembleVerifiesReadsBackAndRegisters(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)

	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("assemble: %+v", assembled)
	}
	packet := assembled.Packet
	if packet.Identity == "" || packet.Entry != "packet-001" || packet.Schema != report.RetainedSchema {
		t.Fatalf("the assembled packet lost its identity: %+v", packet)
	}
	if packet.Current.Status != string(testrunner.Pass) || packet.Baseline == nil || packet.Baseline.Status != string(testrunner.Pass) {
		t.Fatalf("the outcomes were not read back: %+v", packet)
	}
	if packet.Current.CaseProvenance != "imported" || packet.Current.Boundary != testrunner.ACKBoundary {
		t.Fatalf("the provenance and boundary were not carried: %+v", packet)
	}
	if len(packet.Files) == 0 || !packet.ContainsSourceValues {
		t.Fatalf("the inventory or sensitivity was not carried: %+v", packet)
	}

	// Reading the packet back by its registered name verifies the same seal.
	reopened := app.OpenPacket(root, packet.Entry)
	if reopened.State != desktop.Completed || reopened.Packet == nil || reopened.Packet.Identity != packet.Identity {
		t.Fatalf("reopen: %+v", reopened)
	}
	if len(reopened.Packet.Files) != len(packet.Files) {
		t.Fatalf("the inventory changed between assembly and reopen: %+v", reopened.Packet)
	}

	// The packet is registered in the workspace listing as its own kind.
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed || listing.Workspace == nil {
		t.Fatalf("listing: %+v", listing)
	}
	kind := ""
	for _, artifact := range listing.Workspace.Artifacts {
		if artifact.Name == packet.Entry {
			kind = string(artifact.Kind)
		}
	}
	if kind != string(desktop.PacketArtifact) {
		t.Fatalf("the assembled packet did not register as a packet: %q", kind)
	}

	// The command line's own verifier accepts the same packet.
	var stdout, stderr strings.Builder
	if err := cli.Execute("dev", []string{"report", "verify-retained", filepath.Join(root, packet.Entry)}, &stdout, &stderr); err != nil {
		t.Fatalf("verify-retained: %v %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), packet.Identity) {
		t.Fatalf("the command line verified a different identity: %q", stdout.String())
	}

	// Without a baseline the packet says so, honestly and on both sides.
	single := app.AssemblePacket(packetRequest(root, spec, "", current))
	if single.State != desktop.Completed || single.Packet == nil || single.Packet.Baseline != nil {
		t.Fatalf("single-run assemble: %+v", single)
	}
	stated := false
	for _, limitation := range single.Packet.Limitations {
		stated = stated || strings.Contains(limitation, "No observed baseline")
	}
	if !stated {
		t.Fatal("the single-run packet did not state its missing baseline")
	}
	if err := cli.Execute("dev", []string{"report", "verify-retained", filepath.Join(root, single.Packet.Entry)}, &stdout, &stderr); err != nil {
		t.Fatalf("verify single-run packet: %v %s", err, stderr.String())
	}
}

func TestPacketAssembleRefusalsLeavePartialOutputsExplicitlyIncomplete(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)

	// A destination that already exists is refused before anything is written,
	// and what it holds is not touched.
	if err := os.Mkdir(filepath.Join(root, "taken"), 0700); err != nil {
		t.Fatal(err)
	}
	collision := packetRequest(root, spec, baseline, current)
	collision.Output = "taken"
	if refused := app.AssemblePacket(collision); refused.State != desktop.Failed || refused.Packet != nil {
		t.Fatalf("an existing destination was assembled into: %+v", refused)
	}
	if entries := bytesUnder(t, filepath.Join(root, "taken")); len(entries) != 0 {
		t.Fatalf("the collision wrote into the destination: %v", entries)
	}

	// A specification rewritten after the runs is refused by the operation
	// itself; the partial destination it leaves never verifies as complete.
	writeAckSpec(t, root, spec, "AE")
	mismatch := packetRequest(root, spec, baseline, current)
	mismatch.Output = "mismatched"
	refused := app.AssemblePacket(mismatch)
	if refused.State != desktop.Failed || refused.Packet != nil {
		t.Fatalf("a mismatched historical specification assembled: %+v", refused)
	}
	if _, err := os.Lstat(filepath.Join(root, "mismatched")); err == nil {
		t.Log("the refusal left a partial destination behind")
	}
	if verified := app.OpenPacket(root, "mismatched"); verified.State == desktop.Completed {
		t.Fatalf("a partial destination verified: %+v", verified)
	}

	// The same selection assembles again once the spec matches what the run
	// retained: recovery is a new destination, never an overwrite.
	writeAckSpec(t, root, spec, "AA")
	retry := packetRequest(root, spec, baseline, current)
	retry.Output = "packet-two"
	if second := app.AssemblePacket(retry); second.State != desktop.Completed || second.Packet == nil {
		t.Fatalf("retry after refusal: %+v", second)
	}
}

func TestPacketOpenRefusesCorruptionAndUnsupportedVersions(t *testing.T) {
	app := workspaceApp(t)
	root, spec, baseline, current := packetWorkspace(t, app)

	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("assemble: %+v", assembled)
	}

	// An incomplete packet — its completion record missing — is never read as
	// complete evidence.
	incomplete := filepath.Join(t.TempDir(), "incomplete")
	if err := os.CopyFS(incomplete, os.DirFS(filepath.Join(root, assembled.Packet.Entry))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(incomplete, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if opened := app.OpenPacket(filepath.Dir(incomplete), filepath.Base(incomplete)); opened.State == desktop.Completed {
		t.Fatalf("an incomplete packet opened: %+v", opened)
	}

	// Changed content is refused even before the seal is consulted.
	altered := filepath.Join(t.TempDir(), "altered")
	if err := os.CopyFS(altered, os.DirFS(filepath.Join(root, assembled.Packet.Entry))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(altered, "SUMMARY.md"), []byte("Manufactured passing baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	if opened := app.OpenPacket(filepath.Dir(altered), filepath.Base(altered)); opened.State == desktop.Completed {
		t.Fatalf("an altered packet opened: %+v", opened)
	}

	// A packet whose retained engine pin names versions this release cannot
	// read is refused even when its hashes are coherently recomputed.
	repinned := filepath.Join(t.TempDir(), "repinned")
	if err := os.CopyFS(repinned, os.DirFS(filepath.Join(root, assembled.Packet.Entry))); err != nil {
		t.Fatal(err)
	}
	pin, err := engine.Encode(engine.Pin{Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: "readmit-test/v2", Profile: observation.Profile})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repinned, "current", "engine.json"), pin, 0o600); err != nil {
		t.Fatal(err)
	}
	reseal(t, repinned, nil)
	if opened := app.OpenPacket(filepath.Dir(repinned), filepath.Base(repinned)); opened.State == desktop.Completed {
		t.Fatalf("a packet pinned to unsupported versions opened: %+v", opened)
	}

	// An unknown packet contract is unsupported, never read as v1.
	renamed := filepath.Join(t.TempDir(), "renamed")
	if err := os.CopyFS(renamed, os.DirFS(filepath.Join(root, assembled.Packet.Entry))); err != nil {
		t.Fatal(err)
	}
	reseal(t, renamed, func(m *report.RetainedManifest) { m.Schema = "readmit-retained-packet/v2" })
	if opened := app.OpenPacket(filepath.Dir(renamed), filepath.Base(renamed)); opened.State == desktop.Completed {
		t.Fatalf("an unsupported packet contract opened: %+v", opened)
	}
}

func TestPacketExportSealsRenderingsAndReviewsReopenReadOnly(t *testing.T) {
	app := newApp(t, &chooser{})
	root, spec, baseline, current := packetWorkspace(t, app)
	// The review is exported beside the packet, as an entry of the workspace.
	destination := filepath.Join(root, "review")
	app2 := newApp(t, &chooser{destination: destination})
	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("assemble: %+v", assembled)
	}

	// The new folder named natively is a destination only.
	chosen := app2.ChoosePacketExportPath()
	if chosen.State != desktop.Completed || chosen.Path != destination {
		t.Fatalf("choose: %+v", chosen)
	}
	dismissed := newApp(t, &chooser{}).ChoosePacketExportPath()
	if dismissed.State != desktop.Cancelled {
		t.Fatalf("a dismissed destination dialog was not cancelled: %+v", dismissed)
	}

	exported := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: assembled.Packet.Entry, Destination: destination})
	if exported.State != desktop.Completed || exported.Identity == "" {
		t.Fatalf("export: %+v", exported)
	}
	if exported.PacketIdentity != assembled.Packet.Identity || exported.ContainsSourceValues != true {
		t.Fatalf("the review lost its packet binding or sensitivity: %+v", exported)
	}
	if len(exported.Formats) != 5 {
		t.Fatalf("the five offline renderings were not all named: %+v", exported.Formats)
	}
	for _, rendering := range []string{"report.html", "report.pdf", "report.md", "report.json", "junit.xml"} {
		if _, err := os.Lstat(filepath.Join(destination, rendering)); err != nil {
			t.Fatalf("rendering %s: %v", rendering, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(destination, "packet", "manifest.json")); err != nil {
		t.Fatalf("the nested packet: %v", err)
	}

	// The review sits wherever the operator chose; the packet itself is what
	// registers in the workspace listing.
	if kind := listingKind(t, app, root, assembled.Packet.Entry); kind != string(desktop.PacketArtifact) {
		t.Fatalf("packet kind: %q", kind)
	}

	// Read-only open: verification metadata without the sensitive text.
	hidden := app.OpenPacketReview(desktop.PacketReviewRequest{Workspace: root, Entry: filepath.Base(destination)})
	if hidden.State != desktop.Completed || hidden.Review == nil {
		t.Fatalf("review open: %+v", hidden)
	}
	if hidden.Review.Identity != exported.Identity || hidden.Review.PacketIdentity != assembled.Packet.Identity {
		t.Fatalf("the review open lost the identities: %+v", hidden.Review)
	}
	if len(hidden.Review.Renderings) != 5 || hidden.Review.Current != string(testrunner.Pass) {
		t.Fatalf("the renderings or run status were not reported: %+v", hidden.Review)
	}
	if hidden.Review.Revealed || len(hidden.Review.Lines) != 0 {
		t.Fatalf("report text crossed the boundary without the deliberate reveal: %+v", hidden.Review)
	}
	revealed := app.OpenPacketReview(desktop.PacketReviewRequest{Workspace: root, Entry: filepath.Base(destination), Reveal: true})
	if revealed.State != desktop.Completed || !revealed.Review.Revealed || len(revealed.Review.Lines) == 0 {
		t.Fatalf("the deliberate reveal produced nothing: %+v", revealed)
	}

	// The command line verifies the same review read-only and renders the same
	// five formats from it.
	var stdout, stderr strings.Builder
	if err := cli.Execute("dev", []string{"report", "review", destination}, &stdout, &stderr); err != nil {
		t.Fatalf("review: %v %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), exported.Identity) {
		t.Fatalf("the command line verified a different review identity: %q", stdout.String())
	}
	stdout.Reset()
	if err := cli.Execute("dev", []string{"report", "review", destination, "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("review json: %v %s", err, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "{") {
		t.Fatal("the rendered strict JSON report was not written")
	}

	// An existing destination is refused; a corrupted packet exports nothing
	// that verifies, and the partial destination stays explicitly incomplete.
	if second := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: assembled.Packet.Entry, Destination: destination}); second.State != desktop.Failed {
		t.Fatalf("an existing destination was exported into: %+v", second)
	}
	// A packet in the workspace whose bytes no longer verify is assembled as
	// any other, then altered; exporting it produces nothing that verifies.
	if corrupt := app.AssemblePacket(func() desktop.PacketRequest {
		request := packetRequest(root, spec, baseline, current)
		request.Output = "packet-corrupt"
		return request
	}()); corrupt.State != desktop.Completed || corrupt.Packet == nil {
		t.Fatalf("assemble corruptible packet: %+v", corrupt)
	}
	if err := os.WriteFile(filepath.Join(root, "packet-corrupt", "SUMMARY.md"), []byte("Manufactured"), 0o600); err != nil {
		t.Fatal(err)
	}
	corruptDestination := filepath.Join(root, "corrupt-review")
	if exported := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: "packet-corrupt", Destination: corruptDestination}); exported.State == desktop.Completed {
		t.Fatalf("a corrupted packet exported: %+v", exported)
	}
	// Whatever the failed export left behind is not a review, by the command
	// line's own read-only reader.
	var reviewOut, reviewErr strings.Builder
	if err := cli.Execute("dev", []string{"report", "review", corruptDestination}, &reviewOut, &reviewErr); err == nil {
		t.Fatal("a partial portable review verified")
	}
}

func listingKind(t *testing.T, app *desktop.App, root, name string) string {
	t.Helper()
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed || listing.Workspace == nil {
		t.Fatalf("listing: %+v", listing)
	}
	for _, artifact := range listing.Workspace.Artifacts {
		if artifact.Name == name {
			return string(artifact.Kind)
		}
	}
	return ""
}

// Packet reads are capability-free, exactly as the command line's report
// commands are: an installation with no operation policy at all can still
// preview, assemble, verify and review evidence, while a send stays refused.
func TestPacketOperationsAcquireNoSendOrMutationAuthority(t *testing.T) {
	state := t.TempDir()
	author := activatedApp(t, &chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))
	root, spec, baseline, current := packetWorkspace(t, author)

	viewer := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(t.TempDir(), "drafts.json"))

	if preview := viewer.PreviewPacket(packetRequest(root, spec, baseline, current)); preview.State != desktop.Completed {
		t.Fatalf("a policy-less viewer could not preview: %+v", preview)
	}
	assembled := viewer.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("a policy-less viewer could not assemble a local copy: %+v", assembled)
	}
	if opened := viewer.OpenPacket(root, assembled.Packet.Entry); opened.State != desktop.Completed {
		t.Fatalf("a policy-less viewer could not verify a packet: %+v", opened)
	}

	// A review exported beside the packet registers as its own entry of the
	// same workspace; one exported elsewhere is opened by opening the folder
	// that holds it, exactly the offline recipient's journey.
	destination := filepath.Join(root, "review")
	if exported := viewer.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: assembled.Packet.Entry, Destination: destination}); exported.State != desktop.Completed {
		t.Fatalf("a policy-less viewer could not export: %+v", exported)
	}
	if review := viewer.OpenPacketReview(desktop.PacketReviewRequest{Workspace: root, Entry: "review", Reveal: true}); review.State != desktop.Completed || len(review.Review.Lines) == 0 {
		t.Fatalf("a policy-less viewer could not open the review read-only: %+v", review)
	}
	if kind := listingKind(t, viewer, root, "review"); kind != string(desktop.PortableReviewArtifact) {
		t.Fatalf("the portable review did not register: %q", kind)
	}
	// The same policy-less viewer still cannot send: reading never acquires
	// execution authority.
	identity := preflighted(t, viewer, desktop.RunPreflightRequest{Workspace: root, Spec: spec})
	if executed := viewer.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: spec, Output: "another-run", Expected: identity}); executed.State != desktop.PermissionDenied {
		t.Fatalf("opening packets acquired send authority: %+v", executed)
	}
}

// A cancel action names its own operation: cancelling the packet operation
// while a durable run executes cannot stop the run.
func TestPacketCancelNameCannotReachADifferentOperation(t *testing.T) {
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
		t.Fatal("nothing was sent")
	}
	// The packet panels' cancel cannot stop another panel's work.
	app.Cancel(packetOperationName)
	result := <-done
	if result.State != desktop.Completed {
		t.Fatalf("the packet cancel stopped a durable run: %+v", result)
	}
	app.Cancel("durable-run")
	app.Cancel("")
}

// packetOperationName is the operation name the packet panels cancel through;
// duplicated here so the test reads the same sentence the panel does.
const packetOperationName = "packet"
