package cli

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/profileeval"
)

func retainedComponentCLIInputs(t *testing.T, family string, steps []int) (string, string, string, string) {
	t.Helper()
	return retainedProfileCLIInputs(t, family, steps, profileeval.PackSchemaV3, func(_ string, b []byte) []byte { return b })
}

// retainedProfileCLIInputs pins the owned fixture's profile and a pack of the
// given schema, after edit republishes either document.
func retainedProfileCLIInputs(t *testing.T, family string, steps []int, packSchema string, edit func(name string, b []byte) []byte) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{"checks.json": []byte(`{"schema":"readmit-assertion-set/v1","name":"independent ACK check","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"s0001-e000001","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`)}
	ref := func(id, schema, file string) connectedtest.Reference {
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: file, SHA256: connectedtest.Digest(files[file])}
	}
	d := connectedtest.Test{Schema: connectedtest.TestSchema, Project: "lab", ID: "profiles", Revision: "1", Environment: connectedtest.Environment{Project: "lab", ID: "fixture", Revision: "1", Name: "Offline fixture", Classification: "test", Endpoint: "receiver", TargetIdentity: connectedtest.Digest([]byte("target")), AddressPolicyIdentity: connectedtest.Digest([]byte("policy")), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Value: "fixture-v1", Provenance: "operator-declared"}}, Checks: ref("checks", "readmit-assertion-set/v1", "checks.json"), Datasets: []connectedtest.Dataset{{ID: "acks", Kind: "v2-messages", Phase: "after", Source: "legacy-ack", Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: 1000, MaxRecords: 10, MaxBytes: 65536}}}, Bindings: connectedtest.Bindings{Observed: "acks"}, Setup: connectedtest.Setup{Kind: "operator-declared", Isolation: "dedicated-fixture", Instructions: "Offline profile checks"}, Limits: connectedtest.Limits{MaxSteps: 10, MaxBytes: 1048576, DeadlineMS: 5000}, OperatorVersion: connectedtest.OperatorVersion}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/v3/2.5.1", family, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, pin := range []struct{ id, schema string }{{"profile", profileeval.ProfileSchemaV3}, {"pack", packSchema}} {
		name := pin.id + ".json"
		files[name] = edit(name, read(name))
		d.Profiles = append(d.Profiles, ref(pin.id, pin.schema, name))
		if err := os.WriteFile(filepath.Join(root, name), files[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	var inputs []bundle.Input
	for i, n := range steps {
		id := fmt.Sprintf("step-%d", n)
		name := id + ".hl7"
		files[name] = read(name)
		inputs = append(inputs, bundle.Input{Path: name, Data: files[name]})
		step := connectedtest.Step{ID: id, Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: ref(id, "hl7", name), Occurrence: fmt.Sprintf("s%04d-e000001", i+1)}}
		if i > 0 {
			step.After = []string{d.Steps[i-1].ID}
		}
		d.Steps = append(d.Steps, step)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(root, "plan")
	if err := p.Write(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(root, "case")
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := bundle.Write(casePath, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &when}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "profile.json"), filepath.Join(root, "pack.json"), casePath, plan
}

func TestComponentProfileCommandsUseTheSharedEvaluator(t *testing.T) {
	for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
		t.Run(family, func(t *testing.T) {
			profile, pack, casePath, plan := retainedComponentCLIInputs(t, family, []int{1, 2, 3})
			var canonical []byte
			for _, args := range [][]string{{"profile", "evaluate", profile, pack, casePath, "--complete-capture"}, {"diagnose", "profile", profile, pack, casePath, "--complete-capture"}, {"connected", "profile-checks", plan, "profile", "pack", "--complete-capture"}} {
				out, diagnostics, err := runInProcess(t, args...)
				if err != nil {
					t.Fatalf("%s: %v, %s", args[0], err, diagnostics.String())
				}
				var report profileeval.Report
				if err := json.Unmarshal(out.Bytes(), &report, json.RejectUnknownMembers(true)); err != nil {
					t.Fatal(err)
				}
				if report.Verdict != "pass" || report.Operator != profileeval.ComponentOperatorVersion || report.Profile.Schema != profileeval.ProfileSchemaV3 || report.Pack.Schema != profileeval.PackSchemaV3 {
					t.Fatalf("lost v3 semantics: %+v", report)
				}
				report.CaseIdentity = "" // A connected plan binds inputs, not the imported case wrapper.
				raw, err := json.Marshal(report, json.Deterministic(true))
				if err != nil {
					t.Fatal(err)
				}
				if canonical == nil {
					canonical = raw
				} else if !bytes.Equal(canonical, raw) {
					t.Fatalf("%s did not use identical profile evaluation", args[0])
				}
			}
			// A retained plan's check stays available after the selected source files disappear.
			for _, path := range []string{profile, pack} {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			out, _, err := runInProcess(t, "connected", "profile-checks", plan, "profile", "pack", "--complete-capture")
			if err != nil {
				t.Fatal(err)
			}
			var report profileeval.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(report, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, raw) {
				t.Fatal("retained command depends on original profile files")
			}
		})
	}
}

func TestComponentProfileCommandsDistinguishCaptureGapsFromFailures(t *testing.T) {
	profile, pack, casePath, plan := retainedComponentCLIInputs(t, "SIU", []int{1, 3})
	for _, base := range [][]string{{"profile", "evaluate", profile, pack, casePath}, {"diagnose", "profile", profile, pack, casePath}, {"connected", "profile-checks", plan, "profile", "pack"}} {
		for _, tc := range []struct {
			complete bool
			code     int
			verdict  string
		}{{false, 2, "undecided"}, {true, 1, "fail"}} {
			args := append([]string(nil), base...)
			if tc.complete {
				args = append(args, "--complete-capture")
			}
			out, _, err := runInProcess(t, args...)
			if ExitCode(err) != tc.code {
				t.Fatalf("%s exit=%d want%d: %v", base[0], ExitCode(err), tc.code, err)
			}
			var report profileeval.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Verdict != tc.verdict || report.CompleteCapture != tc.complete || report.Operator != profileeval.ComponentOperatorVersion {
				t.Fatalf("wrong capture decision: %+v", report)
			}
		}
	}
}

// A pack/v4 choice reaches every profile command through the same evaluator
// and names evaluator v3; the earlier pins keep their own operators.
func TestChoicePackProfileCommandsUseTheSharedEvaluator(t *testing.T) {
	edit := func(name string, b []byte) []byte {
		var v map[string]any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		if name == "profile.json" {
			v["definition"].(map[string]any)["base"].(map[string]any)["pack"].(map[string]any)["version"] = "2"
		} else {
			v["schema"] = profileeval.PackSchemaV4
			v["metadata"].(map[string]any)["pack"].(map[string]any)["version"] = "2"
			orders := v["messages"].([]any)[0].(map[string]any)["sequence"].([]any)[1].(map[string]any)
			children := orders["children"].([]any)
			orders["children"] = []any{children[0], map[string]any{"name": "kind", "min": 1, "max": "1", "choice": true, "children": []any{children[1],
				map[string]any{"name": "requisition", "segment": "RQD", "min": 1, "max": "1"}}}}
		}
		out, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	profile, pack, casePath, plan := retainedProfileCLIInputs(t, "ORM", []int{1, 2, 3}, profileeval.PackSchemaV4, edit)
	var canonical []byte
	for _, args := range [][]string{{"profile", "evaluate", profile, pack, casePath, "--complete-capture"}, {"diagnose", "profile", profile, pack, casePath, "--complete-capture"}, {"connected", "profile-checks", plan, "profile", "pack", "--complete-capture"}} {
		out, diagnostics, err := runInProcess(t, args...)
		if err != nil {
			t.Fatalf("%s: %v, %s", args[0], err, diagnostics.String())
		}
		var report profileeval.Report
		if err := json.Unmarshal(out.Bytes(), &report, json.RejectUnknownMembers(true)); err != nil {
			t.Fatal(err)
		}
		if report.Verdict != "pass" || report.Operator != profileeval.ChoiceOperatorVersion || report.Pack.Schema != profileeval.PackSchemaV4 || report.Pack.Version != "2" {
			t.Fatalf("lost v4 semantics: %+v", report)
		}
		report.CaseIdentity = ""
		raw, err := json.Marshal(report, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if canonical == nil {
			canonical = raw
		} else if !bytes.Equal(canonical, raw) {
			t.Fatalf("%s did not use identical profile evaluation", args[0])
		}
	}
}
