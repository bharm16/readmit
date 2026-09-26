package receiver_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/fixturetrial"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// trialFixture writes the planted booking and reschedule case and reads the
// fixture spec that sends them, returning the root, the spec and the case
// reference the executed copy names from its session directory.
func trialFixture(t *testing.T) (string, testrunner.Spec) {
	t.Helper()
	root := t.TempDir()
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
	return root, spec
}

// Every trial sends to this fixture in process, under the message timeout the
// trial's target records (#350). A durable fixture flushes its ledger twice per
// message before the ACK, so flushes stalled by three seconds alone run out
// that window, as the durable control of
// TestInProcessLedgerAnswersWithoutWaitingForTheDisk shows at a smaller scale.
// A trial installs its ledger without flushing it, so it still completes with
// the verdict an unstalled disk gives.
func TestFixtureTrialsDoNotWaitForStalledLedgerFlushes(t *testing.T) {
	restore := receiver.DelayLedgerSyncForTest(3 * time.Second)
	defer restore()
	root, spec := trialFixture(t)
	if spec.Observation.Boundary != testrunner.LedgerBoundary {
		t.Fatal("this control holds the trial to the fixture's ledger boundary")
	}
	spec.Input.Case = "../original.case"
	spec.Target = "target.json"
	spec.Observation.Path = "observation.json"
	dir := filepath.Join(root, "trial")
	os.MkdirAll(dir, 0o700)
	outcome := fixturetrial.Run(t.Context(), fixturetrial.Trial{
		Mode: observation.Defective, Dir: dir, Spec: spec,
		CasePath:        filepath.Join(dir, "receiver.case"),
		ObservationPath: filepath.Join(dir, "observation.json"),
		Budget:          30 * time.Second,
		Configure: func(session fixturetrial.Session) error {
			target, err := json.Marshal(replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: session.Address(), Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 16384})
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "target.json"), target, 0o600); err != nil {
				return err
			}
			specBytes, err := json.Marshal(spec)
			if err != nil {
				return err
			}
			return os.WriteFile(session.SpecPath(), specBytes, 0o600)
		},
	})
	if outcome.ConfigErr != nil || outcome.SenderErr != nil || outcome.ServeErr != nil {
		t.Fatalf("the trial did not outlast its sender's stalled storage: config=%v send=%v serve=%v", outcome.ConfigErr, outcome.SenderErr, outcome.ServeErr)
	}
	if outcome.Artifact == nil || outcome.Artifact.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("stalled flushes changed the defective fixture's verdict: %+v", outcome.Artifact)
	}
}
