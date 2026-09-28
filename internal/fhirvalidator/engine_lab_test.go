package fhirvalidator_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

// labEngine selects the lab container engine through the installed container
// command line, as an administrator selects a local engine. Continuous
// integration always has the command line; a developer machine without one
// skips, which is never an application validation result.
func labEngine(t *testing.T) (*connectedlab.ContainerEngine, *fhirvalidator.Engine) {
	t.Helper()
	lab := connectedlab.StartContainerEngine(t, connectedlab.ValidatorImage)
	engine, err := fhirvalidator.LocalEngine(lab.Socket)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	t.Cleanup(engine.Close)
	return lab, engine
}

// The engine drives an actual worker process over the fixed protocol: the
// container is created with its isolation, verified before start, fed the
// request and heartbeats, and its answer interpreted and retained.
func TestFHIRValidatorWorkerExecutesThroughALocalEngine(t *testing.T) {
	lab, engine := labEngine(t)
	capability, err := fhirvalidator.OpenCapability(connectedlab.StageValidatorCapability(t, t.TempDir(), "capability", ""))
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"resourceType":"Patient","identifier":[{"value":"LAB-ENGINE-77"}],"name":[{"family":"Enginefamily"}]}`)
	request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: capability.Identity(), InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, TimeoutMS: 5000, MaxOutputBytes: 1 << 20}
	raw, _ := json.Marshal(request)
	plan, err := fhirvalidator.Prepare(raw, input, capability)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "validation")
	evidence, err := plan.Execute(t.Context(), engine, output)
	if err != nil {
		t.Fatal(err)
	}
	r := evidence.Result()
	if r.State != "conforms" || r.Verdict != assertion.VerdictPass || r.Engine == nil || r.RuntimeStatus != nil || len(r.Findings) != 1 || !strings.Contains(r.Findings[0].Diagnostics, "LAB-ENGINE-77") {
		t.Fatalf("the worker's answer was not interpreted: %+v", r)
	}
	runs := lab.Runs()
	if len(runs) != 1 || runs[0].InputSHA256 != networkaction.Digest(input) || runs[0].Network != "none" || !runs[0].ReadOnly {
		t.Fatalf("the worker did not run isolated over the exact input: %+v", runs)
	}
	if reopened, err := fhirvalidator.Open(t.Context(), output); err != nil || reopened.Identity() != evidence.Identity() {
		t.Fatal("the retained validation does not reopen offline", err)
	}

	// An engine that does not apply the requested isolation is refused before
	// the worker starts.
	lab.DropIsolation()
	refused, err := plan.Execute(t.Context(), engine, filepath.Join(t.TempDir(), "refused"))
	if err != nil {
		t.Fatal(err)
	}
	if r := refused.Result(); r.RuntimeStatus == nil || r.RuntimeStatus.State != "unsupported-runtime" || r.Verdict != assertion.VerdictUndecided || len(lab.Runs()) != 1 {
		t.Fatalf("a container without network and filesystem isolation ran: %+v", r)
	}
}
