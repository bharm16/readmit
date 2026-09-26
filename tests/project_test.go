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
	registered := treeOf(t, filepath.Join(root, "regression"))
	retained := treeOf(t, filepath.Join(packet, "reproducer"))
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

// Customer-derived evidence is the third class the project must tell apart. It
// carries its own provenance mode, and the project copies that — but it is the
// output of a transformation, so it is registered as a revision of the evidence
// it came from rather than as a case standing on its own.
func TestProjectRecordsCustomerDerivedProvenance(t *testing.T) {
	original, derived := redactedRevision(t)
	root := newProject(t)
	copyInto(t, root, "original-case", original)
	copyInto(t, root, "derived-case", derived)
	if _, stderr, err := run(t, "project", "add", root, "original-case", "--title", "Original"); err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	// A transformation is recorded with the identity of what it came from and
	// the operation that produced it, never as a case standing on its own.
	if stdout, _, err := run(t, "project", "add", root, "derived-case", "--title", "Derived for review"); err == nil {
		t.Fatalf("a transformation was registered with no lineage:\n%s", stdout)
	}
	stdout, stderr, err := run(t, "project", "revise", root, "derived-case", "--parent", "original-case")
	if err != nil || stderr != "" {
		t.Fatalf("project revise: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Provenance: derived") || !strings.Contains(stdout, "Schema: readmit-case/v3") {
		t.Fatalf("derived evidence was not recorded as derived:\n%s", stdout)
	}
	if stdout, _, _ = run(t, "project", "show", root); !strings.Contains(stdout, "evidence=verified") || !strings.Contains(stdout, "provenance=derived") {
		t.Fatalf("show did not report the derived revision:\n%s", stdout)
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

// copyInto places an existing artifact directory inside the project under one
// name. A bundle identity covers relative paths and contents only, so a copied
// artifact keeps the identity it was written with.
func copyInto(t *testing.T, root, name, source string) string {
	t.Helper()
	destination := filepath.Join(root, name)
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	return destination
}

// sealed records every byte of the named project entries, so a later comparison
// proves that editing project metadata reached no evidence at all.
func sealed(t *testing.T, root string, names ...string) map[string]map[string][]byte {
	t.Helper()
	files := map[string]map[string][]byte{}
	for _, name := range names {
		files[name] = treeOf(t, filepath.Join(root, name))
	}
	return files
}

func assertSealed(t *testing.T, root string, before map[string]map[string][]byte) {
	t.Helper()
	for name, want := range before {
		got := treeOf(t, filepath.Join(root, name))
		if len(got) != len(want) {
			t.Fatalf("%s gained or lost files: %d then %d", name, len(want), len(got))
		}
		for file, data := range want {
			if !bytes.Equal(got[file], data) {
				t.Fatalf("%s/%s was rewritten by an edit", name, file)
			}
		}
	}
}

// `redact` is the one transformation this release has, so it is what a
// reproducer revision is made with. This returns the original imported case and
// the derived case the review produced.
func redactedRevision(t *testing.T) (string, string) {
	t.Helper()
	request := redactFixture(t)
	stdout, stderr, err := run(t, "redact", request.CasePath, "--spec", request.SpecPath, "--policy", request.PolicyPath, "--inventory", request.InventoryPath, "--local-state", request.LocalState, "--output", request.Output)
	if err != nil {
		t.Fatalf("redact: %v %s %s", err, stdout, stderr)
	}
	return request.CasePath, filepath.Join(request.Output, "case")
}

// A revision records the identity of the evidence it was derived from and the
// operation that produced it, and registering it leaves both bundles untouched.
func TestProjectRevisionRecordsParentIdentityAndOperationManifest(t *testing.T) {
	original, derived := redactedRevision(t)
	root := newProject(t)
	copyInto(t, root, "booking", original)
	copyInto(t, root, "booking-redacted", derived)
	copyInto(t, root, "booking-redacted-again", derived)
	copyInto(t, root, "booking-copy", original)

	added, stderr, err := run(t, "project", "add", root, "booking", "--title", "Duplicate appointment after reschedule")
	if err != nil || stderr != "" {
		t.Fatalf("project add: %v %s", err, stderr)
	}
	parentIdentity := field(t, added, "Identity: ")
	before := sealed(t, root, "booking", "booking-redacted")

	stdout, stderr, err := run(t, "project", "revise", root, "booking-redacted", "--parent", "booking")
	if err != nil || stderr != "" {
		t.Fatalf("project revise: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Revision registered: booking-redacted",
		"Schema: readmit-case/v3",
		"Provenance: derived",
		"Operation: readmit-redact/v1",
		"Parent: booking",
		"Parent identity: " + parentIdentity,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project revise omitted %q:\n%s", want, stdout)
		}
	}
	if identity := field(t, stdout, "Identity: "); identity == parentIdentity {
		t.Fatal("the revision was recorded with the identity of its parent")
	}

	// The editable document is the canonical record of lineage, beside the
	// evidence and never inside it.
	document := readStrictDocument[project.Revisions](t, filepath.Join(root, project.RevisionsDocumentName))
	if document.Schema != project.RevisionsSchema || len(document.Revisions) != 1 {
		t.Fatalf("the stored document is not one revision of readmit-revisions/v1: %+v", document)
	}
	entry := document.Revisions[0]
	if entry.Operation.Name != "readmit-redact/v1" || entry.Operation.Parent != "booking" || entry.Operation.ParentIdentity != parentIdentity {
		t.Fatalf("the operation manifest is not what produced the revision: %+v", entry.Operation)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"evidence that is not a transformation", []string{"booking-copy", "--parent", "booking"}},
		{"a parent this project does not register", []string{"booking-redacted", "--parent", "booking-copy"}},
		{"a parent outside the project", []string{"booking-redacted", "--parent", "../elsewhere"}},
		{"evidence already registered", []string{"booking-redacted", "--parent", "booking"}},
		{"no parent at all", []string{"booking-redacted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := run(t, append([]string{"project", "revise", root}, tc.args...)...)
			if err == nil {
				t.Fatalf("%s was registered as a revision:\n%s", tc.name, stdout)
			}
			for _, secret := range []string{"booking-copy", "elsewhere", root} {
				if strings.Contains(stderr, secret) {
					t.Errorf("a diagnostic echoed %q: %s", secret, stderr)
				}
			}
		})
	}

	// A transformation is registered with its lineage or not at all, and the
	// immutable and editable sides of a project stay disjoint: the same name
	// and the same evidence can never be held by both documents.
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"derived evidence as a plain case", []string{"booking-redacted-again", "--title", "Also a case"}},
		{"a name already registered as a revision", []string{"booking-redacted", "--title", "Also a case"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := run(t, append([]string{"project", "add", root}, tc.args...)...)
			if err == nil {
				t.Fatalf("%s was registered as a case:\n%s", tc.name, stdout)
			}
		})
	}

	assertSealed(t, root, before)
	stdout, stderr, err = run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Cases: 1",
		"Revisions: 1",
		"booking-redacted evidence=verified",
		"schema=readmit-case/v3 provenance=derived",
		"operation=readmit-redact/v1 parent=booking parent_identity=" + parentIdentity,
		"booking evidence=verified",
		"Notes: 0",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project show omitted %q:\n%s", want, stdout)
		}
	}
}

// Notes and drafts are the editable side of a project. Writing one replaces
// only that note, and no note can reach a byte of retained evidence.
func TestProjectNotesAreEditableAndNeverReachEvidence(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression")
	before := sealed(t, root, "regression")
	document, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := run(t, "project", "note", root, "triage", "--subject", "regression", "--title", "Working theory", "--body", "The second S13 keeps the original filler identifier.")
	if err != nil || stderr != "" {
		t.Fatalf("project note: %v %s", err, stderr)
	}
	for _, want := range []string{"Note saved: triage", "Subject: regression", "Title: Working theory", "      The second S13 keeps the original filler identifier."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project note omitted %q:\n%s", want, stdout)
		}
	}
	if _, stderr, err = run(t, "project", "note", root, "backlog", "--title", "Ask the vendor"); err != nil || stderr != "" {
		t.Fatalf("a draft without a subject was refused: %v %s", err, stderr)
	}
	stdout, stderr, err = run(t, "project", "note", root, "triage", "--subject", "regression", "--title", "Confirmed", "--body", "Reproduced against the fixed receiver.")
	if err != nil || stderr != "" {
		t.Fatalf("replacing a note: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Title: Confirmed") {
		t.Fatalf("replacing a note did not store the new title:\n%s", stdout)
	}

	stdout, stderr, err = run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Notes: 2",
		"backlog subject=none",
		"    title: Ask the vendor",
		"    body: none",
		"triage subject=regression",
		"    title: Confirmed",
		"      Reproduced against the fixed receiver.",
		"regression evidence=verified",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project show omitted %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Working theory") {
		t.Fatalf("replacing a note kept the text it replaced:\n%s", stdout)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a note about evidence the project does not register", []string{"stray", "--subject", "not-registered", "--title", "Stray"}},
		{"a note about a path outside the project", []string{"stray", "--subject", "../elsewhere", "--title", "Stray"}},
		{"a note with no title", []string{"untitled", "--body", "text"}},
		{"a note body with a control character", []string{"escaped", "--title", "Escaped", "--body", "one\ttwo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := run(t, append([]string{"project", "note", root}, tc.args...)...)
			if err == nil {
				t.Fatalf("%s was stored:\n%s", tc.name, stdout)
			}
			for _, secret := range []string{"not-registered", "elsewhere", "Stray", root} {
				if strings.Contains(stderr, secret) {
					t.Errorf("a diagnostic echoed %q: %s", secret, stderr)
				}
			}
		})
	}

	// Editing working text is not an edit of evidence, and it is not an edit of
	// the project document either.
	assertSealed(t, root, before)
	after, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil || !bytes.Equal(after, document) {
		t.Fatal("writing a note rewrote the project document")
	}
}

// An interrupted write is retained rather than overwritten, and a document
// written by a later release is reported rather than migrated in place.
func TestProjectRevisionsRecoveryAndUnsupportedVersion(t *testing.T) {
	root := newProject(t)
	registerFrozenCase(t, root, "regression")
	if _, stderr, err := run(t, "project", "note", root, "triage", "--title", "Working theory"); err != nil || stderr != "" {
		t.Fatalf("project note: %v %s", err, stderr)
	}
	stored, err := os.ReadFile(filepath.Join(root, project.RevisionsDocumentName))
	if err != nil {
		t.Fatal(err)
	}

	retained := filepath.Join(root, project.RevisionsIncompleteDocumentName)
	if err := os.WriteFile(retained, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "project", "note", root, "triage", "--title", "Replaced"); err == nil {
		t.Fatal("a retained interrupted write was overwritten")
	}
	if kept, err := os.ReadFile(retained); err != nil || string(kept) != "partial" {
		t.Fatalf("the interrupted write was not retained: %v %s", err, kept)
	}
	if stdout, _, err := run(t, "project", "show", root); err != nil || !strings.Contains(stdout, "title: Working theory") {
		t.Fatalf("reading did not keep working from the intact document: %v\n%s", err, stdout)
	}
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}

	future := bytes.Replace(stored, []byte("readmit-revisions/v1"), []byte("readmit-revisions/v2"), 1)
	if err := os.WriteFile(filepath.Join(root, project.RevisionsDocumentName), future, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"project", "show", root},
		{"project", "note", root, "triage", "--title", "Replaced"},
	} {
		stdout, _, err := run(t, args...)
		if err == nil {
			t.Fatalf("%v accepted a document this release does not read:\n%s", args, stdout)
		}
	}
	unchanged, err := os.ReadFile(filepath.Join(root, project.RevisionsDocumentName))
	if err != nil || !bytes.Equal(unchanged, future) {
		t.Fatal("a document this release does not read was rewritten")
	}
}

// field reads one reported value out of command output.
func field(t *testing.T, out, label string) string {
	t.Helper()
	_, rest, found := strings.Cut(out, label)
	if !found {
		t.Fatalf("output has no %q:\n%s", label, out)
	}
	value, _, _ := strings.Cut(rest, "\n")
	return value
}

// An application-created project is an ordinary project: the command line
// shows it, registers evidence into it, and what the command line registered
// is what the shell's overview then reports verified.
func TestApplicationCreatedProjectReadsThroughTheCommandLine(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)
	created := app.CreateProject("investigation", "Epic scheduling interface", "integration-team", []string{"siu-2.5.1-v1"})
	if created.State != desktop.Empty && created.State != desktop.Completed {
		t.Fatalf("the shell could not create the project: %+v", created)
	}
	root := created.Overview.Root

	stdout, stderr, err := run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	for _, want := range []string{"Project: Epic scheduling interface", "Document: readmit-project/v1", "Interface versions: siu-2.5.1-v1", "Default owner: integration-team", "Cases: 0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project show omitted %q of the application-created project:\n%s", want, stdout)
		}
	}

	registerFrozenCase(t, root, "regression")
	overview := app.OpenProjectOverview(root)
	if overview.State != desktop.Completed || overview.Overview == nil || len(overview.Overview.Cases) != 1 {
		t.Fatalf("the shell did not re-read what the command line registered: %+v", overview)
	}
	registered := overview.Overview.Cases[0]
	if registered.Name != "regression" || registered.Identity != frozenRegressionIdentity || registered.Evidence != "verified" {
		t.Fatalf("the registered case is not the evidence the command line verified: %+v", registered)
	}
}

// A project the window creates from a name alone is a readmit-project/v2
// document the command line reads as exactly that: no interface version
// declared, and a case registered on the command line left with its
// interface version unassigned rather than given one nobody chose.
func TestANamedProjectReadsThroughTheCommandLine(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	if created.State != desktop.Completed || created.Project == nil {
		t.Fatalf("the shell could not create the project: %+v", created)
	}
	root := created.Context.Project
	stdout, stderr, err := run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	for _, want := range []string{"Project: Scheduling QA", "Document: readmit-project/v2", "Cases: 0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("project show omitted %q of the named project:\n%s", want, stdout)
		}
	}
	registerFrozenCase(t, root, "regression")
	cases := app.ListCatalog(desktop.CatalogQuery{Context: created.Context, Kind: desktop.CaseItem})
	if cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatalf("the catalog did not list what the command line registered: %+v", cases)
	}
	registered := cases.Page.Items[0]
	if registered.Summary.Case == nil || !registered.Summary.Case.Registered || registered.Summary.Case.InterfaceVersion != "" ||
		registered.Summary.Case.Evidence != "verified" || registered.Name != "Duplicate appointment after reschedule" {
		t.Fatalf("the registered case: %+v %+v", registered, registered.Summary.Case)
	}
}
