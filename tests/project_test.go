package tests

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// frozenRegressionIdentity is the identity of the `regression` case of the
// frozen readmit-synth-v1 reference vector, quoted from docs/synth-v1-vector.md.
// It was calculated from the literal fixtures, never by running readmit.
const frozenRegressionIdentity = "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"

// newProject creates a project directory and returns its path.
func newProject(t *testing.T, extra ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "investigation")
	args := append([]string{"project", "init", "--output", root, "--title", "Epic scheduling interface", "--interface-version", "siu-2.5.1-v1"}, extra...)
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("project init: %v %s", err, stderr)
	}
	for _, want := range []string{"Project created: Epic scheduling interface", "Document: readmit-project/v1", "Interface versions: siu-2.5.1-v1", "Cases: 0"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("project init omitted %q:\n%s", want, stdout)
		}
	}
	return root
}

// placeFrozenCase copies the frozen regression case of the reference vector
// into the project under the given name. A case bundle identity covers relative
// paths and contents only, so a copied case keeps the identity it was written
// with; this test depends on exactly that.
func placeFrozenCase(t *testing.T, root, name string) string {
	t.Helper()
	family := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(family))
	destination := filepath.Join(root, name)
	if err := os.CopyFS(destination, os.DirFS(filepath.Join(family, "regression"))); err != nil {
		t.Fatal(err)
	}
	return destination
}

func registerFrozenCase(t *testing.T, root, name string, extra ...string) string {
	t.Helper()
	placeFrozenCase(t, root, name)
	args := append([]string{"project", "add", root, name, "--title", "Duplicate appointment after reschedule"}, extra...)
	stdout, stderr, err := run(t, args...)
	if err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	return stdout
}

// A project records interface versions, cases, titles, tags, ownership, status
// and linked incidents, and reports the case identity the shared reader
// verified rather than anything derived from a name.
func TestProjectRecordsCaseMetadataAgainstVerifiedEvidence(t *testing.T) {
	root := newProject(t, "--owner", "integration-team")
	stdout := registerFrozenCase(t, root, "regression", "--tag", "scheduling", "--tag", "duplicate", "--incident", "INC-4821")
	for _, want := range []string{
		"Case registered: regression",
		"Identity: " + frozenRegressionIdentity,
		"Schema: readmit-case/v1",
		"Provenance: generated",
		"Interface version: siu-2.5.1-v1",
		"Status: open",
		"Owner: integration-team",
		"Tags: duplicate, scheduling",
		"Incidents: INC-4821",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project add omitted %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, err := run(t, "project", "update", root, "regression", "--status", "investigating", "--owner", "scheduling-team", "--incident", "INC-4821", "--incident", "INC-5090")
	if err != nil || stderr != "" {
		t.Fatalf("project update: %v %s", err, stderr)
	}
	for _, want := range []string{"Case updated: regression", "Status: investigating", "Owner: scheduling-team", "Incidents: INC-4821, INC-5090", "Tags: duplicate, scheduling"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project update omitted %q:\n%s", want, stdout)
		}
	}

	stdout, stderr, err = run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Project: Epic scheduling interface",
		"Document: readmit-project/v1",
		"Interface versions: siu-2.5.1-v1",
		"Cases: 1",
		"regression evidence=verified identity=" + frozenRegressionIdentity,
		"status=investigating",
		"provenance=generated",
		"title: Duplicate appointment after reschedule",
		"tags: duplicate, scheduling",
		"incidents: INC-4821, INC-5090",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project show omitted %q:\n%s", want, stdout)
		}
	}

	// The document on disk is the canonical record, and it carries exactly what
	// the shared reader accepted about the evidence.
	document, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := project.Decode(document)
	if err != nil {
		t.Fatalf("the written document is not a readable project: %v", err)
	}
	entry := decoded.Cases[0]
	if entry.Identity != frozenRegressionIdentity || entry.Provenance != "generated" || entry.Schema != "readmit-case/v1" {
		t.Fatalf("the document did not record the verified evidence facts: %+v", entry)
	}
	if entry.Status != project.StatusInvestigating || entry.Owner != "scheduling-team" {
		t.Fatalf("the document did not record the managed metadata: %+v", entry)
	}
}

// Project-level settings supply the defaults a case inherits, and declaring a
// further interface version keeps the earlier one usable.
func TestProjectSettingsSupplyDefaultsAndDeclareInterfaceVersions(t *testing.T) {
	root := newProject(t, "--owner", "integration-team")
	stdout, stderr, err := run(t, "project", "settings", root, "--title", "Epic scheduling interface, 2026", "--interface-version", "siu-2.5.1-v2", "--default-interface-version", "siu-2.5.1-v2", "--owner", "scheduling-team")
	if err != nil || stderr != "" {
		t.Fatalf("project settings: %v %s", err, stderr)
	}
	for _, want := range []string{"Project settings updated: Epic scheduling interface, 2026", "Interface versions: siu-2.5.1-v1, siu-2.5.1-v2", "Default interface version: siu-2.5.1-v2", "Default owner: scheduling-team"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project settings omitted %q:\n%s", want, stdout)
		}
	}
	// A case registered without an explicit version or owner inherits both.
	stdout = registerFrozenCase(t, root, "regression")
	for _, want := range []string{"Interface version: siu-2.5.1-v2", "Owner: scheduling-team"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the project settings did not supply %q:\n%s", want, stdout)
		}
	}
	// The earlier declared version is still selectable.
	placeFrozenCase(t, root, "second")
	if _, stderr, err := run(t, "project", "add", root, "second", "--title", "Second", "--interface-version", "siu-2.5.1-v1"); err == nil || stderr == "" {
		t.Fatalf("registering the same evidence twice was accepted: %v", err)
	}
}

// Everything a project refuses is refused with a bounded diagnostic that never
// echoes a path, a title, or an argument.
func TestProjectRefusesUnusableRequests(t *testing.T) {
	root := newProject(t)
	placeFrozenCase(t, root, "regression")

	if _, stderr, err := run(t, "project", "init", "--output", root, "--title", "Again", "--interface-version", "v1"); err == nil || stderr == "" {
		t.Fatal("an existing project directory was overwritten")
	}
	if _, stderr, err := run(t, "project", "init", "--output", filepath.Join(root, "regression", "nested"), "--title", "Inside", "--interface-version", "v1"); err == nil || stderr == "" {
		t.Fatal("a project was created inside retained case evidence")
	}
	if _, stderr, err := run(t, "project", "init", "--output", filepath.Join(t.TempDir(), "p"), "--title", "No versions"); err == nil || stderr == "" {
		t.Fatal("a project was created with no declared interface version")
	}
	for _, entry := range []string{"../regression", "regression/payloads", "absent", ".", "/etc"} {
		if _, stderr, err := run(t, "project", "add", root, entry, "--title", "Traversal"); err == nil || stderr == "" {
			t.Errorf("case entry %q was accepted", entry)
		}
	}
	if _, stderr, err := run(t, "project", "add", root, "regression", "--title", "Wrong", "--interface-version", "undeclared"); err == nil || stderr == "" {
		t.Fatal("an undeclared interface version was accepted")
	}
	if _, stderr, err := run(t, "project", "add", root, "regression", "--title", "Wrong", "--status", "wontfix"); err == nil || stderr == "" {
		t.Fatal("an unknown case status was accepted")
	}
	if _, _, err := run(t, "project", "add", root, "regression", "--title", "Duplicate appointment"); err != nil {
		t.Fatalf("registering a verified case failed: %v", err)
	}
	if _, stderr, err := run(t, "project", "add", root, "regression", "--title", "Twice"); err == nil || stderr == "" {
		t.Fatal("the same case was registered twice")
	}
	if _, stderr, err := run(t, "project", "update", root, "absent", "--status", "closed"); err == nil || stderr == "" {
		t.Fatal("an unregistered case was updated")
	}
	if _, stderr, err := run(t, "project", "update", root, "regression"); err == nil || stderr == "" {
		t.Fatal("an update that changes nothing was accepted")
	}
	if _, stderr, err := run(t, "project", "show", t.TempDir()); err == nil || stderr == "" {
		t.Fatal("a folder with no project document was shown as a project")
	}

	// Damaged evidence is never registered.
	damaged := placeFrozenCase(t, root, "damaged")
	if err := os.WriteFile(filepath.Join(damaged, "identity.sha256"), []byte("0000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "project", "add", root, "damaged", "--title", "Damaged")
	if err == nil || stderr == "" {
		t.Fatal("unverifiable evidence was registered")
	}
	for _, private := range []string{root, "Damaged", "Duplicate appointment", "identity.sha256", "wontfix"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatalf("a project diagnostic echoed %q: %s %s", private, stdout, stderr)
		}
	}
}

// A registered case whose evidence is no longer the evidence that was verified
// is reported as changed. It is never silently re-identified and never shown as
// if it had been verified.
func TestProjectShowReportsEvidenceThatNoLongerMatchesItsRecordedIdentity(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression")

	replacement := filepath.Join(t.TempDir(), "family")
	createSynth(t, synthArgs(replacement))
	if err := os.RemoveAll(filepath.Join(root, "regression")); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(filepath.Join(root, "regression"), os.DirFS(filepath.Join(replacement, "cancellation"))); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "evidence=changed") || strings.Contains(stdout, "evidence=verified") {
		t.Fatalf("replaced evidence was not reported as changed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "identity="+frozenRegressionIdentity) {
		t.Fatalf("the recorded identity was rewritten to match the new evidence:\n%s", stdout)
	}

	// Evidence that cannot be verified at all, and evidence that is gone, are
	// each reported as their own state rather than as a verified case.
	if err := os.WriteFile(filepath.Join(root, "regression", "identity.sha256"), []byte("0000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if stdout, _, _ = run(t, "project", "show", root); !strings.Contains(stdout, "evidence=unreadable") {
		t.Fatalf("damaged evidence was not reported as unreadable:\n%s", stdout)
	}
	if err := os.RemoveAll(filepath.Join(root, "regression")); err != nil {
		t.Fatal(err)
	}
	if stdout, _, _ = run(t, "project", "show", root); !strings.Contains(stdout, "evidence=missing") {
		t.Fatalf("absent evidence was not reported as missing:\n%s", stdout)
	}
}

// An interrupted write is retained, reported, and never overwritten, and the
// intact document keeps reading until the retained file is moved aside.
func TestProjectReportsAndRecoversFromAnInterruptedWrite(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression")
	retained := filepath.Join(root, project.IncompleteDocumentName)
	if err := os.WriteFile(retained, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "project", "update", root, "regression", "--status", "closed")
	if err == nil || stderr == "" {
		t.Fatal("an interrupted write was silently discarded")
	}
	if strings.Contains(stdout+stderr, root) {
		t.Fatalf("the recovery diagnostic echoed a path: %s", stderr)
	}
	if stdout, _, err := run(t, "project", "show", root); err != nil || !strings.Contains(stdout, "status=open") {
		t.Fatalf("the intact document stopped reading: %v\n%s", err, stdout)
	}
	if data, err := os.ReadFile(retained); err != nil || string(data) != "partial" {
		t.Fatalf("the retained incomplete document was changed: %q %v", data, err)
	}
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(t, "project", "update", root, "regression", "--status", "closed"); err != nil || stderr != "" {
		t.Fatalf("recovery did not restore writing: %v %s", err, stderr)
	}
}

// One case has one identity. The desktop facade, the command line, the project
// document, and an exported artifact must all name the same value for the same
// evidence; if any of them derived its own, this fails.
func TestSameCaseIdentityAcrossDesktopCommandLineAndExportedArtifacts(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression")

	// The desktop facade verifies the case through the shared reader.
	app := desktopApp(t, root)
	opened := app.OpenCase(root, "regression")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the desktop facade did not verify the case: %+v", opened)
	}
	identity := opened.Case.Identity

	// The desktop facade reads the same project document the command line wrote.
	shown := app.OpenProject(root)
	if shown.State != desktop.Completed || shown.Project == nil {
		t.Fatalf("the desktop facade did not open the project: %+v", shown)
	}
	if len(shown.Project.Cases) != 1 || shown.Project.Cases[0].Identity != identity {
		t.Fatalf("the project the shell reads names a different identity: %+v", shown.Project.Cases)
	}

	// The command line reports the same identity for the same evidence.
	timeline, stderr, err := run(t, "timeline", filepath.Join(root, "regression"))
	if err != nil || stderr != "" {
		t.Fatalf("timeline: %v %s", err, stderr)
	}
	if !strings.Contains(timeline, "Bundle: "+identity) {
		t.Fatalf("the command line named a different identity:\n%s", timeline)
	}
	show, stderr, err := run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	if !strings.Contains(show, "identity="+identity) {
		t.Fatalf("the project document named a different identity:\n%s", show)
	}

	// An exported artifact carries the same identity for the same evidence. The
	// sealed packet retains its input case, so this is the same evidence rather
	// than two values that happen to agree: the retained case is compared byte
	// for byte with the case this project registered before its identity is.
	packet := filepath.Join(t.TempDir(), "packet")
	if _, stderr, err := run(t, "report", "--scenario", "siu-reschedule-v1", "--output", packet); err != nil || stderr != "" {
		t.Fatalf("report: %v %s", err, stderr)
	}
	registered := synthTree(t, filepath.Join(root, "regression"))
	retained := synthTree(t, filepath.Join(packet, "reproducer"))
	if len(registered) != len(retained) {
		t.Fatalf("the packet retained %d files for a registered case of %d", len(retained), len(registered))
	}
	for name, want := range registered {
		if got, present := retained[name]; !present || !bytes.Equal(got, want) {
			t.Fatalf("the exported packet does not retain the registered case: %s", name)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(packet, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		InputIdentity string `json:"input_identity"`
	}
	if err := json.Unmarshal(manifest, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.InputIdentity != identity {
		t.Fatalf("the exported packet named %s for the case the project records as %s", exported.InputIdentity, identity)
	}
	// The retained case's own completion marker names it too, so the packet's
	// manifest is not the only place the exported identity appears.
	marker, err := os.ReadFile(filepath.Join(packet, "reproducer", "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != identity+"\n" {
		t.Fatalf("the retained case names %q for the case the project records as %s", marker, identity)
	}
	if identity != frozenRegressionIdentity {
		t.Fatalf("every surface agreed on %s, which is not the independently authored identity", identity)
	}
}

// Synthetically generated, imported and customer-derived evidence are told
// apart by the provenance the bundle itself carries. The project copies that
// mode from the verified manifest; no flag sets it and no name implies it.
func TestProjectRecordsTheProvenanceTheEvidenceDeclares(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "generated-case")

	imported := filepath.Join(root, "imported-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", imported); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "project", "add", root, "imported-case", "--title", "Imported from a customer export")
	if err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Provenance: imported") {
		t.Fatalf("imported evidence was not recorded as imported:\n%s", stdout)
	}
	// There is no flag that sets provenance.
	if _, stderr, err := run(t, "project", "update", root, "imported-case", "--provenance", "generated"); err == nil || stderr == "" {
		t.Fatal("provenance could be declared on the command line")
	}
	stdout, stderr, err = run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "imported-case evidence=verified") || !strings.Contains(stdout, "provenance=imported") {
		t.Fatalf("show did not separate the two provenance modes:\n%s", stdout)
	}
	if !strings.Contains(stdout, "generated-case evidence=verified") || !strings.Contains(stdout, "provenance=generated") {
		t.Fatalf("show did not keep the generated case distinct:\n%s", stdout)
	}
}

// Customer-derived evidence is the third class the project must tell apart. A
// derived case carries its own provenance mode, and the project copies that.
func TestProjectRecordsCustomerDerivedProvenance(t *testing.T) {
	request := redactFixture(t)
	if _, stderr, err := run(t, "redact", request.CasePath, "--spec", request.SpecPath, "--policy", request.PolicyPath, "--inventory", request.InventoryPath, "--local-state", request.LocalState, "--output", request.Output); err != nil || stderr != "" {
		t.Fatalf("redact: %v %s", err, stderr)
	}
	root := newProject(t)
	if err := os.CopyFS(filepath.Join(root, "derived-case"), os.DirFS(filepath.Join(request.Output, "case"))); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "project", "add", root, "derived-case", "--title", "Derived for review")
	if err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Provenance: derived") || !strings.Contains(stdout, "Schema: readmit-case/v3") {
		t.Fatalf("derived evidence was not recorded as derived:\n%s", stdout)
	}
	if stdout, _, _ = run(t, "project", "show", root); !strings.Contains(stdout, "evidence=verified") || !strings.Contains(stdout, "provenance=derived") {
		t.Fatalf("show did not report the derived case:\n%s", stdout)
	}
}

// An empty value clears a set. Beside a real value it is a typo, and a typo
// never silently changes what a project records.
func TestProjectRefusesAnEmptyValueBesideARealOne(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression", "--tag", "scheduling")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", filepath.Join(root, "second")); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	// Registering and updating read an empty value the same way.
	for _, args := range [][]string{
		{"project", "update", root, "regression", "--tag", "", "--tag", "duplicate"},
		{"project", "update", root, "regression", "--incident", "INC-1", "--incident", ""},
		{"project", "add", root, "second", "--title", "Second", "--tag", "", "--tag", "duplicate"},
		{"project", "add", root, "second", "--title", "Second", "--incident", "INC-1", "--incident", ""},
	} {
		if stdout, stderr, err := run(t, args...); err == nil || stderr == "" {
			t.Fatalf("an empty value beside a real one was accepted: %v %s", err, stdout)
		}
	}
	stdout, _, _ := run(t, "project", "show", root)
	if !strings.Contains(stdout, "tags: scheduling") {
		t.Fatalf("a refused update changed the project:\n%s", stdout)
	}
	if strings.Contains(stdout, "second") {
		t.Fatalf("a refused registration still registered the case:\n%s", stdout)
	}
}

// A recorded evidence fact that the evidence does not support is never reported
// as verified, even when the bundle identity still matches. A hand-edited
// document cannot make imported evidence look synthetic.
func TestProjectShowRefusesRecordedFactsTheEvidenceDoesNotSupport(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"provenance", `"provenance":"generated"`, `"provenance":"imported"`},
		{"contract version", `"schema":"readmit-case/v1"`, `"schema":"readmit-case/v4"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t)
			registerFrozenCase(t, root, "regression")
			document := filepath.Join(root, "project.json")
			data, err := os.ReadFile(document)
			if err != nil {
				t.Fatal(err)
			}
			edited := strings.Replace(string(data), tc.from, tc.to, 1)
			if edited == string(data) {
				t.Fatalf("the document does not record %s as %s", tc.name, tc.from)
			}
			if err := os.WriteFile(document, []byte(edited), 0600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := run(t, "project", "show", root)
			if err != nil || stderr != "" {
				t.Fatalf("project show: %v %s", err, stderr)
			}
			if strings.Contains(stdout, "evidence=verified") {
				t.Fatalf("an edited %s was reported as verified evidence:\n%s", tc.name, stdout)
			}
			if !strings.Contains(stdout, "evidence=changed") {
				t.Fatalf("an edited %s was not reported as changed:\n%s", tc.name, stdout)
			}
		})
	}
}

// Tags and linked incidents are managed metadata, so a case that was tagged by
// mistake can be cleared again.
func TestProjectUpdateClearsTagsAndLinkedIncidents(t *testing.T) {
	root := newProject(t, "--owner", "integration-team")
	registerFrozenCase(t, root, "regression", "--tag", "scheduling", "--incident", "INC-4821")
	stdout, stderr, err := run(t, "project", "update", root, "regression", "--tag", "", "--incident", "", "--owner", "")
	if err != nil || stderr != "" {
		t.Fatalf("project update: %v %s", err, stderr)
	}
	for _, want := range []string{"Tags: none", "Incidents: none", "Owner: none"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("clearing did not report %q:\n%s", want, stdout)
		}
	}
	if stdout, _, _ = run(t, "project", "show", root); !strings.Contains(stdout, "tags: none") || !strings.Contains(stdout, "incidents: none") {
		t.Fatalf("the cleared metadata came back:\n%s", stdout)
	}
	// Registering treats an explicitly empty value the same way, so the two
	// commands do not disagree about what an empty tag means.
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/case-evidence.mllp", "--output", filepath.Join(root, "second")); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	stdout, stderr, err = run(t, "project", "add", root, "second", "--title", "Second", "--tag", "")
	if err != nil || stderr != "" {
		t.Fatalf("project add with an empty tag: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Tags: none") {
		t.Fatalf("registering with an empty tag did not report none:\n%s", stdout)
	}
}
