package desktop_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/mllp"
	"io"
	"net"
	"time"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

const canonicalSpec = `{"schema":"readmit-test/v1","name":"Full regression","input":{"case":"incident","messages":["s0001-e000001"]},"target":"test-target.json","setup":{"initial_state":"empty-ledger","reset_instructions":"Restart fixture"},"observation":{"boundary":"appointment-ledger","path":"ledger.json"},"assertions":[{"id":"exact","operator":"ledger_equals","expected":{"records":[]}},{"id":"count","operator":"ledger_count","expected":{"count":0}},{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`

func TestCanonicalImportEditExportPreservesEveryClause(t *testing.T) {
	app, root, identity := authoringWorkspace(t)
	original := " \r\n" + canonicalSpec + "\r\n"
	if err := os.WriteFile(filepath.Join(root, "original.json"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	imported := app.ImportTest(root, "original.json")
	if imported.State != desktop.Completed || imported.Document != original {
		t.Fatalf("import: %+v", imported)
	}
	unedited := app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: imported.Document, Output: "unedited.json"})
	if unedited.State != desktop.Completed || unedited.Identity != fmt.Sprintf("%x", sha256.Sum256([]byte(original))) {
		t.Fatalf("unedited identity: %+v", unedited)
	}
	unchangedRoundTrip, err := os.ReadFile(filepath.Join(root, "unedited.json"))
	if err != nil || string(unchangedRoundTrip) != original {
		t.Fatalf("unedited round trip changed bytes: %v", err)
	}
	edited := strings.Replace(imported.Document, "Full regression", "Reviewed regression", 1)
	saved := app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: edited, Output: "copy.json"})
	if saved.State != desktop.Completed {
		t.Fatalf("export: %+v", saved)
	}
	data, err := os.ReadFile(filepath.Join(root, "copy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != edited {
		t.Fatal("export changed document bytes")
	}
	spec, err := testrunner.ReadSpec(filepath.Join(root, "copy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "Reviewed regression" || len(spec.Assertions) != 3 || spec.Assertions[0].Expected.Records == nil || len(*spec.Assertions[0].Expected.Records) != 0 {
		t.Fatalf("lost clauses: %+v", spec)
	}
	if _, err := testrunner.Prepare(filepath.Join(root, "copy.json")); err != nil {
		t.Fatalf("headless preparation: %v", err)
	}
	unchanged, _ := os.ReadFile(filepath.Join(root, "original.json"))
	if string(unchanged) != original {
		t.Fatal("overwrote imported spec")
	}
	if opened := app.OpenCase(root, "incident"); opened.Case.Identity != identity {
		t.Fatal("changed evidence")
	}
}

func TestCanonicalRefusalsAreLosslessAndRecoverable(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	for name, document := range map[string]string{
		"schema":    strings.Replace(canonicalSpec, "readmit-test/v1", "readmit-test/v9000", 1),
		"operator":  strings.Replace(canonicalSpec, "ledger_equals", "ledger_equals/v2", 1),
		"unknown":   strings.Replace(canonicalSpec, `"records":[]`, `"records":[],"ignored":"PRIVATE"`, 1),
		"duplicate": strings.Replace(canonicalSpec, `"count":0`, `"count":0,"count":1`, 1),
		"null":      strings.Replace(canonicalSpec, `"records":[]`, `"records":null`, 1),
		"oversized": canonicalSpec + strings.Repeat(" ", testrunner.MaxSpecBytes),
	} {
		t.Run(name, func(t *testing.T) {
			result := app.ValidateTest(document)
			if result.State != desktop.Failed || result.Document != "" || strings.Contains(result.Reason, "PRIVATE") {
				t.Fatalf("validation: %+v", result)
			}
			output := name + ".json"
			if got := app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: document, Output: output}); got.State != desktop.Failed {
				t.Fatalf("export: %+v", got)
			}
			if _, err := os.Stat(filepath.Join(root, output)); !os.IsNotExist(err) {
				t.Fatal("refusal wrote output")
			}
			if err := os.WriteFile(filepath.Join(root, "invalid.json"), []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			if got := app.ImportTest(root, "invalid.json"); got.State != desktop.Failed || got.Document != "" {
				t.Fatalf("import: %+v", got)
			}
		})
	}
	for _, output := range []string{"../escape.json", "incident/extra.json", ".", ""} {
		if got := app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: canonicalSpec, Output: output}); got.State != desktop.Failed {
			t.Fatalf("unsafe export: %+v", got)
		}
	}
	request := desktop.CanonicalTestRequest{Workspace: root, Document: canonicalSpec, Output: "recovered.json"}
	if got := app.ExportTest(request); got.State != desktop.Completed {
		t.Fatalf("recovery: %+v", got)
	}
	if got := app.ExportTest(request); got.State != desktop.Failed {
		t.Fatalf("overwrite: %+v", got)
	}
	for _, entry := range []string{"../recovered.json", "incident", "missing.json"} {
		if got := app.ImportTest(root, entry); got.State != desktop.Failed {
			t.Fatalf("unsafe import: %+v", got)
		}
	}
	if err := os.Symlink(filepath.Join(root, "recovered.json"), filepath.Join(root, "alias.json")); err == nil {
		if got := app.ImportTest(root, "alias.json"); got.State != desktop.Failed {
			t.Fatalf("followed symlink: %+v", got)
		}
	}
	if got := app.ExportTest(desktop.CanonicalTestRequest{Workspace: filepath.Join(root, "incident"), Document: canonicalSpec, Output: "extra.json"}); got.State != desktop.Failed {
		t.Fatalf("wrote inside evidence: %+v", got)
	}
}

func TestCanonicalOperationsRespectBusySlotAndReleaseIt(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	chooser := &chooser{folder: root}
	app := activatedApp(t, chooser, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))
	chooser.before = func() {
		for _, got := range []desktop.CanonicalTestResult{app.ImportTest(root, "spec.json"), app.ValidateTest(canonicalSpec), app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: canonicalSpec, Output: "spec.json"})} {
			if got.State != desktop.Busy {
				t.Errorf("busy operation: %+v", got)
			}
		}
	}
	app.SelectWorkspace()
	if got := app.ValidateTest(canonicalSpec); got.State != desktop.Completed {
		t.Fatalf("slot did not recover: %+v", got)
	}
}

func TestCanonicalExportExecutesWithDesktopCLIParity(t *testing.T) {
	for _, expected := range []string{"AA", "AE"} {
		t.Run(expected, func(t *testing.T) {
			app, root, _ := authoringWorkspace(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			served := make(chan error, 1)
			go func() {
				for range 2 {
					conn, err := listener.Accept()
					if err != nil {
						served <- err
						return
					}
					conn.SetDeadline(time.Now().Add(10 * time.Second))
					reader, _ := mllp.NewReader(conn, 1<<20)
					_, err = reader.ReadFrame()
					if err == nil {
						_, err = io.WriteString(conn, "\x0b"+repAccepted+"\x1c\r")
					}
					conn.Close()
					if err != nil {
						served <- err
						return
					}
				}
				served <- nil
			}()
			target := strings.Replace(authoringTarget, "127.0.0.1:2575", listener.Addr().String(), 1)
			if err := os.WriteFile(filepath.Join(root, "test-target.json"), []byte(target), 0600); err != nil {
				t.Fatal(err)
			}
			// The same edited expected value must decide both execution surfaces.
			original := `{"schema":"readmit-test/v1","name":"ACK regression","input":{"case":"incident","messages":["s0001-e000001"]},"target":"test-target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Restart fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
			if err := os.WriteFile(filepath.Join(root, "original.json"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			imported := app.ImportTest(root, "original.json")
			if imported.State != desktop.Completed {
				t.Fatal(imported)
			}
			edited := strings.Replace(imported.Document, `"text":"AA"`, `"text":"`+expected+`"`, 1)
			if got := app.ExportTest(desktop.CanonicalTestRequest{Workspace: root, Document: edited, Output: "edited.json"}); got.State != desktop.Completed {
				t.Fatal(got)
			}
			spec := filepath.Join(root, "edited.json")
			identity := preflighted(t, app, desktop.RunPreflightRequest{Workspace: root, Spec: "edited.json"})
			desktopResult := app.StartDurableRun(desktop.DurableRunRequest{Workspace: root, Spec: "edited.json", Output: "desktop-run", Expected: identity})
			if desktopResult.State != desktop.Completed || desktopResult.Run == nil {
				t.Fatalf("desktop: %+v", desktopResult)
			}
			var stdout, stderr bytes.Buffer
			cliErr := cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "test", spec, "--send", "--output", filepath.Join(root, "cli-run")}, &stdout, &stderr)
			want := 0
			if expected == "AE" {
				want = 1
			}
			if cli.ExitCode(cliErr) != want || desktopResult.Run.ExitCode() != want {
				t.Fatalf("verdicts desktop=%+v CLI=%v", desktopResult, cliErr)
			}
			retained, err := testrunner.Open(filepath.Join(root, "cli-run"))
			if err != nil {
				t.Fatal(err)
			}
			if len(retained.Result.Assertions) != 1 || retained.Result.Assertions[0].Assertion.ID != "ack" {
				t.Fatalf("lost assertion: %+v", retained.Result)
			}
			if err := <-served; err != nil {
				t.Fatal(err)
			}
		})
	}
}
