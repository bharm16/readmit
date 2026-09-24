package redact_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestReexecutionRefusesCancellationAndMissingApproval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := redact.PrepareReexecution(ctx, redact.ReexecutionRequest{}); err == nil {
		t.Fatal("cancelled preparation accepted")
	}
	if _, err := redact.PrepareReexecution(context.Background(), redact.ReexecutionRequest{}); err == nil {
		t.Fatal("missing approval accepted")
	}
}

func reexecutionFixture(t *testing.T, phase string) (redact.ReexecutionRequest, string) {
	t.Helper()
	root := t.TempDir()
	var sources []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, bundle.Input{Path: name + ".mllp", Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	now := time.Now().UTC()
	source := filepath.Join(root, "original.case")
	if _, err := bundle.Write(source, sources, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"spec", "policy", "inventory"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(root, name+".json"), raw)
	}
	reviewPath, private := filepath.Join(root, "review"), filepath.Join(root, "private")
	review, err := redact.Create(context.Background(), redact.Request{CasePath: source, SpecPath: filepath.Join(root, "spec.json"), PolicyPath: filepath.Join(root, "policy.json"), InventoryPath: filepath.Join(root, "inventory.json"), Output: reviewPath, LocalState: private})
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("review: %v %+v", err, review)
	}
	run := "baseline"
	if phase == "pass" {
		run = "postfix"
	}
	// These are retained real executions, deliberately labelled fixture evidence.
	proof := filepath.Join(private, "original-proof", run)
	original := filepath.Join(root, "original-packet")
	if _, err := report.Assemble(context.Background(), report.RetainedInput{Case: source, Spec: filepath.Join(proof, "result", "spec.json"), Current: filepath.Join(proof, "result")}, original); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(proof, "target.json"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "target.json"), raw)
	spec, err := testrunner.ReadSpec(filepath.Join(reviewPath, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Case = filepath.Join(reviewPath, "case")
	spec.Target = filepath.Join(root, "target.json")
	spec.Observation.Path = filepath.Join(root, "observation.json")
	raw, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "rerun.json"), raw)
	return redact.ReexecutionRequest{ReviewPath: reviewPath, LocalState: private, Approval: review.Identity, OriginalPacket: original, SpecPath: filepath.Join(root, "rerun.json"), Phase: phase}, root
}
func put(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReexecutionRetainsActualCriteriaWithoutPromotingFixtureProof(t *testing.T) {
	for _, phase := range []string{"failure", "pass", "changed"} {
		t.Run(phase, func(t *testing.T) {
			selectedPhase := phase
			if phase == "changed" {
				selectedPhase = "failure"
			}
			request, root := reexecutionFixture(t, selectedPhase)
			original, err := testrunner.Open(filepath.Join(request.OriginalPacket, "current"))
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp4", original.Result.Target.Address)
			if err != nil {
				t.Fatal(err)
			}
			mode := observation.Defective
			if phase == "pass" || phase == "changed" {
				mode = observation.Fixed
			}
			server, err := receiver.New(receiver.Config{Mode: mode, OutputPath: filepath.Join(root, "receiver"), ObservationPath: filepath.Join(root, "observation.json"), MaxFrameBytes: 1 << 20, IdleTimeout: 5 * time.Second, MaxMessages: 2})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { _, err := server.Serve(ctx, listener); done <- err }()
			defer func() { cancel(); listener.Close(); <-done }()
			plan, err := redact.PrepareReexecution(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Preview().Criteria != "not-executed" {
				t.Fatal("preview invented proof")
			}
			got, err := plan.Execute(ctx, filepath.Join(root, "job"))
			if err != nil {
				t.Fatal(err)
			}
			want := "matched"
			if phase == "changed" {
				want = "changed"
			}
			if got.Criteria != want || got.ExternalEquivalence != "declined" || got.ResultIdentity == "" || got.Disclosure != "customer-local-only-new-review-required" {
				t.Fatalf("invented equivalence or lost proof: %+v", got)
			}
			if _, err := plan.Execute(ctx, filepath.Join(root, "job")); err == nil {
				t.Fatal("overwrote or repeated job")
			}
		})
	}
}

func TestReexecutionRefusesChangedReviewCriteriaSourceAndTargetBeforeSend(t *testing.T) {
	for _, change := range []string{"approval", "phase", "case", "criteria", "target", "policy", "original", "cancel", "after-preview"} {
		t.Run(change, func(t *testing.T) {
			request, root := reexecutionFixture(t, "failure")
			ctx := context.Background()
			switch change {
			case "approval":
				request.Approval = strings.Repeat("0", 64)
			case "phase":
				request.Phase = "pass"
			case "case", "criteria", "target":
				spec, err := testrunner.ReadSpec(request.SpecPath)
				if err != nil {
					t.Fatal(err)
				}
				if change == "case" {
					spec.Input.Case = filepath.Join(root, "original.case")
				}
				if change == "criteria" {
					n := 2
					spec.Assertions[0].Expected.Count = &n
				}
				if change == "target" {
					raw, err := os.ReadFile(filepath.Join(root, "target.json"))
					if err != nil {
						t.Fatal(err)
					}
					put(t, filepath.Join(root, "target.json"), []byte(strings.Replace(string(raw), `"2s"`, `"1s"`, 1)))
				}
				raw, err := json.Marshal(spec)
				if err != nil {
					t.Fatal(err)
				}
				put(t, request.SpecPath, raw)
			case "policy":
				put(t, filepath.Join(root, "policy.json"), []byte("{}"))
			case "original":
				os.Remove(filepath.Join(request.OriginalPacket, "identity.sha256"))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "after-preview":
				plan, err := redact.PrepareReexecution(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(root, "policy.json"), []byte("{}"))
				if _, err := plan.Execute(ctx, filepath.Join(root, "job")); err == nil {
					t.Fatal("stale preview sent")
				}
				return
			}
			if _, err := redact.PrepareReexecution(ctx, request); err == nil {
				t.Fatal("changed admission accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "job")); !os.IsNotExist(err) {
				t.Fatal("refusal created execution")
			}
		})
	}
}

// A pinned send executes only the preparation its preview identified: a
// rebound specification edited since — still a valid reexecution, but not the
// one previewed — or no identity at all is refused before the job exists.
func TestAPinnedReexecutionRefusesAPreparationItsPreviewDidNotIdentify(t *testing.T) {
	request, root := reexecutionFixture(t, "failure")
	ctx := context.Background()
	previewed, err := redact.PrepareReexecution(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := previewed.Identity()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.ReadSpec(request.SpecPath)
	if err != nil {
		t.Fatal(err)
	}
	spec.Setup.ResetInstructions += " Confirm the ledger is empty."
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	put(t, request.SpecPath, raw)
	edited, err := redact.PrepareReexecution(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	for name, pinned := range map[string]string{"an edited specification": identity, "no identity": ""} {
		if _, err := edited.ExecutePinned(ctx, filepath.Join(root, "job"), pinned); !errors.Is(err, durablerun.ErrInputsChanged) {
			t.Fatalf("%s was sent: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "job")); !os.IsNotExist(err) {
		t.Fatal("a refused send created its job")
	}
}

func TestReexecutionCancellationRetainsUncertainDeliveryWithoutRetry(t *testing.T) {
	request, root := reexecutionFixture(t, "failure")
	original, err := testrunner.Open(filepath.Join(request.OriginalPacket, "current"))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := os.ReadFile(filepath.Join(request.OriginalPacket, "current", "initial-observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "observation.json"), initial)
	listener, err := net.Listen("tcp4", original.Result.Target.Address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		var first [1]byte
		if n, _ := connection.Read(first[:]); n > 0 {
			cancel()
		}
	}()
	defer func() { listener.Close(); <-done }()
	plan, err := redact.PrepareReexecution(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "job")
	result, err := plan.Execute(ctx, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Criteria != "unavailable-or-unstable" || result.ExternalEquivalence != "declined" {
		t.Fatalf("cancelled send certified: %+v", result)
	}
	recovered, err := durablerun.Recover(output)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SafeToRepeat || recovered.Uncertain != 1 || !recovered.Run.DeliveryUncertain {
		t.Fatalf("uncertain send became repeatable: %+v", recovered)
	}
	if _, err := plan.Execute(ctx, filepath.Join(root, "retry")); err == nil {
		t.Fatal("cancelled execution retried")
	}
}

func TestReexecutionCannotWriteInsideAnyOriginalEvidence(t *testing.T) {
	request, root := reexecutionFixture(t, "failure")
	plan, err := redact.PrepareReexecution(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Join(root, "original.case"), filepath.Join(request.LocalState, "original-proof", "baseline", "result"), request.OriginalPacket, request.ReviewPath} {
		output := filepath.Join(parent, "job")
		if _, err := plan.Execute(context.Background(), output); err == nil {
			t.Fatal("nested evidence write accepted")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("refusal wrote inside evidence")
		}
	}
	alias := filepath.Join(root, "original-alias")
	if err := os.Symlink(filepath.Join(root, "original.case"), alias); err == nil {
		if _, err := plan.Execute(context.Background(), filepath.Join(alias, "job")); err == nil {
			t.Fatal("symlink bypassed containment")
		}
	}
	if _, err := redact.PrepareReexecution(context.Background(), request); err != nil {
		t.Fatalf("admission modified originals: %v", err)
	}
}
