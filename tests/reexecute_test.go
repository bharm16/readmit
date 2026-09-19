package tests

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestRedactReexecuteCLIRequiresAuthorizationAndPreviewsWithoutSending(t *testing.T) {
	request := redactFixture(t)
	review, err := redact.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	originalRun := filepath.Join(request.LocalState, "original-proof", "baseline")
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Assemble(context.Background(), report.RetainedInput{Case: request.CasePath, Current: filepath.Join(originalRun, "result"), Spec: filepath.Join(originalRun, "result", "spec.json")}, packet); err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.ReadSpec(filepath.Join(request.Output, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Case = filepath.Join(request.Output, "case")
	spec.Target = filepath.Join(originalRun, "target.json")
	spec.Observation.Path = filepath.Join(t.TempDir(), "unavailable-observation.json")
	path := filepath.Join(t.TempDir(), "rebound.json")
	redactJSON(t, path, spec)
	args := []string{"redact", "reexecute", request.Output, "--local-state", request.LocalState, "--approve", review.Identity, "--original-packet", packet, "--spec", path, "--phase", "failure"}
	stdout, stderr, err := run(t, args...)
	if err != nil {
		t.Fatalf("preview: %v %s", err, stderr)
	}
	var result redact.ReexecutionAssessment
	if err := json.Unmarshal([]byte(stdout), &result, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if result.Criteria != "not-executed" || result.ExternalEquivalence != "declined" || result.ReviewIdentity != review.Identity {
		t.Fatalf("false preview: %+v", result)
	}
	output := filepath.Join(t.TempDir(), "job")
	for _, suffix := range [][]string{{"--send"}, {"--output", output}, {"--phase", "unknown"}, {"--approve", "stale"}} {
		_, _, err := run(t, append(append([]string{}, args...), suffix...)...)
		if err == nil {
			t.Fatal("invalid authorization accepted")
		}
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("preview wrote job")
	}
}

func TestRedactReexecuteCLISendsAndRecoversActualOutcomes(t *testing.T) {
	for _, outcome := range []string{"matched", "changed", "unavailable-or-unstable"} {
		t.Run(outcome, func(t *testing.T) {
			request := redactFixture(t)
			review, err := redact.Create(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			originalRun := filepath.Join(request.LocalState, "original-proof", "baseline")
			original, err := testrunner.Open(filepath.Join(originalRun, "result"))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			packet := filepath.Join(root, "packet")
			if _, err := report.Assemble(context.Background(), report.RetainedInput{Case: request.CasePath, Current: filepath.Join(originalRun, "result"), Spec: filepath.Join(originalRun, "result", "spec.json")}, packet); err != nil {
				t.Fatal(err)
			}
			observationPath := filepath.Join(root, "observation.json")
			if outcome == "unavailable-or-unstable" {
				raw, err := os.ReadFile(filepath.Join(originalRun, "result", "initial-observation.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(observationPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				listener, err := net.Listen("tcp4", original.Result.Target.Address)
				if err != nil {
					t.Fatal(err)
				}
				mode := observation.Defective
				if outcome == "changed" {
					mode = observation.Fixed
				}
				server, err := receiver.New(receiver.Config{Mode: mode, OutputPath: filepath.Join(root, "receiver"), ObservationPath: observationPath, MaxFrameBytes: 1 << 20, IdleTimeout: 5 * time.Second, MaxMessages: 2})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { _, err := server.Serve(ctx, listener); done <- err }()
				defer func() { cancel(); listener.Close(); <-done }()
			}
			spec, err := testrunner.ReadSpec(filepath.Join(request.Output, "spec.json"))
			if err != nil {
				t.Fatal(err)
			}
			spec.Input.Case = filepath.Join(request.Output, "case")
			spec.Target = filepath.Join(originalRun, "target.json")
			spec.Observation.Path = observationPath
			path := filepath.Join(root, "rebound.json")
			redactJSON(t, path, spec)
			output := filepath.Join(root, "job")
			stdout, stderr, err := run(t, "redact", "reexecute", request.Output, "--local-state", request.LocalState, "--approve", review.Identity, "--original-packet", packet, "--spec", path, "--phase", "failure", "--send", "--output", output)
			if (err == nil) != (outcome == "matched") {
				t.Fatalf("unexpected exit: %v %s", err, stderr)
			}
			var assessment redact.ReexecutionAssessment
			if err := json.Unmarshal([]byte(stdout), &assessment, json.RejectUnknownMembers(true)); err != nil {
				t.Fatalf("assessment: %v %s", err, stdout)
			}
			if assessment.Criteria != outcome || assessment.ExternalEquivalence != "declined" {
				t.Fatalf("wrong claim: %+v", assessment)
			}
			before, err := os.ReadFile(filepath.Join(output, "journal.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err = run(t, "run", "status", output, "--recovery", "--json")
			wantCode := 1
			if outcome == "changed" {
				wantCode = 0
			}
			if outcome == "unavailable-or-unstable" {
				wantCode = 2
			}
			code := 0
			if err != nil {
				failed, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = failed.ExitCode()
			}
			if code != wantCode {
				t.Fatalf("recovery exit %d want %d: %s", code, wantCode, stderr)
			}
			var recovery durablerun.Recovery
			if err := json.Unmarshal([]byte(stdout), &recovery, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if !recovery.Terminal || recovery.Run.ResultIdentity != assessment.ResultIdentity {
				t.Fatalf("lost execution on recovery: %+v", recovery)
			}
			after, err := os.ReadFile(filepath.Join(output, "journal.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("recovery rewrote retained evidence")
			}
		})
	}
}
