package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioGeneratePublicInterface(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "generated")
	out, stderr, err := run(t, "scenario", "generate", "../testdata/fixtures/scenario-generator.json", "--output", destination)
	if err != nil || stderr != "" || !strings.Contains(out, "Generated synthetic workflow streams") {
		t.Fatalf("generate: %q %q %v", out, stderr, err)
	}
	if strings.Contains(out, "Café") || strings.Contains(out, "SYNTH-PATIENT") {
		t.Fatal("generation output leaked values")
	}
	before, err := os.ReadFile(filepath.Join(destination, "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = run(t, "scenario", "generate", "../testdata/fixtures/scenario-generator.json", "--output", destination)
	if err == nil {
		t.Fatal("CLI overwrote generation")
	}
	after, err := os.ReadFile(filepath.Join(destination, "generation.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed repeat changed completed family")
	}
	for _, args := range [][]string{{"scenario", "generate"}, {"scenario", "generate", "../testdata/fixtures/scenario-generator.json"}} {
		if _, _, err := run(t, args...); err == nil {
			t.Fatal("missing required input accepted")
		}
	}
}
