package report_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/report"
)

const validatedIdentifier = "REVAL-IDENT-5522"

// validatedFlow registers one Patient and validates the server's response to
// it. timeoutMS is the validation's pinned worker deadline.
func validatedFlow(h *connectedlab.Harness, body string, timeoutMS int64) connectedtest.FlowTest {
	patients := h.Observe("register", "patients", "after", "Patient", "identifier=urn%3Areadmit-lab%3Apatient%7C"+validatedIdentifier, "authoritative-application-api",
		connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"),
		connectedlab.IdentityColumn())
	return connectedtest.FlowTest{ID: "validated-registration", Variables: []connectedtest.Variable{{ID: "patient-id", Kind: "response"}},
		Steps: []connectedtest.Step{h.FHIRStep("register", "POST", "Patient", body, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{connectedlab.BindID("patient-id", "logical-id", "phase")})},
		Phases: []connectedtest.FlowPhase{{ID: "register", Steps: []string{"register"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{patients},
			Responses:   []connectedtest.ResponseCheck{{ID: "created", Step: "register", Outcome: "succeeded"}},
			Validations: []connectedtest.ValidationCheck{connectedlab.Validate("patient", "register", timeoutMS)},
			Checks:      h.Checks("register", []connectedtest.Dataset{patients}, connectedlab.RowCount("one", "patients", 1))}}}
}

func validatedPatient(family string) string {
	return `{"resourceType":"Patient","identifier":[{"system":"urn:readmit-lab:patient","value":"` + validatedIdentifier + `"}],"name":[{"family":"` + family + `"}]}`
}

// labValidationEngine starts the lab container engine and reports whether the
// installed container command line can reach it. CI always has one.
func labValidationEngine(t *testing.T) *connectedlab.ContainerEngine {
	t.Helper()
	engine := connectedlab.StartContainerEngine(t, connectedlab.ValidatorImage)
	probe, err := fhirvalidator.LocalEngine(engine.Socket)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	probe.Close()
	return engine
}

func packetFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			raw, _ := os.ReadFile(path)
			rel, _ := filepath.Rel(dir, path)
			out[rel] = sha(raw)
		}
		return nil
	})
	return out
}

func TestConnectedLabRevalidationRunsTheInstalledValidatorOnRetainedBytes(t *testing.T) {
	labLifecycles(t)
	engine := labValidationEngine(t)

	// The historical run had the capability staged but no engine: its
	// validation retained the request and the resource, and no outcome.
	h := connectedlab.New(t, "")
	connectedlab.StageValidatorCapability(t, h.Root, "validator-capability", "")
	h.Validation = &connectedrun.ValidationSelection{Capability: "validator-capability", Engine: "none"}
	h.Compile(validatedFlow(h, validatedPatient("Revalfamily"), 1500))
	r, unvalidated := h.Run("unvalidated")
	if r.State != "complete" || connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "register"), "validation:patient") != assertion.OutcomeUndecided {
		t.Fatalf("the historical validation was not retained undecided: %+v", r)
	}
	// A second historical run validated through the local engine.
	h.Validation = &connectedrun.ValidationSelection{Capability: "validator-capability", Engine: "local", Socket: engine.Socket}
	h.Compile(validatedFlow(h, validatedPatient("Revalfamily"), 1500))
	r, validated := h.Run("validated")
	if connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "register"), "validation:patient") != assertion.OutcomePassed {
		t.Fatalf("the historical validation through the engine did not pass: %+v", r)
	}
	historicalRuns := len(engine.Runs())
	// Every original system is gone before any packet is read.
	h.Lab.Server().Close()
	h.Engine.Listener.Close()

	packet, before := assemble(t, report.ConnectedInput{Current: unvalidated})
	packetPath := packet
	sealedFiles := packetFiles(t, packet)
	// The administrator's installation, staged apart from any packet.
	installed := connectedlab.StageValidatorCapability(t, t.TempDir(), "installed", "")
	historicalInput := ""
	for _, v := range before.Reanalysis[0].Limitations {
		if strings.Contains(v, "validation patient") && !strings.Contains(v, "explicit revalidation") {
			t.Fatal("the reanalysis does not tell reinterpretation from revalidation:", v)
		}
	}
	revalidate := func(t *testing.T, packet string, options report.RevalidationOptions) report.RevalidatedValidation {
		t.Helper()
		out := filepath.Join(t.TempDir(), "revalidation")
		analysis, err := report.RevalidateConnected(t.Context(), packet, options, out)
		if err != nil {
			t.Fatal(err)
		}
		if len(analysis.Manifest.Validations) != 1 {
			t.Fatalf("one declared validation, got %+v", analysis.Manifest.Validations)
		}
		reopened, err := report.OpenConnectedRevalidation(t.Context(), out, packet)
		if err != nil || reopened.Identity != analysis.Identity {
			t.Fatal("the revalidation does not verify offline against its packet", err)
		}
		// The packet and its historical verdict are unchanged.
		if files := packetFiles(t, packet); packet == packetPath && !maps.Equal(files, sealedFiles) {
			t.Fatal("revalidation changed the packet")
		}
		return analysis.Manifest.Validations[0]
	}
	local := func(capability string) report.RevalidationOptions {
		return report.RevalidationOptions{Capability: capability, Engine: "local", Socket: engine.Socket}
	}

	t.Run("the installed worker executes against the retained resource bytes", func(t *testing.T) {
		v := revalidate(t, packet, local(installed))
		if v.Status != report.Revalidated || v.Agreement != report.AgreementNoOutcome || v.Historical == nil || v.Historical.RuntimeState != "worker-missing" || v.Revalidated == nil || v.Revalidated.State != "conforms" || v.Revalidated.Verdict != "pass" {
			t.Fatalf("the retained validation was not run again: %+v %+v", v, v.Revalidated)
		}
		runs := engine.Runs()
		if len(runs) != historicalRuns+1 {
			t.Fatal("no worker executed", len(runs), historicalRuns)
		}
		last := runs[len(runs)-1]
		retained, err := os.ReadFile(filepath.Join(packet, "current", "phases", "register", "validations", "patient", "input.json"))
		if err != nil {
			t.Fatal(err)
		}
		historicalInput = networkaction.Digest(retained)
		if last.InputSHA256 != historicalInput || v.Revalidated.Input != historicalInput || v.Historical.Input != historicalInput || last.Network != "none" || !last.ReadOnly || strings.HasPrefix(last.InputMount, packet) {
			t.Fatalf("the worker did not read exactly the retained bytes through an isolated copy: %+v", last)
		}
		reopened, err := report.OpenConnected(t.Context(), packet)
		if err != nil || reopened.Identity != before.Identity || reopened.Manifest.Current.Verdict != before.Manifest.Current.Verdict {
			t.Fatal("the historical packet or verdict changed", err)
		}
	})

	t.Run("a validation that already ran is repeated and agrees", func(t *testing.T) {
		repeated, _ := assemble(t, report.ConnectedInput{Current: validated})
		v := revalidate(t, repeated, local(installed))
		if v.Status != report.Revalidated || v.Agreement != report.AgreementAgrees || v.Historical.State != "conforms" || len(v.Differences) != 0 {
			t.Fatalf("an unchanged validator over unchanged bytes did not agree: %+v", v)
		}
	})

	t.Run("missing or mismatched dependencies are named and nothing runs", func(t *testing.T) {
		runs := len(engine.Runs())
		for _, tc := range []struct {
			name    string
			options report.RevalidationOptions
			reason  string
		}{
			{"absent capability", report.RevalidationOptions{Engine: "local", Socket: engine.Socket}, "capability-not-installed"},
			{"wrong pins", local(connectedlab.StageValidatorCapability(t, t.TempDir(), "other", "another profile revision")), "capability-mismatch"},
			{"no engine", report.RevalidationOptions{Capability: installed, Engine: "none"}, "worker-missing"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				v := revalidate(t, packet, tc.options)
				if v.Status != report.NotRevalidated || v.Reason != tc.reason || v.Agreement != report.AgreementNotCompare || v.Historical == nil {
					t.Fatalf("got %+v, want %s", v, tc.reason)
				}
			})
		}
		if len(engine.Runs()) != runs {
			t.Fatal("a worker ran without the exact installed dependencies")
		}
		if _, err := report.RevalidateConnected(t.Context(), packet, local(filepath.Join(packet, "current", "phases", "register", "validations", "patient", "capability")), filepath.Join(t.TempDir(), "x")); err == nil {
			t.Fatal("the packet's own capability copy was used as the installed capability")
		}
	})

	t.Run("a timeout, unavailable terminology and missing isolation are never a pass", func(t *testing.T) {
		engine.SetValidator(connectedlab.HangingValidator)
		v := revalidate(t, packet, local(installed))
		if v.Status != report.NotRevalidated || v.Reason != "timed-out" || v.Revalidated == nil || v.Revalidated.Verdict != "undecided" {
			t.Fatalf("a timed-out worker: %+v", v)
		}
		engine.SetValidator(connectedlab.LabValidator("unavailable"))
		v = revalidate(t, packet, local(installed))
		if v.Status != report.Revalidated || v.Revalidated.State != "terminology-unavailable" || v.Revalidated.Verdict != "undecided" {
			t.Fatalf("unavailable terminology: %+v %+v", v, v.Revalidated)
		}
		engine.SetValidator(connectedlab.LabValidator(""))
		runs := len(engine.Runs())
		engine.DropIsolation()
		v = revalidate(t, packet, local(installed))
		if v.Status != report.NotRevalidated || v.Reason != "unsupported-runtime" || len(engine.Runs()) != runs {
			t.Fatalf("a container without networking disabled ran: %+v", v)
		}
	})

	t.Run("the analysis refuses a changed claim or another packet", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "revalidation")
		if _, err := report.RevalidateConnected(t.Context(), packet, report.RevalidationOptions{Capability: installed, Engine: "none"}, out); err != nil {
			t.Fatal(err)
		}
		other, _ := assemble(t, report.ConnectedInput{Current: validated})
		if _, err := report.OpenConnectedRevalidation(t.Context(), out, other); err == nil {
			t.Fatal("a revalidation verified against a packet it does not name")
		}
		raw, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
		changed := bytes.Replace(raw, []byte(`"reason":"worker-missing"`), []byte(`"reason":"capability-mismatch"`), 1)
		_ = os.WriteFile(filepath.Join(out, "manifest.json"), changed, 0o600)
		_ = os.WriteFile(filepath.Join(out, "identity.sha256"), []byte(sha(changed)+"\n"), 0o600)
		if _, err := report.OpenConnectedRevalidation(t.Context(), out, packet); err == nil {
			t.Fatal("a rewritten revalidation claim verified")
		}
	})

	t.Run("the commands revalidate and verify without printing values", func(t *testing.T) {
		engine.SetValidator(connectedlab.LabValidator(""))
		root := t.TempDir()
		var stdout, stderr bytes.Buffer
		out := filepath.Join(root, "analysis")
		err := cli.Execute("report", []string{"report", "connected", "revalidate", packet, "--capability", installed, "--output", out, "--engine", "none"}, &stdout, &stderr)
		if err != nil || !strings.Contains(stdout.String(), "not-revalidated (worker-missing)") {
			t.Fatal(err, stdout.String(), stderr.String())
		}
		stdout.Reset()
		if err := cli.Execute("report", []string{"report", "connected", "revalidation", out, packet}, &stdout, &stderr); err != nil {
			t.Fatal(err, stderr.String())
		}
		if s := stdout.String(); strings.Contains(s, validatedIdentifier) || strings.Contains(s, "Revalfamily") || strings.Contains(s, packet) || !strings.Contains(s, "Historical verdicts unchanged") {
			t.Fatal("the revalidation output carries a value or path:", s)
		}
	})
}
