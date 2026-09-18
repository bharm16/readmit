package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

func testSpecFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile("../testdata/fixtures/test-reschedule.json")
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(dir, "test-reschedule.json")
	if err := os.WriteFile(spec, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "../testdata/fixtures/listen-s13.hl7", "--output", filepath.Join(dir, "test-case"))
	if err != nil {
		t.Fatalf("capture: %v %s %s", err, stdout, stderr)
	}
	return dir, spec
}

func testTarget(t *testing.T, dir, address string) {
	t.Helper()
	raw, err := json.Marshal(replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test-target.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func testListener(t *testing.T, dir, mode string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cmd := exec.CommandContext(ctx, binary, "listen", "--address", "127.0.0.1:0", "--mode", mode, "--output", filepath.Join(dir, "received-case"), "--observation", filepath.Join(dir, "test-observation.json"), "--max-messages", "2", "--idle-timeout", "3s")
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	})
	reader := bufio.NewReader(pipe)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatal("test listener not ready")
	}
	testTarget(t, dir, strings.TrimSpace(strings.TrimPrefix(ready, "Listening: ")))
	return func() {
		t.Helper()
		_, _ = io.Copy(io.Discard, reader)
		err := cmd.Wait()
		waited = true
		if err != nil {
			t.Fatalf("test listener: %v %s", err, diagnostic.String())
		}
	}
}

func processCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return exit.ExitCode()
}

func TestTestExecutableUnchangedSpecFailsPassesAndCatchesReintroduction(t *testing.T) {
	fixture, err := os.ReadFile("../testdata/fixtures/test-reschedule.json")
	if err != nil {
		t.Fatal(err)
	}
	sourceDir, _ := testSpecFixture(t)
	var specIdentity, sourceIdentity string
	for _, mode := range []string{"defective", "fixed", "defective", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			spec := filepath.Join(dir, "test-reschedule.json")
			if err := os.WriteFile(spec, fixture, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.CopyFS(filepath.Join(dir, "test-case"), os.DirFS(filepath.Join(sourceDir, "test-case"))); err != nil {
				t.Fatal(err)
			}
			before, err := bundle.Open(filepath.Join(dir, "test-case"))
			if err != nil {
				t.Fatal(err)
			}
			wait := testListener(t, dir, mode)
			output := filepath.Join(dir, "result")
			stdout, stderr, err := run(t, "test", spec, "--send", "--output", output)
			want, status := 0, testrunner.Pass
			if mode == "defective" {
				want, status = 1, testrunner.AssertionFailure
			}
			if code := processCode(t, err); code != want {
				t.Fatalf("exit %d, want %d: %s %s", code, want, stdout, stderr)
			}
			wait()
			artifact, err := testrunner.Open(output)
			if err != nil {
				t.Fatal(err)
			}
			result := artifact.Result
			if result.Status != status || len(result.Assertions) != 4 || result.ObservationBoundary != "appointment-ledger" || string(result.ReceiverMode) != mode || result.ReceiverSessionID == "" || result.TargetIdentity == "" || artifact.Run == nil {
				t.Fatalf("wrong test result: %+v", result)
			}
			if specIdentity == "" {
				specIdentity = result.SpecIdentity
				sourceIdentity = result.InputBundleIdentity
			}
			if result.SpecIdentity != specIdentity || result.InputBundleIdentity != sourceIdentity {
				t.Fatal("spec or source identity changed to obtain a verdict")
			}
			if *result.Assertions[0].Assertion.Expected.Count != 1 || result.Assertions[2].Status != "passed" || result.Assertions[3].Status != "passed" {
				t.Fatal("expectations changed or ACKs masked the ledger defect")
			}
			if result.Assertions[0].Status == "failed" && *result.Assertions[0].Observed.Count != 2 {
				t.Fatal("failure did not expose the duplicate appointment")
			}
			if artifact.FinalObservation.Processed[1].OccurrenceID != "s0001-e000003" || artifact.Run.Events[1].SourceOccurrence != "s0002-e000001" {
				t.Fatal("receiver and sender occurrence identities were conflated")
			}
			after, _ := bundle.Open(filepath.Join(dir, "test-case"))
			specAfter, _ := os.ReadFile(spec)
			if after == nil || before.Identity != after.Identity || !bytes.Equal(fixture, specAfter) {
				t.Fatal("source case or committed spec changed")
			}
			if !strings.HasPrefix(lastLine(stdout), "Rerun: ") || stderr != "" {
				t.Fatalf("missing final rerun instruction: %s %s", stdout, stderr)
			}
			for _, private := range []string{dir, "SYNTH-001", "APPT-001", "FILL-001", "LISTEN-BOOK", "20260103110000", "Rescheduling updates"} {
				if strings.Contains(stdout+stderr, private) {
					t.Fatal("test console disclosed private paths or values")
				}
			}
		})
	}
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

func TestTestExecutableMissingObservationAndInvalidConfigHaveExitTwo(t *testing.T) {
	for _, kind := range []string{"missing-observation", "invalid-spec", "invalid-target"} {
		t.Run(kind, func(t *testing.T) {
			dir, spec := testSpecFixture(t)
			testTarget(t, dir, "127.0.0.1:2575")
			if kind == "invalid-spec" {
				if err := os.WriteFile(spec, []byte(`{"schema":"readmit-test/v1","SECRET-unknown":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "invalid-target" {
				if err := os.WriteFile(filepath.Join(dir, "test-target.json"), []byte(`{"SECRET-unknown":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(dir, "result")
			stdout, stderr, err := run(t, "test", spec, "--send", "--output", output)
			if processCode(t, err) != 2 {
				t.Fatalf("expected execution error: %v %s %s", err, stdout, stderr)
			}
			artifact, err := testrunner.Open(output)
			if err != nil || artifact.Result.Status != testrunner.ExecutionError || artifact.Result.Run != nil {
				t.Fatalf("missing machine execution error: %v", err)
			}
			if !strings.HasPrefix(lastLine(stdout), "Rerun: ") || strings.Contains(stdout+stderr, "SECRET") || strings.Contains(stdout+stderr, dir) {
				t.Fatal("unsafe execution-error console")
			}
		})
	}
}

func TestTestExecutableCobraErrorsRemainExecutionErrors(t *testing.T) {
	for _, args := range [][]string{{"test"}, {"test", "SECRET", "extra"}, {"test", "SECRET", "--SECRET-flag"}, {"test", "SECRET", "--send=SECRET"}, {"test", "SECRET", "--send"}} {
		stdout, stderr, err := run(t, args...)
		if processCode(t, err) != 2 || stderr == "" || strings.Contains(stdout+stderr, "SECRET") || !strings.HasPrefix(lastLine(stdout), "Rerun: ") {
			t.Fatalf("wrong test argument outcome: %v %s %s", err, stdout, stderr)
		}
	}
}

func TestTestExecutablePreviewDoesNotConnectOrClaimVerdict(t *testing.T) {
	dir, spec := testSpecFixture(t)
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	testTarget(t, dir, listener.Addr().String())
	stdout, stderr, err := run(t, "test", spec)
	if err != nil || stderr != "" || !strings.Contains(stdout, "no verdict or result artifact produced") || !strings.HasPrefix(lastLine(stdout), "Rerun: ") {
		t.Fatalf("preview: %v %s %s", err, stdout, stderr)
	}
	_ = listener.SetDeadline(time.Now().Add(50 * time.Millisecond))
	if connection, err := listener.Accept(); err == nil {
		connection.Close()
		t.Fatal("preview connected")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatal("preview wrote an artifact")
	}
}

func TestTestExecutableACKOnlySuccessNamesTheACKBoundary(t *testing.T) {
	dir, path := testSpecFixture(t)
	spec, err := testrunner.ReadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Messages = []string{"s0001-e000001"}
	spec.Observation = testrunner.Observation{Boundary: testrunner.ACKBoundary}
	spec.Setup.InitialState = "operator-declared"
	want := "AR"
	spec.Assertions = []testrunner.Assertion{{ID: "expected-rejection", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &want}}}}
	raw, err := json.Marshal(spec, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	testTarget(t, dir, listener.Addr().String())
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err != nil {
			done <- err
			return
		}
		_, err = conn.Write(mllp.Frame([]byte("MSH|^~\\&|FIXTURE|TEST|||20260101120000||ACK^S12|ACK|P|2.5.1\rMSA|AR|LISTEN-BOOK\r")))
		done <- err
	}()
	stdout, stderr, err := run(t, "test", path, "--send", "--output", filepath.Join(dir, "result"))
	if processCode(t, err) != 0 || stderr != "" || !strings.Contains(stdout, "ACK contract passed") || strings.Contains(strings.ToLower(stdout), "appointment") {
		t.Fatalf("wrong ACK-only claim: %v %s %s", err, stdout, stderr)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	decision := readDecision(t, filepath.Join(dir, "result.decision.json"))
	if !decision.Allowed || !decision.ExplicitSend || decision.PolicySelected {
		t.Fatalf("missing allowed loopback decision: %+v", decision)
	}
}
