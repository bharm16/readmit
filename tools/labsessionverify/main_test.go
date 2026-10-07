package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedtest"
)

// The seam is the production saved connected-test directory reader. The oracle
// is the frozen synthetic booking/rescheduling example, independently of OIE.
func TestSessionInputsCompileWithUnchangedDuplicateAssertions(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	var previous []byte
	for _, mode := range []string{"defective", "corrected", "reintroduced"} {
		d := driver{state: state, root: filepath.Join(root, mode), session: session{Generation: "lab-" + mode, Mode: mode}}
		d.connection.MLLP.Host = "127.0.0.1"
		d.connection.MLLP.Port = 16662
		d.connection.FHIR.Base = "https://127.0.0.1:19443/fhir"
		d.connection.FHIR.Token = "https://127.0.0.1:19443/token"
		d.connection.FHIR.Client = "observer"
		d.connection.FHIR.Scopes = []string{"system/*.rs"}
		d.connection.FHIR.ServerName = "example.com"
		d.connection.FHIR.PrivateKey = "/operator/key.pem"
		d.key, _ = rsa.GenerateKey(rand.Reader, 2048)
		folder := filepath.Join(state, "session-evidence", d.session.Generation)
		if err := os.MkdirAll(filepath.Join(folder, "stimuli"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(d.root, 0700); err != nil {
			t.Fatal(err)
		}
		provenance := []stimulusProvenance{}
		for _, name := range []string{"siu-s12.hl7", "siu-s13.hl7"} {
			raw, e := os.ReadFile(filepath.Join("../../testdata/integration-lab", name))
			if e != nil {
				t.Fatal(e)
			}
			derived := bytes.ReplaceAll(raw, []byte("__GENERATION__"), []byte(d.session.Generation))
			provenance = append(provenance, stimulusProvenance{Source: "testdata/integration-lab/" + name, SourceSHA256: networkaction.Digest(raw), SourceBase64: base64.StdEncoding.EncodeToString(raw), DerivedSHA256: networkaction.Digest(derived)})
			if e = os.WriteFile(filepath.Join(folder, "stimuli", name), derived, 0600); e != nil {
				t.Fatal(e)
			}
		}
		if e := writeJSON(filepath.Join(folder, "source-provenance.json"), provenance); e != nil {
			t.Fatal(e)
		}
		capability := []byte(`{"resourceType":"CapabilityStatement","status":"active","date":"2026-01-01","kind":"instance","fhirVersion":"4.0.1","format":["json"],"rest":[{"mode":"server","resource":[{"type":"Appointment","interaction":[{"code":"search-type"}],"searchParam":[{"name":"identifier","type":"token"},{"name":"_tag","type":"token"}]}]}]}`)
		if err := os.WriteFile(filepath.Join(folder, "capability.json"), capability, 0600); err != nil {
			t.Fatal(err)
		}
		patient := map[string]any{"resourceType": "Patient", "id": "lab-patient-01", "meta": map[string]any{"versionId": "1", "tag": []any{map[string]any{"system": "urn:readmit:independent-lab", "code": d.session.Generation}}}}
		adapter, err := newAdapter(&d, patient)
		if err != nil {
			t.Fatal(err)
		}
		err = d.author(adapter, patient)
		defer adapter.server.Close()
		d.ca = adapter.ca()
		d.connection.FHIR.Authorities = filepath.Join(d.root, "ca.pem")
		if e := os.WriteFile(d.connection.FHIR.Authorities, d.ca, 0600); e != nil {
			t.Fatal(e)
		}
		if err != nil {
			t.Fatal(err)
		}
		plan, err := connectedtest.PrepareFlowDirectory(filepath.Join(d.root, "inputs"), connectedtest.Generation{Seed: 7, BaseTime: "2030-01-01T00:00:00Z"})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if err = plan.Write(t.Context(), filepath.Join(d.root, "plan")); err != nil {
			t.Fatal(err)
		}
		if err = d.configure(adapter); err != nil {
			t.Fatal(err)
		}
		if _, err = connectedrun.PrepareFlow(filepath.Join(d.root, "plan"), filepath.Join(d.root, "config.json"), "product"); err != nil {
			t.Fatal(err)
		}
		var checks map[string]json.RawMessage
		if err = readJSON(filepath.Join(d.root, "inputs/move-checks.json"), &checks); err != nil {
			t.Fatal(err)
		}
		assertions := checks["assertions"]
		if previous != nil && string(assertions) != string(previous) {
			t.Fatal("oracle changed with target revision")
		}
		previous = assertions
	}
}
func TestSessionControllerRefusesConcurrentProductOwner(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("session controller is supported on Linux and macOS")
	}
	path := filepath.Join(t.TempDir(), "session.lock")
	release, err := lockSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if other, err := lockSession(path); err == nil {
		other()
		t.Fatal("concurrent controller obtained active product lease")
	}
}

func TestProductRunRefusesInterruptedSessionBeforeCreatingOutput(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("session controller is supported on Linux and macOS")
	}
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, "control"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(state, "control/session.json"), session{Schema: "readmit-lab-session/v1", Generation: "lab-stale", Status: "ready", Mode: "defective"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(state, "control/session-intent.json"), map[string]string{"action": "revision", "generation": "lab-next"}); err != nil {
		t.Fatal(err)
	}
	d := driver{state: state, root: filepath.Join(state, "output"), binary: "/unreached/readmit"}
	if err := d.run(); err == nil || !strings.Contains(err.Error(), "session transition is pending") {
		t.Fatalf("pending transition was not refused explicitly: %v", err)
	}
	if _, err := os.Stat(d.root); !os.IsNotExist(err) {
		t.Fatal("interrupted session created product output")
	}
}

func TestProductRunRequiresExplicitNewGenerationAfterAnAttempt(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("session controller is supported on Linux and macOS")
	}
	state := t.TempDir()
	for _, path := range []string{"control", "session-evidence/lab-owned"} {
		if err := os.MkdirAll(filepath.Join(state, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(filepath.Join(state, "control/session.json"), session{Schema: "readmit-lab-session/v1", Generation: "lab-owned", Status: "ready", Mode: "defective"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(state, "session-evidence/lab-owned/product-attempt.json"), map[string]string{"state": "effect-intent"}); err != nil {
		t.Fatal(err)
	}
	d := driver{state: state, root: filepath.Join(state, "output"), binary: "/unreached/readmit"}
	if err := d.run(); err == nil || !strings.Contains(err.Error(), "generation already has a product attempt") {
		t.Fatalf("previous effect intent was not refused explicitly: %v", err)
	}
	if _, err := os.Stat(d.root); !os.IsNotExist(err) {
		t.Fatal("previous attempt created product output")
	}
}
