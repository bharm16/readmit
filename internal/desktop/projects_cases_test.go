package desktop_test

// Projects and Cases (#548): the facade behind the projects list, the cases
// table and the context sheets of a case and a project. Every test here goes
// through the bound facade over a real project folder: the sample workspace's
// verified cases, a project document the test authored, and the facade's own
// writers.

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// casesProject is the sample workspace as a readmit-project/v2 project
// registering the reschedule regression, opened by name. The two other
// sample cases are in the folder and not registered.
func casesProject(t *testing.T) (*desktop.App, *chooser, desktop.RequestContext) {
	t.Helper()
	dialogs := &chooser{folder: t.TempDir()}
	app := newApp(t, dialogs)
	root := sample(t, app).Workspace.Root
	writeDocument(t, root, "project.json", `{"schema":"readmit-project/v2","settings":{"title":"Scheduling QA"},`+
		`"interface_versions":["siu-2.5.1-v1"],"cases":[`+registeredRegression+`]}`+"\n")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed || !opened.Recorded {
		t.Fatalf("open: %+v", opened)
	}
	return app, dialogs, opened.Context
}

// caseAt is the listed case whose bundle is the project entry named.
func caseAt(t *testing.T, app *desktop.App, context desktop.RequestContext, entry string) desktop.CatalogItem {
	t.Helper()
	result := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if result.Page == nil {
		t.Fatalf("cases: %+v", result)
	}
	for _, item := range result.Page.Items {
		if item.Summary.Case != nil && item.Summary.Case.Entry == entry {
			return item
		}
	}
	t.Fatalf("no case at %s: %+v", entry, result.Page.Items)
	return desktop.CatalogItem{}
}

func listsCaseAt(t *testing.T, app *desktop.App, context desktop.RequestContext, entry string) bool {
	t.Helper()
	result := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if result.Page == nil {
		t.Fatalf("cases: %+v", result)
	}
	return slices.ContainsFunc(result.Page.Items, func(item desktop.CatalogItem) bool {
		return item.Summary.Case != nil && item.Summary.Case.Entry == entry
	})
}

func TestTheCasesTableReadsEveryCaseWithItsInvestigationMetadata(t *testing.T) {
	app, _, context := casesProject(t)
	registered := caseAt(t, app, context, "regression")
	summary := registered.Summary.Case
	if registered.Name != "Duplicate appointment after reschedule" || !summary.Registered || summary.Status != project.StatusInvestigating ||
		summary.Owner != "scheduling-team" || !slices.Equal(summary.Tags, []string{"duplicate", "scheduling"}) ||
		!slices.Equal(summary.Incidents, []string{"INC-4821"}) || summary.InterfaceRevision != "siu-2.5.1-v1" {
		t.Fatalf("the registered case: %+v %+v", registered, summary)
	}
	// Generated evidence is marked synthetic; the marker is the only
	// provenance a row carries.
	if summary.Provenance != "synthetic" || registered.Ref.Revision == "" {
		t.Fatalf("provenance or revision: %+v", registered)
	}
	unregistered := caseAt(t, app, context, "cancellation")
	if unregistered.Summary.Case.Registered || unregistered.Summary.Case.Tags == nil || unregistered.Summary.Case.Incidents == nil ||
		unregistered.Ref.Revision != "" || unregistered.UpdatedAt != nil {
		t.Fatalf("the unregistered case: %+v %+v", unregistered, unregistered.Summary.Case)
	}
}

func saveCase(app *desktop.App, context desktop.RequestContext, item desktop.CatalogItem, intent string, draft desktop.CaseDraft) desktop.SaveItemResult {
	return app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CaseItem, Item: item.Ref.ID, BaseRevision: item.Ref.Revision,
		IntentID: intent, Draft: desktop.ItemDraft{Case: &draft}})
}

// Edit details saves the whole case once: a stale base is a conflict that
// keeps the draft, the same click twice is one change, and a rename never
// changes the evidence or its identity.
func TestEditDetailsSavesAWholeCaseOnceWithoutTouchingEvidence(t *testing.T) {
	app, _, context := casesProject(t)
	before := bytesUnder(t, filepath.Join(context.Project, "regression"))
	item := caseAt(t, app, context, "regression")
	draft := desktop.CaseDraft{Name: "Reschedule creates a second appointment", Status: project.StatusResolved,
		Tags: []string{"scheduling", "duplicate", "scheduling"}, Incidents: []string{"INC-4821", "INC-5000"}}
	saved := saveCase(app, context, item, "click-1", draft)
	if saved.State != desktop.Completed || saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.ID != item.Ref.ID ||
		saved.Saved.Revision == item.Ref.Revision || saved.Projection == nil || !slices.Equal(saved.Projection.Case.Tags, []string{"duplicate", "scheduling"}) {
		t.Fatalf("save: %+v", saved)
	}
	again := saveCase(app, context, item, "click-1", draft)
	if again.State != desktop.Completed || !again.Replayed || again.Saved.Revision != saved.Saved.Revision {
		t.Fatalf("the same click again: %+v", again)
	}
	changed := draft
	changed.Name = "Another name"
	if reused := saveCase(app, context, item, "click-1", changed); reused.State != desktop.Failed || reused.Outcome != desktop.FailedOutcome {
		t.Fatalf("a different submission under one click: %+v", reused)
	}
	if stale := saveCase(app, context, item, "click-2", changed); stale.Outcome != desktop.ConflictOutcome || stale.CurrentRevision != saved.Saved.Revision {
		t.Fatalf("a stale base: %+v", stale)
	}
	now := caseAt(t, app, context, "regression")
	summary := now.Summary.Case
	if now.Name != draft.Name || summary.Status != project.StatusResolved || summary.Owner != "" || summary.InterfaceRevision != "" ||
		!slices.Equal(summary.Incidents, []string{"INC-4821", "INC-5000"}) || now.UpdatedAt == nil || now.Ref.Revision != saved.Saved.Revision {
		t.Fatalf("after save: %+v %+v", now, summary)
	}
	opened, err := project.Open(context.Project)
	if err != nil || opened.Document.Cases[0].Identity != "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df" || opened.Document.Cases[0].Name != "regression" {
		t.Fatalf("the evidence facts changed: %+v %v", opened, err)
	}
	if after := bytesUnder(t, filepath.Join(context.Project, "regression")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("saving a case's details rewrote its evidence")
	}
	// Every problem is reported at its member, and nothing is written.
	bad := desktop.CaseDraft{Name: " ", Status: "done", Owner: strings.Repeat("o", 101), Tags: []string{"line\nbreak"}, InterfaceRevision: "undeclared"}
	invalid := saveCase(app, context, now, "click-3", bad)
	fields := []string{}
	for _, problem := range invalid.Problems {
		fields = append(fields, problem.Field)
	}
	if invalid.Outcome != desktop.InvalidOutcome || !slices.Equal(fields, []string{"case.name", "case.status", "case.owner", "case.tags", "case.interface_revision"}) {
		t.Fatalf("invalid: %+v", invalid)
	}
}

// Saving an unregistered case registers it under the same identity.
func TestSavingAnUnregisteredCaseRegistersIt(t *testing.T) {
	app, _, context := casesProject(t)
	item := caseAt(t, app, context, "cancellation")
	// A v2 project's owner, tags and incidents are text a person typed,
	// stored exactly as entered.
	saved := saveCase(app, context, item, "click-1", desktop.CaseDraft{Name: "Cancellation rejected", Status: project.StatusOpen,
		Owner: "Integration team", Tags: []string{"front desk", "Épic"}, Incidents: []string{"INC 42"}, InterfaceRevision: "siu-2.5.1-v1"})
	if saved.State != desktop.Completed || saved.Saved.ID != item.Ref.ID {
		t.Fatalf("save: %+v", saved)
	}
	now := caseAt(t, app, context, "cancellation")
	if !now.Summary.Case.Registered || now.Name != "Cancellation rejected" || now.Summary.Case.Owner != "Integration team" || now.Ref.ID != item.Ref.ID ||
		!slices.Equal(now.Summary.Case.Tags, []string{"front desk", "Épic"}) || !slices.Equal(now.Summary.Case.Incidents, []string{"INC 42"}) {
		t.Fatalf("registered: %+v %+v", now, now.Summary.Case)
	}
	// Validation alone writes nothing.
	validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.CaseItem,
		Draft: desktop.ItemDraft{Case: &desktop.CaseDraft{Name: "x", Status: project.StatusClosed}}})
	if validated.State != desktop.Completed || len(validated.Problems) != 0 || validated.Projection == nil || validated.Projection.Case == nil {
		t.Fatalf("validate: %+v", validated)
	}
}

// Removing a case from the project unregisters it and takes it off the
// project's list; its files stay exactly where they are.
func TestRemovingACaseFromTheProjectKeepsItsFiles(t *testing.T) {
	app, _, context := casesProject(t)
	before := bytesUnder(t, filepath.Join(context.Project, "regression"))
	item := caseAt(t, app, context, "regression")
	// A note about the case holds it in the project.
	noted := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "note-1",
		Note: desktop.NoteInput{Name: "Why", Content: "Because.", Case: &item.Ref}})
	if noted.State != desktop.Completed || noted.Saved == nil {
		t.Fatalf("note: %+v", noted)
	}
	if refused := app.RemoveCaseFromProject(desktop.ItemRequest{Context: context, Ref: item.Ref}); refused.State != desktop.Failed || refused.Reason == "" {
		t.Fatalf("a noted case was removed: %+v", refused)
	}
	other := caseAt(t, app, context, "cancellation")
	for _, ref := range []desktop.ItemRef{other.Ref} {
		removed := app.RemoveCaseFromProject(desktop.ItemRequest{Context: context, Ref: ref})
		if removed.State != desktop.Completed {
			t.Fatalf("remove: %+v", removed)
		}
	}
	if listsCaseAt(t, app, context, "cancellation") || !listsCaseAt(t, app, context, "regression") {
		t.Fatal("the removed case is still listed, or another one went")
	}
	if _, err := os.Lstat(filepath.Join(context.Project, "cancellation")); err != nil {
		t.Fatalf("removing a case from the project removed its files: %v", err)
	}
	if after := bytesUnder(t, filepath.Join(context.Project, "regression")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("evidence changed")
	}
	// A registered case with nothing naming it is unregistered.
	third := caseAt(t, app, context, "invalid")
	if saved := saveCase(app, context, third, "click-r", desktop.CaseDraft{Name: "Invalid", Status: project.StatusOpen}); saved.State != desktop.Completed {
		t.Fatalf("register: %+v", saved)
	}
	if removed := app.RemoveCaseFromProject(desktop.ItemRequest{Context: context, Ref: third.Ref}); removed.State != desktop.Completed {
		t.Fatalf("remove registered: %+v", removed)
	}
	opened, _ := project.Open(context.Project)
	if slices.ContainsFunc(opened.Document.Cases, func(c project.Case) bool { return c.Name == "invalid" }) || listsCaseAt(t, app, context, "invalid") {
		t.Fatal("the registered case was not removed")
	}
}

func saveProject(app *desktop.App, context desktop.RequestContext, intent string, draft desktop.ProjectDraft) desktop.SaveItemResult {
	current := app.OpenItem(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}})
	revision := ""
	if current.Item != nil {
		revision = current.Item.Ref.Revision
	}
	return app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProjectItem, Item: context.ProjectID, BaseRevision: revision,
		IntentID: intent, Draft: desktop.ItemDraft{Project: &draft}})
}

// Project settings save the name, owner, tags and named interface revisions
// in one Save. An existing revision keeps its identity; a new one is given
// one. A revision cases still name is not removed unless they are reassigned,
// and the refusal names them. The folder never moves.
func TestProjectSettingsSaveNamedRevisionsAndNeverDropAReferencedOne(t *testing.T) {
	app, _, context := casesProject(t)
	saved := saveProject(app, context, "p-1", desktop.ProjectDraft{Name: "Scheduling investigation", Owner: "Integration team", Tags: []string{"siu", "front desk"},
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "Current interface", Default: true}, {Name: "Upgrade 2027"}}})
	if saved.State != desktop.Completed || saved.Projection == nil || saved.Projection.Project == nil {
		t.Fatalf("save: %+v", saved)
	}
	revisions := saved.Projection.Project.Revisions
	if len(revisions) != 2 || revisions[0].ID != "siu-2.5.1-v1" || revisions[1].ID == "" || !revisions[0].Default {
		t.Fatalf("revisions: %+v", revisions)
	}
	item := app.OpenItem(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}})
	summary := item.Item.Summary.Project
	if item.Item.Name != "Scheduling investigation" || summary.Owner != "Integration team" || !slices.Equal(summary.Tags, []string{"front desk", "siu"}) ||
		len(summary.Revisions) != 2 || summary.Revisions[0].Name != "Current interface" || summary.Revisions[1].Name != "Upgrade 2027" {
		t.Fatalf("project: %+v %+v", item.Item, summary)
	}
	if _, err := os.Lstat(filepath.Join(context.Project, "project.json")); err != nil {
		t.Fatal("the project folder moved")
	}
	// Dropping the revision the registered case names is refused, naming it.
	dropped := saveProject(app, context, "p-2", desktop.ProjectDraft{Name: "Scheduling investigation",
		Revisions: []desktop.RevisionDraft{{ID: revisions[1].ID, Name: "Upgrade 2027"}}})
	if dropped.Outcome != desktop.InvalidOutcome || len(dropped.Problems) != 1 || len(dropped.Problems[0].Referring) != 1 ||
		dropped.Problems[0].Referring[0].Name != "Duplicate appointment after reschedule" || dropped.Problems[0].Referring[0].Ref.Kind != desktop.CaseItem {
		t.Fatalf("a referenced revision was dropped: %+v", dropped)
	}
	// One revision's cases are reassigned once.
	twice := saveProject(app, context, "p-twice", desktop.ProjectDraft{Name: "Scheduling investigation", Revisions: []desktop.RevisionDraft{},
		Reassign: []desktop.Reassignment{{From: "siu-2.5.1-v1"}, {From: "siu-2.5.1-v1"}}})
	if twice.Outcome != desktop.InvalidOutcome || len(twice.Problems) != 1 || twice.Problems[0].Field != "project.reassign[1]" {
		t.Fatalf("a revision reassigned twice: %+v", twice)
	}
	// Reassigning the cases lets it go, and no revision at all is valid.
	reassigned := saveProject(app, context, "p-3", desktop.ProjectDraft{Name: "Scheduling investigation", Revisions: []desktop.RevisionDraft{},
		Reassign: []desktop.Reassignment{{From: "siu-2.5.1-v1"}}})
	if reassigned.State != desktop.Completed {
		t.Fatalf("reassign: %+v", reassigned)
	}
	opened, _ := project.Open(context.Project)
	if len(opened.Document.InterfaceVersions) != 0 || opened.Document.Cases[0].InterfaceVersion != "" || opened.Document.Settings.DefaultOwner != "" {
		t.Fatalf("after reassign: %+v", opened.Document)
	}
	if stale := saveProject(app, context, "p-3", desktop.ProjectDraft{Name: "Other"}); stale.State == desktop.Completed {
		t.Fatalf("a different submission under one click: %+v", stale)
	}
}

// A v1 project keeps v1's rules: a save that needs v2 is refused with its
// reason, and one that v1 can hold is saved.
func TestProjectSettingsOfAVersionOneProjectKeepItsRules(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := writeProject(t, t.TempDir(), "")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	refused := saveProject(app, opened.Context, "p-1", desktop.ProjectDraft{Name: "Renamed", Tags: []string{"siu"},
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1", Default: true}}})
	if refused.Outcome != desktop.InvalidOutcome {
		t.Fatalf("a v1 project took tags: %+v", refused)
	}
	if spaced := saveProject(app, opened.Context, "p-3", desktop.ProjectDraft{Name: "Renamed", Owner: "Integration team",
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1", Default: true}}}); spaced.Outcome != desktop.InvalidOutcome ||
		spaced.Problems[0].Field != "project.owner" {
		t.Fatalf("a v1 project took an owner that is not an identifier: %+v", spaced)
	}
	saved := saveProject(app, opened.Context, "p-2", desktop.ProjectDraft{Name: "Renamed", Owner: "team",
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1", Default: true}, {Name: "siu-2.6"}}})
	if saved.State != desktop.Completed {
		t.Fatalf("a v1 save: %+v", saved)
	}
	document, _ := project.Open(root)
	if document.Document.Schema != project.Schema || !slices.Equal(document.Document.InterfaceVersions, []string{"siu-2.5.1-v1", "siu-2.6"}) {
		t.Fatalf("v1 after save: %+v", document.Document)
	}
}

// Notes are named, have content and may be about a case; their identity is
// the application's.
func TestNotesAreSavedOnceAndListedPerCase(t *testing.T) {
	app, _, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	request := desktop.NoteSaveRequest{Context: context, IntentID: "n-1", Note: desktop.NoteInput{Name: "First look", Content: "Two SIU^S12.", Case: &item.Ref}}
	saved := app.SaveNoteItem(request)
	if saved.State != desktop.Completed || saved.Saved == nil || saved.Saved.ID == "" || len(saved.Notes) != 1 {
		t.Fatalf("save: %+v", saved)
	}
	if again := app.SaveNoteItem(request); again.State != desktop.Completed || len(again.Notes) != 1 || again.Saved.ID != saved.Saved.ID {
		t.Fatalf("the same click twice: %+v", again)
	}
	edited := request
	edited.IntentID, edited.Note.ID, edited.Note.Content = "n-2", saved.Saved.ID, "Two SIU^S12, one rescheduled."
	if changed := app.SaveNoteItem(edited); changed.State != desktop.Completed || len(changed.Notes) != 1 || changed.Notes[0].Content != edited.Note.Content {
		t.Fatalf("edit: %+v", changed)
	}
	project := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "n-3", Note: desktop.NoteInput{Name: "Plan", Content: ""}})
	if project.State != desktop.Completed || project.Saved.Case != nil {
		t.Fatalf("project note: %+v", project)
	}
	perCase := app.ListNotes(desktop.NotesRequest{Context: context, Case: &item.Ref})
	unbound := app.ListNotes(desktop.NotesRequest{Context: context})
	if len(perCase.Notes) != 1 || perCase.Notes[0].Case == nil || perCase.Notes[0].Case.ID != item.Ref.ID || perCase.Notes[0].UpdatedAt != nil ||
		len(unbound.Notes) != 1 || unbound.Notes[0].Name != "Plan" {
		t.Fatalf("lists: %+v %+v", perCase, unbound)
	}
	// A note about a case the project has not registered is refused.
	other := caseAt(t, app, context, "cancellation")
	if refused := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "n-4", Note: desktop.NoteInput{Name: "x", Case: &other.Ref}}); refused.State != desktop.Failed {
		t.Fatalf("a note on an unregistered case: %+v", refused)
	}
}

// Attachments are copied into the project's own storage under names the
// application gives them; removing one removes the association only.
func TestAttachmentsAreCopiedIntoTheProjectAndRemovedAsAssociations(t *testing.T) {
	app, dialogs, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	outside := t.TempDir()
	writeDocument(t, outside, "ticket.pdf", "%PDF-1.4 ticket")
	writeDocument(t, outside, "notes.txt", "notes")
	original, err := os.Stat(filepath.Join(outside, "ticket.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	dialogs.files = []string{filepath.Join(outside, "ticket.pdf"), filepath.Join(outside, "notes.txt")}
	added := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref})
	if added.State != desktop.Completed || len(added.Attachments) != 2 || dialogs.titles[len(dialogs.titles)-1] != "Add attachment" {
		t.Fatalf("add: %+v %v", added, dialogs.titles)
	}
	first := added.Attachments[0]
	if first.Name != "ticket.pdf" || first.Type != "pdf" || first.AddedAt == nil || strings.Contains(first.ID, "/") {
		t.Fatalf("attachment: %+v", first)
	}
	// Adding the same file again is a second attachment, never an overwrite.
	dialogs.files = dialogs.files[:1]
	if again := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); len(again.Attachments) != 3 {
		t.Fatalf("again: %+v", again)
	}
	stored := managedFiles(t, context.Project)
	if len(stored) != 3 {
		t.Fatalf("stored: %v", stored)
	}
	removed := app.RemoveAttachment(desktop.AttachmentRemoveRequest{Context: context, Case: item.Ref, ID: first.ID})
	if removed.State != desktop.Completed || len(removed.Attachments) != 2 {
		t.Fatalf("remove: %+v", removed)
	}
	if listedNow := app.ListAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); len(listedNow.Attachments) != 2 {
		t.Fatalf("list: %+v", listedNow)
	}
	after, err := os.Stat(filepath.Join(outside, "ticket.pdf"))
	if err != nil || after.Mode() != original.Mode() || !after.ModTime().Equal(original.ModTime()) ||
		string(mustRead(t, filepath.Join(outside, "ticket.pdf"))) != "%PDF-1.4 ticket" {
		t.Fatal("the original file was touched")
	}
	// Attachments belong to a case, never a variant.
	if variant := app.ListAttachments(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.VariantItem, ID: item.Ref.ID}}); variant.State != desktop.Failed {
		t.Fatalf("a variant's attachments: %+v", variant)
	}
	// A symbolic link and a cancelled dialog add nothing.
	if err := os.Symlink(filepath.Join(outside, "notes.txt"), filepath.Join(outside, "link.txt")); err != nil {
		t.Fatal(err)
	}
	dialogs.files = []string{filepath.Join(outside, "link.txt")}
	if linked := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); linked.State != desktop.Failed || strings.Contains(linked.Reason, outside) {
		t.Fatalf("a link was attached: %+v", linked)
	}
	dialogs.files = nil
	if cancelled := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); cancelled.State != desktop.Cancelled {
		t.Fatalf("cancel: %+v", cancelled)
	}
	// The project's own storage is not a case, a loose file or anything a
	// catalog lists.
	files := app.ProjectFiles(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}})
	if slices.ContainsFunc(files.Files, func(file desktop.ProjectFile) bool { return file.Name == catalog.Folder }) {
		t.Fatalf("files: %+v", files)
	}
}

// managedFiles are the attachment files the project's storage holds.
func managedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	folder := filepath.Join(root, catalog.Folder, "attachments")
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files
}

// The Files list names the entries of a project that are no object of it,
// and nothing the application keeps for itself.
func TestProjectFilesListsLooseEntriesByName(t *testing.T) {
	app, _, context := casesProject(t)
	writeDocument(t, context.Project, "export.csv", "a,b\n")
	files := app.ProjectFiles(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}})
	names := []string{}
	for _, file := range files.Files {
		names = append(names, file.Name)
	}
	if files.State != desktop.Completed || !slices.Contains(names, "export.csv") || slices.Contains(names, "regression") ||
		slices.Contains(names, "project.json") || slices.Contains(names, catalog.Folder) {
		t.Fatalf("files: %+v", files)
	}
}

// Remove from recents forgets the entry and nothing else.
func TestForgettingAProjectForgetsOnlyTheEntry(t *testing.T) {
	app, _, context := casesProject(t)
	forgotten := app.ForgetProject(context.ProjectID)
	if forgotten.State != desktop.Completed {
		t.Fatalf("forget: %+v", forgotten)
	}
	projects := app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem})
	if projects.Page != nil && len(projects.Page.Items) != 0 {
		t.Fatalf("still listed: %+v", projects.Page.Items)
	}
	if _, err := project.Open(context.Project); err != nil {
		t.Fatal("forgetting a project touched it")
	}
	if again := app.ForgetProject(context.ProjectID); again.State != desktop.Failed {
		t.Fatalf("forgetting twice: %+v", again)
	}
}

// Show in Finder reveals the object's own place through the host and never
// answers that place to the window.
func TestRevealShowsAnObjectsPlaceWithoutAnsweringIt(t *testing.T) {
	app, _, context := casesProject(t)
	var revealed []string
	desktop.SetRevealForTest(app, func(path string) error {
		revealed = append(revealed, path)
		return nil
	})
	item := caseAt(t, app, context, "regression")
	result := app.RevealItem(desktop.ItemRequest{Context: context, Ref: item.Ref})
	if result.State != desktop.Completed || len(revealed) != 1 || revealed[0] != filepath.Join(context.Project, "regression") {
		t.Fatalf("reveal case: %+v %v", result, revealed)
	}
	if projectShown := app.RevealItem(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}}); projectShown.State != desktop.Completed ||
		revealed[1] != context.Project {
		t.Fatalf("reveal project: %+v %v", projectShown, revealed)
	}
	if err := os.RemoveAll(filepath.Join(context.Project, "invalid")); err != nil {
		t.Fatal(err)
	}
	missing := caseAt(t, app, context, "cancellation")
	if err := os.RemoveAll(filepath.Join(context.Project, "cancellation")); err != nil {
		t.Fatal(err)
	}
	refused := app.RevealItem(desktop.ItemRequest{Context: context, Ref: missing.Ref})
	if refused.State != desktop.Failed || strings.Contains(refused.Reason, context.Project) || len(revealed) != 2 {
		t.Fatalf("a missing case was revealed: %+v", refused)
	}
	if unknown := app.RevealItem(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.CaseItem, ID: strings.Repeat("0", 24)}}); unknown.State != desktop.Failed {
		t.Fatalf("an unknown object was revealed: %+v", unknown)
	}
}

// Locate without a place asks the host's folder dialog, and only a folder
// that is the object is recorded; a cancelled dialog changes nothing.
func TestLocateAsksForTheFolderAndVerifiesIt(t *testing.T) {
	app, dialogs, context := casesProject(t)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(context.Project, moved); err != nil {
		t.Fatal(err)
	}
	ref := desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}
	dialogs.folder = ""
	if cancelled := app.LocateItem(desktop.LocateRequest{Context: context, Ref: ref}); cancelled.State != desktop.Cancelled || dialogs.titles[len(dialogs.titles)-1] != "Locate project" {
		t.Fatalf("cancel: %+v %v", cancelled, dialogs.titles)
	}
	dialogs.folder = t.TempDir()
	if wrong := app.LocateItem(desktop.LocateRequest{Context: context, Ref: ref}); wrong.State == desktop.Completed {
		t.Fatalf("a folder that is not the project was recorded: %+v", wrong)
	}
	dialogs.folder = moved
	located := app.LocateItem(desktop.LocateRequest{Context: context, Ref: ref})
	if located.State != desktop.Completed || located.Item == nil || located.Item.Ref.ID != context.ProjectID {
		t.Fatalf("locate: %+v", located)
	}
}

// Every retained draft of a catalog object says when it was last retained,
// so recovery can list it by object, time and project; a plain draft keeps
// the v1 store it always had, and says nothing.
func TestRetainedDraftsSayWhenTheyWereRetained(t *testing.T) {
	app, _, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	content := []byte(`{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"x"}`)
	plain := app.SaveEditorDraft(desktop.EditorDraft{Kind: "note", Workspace: context.Project, ContentSchema: "readmit-note-draft/v1", Content: content})
	if plain.State != desktop.Completed || len(plain.Drafts) != 1 || plain.Drafts[0].SavedAt != nil {
		t.Fatalf("plain draft: %+v", plain)
	}
	sent := "2001-01-01T00:00:00Z"
	retained := app.SaveEditorDraft(desktop.EditorDraft{Kind: "case", Workspace: context.Project, ContentSchema: "readmit-note-draft/v1", Content: content,
		Item: &desktop.DraftItem{ProjectID: context.ProjectID, Ref: item.Ref}, SavedAt: &sent})
	edits := slices.IndexFunc(retained.Drafts, func(draft desktop.EditorDraft) bool { return draft.Item != nil })
	if retained.State != desktop.Completed || edits < 0 || retained.Drafts[edits].SavedAt == nil || *retained.Drafts[edits].SavedAt == sent {
		t.Fatalf("draft: %+v", retained)
	}
	listed := app.EditorDrafts()
	at := slices.IndexFunc(listed.Drafts, func(draft desktop.EditorDraft) bool { return draft.Item != nil })
	if at < 0 || listed.Drafts[at].SavedAt == nil || *listed.Drafts[at].SavedAt != *retained.Drafts[edits].SavedAt {
		t.Fatalf("drafts: %+v", listed)
	}
}

// Opening a folder that holds a recorded project remembers that project, as
// opening it by name does; nothing else about the folder is remembered, and a
// recent-folder list an earlier release wrote is left exactly as it was.
func TestOpeningAFolderRemembersTheProjectItHolds(t *testing.T) {
	state := t.TempDir()
	earlier := `{"schema":"readmit-desktop-recent/v1","roots":["/tmp"]}`
	writeDocument(t, state, "recent.json", earlier)
	first := activatedApp(t, &chooser{folder: t.TempDir()}, state)
	root := sample(t, first).Workspace.Root
	writeProject(t, root, registeredRegression)
	opened := first.OpenNamedProject(root)
	if opened.State != desktop.Completed || !opened.Recorded {
		t.Fatalf("open: %+v", opened)
	}
	if forgot := first.ForgetProject(opened.Context.ProjectID); forgot.State != desktop.Completed {
		t.Fatalf("forget: %+v", forgot)
	}
	if listed := first.OpenWorkspace(root); listed.State != desktop.Completed {
		t.Fatalf("open workspace: %+v", listed)
	}
	projects := first.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem})
	if projects.Page == nil || len(projects.Page.Items) != 1 || projects.Page.Items[0].Ref.ID != opened.Context.ProjectID ||
		projects.Page.Items[0].LastOpenedAt == nil {
		t.Fatalf("the project was not remembered: %+v", projects)
	}
	if plain := first.OpenWorkspace(t.TempDir()); plain.State != desktop.Empty {
		t.Fatalf("open a plain folder: %+v", plain)
	}
	if again := first.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem}); again.Page == nil || len(again.Page.Items) != 1 {
		t.Fatalf("a folder holding no project was remembered: %+v", again)
	}
	if data, err := os.ReadFile(filepath.Join(state, "recent.json")); err != nil || string(data) != earlier {
		t.Fatalf("the earlier recent-folder list was touched: %q %v", data, err)
	}
}

// A project's quota bounds the copies attachments keep. A removed attachment
// keeps its copy, and that copy no longer counts against adding another.
func TestAttachmentsKeepToTheProjectsQuota(t *testing.T) {
	app, dialogs, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	outside := t.TempDir()
	writeDocument(t, outside, "first.txt", "first")
	writeDocument(t, outside, "second.txt", "other")
	dialogs.files = []string{filepath.Join(outside, "first.txt")}
	added := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref})
	if added.State != desktop.Completed || len(added.Attachments) != 1 {
		t.Fatalf("add: %+v", added)
	}
	if removed := app.RemoveAttachment(desktop.AttachmentRemoveRequest{Context: context, Case: item.Ref, ID: added.Attachments[0].ID}); removed.State != desktop.Empty {
		t.Fatalf("remove: %+v", removed)
	}
	// Room for exactly one more file: the quota document itself takes one.
	usage, err := project.CheckQuota(context.Project)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.SetQuota(context.Project, project.Quota{Schema: project.QuotaSchema, MaxBytes: 1 << 30, MaxFiles: usage.Files + 1}); err != nil {
		t.Fatal(err)
	}
	dialogs.files = []string{filepath.Join(outside, "second.txt")}
	if room := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); room.State != desktop.Completed || len(room.Attachments) != 1 {
		t.Fatalf("the removed attachment's copy was counted against the quota: %+v", room)
	}
	dialogs.files = []string{filepath.Join(outside, "first.txt")}
	if full := app.AddAttachments(desktop.ItemRequest{Context: context, Ref: item.Ref}); full.State != desktop.Failed || !strings.Contains(full.Reason, "quota") {
		t.Fatalf("an attachment past the quota was stored: %+v", full)
	}
	if copies := managedFiles(t, context.Project); len(copies) != 2 {
		t.Fatalf("stored copies: %v", copies)
	}
}
