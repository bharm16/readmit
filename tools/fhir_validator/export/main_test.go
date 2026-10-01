package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// The build machine's export, as docs/fhir-validation.md runs it, answers the
// identity a receiving machine verifies the package against offline.
func TestExportWritesAPackageItsPrintedIdentityVerifies(t *testing.T) {
	staging := connectedlab.StartContainerEngine(t, "")
	if _, err := fhirvalidator.LocalEngine(staging.Socket); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("the container command line is required in CI:", err)
		}
		t.Skip("no container command line is installed:", err)
	}
	image, archive := connectedlab.LabImage("export-command")
	staging.Hold(image, archive)
	root := t.TempDir()
	capability := connectedlab.StageValidatorImageCapability(t, root, "staged", "", image)
	pkg := filepath.Join(root, "package")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--socket", staging.Socket, "--capability", capability, "--output", pkg}, &stdout, &stderr); code != 0 {
		t.Fatalf("export exited %d: %s", code, stderr.String())
	}
	var answer map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &answer); err != nil || answer["image"] != image {
		t.Fatalf("export answered %q", stdout.String())
	}
	if _, err := fhirvalidator.VerifyPackage(pkg, answer["package"]); err != nil {
		t.Fatal("the printed identity does not verify the package:", err)
	}
	stderr.Reset()
	if code := run([]string{"--socket", staging.Socket, "--capability", capability, "--output", pkg}, &stdout, &stderr); code != 1 {
		t.Fatalf("an existing package folder was exported over: %d %s", code, stderr.String())
	}
	if code := run([]string{"--capability", capability}, &stdout, &stderr); code != 2 {
		t.Fatalf("a missing output: %d", code)
	}
}
