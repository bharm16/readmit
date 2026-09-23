package desktop_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/index"
)

// performanceSamples is how many measured samples each operation takes after
// its one excluded warm-up. Nearest-rank p95 of twenty is the nineteenth.
const performanceSamples = 20

// Opt-in measurements, not timing assertions: a busy machine must not turn a
// correctness test into a false failure or a proposed envelope into a pass.
// Every operation goes through the public facade the window calls, over the
// largest case a bundle admits (10,000 occurrences), and every batch records
// the host's load before and after it and the live heap after it, so a number
// lifted out of the log still says what else the machine was doing.
func TestPerformanceEnvelope(t *testing.T) {
	if os.Getenv("READMIT_PERFORMANCE") != "1" {
		t.Skip("set READMIT_PERFORMANCE=1 for local measurements")
	}
	root := t.TempDir()
	wire := strings.Repeat(framed(gridBooking), bundle.MaxEvents)
	opened := writeCase(t, root, "scale", wire)
	name := writeIndex(t, root, "scale.index.json", opened, nil)
	document, err := index.Build(context.Background(), opened, index.Policy{Fields: []string{patientField}, Retention: index.RetainValues}, indexedAt())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "navigation.mllp")
	if err := os.WriteFile(source, []byte(wire), 0600); err != nil {
		t.Fatal(err)
	}
	largest := filepath.Join(root, "largest.mllp")
	if err := os.WriteFile(largest, []byte(largestSource()), 0600); err != nil {
		t.Fatal(err)
	}
	// Import, index construction and draft retention write, so they admit an
	// author; the reads measured beside them do not need one.
	app := workspaceApp(t)
	t.Logf("fixture messages=%d bytes=%d sha256=%x; repeated synthetic gridBooking; warm OS cache, no race; os=%s arch=%s cpus=%d gomaxprocs=%d",
		bundle.MaxEvents, len(wire), sha256.Sum256([]byte(wire)), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))
	measure := func(label string, count int, operation func(sample int)) {
		before := loadContext()
		operation(0) // warm-up excluded; report every measured sample.
		samples := make([]float64, count)
		for i := range samples {
			started := time.Now()
			operation(i + 1)
			samples[i] = milliseconds(time.Since(started))
		}
		t.Logf("%s samples_ms=%v nearest_rank_p95_ms=%.3f load_before=[%s] load_after=[%s] live_heap_bytes=%d",
			label, samples, nearestRankP95(samples), before, loadContext(), liveHeap())
	}
	plan := mllpPlan()

	measure("warm indexed exact search", performanceSamples, func(int) {
		got, e := document.Search(indexedAt(), index.Query{Match: index.Equals, Term: []byte("MRN-1^^^READMIT^MR")})
		if e != nil || len(got.Hits) != bundle.MaxEvents {
			t.Fatalf("search hits=%d error=%v", len(got.Hits), e)
		}
	})
	measure("facade grid navigation (not UI paint)", performanceSamples, func(int) {
		got := app.OpenGrid(root, "scale", name, bundle.MaxEvents-200, 200)
		if got.State != desktop.Completed || got.Grid == nil || len(got.Grid.Rows) != 200 || got.Grid.Total != bundle.MaxEvents {
			t.Fatal(fmt.Sprintf("grid state=%s", got.State))
		}
	})
	measure("facade workspace search", performanceSamples, func(int) {
		if got := app.Search(root, "scale"); got.State != desktop.Completed {
			t.Fatalf("search state=%s", got.State)
		}
	})
	measure("facade own-evidence import preview", performanceSamples, func(int) {
		got := app.PreviewImport(desktop.ImportRequest{Workspace: root, Mode: "plan", Files: []string{source}, Plan: &plan})
		if got.State != desktop.Completed || got.PlanPreview == nil || got.PlanPreview.Totals.Sources != 1 {
			t.Fatalf("preview: %+v", got.State)
		}
	})
	// A file is read whole and its one member divided whole before an import
	// next observes a cancellation, so this is how long that stretch takes at
	// the per-source bound.
	measure("facade import preview, one 16 MiB file", performanceSamples, func(int) {
		got := app.PreviewImport(desktop.ImportRequest{Workspace: root, Mode: "plan", Files: []string{largest}, Plan: &plan})
		if got.State != desktop.Completed || got.PlanPreview == nil || got.PlanPreview.Totals.Sources != 1 {
			t.Fatalf("largest preview: %s %s", got.State, got.Reason)
		}
	})
	// Committing writes one synced file per occurrence, so it is measured over
	// fewer samples; nearest-rank p95 of five is the slowest.
	measure("facade own-evidence import commit", 5, func(sample int) {
		output := "imported-" + strconv.Itoa(sample)
		got := app.CommitImport(desktop.ImportCommitRequest{Workspace: root, Mode: "plan", OutputName: output, Files: []string{source}, Plan: &plan})
		if got.State != desktop.Completed || got.Case == nil || got.Case.Occurrences != bundle.MaxEvents {
			t.Fatalf("commit: %s %s", got.State, got.Reason)
		}
	})
	measure("facade index build", performanceSamples, func(sample int) {
		got := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "scale", Output: "built-" + strconv.Itoa(sample) + ".index.json",
			Fields: []string{patientField}, Retention: "values", RetainUntil: "indefinite"})
		if got.State != desktop.Completed || got.Index == nil {
			t.Fatalf("index build: %s %s", got.State, got.Reason)
		}
	})
	draft := editorDraft("note", desktop.NoteDraftSchema, "")
	measure("facade durable draft acknowledgement", performanceSamples, func(sample int) {
		draft.Content = []byte(noteRevision(sample + 1))
		got := app.SaveEditorDraft(draft)
		if got.State != desktop.Completed || len(got.Drafts) != 1 {
			t.Fatalf("draft: %+v", got.State)
		}
		draft.ID = got.Drafts[0].ID
	})

	// Cancellation is measured from the request to the facade's answer. The
	// request is made only once the operation is under way — by default once it
	// holds the slot, which the facade states by refusing a second read as
	// busy — so it is never timed against an operation that had not started.
	// Every attempt writes to a new name, because an attempt that finished
	// before it could be cancelled wrote one.
	cancellation := func(label, operation string, underway func(attempt int, answered chan desktop.State) bool, start func(attempt int) desktop.State) {
		before := loadContext()
		var samples []float64
		completed, retries, attempt := 0, 0, 0
		for sample := 0; sample <= performanceSamples; {
			attempt++
			var answered chan desktop.State
			holding := false
			if underway == nil {
				answered, holding = startHolding(t, app, root, bareState, func() desktop.State { return start(attempt) })
			} else {
				answered = make(chan desktop.State, 1)
				go func(attempt int) { answered <- start(attempt) }(attempt)
				holding = underway(attempt, answered)
			}
			if !holding {
				// The operation finished before it could be asked to stop,
				// which is not a cancellation sample; the sample is taken again.
				<-answered
				retries++
				if retries > 3*performanceSamples {
					t.Fatalf("%s never stayed under way long enough to cancel", label)
				}
				continue
			}
			requested := time.Now()
			app.Cancel(operation)
			state := <-answered
			elapsed := milliseconds(time.Since(requested))
			if state != desktop.Cancelled && state != desktop.Completed {
				t.Fatalf("%s answered %s to a cancellation", label, state)
			}
			switch {
			case sample == 0: // warm-up excluded
			case state == desktop.Cancelled:
				samples = append(samples, elapsed)
			default:
				completed++
			}
			sample++
		}
		summary := "no cancelled sample"
		if len(samples) > 0 {
			summary = fmt.Sprintf("nearest_rank_p95_ms=%.3f", nearestRankP95(samples))
		}
		t.Logf("%s cancelled_samples_ms=%v %s completed_before_cancel_took_effect=%d finished_before_cancel_retries=%d load_before=[%s] load_after=[%s]",
			label, samples, summary, completed, retries, before, loadContext())
	}
	cancellation("cancel own-evidence import preview", "import", nil, func(int) desktop.State {
		return app.PreviewImport(desktop.ImportRequest{Workspace: root, Mode: "plan", Files: []string{source}, Plan: &plan}).State
	})
	// The longest phase of an import is writing its case, so this cancels
	// only once the first payload of the new case has been written.
	writing := func(attempt int, answered chan desktop.State) bool {
		for {
			if written, _ := os.ReadDir(filepath.Join(root, "cancelled-"+strconv.Itoa(attempt), "payloads")); len(written) > 0 {
				return true
			}
			select {
			case state := <-answered:
				answered <- state
				return false
			case <-time.After(100 * time.Microsecond):
			}
		}
	}
	cancellation("cancel own-evidence import commit while it writes its case", "import", writing, func(attempt int) desktop.State {
		output := "cancelled-" + strconv.Itoa(attempt)
		got := app.CommitImport(desktop.ImportCommitRequest{Workspace: root, Mode: "plan", OutputName: output, Files: []string{source}, Plan: &plan})
		if got.State == desktop.Cancelled {
			if opened := app.OpenCase(root, output); opened.State == desktop.Completed {
				t.Errorf("a cancelled import left a case the reader accepts: %s", output)
			}
		}
		return got.State
	})
	cancellation("cancel index build", "", nil, func(attempt int) desktop.State {
		return app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "scale", Output: "cancelled-" + strconv.Itoa(attempt) + ".index.json",
			Fields: []string{patientField}, Retention: "values", RetainUntil: "indefinite"}).State
	})
}

// largestSource is one MLLP source at both case bounds at once: as many
// occurrences as a bundle admits, padded so the source is as large as one
// source may be.
func largestSource() string {
	frame := (bundle.MaxSourceBytes / bundle.MaxEvents) - len(framed(""))
	padding := frame - len(gridBooking) - len("NTE|1||\r")
	return strings.Repeat(framed(gridBooking+"NTE|1||"+strings.Repeat("X", padding)+"\r"), bundle.MaxEvents)
}

// loadContext reports the host's one, five and fifteen minute load averages as
// the operating system states them. It is recorded beside every batch, never
// judged: nothing here turns a quiet or a busy host into a verdict.
func loadContext() string {
	switch runtime.GOOS {
	case "linux":
		if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
			if fields := strings.Fields(string(raw)); len(fields) >= 3 {
				return strings.Join(fields[:3], " ")
			}
		}
	case "darwin":
		if raw, err := exec.Command("/usr/sbin/sysctl", "-n", "vm.loadavg").Output(); err == nil {
			return strings.Trim(strings.TrimSpace(string(raw)), "{} ")
		}
	}
	return "unavailable"
}

// liveHeap is the Go heap still reachable after a collection: what the
// operations above retained, not what they allocated on the way.
func liveHeap() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func milliseconds(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1e6 }

func nearestRankP95(samples []float64) float64 {
	ordered := append([]float64(nil), samples...)
	sort.Float64s(ordered)
	return ordered[(len(ordered)*95+99)/100-1]
}
