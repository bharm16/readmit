package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntervalCollectorFailureCannotEraseSettledTransportUncertainty(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := intervalPrepared(t, dir, target)
	target.reset("disconnect")
	clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type answer struct {
		r   connectedrun.Result
		err error
	}
	done := make(chan answer, 1)
	output := filepath.Join(dir, "run")
	go func() {
		r, err := connectedrun.ExecuteWithClock(ctx, p, "run-one", output, clock)
		done <- answer{r, err}
	}()
	var advance chan time.Duration
	select {
	case advance = <-clock.waits:
	case <-ctx.Done():
		t.Fatal("collector did not sample")
	}
	waitStimulusFinish(t, ctx, output)
	receipt, err := connectedtransport.Open(filepath.Join(output, "transport"))
	if err != nil || receipt.State != "uncertain" {
		t.Fatal("uncertain receipt not retained before collector failure", receipt, err)
	}
	if err = os.Remove(target.file); err != nil {
		t.Fatal(err)
	}
	advance <- 10 * time.Millisecond
	var got answer
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal("observer failure did not settle")
	}
	if got.err != nil || got.r.State != "uncertain" || got.r.Verdict != assertion.VerdictUndecided {
		t.Fatal(got.r, got.err)
	}
	files := intervalSubstitutionFiles(t, output)
	var envelope connectedrun.IntervalRun
	if err = json.Unmarshal(files["manifest.json"], &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Summary.State = "incomplete"
	files["manifest.json"], _ = json.Marshal(envelope, json.Deterministic(true))
	files["identity.sha256"] = []byte(artifactdir.Identity(connectedrun.SchemaV2, files) + "\n")
	for _, name := range []string{"manifest.json", "identity.sha256"} {
		if err = os.WriteFile(filepath.Join(output, name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = connectedrun.Open(ctx, output); err == nil {
		t.Fatal("reader allowed uncertain transport downgrade")
	}
}
