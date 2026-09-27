package fhirvalidator_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

func fixtureManifest() (fhirvalidator.Manifest, map[string][]byte) {
	assets := map[string][]byte{"metadata/sbom.json": []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"type":"application","name":"synthetic-fixture","version":"1"}]}`), "licenses/fixture.txt": []byte("Synthetic fixture, CC0-1.0")}
	m := fhirvalidator.Manifest{Schema: fhirvalidator.CapabilitySchema, Platform: "linux/arm64", Image: "sha256:" + networkaction.Digest([]byte("immutable-image")), BaseImage: "debian@sha256:" + networkaction.Digest([]byte("base")), LauncherSHA256: networkaction.Digest([]byte("launcher")), Validator: fhirvalidator.Component{Name: "org.hl7.fhir.validation.cli", Version: "6.10.4", SHA256: "1106b9d58f9e363e47bea7c4fc065841e5fc91fe9d062775c3bfdd212bd653cc", License: "Apache-2.0", SBOM: "metadata/sbom.json"}, Runtime: fhirvalidator.Component{Name: "Eclipse Temurin", Version: "21.0.12.1+1", SHA256: "14be1f35ebdbd1f6e8d57eb911a3ffb74d6d9aa255abc5daf2b1302002cf2cf2", License: "GPL-2.0-with-classpath-exception", SBOM: "metadata/sbom.json"}, Packages: []fhirvalidator.Package{{ID: "hl7.fhir.r4.core", Version: "4.0.1", SHA256: "ebd7731df7d36b5b7d39d5fb6c9d77b44bb7fe5742f1a2e87f164738c3289d44", License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{}}}, Profiles: []fhirvalidator.Canonical{{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Version: "4.0.1", SHA256: networkaction.Digest([]byte("profile")), Package: fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}, Terminology: []fhirvalidator.Terminology{}, Assets: []fhirvalidator.Asset{}}
	m.Base = fhirvalidator.Component{Name: "Debian bookworm-slim", Version: "12.12", SHA256: networkaction.Digest([]byte("base")), License: "Debian package licenses", SBOM: "metadata/sbom.json"}
	assets["metadata/OfflineValidator.java"] = []byte("synthetic adapter source")
	assets["metadata/adapter.jar"] = []byte("synthetic adapter binary")
	m.Adapter = fhirvalidator.Adapter{SourceSHA256: networkaction.Digest(assets["metadata/OfflineValidator.java"]), JarSHA256: networkaction.Digest(assets["metadata/adapter.jar"]), Compiler: fhirvalidator.Component{Name: "Eclipse Temurin JDK", Version: "21.0.12.1+1", SHA256: "23e37e026f12f3e706f18938ff611db3032d075b09d0879a25d06718c773e223", License: "GPL-2.0-only WITH Classpath-exception-2.0", SBOM: "metadata/sbom.json"}}
	for name, raw := range assets {
		role := "license"
		if name == "metadata/OfflineValidator.java" {
			role = "adapter-source"
		}
		if name == "metadata/adapter.jar" {
			role = "adapter-binary"
		}
		if name == "metadata/sbom.json" {
			role = "sbom"
		}
		m.Assets = append(m.Assets, fhirvalidator.Asset{Path: name, SHA256: networkaction.Digest(raw), Bytes: int64(len(raw)), Role: role})
	}
	return m, assets
}
func TestFHIRValidatorCapabilityStagesExactClosureWithoutRuntime(t *testing.T) {
	m, assets := fixtureManifest()
	dir := filepath.Join(t.TempDir(), "capability")
	cap, e := fhirvalidator.Stage(context.Background(), dir, m, assets)
	if e != nil {
		t.Fatal(e)
	}
	reopened, e := fhirvalidator.OpenCapability(dir)
	if e != nil || reopened.Identity() != cap.Identity() {
		t.Fatal("offline capability reopen", e)
	}
	base := m.Base
	m.Base.SHA256 = networkaction.Digest([]byte("another base"))
	if _, e := fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "rebased"), m, assets); e == nil {
		t.Fatal("a base component that is not the pinned base image staged")
	}
	m.Base = base
	m.Packages[0].Dependencies = []fhirvalidator.PackageRef{{ID: "missing.package", Version: "1.0.0"}}
	if _, e := fhirvalidator.Stage(context.Background(), filepath.Join(t.TempDir(), "incomplete"), m, assets); e == nil {
		t.Fatal("missing package dependency staged")
	}
}

func fixturePinnedPackage(ref fhirvalidator.PackageRef) fhirvalidator.Package {
	pins := map[string]string{"hl7.fhir.xver-extensions": "f3bb9fa2083402e88a02b41f433655274e8a1cca563211c8f7ba6fd0badf537a", "hl7.terminology.r4": "79404c9cc95491fc0155627cd039c401a6eb4748175328131e91b709a41300e2", "hl7.fhir.uv.extensions.r4": "b406e75575f05676559d0759770c5939d023ee72fb2ef38e0b3259328487720a"}
	return fhirvalidator.Package{ID: ref.ID, Version: ref.Version, SHA256: pins[ref.ID], License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}
}

// The administrator's staging step reads the build's metadata folder only.
func TestFHIRValidatorStageBuildReadsOnlyTheBuildFolder(t *testing.T) {
	m, assets := contractManifest()
	build := t.TempDir()
	for name, raw := range assets {
		if err := os.MkdirAll(filepath.Join(build, filepath.Dir(name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(build, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(build, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := fhirvalidator.StageBuild(t.Context(), build, filepath.Join(t.TempDir(), "capability"))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := fhirvalidator.Stage(t.Context(), filepath.Join(t.TempDir(), "direct"), m, assets)
	if err != nil || direct.Identity() != staged.Identity() {
		t.Fatal("staging a build differs from staging its manifest", err)
	}
	outside := filepath.Join(t.TempDir(), "sbom.json")
	if err := os.WriteFile(outside, assets["metadata/sbom.json"], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(build, "metadata/sbom.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(build, "metadata/sbom.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := fhirvalidator.StageBuild(t.Context(), build, filepath.Join(t.TempDir(), "escaped")); err == nil {
		t.Fatal("a member linked outside the build folder was staged")
	}
}
