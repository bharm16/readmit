package cli

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// The administrator's commands on a machine that never held the validator
// image, in the order docs/fhir-validation.md gives them: verify offline,
// install, check, and remove. A refusal names its state and what to do.
func TestValidatorCommandsInstallAPackageOfflineAndRemoveIt(t *testing.T) {
	staging, target := connectedlab.StartContainerEngine(t, ""), connectedlab.StartContainerEngine(t, "")
	exporter, err := fhirvalidator.LocalEngine(staging.Socket)
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	defer exporter.Close()
	image, archive := connectedlab.LabImage("command-line-install")
	staging.Hold(image, archive)
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	exported, err := exporter.ExportPackage(t.Context(), connectedlab.StageValidatorImageCapability(t, root, "staged", "", image), pkg)
	if err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) (map[string]any, error) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		err := Execute("test", append([]string{"validator"}, args...), &stdout, &stderr)
		answer := map[string]any{}
		if err == nil {
			if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &answer) != nil {
				t.Fatalf("%v answered %q", args, stdout.String())
			}
		}
		return answer, err
	}
	if verified, err := command("verify", pkg, "--identity", exported.Identity); err != nil || verified["validator"] != "6.10.4" || verified["platform"] != "linux/arm64" {
		t.Fatalf("verify: %v %v", verified, err)
	}
	if _, err := command("install", pkg, "--identity", strings.Repeat("0", 64), "--output", filepath.Join(root, "refused"), "--socket", target.Socket); err == nil || !strings.Contains(err.Error(), "untrusted-package") || target.Holds(image) {
		t.Fatalf("an unpublished identity: %v", err)
	}
	installed := filepath.Join(root, "installed")
	if answer, err := command("install", pkg, "--identity", exported.Identity, "--output", installed, "--socket", target.Socket); err != nil || answer["state"] != "ready" || !target.Holds(image) {
		t.Fatalf("install: %v %v", answer, err)
	}
	if answer, err := command("check", installed, "--socket", target.Socket); err != nil || answer["state"] != "ready" {
		t.Fatalf("check: %v %v", answer, err)
	}
	if answer, err := command("remove", installed, "--socket", target.Socket); err != nil || answer["state"] != "removed" || target.Holds(image) {
		t.Fatalf("remove: %v %v", answer, err)
	}
	if _, err := command("check", installed, "--socket", target.Socket); err == nil || !strings.Contains(err.Error(), "capability-unavailable") {
		t.Fatalf("check after removal: %v", err)
	}
}
