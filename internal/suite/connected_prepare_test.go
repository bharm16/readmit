package suite_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

func connectedSuiteFixture(t *testing.T) (*connectedlab.Harness, string) {
	t.Helper()
	h := connectedlab.New(t, "")
	h.Lab.SetDownstream("encounter")
	rows := h.Observe("book", "appointments", "after", "Appointment", "identifier=urn%3Areadmit-lab%3Aappointment%7CSUITE-1", "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"))
	encounters := h.Observe("book", "encounters", "after", "Encounter", "identifier=urn%3Areadmit-lab%3Aappointment%7CSUITE-1", "authoritative-application-api", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"))
	datasets := []connectedtest.Dataset{rows, encounters}
	h.Compile(connectedtest.FlowTest{ID: "booking", Steps: []connectedtest.Step{h.FHIRStep("create", "POST", "Appointment", `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"SUITE-1"}],"status":"booked","start":"2026-05-01T09:00:00Z","participant":[{"status":"accepted"}]}`, connectedtest.FHIRHeaders{}, nil)}, Phases: []connectedtest.FlowPhase{{ID: "book", Steps: []string{"create"}, Datasets: datasets, Checks: h.Checks("book", datasets, connectedlab.RowCount("one", "appointments", 1), connectedlab.RowCount("one-encounter", "encounters", 1))}}})
	review, err := expectation.ReviewConnected(h.PlanPath)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.ApproveConnected(h.PlanPath, "", review.Identity(), "Reviewer", "Independent appointment count", filepath.Join(h.Root, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc := suite.ConnectedDocument{Schema: suite.ConnectedSchema, ID: "regression", Owner: "interop", Tags: []string{}, Parallelism: 2,
		Tests:        []suite.ConnectedTest{{ID: "booking", Revision: "1", Definition: review.Definition, Release: "release.json", ReleaseIdentity: release.Identity(), After: []string{}, State: "enabled"}},
		Environments: []suite.ConnectedEnvironment{{ID: "qa", Bindings: []suite.ConnectedBinding{{Test: "booking", Plan: "flow-plan", PlanIdentity: h.Plan.Identity(), Config: "flow-config.json"}}}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.Root, "suite.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return h, path
}

func copyConnectedProof(source, destination string) error {
	return filepath.WalkDir(source, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		out := filepath.Join(destination, relative)
		if d.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(out, raw, 0600)
	})
}

func changeConnectedProofEngine(t *testing.T, path string) connectedrun.FlowResult {
	t.Helper()
	for _, name := range []string{"manifest.json", "started.json"} {
		file := filepath.Join(path, name)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var record connectedrun.FlowResult
		if err = json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		record.Engine = "different-original-engine"
		raw, err = json.Marshal(record, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	resealConnectedTree(t, path, connectedrun.FlowSchemaV4)
	actual, err := connectedrun.OpenFlow(t.Context(), path)
	if err != nil || actual.Engine != "different-original-engine" {
		t.Fatal("standalone reader did not preserve a structurally valid original engine", err)
	}
	return actual
}

func TestConnectedCompletionErrorCannotPassOrStartDependentJobs(t *testing.T) {
	h, _ := connectedSuiteFixture(t)
	prepared, err := connectedrun.PrepareFlow(h.PlanPath, filepath.Join(h.Root, "flow-config.json"), "metadata")
	if err != nil {
		t.Fatal(err)
	}
	input, err := prepared.InputIdentity()
	if err != nil {
		t.Fatal(err)
	}
	job := runqueue.ConnectedJob{ID: "booking", Plan: "flow-plan", PlanIdentity: h.Plan.Identity(), Config: filepath.Join(h.Root, "flow-config.json"), Input: input, After: []string{}, State: "enabled"}
	dependent := job
	dependent.ID, dependent.After = "dependent", []string{"booking"}
	raw, err := json.Marshal(runqueue.ConnectedPlan{Schema: runqueue.ConnectedPlanSchema, Parallelism: 2, Jobs: []runqueue.ConnectedJob{job, dependent}})
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	var calls atomic.Int32
	report, err := runqueue.RunConnected(t.Context(), runqueue.ConnectedRequest{PlanBytes: raw, PlanDirectory: h.Root, Runs: output, Instance: "late-completion-error", Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, destination string) (connectedrun.FlowResult, error) {
		calls.Add(1)
		for name, binding := range p.Bindings() {
			connectedlab.WriteJSON(t, filepath.Join(h.Root, connectedlab.GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
		}
		actual, err := connectedrun.ExecuteFlow(ctx, p, destination, testisolation.Confirmation{})
		if err != nil {
			return actual, err
		}
		// The public dependency reports a late completion-write error after
		// genuine effects and readable proof. That error must stay authoritative.
		return actual, errors.New("late completion write failed")
	}})
	actual, openErr := connectedrun.OpenFlow(t.Context(), filepath.Join(output, "booking"))
	if openErr != nil || actual.State != "complete" || actual.Verdict != "pass" || h.Lab.Creates.Load() != 1 {
		t.Fatal("the genuine child did not establish the independent passing oracle", actual, openErr)
	}
	if err != nil || report.ExitCode() != 2 || report.Executed != 1 || report.Skipped != 1 || calls.Load() != 1 || !report.Jobs[0].ExecutionError || report.Jobs[0].Flow == nil || report.Jobs[1].Flow != nil {
		t.Fatalf("completion error became a pass or started a dependency: %+v %v calls=%d", report, err, calls.Load())
	}
	if _, err = os.Stat(filepath.Join(output, "dependent")); !os.IsNotExist(err) {
		t.Fatal("a dependent job created execution evidence after the completion error")
	}
}

func TestConnectedSuiteUsesTheRetainedLifecycleOracleAndReopensOffline(t *testing.T) {
	h, path := connectedSuiteFixture(t)
	for index, mode := range []string{"", "encounter", ""} {
		h.Lab.SetDownstream(mode)
		output := filepath.Join(h.Root, "execution-"+string(rune('a'+index)))
		report, err := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: output, Instance: "suite-occurrence-" + string(rune('a'+index)), Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, destination string) (connectedrun.FlowResult, error) {
			// These are the fixture operator's fresh exact grants, not consent
			// restored from a prior run or a fabricated job record.
			for name, binding := range p.Bindings() {
				connectedlab.WriteJSON(t, filepath.Join(h.Root, connectedlab.GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
			}
			return connectedrun.ExecuteFlow(ctx, p, destination, testisolation.Confirmation{})
		}})
		want := 1
		if mode == "encounter" {
			want = 0
		}
		if err != nil || report.ExitCode() != want || report.Executed != 1 || report.Jobs[0].Flow == nil {
			if len(report.Jobs) > 0 && report.Jobs[0].Flow != nil {
				t.Logf("actual child: %+v", *report.Jobs[0].Flow)
			}
			for _, dataset := range []string{"appointments", "encounters"} {
				interval, readErr := os.ReadFile(filepath.Join(output, "runs", "booking", "phases", "book", "intervals", dataset, "manifest.json"))
				if readErr == nil {
					var diagnostic struct {
						State  string `json:"state"`
						Reason string `json:"reason"`
					}
					_ = json.Unmarshal(interval, &diagnostic)
					t.Logf("interval %s: %+v", dataset, diagnostic)
				}
			}
			t.Fatalf("literal oracle expected exit %d in mode %q: %+v %v", want, mode, report, err)
		}
		opened, err := suite.OpenConnectedExecution(t.Context(), output)
		if err != nil || opened.Report.ExitCode() != want {
			t.Fatal("the retained suite did not reproduce its verdict", err)
		}
		if h.Lab.Creates.Load() != 1 {
			t.Fatal("the independent target did not witness exactly one stimulus in this occurrence")
		}
	}
	// A callback can copy a real old passing child, but a new occurrence must
	// not count that evidence as its own actual execution.
	prior := filepath.Join(h.Root, "execution-b", "runs", "booking")
	borrowed := filepath.Join(h.Root, "borrowed-execution")
	report, err := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: borrowed, Instance: "borrowed-occurrence", Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, out string) (connectedrun.FlowResult, error) {
		if e := copyConnectedProof(prior, out); e != nil {
			return connectedrun.FlowResult{}, e
		}
		return connectedrun.OpenFlow(ctx, out)
	}})
	if err == nil && report.ExitCode() == 0 {
		t.Fatal("a new occurrence accepted transplanted prior passing child proof")
	}
	// A compatible current reader preserves the original engine separately
	// from its current reanalysis. The suite must still require its exact pin.
	report, err = suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(h.Root, "borrowed-engine"), Instance: "suite-occurrence-b", Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, out string) (connectedrun.FlowResult, error) {
		if e := copyConnectedProof(prior, out); e != nil {
			return connectedrun.FlowResult{}, e
		}
		return changeConnectedProofEngine(t, out), nil
	}})
	if err == nil && report.ExitCode() == 0 {
		t.Fatal("queue admission accepted a child from a different original engine")
	}
	engineParent := filepath.Join(h.Root, "changed-engine-parent")
	if e := copyConnectedProof(filepath.Join(h.Root, "execution-b"), engineParent); e != nil {
		t.Fatal(e)
	}
	changed := changeConnectedProofEngine(t, filepath.Join(engineParent, "runs", "booking"))
	engineManifest := filepath.Join(engineParent, "manifest.json")
	raw, e := os.ReadFile(engineManifest)
	if e != nil {
		t.Fatal(e)
	}
	var engineEnvelope map[string]any
	if e = json.Unmarshal(raw, &engineEnvelope); e != nil {
		t.Fatal(e)
	}
	engineEnvelope["report"].(map[string]any)["jobs"].([]any)[0].(map[string]any)["flow"] = changed
	raw, e = json.Marshal(engineEnvelope, json.Deterministic(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(engineManifest, raw, 0600); e != nil {
		t.Fatal(e)
	}
	resealConnectedTree(t, engineParent, suite.ConnectedExecutionSchema)
	if _, e = suite.OpenConnectedExecution(t.Context(), engineParent); e == nil {
		t.Fatal("passive parent inspection accepted a different original engine")
	}
	// Resealing an outer parent around its unchanged real child must likewise
	// refuse a claimed different occurrence during passive inspection.
	manifestPath := filepath.Join(h.Root, "execution-b", "manifest.json")
	raw, e = os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal(e)
	}
	var manifest map[string]any
	if e = json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	manifest["instance"] = "different-occurrence"
	encoded, e := json.Marshal(manifest, json.Deterministic(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(manifestPath, encoded, 0600); e != nil {
		t.Fatal(e)
	}
	resealConnectedTree(t, filepath.Join(h.Root, "execution-b"), suite.ConnectedExecutionSchema)
	if _, e = suite.OpenConnectedExecution(t.Context(), filepath.Join(h.Root, "execution-b")); e == nil {
		t.Fatal("resealed parent attributed a child to a different occurrence")
	}
	h.Lab.Server().Close()
	if _, err := suite.OpenConnectedExecution(t.Context(), filepath.Join(h.Root, "execution-a")); err != nil {
		t.Fatal("offline inspection required a target", err)
	}
}

func TestConnectedSuitePreparationPinsTheWholeApprovedExpansionWithoutEffects(t *testing.T) {
	h, path := connectedSuiteFixture(t)
	prepared, err := suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(h.Root, "prepared")})
	if err != nil || len(prepared.Queue.Jobs) != 1 || prepared.Queue.Jobs[0].PlanIdentity != h.Plan.Identity() {
		t.Fatalf("prepared: %+v %v", prepared, err)
	}
	if h.Lab.Creates.Load() != 0 {
		t.Fatal("preparing a connected suite sent a stimulus")
	}
	if _, err := os.Stat(filepath.Join(prepared.Directory, "plans", "booking", "identity.sha256")); err != nil {
		t.Fatal("the prepared suite did not retain its exact plan", err)
	}
	// Replacing even the final job's pinned plan refuses the entire expansion
	// before publishing a configuration or sending any earlier job.
	raw, _ := os.ReadFile(path)
	var doc suite.ConnectedDocument
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Environments[0].Bindings[0].PlanIdentity = doc.Tests[0].Definition
	raw, _ = json.Marshal(doc)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(h.Root, "refused")
	if _, err = suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: "qa", Output: out}); err == nil {
		t.Fatal("changed plan inherited the reviewed binding")
	}
	if _, err = os.Stat(out); !os.IsNotExist(err) || h.Lab.Creates.Load() != 0 {
		t.Fatal("refused preparation published or contacted a target", err)
	}
}
