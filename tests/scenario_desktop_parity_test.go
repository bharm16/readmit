package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/scenariogen"
)

// Desktop scenario generation and `readmit scenario generate` must retain the
// same generation.json for the same plan. The generated case is a desktop
// handoff artifact; stream bytes and the generation record stay CLI-identical.
func TestDesktopScenarioGenerateMatchesCLI(t *testing.T) {
	planPath := filepath.Join("..", "testdata", "fixtures", "scenario-generator.json")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	cliRoot := t.TempDir()
	cliFamily := filepath.Join(cliRoot, "family")
	if _, err := scenariogen.Write(t.Context(), cliFamily, plan); err != nil {
		t.Fatal(err)
	}
	cliGen, err := os.ReadFile(filepath.Join(cliFamily, "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(t, "scenario", "generate", planPath, "--output", filepath.Join(cliRoot, "cli-family")); err != nil || stderr != "" {
		t.Fatalf("cli generate: %v %s", err, stderr)
	}
	cliCmdGen, err := os.ReadFile(filepath.Join(cliRoot, "cli-family", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cliGen) != string(cliCmdGen) {
		t.Fatal("scenariogen.Write and readmit scenario generate disagreed")
	}

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "plan.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	app := desktopApp(t, workspace)
	got := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace:  workspace,
		Document:   "plan.json",
		OutputName: "family",
		CaseName:   "generated-case",
	})
	if got.State != desktop.Completed || got.ProvenanceMode != "generated" {
		t.Fatalf("desktop generate: %+v", got)
	}
	deskGen, err := os.ReadFile(filepath.Join(workspace, "family", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(deskGen) != string(cliGen) {
		t.Fatal("desktop generation.json disagrees with CLI for the same plan")
	}
	opened := app.OpenCase(workspace, "generated-case")
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Provenance != "generated" {
		t.Fatalf("generated case: %+v", opened)
	}
}
