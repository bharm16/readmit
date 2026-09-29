package desktop_test

// The suite documents a workspace holds, as the listing names them, and the
// fixture a suite of the workspace is built from.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

const suiteFixture = `{"schema":"readmit-suite/v1","id":"nightly","owner":"interop","tags":["siu"],"parallelism":2,` +
	`"environments":[{"id":"east","site":"hospital-a","bindings":[{"parameter":"interface","target":"east.json"}]}],` +
	`"tables":[{"id":"patients","rows":[{"id":"one","case":"case-one"}]}],` +
	`"tests":[{"id":"booking","spec":"booking.json","owner":"scheduling","tags":["smoke"],"parameter":"interface",` +
	`"table":"patients","isolation":"shared","sequence":["s0001-e000001"]}]}`

// writeSuiteFixture builds the suite fixture inside a folder an app will open.
func writeSuiteFixture(t *testing.T, root string) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(root, "case-one"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: "127.0.0.1:1", Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}
	targetRaw, _ := json.Marshal(target)
	writeDocument(t, root, "east.json", string(targetRaw))
	value := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "booking", Input: testrunner.Input{Case: "unbound", Messages: []string{"s0001-e000001"}}, Target: "unbound", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture deliberately"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}}}
	specRaw, _ := json.Marshal(spec)
	writeDocument(t, root, "booking.json", string(specRaw))
	// The suite file keeps non-canonical formatting, the way the command line
	// or an operator wrote it: opening must not require canonical bytes.
	writeDocument(t, root, "suite.json", suiteFixture)
}

// suiteApp opens a fresh app over a folder holding the suite fixture.
func suiteApp(t *testing.T) (*desktop.App, string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeSuiteFixture(t, root)
	app := workspaceApp(t)
	if result := app.OpenWorkspace(root); result.State != desktop.Completed {
		t.Fatalf("open workspace: %+v", result)
	}
	return app, root
}

// The listing tells the suite pickers which suite artifact each entry
// declares, from its declared contract or its directory marker and never from
// its name, so a prepared suite is never offered as a suite definition or a
// coverage document. The discriminator admits nothing: the coverage reader
// still refuses a suite definition named as a coverage document.
func TestListingNamesEachSuiteArtifactByItsDeclaredContract(t *testing.T) {
	app, root := suiteApp(t)
	if _, err := suite.Prepare(suite.Request{Path: filepath.Join(root, "suite.json"), Environment: "east", Output: filepath.Join(root, "prepared")}); err != nil {
		t.Fatal(err)
	}
	declared, err := suite.BuildCoverage(filepath.Join(root, "prepared"), []suite.Requirement{{ID: "accept-booking", Jobs: []string{"booking-one"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "declared.json", string(declared))
	writeDocument(t, root, "pins.json", `{"schema":"readmit-suite-releases/v1","tests":[]}`)
	// A file whose name says coverage but whose contract is a suite is a suite.
	writeDocument(t, root, "coverage-looking.json", suiteFixture)
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed || listing.Workspace == nil {
		t.Fatalf("%+v", listing)
	}
	roles := map[string]desktop.SuiteRole{}
	for _, artifact := range listing.Workspace.Artifacts {
		roles[artifact.Name] = artifact.Role
	}
	want := map[string]desktop.SuiteRole{
		"suite.json":            desktop.SuiteDefinitionRole,
		"coverage-looking.json": desktop.SuiteDefinitionRole,
		"prepared":              desktop.PreparedSuiteRole,
		"declared.json":         desktop.SuiteCoverageRole,
		"pins.json":             desktop.SuiteReleasesRole,
		"booking.json":          "",
		"case-one":              "",
	}
	for name, role := range want {
		if roles[name] != role {
			t.Fatalf("%s: role %q, want %q (listing %+v)", name, roles[name], role, listing.Workspace.Artifacts)
		}
	}
	if _, err := suite.AssessCoverage(t.Context(), filepath.Join(root, "prepared"), filepath.Join(root, "coverage-looking.json"), nil, time.Now()); err == nil {
		t.Fatal("a suite definition must not assess as a coverage document")
	}
}

func marshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func replaceOnce(t *testing.T, text, from, to string) string {
	t.Helper()
	index := indexOf(text, from)
	if index < 0 {
		t.Fatal("fixture string not found")
	}
	return text[:index] + to + text[index+len(from):]
}

func indexOf(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
