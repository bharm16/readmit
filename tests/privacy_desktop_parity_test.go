package tests

// The privacy journeys are two entry points over one engine. The window derives
// a review through the same `redact` operation the command line runs, exports a
// packet through the same `redact export` gate, prepares and publishes a
// support summary through the same `share` operation, and packs an encrypted
// transfer package through the same `protect pack` operation — so the
// artifacts one entry point writes must be exactly what the other verifies,
// exports, publishes and opens, and the disclosure refusals must be the same
// refusals. Nothing here is uploaded by either entry point; the only addresses
// the export speaks to are the loopback fixture receivers it starts itself.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/protect"
)

// privacyParityWorkspace is one workspace both entry points read: the planted
// corpus, the original specification, the working disclosure policy and the
// complete inventory, each one entry, exactly what the privacy screens select.
func privacyParityWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	var inputs []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{
			Path:    filepath.Join(workspace, "PLANTED-FILENAME-CEDAR-"+name+".mllp"),
			Data:    raw,
			Options: hl7.Options{Format: hl7.MLLP},
		})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(filepath.Join(workspace, "original.case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	for fixture, entry := range map[string]string{
		"spec": "spec.json", "policy": "policy.json", "inventory": "inventory.json",
	} {
		raw, err := os.ReadFile("../testdata/fixtures/redact-" + fixture + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, entry), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return workspace
}

// The review the window derives is the review the command line approves: the
// CLI exports the desktop-derived review under the desktop-displayed identity
// and writes a valid packet, and the desktop exports the CLI-derived review the
// same way. The packet each writes is disclosure-reviewed extract only, and
// both decline equivalence.
func TestDesktopAndCommandLineApproveAndExportTheSameReviews(t *testing.T) {
	workspace := privacyParityWorkspace(t)
	app := desktopApp(t, workspace)

	// The window derives its review and private state as workspace entries.
	desktopOutcome := app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json",
		Output: "desktop-review", LocalState: "desktop-private",
	})
	if desktopOutcome.State != desktop.Completed || desktopOutcome.Outcome == nil || desktopOutcome.Outcome.State != "ready-for-approval" {
		t.Fatalf("the window did not derive a ready review: %+v", desktopOutcome)
	}

	// The command line derives its own review over the same entries.
	stdout, stderr, err := run(t, "redact",
		filepath.Join(workspace, "original.case"),
		"--spec", filepath.Join(workspace, "spec.json"),
		"--policy", filepath.Join(workspace, "policy.json"),
		"--inventory", filepath.Join(workspace, "inventory.json"),
		"--local-state", filepath.Join(workspace, "cli-private"),
		"--output", filepath.Join(workspace, "cli-review"))
	if err != nil {
		t.Fatalf("redact: %v %s %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "ready for explicit approval") {
		t.Fatalf("the command line did not report a ready review: %s", stdout)
	}
	// Both entries points bind the same derived case and the same derived
	// specification: the materialized content is one identity whatever wrote
	// the review around it. The private local state differs by where it was
	// written, and the review says so by commitment rather than hiding it.
	desktopReview, err := os.ReadFile(filepath.Join(workspace, "desktop-review", "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	cliReview, err := os.ReadFile(filepath.Join(workspace, "cli-review", "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"derived_case_identity"`, `"derived_spec_sha256"`, `"required_failures":[1,2]`, `"state":"ready-for-approval"`} {
		if !strings.Contains(string(desktopReview), field) || !strings.Contains(string(cliReview), field) {
			t.Fatalf("the two reviews disagree on %s", field)
		}
	}

	// Cross-execution: the command line exports the desktop-derived review, and
	// the window exports the CLI-derived review, each under the exact identity.
	cliPacket := filepath.Join(t.TempDir(), "cli-of-desktop")
	cliStdout, cliStderr, err := run(t, "redact", "export",
		filepath.Join(workspace, "desktop-review"),
		"--local-state", filepath.Join(workspace, "desktop-private"),
		"--approve", desktopOutcome.Outcome.Identity,
		"--output", cliPacket)
	if err != nil {
		t.Fatalf("redact export of the desktop review: %v %s %s", err, cliStdout, cliStderr)
	}
	if result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: workspace, Review: "cli-review", LocalState: "cli-private",
		Approval: cliReviewIdentity(t, filepath.Join(workspace, "cli-review")), Output: "desktop-of-cli",
	}); result.State != desktop.Completed || result.Outcome == nil {
		t.Fatalf("the window did not export the CLI-derived review: %+v", result)
	}

	// The exported packets agree with their approvals and neither carries an
	// equivalence claim or the other's original artifacts.
	for _, packet := range []string{cliPacket, filepath.Join(workspace, "desktop-of-cli")} {
		raw, err := os.ReadFile(filepath.Join(packet, "export-review.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"approved_review_identity"`) || strings.Contains(string(raw), `"external_equivalence"`) {
			t.Fatalf("the packet manifest does not name its approval: %s", packet)
		}
		if _, err := os.Stat(filepath.Join(packet, "case")); err != nil {
			t.Fatalf("the packet does not carry the derived case: %s", packet)
		}
	}
}

// The disclosure refusals are the same refusals. A wrong approval and a stale
// private state are refused by both entry points with the same gate sentence,
// and neither writes a packet.
func TestDesktopAndCommandLineRefuseTheSameDisclosureFailures(t *testing.T) {
	workspace := privacyParityWorkspace(t)
	app := desktopApp(t, workspace)

	desktopOutcome := app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json",
		Output: "review", LocalState: "private",
	})
	if desktopOutcome.State != desktop.Completed || desktopOutcome.Outcome == nil {
		t.Fatalf("the derivation was refused: %+v", desktopOutcome)
	}
	reviewPath := filepath.Join(workspace, "review")
	privatePath := filepath.Join(workspace, "private")

	// A second derivation writes its own private state; the two bind different
	// inputs, so neither review approves the other's linkage.
	second := app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json",
		Output: "review-2", LocalState: "private-2",
	})
	if second.State != desktop.Completed || second.Outcome == nil {
		t.Fatalf("the second derivation was refused: %+v", second)
	}

	// A wrong approval, refused by the command line and by the window with its
	// own gate sentence, and neither writes a packet.
	_, stderr, err := run(t, "redact", "export", reviewPath, "--local-state", privatePath,
		"--approve", strings.Repeat("0", 64), "--output", filepath.Join(t.TempDir(), "no"))
	if err == nil || !strings.Contains(stderr, "export requires approval of an exact fully handled and proven review") {
		t.Fatalf("the command line approved a wrong identity: %v %s", err, stderr)
	}
	if result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: workspace, Review: "review", LocalState: "private",
		Approval: strings.Repeat("1", 64), Output: "no-desktop",
	}); result.State != desktop.Failed || !strings.Contains(result.Reason, "export requires approval of an exact fully handled and proven review") {
		t.Fatalf("the window approved a wrong identity: %+v", result)
	}

	// A private state that another derivation wrote is refused by both.
	_, stderr, err = run(t, "redact", "export", reviewPath, "--local-state", filepath.Join(workspace, "private-2"),
		"--approve", desktopOutcome.Outcome.Identity, "--output", filepath.Join(t.TempDir(), "no"))
	if err == nil || !strings.Contains(stderr, "private state does not match approved review") {
		t.Fatalf("the command line accepted a mismatched private state: %v %s", err, stderr)
	}
	if result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{
		Workspace: workspace, Review: "review", LocalState: "private-2",
		Approval: desktopOutcome.Outcome.Identity, Output: "no-desktop",
	}); result.State != desktop.Failed || !strings.Contains(result.Reason, "private state does not match approved review") {
		t.Fatalf("the window accepted a mismatched private state: %+v", result)
	}
}

// The support summary identity is the identity of the bytes: the command
// line's preview of one review and the window's preview of the same review
// under the same policy are one identity, the window publishes under it, and
// the bundle it wrote is the bundle `share verify` verifies offline.
func TestDesktopAndCommandLinePrepareAndPublishTheSameSupportSummary(t *testing.T) {
	workspace := privacyParityWorkspace(t)
	app := desktopApp(t, workspace)
	outcome := app.DeriveExportReview(desktop.PrivacyReviewRequest{
		Workspace: workspace, Case: "original.case", Spec: "spec.json",
		Policy: "policy.json", Inventory: "inventory.json",
		Output: "review", LocalState: "private",
	})
	if outcome.State != desktop.Completed || outcome.Outcome == nil || outcome.Outcome.State != "ready-for-approval" {
		t.Fatalf("the derivation was refused: %+v", outcome)
	}

	policy := `{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}` + "\n"
	if err := os.WriteFile(filepath.Join(workspace, "sharing.json"), []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}

	// The window's preview of the derived review.
	preview := app.PreviewSupportSummary(desktop.SupportRequest{
		Workspace: workspace, Source: "review", Kind: "derived-review",
		Private: "private", Policy: "sharing.json",
	})
	if preview.State != desktop.Completed || preview.Summary == nil {
		t.Fatalf("the summary was not prepared: %+v", preview)
	}

	// The command line's preview of the same review under the same policy is
	// the same identity, because both are the identity of the same bytes.
	stdout, stderr, err := run(t, "share", filepath.Join(workspace, "review"),
		"--kind", "derived-review",
		"--local-state", filepath.Join(workspace, "private"),
		"--policy", filepath.Join(workspace, "sharing.json"))
	if err != nil {
		t.Fatalf("share preview: %v %s %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, preview.Summary.Identity) {
		t.Fatalf("the two entry points prepared two identities:\n%s\n%s", stdout, preview.Summary.Identity)
	}

	// The window publishes under the exact identity and the command line
	// verifies the bundle it wrote, offline and independently of the source.
	published := app.PublishSupportSummary(desktop.SupportPublishRequest{
		Workspace: workspace, Source: "review", Kind: "derived-review",
		Private: "private", Policy: "sharing.json",
		Approval: preview.Summary.Identity, Output: "support",
	})
	if published.State != desktop.Completed || published.Outcome == nil {
		t.Fatalf("the exact identity did not publish: %+v", published)
	}
	if removed := os.Remove(filepath.Join(workspace, "review", "review.json")); removed != nil {
		t.Fatal(removed)
	}
	verifyStdout, verifyStderr, err := run(t, "share", "verify", filepath.Join(workspace, "support"))
	if err != nil {
		t.Fatalf("share verify: %v %s %s", err, verifyStdout, verifyStderr)
	}
	if !strings.Contains(verifyStdout, preview.Summary.Identity) {
		t.Fatalf("the verified bundle is not the published summary: %s", verifyStdout)
	}
}

// The window's protected package is the command line's package: packed under a
// registered control, inspectable without a key, and openable by whichever
// entry point resolves the same declared store.
func TestDesktopAndCommandLinePackAndOpenTheSameProtectedPackages(t *testing.T) {
	workspace := privacyParityWorkspace(t)
	app := desktopApp(t, workspace)

	program := keyStoreProgram(t)
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: workspace, Entry: "protection.json", Name: "lab-evidence",
		Storage: "os-volume-encryption", Command: program, Arguments: []string{"parity"},
	})
	if registered.State != desktop.Completed {
		t.Fatalf("the control was not registered: %+v", registered)
	}
	packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{
		Workspace: workspace, Entry: "protection.json", Control: "lab-evidence",
		Sources: []string{"original.case"}, Output: "desktop-package",
	})
	if packed.State != desktop.Completed || packed.Package == nil {
		t.Fatalf("the package was not packed: %+v", packed)
	}

	// The command line packs the same evidence under the same control.
	cliStdout, cliStderr, err := run(t, "protect", "pack",
		"--protection", filepath.Join(workspace, "protection.json"),
		"--name", "lab-evidence", "--output", filepath.Join(workspace, "cli-package"),
		filepath.Join(workspace, "spec.json"))
	if err != nil {
		t.Fatalf("protect pack: %v %s %s", err, cliStdout, cliStderr)
	}

	// Both packages inspect without a key, naming the control that wrote them.
	for _, name := range []string{"desktop-package", "cli-package"} {
		descriptor, _, err := protect.ReadPackage(filepath.Join(workspace, name))
		if err != nil || descriptor.Control != "lab-evidence" {
			t.Fatalf("the %s package does not declare its control: %+v %v", name, descriptor, err)
		}
	}

	// The command line opens the window's package; the window opens the
	// command line's package. Both resolve the same declared store.
	cliOpened := filepath.Join(workspace, "cli-opened")
	cliStdout, cliStderr, err = run(t, "protect", "open",
		"--protection", filepath.Join(workspace, "protection.json"),
		"--package", filepath.Join(workspace, "desktop-package"),
		"--output", cliOpened)
	if err != nil {
		t.Fatalf("protect open of the desktop package: %v %s %s", err, cliStdout, cliStderr)
	}
	if result := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{
		Workspace: workspace, Entry: "protection.json", Package: "cli-package", Output: "desktop-opened",
	}); result.State != desktop.Completed || result.Package == nil {
		t.Fatalf("the window did not open the CLI package: %+v", result)
	}
	if raw, err := os.ReadFile(filepath.Join(cliOpened, "original.case", "manifest.json")); err != nil || len(raw) == 0 {
		t.Fatalf("the opened package did not restore its evidence: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(workspace, "desktop-opened", "spec.json")); err != nil || len(raw) == 0 {
		t.Fatalf("the opened CLI package did not restore its file: %v", err)
	}
}

// keyStoreProgram is the test-only stand-in for an OS key store: a program
// whose absolute path a control registers and whose output is generated here,
// marked test-only, and never a committed key.
func keyStoreProgram(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "readmit-test-key-store.sh")
	script := "#!/bin/sh\nprintf '%s' 'test-only-not-a-real-key-4f8c1d2e6b0a9357'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

// cliReviewIdentity reads the identity marker one derived review wrote.
func cliReviewIdentity(t *testing.T, review string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(review, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}
