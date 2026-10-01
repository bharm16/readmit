package connectedlab

import (
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

// ValidatorImage is the image ID every lab validator capability names; the
// lab container engine holds exactly this image.
var ValidatorImage = "sha256:" + networkaction.Digest([]byte("readmit-lab-validator-image"))

// StageValidatorCapability stages a synthetic validator capability with the
// pinned validator, runtime and package identities under root/name, as an
// administrator stages the real one. A non-empty variant adds one more staged
// profile revision, so the capability is a different pin of the same
// validator. It returns the capability's directory.
func StageValidatorCapability(t testing.TB, root, name, variant string) string {
	t.Helper()
	return StageValidatorImageCapability(t, root, name, variant, ValidatorImage)
}

// StageValidatorImageCapability stages the same synthetic capability naming
// another worker image, such as one LabImage describes.
func StageValidatorImageCapability(t testing.TB, root, name, variant, image string) string {
	t.Helper()
	assets := map[string][]byte{"metadata/sbom.json": []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"type":"application","name":"synthetic-fixture","version":"1"}]}`), "licenses/fixture.txt": []byte("Synthetic fixture, CC0-1.0"), "metadata/OfflineValidator.java": []byte("synthetic adapter source"), "metadata/adapter.jar": []byte("synthetic adapter binary")}
	pinned := func(id, version, sha string) fhirvalidator.Package {
		return fhirvalidator.Package{ID: id, Version: version, SHA256: sha, License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}
	}
	m := fhirvalidator.Manifest{Schema: fhirvalidator.CapabilitySchema, Platform: "linux/arm64", Image: image, BaseImage: "debian@sha256:" + networkaction.Digest([]byte("base")), LauncherSHA256: networkaction.Digest([]byte("launcher")),
		Validator:         fhirvalidator.Component{Name: "org.hl7.fhir.validation.cli", Version: "6.10.4", SHA256: "1106b9d58f9e363e47bea7c4fc065841e5fc91fe9d062775c3bfdd212bd653cc", License: "Apache-2.0", SBOM: "metadata/sbom.json"},
		Runtime:           fhirvalidator.Component{Name: "Eclipse Temurin", Version: "21.0.12.1+1", SHA256: "14be1f35ebdbd1f6e8d57eb911a3ffb74d6d9aa255abc5daf2b1302002cf2cf2", License: "GPL-2.0-with-classpath-exception", SBOM: "metadata/sbom.json"},
		Base:              fhirvalidator.Component{Name: "Debian bookworm-slim", Version: "12.12", SHA256: networkaction.Digest([]byte("base")), License: "Debian package licenses", SBOM: "metadata/sbom.json"},
		Adapter:           fhirvalidator.Adapter{SourceSHA256: networkaction.Digest(assets["metadata/OfflineValidator.java"]), JarSHA256: networkaction.Digest(assets["metadata/adapter.jar"]), Compiler: fhirvalidator.Component{Name: "Eclipse Temurin JDK", Version: "21.0.12.1+1", SHA256: "23e37e026f12f3e706f18938ff611db3032d075b09d0879a25d06718c773e223", License: "GPL-2.0-only WITH Classpath-exception-2.0", SBOM: "metadata/sbom.json"}},
		Packages:          []fhirvalidator.Package{{ID: "hl7.fhir.r4.core", Version: "4.0.1", SHA256: "ebd7731df7d36b5b7d39d5fb6c9d77b44bb7fe5742f1a2e87f164738c3289d44", License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{}}, pinned("hl7.fhir.xver-extensions", "0.1.0", "f3bb9fa2083402e88a02b41f433655274e8a1cca563211c8f7ba6fd0badf537a"), pinned("hl7.terminology.r4", "6.2.0", "79404c9cc95491fc0155627cd039c401a6eb4748175328131e91b709a41300e2"), pinned("hl7.fhir.uv.extensions.r4", "5.2.0", "b406e75575f05676559d0759770c5939d023ee72fb2ef38e0b3259328487720a")},
		ValidatorPackages: []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}, {ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}, {ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}},
		Profiles:          []fhirvalidator.Canonical{{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Version: "4.0.1", SHA256: networkaction.Digest([]byte("profile")), Package: fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}, Terminology: []fhirvalidator.Terminology{}, Assets: []fhirvalidator.Asset{}}
	if variant != "" {
		m.Profiles = append(m.Profiles, fhirvalidator.Canonical{URL: "http://hl7.org/fhir/StructureDefinition/Appointment", Version: "4.0.1", SHA256: networkaction.Digest([]byte(variant)), Package: fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"}})
	}
	for name, raw := range assets {
		role := map[string]string{"metadata/OfflineValidator.java": "adapter-source", "metadata/adapter.jar": "adapter-binary", "metadata/sbom.json": "sbom"}[name]
		if role == "" {
			role = "license"
		}
		m.Assets = append(m.Assets, fhirvalidator.Asset{Path: name, SHA256: networkaction.Digest(raw), Bytes: int64(len(raw)), Role: role})
	}
	path := filepath.Join(root, name)
	if _, err := fhirvalidator.Stage(t.Context(), path, m, assets); err != nil {
		t.Fatal(err)
	}
	return path
}

// EnableValidation stages the lab capability beside the flow's configuration
// and selects the local engine at socket for every validation check.
func (h *Harness) EnableValidation(socket string) {
	StageValidatorCapability(h.T, h.Root, "validator-capability", "")
	h.Validation = &connectedrun.ValidationSelection{Capability: "validator-capability", Engine: "local", Socket: socket}
}

// Validate is a lab validation of one step's response: structure, profiles,
// invariants and terminology required, fatal and error findings fail.
func Validate(id, step string, timeoutMS int64) connectedtest.ValidationCheck {
	return connectedtest.ValidationCheck{ID: id, Step: step, Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "required", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, TimeoutMS: timeoutMS, MaxOutputBytes: 1 << 20}
}
