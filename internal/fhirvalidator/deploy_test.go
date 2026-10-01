package fhirvalidator_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
)

// deployLab is one machine's container engine: a lab engine holding nothing
// but the images a test gives it, reached through the installed command line.
func deployLab(t *testing.T) (*connectedlab.ContainerEngine, *fhirvalidator.Engine) {
	t.Helper()
	lab := connectedlab.StartContainerEngine(t, "")
	engine, err := fhirvalidator.LocalEngine(lab.Socket)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	t.Cleanup(engine.Close)
	return lab, engine
}

// exported is a package an administrator exported on a staging machine whose
// engine holds the image named, for a capability staged against it.
func exported(t *testing.T, name, variant string) (string, *fhirvalidator.VerifiedPackage, string) {
	t.Helper()
	image, archive := connectedlab.LabImage(name)
	staging, engine := deployLab(t)
	staging.Hold(image, archive)
	root := t.TempDir()
	capability := connectedlab.StageValidatorImageCapability(t, root, "capability", variant, image)
	dir := filepath.Join(root, "package")
	p, err := engine.ExportPackage(t.Context(), capability, dir)
	if err != nil {
		t.Fatal("export:", err)
	}
	return dir, p, image
}

func stateOf(err error) string {
	var status fhirvalidator.Status
	if errors.As(err, &status) && status.Requirement != "" {
		return status.State
	}
	if err == nil {
		return ""
	}
	return "unactionable: " + err.Error()
}

// A package moves the capability and its exact image to a machine whose
// engine has never held it, with no network: the identity published with the
// package is compared first, and the capability is written only once the
// engine holds the image it names.
func TestFHIRValidatorPackageInstallsOfflineOnAMachineWithoutTheImage(t *testing.T) {
	dir, p, image := exported(t, "validator-one", "")
	if p.Manifest.Schema != fhirvalidator.PackageSchema || p.Manifest.Image != image || p.Manifest.Platform != "linux/arm64" || len(p.Identity) != 64 {
		t.Fatalf("package manifest: %+v", p)
	}
	verified, err := fhirvalidator.VerifyPackage(dir, p.Identity)
	if err != nil || verified.Capability.Identity() != p.Capability.Identity() {
		t.Fatal("offline verification:", err)
	}
	target, engine := deployLab(t)
	if target.Holds(image) {
		t.Fatal("the target engine already held the image")
	}
	installed := filepath.Join(t.TempDir(), "installed")
	c, err := engine.InstallPackage(t.Context(), dir, p.Identity, installed)
	if err != nil {
		t.Fatal("install:", err)
	}
	if !target.Holds(image) || c.Identity() != p.Capability.Identity() {
		t.Fatal("the installed capability is not the packaged one")
	}
	reopened, err := fhirvalidator.OpenCapability(installed)
	if err != nil || reopened.Identity() != p.Capability.Identity() {
		t.Fatal("the installed capability does not reopen under its seal", err)
	}
	if status := engine.Check(t.Context(), reopened); status.State != "ready" {
		t.Fatalf("installed capability check: %+v", status)
	}
}

// Each refusal names what to do and leaves the target engine and folder as
// they were.
func TestFHIRValidatorPackageRefusesUntrustedChangedOrIncompletePackages(t *testing.T) {
	dir, p, image := exported(t, "validator-refusals", "")
	copyPackage := func(t *testing.T, change func(string)) string {
		t.Helper()
		copied := filepath.Join(t.TempDir(), "package")
		if err := os.CopyFS(copied, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
		if change != nil {
			change(copied)
		}
		return copied
	}
	other, _ := connectedlab.LabImage("another-image")
	for _, tc := range []struct {
		name, identity, want string
		change               func(string)
	}{
		{name: "unpublished identity", identity: networkaction.Digest([]byte("someone else's package")), want: "untrusted-package"},
		{name: "short identity", identity: "abc", want: "untrusted-package"},
		{name: "changed manifest", want: "untrusted-package", change: func(d string) {
			raw, _ := os.ReadFile(filepath.Join(d, "package.json"))
			_ = os.WriteFile(filepath.Join(d, "package.json"), []byte(strings.Replace(string(raw), image, other, 1)), 0o600)
		}},
		{name: "missing image archive", want: "package-invalid", change: func(d string) { _ = os.Remove(filepath.Join(d, "image.tar")) }},
		{name: "changed image archive", want: "package-invalid", change: func(d string) {
			raw, _ := os.ReadFile(filepath.Join(d, "image.tar"))
			raw[len(raw)-1] ^= 1
			_ = os.WriteFile(filepath.Join(d, "image.tar"), raw, 0o600)
		}},
		{name: "another image's archive", want: "package-invalid", change: func(d string) {
			_, archive := connectedlab.LabImage("another-image")
			_ = os.WriteFile(filepath.Join(d, "image.tar"), archive, 0o600)
		}},
		{name: "altered capability", want: "package-invalid", change: func(d string) {
			_ = os.WriteFile(filepath.Join(d, "capability", "licenses", "fixture.txt"), []byte("changed"), 0o600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copied := copyPackage(t, tc.change)
			identity := tc.identity
			if identity == "" {
				identity = p.Identity
			}
			if _, err := fhirvalidator.VerifyPackage(copied, identity); stateOf(err) != tc.want {
				t.Fatalf("verify: %v, want %s", err, tc.want)
			}
			target, engine := deployLab(t)
			installed := filepath.Join(t.TempDir(), "installed")
			if _, err := engine.InstallPackage(t.Context(), copied, identity, installed); stateOf(err) != tc.want {
				t.Fatalf("install: %v, want %s", err, tc.want)
			}
			if target.Holds(image) {
				t.Fatal("a refused package was loaded into the engine")
			}
			if _, err := os.Lstat(installed); !os.IsNotExist(err) {
				t.Fatal("a refused package staged a capability")
			}
		})
	}
	t.Run("existing folder", func(t *testing.T) {
		target, engine := deployLab(t)
		existing := t.TempDir()
		if _, err := engine.InstallPackage(t.Context(), dir, p.Identity, existing); stateOf(err) != "capability-exists" || target.Holds(image) {
			t.Fatalf("install over an existing folder: %v", err)
		}
	})
}

// A capability whose pins this release does not run is refused before
// anything is exported or installed: the wrong validator and Java, and a
// missing pinned package root.
func TestFHIRValidatorPackageRefusesUnqualifiedPinsAndPlatforms(t *testing.T) {
	image, archive := connectedlab.LabImage("validator-pins")
	for _, tc := range []struct {
		name, want string
		edit       func(*fhirvalidator.Manifest)
	}{
		{name: "wrong validator", want: "unsupported-runtime", edit: func(m *fhirvalidator.Manifest) { m.Validator.Version = "6.10.5" }},
		{name: "wrong Java", want: "unsupported-runtime", edit: func(m *fhirvalidator.Manifest) { m.Runtime.Version = "17.0.14+7" }},
		{name: "missing package root", want: "package-unavailable", edit: func(m *fhirvalidator.Manifest) { m.ValidatorPackages = m.ValidatorPackages[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			staging, engine := deployLab(t)
			staging.Hold(image, archive)
			m, assets := contractManifest()
			m.Image = image
			tc.edit(&m)
			capability := filepath.Join(t.TempDir(), "capability")
			if _, err := fhirvalidator.Stage(t.Context(), capability, m, assets); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "package")
			if _, err := engine.ExportPackage(t.Context(), capability, output); stateOf(err) != tc.want {
				t.Fatalf("export: %v, want %s", err, tc.want)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("a refused export left a package")
			}
		})
	}
	t.Run("unqualified engine platform", func(t *testing.T) {
		dir, p, image := exported(t, "validator-platform", "")
		target, engine := deployLab(t)
		target.SetArchitecture("x86_64")
		installed := filepath.Join(t.TempDir(), "installed")
		if _, err := engine.InstallPackage(t.Context(), dir, p.Identity, installed); stateOf(err) != "unsupported-runtime" || target.Holds(image) {
			t.Fatalf("install on x86_64: %v", err)
		}
	})
	// A command line that reaches no engine may still print its own half of
	// the answer and succeed, depending on its version; either way the engine
	// is unavailable, not an unsupported platform, on any host.
	t.Run("engine answering without a server", func(t *testing.T) {
		dir, p, image := exported(t, "validator-serverless", "")
		target, engine := deployLab(t)
		target.AnswerWithoutServer()
		if _, err := engine.InstallPackage(t.Context(), dir, p.Identity, filepath.Join(t.TempDir(), "installed")); stateOf(err) != "worker-unavailable" || target.Holds(image) {
			t.Fatalf("install with no server in the answer: %v", err)
		}
		c, err := fhirvalidator.OpenCapability(filepath.Join(dir, "capability"))
		if err != nil {
			t.Fatal(err)
		}
		if status := engine.Check(t.Context(), c); status.State != "worker-unavailable" {
			t.Fatalf("check with no server in the answer: %+v", status)
		}
	})
	t.Run("unreachable engine", func(t *testing.T) {
		dir, p, _ := exported(t, "validator-unreachable", "")
		target, engine := deployLab(t)
		_ = os.Remove(target.Socket)
		if _, err := engine.InstallPackage(t.Context(), dir, p.Identity, filepath.Join(t.TempDir(), "installed")); stateOf(err) != "worker-unavailable" {
			t.Fatalf("install with no engine: %v", err)
		}
	})
}

// An upgrade installs the next capability beside the current one; removing
// the old one removes only its image and folder, a capability kept installed
// keeps an image it shares, and a retained capability copy, as a validation
// result keeps one, still reopens offline.
func TestFHIRValidatorUpgradeAndRemovalKeepTheCurrentCapabilityAndRetainedCopies(t *testing.T) {
	oldDir, oldPackage, oldImage := exported(t, "validator-old", "")
	newDir, newPackage, newImage := exported(t, "validator-new", "next")
	sharedDir, sharedPackage, _ := exported(t, "validator-new", "shared")
	target, engine := deployLab(t)
	root := t.TempDir()
	install := func(dir, identity, name string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if _, err := engine.InstallPackage(t.Context(), dir, identity, path); err != nil {
			t.Fatal("install", name, err)
		}
		return path
	}
	old := install(oldDir, oldPackage.Identity, "old")
	retained := filepath.Join(t.TempDir(), "retained")
	if err := os.CopyFS(retained, os.DirFS(old)); err != nil {
		t.Fatal(err)
	}
	current := install(newDir, newPackage.Identity, "current")
	shared := install(sharedDir, sharedPackage.Identity, "shared")
	if err := engine.RemoveInstalled(t.Context(), old, current, shared); err != nil {
		t.Fatal("remove the old capability:", err)
	}
	if target.Holds(oldImage) || !target.Holds(newImage) {
		t.Fatal("removal did not remove exactly the old image")
	}
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatal("the old capability folder remains")
	}
	if err := engine.RemoveInstalled(t.Context(), shared, current); err != nil || !target.Holds(newImage) {
		t.Fatal("removing a capability removed an image another installed one names", err)
	}
	c, err := fhirvalidator.OpenCapability(current)
	if err != nil || engine.Check(t.Context(), c).State != "ready" {
		t.Fatal("the current capability stopped working", err)
	}
	if reopened, err := fhirvalidator.OpenCapability(retained); err != nil || reopened.Identity() != oldPackage.Capability.Identity() {
		t.Fatal("a retained capability copy no longer reopens offline", err)
	}
	if err := engine.RemoveInstalled(t.Context(), filepath.Join(root, "missing")); stateOf(err) != "capability-unavailable" {
		t.Fatalf("removing what is not a capability: %v", err)
	}
	if err := engine.RemoveInstalled(t.Context(), current); err != nil || target.Holds(newImage) {
		t.Fatal("removing the last capability kept its image", err)
	}
	if engine.Check(t.Context(), c).State != "worker-missing" {
		t.Fatal("a removed capability's image still checks ready")
	}
}
