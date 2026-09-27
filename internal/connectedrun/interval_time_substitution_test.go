package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/observesource"
)

func TestConnectedIntervalRejectsOlderEvidenceInLaterActualReplay(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	target.notifications = make(chan int, 2)
	p := intervalPrepared(t, dir, target)
	execute := func(name string) string {
		t.Helper()
		target.reset("fixed")
		clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		type answer struct {
			result connectedrun.Result
			err    error
		}
		done := make(chan answer, 1)
		path := filepath.Join(dir, name)
		go func() {
			r, e := connectedrun.ExecuteWithClock(ctx, p, "same-instance", path, clock)
			done <- answer{r, e}
		}()
		samples := 0
		for {
			select {
			case advance := <-clock.waits:
				samples++
				if samples == 1 {
					for i := 0; i < 2; i++ {
						select {
						case <-target.notifications:
						case <-ctx.Done():
							t.Fatal("independent target did not receive stimulus")
						}
					}
					waitStimulusFinish(t, ctx, path)
				}
				select {
				case advance <- 10 * time.Millisecond:
				case <-ctx.Done():
					t.Fatal("collector did not advance")
				}
			case got := <-done:
				if got.err != nil || got.result.State != "complete" || got.result.Verdict != assertion.VerdictPass {
					t.Fatal("actual replay positive control", got.result, got.err)
				}
				if samples < 10 {
					t.Fatal("horizon ended early")
				}
				if _, err := connectedrun.Open(context.Background(), path); err != nil {
					t.Fatal("unmodified replay failed readback", err)
				}
				return path
			case <-ctx.Done():
				t.Fatal("deterministic replay did not complete", ctx.Err())
			}
		}
	}
	olderPath := execute("older")
	laterPath := execute("later")
	older := intervalSubstitutionFiles(t, olderPath)
	later := intervalSubstitutionFiles(t, laterPath)
	var oldRun, laterRun connectedrun.IntervalRun
	if err := json.Unmarshal(older["manifest.json"], &oldRun); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(later["manifest.json"], &laterRun); err != nil {
		t.Fatal(err)
	}
	if oldRun.Summary.Plan != laterRun.Summary.Plan || oldRun.Summary.Instance != laterRun.Summary.Instance || !oldRun.Summary.CompletedAt.Before(laterRun.Summary.StartedAt) {
		t.Fatal("fixture did not establish distinct actual replays of the same plan and instance")
	}
	for _, variant := range []string{"unchanged", "older-interval-and-acquisitions", "older-after-with-current-baseline"} {
		t.Run(variant, func(t *testing.T) {
			files := map[string][]byte{}
			for name, raw := range later {
				files[name] = raw
			}
			manifest := laterRun
			if variant != "unchanged" {
				for name := range files {
					if strings.HasPrefix(name, "intervals/") || strings.HasPrefix(name, "observations/") || strings.HasPrefix(name, "evaluation/") {
						delete(files, name)
					}
				}
				for name, raw := range older {
					if strings.HasPrefix(name, "intervals/") || strings.HasPrefix(name, "observations/") || strings.HasPrefix(name, "evaluation/") {
						files[name] = raw
					}
				}
				manifest.Intervals = oldRun.Intervals
				manifest.Acquisitions = oldRun.Acquisitions
				manifest.Boundaries = oldRun.Boundaries
				manifest.Summary.Armed = oldRun.Summary.Armed
				manifest.Summary.Observations = oldRun.Summary.Observations
				manifest.Summary.Evaluation = oldRun.Summary.Evaluation
			}
			if variant == "older-after-with-current-baseline" {
				manifest.Acquisitions = map[string][]string{"before": laterRun.Acquisitions["before"], "after": oldRun.Acquisitions["after"]}
				manifest.Summary.Armed = map[string]string{"before": laterRun.Summary.Armed["before"], "after": oldRun.Summary.Armed["after"]}
				for name, raw := range later {
					if strings.HasPrefix(name, "observations/before-before/") {
						files[name] = raw
					}
				}
				plan, err := connectedtest.OpenPlan(filepath.Join(laterPath, "plan"))
				if err != nil {
					t.Fatal(err)
				}
				evaluation, err := connectedtest.OpenDatasetResult(context.Background(), filepath.Join(laterPath, "evaluation"))
				if err != nil {
					t.Fatal(err)
				}
				baseline, err := observesource.OpenDataset(context.Background(), filepath.Join(laterPath, "observations", "before-before"))
				if err != nil {
					t.Fatal(err)
				}
				after, err := dataset.Open(context.Background(), filepath.Join(olderPath, filepath.FromSlash(oldRun.Summary.Observations["after"])))
				if err != nil {
					t.Fatal(err)
				}
				rebuiltPath := filepath.Join(t.TempDir(), "evaluation")
				rebuilt, err := connectedtest.RetainDatasetResult(context.Background(), plan, evaluation.Execution, map[string]*dataset.Snapshot{"before": baseline, "after": after}, rebuiltPath)
				if err != nil || rebuilt.Verdict != assertion.VerdictPass {
					t.Fatal("mixed evidence should remain independently evaluable", rebuilt, err)
				}
				encoded, _ := json.Marshal(rebuilt, json.Deterministic(true))
				manifest.Summary.Evaluation = dataset.Digest(encoded)
				for name := range files {
					if strings.HasPrefix(name, "evaluation/") {
						delete(files, name)
					}
				}
				for name, raw := range intervalSubstitutionFiles(t, rebuiltPath) {
					files["evaluation/"+name] = raw
				}
			}
			raw, err := json.Marshal(manifest, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			files["manifest.json"] = raw
			files["identity.sha256"] = []byte(artifactdir.Identity(connectedrun.SchemaV2, files) + "\n")
			path := filepath.Join(t.TempDir(), "retained")
			for name, raw := range files {
				destination := filepath.Join(path, filepath.FromSlash(name))
				if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(destination, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := connectedrun.Open(context.Background(), path)
			if variant == "unchanged" {
				if err != nil || got.State != "complete" || got.Verdict != assertion.VerdictPass {
					t.Fatal("unchanged resealed control failed", got, err)
				}
			} else if err == nil {
				t.Fatal("valid older interval/acquisitions were accepted as coverage of a later actual replay", got)
			}
		})
	}
}
func intervalSubstitutionFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	if err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		name, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}
