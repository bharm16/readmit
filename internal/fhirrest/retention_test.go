package fhirrest_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
)

func cloneEvidence(t *testing.T, source string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "copied")
	if e := os.Mkdir(to, 0700); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(source)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		b, e := os.ReadFile(filepath.Join(source, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(to, entry.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return to
}
func sealEdited(t *testing.T, dir string) {
	t.Helper()
	files := map[string][]byte{}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		b, e := os.ReadFile(filepath.Join(dir, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		files[entry.Name()] = b
	}
	if e := os.WriteFile(filepath.Join(dir, "identity.sha256"), []byte(artifactdir.Identity(fhirrest.ResultSchema, files)+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
}
func editJSON(t *testing.T, dir, name string, edit func(map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, name)
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var value map[string]any
	if e = json.Unmarshal(raw, &value); e != nil {
		t.Fatal(e)
	}
	edit(value)
	raw, e = json.Marshal(value, json.Deterministic(true))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestFHIRRESTOfflineRelocationAndResealedEvidenceRefusals(t *testing.T) {
	f := newServer(t)
	f.pages = []string{searchPage("", match("p1", "1"))}
	spec := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
	spec.Projection = projection()
	_, source := f.execute(spec)
	calls := f.requests
	f.s.Close()
	copy := cloneEvidence(t, source)
	if _, e := fhirrest.Open(context.Background(), copy); e != nil {
		t.Fatal("relocated evidence", e)
	}
	if f.requests != calls {
		t.Fatal("offline view connected")
	}
	for _, tc := range []struct {
		name string
		edit func(string)
	}{
		{"projection changed", func(dir string) {
			editJSON(t, dir, "result.json", func(v map[string]any) {
				p := v["projections"].([]any)[0].(map[string]any)
				row := p["rows"].([]any)[0].(map[string]any)
				row["values"].([]any)[0].(map[string]any)["state"] = "absent"
			})
		}},
		{"request rebound", func(dir string) {
			editJSON(t, dir, "attempt-0001.json", func(v map[string]any) {
				v["request"].(map[string]any)["http"].(map[string]any)["url"] = "https://different.invalid/fhir/Patient"
			})
		}},
		{"body changed", func(dir string) {
			os.WriteFile(filepath.Join(dir, "response-0001.bin"), []byte(`{"resourceType":"Bundle","type":"searchset"}`), 0600)
		}},
		{"orphan intent", func(dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, "intent-0001.json"))
			os.WriteFile(filepath.Join(dir, "intent-0002.json"), raw, 0600)
		}},
		{"invented pass status", func(dir string) { editJSON(t, dir, "result.json", func(v map[string]any) { v["state"] = "passed" }) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := cloneEvidence(t, source)
			tc.edit(dir)
			sealEdited(t, dir)
			if _, e := fhirrest.Open(context.Background(), dir); e == nil {
				t.Fatal("resealed contradiction accepted")
			}
		})
	}
	interrupted := cloneEvidence(t, source)
	os.Remove(filepath.Join(interrupted, "identity.sha256"))
	os.Remove(filepath.Join(interrupted, "result.json"))
	if state, e := fhirrest.Inspect(interrupted); e != nil || state != "delivery-uncertain" {
		t.Fatal("abrupt interruption lost", state, e)
	}
	if f.requests != calls {
		t.Fatal("inspection resumed work")
	}
}

type bearer struct{}

func (bearer) Identity() string { return networkaction.Digest([]byte("fixture provider")) }
func (bearer) Material(context.Context, networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	return networkaction.BearerMaterial([]byte("private-runtime-bearer")), nil
}
func TestFHIRRESTCredentialEchoIsWithheldFromArtifacts(t *testing.T) {
	f := newServer(t)
	f.statusOverride = 200
	f.bodyOverride = []byte(`{"echo":"private-runtime-bearer"}`)
	spec := f.spec("GET", "/Patient/p", nil)
	spec.HTTP.Authorization = (bearer{}).Identity()
	raw, _ := json.Marshal(spec)
	plan, e := fhirrest.Prepare(raw, f.policy)
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(t.TempDir(), "result")
	r, e := plan.Execute(context.Background(), admission(plan), bearer{}, dir, nil)
	if e != nil || r.State != "response-withheld" {
		t.Fatal(r.State, e)
	}
	if _, e = fhirrest.Open(context.Background(), dir); e != nil {
		t.Fatal(e)
	}
	output := fmt.Sprintf("%v %+v %#v %s %q %x", r, r, r, r, r, r)
	summary, _ := json.Marshal(r.Summary())
	if strings.Contains(output+string(summary), f.s.URL) || strings.Contains(output+string(summary), "private-runtime-bearer") {
		t.Fatal("operational projection exposed private request")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		raw, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		if bytes.Contains(raw, []byte("private-runtime-bearer")) {
			t.Fatal("credential retained")
		}
	}
}

func TestFHIRRESTReaderRejectsConsistentButUnwitnessedSuccess(t *testing.T) {
	f := newServer(t)
	_, source := f.execute(f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`)))
	dir := cloneEvidence(t, source)
	editJSON(t, dir, "attempt-0001.json", func(v map[string]any) { v["receipt"].(map[string]any)["schema"] = "" })
	editJSON(t, dir, "result.json", func(v map[string]any) {
		v["attempts"].([]any)[1].(map[string]any)["receipt"].(map[string]any)["schema"] = ""
	})
	sealEdited(t, dir)
	if _, e := fhirrest.Open(context.Background(), dir); e == nil {
		t.Fatal("response with no valid transport receipt accepted")
	}
}

func TestFHIRRESTExactBytesShareTheVerifiedOfflineSnapshot(t *testing.T) {
	f := newServer(t)
	request := []byte("{\n  \"resourceType\": \"Patient\", \"active\": true\n}")
	_, source := f.execute(f.spec("POST", "/Patient", request))
	expected := append([]byte(nil), f.resources["1"]...)
	f.s.Close()
	evidence, e := fhirrest.OpenEvidence(context.Background(), source)
	if e != nil {
		t.Fatal(e)
	}
	sent, ok := evidence.RequestBytes(1)
	if !ok || !bytes.Equal(sent, request) {
		t.Fatal("request bytes rewritten")
	}
	received, ok := evidence.ResponseBytes(1)
	if !ok || !bytes.Equal(received, expected) {
		t.Fatal("response bytes rewritten")
	}
	os.WriteFile(filepath.Join(source, "response-0001.bin"), []byte("changed after read"), 0600)
	again, ok := evidence.ResponseBytes(1)
	if !ok || !bytes.Equal(again, expected) {
		t.Fatal("snapshot re-read changed source")
	}
	received[0] = 'X'
	again, _ = evidence.ResponseBytes(1)
	if !bytes.Equal(again, expected) {
		t.Fatal("caller mutated private snapshot")
	}
	if _, e = fhirrest.Open(context.Background(), source); e == nil {
		t.Fatal("new read accepted changed evidence")
	}
}
