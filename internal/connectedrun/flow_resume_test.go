package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

func interruptedFlow(t *testing.T) (*flowContractHarness, *connectedrun.PreparedFlow, string) {
	t.Helper()
	// Resume/store tests do not qualify a tighter sampling threshold than an
	// otherwise admitted HTTP read. Keep the finite parent gap allowance while
	// preserving the patient acquisition budget and the 120 ms business horizon.
	h := newFlowContractHarnessForRecovery(t)
	store := filepath.Join(h.root, "recovery-store")
	if err := os.Mkdir(store, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "store-id"), []byte(networkaction.Digest([]byte("independent owner store"))+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	config.RecoveryStore = store
	write(t, h.configPath, config)
	p := h.prepare(t, "resumable")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(h.root, "stopped")
	result, err := connectedrun.ExecuteFlow(ctx, p, path, testisolation.Confirmation{}, func(phase connectedrun.FlowPhaseResult) {
		if phase.ID == "booking" && phase.State == "complete" && phase.Verdict == assertion.VerdictPass {
			// The detached notification may be mutated by a display consumer. Neither
			// the approved checks nor the stored parent result may change with it.
			phase.Checks[0].Outcome = assertion.OutcomeFailed
			phase.Steps[0].Outcome = "forged"
			cancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "cancelled" || result.Cleanup != "not-started" || result.Phases[0].State != "complete" || result.Phases[0].Verdict != assertion.VerdictPass || result.Phases[1].State != "not-attempted" || h.fixture.target.received.Load() != 1 {
		logFlowStoppingEvidence(t, path)
		t.Fatalf("unsafe stopping point: %+v", result)
	}
	return h, p, path
}
func grantContinuation(t *testing.T, h *flowContractHarness, p *connectedrun.FlowResume) {
	t.Helper()
	for role, review := range p.IsolationReviews() {
		write(t, filepath.Join(h.root, role+"-isolation-grant.json"), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: review.Binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
}
func TestFlowResumeContinuesOnlyNeverAttemptedSuffixWithFreshAuthority(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	before, err := connectedrun.OpenFlow(t.Context(), previous)
	if err != nil {
		t.Fatal(err)
	}
	resume, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	grantContinuation(t, h, resume)
	result, err := connectedrun.ResumeFlow(t.Context(), resume, filepath.Join(h.root, "continued"), testisolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Inherited != 1 || result.Previous == "" || result.Cleanup != "complete" || h.fixture.target.received.Load() != 2 {
		t.Fatalf("resume repeated or omitted work: %+v", result)
	}
	after, err := connectedrun.OpenFlow(t.Context(), previous)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("previous run changed", err)
	}
	if _, err = connectedrun.ResumeFlow(t.Context(), resume, filepath.Join(h.root, "again"), testisolation.Confirmation{}); err == nil {
		t.Fatal("same prior execution resumed twice")
	}
	if h.fixture.target.received.Load() != 2 {
		t.Fatal("duplicate suffix stimulus")
	}
}
func TestFlowCopiedArtifactsCannotResumeConcurrently(t *testing.T) {
	h, p, previous := interruptedFlow(t)
	copyPath := filepath.Join(h.root, "relocated")
	layout := artifactdir.Layout{Noun: "test-copy", AllowFile: func(string) bool { return true }, AllowDirectory: func(string) bool { return true }, MaxFiles: 200000, MaxFileBytes: 64 << 20, MaxBytes: 1 << 30}
	files, err := artifactdir.Read(previous, layout)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range files {
		path := filepath.Join(copyPath, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := connectedrun.PrepareFlowResume(p, previous)
	if err != nil {
		t.Fatal(err)
	}
	b, err := connectedrun.PrepareFlowResume(p, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	grantContinuation(t, h, a)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, item := range []*connectedrun.FlowResume{a, b} {
		wg.Add(1)
		go func(i int, item *connectedrun.FlowResume) {
			defer wg.Done()
			_, e := connectedrun.ResumeFlow(t.Context(), item, filepath.Join(h.root, []string{"one", "two"}[i]), testisolation.Confirmation{})
			errs <- e
		}(i, item)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 || h.fixture.target.received.Load() != 2 {
		t.Fatalf("copied evidence bypassed store: winners=%d sends=%d", success, h.fixture.target.received.Load())
	}
}
func TestFlowResumeRefusesChangedOrAliasedCoordinationStore(t *testing.T) {
	for _, kind := range []string{"identity", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			h, p, previous := interruptedFlow(t)
			var c connectedrun.FlowConfig
			flowContractRead(t, h.configPath, &c)
			switch kind {
			case "identity":
				if err := os.WriteFile(filepath.Join(c.RecoveryStore, "store-id"), []byte(strings.Repeat("b", 64)), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				other := t.TempDir()
				if err := os.WriteFile(filepath.Join(other, "store-id"), []byte(networkaction.Digest([]byte("independent owner store"))), 0600); err != nil {
					t.Fatal(err)
				}
				c.RecoveryStore = other
				write(t, h.configPath, c)
			case "symlink":
				alias := filepath.Join(h.root, "alias")
				if err := os.Symlink(c.RecoveryStore, alias); err != nil {
					t.Fatal(err)
				}
				c.RecoveryStore = alias
				write(t, h.configPath, c)
			}
			if _, err := connectedrun.PrepareFlowResume(p, previous); err == nil {
				t.Fatal("changed trusted store accepted")
			}
			if next, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "resumable"); err == nil {
				if _, err = connectedrun.PrepareFlowResume(next, previous); err == nil {
					t.Fatal("fresh preparation chose another store")
				}
			}
		})
	}
}

// Keep failed CI stopping points diagnosable even when temporary files are not
// uploaded. Report only the independently synthetic fixture's bounded metadata.
func logFlowStoppingEvidence(t *testing.T, path string) {
	t.Helper()
	for _, name := range []string{"phases/booking/manifest.json", "phases/booking/intervals/after/manifest.json"} {
		raw, err := os.ReadFile(filepath.Join(path, name))
		if err != nil {
			t.Log(name, err)
			continue
		}
		if len(raw) > 128<<10 {
			t.Log(name, "metadata exceeds diagnostic bound")
			continue
		}
		t.Logf("retained %s: %s", name, raw)
	}
	_ = filepath.WalkDir(filepath.Join(path, "phases", "booking", "observations"), func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "manifest.json" {
			return nil
		}
		raw, e := os.ReadFile(name)
		if e != nil || len(raw) > 1<<20 {
			return nil
		}
		var doc struct {
			Schema      string `json:"schema"`
			Status      string `json:"status"`
			Acquisition any    `json:"acquisition"`
		}
		if json.Unmarshal(raw, &doc) == nil && doc.Schema == "readmit-dataset/v1" {
			relative, _ := filepath.Rel(path, name)
			t.Logf("retained %s: status=%s acquisition=%+v", relative, doc.Status, doc.Acquisition)
		}
		return nil
	})
}
