package connectedrun_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestFlowOrphanEvidenceCannotBecomeNeverAttemptedResumeWork(t *testing.T) {
	for _, sealed := range []bool{true, false} {
		for _, checkpoint := range []bool{true, false} {
			t.Run(map[bool]string{true: "sealed", false: "interrupted"}[sealed]+map[bool]string{true: "-checkpoint", false: "-child-only"}[checkpoint], func(t *testing.T) {
				h, p, previous := interruptedFlow(t)
				previouslyPrepared, err := connectedrun.PrepareFlowResume(p, previous)
				if err != nil {
					t.Fatal(err)
				}
				var r, start connectedrun.FlowResult
				flowContractRead(t, filepath.Join(previous, "manifest.json"), &r)
				flowContractRead(t, filepath.Join(previous, "started.json"), &start)
				phase := r.Phases[0].ID
				r.Phases[0] = start.Phases[0]
				if err = os.Remove(filepath.Join(previous, "intents", phase+".json")); err != nil {
					t.Fatal(err)
				}
				if !checkpoint {
					if err = os.Remove(filepath.Join(previous, "phase-"+phase+".json")); err != nil {
						t.Fatal(err)
					}
				}
				write(t, filepath.Join(previous, "manifest.json"), r)
				resealDirectory(t, previous, connectedrun.FlowSchema)
				if !sealed {
					if err = os.Remove(filepath.Join(previous, "identity.sha256")); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = connectedrun.OpenFlow(t.Context(), previous); err == nil {
					t.Fatal("orphan child accepted as never attempted")
				}
				if _, err = connectedrun.InspectFlow(t.Context(), previous); err == nil {
					t.Fatal("interrupted reader hid orphan evidence")
				}
				if _, err = connectedrun.PrepareFlowResume(p, previous); err == nil {
					t.Fatal("orphan execution granted resume")
				}
				if _, err = connectedrun.ResumeFlow(t.Context(), previouslyPrepared, filepath.Join(h.root, "forbidden"), testisolation.Confirmation{}); err == nil {
					t.Fatal("old prepared continuation accepted changed orphan evidence")
				}
				if h.fixture.target.received.Load() != 1 {
					t.Fatal("booking retransmitted")
				}
			})
		}
	}
}
func TestFlowInheritedPhaseCannotCarryNewLocalExecutionEvidence(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	resume, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	grantContinuation(t, h, resume)
	output := filepath.Join(h.root, "continued")
	if _, err = connectedrun.ResumeFlow(t.Context(), resume, output, testisolation.Confirmation{}); err != nil {
		t.Fatal(err)
	}
	layout := artifactdir.Layout{AllowFile: func(string) bool { return true }, AllowDirectory: func(string) bool { return true }, MaxFiles: 100000, MaxFileBytes: 64 << 20, MaxBytes: 512 << 20}
	files, err := artifactdir.Read(filepath.Join(previous, "phases", "booking"), layout)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range files {
		target := filepath.Join(output, "phases", "booking", name)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(target, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	resealDirectory(t, output, connectedrun.FlowSchema)
	if _, err = connectedrun.OpenFlow(t.Context(), output); err == nil {
		t.Fatal("inherited phase hid local child execution")
	}
	if err = os.Remove(filepath.Join(output, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if _, err = connectedrun.InspectFlow(t.Context(), output); err == nil {
		t.Fatal("interrupted continuation ignored contradictory child")
	}
	if h.fixture.target.received.Load() != 2 {
		t.Fatal("readback repeated stimulus")
	}
}

func TestFlowEmptyOrphanPhaseDirectoryCannotAuthorizeContinuation(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	prepared, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(previous, "phases", "reschedule"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = connectedrun.OpenFlow(t.Context(), previous); err == nil {
		t.Fatal("empty child directory ignored")
	}
	if _, err = connectedrun.PrepareFlowResume(p, previous); err == nil {
		t.Fatal("empty child directory granted continuation")
	}
	if _, err = connectedrun.ResumeFlow(t.Context(), prepared, filepath.Join(h.root, "forbidden"), testisolation.Confirmation{}); err == nil {
		t.Fatal("prepared continuation ignored new child directory")
	}
	if err = os.Remove(filepath.Join(previous, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	if _, err = connectedrun.InspectFlow(t.Context(), previous); err == nil {
		t.Fatal("interrupted inspector ignored empty child directory")
	}
	if h.fixture.target.received.Load() != 1 {
		t.Fatal("contradictory directory triggered send")
	}
}
