package fhirvalidator_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

func contractManifest() (fhirvalidator.Manifest, map[string][]byte) {
	m, assets := fixtureManifest()
	core := fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"}
	for _, ref := range []fhirvalidator.PackageRef{{ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}, {ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}} {
		m.Packages = append(m.Packages, fixturePinnedPackage(ref))
	}
	m.ValidatorPackages = []fhirvalidator.PackageRef{core, {ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}, {ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}}
	m.Terminology = []fhirvalidator.Terminology{
		{Kind: "ValueSet", Content: "compose", Canonical: fhirvalidator.Canonical{URL: "urn:readmit:fixture:values", Version: "1.0.0", SHA256: networkaction.Digest([]byte("independent values")), Package: core}, References: []fhirvalidator.CanonicalLink{{URL: "urn:readmit:fixture:codes", Version: "1.0.0"}}},
		{Kind: "CodeSystem", Content: "complete", Canonical: fhirvalidator.Canonical{URL: "urn:readmit:fixture:codes", Version: "1.0.0", SHA256: networkaction.Digest([]byte("independent codes")), Package: core}, References: []fhirvalidator.CanonicalLink{}},
	}
	return m, assets
}

func contractJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func contractRequest(capability *fhirvalidator.Capability, input []byte) fhirvalidator.Request {
	return fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: capability.Identity(), InputSHA256: networkaction.Digest(input), Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"error", "fatal"}}, TimeoutMS: 10000, MaxOutputBytes: 1 << 20}
}

func TestFHIRValidatorCapabilityContractStageDetachesCallerState(t *testing.T) {
	for name, mutate := range map[string]func(*fhirvalidator.Manifest, map[string][]byte){
		"package slice":        func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.Packages[0].License = "changed" },
		"nested dependencies":  func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.Packages[1].Dependencies[0].Version = "9.0.0" },
		"validator root slice": func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.ValidatorPackages[0].ID = "different.package" },
		"profile slice":        func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.Profiles[0].URL = "urn:changed:profile" },
		"terminology slice":    func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.Terminology[0].Canonical.Version = "2.0.0" },
		"nested terminology references": func(m *fhirvalidator.Manifest, _ map[string][]byte) {
			m.Terminology[0].References[0].URL = "urn:changed:codes"
		},
		"asset descriptors": func(m *fhirvalidator.Manifest, _ map[string][]byte) { m.Assets[0].Path = "metadata/changed.json" },
		"asset map and bytes": func(_ *fhirvalidator.Manifest, assets map[string][]byte) {
			assets["metadata/sbom.json"][0] = '!'
			delete(assets, "licenses/fixture.txt")
			assets["licenses/added.txt"] = []byte("added after staging")
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, assets := contractManifest()
			dir := filepath.Join(t.TempDir(), "capability")
			capability, err := fhirvalidator.Stage(t.Context(), dir, m, assets)
			if err != nil {
				t.Fatal(err)
			}
			before, identity := contractJSON(t, capability.Manifest()), capability.Identity()
			mutate(&m, assets)
			if !bytes.Equal(before, contractJSON(t, capability.Manifest())) || capability.Identity() != identity {
				t.Fatal("caller mutation changed capability under its original identity")
			}
			moved := filepath.Join(t.TempDir(), "moved")
			if err := os.Rename(dir, moved); err != nil {
				t.Fatal(err)
			}
			reopened, err := fhirvalidator.OpenCapability(moved)
			if err != nil || reopened.Identity() != identity || !bytes.Equal(before, contractJSON(t, reopened.Manifest())) {
				t.Fatal("staged capability did not reopen with exact original facts", err)
			}
		})
	}
}

func TestFHIRValidatorCapabilityContractGettersAndRequestBytesAreDetached(t *testing.T) {
	m, assets := contractManifest()
	capability, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "capability"), m, assets)
	if err != nil {
		t.Fatal(err)
	}
	before := contractJSON(t, capability.Manifest())
	view := capability.Manifest()
	view.Packages[1].Dependencies[0].Version = "9.0.0"
	view.Profiles[0].SHA256 = strings.Repeat("f", 64)
	view.Terminology[0].References[0].URL = "urn:changed:codes"
	view.ValidatorPackages[0].ID = "changed.package"
	if !bytes.Equal(before, contractJSON(t, capability.Manifest())) {
		t.Fatal("manifest getter exposes retained state")
	}
	input := []byte(`{"resourceType":"Patient","active":true}`)
	request := contractRequest(capability, input)
	request.Profiles = []fhirvalidator.Canonical{m.Profiles[0]}
	raw := contractJSON(t, request)
	plan, err := fhirvalidator.Prepare(raw, input, capability)
	if err != nil {
		t.Fatal(err)
	}
	identity, expected := plan.Identity(), contractJSON(t, plan.Request())
	for i := range raw {
		raw[i] = ' '
	}
	for i := range input {
		input[i] = ' '
	}
	selected := plan.Request()
	selected.Profiles[0].URL = "urn:changed:profile"
	selected.Requirements.FailSeverities[0] = "information"
	if plan.Identity() != identity || !bytes.Equal(expected, contractJSON(t, plan.Request())) {
		t.Fatal("caller bytes or request getter changed prepared contract")
	}
}

func TestFHIRValidatorCapabilityContractRejectsBrokenPackageClosure(t *testing.T) {
	for name, change := range map[string]func(*fhirvalidator.Manifest){
		"missing dependency": func(m *fhirvalidator.Manifest) {
			m.Packages[1].Dependencies = []fhirvalidator.PackageRef{{ID: "missing.package", Version: "1.0.0"}}
		},
		"wrong dependency version": func(m *fhirvalidator.Manifest) { m.Packages[1].Dependencies[0].Version = "4.0.2" },
		"self cycle": func(m *fhirvalidator.Manifest) {
			m.Packages[1].Dependencies = []fhirvalidator.PackageRef{{ID: m.Packages[1].ID, Version: m.Packages[1].Version}}
		},
		"two package cycle": func(m *fhirvalidator.Manifest) {
			m.Packages[0].Dependencies = []fhirvalidator.PackageRef{{ID: m.Packages[1].ID, Version: m.Packages[1].Version}}
		},
		"duplicate dependency": func(m *fhirvalidator.Manifest) {
			m.Packages[1].Dependencies = append(m.Packages[1].Dependencies, m.Packages[1].Dependencies[0])
		},
		"missing validator root":        func(m *fhirvalidator.Manifest) { m.ValidatorPackages[0].Version = "4.0.2" },
		"profile wrong package version": func(m *fhirvalidator.Manifest) { m.Profiles[0].Package.Version = "4.0.2" },
	} {
		t.Run(name, func(t *testing.T) {
			m, assets := contractManifest()
			if _, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "valid"), m, assets); err != nil {
				t.Fatal("valid closure control refused", err)
			}
			change(&m)
			if _, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "invalid"), m, assets); err == nil {
				t.Fatal("incomplete or cyclic package closure accepted")
			}
		})
	}
}

func TestFHIRValidatorCapabilityContractRequiresExactProfileBinding(t *testing.T) {
	m, assets := contractManifest()
	second := m.Profiles[0]
	second.Version, second.SHA256 = "alternate", networkaction.Digest([]byte("independent alternate profile"))
	m.Profiles = append(m.Profiles, second)
	capability, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "capability"), m, assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, profile string
		selected      bool
		available     bool
	}{
		{"ambiguous implicit revision", m.Profiles[0].URL, false, false},
		{"exact implicit revision", m.Profiles[0].URL + "|4.0.1", false, true},
		{"missing implicit revision", m.Profiles[0].URL + "|missing", false, false},
		{"explicit alternate revision", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(`{"resourceType":"Patient","active":true}`)
			if tc.profile != "" {
				input = []byte(fmt.Sprintf(`{"resourceType":"Patient","meta":{"profile":[%q]},"active":true}`, tc.profile))
			}
			r := contractRequest(capability, input)
			if tc.selected {
				r.Profiles = []fhirvalidator.Canonical{second}
			}
			_, err := fhirvalidator.Prepare(contractJSON(t, r), input, capability)
			if tc.available && err != nil || !tc.available && (err == nil || !strings.Contains(err.Error(), "profile-unavailable")) {
				t.Fatalf("profile binding outcome differs: available=%t error=%v", tc.available, err)
			}
		})
	}
	input := []byte(`{"resourceType":"Patient"}`)
	for _, kind := range []string{"profile digest", "package version", "input digest", "capability identity"} {
		t.Run(kind, func(t *testing.T) {
			r := contractRequest(capability, input)
			r.Profiles = []fhirvalidator.Canonical{second}
			switch kind {
			case "profile digest":
				r.Profiles[0].SHA256 = strings.Repeat("f", 64)
			case "package version":
				r.Profiles[0].Package.Version = "4.0.2"
			case "input digest":
				r.InputSHA256 = strings.Repeat("f", 64)
			case "capability identity":
				r.Capability = strings.Repeat("f", 64)
			}
			if _, err := fhirvalidator.Prepare(contractJSON(t, r), input, capability); err == nil {
				t.Fatal("altered request identity accepted")
			}
		})
	}
}

func TestFHIRValidatorCapabilityContractRejectsUnsafeMembersAndMetadata(t *testing.T) {
	for _, member := range []string{"metadata/../outside.json", "licenses/../../outside.txt", "/metadata/sbom.json", `licenses\outside.txt`, "metadata/.hidden", "metadata//sbom.json", "licenses/name\n.txt", "metadata/C:drive.json"} {
		t.Run(member, func(t *testing.T) {
			m, assets := contractManifest()
			for i := range m.Assets {
				if m.Assets[i].Role == "sbom" {
					old := m.Assets[i].Path
					m.Assets[i].Path = member
					assets[member] = assets[old]
					delete(assets, old)
				}
			}
			m.Validator.SBOM, m.Runtime.SBOM = member, member
			if _, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "bad"), m, assets); err == nil {
				t.Fatal("unsafe member path accepted")
			}
		})
	}
	for name, raw := range map[string]string{"malformed": `{"a":`, "duplicate": `{"a":1,"a":2}`, "null": `null`, "array": `[]`, "overdeep": `{"a":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`} {
		t.Run(name, func(t *testing.T) {
			m, assets := contractManifest()
			assets["metadata/sbom.json"] = []byte(raw)
			for i := range m.Assets {
				if m.Assets[i].Path == "metadata/sbom.json" {
					m.Assets[i].Bytes = int64(len(raw))
					m.Assets[i].SHA256 = networkaction.Digest([]byte(raw))
				}
			}
			if _, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "bad"), m, assets); err == nil {
				t.Fatal("invalid or unbounded JSON metadata accepted")
			}
		})
	}
}

func TestFHIRValidatorCapabilityContractRejectsStrictJSONViolations(t *testing.T) {
	m, assets := contractManifest()
	capability, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "capability"), m, assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"resourceType":"Patient",}`, `{"resourceType":"Patient","resourceType":"Observation"}`, `{"resourceType":"Patient","extra":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`, `{"resourceType":"Patient"} {"resourceType":"Patient"}`} {
		raw := []byte(input)
		if _, err := fhirvalidator.Prepare(contractJSON(t, contractRequest(capability, raw)), raw, capability); err == nil {
			t.Fatal("malformed, duplicate or overdeep resource accepted")
		}
	}
	input := []byte(`{"resourceType":"Patient"}`)
	valid := contractJSON(t, contractRequest(capability, input))
	for _, raw := range [][]byte{append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"schema":"readmit-fhir-validation-request/v1"}`)...), append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"program":"other-worker"}`)...)} {
		if _, err := fhirvalidator.Prepare(raw, input, capability); err == nil {
			t.Fatal("ambiguous or executable request member accepted")
		}
	}
	for _, suffix := range []string{`,"schema":"readmit-fhir-validator-capability/v1"}`, `,"program":"other-worker"}`} {
		manifest := contractJSON(t, capability.Manifest())
		manifest = append(manifest[:len(manifest)-1], []byte(suffix)...)
		files := map[string][]byte{"manifest.json": manifest}
		for name, data := range assets {
			files[name] = bytes.Clone(data)
		}
		files["identity.sha256"] = []byte(artifactdir.Identity(fhirvalidator.CapabilitySchema, files) + "\n")
		dir := t.TempDir()
		for name, data := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := fhirvalidator.OpenCapability(dir); err == nil {
			t.Fatal("resealed ambiguous capability JSON accepted")
		}
	}
}

func TestFHIRValidatorCapabilityContractPassiveOperationsDoNotNeedRuntime(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("DOCKER_HOST", "tcp://unavailable.invalid:2375")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	m, assets := contractManifest()
	m.Profiles[0].URL = server.URL + "/StructureDefinition/site-patient"
	dir := filepath.Join(t.TempDir(), "capability")
	capability, err := fhirvalidator.Stage(t.Context(), dir, m, assets)
	if err != nil {
		t.Fatal(err)
	}
	capability, err = fhirvalidator.OpenCapability(dir)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(fmt.Sprintf(`{"resourceType":"Patient","meta":{"profile":[%q]},"generalPractitioner":[{"reference":%q}]}`, m.Profiles[0].URL+"|4.0.1", server.URL+"/Practitioner/external"))
	r := contractRequest(capability, input)
	r.Profiles = []fhirvalidator.Canonical{m.Profiles[0]}
	if _, err := fhirvalidator.Prepare(contractJSON(t, r), input, capability); err != nil {
		t.Fatal("pure preparation required a runtime", err)
	}
	if requests.Load() != 0 {
		t.Fatal("passive operation fetched referenced content")
	}
}

func TestFHIRValidatorCapabilityContractRejectsInvalidUTF8BeforeSealing(t *testing.T) {
	for name, change := range map[string]func(*fhirvalidator.Manifest){
		"component name":    func(m *fhirvalidator.Manifest) { m.Validator.Name = "validator-" + string([]byte{0xff}) },
		"component license": func(m *fhirvalidator.Manifest) { m.Runtime.License = "license-" + string([]byte{0xff}) },
		"package license":   func(m *fhirvalidator.Manifest) { m.Packages[1].License = "license-" + string([]byte{0xff}) },
		"profile version":   func(m *fhirvalidator.Manifest) { m.Profiles[0].Version = "revision-" + string([]byte{0xff}) },
		"terminology reference version": func(m *fhirvalidator.Manifest) {
			m.Terminology[0].References[0].Version = "revision-" + string([]byte{0xff})
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, assets := contractManifest()
			change(&m)
			dir := filepath.Join(t.TempDir(), "refused")
			if _, err := fhirvalidator.Stage(t.Context(), dir, m, assets); err == nil {
				t.Error("invalid UTF-8 manifest was staged")
			}
			if _, err := os.Stat(filepath.Join(dir, "identity.sha256")); !os.IsNotExist(err) {
				t.Fatalf("invalid UTF-8 manifest received a completion seal: %v", err)
			}
		})
	}
	t.Run("valid Unicode metadata", func(t *testing.T) {
		m, assets := contractManifest()
		m.Validator.Name = "Validateur synthétique 🧪"
		dir := filepath.Join(t.TempDir(), "valid")
		capability, err := fhirvalidator.Stage(t.Context(), dir, m, assets)
		if err != nil {
			t.Fatal("valid Unicode metadata refused", err)
		}
		reopened, err := fhirvalidator.OpenCapability(dir)
		if err != nil || reopened.Identity() != capability.Identity() || reopened.Manifest().Validator.Name != m.Validator.Name {
			t.Fatal("valid Unicode metadata changed on reopen", err)
		}
	})
}

// The pinned R4 core ships example ValueSets whose expansions name contained
// systems ("#hacked"). The inventory keeps them as package facts; only a FHIR
// id after '#' is a contained reference, never a path, query or second URL.
func TestFHIRValidatorCapabilityContractInventoriesContainedTerminologyReferences(t *testing.T) {
	for reference, accepted := range map[string]bool{"#hacked": true, "#a.b-9": true, "#": false, "#../x": false, "#a/b": false, "#" + strings.Repeat("a", 65): false, "hacked": false} {
		t.Run(reference, func(t *testing.T) {
			m, assets := contractManifest()
			m.Terminology[0].References = append(m.Terminology[0].References, fhirvalidator.CanonicalLink{URL: reference})
			_, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "capability"), m, assets)
			if accepted != (err == nil) {
				t.Fatalf("contained reference %q accepted=%t: %v", reference, err == nil, err)
			}
		})
	}
}
