package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
)

// createdProject writes the project `readmit project init` writes into a new
// folder of parent — two declared interface versions, the first the default —
// and returns its root and the overview the window reads of it. A fresh
// project holds nothing yet, so the overview's state is Empty.
func createdProject(t *testing.T, app *desktop.App, parent string) (string, *desktop.ProjectOverview) {
	t.Helper()
	created, err := project.Create(filepath.Join(parent, "scheduling-investigation"), project.Document{Schema: project.Schema,
		Settings:          project.Settings{Title: "Epic scheduling interface", DefaultOwner: "integration-team", DefaultInterfaceVersion: "siu-2.5.1-v1"},
		InterfaceVersions: []string{"siu-2.5.1-v1", "siu-2.5.1-v2"}})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	result := app.OpenProjectOverview(created.Root)
	if result.State != desktop.Empty || result.Overview == nil {
		t.Fatalf("overview: %+v", result)
	}
	return created.Root, result.Overview
}

// registerCase registers one case of a project through the shared operation
// `readmit project add` runs, for a test whose subject is what follows.
func registerCase(t *testing.T, root, name, title string) {
	t.Helper()
	if _, err := operation.RegisterCase(root, name, operation.CaseRegistration{Title: title}); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func TestRegisterCaseRecordsWhatTheSharedReaderAccepted(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	written := writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}

	// Saving the details of a case the project has not registered registers
	// it through the operation `readmit project add` runs.
	item := caseAt(t, app, opened.Context, "incident-4821")
	result := saveCase(app, opened.Context, item, "register", desktop.CaseDraft{Name: "Duplicate appointment after reschedule",
		Status: project.StatusOpen, Tags: []string{"duplicate", "scheduling"}, InterfaceRevision: "siu-2.5.1-v1"})
	if result.State != desktop.Completed {
		t.Fatalf("register case: %+v", result)
	}
	overview := app.OpenProjectOverview(root)
	if overview.State != desktop.Completed || len(overview.Overview.Cases) != 1 {
		t.Fatalf("the overview does not carry the case: %+v", overview)
	}
	registered := overview.Overview.Cases[0]
	if registered.Identity != written.Identity || registered.Schema != written.Manifest.Schema {
		t.Fatalf("the project did not record what the reader accepted: %+v", registered)
	}
	if registered.Provenance != "imported" {
		t.Fatalf("the provenance mode was not read from the evidence: %+v", registered)
	}
	// An owner the details left empty is inherited from the project.
	if registered.Status != "open" || registered.Owner != "integration-team" || registered.InterfaceVersion != "siu-2.5.1-v1" {
		t.Fatalf("the project defaults were not inherited: %+v", registered)
	}
	if registered.Evidence != "verified" {
		t.Fatalf("freshly registered evidence is not verified: %+v", registered)
	}
	// The stored document is what the command line reads back.
	stored, err := project.Open(root)
	if err != nil || len(stored.Document.Cases) != 1 || stored.Document.Cases[0].Identity != written.Identity {
		t.Fatalf("the stored project document does not hold the registered case: %v %+v", err, stored)
	}
}

func TestUpdateRegisteredCaseChangesOnlyWhatItNames(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	if _, err := operation.RegisterCase(root, "incident-4821", operation.CaseRegistration{Title: "Duplicate appointment", Tags: []string{"scheduling"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := project.Open(root)
	opened := app.OpenNamedProject(root)
	item := caseAt(t, app, opened.Context, "incident-4821")

	// The case's details are saved whole, through the operation `readmit
	// project update` runs: what the draft states is stored, and the
	// evidence facts are out of its reach.
	result := saveCase(app, opened.Context, item, "status", desktop.CaseDraft{Name: "Duplicate appointment", Status: project.StatusInvestigating,
		Owner: "integration-team", Tags: []string{"scheduling"}, InterfaceRevision: "siu-2.5.1-v1"})
	if result.State != desktop.Completed {
		t.Fatalf("update case: %+v", result)
	}
	updated, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	stored, was := updated.Document.Cases[0], before.Document.Cases[0]
	if stored.Status != "investigating" || len(stored.Tags) != 1 || stored.Tags[0] != "scheduling" {
		t.Fatalf("the details were not stored as saved: %+v", stored)
	}
	if stored.Name != was.Name || stored.Identity != was.Identity || stored.Schema != was.Schema || stored.Provenance != was.Provenance {
		t.Fatalf("a metadata edit changed what the evidence is: %+v", stored)
	}
	if overview := app.OpenProjectOverview(root); overview.Overview.Cases[0].Evidence != "verified" {
		t.Fatalf("a metadata edit changed the evidence state: %+v", overview.Overview.Cases[0])
	}
	// An unknown status is refused at its member, and nothing is written.
	now := caseAt(t, app, opened.Context, "incident-4821")
	refused := saveCase(app, opened.Context, now, "ended", desktop.CaseDraft{Name: "Duplicate appointment", Status: "ended", InterfaceRevision: "siu-2.5.1-v1"})
	if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "case.status" {
		t.Fatalf("an unknown status change was not refused by name: %+v", refused)
	}
}

func TestProjectOverviewReVerifiesRegisteredEvidence(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	registerCase(t, root, "incident-4821", "Duplicate appointment")

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
	if result := app.SaveItem(desktop.SaveItemRequest{Context: desktop.RequestContext{Project: root}, Kind: desktop.CaseItem, Item: strings.Repeat("a", 24),
		IntentID: "anything", Draft: desktop.ItemDraft{Case: &desktop.CaseDraft{Name: "x", Status: project.StatusOpen}}}); result.State != desktop.Failed ||
		!strings.Contains(result.Reason, "version this release cannot read") {
		t.Fatalf("a save into an unreadable project was not refused by name: %+v", result)
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
	registerCase(t, root, "incident", "Original incident")

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
	registerCase(t, root, "incident", "Original")
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
