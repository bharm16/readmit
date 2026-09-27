package fhirvalidator_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"path/filepath"
	"strings"
	"testing"
)

func TestFHIRValidatorPrepareNeedsExplicitPinnedCapability(t *testing.T) {
	input := []byte(`{"resourceType":"Patient","active":true}`)
	request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"error", "fatal"}}, TimeoutMS: 120000, MaxOutputBytes: 1 << 20}
	raw, _ := json.Marshal(request)
	if _, e := fhirvalidator.Prepare(raw, input, nil); e == nil || !strings.Contains(e.Error(), "worker-missing") {
		t.Fatal("missing worker became optional", e)
	}
	m, assets := fixtureManifest()
	m.ValidatorPackages = []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}
	cap, e := fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "capability"), m, assets)
	if e != nil {
		t.Fatal(e)
	}
	request.Capability = cap.Identity()
	request.Profiles = []fhirvalidator.Canonical{m.Profiles[0]}
	raw, _ = json.Marshal(request)
	if _, e = fhirvalidator.Prepare(raw, input, cap); e == nil || !strings.Contains(e.Error(), "package-unavailable") {
		t.Fatal("missing implicit validator root considered ready", e)
	}
	m.Packages = append(m.Packages, fixturePinnedPackage(fhirvalidator.PackageRef{ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}))
	m.ValidatorPackages = append(m.ValidatorPackages, fhirvalidator.PackageRef{ID: "hl7.fhir.xver-extensions", Version: "0.1.0"})
	for _, root := range []fhirvalidator.PackageRef{{ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}} {
		m.Packages = append(m.Packages, fixturePinnedPackage(root))
		m.ValidatorPackages = append(m.ValidatorPackages, root)
	}
	cap, e = fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "ready"), m, assets)
	if e != nil {
		t.Fatal(e)
	}
	request.Capability = cap.Identity()
	raw, _ = json.Marshal(request)
	if _, e = fhirvalidator.Prepare(raw, input, cap); e != nil {
		t.Fatal(e)
	}
	request.Profiles[0].Version = "wrong"
	raw, _ = json.Marshal(request)
	if _, e = fhirvalidator.Prepare(raw, input, cap); e == nil || !strings.Contains(e.Error(), "profile-unavailable") {
		t.Fatal("wrong version accepted", e)
	}
}

func TestFHIRValidatorPrepareRefusesAnUnqualifiedValidatorOrRuntime(t *testing.T) {
	input := []byte(`{"resourceType":"Patient","active":true}`)
	for name, edit := range map[string]func(*fhirvalidator.Manifest){
		"validator version": func(m *fhirvalidator.Manifest) { m.Validator.Version = "6.10.5" },
		"validator digest":  func(m *fhirvalidator.Manifest) { m.Validator.SHA256 = strings.Repeat("a", 64) },
		"runtime version":   func(m *fhirvalidator.Manifest) { m.Runtime.Version = "17.0.14+7" },
		"runtime digest":    func(m *fhirvalidator.Manifest) { m.Runtime.SHA256 = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			m, assets := contractManifest()
			edit(&m)
			cap, e := fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "capability"), m, assets)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(contractRequest(cap, input))
			if _, e = fhirvalidator.Prepare(raw, input, cap); e == nil || !strings.Contains(e.Error(), "unsupported-runtime") {
				t.Fatal("unqualified combination prepared", e)
			}
		})
	}
}
