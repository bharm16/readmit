package connectedrun_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestFlowSnapshotVerifiesAndExtractsEvidenceWithoutItsSourceFolder(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	p := h.prepare(t, "snapshot")
	output := filepath.Join(h.root, "run-snapshot")
	if _, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{}); err != nil {
		t.Fatal(err)
	}
	files := capturedLifecycle(t, output)
	if err := os.Rename(output, filepath.Join(h.root, "moved-snapshot")); err != nil {
		t.Fatal(err)
	}
	witness := h.fixture.target.received.Load()
	evidence, err := connectedrun.VerifyFlowEvidence(t.Context(), files)
	if err != nil || len(evidence.Phases) != 2 {
		t.Fatal("captured lifecycle depends on a reopened directory", err)
	}
	for id, phase := range evidence.Phases {
		if len(phase.Tables) == 0 {
			t.Fatal("missing retained table", id)
		}
		for dataset, table := range phase.Tables {
			if !table.Usable || phase.Observations[dataset] == "" {
				t.Fatal("captured table lost its retained provenance", id, dataset)
			}
		}
	}
	identity := evidence.Identity
	files["plan/flow.json"][0] = '!'
	if evidence.Plan.Identity() != evidence.Result.Plan || evidence.Identity != identity {
		t.Fatal("caller bytes altered the verified reading")
	}
	if _, err := connectedrun.VerifyFlowEvidence(t.Context(), files); err == nil {
		t.Fatal("changed captured bytes were accepted")
	}
	if h.fixture.target.received.Load() != witness {
		t.Fatal("offline snapshot verification repeated stimulus")
	}
}

func capturedLifecycle(t *testing.T, output string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel(output, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestFHIRFlowSnapshotKeepsResponsesTablesAndBindingsAfterRelocation(t *testing.T) {
	h := connectedlab.New(t, "")
	h.Compile(nativeFlow(h))
	h.Lab.SetDownstream("encounter")
	result, _ := h.Run("snapshot")
	if result.Verdict != assertion.VerdictPass {
		t.Fatal("independent corrected target did not pass")
	}
	output := filepath.Join(h.Root, "snapshot")
	files := capturedLifecycle(t, output)
	if err := os.Rename(output, filepath.Join(h.Root, "relocated")); err != nil {
		t.Fatal(err)
	}
	evidence, err := connectedrun.VerifyFlowEvidence(t.Context(), files)
	if err != nil || evidence.Result.Verdict != assertion.VerdictPass || len(evidence.Phases) != 3 {
		t.Fatal("FHIR reading lost its captured lifecycle", err)
	}
	rescheduled := evidence.Phases["reschedule"]
	if !rescheduled.Tables["appointments"].Usable || !rescheduled.Tables["encounters"].Usable || len(rescheduled.Bound) == 0 {
		t.Fatal("FHIR projection or response bindings were lost")
	}
}
