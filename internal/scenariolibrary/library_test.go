package scenariolibrary_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandAuthoredLibrary(t *testing.T) {
	library, err := os.ReadFile("../../testdata/fixtures/scenario-library.json")
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := os.ReadFile("../../testdata/fixtures/scenario-expectations.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := scenariolibrary.Check(context.Background(), library, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Streams != 2 || result.Target != "unverified" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func fixtures(t *testing.T) ([]byte, []byte) {
	t.Helper()
	l, e := os.ReadFile("../../testdata/fixtures/scenario-library.json")
	if e != nil {
		t.Fatal(e)
	}
	o, e := os.ReadFile("../../testdata/fixtures/scenario-expectations.json")
	if e != nil {
		t.Fatal(e)
	}
	return l, o
}
func TestOracleDisagreementAndIncompleteCoverageRefuse(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"wrong field", "5349555e533132", "5349555e533133"},
		{"wrong lifecycle", `"to": "booked"`, `"to": "cancelled"`},
		{"wrong state", `"state": "omitted"`, `"state": "empty"`},
		{"wrong template version", `"template_version": "1"`, `"template_version": "2"`},
		{"wrong arrival", `"after": "1m0s"`, `"after": "2m0s"`},
		{"missing required false", `"duplicate": false,`, ``},
		{"unknown nested", `"duplicate": false,`, `"duplicate": false, "extra": true,`},
		{"null nested", `"fields": [`, `"fields": [null,`},
		{"null lifecycle", `"lifecycle": [`, `"lifecycle": [null,`},
		{"null string", `"provenance": "Readmit-authored`, `"provenance": null, "extra": "Readmit-authored`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, o := fixtures(t)
			changed := bytes.Replace(o, []byte(tc.old), []byte(tc.new), 1)
			if bytes.Equal(changed, o) {
				t.Fatal("mutation did not apply")
			}
			if _, err := scenariolibrary.Check(context.Background(), l, changed); err == nil {
				t.Fatal("accepted invalid oracle")
			}
		})
	}
	l, o := fixtures(t)
	var e scenariolibrary.Expectations
	if err := json.Unmarshal(o, &e); err != nil {
		t.Fatal(err)
	}
	e.Streams = e.Streams[:1]
	o, _ = json.Marshal(e)
	if _, err := scenariolibrary.Check(context.Background(), l, o); err == nil {
		t.Fatal("accepted partial streams")
	}
}
func TestPinsPrivacyCancellationAndIndependentVersions(t *testing.T) {
	l, o := fixtures(t)
	changed := bytes.Replace(l, []byte(`"patient_name": "SYNTHETIC"`), []byte(`"patient_name": "PRIVATE-CANARY"`), 1)
	_, err := scenariolibrary.Check(context.Background(), changed, o)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-CANARY") {
		t.Fatalf("unsafe result %v", err)
	}
	// An oracle revision can change while the template's identity stays pinned.
	o = bytes.Replace(o, []byte(`"version": "1"`), []byte(`"version": "2"`), 1)
	if _, err := scenariolibrary.Check(context.Background(), l, o); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scenariolibrary.Check(ctx, l, o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Cancellation creates no durable output and a fresh invocation succeeds.
	if _, err := scenariolibrary.Check(context.Background(), l, o); err != nil {
		t.Fatal(err)
	}
}
func TestLibraryRefusesFalseProfileAndDuplicateVersion(t *testing.T) {
	l, o := fixtures(t)
	var library scenariolibrary.Library
	if err := json.Unmarshal(l, &library); err != nil {
		t.Fatal(err)
	}
	library.Templates[0].Profile = "readmit-adt-lifecycle-v1"
	bad, _ := json.Marshal(library)
	if _, err := scenariolibrary.Check(context.Background(), bad, o); err == nil {
		t.Fatal("accepted false profile")
	}
	json.Unmarshal(l, &library)
	library.Templates = append(library.Templates, library.Templates[0])
	bad, _ = json.Marshal(library)
	if _, err := scenariolibrary.Check(context.Background(), bad, o); err == nil {
		t.Fatal("accepted duplicate version")
	}
}

func TestOtherSupportedProfilesUseTheirOwnIndependentExpectations(t *testing.T) {
	for _, tc := range []struct{ family, step, outcome, from, to, event string }{
		{"adt", "register", "accepted", "none", "preadmit", "ADT^A04"},
		{"orm", "step-1", "refused", "none", "none", "ORM^O01"},
		{"oru", "step-1", "refused", "ordered", "ordered", "ORU^R01"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			l, o := fixtures(t)
			var library scenariolibrary.Library
			var oracle scenariolibrary.Expectations
			if err := json.Unmarshal(l, &library); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(o, &oracle); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile("../../testdata/fixtures/scenario-" + tc.family + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var template map[string]jsontext.Value
			if err := json.Unmarshal(raw, &template); err != nil {
				t.Fatal(err)
			}
			var steps []jsontext.Value
			json.Unmarshal(template["steps"], &steps)
			template["steps"], _ = json.Marshal(steps[:1])
			if tc.family == "adt" {
				var subjects []jsontext.Value
				json.Unmarshal(template["subjects"], &subjects)
				template["subjects"], _ = json.Marshal([]jsontext.Value{subjects[0], subjects[3]})
			}
			if tc.family == "oru" {
				var results []jsontext.Value
				json.Unmarshal(template["results"], &results)
				template["results"], _ = json.Marshal(results[:1])
			}
			var plan scenariogen.Plan
			json.Unmarshal(library.Templates[0].Plan, &plan)
			plan.Template, _ = json.Marshal(template)
			plan.Variants = plan.Variants[:1]
			library.Templates[0].Plan, _ = json.Marshal(plan)
			library.Templates[0].Profile = "readmit-" + tc.family + "-lifecycle-v1"
			oracle.PlanSHA256, err = scenariolibrary.PlanDigest(library.Templates[0].Plan)
			if err != nil {
				t.Fatal(err)
			}
			oracle.Lifecycle = []scenariolibrary.Outcome{{Step: tc.step, Outcome: tc.outcome, From: tc.from, To: tc.to}}
			oracle.Streams = oracle.Streams[:1]
			oracle.Streams[0].Messages = []scenariolibrary.Message{{Step: tc.step, After: "0s", Duplicate: false, Fields: []scenariolibrary.Field{{Selector: "MSH-9", State: "present", Hex: hex.EncodeToString([]byte(tc.event))}}}}
			l, _ = json.Marshal(library)
			o, _ = json.Marshal(oracle)
			if _, err := scenariolibrary.Check(context.Background(), l, o); err != nil {
				t.Fatal(err)
			}
			library.Templates[0].Profile = "unknown-profile"
			l, _ = json.Marshal(library)
			if _, err := scenariolibrary.Check(context.Background(), l, o); err == nil {
				t.Fatal("unknown profile passed")
			}
		})
	}
}

// Cancellation is injected at the public context boundary after real stream
// bytes exist, not at an internal helper or by racing a timer against I/O.
type cancelAfterFirstStream struct {
	context.Context
	parent   string
	observed bool
}

func (c *cancelAfterFirstStream) Err() error {
	files, _ := filepath.Glob(filepath.Join(c.parent, "readmit-library-*", "generation", "stream-001.mllp"))
	if len(files) > 0 {
		c.observed = true
		return context.Canceled
	}
	return nil
}
func TestInterruptedCheckRemovesPrivateStreamsAndCanRetry(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("TMPDIR", parent)
	t.Setenv("TMP", parent)
	t.Setenv("TEMP", parent)
	l, o := fixtures(t)
	ctx := &cancelAfterFirstStream{Context: context.Background(), parent: parent}
	if _, err := scenariolibrary.Check(ctx, l, o); !errors.Is(err, context.Canceled) || !ctx.observed {
		t.Fatalf("did not cancel after creating stream: %v", err)
	}
	files, err := os.ReadDir(parent)
	if err != nil || len(files) != 0 {
		t.Fatalf("retained private data after interruption: %v %v", files, err)
	}
	if _, err := scenariolibrary.Check(context.Background(), l, o); err != nil {
		t.Fatal(err)
	}
	files, err = os.ReadDir(parent)
	if err != nil || len(files) != 0 {
		t.Fatal("retry retained temporary streams")
	}
}

// A library read on its own is held to exactly what Check holds the library
// it is given to: the shipped library decodes, and every library defect Check
// refuses with a valid oracle, Decode refuses in the same words.
func TestDecodeRefusesEveryLibraryDefectCheckRefuses(t *testing.T) {
	l, o := fixtures(t)
	decoded, err := scenariolibrary.Decode(l)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Templates) == 0 || decoded.Templates[0].ID != "siu-cancel-book" {
		t.Fatalf("decoded library: %+v", decoded)
	}
	var library scenariolibrary.Library
	mutated := func(change func(*scenariolibrary.Library)) []byte {
		t.Helper()
		if err := json.Unmarshal(l, &library); err != nil {
			t.Fatal(err)
		}
		change(&library)
		out, err := json.Marshal(library)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for name, bad := range map[string][]byte{
		"false profile":       mutated(func(l *scenariolibrary.Library) { l.Templates[0].Profile = "readmit-adt-lifecycle-v1" }),
		"duplicate version":   mutated(func(l *scenariolibrary.Library) { l.Templates = append(l.Templates, l.Templates[0]) }),
		"no templates":        mutated(func(l *scenariolibrary.Library) { l.Templates = nil }),
		"seventeen templates": mutated(func(l *scenariolibrary.Library) { l.Templates = versionedCopies(l.Templates[0], 17) }),
		"invalid identity":    mutated(func(l *scenariolibrary.Library) { l.Templates[0].ID = "not an id" }),
		"no coverage":         mutated(func(l *scenariolibrary.Library) { l.Templates[0].Coverage = []string{} }),
		"duplicate coverage":  mutated(func(l *scenariolibrary.Library) { l.Templates[0].Coverage = []string{"a", "a"} }),
		"invalid plan": mutated(func(l *scenariolibrary.Library) {
			l.Templates[0].Plan = jsontext.Value(`{"schema":"readmit-scenario-generator/v1"}`)
		}),
		"another schema":       bytes.Replace(l, []byte(`"readmit-scenario-library/v1"`), []byte(`"readmit-scenario-library/v2"`), 1),
		"unknown member":       bytes.Replace(l, []byte(`"templates": [`), []byte(`"extra": true, "templates": [`), 1),
		"missing coverage":     bytes.Replace(l, []byte(`"coverage": [`), []byte(`"covered": [`), 1),
		"larger than 4 MiB":    append(bytes.Repeat([]byte(" "), scenariolibrary.MaxBytes), l...),
		"not a JSON object":    []byte(`[]`),
		"null templates array": bytes.Replace(l, []byte(`"templates": [`), []byte(`"templates": null, "x": [`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, checked := scenariolibrary.Check(context.Background(), bad, o)
			_, read := scenariolibrary.Decode(bad)
			if checked == nil || read == nil || read.Error() != checked.Error() {
				t.Fatalf("Decode refused %v where Check refused %v", read, checked)
			}
		})
	}
}

func versionedCopies(template scenariolibrary.Template, count int) []scenariolibrary.Template {
	templates := make([]scenariolibrary.Template, 0, count)
	for i := range count {
		copied := template
		copied.Version = fmt.Sprint(i + 1)
		templates = append(templates, copied)
	}
	return templates
}
