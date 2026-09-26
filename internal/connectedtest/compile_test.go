package connectedtest_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/connectedtest"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCompilePinsIndependentChecksAndPreservesDuplicateBusinessIDs(t *testing.T) {
	raw, files := example(t)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{Seed: 42, BaseTime: "2026-01-01T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := connectedtest.Compile(raw, files, connectedtest.Generation{Seed: 42, BaseTime: "2026-01-01T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Identity() != again.Identity() {
		t.Fatal("same explicit inputs changed plan")
	}
	if !bytes.Equal(p.Files()["inputs/book.hl7"], files["book.hl7"]) {
		t.Fatal("wire bytes changed")
	}
	if p.Document().Environment.TargetRevision.Provenance != "operator-declared" {
		t.Fatal("lost target provenance")
	}
	// Returned documents and bytes cannot mutate the compiled identity.
	changed := p.Files()
	changed["inputs/book.hl7"][0] = 'X'
	if !bytes.Equal(p.Files()["inputs/book.hl7"], files["book.hl7"]) {
		t.Fatal("mutable prepared plan")
	}
}

func example(t *testing.T) ([]byte, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{
		"book.hl7":    []byte("MSH|^~\\&|SYNTH|LAB|TARGET|LAB|20260101120000+0000||SIU^S12|DUPLICATE|P|2.5.1\rSCH|APT1||||||||||20260102120000+0000\r"),
		"checks.json": []byte(`{"schema":"readmit-assertion-set/v1","name":"independent ACK check","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"s0001-e000001","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`),
	}
	ref := func(id, schema, file string) connectedtest.Reference {
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: file, SHA256: connectedtest.Digest(files[file])}
	}
	d := connectedtest.Test{Schema: connectedtest.TestSchema, Project: "lab", ID: "reschedule", Revision: "1", Environment: connectedtest.Environment{Project: "lab", ID: "fixture", Revision: "1", Name: "Local fixture", Classification: "test", Endpoint: "receiver", TargetIdentity: connectedtest.Digest([]byte("target")), AddressPolicyIdentity: connectedtest.Digest([]byte("policy")), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Value: "fixture-v1", Provenance: "operator-declared"}}, Checks: ref("checks", "readmit-assertion-set/v1", "checks.json"), Steps: []connectedtest.Step{{ID: "book", Endpoint: "receiver", V2: &connectedtest.V2Stimulus{Input: ref("book", "hl7", "book.hl7"), Occurrence: "s0001-e000001"}}}, Datasets: []connectedtest.Dataset{{ID: "acks", Kind: "v2-messages", Phase: "after", Source: "legacy-ack", Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: 1000, MaxRecords: 10, MaxBytes: 65536}}}, Bindings: connectedtest.Bindings{Observed: "acks"}, Setup: connectedtest.Setup{Kind: "operator-declared", Isolation: "dedicated-fixture", Instructions: "Independent empty fixture; no automatic reset"}, Limits: connectedtest.Limits{MaxSteps: 10, MaxBytes: 1048576, DeadlineMS: 5000}, OperatorVersion: connectedtest.OperatorVersion}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw, files
}

func TestCompileRefusesReferencesOrderingTypesAndUnknownMembers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*connectedtest.Test, map[string][]byte)
	}{
		{"cross project", func(d *connectedtest.Test, _ map[string][]byte) { d.Checks.Project = "other" }},
		{"wrong digest", func(d *connectedtest.Test, _ map[string][]byte) { d.Checks.SHA256 = connectedtest.Digest(nil) }},
		{"missing bytes", func(d *connectedtest.Test, f map[string][]byte) { delete(f, d.Checks.File) }},
		{"unknown version", func(d *connectedtest.Test, _ map[string][]byte) { d.Schema = "readmit-connected-test/v2" }},
		{"duplicate steps", func(d *connectedtest.Test, _ map[string][]byte) { d.Steps = append(d.Steps, d.Steps[0]) }},
		{"missing dependency", func(d *connectedtest.Test, _ map[string][]byte) { d.Steps[0].After = []string{"missing"} }},
		{"cycle", func(d *connectedtest.Test, _ map[string][]byte) { d.Steps[0].After = []string{"book"} }},
		{"incompatible dataset", func(d *connectedtest.Test, _ map[string][]byte) { d.Datasets[0].Kind = "fhir-resources" }},
		{"missing dataset", func(d *connectedtest.Test, _ map[string][]byte) { d.Bindings.Observed = "missing" }},
		{"unknown operator", func(d *connectedtest.Test, _ map[string][]byte) { d.OperatorVersion = "future" }},
		{"production", func(d *connectedtest.Test, _ map[string][]byte) { d.Environment.Classification = "production" }},
		{"no TLS name", func(d *connectedtest.Test, _ map[string][]byte) { d.Environment.TLS.Mode = "tls" }},
		{"unbounded horizon", func(d *connectedtest.Test, _ map[string][]byte) { d.Datasets[0].Completion.HorizonMS = 0 }},
		{"path escape", func(d *connectedtest.Test, _ map[string][]byte) { d.Checks.File = "../checks.json" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, f := example(t)
			var d connectedtest.Test
			_ = json.Unmarshal(raw, &d)
			tc.change(&d, f)
			raw, _ = json.Marshal(d)
			if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
				t.Fatal("accepted refused configuration")
			}
		})
	}
	raw, f := example(t)
	raw = bytes.Replace(raw, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1)
	if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("accepted unknown member")
	}
}

func TestGeneratedValuesBindExactWireBytesWithoutChangingOriginal(t *testing.T) {
	raw, f := example(t)
	original := bytes.Clone(f["book.hl7"])
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	d.Variables = []connectedtest.Variable{{ID: "control", Kind: "literal", Value: "INTENTIONAL-DUPLICATE"}, {ID: "clock", Kind: "timestamp"}}
	d.Steps[0].V2.Assignments = []connectedtest.Assignment{{Selector: "MSH-10", Variable: "control"}, {Selector: "MSH-7", Variable: "clock"}}
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, f, connectedtest.Generation{Seed: 42, BaseTime: "2026-01-01T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(original, []byte("DUPLICATE"), []byte("INTENTIONAL-DUPLICATE"), 1)
	if !bytes.Equal(want, p.Files()["inputs/book.hl7"]) || !bytes.Equal(original, f["book.hl7"]) {
		t.Fatal("generation lost exact bytes or mutated source")
	}
}

func TestObservedRevisionRetainsPreflightSourceAndRejectsCrossTarget(t *testing.T) {
	raw, f := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	f["metadata.txt"] = []byte("Independent fixture server version 7\n")
	source := connectedtest.Reference{Project: "lab", ID: "metadata", Schema: "target-metadata", File: "metadata.txt", SHA256: connectedtest.Digest(f["metadata.txt"])}
	receipt := connectedtest.RevisionEvidence{Schema: connectedtest.RevisionEvidenceSchema, TargetIdentity: d.Environment.TargetIdentity, AddressPolicyIdentity: d.Environment.AddressPolicyIdentity, Revision: "7", ObservedAt: "2026-01-01T12:00:00Z", CollectorVersion: "independent-fixture-probe/1", Source: source}
	f["preflight.json"], _ = json.Marshal(receipt)
	d.Environment.TargetRevision = connectedtest.TargetRevision{Value: "7", Provenance: "independently-observed", Evidence: &connectedtest.Reference{Project: "lab", ID: "preflight", Schema: connectedtest.RevisionEvidenceSchema, File: "preflight.json", SHA256: connectedtest.Digest(f["preflight.json"])}}
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir() + "/plan"
	if err := p.Write(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := connectedtest.OpenPlan(dir); err != nil {
		t.Fatal(err)
	}
	d.Environment.TargetIdentity = connectedtest.Digest([]byte("another-target"))
	raw, _ = json.Marshal(d)
	if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("accepted cross-target preflight")
	}
}

func TestRepeatedMaterializedInputsCountAgainstByteLimit(t *testing.T) {
	raw, f := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	f["book.hl7"] = append(f["book.hl7"], []byte("NTE|1||"+string(bytes.Repeat([]byte("x"), 100000))+"\r")...)
	d.Steps[0].V2.Input.SHA256 = connectedtest.Digest(f["book.hl7"])
	second := d.Steps[0]
	second.ID = "repeat"
	v2 := *second.V2
	v2.Occurrence = "s0001-e000002"
	second.V2 = &v2
	d.Steps = append(d.Steps, second)
	d.Limits.MaxBytes = 250000
	raw, _ = json.Marshal(d)
	if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("repeated materialized bytes escaped bound")
	}
}

func TestCompileHasNoDNSOrSecretResolution(t *testing.T) {
	raw, f := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	d.Environment.TLS = connectedtest.TLS{Mode: "tls", ServerName: "must-not-resolve.invalid"}
	raw, _ = json.Marshal(d)
	previous := net.DefaultResolver
	calls := 0
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("compiler attempted DNS")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("compiler resolved a host")
	}
	// A provider member is refused as configuration, never executed. This
	// executable marker would create a file if a secret provider were invoked.
	dir := t.TempDir()
	marker := filepath.Join(dir, "resolved")
	provider := filepath.Join(dir, "provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	extra, _ := json.Marshal(provider)
	raw = bytes.Replace(raw, []byte(`"schema":`), append(append([]byte(`"secret_provider":`), extra...), []byte(`,"schema":`)...), 1)
	if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
		t.Fatal("accepted secret provider configuration")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("secret provider ran during compilation")
	}
}

func TestIntentionalDuplicatesAndExplicitPCGGeneration(t *testing.T) {
	raw, f := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	d.Variables = []connectedtest.Variable{{ID: "synthetic", Kind: "synthetic-id", Namespace: "patient"}}
	repeat := d.Steps[0]
	repeat.ID = "repeat"
	v := *repeat.V2
	v.Occurrence = "s0001-e000002"
	repeat.V2 = &v
	d.Steps = append(d.Steps, repeat)
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, f, connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := connectedtest.Compile(raw, f, connectedtest.Generation{Seed: 2, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Files()["inputs/book.hl7"], p.Files()["inputs/repeat.hl7"]) {
		t.Fatal("intentional duplicate control ID rewritten")
	}
	if p.Document().Resolution["synthetic"] == q.Document().Resolution["synthetic"] {
		t.Fatal("explicit seed did not affect synthetic generation")
	}
}
