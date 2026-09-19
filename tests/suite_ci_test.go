package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestCustomerCIExamplesExecuteSavedSuiteAndPropagateFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("documented POSIX customer-agent examples")
	}
	doc, e := os.ReadFile("../docs/customer-ci.md")
	if e != nil {
		t.Fatal(e)
	}
	var commands []string
	for _, line := range strings.Split(string(doc), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"$READMIT_BIN" suite ci `) {
			commands = append(commands, line)
		}
	}
	if len(commands) != 3 {
		t.Fatalf("expected generic/GitHub/Azure commands, got %d", len(commands))
	}
	for example, command := range commands {
		for _, ackCode := range []string{"AA", "AE"} {
			t.Run(fmt.Sprintf("example-%d-%s", example, ackCode), func(t *testing.T) {
				listener, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer listener.Close()
				done := make(chan struct{})
				go func() {
					defer close(done)
					c, e := listener.Accept()
					if e != nil {
						return
					}
					defer c.Close()
					c.SetDeadline(time.Now().Add(10 * time.Second))
					r, _ := mllp.NewReader(c, 1<<20)
					if _, e = r.ReadFrame(); e == nil {
						fmt.Fprintf(c, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|%s|LISTEN-BOOK\r\x1c\r", ackCode)
					}
				}()
				t.Cleanup(func() { listener.Close(); <-done })
				root := t.TempDir()
				root, _ = filepath.EvalSymlinks(root)
				put := func(name string, v any) {
					t.Helper()
					b, e := json.Marshal(v)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
						t.Fatal(e)
					}
				}
				raw, e := os.ReadFile("../testdata/fixtures/listen-s12.hl7")
				if e != nil {
					t.Fatal(e)
				}
				_, e = bundle.Write(filepath.Join(root, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
				if e != nil {
					t.Fatal(e)
				}
				put("target.json", replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: listener.Addr().String(), Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096})
				value := "AA"
				put("test.json", testrunner.Spec{Schema: testrunner.SpecSchema, Name: "PRIVATE-PATIENT", Input: testrunner.Input{Case: "unbound", Messages: []string{"s0001-e000001"}}, Target: "unbound", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "operator resets fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}}})
				put("suite.json", suite.Document{Schema: suite.Schema, ID: "fixture", Owner: "PRIVATE-OWNER", Tags: []string{}, Parallelism: 1, Environments: []suite.Environment{{ID: "lab", Site: "PRIVATE-SITE", Bindings: []suite.Binding{{Parameter: "target", Target: "target.json"}}}}, Tables: []suite.Table{{ID: "rows", Rows: []suite.Row{{ID: "one", Case: "case"}}}}, Tests: []suite.Test{{ID: "booking", Spec: "test.json", Owner: "PRIVATE-OWNER", Tags: []string{}, Parameter: "target", Table: "rows", Isolation: "shared", Sequence: []string{"s0001-e000001"}}}})
				preview, e := suite.Prepare(filepath.Join(root, "suite.json"), "lab", filepath.Join(root, "preview"))
				if e != nil {
					t.Fatal(e)
				}
				hashFile := func(name string) string {
					b, e := os.ReadFile(name)
					if e != nil {
						t.Fatal(e)
					}
					sum := sha256.Sum256(b)
					return hex.EncodeToString(sum[:])
				}
				put("coverage.json", suite.CoverageDocument{Schema: suite.CoverageSchema, SuiteSHA256: hashFile(filepath.Join(root, "suite.json")), Specifications: []suite.CoverageSpecification{{Job: "booking-one", SHA256: hashFile(filepath.Join(preview.Directory, "booking-one.json"))}}, Requirements: []suite.Requirement{{ID: "booking", Jobs: []string{"booking-one"}}}, Exclusions: []suite.Exclusion{}})
				cmd := exec.Command("sh", "-c", command)
				cmd.Env = append(os.Environ(), "READMIT_BIN="+binary, "SUITE_FILE="+filepath.Join(root, "suite.json"), "SUITE_ENVIRONMENT=lab", "RUN_DIRECTORY="+filepath.Join(root, "run"), "COVERAGE_FILE="+filepath.Join(root, "coverage.json"))
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				want := 0
				if ackCode == "AE" {
					want = 2
				}
				if cmd.ProcessState.ExitCode() != want {
					t.Fatalf("exit %d want %d: %v %s %s", cmd.ProcessState.ExitCode(), want, err, stdout.String(), stderr.String())
				}
				if strings.Contains(stdout.String()+stderr.String(), "PRIVATE") || strings.Contains(stdout.String()+stderr.String(), root) {
					t.Fatal("example disclosed customer data")
				}
				r, e := suite.DecodeCI(stdout.Bytes())
				if e != nil || r.Executed != 1 || r.ExitCode != want {
					t.Fatalf("%+v %v", r, e)
				}
			})
		}
	}
}
