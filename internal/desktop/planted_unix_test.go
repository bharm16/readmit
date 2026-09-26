//go:build !windows

package desktop_test

// Planted patient values and a planted credential stay where they belong. The
// case holds a patient name and identifier nobody typed into the window, and a
// credential is resolvable only through the program its reference names. The
// window opens, verifies and inspects that evidence, records where the person
// was and what they were writing, registers, tests, rotates and scans the
// credential reference, and prepares a runner configuration naming it. The
// credential value then appears in no result, not even an evidence view, and
// in no file the window wrote; the patient values appear in no shell state
// document, in no result that is not a view of the evidence itself, and in no
// refusal or diagnostic reason.

import (
	"encoding/base64"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/testlicense"
)

const (
	plantedPatient    = "CEDARPLANT^ONEELEVEN"
	plantedIdentifier = "MRN-PLANTED-111"
	plantedCredential = "PLANTED-CREDENTIAL-VALUE-111"
)

func TestPlantedPatientValuesAndCredentialsStayOutOfResultsAndShellState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "readmit")
	configuration := t.TempDir()
	credential := filepath.Join(configuration, "credential")
	if err := os.WriteFile(credential, []byte(plantedCredential), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	message := strings.NewReplacer("EXAMPLE^PATIENT", plantedPatient, "SYNTH-001", plantedIdentifier).Replace(string(listenFrame(t)))
	if !strings.Contains(message, plantedPatient) || !strings.Contains(message, plantedIdentifier) {
		t.Fatal("the fixture no longer carries the fields the planted values replace")
	}
	writeCase(t, workspace, "case", framed(message))
	app := desktop.NewWithOperationSelection(&chooser{}, desktop.ShellDocuments{Folder: state})
	if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}

	// Results that are not a view of the evidence, and views that are.
	var results, views []any
	keep := func(result any) any { results = append(results, result); return result }
	opened := app.OpenCase(workspace, "case")
	keep(opened)
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	keep(app.OpenWorkspace(workspace))
	keep(app.BuildIndex(desktop.BuildIndexRequest{Workspace: workspace, Case: "case", Identity: opened.Case.Identity, Output: "case.index.json",
		Fields: []string{"PID-3"}, Retention: "digests", RetainUntil: "indefinite"}))
	grid := app.OpenGrid(workspace, "case", "case.index.json", 0, 10)
	views = append(views, grid)
	if grid.State == desktop.Completed && grid.Grid != nil && len(grid.Grid.Rows) > 0 {
		inspected := app.InspectOccurrence(desktop.InspectRequest{Workspace: workspace, Case: "case", Identity: opened.Case.Identity, Occurrence: grid.Grid.Rows[0].ID, Path: "PID[1]-5", ByteOffset: -1})
		views = append(views, inspected)
		if encoded, _ := json.Marshal(inspected); !strings.Contains(string(encoded), "CEDARPLANT") {
			t.Fatalf("the evidence view does not show the evidence, so the absences below prove nothing: %s", encoded)
		}
	} else {
		t.Fatalf("grid: %+v", grid)
	}
	keep(app.RecordView(desktop.View{Workspace: resolved(t, workspace), Region: "evidence", Case: "case"}))
	draft := editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)
	draft.Workspace = resolved(t, workspace)
	keep(app.SaveEditorDraft(draft))
	keep(app.EditorDrafts())
	keep(app.Guide(workspace))
	keep(app.DisclosureStatus())
	reference := secret.Reference{Name: "lab-credential", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint,
		Address: "127.0.0.1:2575", Command: "/bin/cat", Arguments: []string{credential}, MaxAge: "720h"}
	for _, result := range []any{
		keep(app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: workspace, SecretsFile: "secrets.json", Reference: reference})),
		keep(app.TestSecretReference(workspace, "secrets.json", "lab-credential")),
		keep(app.RotateSecretReference(workspace, "secrets.json", "lab-credential")),
		keep(app.ReadSecrets(workspace, "secrets.json")),
		keep(app.ScanSecrets(desktop.SecretScanRequest{Workspace: workspace, SecretsFile: "secrets.json", Paths: []string{"case", "secrets.json"}})),
	} {
		if state, reason := stateOf(result); state != desktop.Completed {
			t.Fatalf("credential reference journey: %s %s", state, reason)
		}
	}
	runner := runnerConfigInput(t, filepath.Join(configuration, "runs"))
	runner.Key = desktop.RunnerReferenceInput{Command: "/bin/cat", Arguments: []string{credential}}
	runner.Token = desktop.RunnerReferenceInput{Command: "/bin/cat", Arguments: []string{credential}}
	runner.Output = filepath.Join(configuration, "runner.json")
	if result := keep(app.SaveRunnerConfig(runner)); func() bool { s, _ := stateOf(result); return s != desktop.Completed }() {
		t.Fatalf("runner configuration: %+v", result)
	}
	keep(app.ReadRunnerConfig(runner.Output))

	credentialForms := []string{plantedCredential, base64.StdEncoding.EncodeToString([]byte(plantedCredential))}
	patientForms := []string{"CEDARPLANT", "ONEELEVEN", plantedIdentifier}
	for _, result := range append(append([]any{}, results...), views...) {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		for _, form := range credentialForms {
			if strings.Contains(string(encoded), form) {
				t.Errorf("a result carried the credential value: %s", encoded)
			}
		}
		if _, reason := stateOf(result); containsAny(reason, patientForms) {
			t.Errorf("a reason repeated a patient value: %q", reason)
		}
	}
	for _, result := range results {
		if encoded, _ := json.Marshal(result); containsAny(string(encoded), patientForms) {
			t.Errorf("a result that is not an evidence view carried a patient value: %s", encoded)
		}
	}
	for _, folder := range []string{workspace, state, configuration} {
		err := filepath.WalkDir(folder, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path == credential {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if containsAny(string(data), credentialForms) {
				t.Errorf("%s holds the credential value", path)
			}
			if folder == state && containsAny(string(data), patientForms) {
				t.Errorf("the shell state document %s holds a patient value no one typed", filepath.Base(path))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func containsAny(text string, values []string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
