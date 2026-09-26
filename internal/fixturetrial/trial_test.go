package fixturetrial

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// newTrial builds one trial over a freshly written planted case: root holds
// the case and the trial's session directory, and the spec is rebound onto
// them.
func newTrial(t *testing.T, mode observation.Mode, budget time.Duration) Trial {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "trial")
	os.MkdirAll(dir, 0o700)
	var inputs []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, bundle.Input{Path: filepath.Join(root, name+".mllp"), Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	imported := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(filepath.Join(root, "original.case"), inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.ReadSpec("../../testdata/fixtures/redact-spec.json")
	if err != nil {
		t.Fatal(err)
	}
	spec.Input.Case = "../original.case"
	spec.Target = targetName
	spec.Observation.Path = "observation.json"
	return Trial{
		Mode: mode, Dir: dir, Spec: spec,
		CasePath:        filepath.Join(dir, "receiver.case"),
		ObservationPath: filepath.Join(dir, "observation.json"),
		Budget:          budget,
		Configure: func(session Session) error {
			target, err := json.Marshal(replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: session.Address(), Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 16384})
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, targetName), target, 0o600); err != nil {
				return err
			}
			specBytes, err := json.Marshal(spec)
			if err != nil {
				return err
			}
			return os.WriteFile(session.SpecPath(), specBytes, 0o600)
		},
	}
}

// Run sends the spec against a freshly bound fixture and the sender retains a
// complete result whose acknowledgements are the canonical fixture ones.
func TestTrialRunsASpecAndAnswersWhetherItsACKsAreCanonical(t *testing.T) {
	trial := newTrial(t, observation.Fixed, 30*time.Second)
	outcome := Run(t.Context(), trial)
	if outcome.ConfigErr != nil || outcome.SenderErr != nil || outcome.ServeErr != nil {
		t.Fatalf("the trial failed: config=%v send=%v serve=%v", outcome.ConfigErr, outcome.SenderErr, outcome.ServeErr)
	}
	artifact := outcome.Artifact
	if artifact == nil || artifact.Result.Status != testrunner.Pass || artifact.Run == nil || artifact.FinalObservation == nil {
		t.Fatalf("the corrected fixture did not pass the spec: %+v", artifact)
	}
	if len(artifact.Run.Events) != len(trial.Spec.Input.Messages) {
		t.Fatalf("the trial sent %d messages: %+v", len(artifact.Run.Events), artifact.Run.Events)
	}
	for i, event := range artifact.Run.Events {
		sent, err := artifact.Run.Raw(event.Sent)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := artifact.Run.Raw(event.Received)
		if err != nil {
			t.Fatal(err)
		}
		receivedID := fmt.Sprintf("s0001-e%06d", 2*i+1)
		if err := VerifyACK(sent, raw, i+1, artifact.Result.ReceiverSessionID, receivedID); err != nil {
			t.Fatalf("acknowledgement %d is not the canonical fixture answer: %v", i+1, err)
		}
		corrupted := append([]byte{}, raw...)
		corrupted[len(corrupted)-4] ^= 0x20
		if VerifyACK(sent, corrupted, i+1, artifact.Result.ReceiverSessionID, receivedID) == nil {
			t.Fatalf("acknowledgement %d verified despite a changed byte", i+1)
		}
	}
}

// The fixture waits as long as the trial itself may take for the sender, whose
// own evidence storage syncs what it has just recorded before sending the next
// message; a sender that fails ends the trial as the send's failure, not the
// fixture's.
func TestTrialOutlastsAStalledSenderAndNamesAFailedOne(t *testing.T) {
	stall := 6 * time.Second
	if testing.Short() {
		stall = time.Second
	}
	original := Sender
	t.Cleanup(func() { Sender = original })
	Sender = func(ctx context.Context, specPath, output string, durability artifactdir.Durability) (*testrunner.Artifact, error) {
		// Stand in for the sender's own storage holding the recorded evidence.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(stall):
		}
		return testrunner.Run(ctx, specPath, output)
	}
	trial := newTrial(t, observation.Defective, 30*time.Second)
	outcome := Run(t.Context(), trial)
	if outcome.SenderErr != nil || outcome.ServeErr != nil || outcome.Artifact == nil || outcome.Artifact.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("a stalled sender changed the trial: %+v", outcome)
	}

	Sender = func(ctx context.Context, specPath, output string, durability artifactdir.Durability) (*testrunner.Artifact, error) {
		return nil, errors.New("sender storage refused the result")
	}
	failed := newTrial(t, observation.Defective, 30*time.Second)
	outcome = Run(t.Context(), failed)
	if outcome.SenderErr == nil || outcome.ServeErr != nil {
		t.Fatalf("a failed send was not the send's failure: %+v", outcome)
	}
}
