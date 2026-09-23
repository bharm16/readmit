//go:build !windows

package desktop_test

// The facade is the boundary, not the listing or the file dialog that would
// have offered an entry: a caller can name any entry directly. Every entry an
// operation reads by name is therefore held to the rule the listing and
// ChooseRunSpec apply — one regular file or one real folder of the open
// workspace, never a symbolic link wherever it points, never a `..` or
// absolute escape — and refused before its target is read or anything is
// sent. Each hostile entry below leads to something that would really be
// read, run or sent, so only the rule can refuse it.

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

// plantHostile puts a folder and a FIFO in the workspace, and for each named
// entry, which both folders hold, a link to the one outside and a link to the
// workspace's own.
func plantHostile(t *testing.T, root, outside string, names ...string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "fifo.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for link, target := range map[string]string{"link-" + name: filepath.Join(outside, name), "alias-" + name: filepath.Join(root, name)} {
			if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// hostileDocuments names document, which both folders hold, in every way the
// rule refuses: a link out of the workspace, a link to the workspace's own
// entry, a `..` escape, an absolute path inside and outside the workspace, a
// FIFO and a folder.
func hostileDocuments(root, outside, document string) map[string]string {
	return map[string]string{
		"a symbolic link out of the workspace":         "link-" + document,
		"a symbolic link to an entry of the workspace": "alias-" + document,
		"a `..` escape": filepath.Join("..", "outside", document),
		"an absolute path to the workspace's entry": filepath.Join(root, document),
		"an absolute path outside the workspace":    filepath.Join(outside, document),
		"a FIFO":                                    "fifo.json",
		"a folder":                                  "folder.json",
	}
}

// refused is what one call handed a hostile entry answered.
type refused struct {
	state  desktop.State
	reason string
}

// confinedMember is one operation member handed every entry in entries. The
// refusal must be one of reasons: the operation's own sentences for an entry
// that is not one of the open workspace, which it states before it reads the
// entry, so the refusal cannot have come from a reader that followed it first.
type confinedMember struct {
	name    string
	reasons []string
	entries map[string]string
	call    func(entry string) refused
}

func refusesEveryEntry(t *testing.T, members []confinedMember) {
	t.Helper()
	for _, member := range members {
		for how, entry := range member.entries {
			call := member.name + " with " + how
			got := answeredWithin(t, call, func() refused { return member.call(entry) })
			if got.state != desktop.Failed || !slices.Contains(member.reasons, got.reason) {
				t.Errorf("%s: %+v, want one of the refusals %q", call, got, member.reasons)
			}
		}
	}
}

// confinementWorkspaces builds a workspace and a folder beside it that each
// hold the same runnable saved test, suite, target and case at one acking
// peer, and the same reset plan, and plants every hostile entry for them in
// the workspace, with a link out to a folder and a link to one of its own.
func confinementWorkspaces(t *testing.T, address string) (root, outside string) {
	t.Helper()
	parent := t.TempDir()
	for _, folder := range []string{"workspace", "outside"} {
		built := ackWorkspace(t, address)
		if err := os.Rename(built, filepath.Join(parent, folder)); err != nil {
			t.Fatal(err)
		}
		writeAckSpec(t, filepath.Join(parent, folder), "booking.json", "AA")
		writeSuite(t, filepath.Join(parent, folder), "nightly.json")
		writeDocument(t, filepath.Join(parent, folder), "plan.json", `{"schema":"readmit-reset-plan/v1"}`)
	}
	root, outside = resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
	plantHostile(t, root, outside, "booking.json", "nightly.json", "target.json", "plan.json")
	if err := os.Mkdir(filepath.Join(outside, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"link-folder": filepath.Join(outside, "empty"), "alias-folder": filepath.Join(root, "folder.json")} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	return root, outside
}

func TestRunOperationsRefuseEveryEntryThatIsNotOneRegularFileOfTheWorkspace(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root, outside := confinementWorkspaces(t, peer.address)
	app := workspaceApp(t)

	// Every document a hostile entry leads to really runs: named properly, in
	// either folder, it preflights.
	for _, folder := range []string{root, outside} {
		for _, document := range []string{"booking.json", "nightly.json"} {
			if preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: folder, Spec: document, Environment: "east"}); preflight.State != desktop.Completed {
				t.Fatalf("%s in %s does not preflight, so it cannot show a refusal: %+v", document, folder, preflight)
			}
		}
	}
	opened := app.OpenCase(root, "case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the case a reduction reduces: %+v", opened)
	}
	// reduction is a reduction of the workspace's case whose members are the
	// workspace's own documents, apart from the one a call replaces.
	reduction := func(replace func(*desktop.ReductionRequest)) desktop.ReductionRequest {
		request := desktop.ReductionRequest{Workspace: root, Case: "case", Identity: opened.Case.Identity,
			Spec: "booking.json", Assertions: []string{"ack"}, Trials: 4, Confirmations: 1,
			ResetPlan: "plan.json", Target: "target.json", Work: "fresh-run"}
		replace(&request)
		return request
	}
	// The same documents, named as the reduction panel names them, are still
	// accepted.
	if preview := app.PreviewReduction(reduction(func(*desktop.ReductionRequest) {})); preview.State != desktop.Completed {
		t.Fatalf("a reduction of the workspace's own documents is not previewed: %+v", preview)
	}
	listed, before := entriesOf(t, root), bytesUnder(t, outside)

	saved := []string{"the saved test must be one regular entry of the open workspace"}
	members := []confinedMember{
		{"PreflightRun(Spec) of a saved test", saved, hostileDocuments(root, outside, "booking.json"), func(entry string) refused {
			result := app.PreflightRun(desktop.RunPreflightRequest{Workspace: root, Spec: entry})
			return refused{result.State, result.Reason}
		}},
		{"PreflightRun(Spec) of a suite", saved, hostileDocuments(root, outside, "nightly.json"), func(entry string) refused {
			result := app.PreflightRun(desktop.RunPreflightRequest{Workspace: root, Spec: entry, Environment: "east"})
			return refused{result.State, result.Reason}
		}},
		{"StartDurableRun(Spec)", saved, hostileDocuments(root, outside, "booking.json"), func(entry string) refused {
			result := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: entry, Output: "fresh-run"})
			return refused{result.State, result.Reason}
		}},
		{"StartSuiteRun(Suite)", []string{"the suite must be one regular entry of the open workspace"}, hostileDocuments(root, outside, "nightly.json"), func(entry string) refused {
			result := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: root, Suite: entry, Environment: "east", Output: "fresh-run"})
			return refused{result.State, result.Reason}
		}},
		{"StartSuiteRun(References)", []string{"released references must be one regular entry of the open workspace"}, hostileDocuments(root, outside, "booking.json"), func(entry string) refused {
			result := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: root, Suite: "nightly.json", Environment: "east", References: entry, Output: "fresh-run"})
			return refused{result.State, result.Reason}
		}},
		{"PreviewPacket(Spec)", []string{"the specification must be one entry of the open workspace"}, hostileDocuments(root, outside, "booking.json"), func(entry string) refused {
			result := app.PreviewPacket(desktop.PacketRequest{Workspace: root, Case: "case", Spec: entry, Current: "run"})
			return refused{result.State, result.Reason}
		}},
	}
	// A reduction runs its regression test once per trial, against the
	// environment, reset plan and policy it names beside it.
	for _, member := range []struct {
		name, document, reason string
		set                    func(*desktop.ReductionRequest, string)
	}{
		{"Spec", "booking.json", "the regression test must be one regular file of the open workspace, never a symbolic link",
			func(r *desktop.ReductionRequest, entry string) { r.Spec = entry }},
		{"Target", "target.json", "the approved environment must be one regular file of the open workspace, never a symbolic link",
			func(r *desktop.ReductionRequest, entry string) { r.Target = entry }},
		{"ResetPlan", "plan.json", "the reviewed reset plan must be one regular file of the open workspace, never a symbolic link",
			func(r *desktop.ReductionRequest, entry string) { r.ResetPlan = entry }},
		{"Policy", "plan.json", "the approved-destination policy must be one regular file of the open workspace, never a symbolic link",
			func(r *desktop.ReductionRequest, entry string) { r.Policy = entry }},
	} {
		entries := hostileDocuments(root, outside, member.document)
		members = append(members,
			confinedMember{"PreviewReduction(" + member.name + ")", []string{member.reason}, entries, func(entry string) refused {
				result := app.PreviewReduction(reduction(func(r *desktop.ReductionRequest) { member.set(r, entry) }))
				return refused{result.State, result.Reason}
			}},
			confinedMember{"StartReduction(" + member.name + ")", []string{member.reason}, entries, func(entry string) refused {
				result := app.StartReduction(reduction(func(r *desktop.ReductionRequest) { member.set(r, entry) }))
				return refused{result.State, result.Reason}
			}})
	}
	refusesEveryEntry(t, members)

	// A run folder is an entry the run creates, so an entry already at that
	// name — a link out, a link in, a FIFO or a folder — is refused as not
	// fresh, and a name that leaves the workspace is refused as not one entry.
	for how, output := range map[string]string{
		"a symbolic link out of the workspace to a folder": "link-folder",
		"a symbolic link to a folder of the workspace":     "alias-folder",
		"a `..` escape":                         filepath.Join("..", "outside", "fresh-run"),
		"an absolute path inside the workspace": filepath.Join(root, "fresh-run"),
		"a FIFO":                                "fifo.json",
		"a folder":                              "folder.json",
	} {
		for name, call := range map[string]func() refused{
			"PreflightRun(Output)": func() refused {
				result := app.PreflightRun(desktop.RunPreflightRequest{Workspace: root, Spec: "booking.json", Output: output})
				return refused{result.State, result.Reason}
			},
			"StartDurableRun(Output)": func() refused {
				result := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: "booking.json", Output: output})
				return refused{result.State, result.Reason}
			},
			"StartSuiteRun(Output)": func() refused {
				result := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: root, Suite: "nightly.json", Environment: "east", Output: output})
				return refused{result.State, result.Reason}
			},
		} {
			if got := answeredWithin(t, name+" with "+how, call); got.state != desktop.Failed {
				t.Errorf("%s with %s: %+v, want a refusal", name, how, got)
			}
		}
	}
	if peer.deliveries() != 0 {
		t.Fatalf("a refused entry was executed: %d deliveries", peer.deliveries())
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused run created an entry in the workspace: %v, was %v", after, listed)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused run changed the folder outside the workspace")
	}
}

// A retained execution is a folder, so its rule is the listing's rule for a
// folder: one real folder of the workspace, or one job reached through a suite
// execution's real `runs` folder, never through a symbolic link at any step.
// Each hostile entry leads to a run that really reopens.
func TestRunEvidenceReadsRefuseEveryEntryThatLeavesTheWorkspace(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root, outside := confinementWorkspaces(t, peer.address)
	app := workspaceApp(t)
	for _, folder := range []string{root, outside} {
		if run := app.StartDurableRun(desktop.DurableRunRequest{Workspace: folder, Spec: "booking.json", Output: "run"}); run.State != desktop.Completed {
			t.Fatalf("run in %s: %+v", folder, run)
		}
		if run := app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: folder, Suite: "nightly.json", Environment: "east", Output: "suite-run"}); run.State != desktop.Completed {
			t.Fatalf("suite run in %s: %+v", folder, run)
		}
		for _, entry := range []string{"run", "suite-run/runs/booking-one"} {
			if opened := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: folder, Entry: entry}); opened.State != desktop.Completed {
				t.Fatalf("%s in %s does not reopen, so it cannot show a refusal: %+v", entry, folder, opened)
			}
		}
	}
	if err := os.Mkdir(filepath.Join(root, "linked-runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"link-run":         filepath.Join(outside, "run"),
		"alias-run":        filepath.Join(root, "run"),
		"link-suite-run":   filepath.Join(outside, "suite-run"),
		"alias-suite-run":  filepath.Join(root, "suite-run"),
		"linked-runs/runs": filepath.Join(outside, "suite-run", "runs"),
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	delivered, before := peer.deliveries(), bytesUnder(t, outside)

	// A name that is not one entry, or reaches a job through a link, is refused
	// by the operation's own sentence before any reader is handed it. Every
	// operation that names a retained execution is held to it.
	named := map[string]string{
		"a `..` escape": filepath.Join("..", "outside", "run"),
		"an absolute path to the workspace's run":            filepath.Join(root, "run"),
		"an absolute path outside the workspace":             filepath.Join(outside, "run"),
		"a job through a symbolic link out of the workspace": "link-suite-run/runs/booking-one",
		"a job through a symbolic link to a workspace run":   "alias-suite-run/runs/booking-one",
		"a job through a linked runs folder":                 "linked-runs/runs/booking-one",
	}
	packet := "a retained packet is named by one entry of the open workspace"
	refusesEveryEntry(t, []confinedMember{
		{"OpenRunEvidence", []string{"a retained execution is named by one workspace entry, or one job inside a suite execution's runs"}, named, func(entry string) refused {
			result := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: root, Entry: entry})
			return refused{result.State, result.Reason}
		}},
		{"DurableRunProgress", []string{"a run is named by one workspace entry, or one job inside a suite execution's runs"}, named, func(entry string) refused {
			result := app.DurableRunProgress(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"PreviewPacket(Current)", []string{"the current result must be one retained execution of the open workspace"}, named, func(entry string) refused {
			result := app.PreviewPacket(desktop.PacketRequest{Workspace: root, Case: "case", Spec: "booking.json", Current: entry})
			return refused{result.State, result.Reason}
		}},
		{"PreviewPacket(Baseline)", []string{"the baseline result must be one retained execution of the open workspace"}, named, func(entry string) refused {
			result := app.PreviewPacket(desktop.PacketRequest{Workspace: root, Case: "case", Spec: "booking.json", Current: "run", Baseline: entry})
			return refused{result.State, result.Reason}
		}},
		{"OpenPacket", []string{packet}, named, func(entry string) refused {
			result := app.OpenPacket(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"ExportPacketReview", []string{packet}, named, func(entry string) refused {
			result := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: entry, Destination: filepath.Join(t.TempDir(), "review")})
			return refused{result.State, result.Reason}
		}},
		{"OpenPacketReview", []string{"a portable review is named by one entry of the open workspace"}, named, func(entry string) refused {
			result := app.OpenPacketReview(desktop.PacketReviewRequest{Workspace: root, Entry: entry})
			return refused{result.State, result.Reason}
		}},
	})
	// A link named as the run itself, and an entry that is not a folder, are
	// refused by the retained-evidence readers, which inspect the entry without
	// following it before they read anything in it.
	for how, entry := range map[string]string{
		"a symbolic link out of the workspace":      "link-run",
		"a symbolic link to a run of the workspace": "alias-run",
		"a FIFO":                                 "fifo.json",
		"a regular file where a run is expected": "booking.json",
	} {
		if opened := answeredWithin(t, "OpenRunEvidence with "+how, func() desktop.RunEvidenceResult {
			return app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: root, Entry: entry})
		}); opened.State != desktop.Failed || opened.Evidence != nil {
			t.Errorf("OpenRunEvidence with %s: %+v, want a refusal", how, opened)
		}
		if read := answeredWithin(t, "DurableRunProgress with "+how, func() desktop.RunProgressResult { return app.DurableRunProgress(root, entry) }); read.State != desktop.Failed || read.Progress != nil {
			t.Errorf("DurableRunProgress with %s: %+v, want a refusal", how, read)
		}
	}
	if peer.deliveries() != delivered {
		t.Fatal("reading retained evidence sent something")
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("reading retained evidence changed the folder outside the workspace")
	}
}

// Every other operation that reads an entry named by the caller: the
// protection document each protection operation reads and whose key program
// it may run, the entries a package is packed from, the private local state an
// export or a support summary is bound to, and the built reproducer a revision
// is copied from.
func TestEveryOtherEntryReadByNameRefusesALinkAndEveryEscape(t *testing.T) {
	parent := t.TempDir()
	app := workspaceApp(t)
	for _, folder := range []string{"workspace", "outside"} {
		if err := os.Mkdir(filepath.Join(parent, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, outside := resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
	// The document outside names a key program that leaves a mark when it
	// runs, which keyProgram's stand-in does not, so following the link to it
	// and using it is visible.
	marker := filepath.Join(t.TempDir(), "outside-key-program-ran")
	outsideProgram := filepath.Join(t.TempDir(), "readmit-test-key-store.sh")
	if err := os.WriteFile(outsideProgram, []byte("#!/bin/sh\ntouch '"+marker+"'\nprintf '%s' 'test-only-not-a-real-key-4f8c1d2e6b0a9357'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for folder, program := range map[string]string{root: keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", ""), outside: outsideProgram} {
		if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{
			Workspace: folder, Entry: "protection.json", Name: "lab-evidence", Storage: "os-volume-encryption",
			Command: program, Arguments: []string{"find-generic-password", "-w", "-s", "readmit-lab-key"}, MaxAge: "720h", Retain: "1h",
		}); saved.State != desktop.Completed {
			t.Fatalf("control in %s: %+v", folder, saved)
		}
		writeDocument(t, folder, "evidence.txt", "synthetic retained evidence bytes")
		for _, name := range []string{"private", "built"} {
			if err := os.Mkdir(filepath.Join(folder, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	if packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: "protection.json", Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "transfer"}); packed.State != desktop.Completed {
		t.Fatalf("pack: %+v", packed)
	}
	plantHostile(t, root, outside, "protection.json", "evidence.txt", "private", "built")
	listed, before := entriesOf(t, root), bytesUnder(t, outside)

	// hostile names entry, which both folders hold, in every way the rule
	// refuses, with wrong as the entry of the other kind beside it.
	hostile := func(entry, wrong string) map[string]string {
		named := hostileDocuments(root, outside, entry)
		delete(named, "a folder")
		named["an entry of the wrong kind"] = wrong
		return named
	}
	// A name that is not one entry and an entry that is not one regular file
	// are the protection document's two refusals.
	document := []string{"the protection document must be one entry of the open workspace",
		"the protection document must be one regular file of the open workspace, never a symbolic link"}
	private := []string{"the private local state must be one folder of the open workspace, never a symbolic link"}
	refusesEveryEntry(t, []confinedMember{
		{"ReadProtection", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.ReadProtection(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"SaveProtectionControl", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.SaveProtectionControl(desktop.ProtectionControlRequest{
				Workspace: root, Entry: entry, Name: "second", Storage: "os-volume-encryption",
				Command: outsideProgram, Arguments: []string{"find-generic-password"}, MaxAge: "720h", Retain: "1h",
			})
			return refused{result.State, result.Reason}
		}},
		{"RotateProtectionControl", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.RotateProtectionControl(root, entry, "lab-evidence")
			return refused{result.State, result.Reason}
		}},
		{"RetireProtectionControl", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.RetireProtectionControl(root, entry, "lab-evidence")
			return refused{result.State, result.Reason}
		}},
		{"PackProtectedPackage(Entry)", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: entry, Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: "fresh"})
			return refused{result.State, result.Reason}
		}},
		{"OpenProtectedPackage(Entry)", document, hostile("protection.json", "folder.json"), func(entry string) refused {
			result := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{Workspace: root, Entry: entry, Control: "lab-evidence", Package: "transfer", Output: "fresh"})
			return refused{result.State, result.Reason}
		}},
		{"PackProtectedPackage(Sources)", []string{"every entry to pack must be one regular file or folder of the open workspace, never a symbolic link"},
			hostile("evidence.txt", "fifo.json"), func(entry string) refused {
				result := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: "protection.json", Control: "lab-evidence", Sources: []string{entry}, Output: "fresh"})
				return refused{result.State, result.Reason}
			}},
		{"ExportDerivedPacket(LocalState)", private, hostile("private", "evidence.txt"), func(entry string) refused {
			result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{Workspace: root, Review: "review", LocalState: entry, Approval: "approved", Output: "fresh"})
			return refused{result.State, result.Reason}
		}},
		{"PreviewSupportSummary(Private)", private, hostile("private", "evidence.txt"), func(entry string) refused {
			result := app.PreviewSupportSummary(desktop.SupportRequest{Workspace: root, Source: "review", Kind: "derived-review", Private: entry, Policy: "policy.json"})
			return refused{result.State, result.Reason}
		}},
		{"PublishSupportSummary(Private)", private, hostile("private", "evidence.txt"), func(entry string) refused {
			result := app.PublishSupportSummary(desktop.SupportPublishRequest{Workspace: root, Source: "review", Kind: "derived-review", Private: entry, Policy: "policy.json", Approval: "approved", Output: "fresh"})
			return refused{result.State, result.Reason}
		}},
		{"RegisterRevision(Source)", []string{"a built reproducer must be named by one directory entry of the open workspace"}, hostile("built", "evidence.txt"), func(entry string) refused {
			result := app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: entry, Name: "fresh", Parent: "incident"})
			return refused{result.State, result.Reason}
		}},
	})
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatal("the key program named by the document outside the workspace was run")
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused operation changed the workspace's entries: %v, was %v", after, listed)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused operation changed the folder outside the workspace")
	}
}

// A practice run executes the spec it names against the guided sample's own
// receiver, so the spec is held to the same rule as any other saved test.
func TestAPracticeRunRefusesASpecThatIsNotOneRegularFileOfTheWorkspace(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)
	root = resolved(t, root)
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(root, spec))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, outside, spec, string(saved))
	plantHostile(t, root, outside, spec)
	listed := entriesOf(t, root)
	refusesEveryEntry(t, []confinedMember{
		{"RunPractice", []string{"a test spec is one regular file of the open workspace, never a symbolic link"}, hostileDocuments(root, outside, spec), func(entry string) refused {
			result := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: entry, Trial: guide.StepBaseline, Output: "baseline-run"})
			return refused{result.State, result.Reason}
		}},
	})
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused practice run created an entry: %v, was %v", after, listed)
	}
	// The spec itself, named properly, still runs.
	if result := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"}); result.State != desktop.Completed {
		t.Fatalf("the saved spec no longer runs: %+v", result)
	}
}

// besideWorkspace makes an empty workspace and an empty folder beside it,
// each named as the shell records a workspace.
func besideWorkspace(t *testing.T) (root, outside string) {
	t.Helper()
	parent := t.TempDir()
	for _, folder := range []string{"workspace", "outside"} {
		if err := os.Mkdir(filepath.Join(parent, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
}

// A profile library is a folder of pack documents the library reader lists and
// reads, so the folder is an entry like any other: the workspace itself, or
// one real folder of it, never a folder reached through a link, a `..` or an
// absolute path. The folder beside the workspace holds the same pack, so only
// the rule can refuse it.
func TestOpeningAProfileLibraryRefusesAFolderThatIsNotOneOfTheWorkspace(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	for _, folder := range []string{root, outside} {
		if err := os.Mkdir(filepath.Join(folder, "library"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeDocument(t, filepath.Join(folder, "library"), "pack.json", fixture(t, "profile-pack.json"))
	}
	if opened := app.OpenProfileLibrary(outside, "library"); opened.State != desktop.Completed || len(opened.Entries) != 1 {
		t.Fatalf("the library outside does not open, so it cannot show a refusal: %+v", opened)
	}
	writeDocument(t, root, "evidence.txt", "synthetic bytes where a library is expected")
	plantHostile(t, root, outside, "library")
	refusesEveryEntry(t, []confinedMember{
		{"OpenProfileLibrary", []string{"a profile library must be the open workspace or one folder of it, never a symbolic link"}, map[string]string{
			"a symbolic link out of the workspace":         "link-library",
			"a symbolic link to a folder of the workspace": "alias-library",
			"a `..` escape": filepath.Join("..", "outside", "library"),
			"an absolute path to the workspace's folder": filepath.Join(root, "library"),
			"an absolute path outside the workspace":     filepath.Join(outside, "library"),
			"a FIFO":                                     "fifo.json",
			"a regular file":                             "evidence.txt",
		}, func(entry string) refused {
			result := app.OpenProfileLibrary(root, entry)
			return refused{result.State, result.Reason}
		}},
	})
	if opened := app.OpenProfileLibrary(root, "library"); opened.State != desktop.Completed || len(opened.Entries) != 1 {
		t.Fatalf("the workspace's own library no longer opens: %+v", opened)
	}
}

// A generated family, a collection's staging folder and receipt, a capture's
// case and observation, and the case a staged collection is finalized into
// are new entries of the workspace. Their creation is exclusive, but a name
// that is not one entry would still place them somewhere else: beside the
// workspace, inside one of its folders, or through a linked folder out of it.
// Each request below is complete: the generation, synthesis, collection and
// finalization write under fresh names at the end, and a capture handed a name
// the rule missed would start listening.
func TestEveryGeneratedCollectedOrCapturedOutputIsOneNewEntryOfTheWorkspace(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	writeDocument(t, root, "plan.json", fixture(t, "scenario-generator.json"))
	payload := "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r"
	for _, folder := range []string{"export", "staged", "folder"} {
		if err := os.Mkdir(filepath.Join(root, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, folder := range []string{"export", "staged"} {
		writeDocument(t, filepath.Join(root, folder), "one.hl7", payload)
	}
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: filepath.Join(root, "export"),
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: source}); saved.State != desktop.Completed {
		t.Fatalf("source registration: %+v", saved)
	}
	policy, err := collection.DecodePolicy([]byte(facadeAnyPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if saved := app.SaveReceiverPolicy(desktop.ReceiverPolicyRequest{Workspace: root, PolicyFile: "policy.json", Policy: policy}); saved.State != desktop.Completed {
		t.Fatalf("receiver policy: %+v", saved)
	}
	importPlan := importer.Plan{Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"}}
	if err := os.Symlink(outside, filepath.Join(root, "link-folder")); err != nil {
		t.Fatal(err)
	}
	listed, before := entriesOf(t, root), bytesUnder(t, outside)

	generate := func(output, caseName string) desktop.ScenarioGenerateRequest {
		return desktop.ScenarioGenerateRequest{Workspace: root, Document: "plan.json", OutputName: output, CaseName: caseName}
	}
	synthesize := func(output string) desktop.SynthGenerateRequest {
		return desktop.SynthGenerateRequest{Workspace: root, OutputName: output, Seed: 0, BaseTime: "2026-01-01T12:00:00Z",
			GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1"}
	}
	collect := func(output, receipt string) desktop.SourceWorkRequest {
		return desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", Plan: &importPlan, OutputName: output, ReceiptName: receipt}
	}
	// A capture that were started would listen until its idle timeout, so a
	// name the rule missed answers late and with the capture's own outcome.
	capture := func(kind, output, observation string) desktop.CaptureRequest {
		return desktop.CaptureRequest{Workspace: root, Kind: kind, Address: "127.0.0.1:0", PolicyFile: "policy.json",
			FixtureMode: "fixed", OutputName: output, ObservationName: observation, MaxMessages: 1, IdleTimeout: "1s"}
	}
	finalize := func(output, receipt string) desktop.FinalizeCaptureRequest {
		return desktop.FinalizeCaptureRequest{Workspace: root, Folder: "staged", OutputName: output, ReceiptName: receipt}
	}
	names := map[string]string{
		"a `..` escape":                                       filepath.Join("..", "outside", "fresh"),
		"an absolute path outside the workspace":              filepath.Join(outside, "fresh"),
		"an absolute path inside the workspace":               filepath.Join(root, "fresh"),
		"a name inside a folder of the workspace":             filepath.Join("folder", "fresh"),
		"a name through a symbolic link out of the workspace": filepath.Join("link-folder", "fresh"),
	}
	caseDestination := []string{"the case destination must be one valid directory entry name"}
	refusesEveryEntry(t, []confinedMember{
		{"GenerateScenario(OutputName)", []string{"generation destination must be a new directory entry of the open workspace"}, names, func(name string) refused {
			result := app.GenerateScenario(generate(name, "fresh-case"))
			return refused{result.State, result.Reason}
		}},
		{"GenerateScenario(CaseName)", []string{"case destination must be a new directory entry of the open workspace"}, names, func(name string) refused {
			result := app.GenerateScenario(generate("fresh-family", name))
			return refused{result.State, result.Reason}
		}},
		{"GenerateSynth(OutputName)", []string{"synth destination must be a new directory entry of the open workspace"}, names, func(name string) refused {
			result := app.GenerateSynth(synthesize(name))
			return refused{result.State, result.Reason}
		}},
		{"CollectSource(OutputName)", []string{"the staging folder must be one new entry of the open workspace"}, names, func(name string) refused {
			result := app.CollectSource(collect(name, "fresh-receipt.json"))
			return refused{result.State, result.Reason}
		}},
		{"CollectSource(ReceiptName)", []string{"the collection receipt must be one new entry of the open workspace"}, names, func(name string) refused {
			result := app.CollectSource(collect("fresh-staged", name))
			return refused{result.State, result.Reason}
		}},
		{"StartCapture(OutputName) collecting", []string{"the captured case must be one new entry of the open workspace"}, names, func(name string) refused {
			result := app.StartCapture(capture("collect", name, ""))
			return refused{result.State, result.Reason}
		}},
		{"StartCapture(OutputName) listening", []string{"the captured case must be one new entry of the open workspace"}, names, func(name string) refused {
			result := app.StartCapture(capture("listen", name, "fresh-observation.json"))
			return refused{result.State, result.Reason}
		}},
		{"StartCapture(ObservationName)", []string{"the observation record must be one new entry of the open workspace"}, names, func(name string) refused {
			result := app.StartCapture(capture("listen", "fresh-capture", name))
			return refused{result.State, result.Reason}
		}},
		{"FinalizeCaptureImport(OutputName)", caseDestination, names, func(name string) refused {
			result := app.FinalizeCaptureImport(finalize(name, "fresh-import.json"))
			return refused{result.State, result.Reason}
		}},
		{"FinalizeCaptureImport(ReceiptName)", []string{"the receipt destination must be one valid file entry name"}, names, func(name string) refused {
			result := app.FinalizeCaptureImport(finalize("fresh-import", name))
			return refused{result.State, result.Reason}
		}},
	})
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused output created an entry in the workspace: %v, was %v", after, listed)
	}
	if inside := entriesOf(t, filepath.Join(root, "folder")); len(inside) != 0 {
		t.Fatalf("a refused output was written inside a folder of the workspace: %v", inside)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused output was written outside the workspace")
	}

	// The same requests, each naming fresh entries of the workspace, write.
	if result := app.GenerateScenario(generate("fresh-family", "fresh-case")); result.State != desktop.Completed {
		t.Fatalf("a generation into fresh entries: %+v", result)
	}
	if result := app.GenerateSynth(synthesize("fresh-synth")); result.State != desktop.Completed {
		t.Fatalf("a synthetic family into a fresh entry: %+v", result)
	}
	if result := app.CollectSource(collect("fresh-staged", "fresh-receipt.json")); result.State != desktop.Completed {
		t.Fatalf("a collection into fresh entries: %+v", result)
	}
	if result := app.FinalizeCaptureImport(finalize("fresh-import", "fresh-import.json")); result.State != desktop.Completed {
		t.Fatalf("finalizing into fresh entries: %+v", result)
	}
}

// accessedAt reports when path was last read.
func accessedAt(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := reflect.ValueOf(info.Sys()).Elem()
	for _, name := range []string{"Atim", "Atimespec"} {
		if field := stat.FieldByName(name); field.IsValid() {
			spec := field.Interface().(syscall.Timespec)
			return time.Unix(spec.Unix())
		}
	}
	t.Fatal("this platform does not report when a file was last read, so no read can be ruled out here")
	return time.Time{}
}

// unreadSince is the access time ageReads gives a file.
var unreadSince = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// ageReads sets each path's access time before its modification time, so that
// any later read of it moves the access time, even where reads are recorded
// lazily. On a filesystem that records no read at all nothing could show one,
// and a proof that did not run is no proof: the test fails there, naming why,
// rather than skipping, which the #111 launcher would count as a problem
// anyway. Run it where the temporary folder records reads.
func ageReads(t *testing.T, paths ...string) {
	t.Helper()
	directory := t.TempDir()
	writeDocument(t, directory, "probe", "probe")
	probe := filepath.Join(directory, "probe")
	for _, path := range append([]string{probe}, paths...) {
		if err := os.Chtimes(path, unreadSince, unreadSince.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.ReadFile(probe); err != nil {
		t.Fatal(err)
	}
	if !readSince(t, probe) {
		t.Fatal("this filesystem does not record when a file is read, so no read through a link can be ruled out here")
	}
}

// readSince reports whether path was read after ageReads aged it.
func readSince(t *testing.T, path string) bool {
	t.Helper()
	return !accessedAt(t, path).Equal(unreadSince)
}

// Finding an applicable index when none is named scans the workspace's
// entries. An entry that is a symbolic link is never an index of the
// workspace, so the scan passes over it without reading through it, even to
// see what it declares. The link leads to a real index of the same case, so
// only the rule keeps the scan out of it.
func TestFindingAnIndexNeverReadsThroughALink(t *testing.T) {
	app := workspaceApp(t)
	root, outside := besideWorkspace(t)
	writeCase(t, root, "incident", framed(gridBooking))
	incident := writeCase(t, outside, "incident", framed(gridBooking))
	writeIndex(t, outside, "incident.index.json", incident, nil)
	if found := app.DescribeIndex(outside, "incident", ""); found.State != desktop.Completed || found.Index == nil || !found.Index.Applicable {
		t.Fatalf("the index outside is not found beside its own case, so it cannot show a refusal: %+v", found)
	}
	target := filepath.Join(outside, "incident.index.json")
	if err := os.Symlink(target, filepath.Join(root, "linked.index.json")); err != nil {
		t.Fatal(err)
	}
	ageReads(t, target)
	if found := app.DescribeIndex(root, "incident", ""); found.State != desktop.Empty || found.Index != nil {
		t.Fatalf("an index reached through a link was offered: %+v", found)
	}
	if readSince(t, target) {
		t.Fatal("finding an index read through a symbolic link out of the workspace")
	}
}
