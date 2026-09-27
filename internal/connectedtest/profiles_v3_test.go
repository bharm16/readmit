package connectedtest_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/profileeval"
)

func componentProfilePlan(t *testing.T, family string) (connectedtest.Test, map[string][]byte) {
	t.Helper()
	raw, files := example(t)
	var d connectedtest.Test
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/v3/2.5.1", family, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	d.Steps = nil
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("step-%d", i)
		name := id + ".hl7"
		files[name] = read(name)
		step := connectedtest.Step{ID: id, Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: connectedtest.Reference{Project: "lab", ID: id, File: name, Schema: "hl7", SHA256: connectedtest.Digest(files[name])}, Occurrence: fmt.Sprintf("s0001-e%06d", i)}}
		if i > 1 {
			step.After = []string{fmt.Sprintf("step-%d", i-1)}
		}
		d.Steps = append(d.Steps, step)
	}
	for _, pin := range []struct{ id, schema string }{{"profile", profileeval.ProfileSchemaV3}, {"pack", profileeval.PackSchemaV3}} {
		name := pin.id + ".json"
		files[name] = read(name)
		d.Profiles = append(d.Profiles, connectedtest.Reference{Project: "lab", ID: pin.id, File: name, Schema: pin.schema, SHA256: connectedtest.Digest(files[name])})
	}
	return d, files
}

func compileComponentProfile(t *testing.T, d connectedtest.Test, files map[string][]byte) *connectedtest.Plan {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConnectedComponentProfilesRetainExactRevisions(t *testing.T) {
	for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
		t.Run(family, func(t *testing.T) {
			d, files := componentProfilePlan(t, family)
			p := compileComponentProfile(t, d, files)
			before, err := connectedtest.EvaluateProfiles(t.Context(), p, "profile", "pack", profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if before.Verdict != "pass" || before.Operator != profileeval.ComponentOperatorVersion || before.Profile.Schema != profileeval.ProfileSchemaV3 || before.Pack.Schema != profileeval.PackSchemaV3 {
				t.Fatalf("component pins not evaluated: %+v", before)
			}
			path := filepath.Join(t.TempDir(), "retained")
			if err := p.Write(t.Context(), path); err != nil {
				t.Fatal(err)
			}
			for name, raw := range files {
				clear(raw)
				files[name] = []byte("source replaced")
			}
			reopened, err := connectedtest.OpenPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := connectedtest.EvaluateProfiles(t.Context(), reopened, "profile", "pack", profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			a, err := json.Marshal(before, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(after, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			if p.Identity() != reopened.Identity() || !bytes.Equal(a, b) {
				t.Fatal("source changes altered retained profile evaluation")
			}
		})
	}
}

func TestConnectedComponentProfileRevisionChangesOnlyNewPlan(t *testing.T) {
	d, files := componentProfilePlan(t, "SIU")
	old := compileComponentProfile(t, d, files)
	var revised profileeval.ProfileV3
	if err := json.Unmarshal(files["profile.json"], &revised); err != nil {
		t.Fatal(err)
	}
	revised.Definition.Identity.Version = "2"
	for i := range revised.Datatypes {
		if revised.Datatypes[i].Name == "EI" {
			revised.Datatypes[i].Components[0].MaxLength = 1
		}
	}
	var err error
	files["profile.json"], err = json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	// A source revision never silently advances an existing exact reference.
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("changed profile accepted under old digest")
	}
	d.Revision = "2"
	d.Profiles[0].SHA256 = connectedtest.Digest(files["profile.json"])
	next := compileComponentProfile(t, d, files)
	for _, tc := range []struct {
		p                *connectedtest.Plan
		verdict, version string
	}{{old, "pass", "1"}, {next, "fail", "2"}} {
		report, err := connectedtest.EvaluateProfiles(t.Context(), tc.p, "profile", "pack", profileeval.Options{CompleteCapture: true})
		if err != nil {
			t.Fatal(err)
		}
		if report.Verdict != tc.verdict || report.Profile.Version != tc.version {
			t.Fatalf("wrong explicit revision evaluation: %+v", report)
		}
	}
	if old.Identity() == next.Identity() {
		t.Fatal("new profile revision reused plan identity")
	}
}

func TestConnectedLegacyProfileSchemasKeepTheirOperator(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			d, files := componentProfilePlan(t, "SIU")
			for i, pin := range d.Profiles {
				var envelope map[string]any
				if err := json.Unmarshal(files[pin.File], &envelope); err != nil {
					t.Fatal(err)
				}
				if version == "v1" {
					key := "definition"
					if pin.ID == "pack" {
						key = "metadata"
					}
					envelope = envelope[key].(map[string]any)
				} else {
					delete(envelope, "datatypes")
					if workflows, ok := envelope["workflows"].([]any); ok {
						for _, workflow := range workflows {
							delete(workflow.(map[string]any), "parent_segments")
						}
					}
				}
				schema := "readmit-local-profile/" + version
				if pin.ID == "pack" {
					schema = "readmit-profile-pack/" + version
				}
				envelope["schema"] = schema
				raw, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				files[pin.File] = raw
				d.Profiles[i].Schema = schema
				d.Profiles[i].SHA256 = connectedtest.Digest(raw)
			}
			report, err := connectedtest.EvaluateProfiles(t.Context(), compileComponentProfile(t, d, files), "profile", "pack", profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Operator != profileeval.OperatorVersion {
				t.Fatalf("legacy %s reinterpreted with %s", version, report.Operator)
			}
		})
	}
}
