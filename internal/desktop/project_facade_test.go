package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
)

// createdProject creates a project through the facade's native create flow and
// returns the created root and the overview it reported. A fresh project holds
// nothing yet, so the state is Empty: the operation succeeded, and the
// overview is the answer a person acts from.
func createdProject(t *testing.T, app *desktop.App, parent string) (string, *desktop.ProjectOverview) {
	t.Helper()
	result := app.CreateProject("scheduling-investigation", "Epic scheduling interface", "integration-team", []string{"siu-2.5.1-v1", "siu-2.5.1-v2"})
	if (result.State != desktop.Completed && result.State != desktop.Empty) || result.Overview == nil {
		t.Fatalf("create project: %+v", result)
	}
	root := result.Overview.Root
	if filepath.Base(root) != "scheduling-investigation" || filepath.Dir(root) != resolved(t, parent) {
		t.Fatalf("the project was not created in the chosen folder: %s", root)
	}
	return root, result.Overview
}

// The native create flow writes the same document the command line writes, and
// what it returns is read back from disk rather than from the request.
func TestCreateProjectWritesTheSharedDocumentAndReadsItBack(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, overview := createdProject(t, app, parent)

	if overview.Title != "Epic scheduling interface" || overview.DefaultOwner != "integration-team" {
		t.Fatalf("the overview does not report the settings that were written: %+v", overview)
	}
	if len(overview.InterfaceVersions) != 2 || overview.DefaultVersion != "siu-2.5.1-v1" {
		t.Fatalf("the first declared version is not the default: %+v", overview.InterfaceVersions)
	}
	// What the application created reads through the same project reader the
	// command line opens, under the same contract.
	opened, err := project.Open(root)
	if err != nil {
		t.Fatalf("the created project does not open as a project: %v", err)
	}
	if opened.Document.Settings.Title != "Epic scheduling interface" || opened.Document.Settings.DefaultInterfaceVersion != "siu-2.5.1-v1" {
		t.Fatalf("the stored document is not what the flow recorded: %+v", opened.Document.Settings)
	}
	// The state is Empty, not Completed, because nothing is registered yet.
	if result := app.OpenProjectOverview(root); result.State != desktop.Empty || result.Overview == nil {
		t.Fatalf("an empty project is a state a person acts from, not a refusal: %+v", result)
	}
}

func TestCreateProjectRefusalsLeaveTheChosenFolderAsItWas(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	if result := app.CreateProject("", "Title", "", []string{"v1"}); result.State != desktop.Failed || !strings.Contains(result.Reason, "folder name") {
		t.Fatalf("an unnamed project was not refused by name: %+v", result)
	}
	if result := app.CreateProject("new-project", "Title", "", nil); result.State != desktop.Failed || !strings.Contains(result.Reason, "at least one interface version") {
		t.Fatalf("a project without an interface version was not refused by name: %+v", result)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("a refused create left something behind: %v %v", entries, err)
	}
	// A dismissed dialog is cancelled, like every other dialog flow.
	dismissed := newApp(t, &chooser{})
	if result := dismissed.CreateProject("new-project", "Title", "", []string{"v1"}); result.State != desktop.Cancelled {
		t.Fatalf("a dismissed dialog was not cancelled: %+v", result)
	}
}

func TestCreateProjectRefusesAnExistingDestination(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	before := bytesUnder(t, root)

	retry := app.CreateProject("scheduling-investigation", "Different title", "", []string{"siu-2.5.1-v1"})
	if retry.State != desktop.Failed || retry.Overview != nil {
		t.Fatalf("an existing destination was not refused: %+v", retry)
	}
	if after := bytesUnder(t, root); len(before) != len(after) || string(before["project.json"]) != string(after["project.json"]) {
		t.Fatal("a refused create changed the project already there")
	}
}

func TestCreateProjectReportsBusyAndRecovers(t *testing.T) {
	parent := t.TempDir()
	reentrant := &chooser{folder: parent}
	app := activatedApp(t, reentrant, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"))
	var concurrent desktop.ProjectOverviewResult
	reentrant.before = func() { concurrent = app.CreateProject("other", "Title", "", []string{"v1"}) }
	if first := app.CreateProject("first", "Title", "", []string{"v1"}); first.State != desktop.Completed && first.State != desktop.Empty {
		t.Fatalf("the first create did not complete: %+v", first)
	}
	if concurrent.State != desktop.Busy || concurrent.Overview != nil {
		t.Fatalf("a second create ran while the first held the facade: %+v", concurrent)
	}
}

func TestRegisterCaseRecordsWhatTheSharedReaderAccepted(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	written := writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))

	result := app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{
		Title: "Duplicate appointment after reschedule",
		Tags:  []string{"duplicate", "scheduling"},
	})
	if result.State != desktop.Completed || result.Overview == nil {
		t.Fatalf("register case: %+v", result)
	}
	if len(result.Overview.Cases) != 1 {
		t.Fatalf("the overview the registration returned does not carry the case: %+v", result.Overview)
	}
	registered := result.Overview.Cases[0]
	if registered.Identity != written.Identity || registered.Schema != written.Manifest.Schema {
		t.Fatalf("the project did not record what the reader accepted: %+v", registered)
	}
	if registered.Provenance != "imported" {
		t.Fatalf("the provenance mode was not read from the evidence: %+v", registered)
	}
	// Defaults the registration left empty are inherited from the project.
	if registered.Status != "open" || registered.Owner != "integration-team" || registered.InterfaceVersion != "siu-2.5.1-v1" {
		t.Fatalf("the project defaults were not inherited: %+v", registered)
	}
	if registered.Evidence != "verified" {
		t.Fatalf("freshly registered evidence is not verified: %+v", registered)
	}
	// The stored document is what the command line reads back.
	opened, err := project.Open(root)
	if err != nil || len(opened.Document.Cases) != 1 || opened.Document.Cases[0].Identity != written.Identity {
		t.Fatalf("the stored project document does not hold the registered case: %v %+v", err, opened)
	}
}

func TestRegisterCaseRefusalsAreReportedInWords(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))

	// A case is named by one directory entry of the project.
	if result := app.RegisterCase(root, "../escape", desktop.CaseRegistration{}); result.State != desktop.Failed || result.Overview != nil {
		t.Fatalf("a path outside the project was not refused: %+v", result)
	}
	// An unknown status is the project's own refusal, not a silent rewrite.
	if result := app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment", Status: "ended"}); result.State != desktop.Failed || !strings.Contains(result.Reason, "status") {
		t.Fatalf("an unknown status was not refused by name: %+v", result)
	}
	// The registration the project refused changed nothing.
	if result := app.OpenProjectOverview(root); result.State != desktop.Empty || len(result.Overview.Cases) != 0 {
		t.Fatalf("a refused registration changed the project: %+v", result)
	}
	// Registering twice is refused under the same name.
	app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment"})
	if again := app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment"}); again.State != desktop.Failed || !strings.Contains(again.Reason, "already registered") {
		t.Fatalf("a second registration under one name was not refused: %+v", again)
	}
}

func TestUpdateRegisteredCaseChangesOnlyWhatItNames(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment", Tags: []string{"scheduling"}})

	status := project.StatusInvestigating
	result := app.UpdateRegisteredCase(root, "incident-4821", desktop.CaseChange{Status: &status})
	if result.State != desktop.Completed || result.Overview == nil {
		t.Fatalf("update case: %+v", result)
	}
	updated := result.Overview.Cases[0]
	if updated.Status != "investigating" {
		t.Fatalf("the status was not changed: %+v", updated)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "scheduling" {
		t.Fatalf("changing the status restated the tags: %+v", updated.Tags)
	}
	if updated.Evidence != "verified" {
		t.Fatalf("a metadata edit changed what the evidence is: %+v", updated)
	}
	// An unknown status is refused in the project's own words.
	ended := project.Status("ended")
	if refused := app.UpdateRegisteredCase(root, "incident-4821", desktop.CaseChange{Status: &ended}); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "status") {
		t.Fatalf("an unknown status change was not refused by name: %+v", refused)
	}
}

func TestProjectOverviewReVerifiesRegisteredEvidence(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment"})

	if result := app.OpenProjectOverview(root); result.State != desktop.Completed || result.Overview.Cases[0].Evidence != "verified" {
		t.Fatalf("registered evidence did not verify: %+v", result)
	}
	// Evidence replaced under the same name is changed, never silently
	// re-identified: the recorded identity is the one the project keeps. A
	// case bundle is written once, so the replaced bundle is a new directory
	// over the old one.
	if err := os.RemoveAll(filepath.Join(root, "incident-4821")); err != nil {
		t.Fatal(err)
	}
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|2|P|2.5.1\r"))
	if result := app.OpenProjectOverview(root); result.Overview.Cases[0].Evidence != "changed" {
		t.Fatalf("replaced evidence was not reported as changed: %+v", result.Overview.Cases[0])
	}
	// Evidence that is gone is missing, and the project still reports what it
	// recorded exactly as recorded.
	if err := os.RemoveAll(filepath.Join(root, "incident-4821")); err != nil {
		t.Fatal(err)
	}
	result := app.OpenProjectOverview(root)
	if result.State != desktop.Completed || result.Overview.Cases[0].Evidence != "missing" || result.Overview.Cases[0].Identity == "" {
		t.Fatalf("missing evidence was not reported beside the recorded facts: %+v", result.Overview.Cases[0])
	}
}

func TestUpdateProjectSettingsDeclaresAndApplies(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)

	owner := "scheduling-team"
	version := "siu-2.5.1-v2"
	result := app.UpdateProjectSettings(root, desktop.SettingsChange{
		Title:                   strPtr("Epic scheduling interface, 2026"),
		DefaultOwner:            &owner,
		DefaultInterfaceVersion: &version,
		DeclareVersions:         &[]string{"siu-2.5.1-v2"},
	})
	if result.State != desktop.Completed || result.Overview == nil {
		t.Fatalf("update settings: %+v", result)
	}
	overview := result.Overview
	if overview.Title != "Epic scheduling interface, 2026" || overview.DefaultOwner != "scheduling-team" || overview.DefaultVersion != "siu-2.5.1-v2" {
		t.Fatalf("the settings were not applied: %+v", overview)
	}
	if len(overview.InterfaceVersions) != 2 || overview.InterfaceVersions[1] != "siu-2.5.1-v2" {
		t.Fatalf("the further version was not declared: %+v", overview.InterfaceVersions)
	}
	// The default must be a declared version; a typo invents nothing.
	undeclared := "siu-2.6.0-v1"
	if refused := app.UpdateProjectSettings(root, desktop.SettingsChange{DefaultInterfaceVersion: &undeclared}); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "declared") {
		t.Fatalf("an undeclared default was not refused by name: %+v", refused)
	}
	// Declaring an already-declared version declares it once.
	same := "siu-2.5.1-v2"
	again := app.UpdateProjectSettings(root, desktop.SettingsChange{DeclareVersions: &[]string{"siu-2.5.1-v2", same}})
	if again.State != desktop.Completed || len(again.Overview.InterfaceVersions) != 2 {
		t.Fatalf("declaring a declared version changed the project: %+v", again.Overview.InterfaceVersions)
	}
}

func TestProjectOverviewReportsDocumentsThisReleaseCannotRead(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	future := `{"schema":"readmit-project/v999","settings":{"title":"x"},"interface_versions":["v1"],"cases":[]}`
	writeDocument(t, root, "project.json", future)

	if result := app.OpenProjectOverview(root); result.State != desktop.Failed || !strings.Contains(result.Reason, "version this release cannot read") {
		t.Fatalf("a future project document was not reported as such: %+v", result)
	}
	// The document the window cannot read is left exactly as written.
	if data, err := os.ReadFile(filepath.Join(root, "project.json")); err != nil || string(data) != future {
		t.Fatalf("an unreadable project document was changed: %v", err)
	}
	if result := app.RegisterCase(root, "anything", desktop.CaseRegistration{}); result.State != desktop.Failed || !strings.Contains(result.Reason, "version this release cannot read") {
		t.Fatalf("registration into an unreadable project was not refused by name: %+v", result)
	}
}

func TestListingDistinguishesWhatWorkspaceEntriesDeclare(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root

	// A retained result, a durable run, a review, and the documents the
	// pickers offer — each named for what it declares, never all lumped
	// together as unsupported. The sample workspace already ships a target
	// configuration and one index of its regression case; those two are
	// classified from what they declare, not rewritten here.
	writeDocument(t, root, "correlate.rules.json", `{"schema":"readmit-correlation-rules/v1","rules":[]}`)
	writeDocument(t, root, "redact.plan.json", `{"schema":"readmit-transform-plan/v1","steps":[]}`)
	writeDocument(t, root, "saved-test.json", `{"schema":"readmit-test/v1","name":"t","case":{"entry":"regression","identity":"i"},"messages":[],"target":"p","boundary":"messages","observation":"","reset":"","expectations":[]}`)
	if err := os.MkdirAll(filepath.Join(root, "baseline-run", "result"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, filepath.Join("baseline-run", "engine.json"), `{"schema":"readmit-job/v1"}`)
	writeDocument(t, root, filepath.Join("baseline-run", "result", "result.json"), `{"schema":"readmit-result/v1"}`)
	if err := os.MkdirAll(filepath.Join(root, "review-out"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, filepath.Join("review-out", "review.json"), `{"schema":"readmit-review/v1"}`)
	writeDocument(t, root, "notes.txt", "not evidence")
	// The diagnosis, finding-review and correlation-review directories and the
	// three authored documents this release adds pickers for — each named for
	// what its own record declares. A review.json declaring the finding-review
	// contract is a finding review, and the export review above stays an
	// export review; a report.json declaring anything but a diagnosis contract
	// stays unsupported here.
	writeDocument(t, root, "compare.policy.json", `{"schema":"readmit-normalization-policy/v1","rules":[]}`)
	writeDocument(t, root, "diagnose.config.json", `{"schema":"readmit-diagnose-config/v1"}`)
	writeDocument(t, root, "verdicts.json", `{"schema":"readmit-finding-decisions/v1","report_sha256":"x","decisions":[]}`)
	for directory, marker := range map[string]string{
		"diagnosis-out":      `{"schema":"readmit-diagnosis/v1"}`,
		"groups-out":         `{"schema":"readmit-diagnosis-groups/v1"}`,
		"finding-review-out": `{"schema":"readmit-finding-review/v1"}`,
		"other-report":       `{"schema":"readmit-other-report/v1"}`,
	} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
		name := "report.json"
		if directory == "finding-review-out" {
			name = "review.json"
		}
		writeDocument(t, root, filepath.Join(directory, name), marker)
	}
	if err := os.MkdirAll(filepath.Join(root, "correlation-review-out"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, filepath.Join("correlation-review-out", "machine.json"), `{"schema":"readmit-correlation/v1"}`)

	result := app.OpenWorkspace(root)
	if result.State != desktop.Completed || result.Workspace == nil {
		t.Fatalf("open workspace: %+v", result)
	}
	kinds := map[string]desktop.Kind{}
	for _, artifact := range result.Workspace.Artifacts {
		kinds[artifact.Name] = artifact.Kind
	}
	for name, want := range map[string]desktop.Kind{
		"regression.index.json":  desktop.IndexArtifact,
		"practice-target.json":   desktop.TargetArtifact,
		"correlate.rules.json":   desktop.RulesArtifact,
		"redact.plan.json":       desktop.PlanArtifact,
		"saved-test.json":        desktop.SpecArtifact,
		"baseline-run":           desktop.JobArtifact,
		"review-out":             desktop.ReviewArtifact,
		"compare.policy.json":    desktop.NormalizationArtifact,
		"diagnose.config.json":   desktop.DiagnoseConfigArtifact,
		"verdicts.json":          desktop.DecisionsArtifact,
		"diagnosis-out":          desktop.DiagnosisArtifact,
		"groups-out":             desktop.DiagnosisGroupsArtifact,
		"finding-review-out":     desktop.FindingReviewArtifact,
		"correlation-review-out": desktop.CorrelationReviewArtifact,
		"other-report":           desktop.UnsupportedArtifact,
	} {
		if kinds[name] != want {
			t.Errorf("%s was listed as %q, not %q", name, kinds[name], want)
		}
	}
	if kinds["notes.txt"] != desktop.UnsupportedArtifact {
		t.Errorf("a file that declares nothing was not listed as unsupported: %q", kinds["notes.txt"])
	}
	// The regression case itself is still listed as a case.
	if kinds["regression"] != desktop.CaseArtifact {
		t.Errorf("the generated case was not listed as a case: %q", kinds["regression"])
	}
}

func TestListingAClassifiedArtifactVerifiesNothing(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	// A file that merely claims the index contract is a claim, not an index:
	// it is listed as one, and opening it is a separate verification that
	// nothing here performs.
	writeDocument(t, root, "fake.index.json", `{"schema":"readmit-index/v1","not":"a real index"}`)
	result := app.OpenWorkspace(root)
	for _, artifact := range result.Workspace.Artifacts {
		if artifact.Name == "fake.index.json" && artifact.Kind != desktop.IndexArtifact {
			t.Fatalf("the listing did not report what the entry declares: %+v", artifact)
		}
	}
	// Nothing was opened: the entry's bytes are exactly as they were written.
	if data, err := os.ReadFile(filepath.Join(root, "fake.index.json")); err != nil || string(data) != `{"schema":"readmit-index/v1","not":"a real index"}` {
		t.Fatalf("listing changed the entry it classified: %v", err)
	}
}

func strPtr(value string) *string { return &value }

func TestRegisterRevisionRecordsLineageFromABuiltReproducer(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	registered := app.RegisterCase(root, "incident", desktop.CaseRegistration{Title: "Original incident"})
	if registered.State != desktop.Completed {
		t.Fatalf("register parent: %+v", registered)
	}

	selected := app.EditReproducer(desktop.ReproducerRequest{
		Workspace: root, Case: "incident", Identity: incident.Identity,
		Step: reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID},
	})
	if selected.Reproducer == nil {
		t.Fatalf("select: %+v", selected)
	}
	build := desktop.ReproducerRequest{
		Workspace: root, Case: "incident", Identity: incident.Identity,
		Plan: selected.Reproducer.Plan, Output: "incident-reproducer",
	}
	built := app.BuildReproducer(build)
	if built.State != desktop.Completed || built.Reproducer == nil {
		t.Fatalf("build: %+v", built)
	}

	result := app.RegisterRevision(desktop.RevisionRegistration{
		Workspace: root,
		Source:    "incident-reproducer",
		Name:      "incident-revision",
		Parent:    "incident",
	})
	if result.State != desktop.Completed || result.Overview == nil {
		t.Fatalf("register revision: %+v", result)
	}
	if len(result.Overview.Revisions) != 1 {
		t.Fatalf("overview missing revision: %+v", result.Overview)
	}
	entry := result.Overview.Revisions[0]
	if entry.Name != "incident-revision" || entry.Parent != "incident" || entry.Operation != reproducer.Derivation {
		t.Fatalf("lineage was not recorded from the evidence: %+v", entry)
	}
	if entry.Identity != built.Reproducer.Identity || entry.Evidence != "verified" {
		t.Fatalf("placed case does not match the build: %+v", entry)
	}
	// The build folder is still there for comparison; the parent is unchanged.
	if _, err := os.Stat(filepath.Join(root, "incident-reproducer", reproducer.CaseName)); err != nil {
		t.Fatalf("registering removed the build folder: %v", err)
	}
	if original := app.OpenCase(root, "incident"); original.State != desktop.Completed || original.Case.Identity != incident.Identity {
		t.Fatalf("registering changed the parent: %+v", original)
	}
	opened := app.OpenCase(root, "incident-revision")
	if opened.State != desktop.Completed || opened.Case.Identity != built.Reproducer.Identity {
		t.Fatalf("the placed revision cannot be opened as a case: %+v", opened)
	}
}

func TestRegisterRevisionRefusesWhatItCannotStandBehind(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	incident := writeCase(t, root, "incident", framed(repBooking))
	app.RegisterCase(root, "incident", desktop.CaseRegistration{Title: "Original"})
	selected := app.EditReproducer(desktop.ReproducerRequest{
		Workspace: root, Case: "incident", Identity: incident.Identity,
		Step: reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID},
	})
	build := desktop.ReproducerRequest{
		Workspace: root, Case: "incident", Identity: incident.Identity,
		Plan: selected.Reproducer.Plan, Output: "built",
	}
	if built := app.BuildReproducer(build); built.State != desktop.Completed {
		t.Fatalf("build: %+v", built)
	}

	refusals := []desktop.ProjectOverviewResult{
		app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Name: "rev", Parent: ""}),
		app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: "built", Name: "incident", Parent: "incident"}),
		app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: "missing", Name: "rev", Parent: "incident"}),
		app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Name: "absent", Parent: "incident"}),
		app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Source: "built", Name: "../escape", Parent: "incident"}),
	}
	for _, result := range refusals {
		if result.State != desktop.Failed || result.Overview != nil {
			t.Fatalf("a refusal was not reported: %+v", result)
		}
	}
	if again := app.OpenProjectOverview(root); again.State != desktop.Completed || len(again.Overview.Revisions) != 0 {
		t.Fatalf("a refused registration changed the project: %+v", again)
	}
}
