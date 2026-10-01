package fhirvalidator_test

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// The opt-in deployment qualification moves capability packages through the
// actual local container engine: export from the engine, removal of the image
// as from a machine that never held it, offline verification and installation,
// an upgrade beside the current capability, and removal. The images are small
// throwaway images built offline for this run, never the qualified worker
// image, which TestFHIRValidatorLiveQualification qualifies. A skipped
// developer test is not a deployment result.
func TestFHIRValidatorLiveDeployment(t *testing.T) {
	if os.Getenv("READMIT_FHIR_VALIDATOR_DEPLOYMENT") != "1" {
		t.Skip("explicit local deployment qualification not requested")
	}
	engine, err := fhirvalidator.LocalEngine("")
	if err != nil {
		t.Fatal("select the local engine:", err)
	}
	defer engine.Close()
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	run := hex.EncodeToString(suffix)
	build := func(name string) string {
		t.Helper()
		context := t.TempDir()
		if err := os.WriteFile(filepath.Join(context, "marker"), []byte(name+" "+run+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(context, "Dockerfile"), []byte("FROM scratch\nCOPY marker /marker\nLABEL readmit.deployment.qualification="+run+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := liveDocker(t, "build", "--quiet", "--network", "none", "--pull=false", "--tag", "readmit-deployment-"+name+":"+run, context); err != nil {
			t.Fatal("build a throwaway image offline:", err, string(out))
		}
		raw, err := liveDocker(t, "image", "inspect", "readmit-deployment-"+name+":"+run, "--format", "{{.Id}}")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(raw))
	}
	type step struct {
		Step  string `json:"step"`
		State string `json:"state"`
	}
	steps := []step{}
	record := func(name string, err error) {
		t.Helper()
		state := stateOf(err)
		if state == "" {
			state = "completed"
		}
		steps = append(steps, step{name, state})
	}
	root := t.TempDir()
	current, next := build("current"), build("next")
	t.Cleanup(func() {
		for _, image := range []string{current, next} {
			_, _ = liveDocker(t, "image", "rm", "--force", image)
		}
	})
	exportOne := func(name, image, variant string) *fhirvalidator.VerifiedPackage {
		t.Helper()
		capability := connectedlab.StageValidatorImageCapability(t, root, name+"-staged", variant, image)
		p, err := engine.ExportPackage(t.Context(), capability, filepath.Join(root, name+"-package"))
		record("export "+name, err)
		if err != nil {
			t.Fatal("export:", err)
		}
		// The installing machine never held the image.
		if _, err := liveDocker(t, "image", "rm", "--force", image); err != nil {
			t.Fatal(err)
		}
		return p
	}
	currentPackage := exportOne("current", current, "")
	nextPackage := exportOne("next", next, "next")
	_, err = engine.InstallPackage(t.Context(), filepath.Join(root, "current-package"), "0000000000000000000000000000000000000000000000000000000000000000", filepath.Join(root, "refused"))
	record("install with an unpublished identity", err)
	if stateOf(err) != "untrusted-package" {
		t.Fatal("an unpublished identity was installed:", err)
	}
	install := func(name string, p *fhirvalidator.VerifiedPackage) string {
		t.Helper()
		if _, err := fhirvalidator.VerifyPackage(filepath.Join(root, name+"-package"), p.Identity); err != nil {
			t.Fatal("offline verification:", err)
		}
		installed := filepath.Join(root, name+"-installed")
		_, err := engine.InstallPackage(t.Context(), filepath.Join(root, name+"-package"), p.Identity, installed)
		record("install "+name, err)
		if err != nil {
			t.Fatal("install:", err)
		}
		c, err := fhirvalidator.OpenCapability(installed)
		if err != nil || engine.Check(t.Context(), c).State != "ready" {
			t.Fatal("the installed capability is not ready", err)
		}
		return installed
	}
	installedCurrent := install("current", currentPackage)
	installedNext := install("next", nextPackage)
	err = engine.RemoveInstalled(t.Context(), installedCurrent, installedNext)
	record("remove the previous capability, keeping the upgrade", err)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := liveDocker(t, "image", "inspect", current); err == nil {
		t.Fatal("the previous image remains installed")
	}
	c, err := fhirvalidator.OpenCapability(installedNext)
	if err != nil || engine.Check(t.Context(), c).State != "ready" {
		t.Fatal("the upgrade stopped working", err)
	}
	err = engine.RemoveInstalled(t.Context(), installedNext)
	record("remove the last capability", err)
	if err != nil || engine.Check(t.Context(), c).State != "worker-missing" {
		t.Fatal("removal left the image ready", err)
	}
	if path := os.Getenv("READMIT_FHIR_VALIDATOR_DEPLOYMENT_RECEIPT"); path != "" {
		raw, err := liveDocker(t, "info", "--format", "{{json .}}")
		if err != nil {
			t.Fatal(err)
		}
		var info struct {
			ServerVersion, OSType, Architecture, Driver string
		}
		if err := json.Unmarshal(raw, &info, json.RejectUnknownMembers(false)); err != nil {
			t.Fatal(err)
		}
		receipt, _ := json.Marshal(map[string]any{"schema": "readmit-fhir-validator-deployment/v1", "host": runtime.GOOS + "/" + runtime.GOARCH, "engine": info.ServerVersion, "os": info.OSType, "architecture": info.Architecture, "storage": info.Driver, "package": fhirvalidator.PackageSchema, "steps": steps}, json.Deterministic(true))
		if err := os.WriteFile(path, append(receipt, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
