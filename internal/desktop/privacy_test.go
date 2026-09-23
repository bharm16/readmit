package desktop_test

// The privacy journeys: a derived review is the existing redaction operation's
// fail-closed output written from actual workspace entries, an exported packet
// is the existing export operation's freshly generated one under an approval
// that names the exact materialized identity, and a support summary is the
// existing share operation's value-free preview published only under that
// exact identity. Every gate here is the command line's own; what these tests
// prove is that the window reaches the same verified decisions and the same
// disclosure refusals, never invents a shortcut around them, never carries a
// planted value, and never reaches past the folder the person opened.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sharing"
)

// privacyFixture copies the planted-identifier corpus into one workspace: the
// case with its planted source filenames, the original specification, the
// disclosure policy and the complete inventory are each one entry, exactly the
// documents the privacy screen selects. Every planted value below is invented
// synthetic test data.
func privacyFixture(t *testing.T, policyEntry string, policyFixture string) string {
	t.Helper()
	root := t.TempDir()
	var sources []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, bundle.Input{
			Path:    filepath.Join(root, "PLANTED-FILENAME-CEDAR-"+name+".mllp"),
			Data:    raw,
			Options: hl7.Options{Format: hl7.MLLP},
		})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(filepath.Join(root, "original.case"), sources, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	for fixture, entry := range map[string]string{
		"spec": "spec.json", policyFixture: policyEntry, "inventory": "inventory.json",
	} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + fixture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		writeDocument(t, root, entry, string(raw))
	}
	return root
}

// privacyRequest names the entries privacyFixture wrote.
func privacyRequest(root, policyEntry string) desktop.PrivacyReviewRequest {
	return desktop.PrivacyReviewRequest{
		Workspace: root, Case: "original.case", Spec: "spec.json",
		Policy: policyEntry, Inventory: "inventory.json",
		Output: "review", LocalState: "review-private",
	}
}

// derived runs one derivation through the facade and fails the test if the
// operation did not return.
func derived(t *testing.T, app *desktop.App, request desktop.PrivacyReviewRequest) *desktop.PrivacyReviewOutcome {
	t.Helper()
	result := app.DeriveExportReview(request)
	if result.State != desktop.Completed || result.Outcome == nil {
		t.Fatalf("the facade refused a derivation it supports: %+v", result)
	}
	return result.Outcome
}

// readyReview derives one approvable review over the working policy and
// exports its packet under the exact approval, returning the workspace, the
// outcome and the exported packet.
func readyReview(t *testing.T, app *desktop.App) (string, *desktop.PrivacyReviewOutcome, *desktop.PrivacyExportOutcome) {
	t.Helper()
	root := privacyFixture(t, "policy.json", "policy")
	outcome := derived(t, app, privacyRequest(root, "policy.json"))
	if outcome.State != "ready-for-approval" || outcome.Identity == "" || outcome.Unresolved != 0 {
		t.Fatalf("the working policy did not produce an approvable review: %+v", outcome)
	}
	exported := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: root, Review: outcome.Review, LocalState: outcome.Private,
		Approval: outcome.Identity, Output: "packet",
	})
	if exported.State != desktop.Completed || exported.Outcome == nil {
		t.Fatalf("the exact approval did not export the reviewed packet: %+v", exported)
	}
	return root, outcome, exported.Outcome
}

// The whole journey over the working policy: a ready review binds its input,
// private state, derived case and derived specification; the export under the
// exact approval generates a packet whose fresh proof preserved the agreed
// defect; and both establish a disclosure-reviewed extract while explicitly
// declining external regression equivalence.
func TestThePrivacyJourneyDerivesExportsAndDeclinesEquivalence(t *testing.T) {
	app := workspaceApp(t)
	root, review, packet := readyReview(t, app)

	if review.Establishes != desktop.DisclosureReviewed {
		t.Fatalf("a ready review did not name what it establishes: %+v", review)
	}
	if len(review.Limitations) == 0 {
		t.Fatal("the derivation reached the window without its boundary statements")
	}
	if packet.Establishes != desktop.DisclosureReviewed || packet.Equivalence != desktop.DeclinedEquivalence {
		t.Fatalf("the export did not state its own boundary: %+v", packet)
	}
	if packet.ApprovedReview != review.Identity || packet.Files == 0 {
		t.Fatalf("the packet does not carry the exact approved review: %+v", packet)
	}
	if packet.ProofBaseline != "assertion_failure" || packet.ProofPostfix != "pass" {
		t.Fatalf("the fresh fixture proof did not preserve the agreed defect and full fixed pass: %+v", packet)
	}
	// The packet registers as its own entry of the workspace, and so does the
	// review; the private local state was written where the request named it.
	if kind := listingKind(t, app, root, "packet"); kind != string(desktop.DerivedExportArtifact) {
		t.Fatalf("the derived export did not register as what it declares: %q", kind)
	}
	if kind := listingKind(t, app, root, "review"); kind != string(desktop.ReviewArtifact) {
		t.Fatalf("the export review did not register as a review: %q", kind)
	}
}

// A blocked review is the normal first answer, and its blockers are the whole
// inventory: every surface the working export could include stays explicitly
// unresolved, no derived case exists, and the exact identity of a blocked
// review cannot approve it.
func TestABlockedReviewLeavesEveryUnreviewedSurfaceAnExplicitBlocker(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy-blocked")
	outcome := derived(t, app, privacyRequest(root, "policy.json"))

	if outcome.State != "blocked" || outcome.Identity == "" {
		t.Fatalf("a policy that handles nothing produced a review with nothing to approve: %+v", outcome)
	}
	if outcome.Unresolved == 0 || outcome.Unresolved != outcome.Findings {
		t.Fatalf("the blocked review left resolved findings behind: %+v", outcome)
	}
	if outcome.Establishes != "" {
		t.Fatalf("a blocked review established something: %+v", outcome)
	}
	// The blocked review's inventory is read through the same review reader as
	// ever, and it holds the surfaces the corpus plants: the named fields, the
	// free text and embedded payloads, the unknown segment, the source
	// filenames and metadata, the specification literals and the report.
	read := app.OpenReview(desktop.ReviewRequest{Workspace: root, Review: "review", Offset: 0, Limit: desktop.MaxReviewFindings})
	if read.State != desktop.Completed || read.Review == nil {
		t.Fatalf("the blocked review could not be read back: %+v", read)
	}
	surfaces := map[string]bool{}
	for _, surface := range read.Review.Surfaces {
		surfaces[surface.Name+" "+surface.Content] = true
	}
	for _, wanted := range []string{
		"case unmapped-field", "case free-text-or-embedded-payload", "case unknown-segment",
		"case source-filename", "case source-metadata", "spec spec-literal", "packet diagnosis-regeneration-required",
	} {
		if !surfaces[wanted] {
			t.Fatalf("the %q surface was not an explicit blocker: %+v", wanted, surfaces)
		}
	}
	refused := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: root, Review: "review", LocalState: "review-private",
		Approval: outcome.Identity, Output: "packet",
	})
	if refused.State != desktop.Failed || refused.Outcome != nil {
		t.Fatalf("the exact identity approved an incomplete review: %+v", refused)
	}
}

// The original artifacts a complete inventory can declare: one retained
// execution of the case under a replay transformation, and one diagnosis
// carrying a planted finding, written as workspace entries the inventory names
// relatively. The privacy screen reaches nothing outside the open folder.
func privacyOriginalArtifacts(t *testing.T, root string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	plan, err := replay.Prepare(filepath.Join(root, "original.case"), replay.Target{
		Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain",
		ConnectTimeout: "100ms", MessageTimeout: "100ms", MaxACKBytes: 4096,
	}, replay.Options{
		Occurrences:     []string{"s0001-e000001", "s0002-e000001"},
		Transformations: []replay.Transformation{{Name: "rebase-control-ids"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Execute(context.Background(), plan, filepath.Join(root, "original-run")); err != nil {
		t.Fatal(err)
	}
	report, err := diagnose.Run(filepath.Join(root, "original.case"), diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report.Findings = append(report.Findings, diagnose.Finding{ID: "f-planted", Summary: "PLANTED-DIAG-MAPLE"})
	encoded, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "PLANTED-DIAG-FILENAME.json", string(encoded))
	inventory, err := os.ReadFile(filepath.Join(root, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared struct {
		Schema         string           `json:"schema"`
		Complete       bool             `json:"complete"`
		Artifacts      []redactArtifact `json:"artifacts"`
		ResidualValues []string         `json:"residual_values"`
	}
	if json.Unmarshal(inventory, &declared) != nil {
		t.Fatal("the inventory fixture changed shape")
	}
	declared.Artifacts = append(declared.Artifacts,
		redactArtifact{Kind: "run", Path: "original-run"},
		redactArtifact{Kind: "diagnosis-json", Path: "PLANTED-DIAG-FILENAME.json"},
	)
	updated, err := json.Marshal(declared, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "inventory.json", string(updated))
}

// redactArtifact is one original-artifact declaration of the inventory.
type redactArtifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// The original artifacts stay original: the derived review binds them, the
// exported packet carries none of them, and the private local state alone
// retains the linkage and the known residual values. Original-versus-derived
// inspection is deliberate and local: the original case is untouched, and the
// derived case is an ordinary verified bundle beside it.
func TestOriginalArtifactsStayOriginalAndPrivateLinkageStaysPrivate(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	privacyOriginalArtifacts(t, root)
	outcome := derived(t, app, privacyRequest(root, "policy.json"))
	if outcome.State != "ready-for-approval" {
		t.Fatalf("the declared original artifacts did not derive a ready review: %+v", outcome)
	}
	exported := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: root, Review: outcome.Review, LocalState: outcome.Private,
		Approval: outcome.Identity, Output: "packet",
	})
	if exported.State != desktop.Completed || exported.Outcome == nil {
		t.Fatalf("the exact approval did not export: %+v", exported)
	}
	// No original artifact name, planted diagnosis finding or original proof
	// directory is inside the packet; the original case bytes are unchanged.
	// The engine's own fixed vocabulary ("original-run-excluded-and-rerun")
	// is not a path, so the checks match the run's entry name as a path or a
	// quoted value, and the planted names whole.
	for _, name := range []string{"original-run/", "\"original-run\"", "PLANTED-DIAG-FILENAME", "original-proof/", "PLANTED-DIAG-MAPLE"} {
		err := filepath.Walk(filepath.Join(root, "packet"), func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if strings.Contains(filepath.Base(path), name) {
				return errors.New("packet carries " + name)
			}
			if info.Mode().IsRegular() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(raw), name) {
					return errors.New("packet carries " + name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
	}
	// The private state retains the linkage and the residual values; nothing
	// else in the window reads it, and the derived case is registered as
	// derived evidence a deliberate inspector can open.
	private, err := os.ReadFile(filepath.Join(root, "review-private", "state.json"))
	if err != nil || !strings.Contains(string(private), "PLANTED-PATIENT-7391") {
		t.Fatalf("the private state did not retain the linkage locally: %v", err)
	}
	if kind := listingKind(t, app, root, "review-private"); kind == string(desktop.DerivedExportArtifact) || kind == string(desktop.ReviewArtifact) {
		t.Fatalf("the private state does not list as the public artifact it is not: %q", kind)
	}
}

// An approval is the exact identity the bytes have now. A wrong approval, no
// approval, a private state another derivation wrote, and a review edited after
// the approval are four refusals, and none of them is a warning.
func TestExportApprovalIsExactAndInvalidatedByAnyChange(t *testing.T) {
	app := workspaceApp(t)
	root, review, _ := readyReview(t, app)

	for name, request := range map[string]desktop.PrivacyExportRequest{
		"an approval that names nothing": {
			Workspace: root, Review: review.Review, LocalState: review.Private, Approval: "", Output: "packet-x",
		},
		"an approval that names another review": {
			Workspace: root, Review: review.Review, LocalState: review.Private, Approval: strings.Repeat("a", 64), Output: "packet-x",
		},
		"a private state another derivation wrote": {
			Workspace: root, Review: review.Review, LocalState: "absent-private", Approval: review.Identity, Output: "packet-x",
		},
		"a review entry that does not exist": {
			Workspace: root, Review: "absent", LocalState: review.Private, Approval: review.Identity, Output: "packet-x",
		},
	} {
		if result := app.ExportDerivedPacket(request); result.State != desktop.Failed || result.Outcome != nil {
			t.Fatalf("%s was exported: %+v", name, result)
		}
	}

	// Changing any byte of the approved review changes the identity an approval
	// must name: the edited review is refused whole rather than read as the one
	// that was approved.
	document := filepath.Join(root, "review", "review.json")
	raw, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(document, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: root, Review: review.Review, LocalState: review.Private,
		Approval: review.Identity, Output: "packet-x",
	}); result.State != desktop.Failed || result.Outcome != nil {
		t.Fatalf("an edited review was exported under its old approval: %+v", result)
	}
}

// No planted value of the corpus reaches the window, resolved or not, through
// any result the privacy screens produce. Reading a transformed value is the
// inspector over the derived case, deliberately, exactly as everywhere else.
func TestPrivacyResultsNeverCarryAPlantedValue(t *testing.T) {
	app := workspaceApp(t)
	blockedRoot := privacyFixture(t, "policy.json", "policy-blocked")
	blocked := derived(t, app, privacyRequest(blockedRoot, "policy.json"))
	workingRoot := privacyFixture(t, "policy.json", "policy")
	working := derived(t, app, privacyRequest(workingRoot, "policy.json"))
	if working.State != "ready-for-approval" {
		t.Fatalf("the working policy did not produce an approvable review: %+v", working)
	}
	exported := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: workingRoot, Review: working.Review, LocalState: working.Private,
		Approval: working.Identity, Output: "packet",
	})
	if exported.State != desktop.Completed {
		t.Fatalf("the export was refused: %+v", exported)
	}
	encoded, err := json.Marshal([]any{blocked, working, exported})
	if err != nil {
		t.Fatal(err)
	}
	for _, planted := range []string{
		"PLANTED-PATIENT-7391", "PLANTED-NAME-ORCHID", "PLANTED-NTE-ALDER",
		"PLANTED-ERR-BIRCH", "PLANTED-ACK-WILLOW", "PLANTED-EMBEDDED-ELM",
		"PLANTED-UNKNOWN-ASH", "PLANTED-SPEC-JUNIPER", "PLANTED-RESET-SPRUCE",
		"PLANTED-FILENAME-CEDAR",
	} {
		if strings.Contains(string(encoded), planted) {
			t.Fatalf("the window disclosed the planted value %s", planted)
		}
	}
}

// The support journey: a policy is authored through the sharing contract's own
// decoder, the preview is every byte the bundle will hold, publication requires
// the exact preview identity over the current sources, and a published bundle
// verifies offline and refuses anything beyond its three members.
func TestTheSupportJourneyPreviewsPublishesAndVerifies(t *testing.T) {
	app := workspaceApp(t)
	root, review, _ := readyReview(t, app)

	saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "sharing.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 4096,
	})
	if saved.State != desktop.Completed || saved.Policy == nil {
		t.Fatalf("the sharing policy was not authored: %+v", saved)
	}
	if kind := listingKind(t, app, root, "sharing.json"); kind != string(desktop.SharingPolicyArtifact) {
		t.Fatalf("the sharing policy did not register as what it declares: %q", kind)
	}
	preview := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing.json",
	})
	if preview.State != desktop.Completed || preview.Summary == nil {
		t.Fatalf("the summary was not prepared: %+v", preview)
	}
	if preview.Summary.Outcome != "reviewed-extract-only" || preview.Summary.ExternalEquivalence != desktop.DeclinedEquivalence {
		t.Fatalf("the summary did not state its closed outcome and decline: %+v", preview.Summary)
	}
	// A derived review's summary needs the private binding named: without it
	// the summary cannot be prepared, and the refusal says so.
	if missing := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review", Policy: "sharing.json",
	}); missing.State != desktop.Failed || !strings.Contains(missing.Reason, "private local-state entry") {
		t.Fatalf("a derived review was summarized without its private binding: %+v", missing)
	}

	published := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing.json",
		Approval: preview.Summary.Identity, Output: "support",
	})
	if published.State != desktop.Completed || published.Outcome == nil {
		t.Fatalf("the exact preview identity did not publish: %+v", published)
	}
	if len(published.Outcome.Files) != 3 || published.Outcome.Bundle != "support" {
		t.Fatalf("the published bundle is not the closed three-member bundle: %+v", published.Outcome)
	}
	if kind := listingKind(t, app, root, "support"); kind != string(desktop.SupportArtifact) {
		t.Fatalf("the support bundle did not register as what it declares: %q", kind)
	}

	// A stale approval is a refusal, never a warning: publishing under the
	// identity of a summary whose policy has since changed is refused, because
	// the regenerated summary is no longer the one that was approved.
	changed := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "sharing-2.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 2048,
	})
	if changed.State != desktop.Completed {
		t.Fatalf("the second policy was not authored: %+v", changed)
	}
	if stale := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing-2.json",
		Approval: preview.Summary.Identity, Output: "support-2",
	}); stale.State != desktop.Failed || stale.Outcome != nil || !strings.Contains(stale.Reason, "does not name the summary") {
		t.Fatalf("a stale approval published a summary: %+v", stale)
	}
	// A policy that denies support denies preparation.
	denied := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "denied.json", Support: false,
		Destinations: []string{"local-file"}, MaxBytes: 4096,
	})
	if denied.State != desktop.Completed {
		t.Fatalf("the denying policy was not authored: %+v", denied)
	}
	if refused := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "denied.json",
	}); refused.State != desktop.Failed || refused.Summary != nil {
		t.Fatalf("a denying policy prepared a summary: %+v", refused)
	}

	// A native destination chosen outside the workspace receives the bundle
	// under the same exact-approval gate; the share operation reserves it, so a
	// destination inside the source or the private linkage is refused there.
	native := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing.json",
		Approval: preview.Summary.Identity, Output: filepath.Join(t.TempDir(), "native-support"),
	})
	if native.State != desktop.Completed || native.Outcome == nil {
		t.Fatalf("the native destination did not publish: %+v", native)
	}
	if inside := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing.json",
		Approval: preview.Summary.Identity, Output: filepath.Join(root, "review"),
	}); inside.State != desktop.Failed || inside.Outcome != nil {
		t.Fatalf("a bundle was published inside protected evidence: %+v", inside)
	}

	// The published bundle verifies offline, independently of its source, and
	// refuses a bundle that lost a member or gained one.
	verified := app.VerifySupportBundle(root, "support")
	if verified.State != desktop.Completed || verified.Summary == nil || verified.Summary.Identity != preview.Summary.Identity {
		t.Fatalf("the published bundle did not verify to its identity: %+v", verified)
	}
	if err := os.Remove(filepath.Join(root, "support", "event.json")); err != nil {
		t.Fatal(err)
	}
	if broken := app.VerifySupportBundle(root, "support"); broken.State != desktop.Failed || broken.Summary != nil {
		t.Fatalf("an incomplete bundle verified: %+v", broken)
	}
}

// Every operation here runs under the admission the command line declares:
// deriving a review is authoring, and authoring a policy is a write, so a
// viewer with no operation policy is refused all of them with the permission
// state, while reading and preparing stay free.
func TestPrivacyWritesAdmitTheAuthorAndReadsStayFree(t *testing.T) {
	app := workspaceApp(t)
	root, review, _ := readyReview(t, app)
	saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "sharing.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 4096,
	})
	if saved.State != desktop.Completed {
		t.Fatalf("the sharing policy was not authored: %+v", saved)
	}

	viewer := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(t.TempDir(), "drafts.json"))

	request := privacyRequest(root, "policy.json")
	request.Output = "review-viewer"
	request.LocalState = "review-private-viewer"
	if denied := viewer.DeriveExportReview(request); denied.State != desktop.PermissionDenied {
		t.Fatalf("a policy-less viewer derived a review: %+v", denied)
	}
	if denied := viewer.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "viewer.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 4096,
	}); denied.State != desktop.PermissionDenied {
		t.Fatalf("a policy-less viewer authored a policy: %+v", denied)
	}
	if preview := viewer.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "sharing.json",
	}); preview.State != desktop.Completed || preview.Summary == nil {
		t.Fatalf("a policy-less viewer could not prepare the free preview: %+v", preview)
	}
}

// No operation of this journey resolves a name. The resolver is replaced by
// one that refuses every lookup and counts them, and the journey runs whole:
// a derivation, an export, a policy authoring, a summary preparation,
// publication and verification happen without a single lookup. The only
// addresses the export ever speaks to are the loopback fixture receivers it
// started itself, and their configuration is inside the packet it wrote.
func TestThePrivacyJourneyMakesNoNameLookupsAndRecordsLoopbackOnly(t *testing.T) {
	lookups := 0
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			lookups++
			return nil, errors.New("lookup refused")
		},
	}
	defer func() { net.DefaultResolver = previous }()

	app := workspaceApp(t)
	root, review, packet := readyReview(t, app)
	if packet.ApprovedReview != review.Identity {
		t.Fatalf("the export did not carry the approved review: %+v", packet)
	}

	saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "support-policy.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 4096,
	})
	if saved.State != desktop.Completed {
		t.Fatalf("the policy was not authored: %+v", saved)
	}
	summary := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "support-policy.json",
	})
	if summary.State != desktop.Completed || summary.Summary == nil {
		t.Fatalf("the preview did not prepare: %+v", summary)
	}
	published := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: root, Source: review.Review, Kind: "derived-review",
		Private: review.Private, Policy: "support-policy.json",
		Approval: summary.Summary.Identity, Output: "support",
	})
	if published.State != desktop.Completed {
		t.Fatalf("the summary did not publish: %+v", published)
	}
	if verified := app.VerifySupportBundle(root, "support"); verified.State != desktop.Completed {
		t.Fatalf("the bundle did not verify: %+v", verified)
	}
	if lookups != 0 {
		t.Fatalf("the privacy journey made %d name lookups", lookups)
	}
	// The proof targets the export recorded are loopback fixture receivers,
	// by the packet's own retained configuration.
	for _, mode := range []string{"baseline", "postfix"} {
		raw, err := os.ReadFile(filepath.Join(root, "packet", "proof", mode, "target.json"))
		if err != nil {
			t.Fatal(err)
		}
		var target struct {
			Address string `json:"address"`
		}
		if json.Unmarshal(raw, &target) != nil || !strings.HasPrefix(target.Address, "127.0.0.1:") {
			t.Fatalf("the %s proof target is not a loopback fixture receiver: %s", mode, target.Address)
		}
	}
}

// A saved sharing policy reopens as exactly what the sharing contract's own
// decoder reads from the bytes on disk — the command line's `share` reads the
// same document the same way — and an entry that is not one is refused.
func TestASavedSharingPolicyReopensAsTheSharedDecoderReadsIt(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{
		Workspace: root, Output: "sharing.json", Support: true,
		Destinations: []string{"local-file", "customer-hub-download"}, MaxBytes: 2048,
	})
	if saved.State != desktop.Completed {
		t.Fatalf("the sharing policy was not authored: %+v", saved)
	}
	data, err := os.ReadFile(filepath.Join(root, "sharing.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := sharing.DecodePolicy(data)
	if err != nil {
		t.Fatalf("the saved policy is not one the shared decoder reads: %v", err)
	}
	read := app.ReadSharingPolicy(root, "sharing.json")
	if read.State != desktop.Completed || read.Policy == nil {
		t.Fatalf("the saved policy did not reopen: %+v", read)
	}
	want := desktop.SupportPolicy{Entry: "sharing.json", Schema: decoded.Schema, Support: decoded.Support, Destinations: decoded.Destinations, MaxBytes: decoded.MaxBytes}
	if got := *read.Policy; got.Entry != want.Entry || got.Schema != want.Schema || got.Support != want.Support ||
		strings.Join(got.Destinations, ",") != strings.Join(want.Destinations, ",") || got.MaxBytes != want.MaxBytes || got.MaxBytes != 2048 {
		t.Fatalf("reopened %+v, the shared decoder read %+v", got, want)
	}
	if err := os.WriteFile(filepath.Join(root, "other.json"), []byte(`{"schema":"readmit-something-else/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if refused := app.ReadSharingPolicy(root, "other.json"); refused.State != desktop.Failed || refused.Policy != nil {
		t.Fatalf("an entry that is not a sharing policy was read as one: %+v", refused)
	}
}
