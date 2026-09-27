package connectedrun_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestFlowResumeRefusesAChangedRetainedSnapshotBeforeEffects(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	resume, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	grantContinuation(t, h, resume)
	var r connectedrun.FlowResult
	flowContractRead(t, filepath.Join(previous, "manifest.json"), &r)
	r.CompletedAt = r.CompletedAt.Add(time.Nanosecond)
	write(t, filepath.Join(previous, "manifest.json"), r)
	resealDirectory(t, previous, connectedrun.FlowSchema)
	// This is a different but independently readable artifact, not mere damage.
	if _, err = connectedrun.OpenFlow(t.Context(), previous); err != nil {
		t.Fatal(err)
	}
	if _, err = connectedrun.ResumeFlow(t.Context(), resume, filepath.Join(h.root, "refused"), testisolation.Confirmation{}); err == nil {
		t.Fatal("reviewed snapshot silently replaced")
	}
	if h.fixture.target.received.Load() != 1 {
		t.Fatal("changed prior artifact caused stimulus")
	}
}
func TestFlowReaderRejectsResealedDenominatorChronologyAndBranchClaims(t *testing.T) {
	for _, kind := range []string{"missing-check", "fake-skip", "phase-time", "unknown-phase", "checkpoint"} {
		t.Run(kind, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true)
			p := h.prepare(t, "tamper")
			out := filepath.Join(h.root, "tamper")
			r, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-check":
				r.Phases[0].Checks = r.Phases[0].Checks[:1]
				write(t, filepath.Join(out, "manifest.json"), r)
			case "fake-skip":
				r.Phases[1].State = "skipped"
				write(t, filepath.Join(out, "manifest.json"), r)
			case "phase-time":
				var intent map[string]any
				flowContractRead(t, filepath.Join(out, "intents", "reschedule.json"), &intent)
				intent["at"] = r.StartedAt.Add(-time.Second).Format(time.RFC3339Nano)
				write(t, filepath.Join(out, "intents", "reschedule.json"), intent)
			case "unknown-phase":
				if err = os.WriteFile(filepath.Join(out, "intents", "hidden.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "checkpoint":
				raw, _ := json.Marshal(r.Phases[1])
				if err = os.WriteFile(filepath.Join(out, "phase-booking.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			resealDirectory(t, out, connectedrun.FlowSchema)
			if _, err = connectedrun.OpenFlow(t.Context(), out); err == nil {
				t.Fatal("semantic tamper accepted")
			}
		})
	}
}
