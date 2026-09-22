package desktop_test

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// mllpPeer answers every framed message with one ACK carrying code, exactly
// as the suite fixtures do.
func mllpPeer(t *testing.T, code string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					conn.Close()
					return
				}
				_, _ = conn.Write([]byte("\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|" + code + "|LISTEN-BOOK\r\x1c\r"))
				conn.Close()
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return listener.Addr().String()
}

// suiteFixtureDir authors one saved suite beside its template, target and
// case, exactly as the suite preparation fixtures do.
func suiteFixtureDir(t *testing.T, address string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Write(filepath.Join(dir, "case-one"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}
	value := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "booking", Input: testrunner.Input{Case: "unbound", Messages: []string{"s0001-e000001"}}, Target: "unbound", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture deliberately"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}}}
	for name, document := range map[string]any{"east.json": target, "booking.json": spec} {
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// cliExecutable builds the exact command-line executable this module ships.
func cliExecutable(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "readmit")
	build := exec.Command("go", "build", "-o", bin, "./cmd/readmit")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return bin
}

// TestGUIPreparedSuiteAndCIHandoffExecuteThroughTheUnchangedCLI proves the
// differential the ticket promises: a suite the window authored and a CI
// handoff the window generated execute through the unchanged command-line
// contracts, and reach the same verdict an equivalent direct invocation
// reaches. The GUI path adds no second execution path; it only prepares
// reviewed inputs and the exact documented command around them.
func TestGUIPreparedSuiteAndCIHandoffExecuteThroughTheUnchangedCLI(t *testing.T) {
	for _, code := range []string{"AA", "AE"} {
		t.Run(code, func(t *testing.T) {
			// AA passes and passes its coverage requirement. AE fails the
			// assertion, and because the invocation requests a coverage gate,
			// a failed run is an ineligible job: the backend fails the whole
			// gate with exit 2. The equivalence and sanitization assertions
			// below are the differential's point, whichever verdict applies.
			want := 0
			wantState := "passed"
			if code == "AE" {
				want = 2
				wantState = "error"
			}
			dir := suiteFixtureDir(t, mllpPeer(t, code))
			app := workspaceApp(t)
			if opened := app.OpenWorkspace(dir); opened.State != desktop.Completed {
				t.Fatalf("open: %+v", opened)
			}
			// The window authors the suite through its structured editor seam
			// and saves one canonical entry into the workspace.
			canonical := `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":2,"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"east.json"}]}],"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface","table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`
			saved := app.SaveSuite(desktop.RuleDocumentSaveRequest{Workspace: dir, Document: canonical, Output: "suite.json"})
			if saved.State != desktop.Completed {
				t.Fatalf("save suite: %+v", saved)
			}
			// The window generates the CI handoff for the documented POSIX
			// integration: the exact command, six provisioned variables.
			operationPolicy := testlicense.New(t)
			handoff := app.SaveCIHandoff(desktop.CIHandoffRequest{
				Integration: "posix", Binary: "/opt/readmit/readmit",
				OperationPolicy: operationPolicy, SuiteFile: filepath.Join(dir, "suite.json"),
				Environment: "east", RunDirectory: "/var/lib/readmit-ci/run", CoverageFile: "/srv/readmit/coverage.json",
				Output: filepath.Join(t.TempDir(), "handoff.sh"),
			})
			if handoff.State != desktop.Completed {
				t.Fatalf("handoff: %+v", handoff)
			}
			// The handoff must be coverable by a coverage declaration; the
			// window authors one through the retained prepared inputs (#258),
			// and the differential covers the gate the CI command enforces.
			prepared := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: dir, Entry: "suite.json", Environment: "east", Output: "prepared"})
			if prepared.State != desktop.Completed {
				t.Fatalf("prepare: %+v", prepared)
			}
			bin := cliExecutable(t)
			coverage := filepath.Join(dir, "coverage.json")
			// The coverage declaration names one requirement over the whole
			// expanded suite, authored from the retained prepared directory.
			if declared := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{
				Workspace:    dir,
				Prepared:     "prepared",
				Requirements: []suite.Requirement{{ID: "regression", Jobs: []string{"booking-one"}}},
				Output:       "coverage.json",
			}); declared.State != desktop.Completed {
				t.Fatalf("coverage: %+v", declared)
			}
			if _, err := os.Stat(coverage); err != nil {
				t.Fatal(err)
			}

			runScript := func(runDir string) int {
				t.Helper()
				cmd := exec.Command("sh", handoff.Output)
				cmd.Env = append(os.Environ(),
					"OPERATION_POLICY="+operationPolicy,
					"READMIT_BIN="+bin,
					"SUITE_FILE="+filepath.Join(dir, "suite.json"),
					"SUITE_ENVIRONMENT=east",
					"RUN_DIRECTORY="+runDir,
					"COVERAGE_FILE="+coverage,
				)
				err := cmd.Run()
				exit := 0
				if err != nil {
					exit = cmd.ProcessState.ExitCode()
				}
				return exit
			}
			runDirect := func(runDir string) int {
				t.Helper()
				cmd := exec.Command(bin, "--operation-policy", operationPolicy, "suite", "ci",
					filepath.Join(dir, "suite.json"), "--environment", "east",
					"--output", runDir, "--requirements", coverage, "--send", "--deadline", "5m")
				err := cmd.Run()
				exit := 0
				if err != nil {
					exit = cmd.ProcessState.ExitCode()
				}
				return exit
			}
			guiRun, directRun := filepath.Join(dir, "gui-run"), filepath.Join(dir, "direct-run")
			if got := runScript(guiRun); got != want {
				if raw, e := os.ReadFile(filepath.Join(guiRun, "ci.json")); e == nil {
					t.Fatalf("GUI-prepared handoff exited %d, want %d: %s", got, want, raw)
				}
				t.Fatalf("GUI-prepared handoff exited %d, want %d (no ci.json)", got, want)
			}
			if got := runDirect(directRun); got != want {
				t.Fatalf("direct invocation exited %d, want %d", got, want)
			}
			// Equivalent verdicts, and the CI-visible summaries disclose none
			// of the fixture's local values on either path.
			guiCI, err := os.ReadFile(filepath.Join(guiRun, "ci.json"))
			if err != nil {
				t.Fatal(err)
			}
			directCI, err := os.ReadFile(filepath.Join(directRun, "ci.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(guiCI), `"state":"`+wantState+`"`) || string(guiCI) != string(directCI) {
				t.Fatalf("verdicts diverge: gui=%s direct=%s", guiCI, directCI)
			}
			for _, run := range []string{guiRun, directRun} {
				for _, name := range []string{"ci.json", "junit.xml"} {
					raw, err := os.ReadFile(filepath.Join(run, name))
					if err != nil {
						t.Fatal(err)
					}
					for _, disclosed := range []string{"interop", "hospital-a", "scheduling", dir, "LISTEN-BOOK"} {
						if strings.Contains(string(raw), disclosed) {
							t.Fatalf("%s disclosed %q: %s", name, disclosed, raw)
						}
					}
				}
			}
			// The retained aggregate reads back through the panel's own
			// inspection operation.
			if inspected := app.InspectCIResults(guiRun); inspected.State != desktop.Completed || inspected.CI == nil || inspected.CI.State != wantState {
				t.Fatalf("inspect: %+v", inspected)
			}
		})
	}
}
