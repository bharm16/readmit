package desktop_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/replay"
)

// fhirValidatorEnvironment saves a FHIR environment whose validation selects
// the capability folder and engine socket given.
func fhirValidatorEnvironment(t *testing.T, app *desktop.App, context desktop.RequestContext, intent string, validation *desktop.ConnectionValidation) desktop.ItemRef {
	t.Helper()
	draft := desktop.ItemDraft{Name: "FHIR with validation", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: "https://qa.invalid/fhir", ServerName: "qa.invalid", Classification: replay.Unclassified, Authentication: "none", Validation: validation}}
	return saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: draft, IntentID: "validator-" + intent})
}

// Check validator is the explicit capability check: it reads the selected
// capability's pins and asks the selected local engine whether it holds the
// exact worker image, the same check the deployment command and a validation
// make, and answers each state with what to do. Nothing is fetched.
func TestCheckValidatorAnswersTheInstalledCapabilityStateAsTheDeploymentCheckDoes(t *testing.T) {
	image, archive := connectedlab.LabImage("desktop-validator")
	lab := connectedlab.StartContainerEngine(t, "")
	if _, err := fhirvalidator.LocalEngine(lab.Socket); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	capability := connectedlab.StageValidatorImageCapability(t, t.TempDir(), "capability", "", image)
	app, context := namedProject(t)
	check := func(ref desktop.ItemRef) *desktop.ValidatorCheck {
		t.Helper()
		answer := app.CheckValidator(desktop.ItemRequest{Context: context, Ref: ref})
		if answer.State != desktop.Completed || answer.Check == nil {
			t.Fatalf("check: %+v", answer)
		}
		if answer.Check.State != "ready" && answer.Check.Requirement == "" {
			t.Fatalf("an unready validator says nothing to do: %+v", answer.Check)
		}
		return answer.Check
	}
	missing := fhirValidatorEnvironment(t, app, context, "selected", &desktop.ConnectionValidation{Capability: capability, Engine: "local", Socket: lab.Socket})
	if got := check(missing); got.State != "worker-missing" || got.Validator != "6.10.4" || got.Runtime != "21.0.12.1+1" || len(got.Packages) != 4 {
		t.Fatalf("an engine without the image: %+v", got)
	}
	lab.Hold(image, archive)
	if got := check(missing); got.State != "ready" || got.Requirement != "" {
		t.Fatalf("an installed image: %+v", got)
	}
	// The deployment command's check reads the same state.
	engine, err := fhirvalidator.LocalEngine(lab.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	opened, err := fhirvalidator.OpenCapability(capability)
	if err != nil || engine.Check(t.Context(), opened).State != "ready" {
		t.Fatal("the shared engine check disagrees", err)
	}
	lab.SetArchitecture("x86_64")
	if got := check(missing); got.State != "unsupported-runtime" {
		t.Fatalf("an unqualified platform: %+v", got)
	}
	none := fhirValidatorEnvironment(t, app, context, "none", nil)
	if got := check(none); got.State != "not-configured" {
		t.Fatalf("no validation selected: %+v", got)
	}
	gone := fhirValidatorEnvironment(t, app, context, "gone", &desktop.ConnectionValidation{Capability: filepath.Join(t.TempDir(), "never-installed"), Engine: "local", Socket: lab.Socket})
	if got := check(gone); got.State != "capability-unavailable" {
		t.Fatalf("a capability folder that is not installed: %+v", got)
	}
	v2 := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "Engine input", Environment: &replay.Target{Schema: replay.TargetSchemaV3, Address: "127.0.0.1:2575", Classification: replay.Nonproduction, Transport: "plain", ConnectTimeout: "5s", MessageTimeout: "5s", MaxACKBytes: 4096}}, IntentID: "validator-v2"})
	if answer := app.CheckValidator(desktop.ItemRequest{Context: context, Ref: v2}); answer.State != desktop.Failed || answer.Check != nil {
		t.Fatalf("a v2 environment has no validator: %+v", answer)
	}
}

// Install validator verifies a package against its published identity and
// installs it into the selected engine and this application's storage, as
// `readmit validator install` does, then saves the connection selecting it;
// the command line's check reads the same installed validator ready. Remove
// takes the image and folder away again and saves the connection without it.
func TestInstallValidatorInstallsAPackageAsTheCommandLineDoesAndRemoveTakesItAway(t *testing.T) {
	image, archive := connectedlab.LabImage("desktop-install")
	staging := connectedlab.StartContainerEngine(t, "")
	if _, err := fhirvalidator.LocalEngine(staging.Socket); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	staging.Hold(image, archive)
	exporter, _ := fhirvalidator.LocalEngine(staging.Socket)
	defer exporter.Close()
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	exported, err := exporter.ExportPackage(t.Context(), connectedlab.StageValidatorImageCapability(t, root, "staged", "", image), pkg)
	if err != nil {
		t.Fatal(err)
	}
	target := connectedlab.StartContainerEngine(t, "")
	app, context := namedProject(t)
	ref := fhirValidatorEnvironment(t, app, context, "install", &desktop.ConnectionValidation{Engine: "local", Socket: target.Socket})
	refused := app.InstallValidator(desktop.ValidatorInstallRequest{Context: context, Ref: ref, Package: pkg, Identity: strings.Repeat("0", 64), IntentID: "install-untrusted"})
	if refused.State != desktop.Completed || refused.Check == nil || refused.Check.State != "untrusted-package" || refused.Saved != nil || target.Holds(image) {
		t.Fatalf("an unpublished identity: %+v %+v", refused, refused.Check)
	}
	installed := app.InstallValidator(desktop.ValidatorInstallRequest{Context: context, Ref: ref, Package: pkg, Identity: exported.Identity, IntentID: "install"})
	if installed.State != desktop.Completed || installed.Check == nil || installed.Check.State != "ready" || installed.Saved == nil || !target.Holds(image) {
		t.Fatalf("install: %+v %+v", installed, installed.Check)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *installed.Saved})
	if opened.Draft == nil || opened.Draft.FHIR.Validation == nil || opened.Draft.FHIR.Validation.Socket != target.Socket {
		t.Fatalf("the saved connection: %+v", opened)
	}
	capability := opened.Draft.FHIR.Validation.Capability
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("test", []string{"validator", "check", "--socket", target.Socket, capability}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), `"state":"ready"`) {
		t.Fatalf("the command line's check of the installed validator: %v %s %s", err, stdout.String(), stderr.String())
	}
	// A removal that cannot save the connection removes nothing.
	if stale := app.RemoveValidator(desktop.ValidatorRemoveRequest{Context: context, Ref: ref, IntentID: "remove-stale"}); stale.State != desktop.Failed || !target.Holds(image) {
		t.Fatalf("a removal from a stale revision: %+v", stale)
	}
	if _, err := os.Stat(capability); err != nil {
		t.Fatal("a refused removal removed the installed folder")
	}
	if blank := app.RemoveValidator(desktop.ValidatorRemoveRequest{Context: context, Ref: *installed.Saved}); blank.State != desktop.Failed || !target.Holds(image) {
		t.Fatalf("a removal naming no submission: %+v", blank)
	}
	if blank := app.InstallValidator(desktop.ValidatorInstallRequest{Context: context, Ref: *installed.Saved, Package: pkg, Identity: exported.Identity}); blank.State != desktop.Failed {
		t.Fatalf("an installation naming no submission: %+v", blank)
	}
	removed := app.RemoveValidator(desktop.ValidatorRemoveRequest{Context: context, Ref: *installed.Saved, IntentID: "remove"})
	if removed.State != desktop.Completed || removed.Check == nil || removed.Check.State != "not-configured" || removed.Saved == nil || target.Holds(image) {
		t.Fatalf("remove: %+v %+v", removed, removed.Check)
	}
	if _, err := os.Stat(capability); !os.IsNotExist(err) {
		t.Fatal("the installed folder remains")
	}
	if again := app.RemoveValidator(desktop.ValidatorRemoveRequest{Context: context, Ref: *removed.Saved, IntentID: "remove-again"}); again.State != desktop.Failed {
		t.Fatalf("removing what is not installed: %+v", again)
	}
}
