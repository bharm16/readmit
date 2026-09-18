package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// backupIndexName is the derived index a project keeps beside its evidence. It
// is a file name, never a member of any bundle.
const backupIndexName = "regression.index.json"

func newBackup(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "backup")
}

// registerFrozenMember copies one named member of the frozen reference family
// into the project and registers it. The shared helper always places the
// `regression` member, and a project cannot register the same identity twice.
func registerFrozenMember(t *testing.T, root, member string) {
	t.Helper()
	family := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(family))
	if err := os.CopyFS(filepath.Join(root, member), os.DirFS(filepath.Join(family, member))); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(t, "project", "add", root, member, "--title", "Cancelled appointment"); err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
}

// indexedProject registers the frozen regression case and builds one derived
// index of it beside the project, which is the shape a backup has to account
// for: canonical evidence, an editable document, and a disposable index.
func indexedProject(t *testing.T) string {
	t.Helper()
	root := newProject(t)
	registerFrozenCase(t, root, "regression")
	if _, stderr, err := run(t, "index", "build", filepath.Join(root, "regression"),
		"--output", filepath.Join(root, backupIndexName),
		"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	return root
}

// A project is copied into a verified backup, read back whole, and restored
// somewhere else with every identity intact and its index built again from the
// restored canonical evidence.
func TestBackupRestoresAProjectElsewhereAndRebuildsItsIndex(t *testing.T) {
	root := indexedProject(t)
	stored := newBackup(t)

	stdout, stderr, err := run(t, "backup", "create", root, "--output", stored)
	if err != nil || stderr != "" {
		t.Fatalf("backup create: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Backup written: readmit-backup/v1", "Complete: yes", "Registered artifacts: 1",
		"regression kind=case evidence=verified identity=" + frozenRegressionIdentity,
		"Indexes: 1", backupIndexName + " case=regression index=recorded",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing backup summary %q:\n%s", want, stdout)
		}
	}
	// A backup holds a project's files; it never holds a copy of a derived
	// index, because that would put the values it retained in a second place.
	if _, err := os.Stat(filepath.Join(stored, "files", backupIndexName)); !os.IsNotExist(err) {
		t.Errorf("the backup carried a derived index: %v", err)
	}

	stdout, stderr, err = run(t, "backup", "verify", stored)
	if err != nil || stderr != "" {
		t.Fatalf("backup verify: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Backup: readmit-backup/v1", "Complete: yes",
		backupIndexName + " case=regression recipe=declared fields=PID[1]-3[1] retention=values retain-until=indefinite",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing verification summary %q:\n%s", want, stdout)
		}
	}

	restored := filepath.Join(t.TempDir(), "restored")
	stdout, stderr, err = run(t, "backup", "restore", stored, "--output", restored)
	if err != nil || stderr != "" {
		t.Fatalf("backup restore: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Project restored: readmit-backup/v1", "Complete: yes",
		"regression kind=case recorded=verified restored=verified identity=" + frozenRegressionIdentity,
		backupIndexName + " case=regression index=rebuilt",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing restore summary %q:\n%s", want, stdout)
		}
	}
	// The restored project is a project: it opens, it reports the identity it
	// was registered with, and its rebuilt index answers about the case beside
	// it rather than about the one it was originally built from.
	stdout, stderr, err = run(t, "project", "show", restored)
	if err != nil || stderr != "" || !strings.Contains(stdout, "regression evidence=verified identity="+frozenRegressionIdentity) {
		t.Fatalf("project show of a restored project: %v %s\n%s", err, stderr, stdout)
	}
	stdout, stderr, err = run(t, "index", "search", filepath.Join(restored, "regression"), filepath.Join(restored, backupIndexName), "--state", "present")
	if err != nil || stderr != "" {
		t.Fatalf("index search of a rebuilt index: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Case: "+frozenRegressionIdentity) || !strings.Contains(stdout, "Undecided: 0") {
		t.Errorf("a rebuilt index does not describe the restored case:\n%s", stdout)
	}
	for _, secret := range []string{"SYNTH-", "MRN", "corpus", root, stored, restored} {
		if strings.Contains(stdout+stderr, secret) {
			t.Errorf("a backup workflow disclosed %q", secret)
		}
	}
}

// Evidence a project no longer holds is reported, never replaced, and the
// command says so with a status nobody can mistake for success.
func TestBackupReportsEvidenceItCouldNotVerify(t *testing.T) {
	root := indexedProject(t)
	registerFrozenMember(t, root, "cancellation")
	if err := os.RemoveAll(filepath.Join(root, "cancellation")); err != nil {
		t.Fatal(err)
	}
	stored := newBackup(t)

	stdout, stderr, err := run(t, "backup", "create", root, "--output", stored)
	if err == nil {
		t.Fatal("a backup of a project missing evidence reported success")
	}
	for _, want := range []string{"Complete: no", "cancellation kind=case evidence=missing", "regression kind=case evidence=verified"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing backup summary %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "puts nothing in its place") {
		t.Errorf("stderr did not report incomplete evidence: %s", stderr)
	}
	if strings.Contains(stderr, root) || strings.Contains(stderr, stored) {
		t.Errorf("a diagnostic echoed a path: %s", stderr)
	}

	// Verifying and restoring that backup say exactly the same thing, and the
	// restore creates nothing where the missing case had been.
	if stdout, _, err := run(t, "backup", "verify", stored); err == nil || !strings.Contains(stdout, "cancellation kind=case evidence=missing") {
		t.Errorf("backup verify: %v\n%s", err, stdout)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	stdout, _, err = run(t, "backup", "restore", stored, "--output", restored)
	if err == nil {
		t.Fatal("a restore of a backup missing evidence reported success")
	}
	if !strings.Contains(stdout, "cancellation kind=case recorded=missing restored=missing") {
		t.Errorf("restore did not report the missing case:\n%s", stdout)
	}
	if _, err := os.Lstat(filepath.Join(restored, "cancellation")); !os.IsNotExist(err) {
		t.Error("a restore created something where the missing case had been")
	}
}

// An index whose evidence did not come back is reported rather than written
// empty, and a damaged index is a rebuild rather than a loss: the case stays
// readable either way.
func TestBackupReportsAnIndexItCannotRebuild(t *testing.T) {
	root := indexedProject(t)
	if err := os.WriteFile(filepath.Join(root, "other.index.json"),
		[]byte(`{"schema":"readmit-index/v1","case":{"identity":"x"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stored := newBackup(t)
	stdout, _, err := run(t, "backup", "create", root, "--output", stored)
	if err == nil {
		t.Fatal("a backup whose index could not be read reported success")
	}
	if !strings.Contains(stdout, "other.index.json case=none index=undeclared") {
		t.Errorf("a damaged index was not reported:\n%s", stdout)
	}

	restored := filepath.Join(t.TempDir(), "restored")
	stdout, _, err = run(t, "backup", "restore", stored, "--output", restored)
	if err == nil {
		t.Fatal("a restore of a backup with an unreadable index reported success")
	}
	if !strings.Contains(stdout, "other.index.json case=none index=undeclared") {
		t.Errorf("a damaged index was not reported by the restore:\n%s", stdout)
	}
	if _, err := os.Lstat(filepath.Join(restored, "other.index.json")); !os.IsNotExist(err) {
		t.Error("a restore wrote an index whose declarations it could not read")
	}
	// The evidence beside it is untouched and the index that could be read was
	// built again.
	if _, _, err := run(t, "timeline", filepath.Join(restored, "regression")); err != nil {
		t.Errorf("a damaged index made the case beside it unreadable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(restored, backupIndexName)); err != nil {
		t.Errorf("the index that could be read was not rebuilt: %v", err)
	}
}

// A backup interrupted before it finished carries no completion marker, and no
// reader treats it as a usable one.
func TestBackupRefusesAnInterruptedBackup(t *testing.T) {
	root := indexedProject(t)
	stored := newBackup(t)
	if _, stderr, err := run(t, "backup", "create", root, "--output", stored); err != nil || stderr != "" {
		t.Fatalf("backup create: %v %s", err, stderr)
	}
	if err := os.Remove(filepath.Join(stored, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	for _, args := range [][]string{
		{"backup", "verify", stored},
		{"backup", "restore", stored, "--output", restored},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil {
			t.Fatalf("%v accepted an interrupted backup:\n%s", args, stdout)
		}
		if !strings.Contains(stderr, "carries no completion marker") {
			t.Errorf("%v did not report an interrupted backup: %s", args, stderr)
		}
	}
	if _, err := os.Lstat(restored); !os.IsNotExist(err) {
		t.Error("restoring an interrupted backup created a destination")
	}
}

// A damaged backup is refused whole, before anything is written.
func TestBackupRefusesADamagedBackup(t *testing.T) {
	root := indexedProject(t)
	stored := newBackup(t)
	if _, stderr, err := run(t, "backup", "create", root, "--output", stored); err != nil || stderr != "" {
		t.Fatalf("backup create: %v %s", err, stderr)
	}
	payload := filepath.Join(stored, "files", "regression", "payloads", "s0001-e000001.bin")
	data, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	stdout, stderr, err := run(t, "backup", "restore", stored, "--output", restored)
	if err == nil {
		t.Fatalf("a damaged backup restored:\n%s", stdout)
	}
	if !strings.Contains(stderr, "is not the file its document records") {
		t.Errorf("a damaged backup was not reported as such: %s", stderr)
	}
	if strings.Contains(stderr, "payloads") || strings.Contains(stderr, stored) {
		t.Errorf("a diagnostic echoed a stored path: %s", stderr)
	}
	if _, err := os.Lstat(restored); !os.IsNotExist(err) {
		t.Error("restoring a damaged backup created a destination")
	}
}

// Destinations are reserved through the shared path policy: a backup is never
// written inside the project it copies, a project is never restored inside the
// backup it comes from, and neither overwrites anything.
func TestBackupRefusesDestinationsInsideTheirSource(t *testing.T) {
	root := indexedProject(t)
	stored := newBackup(t)
	if _, stderr, err := run(t, "backup", "create", root, "--output", stored); err != nil || stderr != "" {
		t.Fatalf("backup create: %v %s", err, stderr)
	}
	for _, refused := range [][]string{
		{"backup", "create", root, "--output", filepath.Join(root, "inside")},
		{"backup", "create", root, "--output", filepath.Join(root, "regression", "inside")},
		{"backup", "create", root, "--output", stored},
		{"backup", "restore", stored, "--output", filepath.Join(stored, "inside")},
		{"backup", "restore", stored, "--output", filepath.Join(stored, "files", "inside")},
		{"backup", "restore", stored, "--output", root},
	} {
		stdout, stderr, err := run(t, refused...)
		if err == nil {
			t.Errorf("%v was accepted:\n%s", refused, stdout)
		}
		if stderr == "" {
			t.Errorf("%v reported nothing on stderr", refused)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "inside")); !os.IsNotExist(err) {
		t.Error("a refused backup created a destination inside the project")
	}
}

// Every declaration a backup needs has its own flag and no default, and a
// missing one is an error rather than a likely value.
func TestBackupRequiresItsArgumentsAndDestination(t *testing.T) {
	root := indexedProject(t)
	for _, refused := range [][]string{
		{"backup"},
		{"backup", "create"},
		{"backup", "create", root},
		{"backup", "create", root, "--output", ""},
		{"backup", "create", root, "second", "--output", newBackup(t)},
		{"backup", "verify"},
		{"backup", "restore", root},
		{"backup", "verify", filepath.Join(root, "regression")},
		{"backup", "restore", filepath.Join(root, "regression"), "--output", newBackup(t)},
	} {
		stdout, stderr, err := run(t, refused...)
		if err == nil {
			t.Errorf("%v was accepted:\n%s", refused, stdout)
		}
		if stderr == "" {
			t.Errorf("%v reported nothing on stderr", refused)
		}
	}
}
