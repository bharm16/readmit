package fhirvalidator_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

type liveCase struct {
	Input     string   `json:"input"`
	Profiles  []string `json:"profiles"`
	Expected  string   `json:"expected"`
	Evaluated string   `json:"expected_if_local_terminology_evaluated"`
	Because   string   `json:"because"`
}

// liveCapability opens the capability the administrator's build staged. The
// opt-in qualification invokes the real worker; a skipped developer test is
// not an application validation result.
func liveCapability(t *testing.T) (*fhirvalidator.Capability, fhirvalidator.Manifest) {
	t.Helper()
	directory := os.Getenv("READMIT_FHIR_VALIDATOR_CAPABILITY")
	if directory == "" {
		t.Skip("explicit local capability qualification not requested")
	}
	capability, err := fhirvalidator.OpenCapability(directory)
	if err != nil {
		t.Fatal("open staged capability", err)
	}
	return capability, capability.Manifest()
}

func liveRequest(t *testing.T, capability *fhirvalidator.Capability, m fhirvalidator.Manifest, input []byte, profiles []string) fhirvalidator.Request {
	t.Helper()
	request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: capability.Identity(), InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, TimeoutMS: 120000, MaxOutputBytes: 4 << 20}
	for _, name := range profiles {
		url, version, _ := strings.Cut(name, "|")
		found := false
		for _, p := range m.Profiles {
			if p.URL == url && p.Version == version {
				request.Profiles, found = append(request.Profiles, p), true
			}
		}
		if !found {
			// A canonical the administrator never staged is still requested
			// by exact identity, which preparation must refuse.
			request.Profiles = append(request.Profiles, fhirvalidator.Canonical{URL: url, Version: version, SHA256: networkaction.Digest([]byte(name)), Package: fhirvalidator.PackageRef{ID: "readmit.synthetic.validation", Version: "1.0.0"}})
		}
	}
	return request
}

func liveEngine(t *testing.T) *fhirvalidator.Engine {
	t.Helper()
	engine, err := fhirvalidator.LocalEngine("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close)
	return engine
}

// semantic is what a repeat run over the same pinned inputs must reproduce:
// everything except the worker's private diagnostic stream digest.
func semantic(r fhirvalidator.Result) []byte {
	r.Worker.DiagnosticSHA256 = ""
	raw, _ := json.Marshal(r, json.Deterministic(true))
	return raw
}

// TestFHIRValidatorLiveQualification runs every case of the independently
// authored oracle through the real worker twice, then the runtime failures
// the oracle cannot author: timeout, output limit, a killed worker and a
// missing image. READMIT_FHIR_VALIDATOR_RECORD names a folder that receives
// each case's actual OperationOutcome for the offline policy tests.
func TestFHIRValidatorLiveQualification(t *testing.T) {
	capability, m := liveCapability(t)
	engine := liveEngine(t)
	raw, err := os.ReadFile("../../testdata/fhir-validation/expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Cases []liveCase `json:"cases"`
	}
	if err = json.Unmarshal(raw, &oracle, json.RejectUnknownMembers(false)); err != nil {
		t.Fatal(err)
	}
	record := os.Getenv("READMIT_FHIR_VALIDATOR_RECORD")
	type cell struct {
		Case          string            `json:"case"`
		State         string            `json:"state"`
		Verdict       assertion.Verdict `json:"verdict"`
		Findings      int               `json:"findings"`
		OutcomeSHA256 string            `json:"outcome_sha256,omitzero"`
		RepeatStable  bool              `json:"repeat_stable,omitzero"`
	}
	var cells []cell
	engineVersion := ""
	defer func() {
		if record == "" || t.Failed() {
			return
		}
		receipt := map[string]any{"schema": "readmit-fhir-validator-live/v1", "engine": engineVersion, "capability": capability.Identity(), "image": m.Image, "platform": m.Platform, "validator": m.Validator.Version, "runtime": m.Runtime.Version, "policy": fhirvalidator.Policy, "packages": m.ValidatorPackages, "cells": cells}
		raw, _ := json.Marshal(receipt, json.Deterministic(true))
		if err := os.WriteFile(filepath.Join(record, "..", "qualification", "live-"+strings.ReplaceAll(m.Platform, "/", "-")+".json"), append(raw, '\n'), 0o644); err != nil {
			t.Error(err)
		}
	}()
	for _, c := range oracle.Cases {
		name := strings.TrimSuffix(c.Input, ".json")
		if len(c.Profiles) > 0 {
			name += "+" + strings.TrimSuffix(filepath.Base(strings.Split(c.Profiles[0], "|")[0]), ".json")
		}
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("../../testdata/fhir-validation", c.Input))
			if err != nil {
				t.Fatal(err)
			}
			request := liveRequest(t, capability, m, input, c.Profiles)
			rawRequest, _ := json.Marshal(request)
			plan, err := engine.PrepareInstalled(t.Context(), rawRequest, input, capability)
			if err != nil {
				var status fhirvalidator.Status
				if !errors.As(err, &status) || status.State != c.Expected {
					t.Fatalf("preparation %v, oracle %q", err, c.Expected)
				}
				cells = append(cells, cell{Case: name, State: status.State, Verdict: assertion.VerdictUndecided, RepeatStable: true})
				return
			}
			var first []byte
			for run := range 2 {
				output := filepath.Join(t.TempDir(), "result")
				evidence, err := plan.Execute(t.Context(), engine, output)
				if err != nil {
					t.Fatal("execute", err)
				}
				reopened, err := fhirvalidator.Open(t.Context(), output)
				if err != nil || reopened.Identity() != evidence.Identity() {
					t.Fatal("offline reopen", err)
				}
				result := reopened.Result()
				check, err := connectedtest.ReadFHIRValidationCheck(t.Context(), output, evidence.Identity(), request.InputSHA256, "fhir-validation")
				if err != nil || check.Result.Outcome != map[assertion.Verdict]assertion.Outcome{assertion.VerdictPass: assertion.OutcomePassed, assertion.VerdictFail: assertion.OutcomeFailed, assertion.VerdictUndecided: assertion.OutcomeUndecided}[result.Verdict] {
					t.Fatal("connected check", check, err)
				}
				// The pinned worker evaluates the synthetic package's
				// complete local CodeSystem, so its evaluated oracle applies.
				expected := c.Expected
				if expected == "" {
					expected = c.Evaluated
				}
				if result.Engine == nil || result.Engine.Version == "" {
					t.Fatal("the engine that ran the worker is not retained")
				}
				engineVersion = result.Engine.Version
				if result.State != expected {
					t.Fatalf("run %d: state %s verdict %s findings %d, oracle %q", run, result.State, result.Verdict, len(result.Findings), expected)
				}
				if run == 0 {
					first = semantic(result)
					if record != "" {
						if err := os.WriteFile(filepath.Join(record, name+".json"), reopened.OutcomeBytes(), 0o644); err != nil {
							t.Fatal(err)
						}
					}
				} else if !bytes.Equal(first, semantic(result)) {
					t.Fatalf("repeat run changed semantic findings:\n%s\n%s", first, semantic(result))
				} else {
					cells = append(cells, cell{Case: name, State: result.State, Verdict: result.Verdict, Findings: len(result.Findings), OutcomeSHA256: result.Worker.OutcomeSHA256, RepeatStable: true})
				}
				t.Logf("run %d: state=%s verdict=%s findings=%d identity=%s", run, result.State, result.Verdict, len(result.Findings), reopened.Identity())
			}
		})
	}
	valid, err := os.ReadFile("../../testdata/fhir-validation/patient-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	profile := []string{"https://readmit.example/fhir/StructureDefinition/required-patient|1.0.0"}
	// started waits until the worker container exists and has begun.
	started := func() string {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			out, _ := liveDocker(t, "ps", "--quiet", "--filter", "label=readmit.validator.job")
			if id := strings.TrimSpace(string(out)); id != "" {
				time.Sleep(time.Second)
				return id
			}
			time.Sleep(100 * time.Millisecond)
		}
		return ""
	}
	runtimeCase := func(t *testing.T, edit func(*fhirvalidator.Request), during func(context.CancelFunc)) fhirvalidator.Result {
		t.Helper()
		request := liveRequest(t, capability, m, valid, profile)
		edit(&request)
		rawRequest, _ := json.Marshal(request)
		plan, err := engine.PrepareInstalled(t.Context(), rawRequest, valid, capability)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if during != nil {
			go during(cancel)
		}
		output := filepath.Join(t.TempDir(), "result")
		evidence, err := plan.Execute(ctx, engine, output)
		if err != nil {
			t.Fatal("execute", err)
		}
		if _, err = fhirvalidator.Open(t.Context(), output); err != nil {
			t.Fatal("offline reopen", err)
		}
		if evidence.Result().Verdict != assertion.VerdictUndecided {
			t.Fatalf("runtime failure decided a verdict: %v", evidence.Result())
		}
		cells = append(cells, cell{Case: t.Name()[len("TestFHIRValidatorLiveQualification/"):], State: evidence.Result().State, Verdict: evidence.Result().Verdict})
		return evidence.Result()
	}
	t.Run("timeout", func(t *testing.T) {
		if r := runtimeCase(t, func(r *fhirvalidator.Request) { r.TimeoutMS = 1500 }, nil); r.State != "timed-out" {
			t.Fatal(r.State)
		}
	})
	t.Run("output limit", func(t *testing.T) {
		if r := runtimeCase(t, func(r *fhirvalidator.Request) { r.MaxOutputBytes = 1024 }, nil); r.State != "output-limit" {
			t.Fatal(r.State)
		}
	})
	t.Run("killed worker", func(t *testing.T) {
		kill := func(context.CancelFunc) {
			if id := started(); id != "" {
				_, _ = liveDocker(t, "kill", id)
			}
		}
		if r := runtimeCase(t, func(*fhirvalidator.Request) {}, kill); r.State != "worker-crashed" {
			t.Fatal(r.State)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		stop := func(cancel context.CancelFunc) {
			started()
			cancel()
		}
		if r := runtimeCase(t, func(*fhirvalidator.Request) {}, stop); r.State != "cancelled" {
			t.Fatal(r.State)
		}
		if out, _ := liveDocker(t, "ps", "--all", "--quiet", "--filter", "label=readmit.validator.job"); strings.TrimSpace(string(out)) != "" {
			t.Fatal("a cancelled worker container remains")
		}
	})
	t.Run("missing image", func(t *testing.T) {
		m, assets := m, map[string][]byte{}
		for _, a := range m.Assets {
			assets[a.Path], _ = os.ReadFile(filepath.Join(os.Getenv("READMIT_FHIR_VALIDATOR_CAPABILITY"), filepath.FromSlash(a.Path)))
		}
		m.Image = "sha256:" + networkaction.Digest([]byte("never staged"))
		absent, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "absent"), m, assets)
		if err != nil {
			t.Fatal(err)
		}
		request := liveRequest(t, absent, m, valid, profile)
		rawRequest, _ := json.Marshal(request)
		_, err = engine.PrepareInstalled(t.Context(), rawRequest, valid, absent)
		var status fhirvalidator.Status
		if !errors.As(err, &status) || status.State != "worker-missing" || status.Requirement == "" {
			t.Fatalf("missing image was not an actionable requirement: %v", err)
		}
		cells = append(cells, cell{Case: "missing_image", State: status.State, Verdict: assertion.VerdictUndecided})
	})
}

// liveDocker reaches the same local engine socket LocalEngine selects.
func liveDocker(t *testing.T, args ...string) ([]byte, error) {
	socket := "/var/run/docker.sock"
	if _, err := os.Stat(socket); err != nil {
		home, _ := os.UserHomeDir()
		socket = filepath.Join(home, ".docker/run/docker.sock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", append([]string{"--host", "unix://" + socket}, args...)...).Output()
}
