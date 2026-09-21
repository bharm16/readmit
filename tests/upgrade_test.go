package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/upgrade"
)

// stagedCandidate writes what an administrator downloaded onto a workstation:
// the packages and the readmit-desktop-package/v1 manifest beside them. The
// manifest is literal JSON, because the point of the check is to read the
// document a separate build machine wrote.
func stagedCandidate(t *testing.T, signed bool, version string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "staged")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "readmit-desktop_" + version + "_" + runtime.GOARCH + ".pkg"
	payload := []byte("the bytes of a package this test never installs")
	if err := os.WriteFile(filepath.Join(directory, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	document := `{"schema":"` + upgrade.CandidateSchema + `","version":"` + version +
		`","os":"` + runtime.GOOS + `","arch":"` + runtime.GOARCH +
		`","signed_for_distribution":` + map[bool]string{true: "true", false: "false"}[signed] +
		`,"packages":[{"name":"` + name + `","format":"pkg","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	if err := os.WriteFile(filepath.Join(directory, upgrade.CandidateDocumentName), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func sameTree(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the upgrade added or removed a file of the %s it reviewed", what)
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatalf("the upgrade rewrote a file of the %s it reviewed", what)
		}
	}
}

// An explicit update check reads a staged candidate and the evidence this
// machine already holds, reports one versioned plan over both, and leaves
// every byte of that evidence exactly as it found it. Nothing is downloaded
// and no update service is contacted: the candidate is a directory somebody
// put there.
func TestUpgradeChecksAStagedCandidateWithoutRewritingEvidence(t *testing.T) {
	root := indexedProject(t)
	spec, dir := durableSpec(t, durablePeer(t, "AA"))
	job := filepath.Join(dir, "job")
	if _, stderr, err := run(t, "run", "start", spec, "--send", "--output", job, "--json"); err != nil || stderr != "" {
		t.Fatalf("run start: %v %s", err, stderr)
	}
	candidate := stagedCandidate(t, true, "9.9.9")

	beforeProject, beforeJob := treeDigests(t, root), treeDigests(t, job)
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", candidate, "--project", root, "--run", job)
	if err != nil || stderr != "" {
		t.Fatalf("upgrade check: %v %s %s", err, stdout, stderr)
	}
	for _, want := range []string{
		`"schema":"` + upgrade.PlanSchema + `"`,
		`"candidate":"9.9.9"`,
		`"signed_for_distribution":true`,
		`"name":"investigation","kind":"project","state":"readable"`,
		`"name":"job","kind":"run","state":"readable"`,
		`"state":"ready"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("the plan omitted %s:\n%s", want, stdout)
		}
	}
	sameTree(t, "project", beforeProject, treeDigests(t, root))
	sameTree(t, "run", beforeJob, treeDigests(t, job))

	// A plan names what an operator opened, never the path they opened it at,
	// and it carries no message content.
	for _, private := range []string{root, job, candidate, "SYNTH-001", "LISTEN-BOOK"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatalf("the plan disclosed %q", private)
		}
	}
}

// Every package this repository builds records that it is not signed for
// distribution, so every candidate staged from one is refused. The plan is
// still printed, because an operator reads why before they read that.
func TestUpgradeRefusesADevelopmentPreviewAndPrintsThePlanAnyway(t *testing.T) {
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", stagedCandidate(t, false, "9.9.9"), "--project", newProject(t))
	if exitCode(t, err) != exitRefused {
		t.Fatalf("a development preview was reported as an upgrade: %v %s", err, stdout)
	}
	if !strings.Contains(stdout, `"signed_for_distribution":false`) || !strings.Contains(stdout, `"state":"refused"`) {
		t.Fatalf("the refused plan was not printed:\n%s", stdout)
	}
	if !strings.Contains(stderr, "not signed for distribution") {
		t.Fatalf("the refusal did not say why: %s", stderr)
	}
}

// A candidate whose staged bytes are not the bytes its manifest recorded is
// refused, and one holding a file the manifest does not record is refused
// before any of it is read as a candidate at all. Either is what a failed or
// half-finished download leaves behind.
func TestUpgradeRefusesAStagedCandidateThatIsNotWhatItRecords(t *testing.T) {
	root := newProject(t)
	candidate := stagedCandidate(t, true, "9.9.9")
	name := filepath.Join(candidate, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg")
	if err := os.WriteFile(name, []byte("a download that stopped partway"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", candidate, "--project", root)
	if exitCode(t, err) != exitRefused || !strings.Contains(stdout, `"state":"altered"`) || !strings.Contains(stderr, "stage the candidate again") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}

	whole := stagedCandidate(t, true, "9.9.9")
	if err := os.WriteFile(filepath.Join(whole, "readmit-desktop_9.9.8_"+runtime.GOARCH+".pkg"), []byte("an older package"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, err := run(t, "upgrade", "check", "--candidate", whole, "--project", root); err == nil || stdout != "" || !strings.Contains(stderr, "does not record") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
}

// A check that reviewed nothing is not a compatibility review, and reporting
// one as ready would be exactly the auto-pass this command exists to prevent.
func TestUpgradeCheckRefusesToReviewNothing(t *testing.T) {
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", stagedCandidate(t, true, "9.9.9"))
	if err == nil || stdout != "" || !strings.Contains(stderr, "at least one --project or --run") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
}

// Recovering from an installation that did not complete needs no state of
// ours, because no installer of ours touches evidence: what is uncertain is
// only which application is installed and whether the candidate is still whole,
// and both are read back by running the check again. A candidate left partly
// written reports `altered` and refuses; the same directory staged whole reads
// as `intact` and is ready, with the evidence untouched throughout.
func TestUpgradeRecoversFromAnInstallationThatDidNotComplete(t *testing.T) {
	root := indexedProject(t)
	before := treeDigests(t, root)
	candidate := stagedCandidate(t, true, "9.9.9")
	name := filepath.Join(candidate, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg")
	whole, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, whole[:len(whole)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	installed, _, err := run(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	stdout, _, err := run(t, "upgrade", "check", "--candidate", candidate, "--project", root)
	if exitCode(t, err) != exitRefused || !strings.Contains(stdout, `"state":"altered"`) {
		t.Fatalf("%v %s", err, stdout)
	}
	// The plan names the build actually installed, which is what says whether
	// the installation took effect at all.
	if !strings.Contains(stdout, `"installed":"`+strings.Fields(installed)[2]+`"`) {
		t.Fatalf("the plan did not name the installed build:\n%s\n%s", installed, stdout)
	}

	if err := os.WriteFile(name, whole, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", candidate, "--project", root)
	if err != nil || stderr != "" || !strings.Contains(stdout, `"state":"ready"`) {
		t.Fatalf("staging the candidate again did not recover: %v %s %s", err, stdout, stderr)
	}
	sameTree(t, "project", before, treeDigests(t, root))
}

// Taking the archive an upgrade rolls back to is an administrator's explicit
// decision: without the approval nothing is written at all. With it, the
// archive is a verified recovery archive of the project exactly as it stands,
// the project itself is untouched, and the archive restores.
func TestUpgradePrepareNeedsApprovalAndTakesAVerifiedRollbackPoint(t *testing.T) {
	root := indexedProject(t)
	candidate := stagedCandidate(t, false, "9.9.9")
	archive := filepath.Join(t.TempDir(), "rollback")

	if _, stderr, err := run(t, "upgrade", "prepare", root, "--candidate", candidate, "--output", archive); err == nil || !strings.Contains(stderr, "--approve") {
		t.Fatalf("a rollback point was taken without an administrator's approval: %v %s", err, stderr)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("an unapproved upgrade wrote its archive anyway")
	}

	before := treeDigests(t, root)
	stdout, stderr, err := run(t, "upgrade", "prepare", root, "--candidate", candidate, "--output", archive, "--approve")
	if err != nil || stderr != "" {
		t.Fatalf("upgrade prepare: %v %s %s", err, stdout, stderr)
	}
	for _, want := range []string{
		`"name":"investigation","kind":"project","state":"readable"`,
		"Backup written: readmit-backup/v1",
		"Complete: yes",
		"regression kind=case evidence=verified identity=" + frozenRegressionIdentity,
		"Rollback point taken under engine build: ",
		// The archive is taken whatever the signing decision, and prepare
		// still says the candidate may not be installed.
		"Installing this candidate is still refused: ",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("upgrade prepare omitted %q:\n%s", want, stdout)
		}
	}
	sameTree(t, "project", before, treeDigests(t, root))

	// The boundary the archive states: it restores the project as it was when
	// it was taken, and work done after that is not in it. Editing the project
	// afterwards leaves the live project holding that edit and the rollback
	// point not.
	if _, stderr, err := run(t, "project", "update", root, "regression", "--incident", "INC-AFTER-THE-ARCHIVE"); err != nil || stderr != "" {
		t.Fatalf("project update: %v %s", err, stderr)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if _, stderr, err := run(t, "backup", "restore", archive, "--output", restored); err != nil {
		t.Fatalf("the rollback point did not restore: %v %s", err, stderr)
	}
	stdout, _, err = run(t, "project", "show", restored)
	if err != nil || !strings.Contains(stdout, frozenRegressionIdentity) {
		t.Fatalf("the rollback point lost the identity the project recorded: %v %s", err, stdout)
	}
	if strings.Contains(stdout, "INC-AFTER-THE-ARCHIVE") {
		t.Fatal("the rollback point held work done after it was taken")
	}
	if live, _, err := run(t, "project", "show", root); err != nil || !strings.Contains(live, "INC-AFTER-THE-ARCHIVE") {
		t.Fatalf("rolling back removed work from the project it was taken of: %v %s", err, live)
	}
}

// A rollback point is never taken against a candidate that is not what it
// records, because an archive that presents itself as this upgrade's rollback
// point stands for an upgrade that was actually staged.
func TestUpgradePrepareRefusesACandidateThatIsNotIntact(t *testing.T) {
	root := indexedProject(t)
	candidate := stagedCandidate(t, true, "9.9.9")
	if err := os.Remove(filepath.Join(candidate, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "rollback")
	stdout, stderr, err := run(t, "upgrade", "prepare", root, "--candidate", candidate, "--output", archive, "--approve")
	if err == nil || !strings.Contains(stdout, `"state":"absent"`) || !strings.Contains(stderr, "stage the candidate again") {
		t.Fatalf("%v %s %s", err, stdout, stderr)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("a refused preparation wrote its archive anyway")
	}
}
