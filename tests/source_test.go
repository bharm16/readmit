package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The documents below are authored here rather than produced by the engine, so
// the command is exercised against text an operator could have written.
const sourceDirectoryDocument = `{
  "schema": "readmit-source/v1",
  "name": "scheduling-exports",
  "kind": "directory",
  "scope": "appointments",
  "root": "ROOT",
  "quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},
  "retry": {"attempts": 1, "backoff": "0s"}
}`

const sourceAPIDocument = `{
  "schema": "readmit-source/v1",
  "name": "scheduling-api",
  "kind": "api",
  "scope": "appointments",
  "address": "127.0.0.1:8443",
  "classification": "nonproduction",
  "quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},
  "retry": {"attempts": 1, "backoff": "0s"}
}`

const sourceMessage = "MSH|^~\\&|SEND|FAC|RECV|FAC|20260103110000||SIU^S12|MSG00001|P|2.5.1\r" +
	"SCH|1||||||||||||||||||||||||BOOKED\r"

// sourceDeclarations writes one directory source over a tree holding the named
// entries and returns the working directory and the declaration path.
func sourceDeclarations(t *testing.T, entries map[string]string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	root := filepath.Join(directory, "exports")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	declaration := filepath.Join(directory, "source.json")
	if err := os.WriteFile(declaration, []byte(strings.Replace(sourceDirectoryDocument, "ROOT", root, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	return directory, declaration
}

// sourcePlanFlags are the declarations both subcommands run under. Nothing here
// has a default, exactly as an import declares nothing by default.
func sourcePlanFlags() []string {
	return []string{"--framing", "raw", "--terminator", "cr", "--encoding", "utf-8", "--direction", "inbound", "--member", ".hl7"}
}

func TestSourceCollectStagesEvidenceAndWritesItsReceipt(t *testing.T) {
	directory, declaration := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage, "notes.md": "not evidence"})
	output := filepath.Join(directory, "collected")
	receipt := filepath.Join(directory, "collection.json")
	stdout, stderr, err := run(t, append([]string{"source", "collect", declaration, "--output", output, "--receipt", receipt}, sourcePlanFlags()...)...)
	if err != nil {
		t.Fatalf("source collect: %v; stderr=%s", err, stderr)
	}
	for _, expected := range []string{
		"Collection receipt: readmit-source-collection/v1", "Status: complete",
		"Run state: none; the observation completed", "Collected: 1", "Excluded: 1",
		"a.hl7 collected", "notes.md excluded",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("collection summary did not report %q:\n%s", expected, stdout)
		}
	}
	if strings.Contains(stdout+stderr, "MSH|") {
		t.Fatalf("the command displayed a byte of the evidence:\n%s%s", stdout, stderr)
	}
	staged, err := os.ReadFile(filepath.Join(output, "a.hl7"))
	if err != nil || string(staged) != sourceMessage {
		t.Fatalf("staged bytes = %q (err %v), want the source's own bytes unchanged", staged, err)
	}
	// The staged directory is an ordinary container, so the import that follows
	// a collection is the import readmit already has.
	imported := filepath.Join(directory, "incident.case")
	if _, importErr, err := run(t, append([]string{"import", "--folder", output, "--output", imported,
		"--receipt", filepath.Join(directory, "import.json")}, sourcePlanFlags()...)...); err != nil {
		t.Fatalf("import of the staged evidence: %v; stderr=%s", err, importErr)
	}
	var recorded struct {
		Schema string `json:"schema"`
		Status string `json:"status"`
		Totals struct {
			Collected int `json:"collected"`
		} `json:"totals"`
		Identity string `json:"identity"`
	}
	data, err := os.ReadFile(receipt)
	if err != nil || json.Unmarshal(data, &recorded) != nil {
		t.Fatalf("the receipt does not read back: %v", err)
	}
	if recorded.Schema != "readmit-source-collection/v1" || recorded.Status != "complete" || recorded.Totals.Collected != 1 || len(recorded.Identity) != 64 {
		t.Fatalf("receipt = %+v, want one completed collection with an identity", recorded)
	}
	if strings.Contains(string(data), "MSH|") || strings.Contains(string(data), directory) {
		t.Fatal("the receipt carries a byte of the evidence or the source path")
	}
}

// A collection that could not read part of its source exits non-zero and says
// so. It never reports what it staged as the whole of the declared scope.
func TestSourceCollectExitsTwoWhenTheCollectionDidNotComplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged user reads a mode 0000 file, so there is no unreadable entry to report")
	}
	directory, declaration := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage, "b.hl7": sourceMessage + "\r"})
	unreadable := filepath.Join(directory, "exports", "b.hl7")
	if err := os.Chmod(unreadable, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0600) })
	output := filepath.Join(directory, "collected")
	stdout, stderr, err := run(t, append([]string{"source", "collect", declaration, "--output", output,
		"--receipt", filepath.Join(directory, "collection.json")}, sourcePlanFlags()...)...)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("exit code = %d, want 2 for a collection that did not complete", code)
	}
	for _, expected := range []string{"Status: failed", "Run state: execution_error", "Unreadable: 1", "b.hl7 unreadable"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("collection summary did not report %q:\n%s", expected, stdout)
		}
	}
	if !strings.Contains(stderr, "the collection did not complete") {
		t.Fatalf("the diagnostic did not name the failure: %q", stderr)
	}
}

// The permission diagnosis reports what access was available and collects
// nothing. An unsupported source is an execution error, never a pass.
func TestSourceDiagnoseReportsAvailableAccessAndCollectsNothing(t *testing.T) {
	directory, declaration := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage})
	report := filepath.Join(directory, "access.json")
	stdout, stderr, err := run(t, append([]string{"source", "diagnose", declaration, "--report", report}, sourcePlanFlags()...)...)
	if err != nil {
		t.Fatalf("source diagnose: %v; stderr=%s", err, stderr)
	}
	for _, expected := range []string{"Access diagnosis: readmit-source-access/v1", "Status: complete",
		"Listed: true", "Readable: 1", "Unreadable: 0", "Quota: within",
		"Destination: none; this source reaches no address", "Credential: none"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("diagnosis did not report %q:\n%s", expected, stdout)
		}
	}
	if _, err := os.Lstat(report); err != nil {
		t.Fatalf("the diagnosis did not retain its record: %v", err)
	}

	unsupported := filepath.Join(directory, "api.json")
	if err := os.WriteFile(unsupported, []byte(sourceAPIDocument), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(t, append([]string{"source", "diagnose", unsupported}, sourcePlanFlags()...)...)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("exit code = %d, want 2 for a source this release does not support", code)
	}
	if !strings.Contains(stdout, "Status: unsupported") || !strings.Contains(stdout, "Run state: execution_error") {
		t.Fatalf("the diagnosis did not report the unsupported source:\n%s", stdout)
	}
	if !strings.Contains(stderr, "source access is not available") {
		t.Fatalf("the diagnostic did not name the failure: %q", stderr)
	}
}

// Every refusal below stops before anything is staged, and no diagnostic
// repeats the declaration it refused.
func TestSourceRefusesEveryDeclarationItCannotRun(t *testing.T) {
	directory, declaration := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage})
	output := filepath.Join(directory, "collected")
	taken := filepath.Join(directory, "taken.json")
	if err := os.WriteFile(taken, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(directory, "broken.json")
	if err := os.WriteFile(broken, []byte(strings.Replace(sourceDirectoryDocument, `"kind": "directory"`, `"kind": "database"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(directory, "remote.json")
	if err := os.WriteFile(remote, []byte(strings.NewReplacer(
		`"kind": "directory"`, `"kind": "transfer"`,
		`"root": "ROOT",`, `"address": "10.9.9.9:2222", "classification": "nonproduction", "command": "/bin/echo", "arguments": [],`,
	).Replace(sourceDirectoryDocument)), 0600); err != nil {
		t.Fatal(err)
	}
	for name, arguments := range map[string][]string{
		"no declaration":     {"source", "collect", declaration, "--output", output, "--receipt", filepath.Join(directory, "r.json")},
		"no output":          append([]string{"source", "collect", declaration, "--receipt", filepath.Join(directory, "r.json")}, sourcePlanFlags()...),
		"a taken receipt":    append([]string{"source", "collect", declaration, "--output", output, "--receipt", taken}, sourcePlanFlags()...),
		"an unknown kind":    append([]string{"source", "collect", broken, "--output", output, "--receipt", filepath.Join(directory, "r.json")}, sourcePlanFlags()...),
		"no selected policy": append([]string{"source", "collect", remote, "--output", output, "--receipt", filepath.Join(directory, "r.json")}, sourcePlanFlags()...),
		"no subcommand":      {"source"},
	} {
		stdout, stderr, err := run(t, arguments...)
		if err == nil {
			t.Fatalf("%s: a declaration readmit must refuse was accepted:\n%s", name, stdout)
		}
		if strings.Contains(stderr, directory) {
			t.Fatalf("%s: the diagnostic echoed a path: %q", name, stderr)
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatalf("%s: a refused collection left a collection directory behind", name)
		}
	}
}
