package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both values below are generated here and are test-only material. No key is
// committed anywhere in this repository, and no test needs a real one.
const (
	testOnlyKeyMaterial      = "test-only-not-a-real-key-2f9a41c7d05b83e6"
	testOnlyOtherKeyMaterial = "test-only-not-a-real-key-000000000000000f"
	protectControl           = "lab-evidence"
)

// keyMaterial writes the bytes the stand-in key store answers with. It is
// deliberately outside every directory a test packs.
func keyMaterial(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-only-key-material")
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// registerControl registers one control in a new protection document.
func registerControl(t *testing.T, directory, name, material string, extra ...string) string {
	t.Helper()
	document := filepath.Join(directory, "protection.json")
	args := append([]string{
		"protect", "register", "--protection", document, "--name", name,
		"--storage", "os-volume-encryption", "--command", providerCommand(t),
		"--argument", keyMaterial(t, material),
	}, extra...)
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("protect register: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "********") || strings.Contains(stdout, material) {
		t.Fatalf("protect register rendered key material:\n%s", stdout)
	}
	return document
}

// evidence writes a small stand-in for retained run evidence.
func evidence(t *testing.T, directory string) string {
	t.Helper()
	root := filepath.Join(directory, "run-2026-09-18")
	if err := os.MkdirAll(filepath.Join(root, "observations"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"manifest.json":            `{"schema":"readmit-run/v1"}`,
		"observations/ledger.json": `{"schema":"readmit-observation/v1","subject":"SYNTH-001"}`,
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestProtectRegistersAControlWithoutEverRenderingAKey(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial, "--max-age", "720h", "--retain", "2160h")

	stdout, stderr, err := run(t, "protect", "show", "--protection", document)
	if err != nil || stderr != "" {
		t.Fatalf("protect show: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Document: readmit-protection/v1", "Controls: 1",
		"storage=os-volume-encryption (declared, never verified)", "state=active", "generation=1",
		"key: ******** (never stored, never written, never exported)",
		"rotation: current", "retain: 2160h", "1 locator arguments",
		"Removing a package unlinks it; it does not erase it",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("protect show is missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, testOnlyKeyMaterial) {
		t.Fatal("protect show rendered key material")
	}
	stored, err := os.ReadFile(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), testOnlyKeyMaterial) {
		t.Fatal("the protection document holds key material")
	}

	// A second registration under the same name, an undeclared storage kind and
	// a command resolved through PATH are each refused.
	for _, refused := range [][]string{
		{"protect", "register", "--protection", document, "--name", protectControl, "--storage", "os-volume-encryption", "--command", providerCommand(t)},
		{"protect", "register", "--protection", document, "--name", "second", "--storage", "hoped-for", "--command", providerCommand(t)},
		{"protect", "register", "--protection", document, "--name", "third", "--storage", "customer-key", "--command", "security"},
		{"protect", "show"},
	} {
		if _, _, err := run(t, refused...); err == nil {
			t.Errorf("%v was accepted", refused)
		}
	}
}

func TestProtectPacksAndOpensEvidenceWithoutRewritingIt(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial, "--retain", "2160h")
	source := evidence(t, directory)
	before, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	packet := filepath.Join(directory, "packet")
	stdout, stderr, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl, "--output", packet, source)
	if err != nil || stderr != "" {
		t.Fatalf("protect pack: %v %s", err, stderr)
	}
	for _, want := range []string{"Document: readmit-transfer/v1", "Encryption: aes-256-gcm with hkdf-sha256", "Entries: 2", "Files packed: 2", "Retention: within-retention"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("protect pack is missing %q:\n%s", want, stdout)
		}
	}
	after, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("packing altered the evidence it read")
	}

	stdout, stderr, err = run(t, "protect", "inspect", packet)
	if err != nil || stderr != "" {
		t.Fatalf("protect inspect: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Packed names: encrypted in the package index") {
		t.Errorf("protect inspect does not say where the names are:\n%s", stdout)
	}
	for _, leak := range []string{"run-2026-09-18", "manifest.json", "ledger.json", "SYNTH-001", testOnlyKeyMaterial} {
		if strings.Contains(stdout, leak) {
			t.Errorf("protect inspect rendered %q without a key", leak)
		}
	}

	opened := filepath.Join(directory, "opened")
	stdout, stderr, err = run(t, "protect", "open", "--protection", document, "--package", packet, "--output", opened)
	if err != nil || stderr != "" {
		t.Fatalf("protect open: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Files written: 2") {
		t.Errorf("protect open reported:\n%s", stdout)
	}
	reopened, err := os.ReadFile(filepath.Join(opened, "run-2026-09-18", "manifest.json"))
	if err != nil || string(reopened) != string(before) {
		t.Fatalf("the opened package does not hold the packed bytes: %q (%v)", reopened, err)
	}
}

func TestProtectRefusesAWrongKeyARotatedKeyATamperedPackageAndARetainedDestination(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial)
	source := evidence(t, directory)
	packet := filepath.Join(directory, "packet")
	if _, stderr, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl, "--output", packet, source); err != nil {
		t.Fatalf("protect pack: %v %s", err, stderr)
	}

	// A control of the same name reading different material cannot open the
	// package, and the refusal names the key rather than guessing at the cause.
	elsewhere := t.TempDir()
	other := registerControl(t, elsewhere, protectControl, testOnlyOtherKeyMaterial)
	_, stderr, err := run(t, "protect", "open", "--protection", other, "--package", packet, "--output", filepath.Join(directory, "wrong-key"))
	if err == nil {
		t.Fatal("a control that reads different key material opened the package")
	}
	if !strings.Contains(stderr, "not written with the key this control reads") {
		t.Errorf("the refusal did not name the key: %s", stderr)
	}

	// Once that control records a rotation, the same refusal says which of the
	// two it is: readmit never held either key and cannot tell them apart by
	// any other means than the generation the package recorded.
	if _, stderr, err := run(t, "protect", "rotate", "--protection", other, "--name", protectControl); err != nil {
		t.Fatalf("protect rotate: %v %s", err, stderr)
	}
	_, stderr, err = run(t, "protect", "open", "--protection", other, "--package", packet, "--output", filepath.Join(directory, "rotated"))
	if err == nil {
		t.Fatal("a rotated-away key opened the package")
	}
	if !strings.Contains(stderr, "earlier key generation") {
		t.Errorf("the refusal did not name the generation: %s", stderr)
	}

	// Naming a control the package was not written under is refused before any
	// key is read at all.
	if _, stderr, err := run(t, "protect", "register", "--protection", document, "--name", "second",
		"--storage", "customer-key", "--command", providerCommand(t), "--argument", keyMaterial(t, testOnlyOtherKeyMaterial)); err != nil {
		t.Fatalf("protect register: %v %s", err, stderr)
	}
	_, stderr, err = run(t, "protect", "open", "--protection", document, "--name", "second", "--package", packet, "--output", filepath.Join(directory, "wrong-control"))
	if err == nil {
		t.Fatal("a control the package was not written under opened it")
	}
	if !strings.Contains(stderr, "different protection control") {
		t.Errorf("the refusal did not name the control: %s", stderr)
	}

	// Truncating an entry is named by the descriptor's own integrity check.
	entry := filepath.Join(packet, "e0001.bin")
	sealed, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, sealed[:len(sealed)-1], 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = run(t, "protect", "open", "--protection", document, "--package", packet, "--output", filepath.Join(directory, "tampered"))
	if err == nil {
		t.Fatal("a truncated package was opened")
	}
	if !strings.Contains(stderr, "size and digest the descriptor records") {
		t.Errorf("the refusal did not name the integrity check: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(directory, "tampered")); !os.IsNotExist(err) {
		t.Error("a refused open left an output directory behind")
	}

	// Retained evidence is immutable: a package is a new artifact beside it.
	sealedCase := filepath.Join(directory, "sealed-case")
	if err := os.Mkdir(sealedCase, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sealedCase, "identity.sha256"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl,
		"--output", filepath.Join(sealedCase, "packet"), source); err == nil {
		t.Error("a package was written inside retained evidence")
	}
}

func TestProtectRotateRecordsAGenerationOnlyWhenTheStoreAnswers(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial, "--max-age", "720h")

	t.Setenv(providerSwitch, "fail")
	if _, _, err := run(t, "protect", "rotate", "--protection", document, "--name", protectControl); err == nil {
		t.Fatal("a rotation was recorded against a key that did not resolve")
	}
	stdout, _, err := run(t, "protect", "show", "--protection", document)
	if err != nil || !strings.Contains(stdout, "generation=1") {
		t.Fatalf("the refused rotation changed the record:\n%s", stdout)
	}

	t.Setenv(providerSwitch, "emit")
	stdout, stderr, err := run(t, "protect", "rotate", "--protection", document, "--name", protectControl)
	if err != nil || stderr != "" {
		t.Fatalf("protect rotate: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "generation=2") {
		t.Errorf("protect rotate reported:\n%s", stdout)
	}
	if _, _, err := run(t, "protect", "rotate", "--protection", document, "--name", "absent"); err == nil {
		t.Error("an unregistered control was rotated")
	}
}

func TestProtectRetireStopsNewPackagesAndStillOpensOldOnes(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial)
	source := evidence(t, directory)
	packet := filepath.Join(directory, "packet")
	if _, stderr, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl, "--output", packet, source); err != nil {
		t.Fatalf("protect pack: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "protect", "retire", "--protection", document, "--name", protectControl)
	if err != nil || stderr != "" {
		t.Fatalf("protect retire: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "state=retired") {
		t.Errorf("protect retire reported:\n%s", stdout)
	}
	if _, _, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl, "--output", filepath.Join(directory, "second"), source); err == nil {
		t.Error("a retired control wrote a new package")
	}
	if _, stderr, err := run(t, "protect", "open", "--protection", document, "--package", packet, "--output", filepath.Join(directory, "opened")); err != nil {
		t.Fatalf("a retired control did not open its own package: %v %s", err, stderr)
	}
}

func TestProtectDiscardStatesWhatRemovingAPackageDoesNotEstablish(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	document := registerControl(t, directory, protectControl, testOnlyKeyMaterial, "--retain", "1h")
	packet := filepath.Join(directory, "packet")
	if _, stderr, err := run(t, "protect", "pack", "--protection", document, "--name", protectControl, "--output", packet, evidence(t, directory)); err != nil {
		t.Fatalf("protect pack: %v %s", err, stderr)
	}
	if err := os.WriteFile(filepath.Join(packet, "notes.txt"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "protect", "discard", packet); err == nil {
		t.Error("a package directory holding an undeclared file was removed")
	}
	if err := os.Remove(filepath.Join(packet, "notes.txt")); err != nil {
		t.Fatal(err)
	}

	// The declared retention gates the destructive action rather than being
	// reported after it, and the refusal names the instant it was declared to.
	_, stderr, err := run(t, "protect", "discard", packet)
	if err == nil {
		t.Fatal("a package inside its declared retention was removed")
	}
	if !strings.Contains(stderr, "retained until") {
		t.Errorf("the refusal did not name the declared retention: %s", stderr)
	}
	stdout, stderr, err := run(t, "protect", "discard", "--override-retention", packet)
	if err != nil || stderr != "" {
		t.Fatalf("protect discard: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Files unlinked: 4", "Declared retention: within-retention",
		"Declared retention overridden: retained until",
		"It does not overwrite the bytes, and it establishes nothing about copies",
		"readmit never held the key and cannot destroy it",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("protect discard is missing %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(packet); !os.IsNotExist(err) {
		t.Error("the discarded package directory is still there")
	}
	if _, _, err := run(t, "protect", "discard", packet); err == nil {
		t.Error("a removed package was discarded twice")
	}
}
