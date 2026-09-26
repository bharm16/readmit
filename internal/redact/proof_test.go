package redact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/fixturetrial"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// proofFixture writes the planted booking and reschedule case beside a new
// proof directory and returns the root, the spec and that directory.
func proofFixture(t *testing.T) (string, testrunner.Spec, string) {
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
	proof := filepath.Join(root, "proof")
	if err := os.Mkdir(proof, 0700); err != nil {
		t.Fatal(err)
	}
	return root, spec, proof
}

// observedSender replaces the proof's sender for one test with the ordinary
// test runner reporting to observer.
func observedSender(t *testing.T, observer replay.Observer) {
	t.Helper()
	original := fixturetrial.Sender
	t.Cleanup(func() { fixturetrial.Sender = original })
	fixturetrial.Sender = func(ctx context.Context, specPath, output string, durability artifactdir.Durability) (*testrunner.Artifact, error) {
		plan, err := testrunner.Prepare(specPath)
		if err != nil {
			return nil, err
		}
		return testrunner.ExecuteObserved(ctx, plan, output, observer)
	}
}

// senderStorage stands in for the sender's own evidence storage around the
// first message: before it is sent, and once its event has been recorded.
type senderStorage struct {
	beforeSend func() error
	recorded   func() error
}

func (s senderStorage) BeforeSend(string) error {
	if s.beforeSend == nil {
		return nil
	}
	return s.beforeSend()
}

func (senderStorage) Sent(string, []byte) error { return nil }

func (s senderStorage) Recorded(event replay.Event) error {
	if s.recorded == nil || event.SourceOccurrence != "s0001-e000001" {
		return nil
	}
	return s.recorded()
}

// A fixture proof failed on a loaded Windows runner when local storage, not the
// fixture, ended its session (#331). Between messages the proof's own sender
// syncs the evidence it has just recorded, and the fixture must wait for it as
// long as the proof itself may take. The fixture must also answer without
// flushing its ledger; the receiver's own test stalls that flush, and here the
// proof is held to configuring its fixture that way.
func TestFixtureProofOutlastsItsSendersStorage(t *testing.T) {
	root, spec, proof := proofFixture(t)
	// One second past the five-second idle limit the fixture used to have.
	observedSender(t, senderStorage{recorded: func() error { time.Sleep(6 * time.Second); return nil }})
	artifact, err := runFixture(context.Background(), spec, "../../original.case", filepath.Join(proof, "baseline"), observation.Defective)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Result.Status != testrunner.AssertionFailure || !slices.Equal(failedAssertions(artifact), []int{1, 2}) {
		t.Fatalf("a slow sender changed the defective fixture's verdict: %s", outcome(artifact))
	}
	// A temporary directory's spelling can differ from the resolved one (a
	// short Windows name), so no separator at all may appear.
	if strings.Contains(outcome(artifact), root) || strings.ContainsAny(outcome(artifact), `/\`) {
		t.Fatal("fixture outcome names a path")
	}
}

// Every way a fixture execution can fail says which step failed and why, in
// words that name no path, so it can be reported wherever the proof ran.
func TestFixtureProofFailureNamesItsStepWithoutAPath(t *testing.T) {
	for _, test := range []struct {
		name    string
		storage func(proof string) senderStorage
		want    string
	}{
		{
			name: "receiver-storage",
			storage: func(proof string) senderStorage {
				return senderStorage{beforeSend: func() error {
					ledger := filepath.Join(proof, "baseline", "observation.json")
					if err := os.Remove(ledger); err != nil {
						return err
					}
					return os.Mkdir(ledger, 0700)
				}}
			},
			want: "fixture receiver could not finalize proof evidence (cannot install observation file; startup destination must be new); sender execution_error (transport) at message 1: ",
		},
		{
			name: "sender-storage",
			storage: func(string) senderStorage {
				return senderStorage{recorded: func() error { return errors.New("recording stalled") }}
			},
			want: "fixture test did not complete: cannot execute or finalize test replay; retained output may be incomplete: recording stalled",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, spec, proof := proofFixture(t)
			observedSender(t, test.storage(proof))
			_, err := runFixture(context.Background(), spec, "../../original.case", filepath.Join(proof, "baseline"), observation.Defective)
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), root) || strings.ContainsAny(err.Error(), `/\`) {
				t.Fatalf("proof failure did not name its step privately: %v", err)
			}
		})
	}
}
