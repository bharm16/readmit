//go:build !windows

package desktop_test

// A program an operator declared runs by the absolute path the operator gave
// and may connect wherever it is configured to; Readmit cannot see where. So
// the privacy status says when one runs: its declared-program row is active
// exactly while such a program is running, says whose program it is, and is
// idle otherwise. Each test here declares a program it holds. Every run of it
// says it has started and then waits, and the test reads the privacy status
// before it releases that run.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
)

// heldProgram is an operator-declared program under the test's control. Each
// run marks that it has started and waits to be released, for at most about
// fifteen seconds before it fails; then it does what its tail says, by default
// printing the file its arguments name, as a locator whose arguments select a
// stored value does.
type heldProgram struct {
	path, runs string
}

func newHeldProgram(t *testing.T, tail string) *heldProgram {
	t.Helper()
	directory := t.TempDir()
	runs := filepath.Join(directory, "runs")
	if err := os.Mkdir(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	if tail == "" {
		tail = `exec /bin/cat "$@"`
	}
	path := filepath.Join(directory, "readmit-test-declared-program.sh")
	script := "#!/bin/sh\n" +
		": > '" + runs + "/started-'$$\n" +
		"waited=0\n" +
		"while [ ! -e '" + runs + "/release-'$$ ]; do\n" +
		"  [ \"$waited\" -ge 1500 ] && exit 1\n" +
		"  sleep 0.01; waited=$((waited + 1))\n" +
		"done\n" +
		tail + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return &heldProgram{path: path, runs: runs}
}

// during runs call, one operation, and each time a run of the program has
// started and is waiting, reads the privacy status and then releases it.
// Nothing is active before the operation; every reading while a program
// waits is exactly want, with the declared-program row saying phrase; there
// is at least one; and once the operation has answered nothing is active.
func (p *heldProgram) during(t *testing.T, app *desktop.App, operation, want, phrase string, call func()) {
	t.Helper()
	if before := activeNow(app); before != "" {
		t.Fatalf("before %s the privacy status reports %q active", operation, before)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		call()
	}()
	released := map[string]bool{}
	deadline := time.After(30 * time.Second)
	for finished := false; !finished; {
		select {
		case <-done:
			finished = true
		case <-deadline:
			t.Fatalf("%s did not answer within 30s", operation)
		case <-time.After(2 * time.Millisecond):
		}
		entries, err := os.ReadDir(p.runs)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			run, started := strings.CutPrefix(entry.Name(), "started-")
			if !started || released[run] {
				continue
			}
			if reading, detail := activeNow(app), declaredProgramDetail(app); reading != want || !strings.Contains(detail, phrase) {
				t.Errorf("while %s ran its declared program the privacy status read %q, %q; want %q active, saying %q", operation, reading, detail, want, phrase)
			}
			if err := os.WriteFile(filepath.Join(p.runs, "release-"+run), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			released[run] = true
		}
	}
	if len(released) == 0 {
		t.Errorf("%s never ran its declared program", operation)
	}
	if after := activeNow(app); after != "" {
		t.Errorf("after %s the privacy status still reports %q active", operation, after)
	}
	// The next operation's runs start from nothing, whatever process
	// identifiers they are given.
	for run := range released {
		os.Remove(filepath.Join(p.runs, "started-"+run))
		os.Remove(filepath.Join(p.runs, "release-"+run))
	}
}

// Testing, rotating and scanning a credential reference each run the locator
// the reference declares, and the declared-program row is active while it
// runs and idle before and after. A test refused before its locator runs
// never makes the row active.
func TestDisclosureStatusReportsACredentialLocatorActiveWhileItRuns(t *testing.T) {
	app := workspaceApp(t)
	program := newHeldProgram(t, "")
	workspace := t.TempDir()
	writeDocument(t, workspace, "stored-credential", "test-only-not-a-real-credential-7d31")
	saved := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: secret.Reference{
		Name: "lab-mllp", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint, Address: "127.0.0.1:2575",
		Command: program.path, Arguments: []string{filepath.Join(workspace, "stored-credential")}, MaxAge: "720h",
	}})
	if saved.State != desktop.Completed {
		t.Fatalf("reference: %+v", saved)
	}
	writeDocument(t, workspace, "notes.txt", "synthetic notes")

	program.during(t, app, "a credential reference test", "declared-program", "the credential reference being tested", func() {
		if tested := app.TestSecretReference(workspace, "secrets.json", "lab-mllp"); tested.State != desktop.Completed || !tested.Success {
			t.Errorf("test: %+v", tested)
		}
	})
	program.during(t, app, "a credential rotation", "declared-program", "the credential reference being rotated", func() {
		if rotated := app.RotateSecretReference(workspace, "secrets.json", "lab-mllp"); rotated.State != desktop.Completed {
			t.Errorf("rotation: %+v", rotated)
		}
	})
	program.during(t, app, "a residual credential scan", "declared-program", "each credential reference a residual scan checks for", func() {
		if scanned := app.ScanSecrets(desktop.SecretScanRequest{Workspace: workspace, SecretsFile: "secrets.json", Paths: []string{"notes.txt"}}); scanned.State != desktop.Completed {
			t.Errorf("scan: %+v", scanned)
		}
	})

	for reading := range sampling(app, func() {
		if refused := app.TestSecretReference(workspace, "secrets.json", "absent"); refused.State != desktop.Failed {
			t.Errorf("a test of an unregistered reference: %+v", refused)
		}
	}) {
		if reading != "" {
			t.Errorf("a test refused before its locator ran read %q active", reading)
		}
	}
}

// A connectivity check and a fixture reset against an environment whose
// client certificate names its private key through a credential reference
// each run that reference's locator before they connect, and the environment
// row is active beside the declared program while it runs.
func TestDisclosureStatusReportsAClientCertificateKeyLocatorActiveWhileItRuns(t *testing.T) {
	app := workspaceApp(t)
	program := newHeldProgram(t, "")
	workspace := t.TempDir()
	authority := newHubTestAuthority(t, "lab-mllp-ca")
	certificate, key := authority.issue(t, "readmit-lab-client", false)
	for name, data := range map[string][]byte{"ca.pem": authority.pem, "client.pem": certificate, "client-key.pem": key} {
		writeDocument(t, workspace, name, string(data))
	}
	// Nothing listens here: the locator runs before the check connects.
	address := "127.0.0.1:1"
	saved := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: secret.Reference{
		Name: "lab-mtls-key", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint, Address: address,
		Command: program.path, Arguments: []string{filepath.Join(workspace, "client-key.pem")}, MaxAge: "720h",
	}})
	if saved.State != desktop.Completed {
		t.Fatalf("reference: %+v", saved)
	}
	recorded := app.ReadTarget(workspace, "target.json")
	if recorded.State != desktop.Completed || recorded.Target == nil {
		t.Fatalf("new target: %+v", recorded)
	}
	environment := *recorded.Target
	environment.Name, environment.Classification, environment.Address = "lab-mtls", "nonproduction", address
	environment.Transport, environment.CAFile, environment.ServerName = "tls", filepath.Join(workspace, "ca.pem"), "localhost"
	environment.ClientCertificate = filepath.Join(workspace, "client.pem")
	environment.Credential = replay.Credential{SecretsFile: filepath.Join(workspace, "secrets.json"), Reference: "lab-mtls-key"}
	if saved := app.SaveTarget(desktop.TargetSaveRequest{Workspace: workspace, TargetFile: "target.json", Target: environment}); saved.State != desktop.Completed {
		t.Fatalf("target: %+v", saved)
	}
	writeDocument(t, workspace, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	writeDocument(t, workspace, "reset-plan.json", `{"schema":"readmit-reset-plan/v1","environment":"lab-mtls","actions":[`+
		`{"id":"quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"Confirm the endpoint is quiet"}]}`)
	program.during(t, app, "a connectivity check", "environment,declared-program", "the connectivity check's client certificate", func() {
		if checked := app.CheckTarget(desktop.TargetCheckRequest{Workspace: workspace, TargetFile: "target.json", PolicyFile: "policy.json"}); checked.Report == nil {
			t.Errorf("check: %+v", checked)
		}
	})
	program.during(t, app, "a fixture reset", "environment,declared-program", "the client certificate of a fixture reset's check", func() {
		app.ResetTarget(desktop.TargetResetRequest{Workspace: workspace, TargetFile: "target.json", PlanFile: "reset-plan.json",
			OutcomeFile: "reset-outcome.json", PolicyFile: "policy.json"})
	})
}

// Connecting to the hub and diagnosing it each run the key command the
// selected hub configuration declares, and the privacy status reports the
// declared program active beside the hub while it runs. Once the command has
// ended the hub request alone is active, which the hub test holds.
func TestDisclosureStatusReportsTheHubKeyCommandActiveWhileItRuns(t *testing.T) {
	app := newApp(t, &chooser{})
	program := newHeldProgram(t, "")
	configuration := writeHubClientConfig(t, t.TempDir(), "https://127.0.0.1:1")
	written, err := os.ReadFile(configuration)
	if err != nil {
		t.Fatal(err)
	}
	held := strings.Replace(string(written), `"command":"/bin/cat"`, `"command":"`+program.path+`"`, 1)
	if held == string(written) {
		t.Fatal("the hub configuration names no key command to hold")
	}
	if err := os.WriteFile(configuration, []byte(held), 0o600); err != nil {
		t.Fatal(err)
	}
	if selected := app.SelectHubConfig(configuration); selected.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", selected)
	}
	phrase := "the key command of the selected hub configuration"
	program.during(t, app, "connecting to the hub", "hub,declared-program", phrase, func() { app.ConnectHub() })
	program.during(t, app, "a hub diagnosis", "hub,declared-program", phrase, func() { app.DiagnoseHub() })
}

// Rotating a protection control, packing a package under it and opening that
// package each read the key from the program the control declares.
func TestDisclosureStatusReportsAProtectionKeyProgramActiveWhileItRuns(t *testing.T) {
	app := workspaceApp(t)
	program := newHeldProgram(t, "")
	root := t.TempDir()
	writeDocument(t, root, "evidence.txt", "synthetic retained evidence bytes")
	material := filepath.Join(t.TempDir(), "test-only-key-material")
	if err := os.WriteFile(material, []byte("test-only-not-a-real-key-4f8c1d2e6b0a9357"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "protection.json", Name: "lab-evidence", Storage: "os-volume-encryption",
		Command: program.path, Arguments: []string{material}, MaxAge: "720h", Retain: "1h",
	})
	if registered.State != desktop.Completed {
		t.Fatalf("control: %+v", registered)
	}
	phrase := "the key program of the protection control"
	program.during(t, app, "a protection control rotation", "declared-program", phrase, func() {
		if rotated := app.RotateProtectionControl(root, "protection.json", "lab-evidence"); rotated.State != desktop.Completed {
			t.Errorf("rotation: %+v", rotated)
		}
	})
	program.during(t, app, "packing a protected package", "declared-program", phrase, func() {
		packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: "protection.json", Control: "lab-evidence",
			Sources: []string{"evidence.txt"}, Output: "transfer"})
		if packed.State != desktop.Completed {
			t.Errorf("pack: %+v", packed)
		}
	})
	program.during(t, app, "opening a protected package", "declared-program", phrase, func() {
		opened := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{Workspace: root, Entry: "protection.json", Package: "transfer", Output: "opened"})
		if opened.State != desktop.Completed {
			t.Errorf("open: %+v", opened)
		}
	})
}

// Enrolling a runner and executing its job each read the key and token the
// runner configuration's commands declare; the token command here is held,
// and the runner row is active beside the declared program while it runs.
func TestDisclosureStatusReportsARunnerTokenCommandActiveWhileItRuns(t *testing.T) {
	fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
	program := newHeldProgram(t, "")
	request := runnerConfigInput(t, mustPrivateRoot(t))
	request.Hub, request.Project, request.CA, request.Certificate = fixture.serverURL, "cardio-study", fixture.caPath, fixture.runnerCert
	request.Key = desktop.RunnerReferenceInput{Command: fixture.runnerKey, Arguments: []string{}}
	request.Token = desktop.RunnerReferenceInput{Command: program.path, Arguments: []string{fixture.runnerToken}}
	request.Output = filepath.Join(t.TempDir(), "runner.json")
	if saved := fixture.app.SaveRunnerConfig(request); saved.State != desktop.Completed {
		t.Fatalf("runner configuration: %+v", saved)
	}
	job := filepath.Join(t.TempDir(), "job.json")
	if saved := fixture.app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: writableSpec(t, t.TempDir()), Output: job}); saved.State != desktop.Completed {
		t.Fatalf("job: %+v", saved)
	}
	program.during(t, fixture.app, "a runner enrollment", "runner,declared-program", "the runner configuration an enrollment presents", func() {
		fixture.app.EnrollRunner(request.Output)
	})
	program.during(t, fixture.app, "a runner execution", "runner,declared-program", "the runner configuration a runner execution presents", func() {
		fixture.app.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: request.Output, JobPath: job})
	})
	if fixture.admissions() == 0 {
		t.Fatal("no runner work reached the hub's runner admission")
	}
}

// Checking a source's access and collecting from it each run the transfer
// program the source registration declares, and the capture row is active
// beside the declared program while it runs.
func TestDisclosureStatusReportsATransferProgramActiveWhileItRuns(t *testing.T) {
	app := workspaceApp(t)
	message := "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||1||DOE^JOHN\r"
	program := newHeldProgram(t, "MESSAGE='"+message+"'\n"+`case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  printf '%s' "$MESSAGE" ;;
esac`)
	root := t.TempDir()
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Transfer, Scope: "appointments",
		Address: "127.0.0.1:2222", Classification: "nonproduction", Command: program.path,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: source}); saved.State != desktop.Completed {
		t.Fatalf("source: %+v", saved)
	}
	writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	program.during(t, app, "a source access check", "capture,declared-program", "for a source access check", func() {
		if checked := app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json"}); checked.State != desktop.Completed {
			t.Errorf("access check: %+v", checked)
		}
	})
	program.during(t, app, "a source collection", "capture,declared-program", "for a source collection", func() {
		collected := app.CollectSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", PolicyFile: "policy.json",
			OutputName: "staged", ReceiptName: "receipt.json"})
		if collected.State != desktop.Completed {
			t.Errorf("collection: %+v", collected)
		}
	})
}
