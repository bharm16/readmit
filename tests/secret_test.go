package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/replay"
)

// The store a credential test points readmit at is this test binary, re-run
// with providerSwitch set. It emits the bytes of the file its locator argument
// names. Every value below is generated here and is test-only material: no
// credential is committed anywhere in this repository, and no test needs one.
const (
	providerSwitch       = "READMIT_TEST_SECRET_PROVIDER"
	testOnlyCredential   = "test-only-not-a-real-credential-4b71e6"
	testOnlyEndpoint     = "127.0.0.1:2575"
	credentialReferences = "lab-mllp"
)

func testOnlyProvider(mode string, args []string) int {
	if mode == "fail" {
		return 3
	}
	if len(args) != 1 {
		return 4
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return 4
	}
	os.Stdout.Write(data)
	return 0
}

func providerCommand(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// testOnlyMaterial writes the generated credential the stand-in store answers
// with. It is deliberately outside every directory a scan is pointed at.
func testOnlyMaterial(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-only-material")
	if err := os.WriteFile(path, []byte(testOnlyCredential+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// registerReference registers one reference in a new store and returns both
// the store path and the command's output.
func registerReference(t *testing.T, directory, address string, extra ...string) (string, string) {
	t.Helper()
	store := filepath.Join(directory, "secrets.json")
	args := append([]string{
		"secret", "add", "--secrets", store, "--name", credentialReferences,
		"--store", "os-keychain", "--address", address,
		"--command", providerCommand(t), "--argument", testOnlyMaterial(t),
	}, extra...)
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("secret add: %v %s", err, stderr)
	}
	return store, stdout
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var status *exec.ExitError
	if !errors.As(err, &status) {
		t.Fatalf("command did not exit with a status: %v", err)
	}
	return status.ExitCode()
}

// requireMasked fails when any readmit output or artifact repeats a credential.
func requireMasked(t *testing.T, where string, text string) {
	t.Helper()
	if strings.Contains(text, testOnlyCredential) {
		t.Fatalf("%s disclosed a credential value", where)
	}
}

// A reference is registered, shown and edited without a credential value ever
// entering readmit: there is no flag that accepts one, nothing renders one, and
// the document readmit writes does not contain one.
func TestSecretReferencesAreEditedWithoutEverRenderingACredential(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	store, stdout := registerReference(t, directory, testOnlyEndpoint, "--max-age", "720h")
	for _, want := range []string{
		"Credential reference registered: " + credentialReferences,
		"store=os-keychain", "purpose=mllp-endpoint", "address=" + testOnlyEndpoint, "generation=1",
		"value: ******** (never read, never stored, never exported)",
		"rotation: current", "max-age: 720h", "locator arguments)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("secret add omitted %q:\n%s", want, stdout)
		}
	}
	requireMasked(t, "secret add", stdout)

	shown, stderr, err := run(t, "secret", "show", "--secrets", store)
	if err != nil || stderr != "" {
		t.Fatalf("secret show: %v %s", err, stderr)
	}
	if !strings.Contains(shown, "Document: readmit-secrets/v1") || !strings.Contains(shown, "References: 1") {
		t.Fatalf("secret show:\n%s", shown)
	}
	requireMasked(t, "secret show", shown)

	// Editing changes the scope and the rotation interval of the reference. The
	// value stays masked throughout because readmit never held one.
	updated, stderr, err := run(t, "secret", "update", "--secrets", store, "--name", credentialReferences, "--max-age", "1ns")
	if err != nil || stderr != "" {
		t.Fatalf("secret update: %v %s", err, stderr)
	}
	if !strings.Contains(updated, "Credential reference updated: "+credentialReferences) || !strings.Contains(updated, "rotation: overdue") {
		t.Fatalf("an expired rotation interval was not reported as overdue:\n%s", updated)
	}
	requireMasked(t, "secret update", updated)

	document, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	requireMasked(t, "the secret reference document", string(document))
	if !strings.Contains(string(document), `"schema":"readmit-secrets/v1"`) {
		t.Fatalf("the document is not the declared contract: %s", document)
	}
}

// There is no input path for a credential value, and a reference outside its
// declared contract is refused rather than stored.
func TestSecretAddRefusesValuesAndUndeclaredReferences(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	store, _ := registerReference(t, directory, testOnlyEndpoint)
	material := testOnlyMaterial(t)
	base := []string{"secret", "add", "--secrets", store, "--command", providerCommand(t), "--argument", material}
	for name, args := range map[string][]string{
		"a credential passed as a value":  append(append([]string{}, base...), "--name", "other", "--store", "os-keychain", "--address", testOnlyEndpoint, "--value", testOnlyCredential),
		"a duplicate name":                append(append([]string{}, base...), "--name", credentialReferences, "--store", "os-keychain", "--address", testOnlyEndpoint),
		"an undeclared store":             append(append([]string{}, base...), "--name", "other", "--store", "somewhere", "--address", testOnlyEndpoint),
		"an address without a port":       append(append([]string{}, base...), "--name", "other", "--store", "os-keychain", "--address", "127.0.0.1"),
		"a command resolved through PATH": {"secret", "add", "--secrets", store, "--name", "other", "--store", "os-keychain", "--address", testOnlyEndpoint, "--command", "security"},
	} {
		stdout, stderr, err := run(t, args...)
		if err == nil {
			t.Errorf("%s was accepted:\n%s", name, stdout)
		}
		requireMasked(t, name, stdout+stderr)
	}
}

// A rotation is recorded only when the declared store answers for the
// reference, and a store that cannot answer leaves the record exactly as it was.
func TestSecretRotateRecordsAGenerationOnlyWhenTheStoreAnswers(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	store, _ := registerReference(t, directory, testOnlyEndpoint, "--max-age", "720h")
	stdout, stderr, err := run(t, "secret", "rotate", "--secrets", store, "--name", credentialReferences)
	if err != nil || stderr != "" {
		t.Fatalf("secret rotate: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Rotation recorded: "+credentialReferences) || !strings.Contains(stdout, "generation=2") {
		t.Fatalf("rotation was not recorded:\n%s", stdout)
	}
	requireMasked(t, "secret rotate", stdout)

	t.Setenv(providerSwitch, "fail")
	stdout, stderr, err = run(t, "secret", "rotate", "--secrets", store, "--name", credentialReferences)
	if err == nil {
		t.Fatalf("a rotation was recorded against a store that did not answer:\n%s", stdout)
	}
	if !strings.Contains(stderr, "the recorded rotation is unchanged") {
		t.Fatalf("refusal did not say the record was unchanged: %s", stderr)
	}
	requireMasked(t, "a refused rotation", stdout+stderr)

	t.Setenv(providerSwitch, "emit")
	shown, _, err := run(t, "secret", "show", "--secrets", store)
	if err != nil || !strings.Contains(shown, "generation=2") {
		t.Fatalf("the refused rotation changed the record:\n%s", shown)
	}
	if _, _, err := run(t, "secret", "rotate", "--secrets", "absent.json", "--name", credentialReferences); err == nil {
		t.Fatal("a rotation was recorded against a store that is not there")
	}
}

// The scan is the delivery's credential-leakage check. It finds a known value
// in a manifest, a log, a report and local browser state, reports the locations
// without repeating the value, and exits with a distinct status.
func TestSecretScanFindsAPlantedCredentialAndReportsLocationsOnly(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	store, _ := registerReference(t, directory, testOnlyEndpoint)
	evidence := filepath.Join(directory, "evidence")
	planted := map[string]string{
		filepath.Join("run", "manifest.json"):   `{"schema":"readmit-run/v1","note":"` + testOnlyCredential + `"}`,
		filepath.Join("logs", "readmit.log"):    "operation=replay credential=" + testOnlyCredential,
		filepath.Join("report", "report.md"):    "encoded: " + base64.StdEncoding.EncodeToString([]byte(testOnlyCredential)),
		filepath.Join("browser", "state.json"):  `{"saved":"` + testOnlyCredential + `"}`,
		filepath.Join("clean", "manifest.json"): `{"schema":"readmit-run/v1","credential":"` + credentialReferences + `"}`,
	}
	for name, body := range planted {
		path := filepath.Join(evidence, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, err := run(t, "secret", "scan", "--secrets", store, evidence)
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("scan exited %d, want 1:\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"Credential leakage scan", "References checked: 1", "Status: blocked",
		"Entries not read: 0",
		"Locations holding a known credential value: 4",
		"run/manifest.json", "logs/readmit.log", "report/report.md", "browser/state.json",
		"This is one check, not proof.",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("scan omitted %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "clean/manifest.json") {
		t.Fatalf("a document holding only the reference name was reported:\n%s", stdout)
	}
	requireMasked(t, "the scan report", stdout+stderr)
}

// A credential that cannot be resolved is an error, never a clean scan: an
// unavailable check does not pass.
func TestSecretScanRefusesWhenACredentialCannotBeResolved(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	store, _ := registerReference(t, directory, testOnlyEndpoint)
	t.Setenv(providerSwitch, "fail")
	stdout, stderr, err := run(t, "secret", "scan", "--secrets", store, directory)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("scan exited %d, want 2:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "this scan checked nothing") {
		t.Fatalf("the refusal did not say the scan established nothing: %s", stderr)
	}
	if strings.Contains(stdout, "Status: passed") {
		t.Fatalf("an unresolvable credential produced a clean scan:\n%s", stdout)
	}
}

// A target configuration carries a reference; run evidence records the
// transport. Neither holds a credential, and the scan is what says so.
func TestSecretScanPassesOverConfigurationAndRunEvidence(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	source := replayCase(t)
	address, wait := startFixtureReceiver(t, directory)
	store, _ := registerReference(t, directory, address)
	target := credentialTargetFile(t, directory, address)
	output := filepath.Join(directory, "run")
	stdout, stderr, err := run(t, "replay", source, "--target", target, "--send", "--output", output)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Schema: readmit-run/v1") {
		t.Fatalf("replay with a credential-bearing target: %v %s %s", err, stdout, stderr)
	}
	wait()
	scanned, stderr, err := run(t, "secret", "scan", "--secrets", store, target, output)
	if err != nil || stderr != "" {
		t.Fatalf("scan: %v %s\n%s", err, stderr, scanned)
	}
	for _, want := range []string{"Status: passed", "Entries not read: 0", "Locations holding a known credential value: 0"} {
		if !strings.Contains(scanned, want) {
			t.Fatalf("scan omitted %q:\n%s", want, scanned)
		}
	}
	configuration, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configuration), `"schema":"readmit-target/v2"`) || !strings.Contains(string(configuration), credentialReferences) {
		t.Fatalf("the shared configuration did not carry the reference: %s", configuration)
	}
	requireMasked(t, "the shared configuration", string(configuration))
}

// A target whose credential is scoped to another endpoint is refused before any
// connection is opened, and the refusal names no credential.
func TestReplayRefusesATargetWhoseCredentialIsScopedElsewhere(t *testing.T) {
	t.Setenv(providerSwitch, "emit")
	directory := t.TempDir()
	source := replayCase(t)
	registerReference(t, directory, "127.0.0.1:65535")
	target := credentialTargetFile(t, directory, testOnlyEndpoint)
	stdout, stderr, err := run(t, "replay", source, "--target", target)
	if err == nil {
		t.Fatalf("a credential scoped to another endpoint was accepted:\n%s", stdout)
	}
	if !strings.Contains(stderr, "scoped to a different endpoint address") {
		t.Fatalf("refusal did not name the scope: %s", stderr)
	}
	requireMasked(t, "the refusal", stdout+stderr)
}

// credentialTargetFile writes a readmit-target/v2 configuration that names the
// reference beside it. The secrets document is a relative reference resolved
// against the target file, exactly as a CA file is.
func credentialTargetFile(t *testing.T, directory, address string) string {
	t.Helper()
	data, err := json.Marshal(replay.Target{
		Schema: replay.TargetSchemaV2, TestEndpoint: true, Address: address, Transport: "plain",
		ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096,
		Credential: replay.Credential{SecretsFile: "secrets.json", Reference: credentialReferences},
	}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "target.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// startFixtureReceiver runs the bounded fixture receiver until two messages
// have been handled, and returns its address and a function that waits for it.
func startFixtureReceiver(t *testing.T, directory string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	command := exec.CommandContext(ctx, binary, "listen", "--address", "127.0.0.1:0", "--mode", "fixed",
		"--output", filepath.Join(directory, "recorded"), "--observation", filepath.Join(directory, "observation.json"),
		"--max-messages", "2", "--idle-timeout", "5s")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(pipe)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("receiver not ready: %v", err)
	}
	address := strings.TrimSpace(strings.TrimPrefix(ready, "Listening: "))
	return address, func() {
		t.Helper()
		_, _ = io.Copy(io.Discard, reader)
		if err := command.Wait(); err != nil {
			t.Fatalf("receiver: %v %s", err, diagnostic.String())
		}
	}
}
