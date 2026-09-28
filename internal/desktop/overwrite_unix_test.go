//go:build !windows

package desktop_test

// A save that may overwrite its output replaces the entry at the output name
// and never writes into what is there. Whatever is planted at that name — a
// symbolic link out of the workspace or to one of its own entries, a hard link
// to a file elsewhere, a FIFO — is itself replaced, so the file it led to keeps
// its bytes; a folder there, or anything where the replacement is written
// first, is refused. A regular file at the name is replaced with the same bytes
// the save has always written there.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testlicense"
)

// victim is what the file outside the workspace holds before any save.
const victim = "synthetic document outside the workspace; a save must never change it\n"

// saved is what one save answered, and report what it said it saved, which
// must be the same over a regular file and over every entry it replaces.
type saved struct {
	state  desktop.State
	reason string
	report any
}

// overwriteWorkspaces returns a prepared workspace and a folder beside it
// holding the victim document.
func overwriteWorkspaces(t *testing.T, prepare func(*testing.T, string)) (root, outside string) {
	t.Helper()
	parent := t.TempDir()
	for _, folder := range []string{"workspace", "outside"} {
		if err := os.Mkdir(filepath.Join(parent, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, outside = resolved(t, filepath.Join(parent, "workspace")), resolved(t, filepath.Join(parent, "outside"))
	writeDocument(t, outside, "victim.json", victim)
	if prepare != nil {
		prepare(t, root)
	}
	return root, outside
}

// ownerOnlyBytes reads the file at path, failing the test unless the entry
// there is one regular file only its owner can read.
func ownerOnlyBytes(t *testing.T, path string) []byte {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file: %v %v", filepath.Base(path), info, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s is %v, want owner-only", filepath.Base(path), info.Mode().Perm())
	}
	return mustRead(t, path)
}

// replacingSave is one save that writes over the document at its name by
// renaming a complete new file onto it. create writes the document the first
// time, into the folder outside the workspace; prepare, when set, puts what
// else the save needs into the workspace; replace saves over the document. A
// save whose reader refuses a symbolic link before anything is written names
// that refusal in linkRefusal.
type replacingSave struct {
	name        string
	file        string
	create      func(app *desktop.App, folder, file string) saved
	prepare     func(t *testing.T, root string)
	replace     func(app *desktop.App, folder, file string) saved
	linkRefusal string
}

// Every other save that may write over an existing document. Each is handed
// a symbolic link and a hard link, at the name it writes, to a document of
// its own kind outside the workspace that it would really read and replace,
// and must leave that document's bytes as they were: a save that completes has
// replaced the entry at the name with a regular file of its own.
func TestEverySaveThatReplacesADocumentLeavesTheFileALinkLedToUnchanged(t *testing.T) {
	reference := secret.Reference{
		Name: "vault-key", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint,
		Address: "127.0.0.1:2575", Command: "/bin/echo", Arguments: []string{"test-only-credential"}, MaxAge: "720h",
	}
	saveSecret := func(app *desktop.App, folder, file string, change func(*secret.Reference)) saved {
		declared := reference
		if change != nil {
			change(&declared)
		}
		result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: folder, SecretsFile: file, Reference: declared})
		return saved{result.State, result.Reason, result.Document}
	}
	createSecrets := func(app *desktop.App, folder, file string) saved { return saveSecret(app, folder, file, nil) }
	saveTarget := func(app *desktop.App, folder, file, name string) saved {
		opened := app.ReadTarget(folder, "absent-target.json")
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		declared := *opened.Target
		declared.Name, declared.Classification, declared.Address, declared.ApprovedTransport = name, "nonproduction", "127.0.0.1:2575", true
		result := app.SaveTarget(desktop.TargetSaveRequest{Workspace: folder, TargetFile: file, Target: declared})
		return saved{result.State, result.Reason, result.Target}
	}
	savePolicy := func(app *desktop.App, folder, file, destination string) saved {
		result := app.SaveSendPolicy(desktop.SendPolicySaveRequest{Workspace: folder, PolicyFile: file, Policy: sendpolicy.Policy{
			Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{destination},
		}})
		return saved{result.State, result.Reason, result.Policy}
	}
	savePlan := func(app *desktop.App, folder, file, environment string) saved {
		result := app.SaveResetPlan(desktop.ResetPlanSaveRequest{Workspace: folder, PlanFile: file, Plan: fixturereset.Plan{
			Schema: fixturereset.PlanSchema, Environment: environment,
			Actions: []fixturereset.Action{{ID: "confirm", Operator: fixturereset.OperatorConfirms, Authority: fixturereset.NoAuthority, Instructions: "Confirm the reset"}},
		}})
		return saved{result.State, result.Reason, result.Plan}
	}
	saveWindow := func(app *desktop.App, folder, file, declared string) saved {
		opened := app.OpenObservationWindow(folder, "absent-window.json")
		if declared != "" {
			writeDocument(t, folder, "declared-window.json", declared)
			opened = app.OpenObservationWindow(folder, "declared-window.json")
		}
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		result := app.SaveObservationWindow(desktop.ObservationWindowRequest{Workspace: folder, WindowFile: file, Window: opened.Window})
		return saved{result.State, result.Reason, result.Identity}
	}
	saveSource := func(app *desktop.App, folder, file, declared string) saved {
		opened := app.OpenObservationSource(folder, "absent-source.json")
		if declared != "" {
			writeDocument(t, folder, "declared-source.json", declared)
			opened = app.OpenObservationSource(folder, "declared-source.json")
		}
		if opened.State != desktop.Completed {
			return saved{opened.State, opened.Reason, nil}
		}
		result := app.SaveObservationSource(desktop.ObservationSourceRequest{Workspace: folder, SourceFile: file, Source: opened.Source})
		return saved{result.State, result.Reason, result.Identity}
	}
	// A project's documents have fixed names in its folder: the project
	// document, the editable revisions document a note and a revision are
	// kept in, and the quota. Each is read through the folder itself, which
	// refuses a symbolic link out of it before anything is written.
	aProject := func(t *testing.T, folder string) { writeProject(t, folder, registeredRegression) }
	createProject := func(app *desktop.App, folder, _ string) saved {
		aProject(t, folder)
		return saved{desktop.Completed, "", nil}
	}
	saveNote := func(app *desktop.App, folder, title string) saved {
		result := app.SaveNoteItem(desktop.NoteSaveRequest{Context: desktop.RequestContext{Project: folder}, IntentID: "note-" + strconv.Itoa(len(title)) + "-" + filepath.Base(folder),
			Note: desktop.NoteInput{Name: title, Content: "Synthetic working theory."}})
		return saved{result.State, result.Reason, nil}
	}
	createNote := func(app *desktop.App, folder, _ string) saved {
		aProject(t, folder)
		return saveNote(app, folder, "Outside theory")
	}
	setQuota := func(app *desktop.App, folder string, files int) saved {
		result := app.SetProjectQuota(desktop.ProjectQuotaChange{Project: folder, MaxBytes: 50_000_000, MaxFiles: files})
		return saved{result.State, result.Reason, result.Quota}
	}
	// A recovery copy of an earlier project document, which recovering
	// writes back over the current one.
	earlier := mustRead(t, filepath.Join(writeProject(t, t.TempDir(), ""), project.DocumentName))
	sum := sha256.Sum256(earlier)
	earlierDigest := hex.EncodeToString(sum[:])
	aRecoveryCopy := func(t *testing.T, root string) {
		writeDocument(t, root, project.DocumentName+".recovery-"+earlierDigest, string(earlier))
	}
	// A registered incident and a reproducer built from it, which
	// registering a revision records in the revisions document.
	aBuiltReproducer := func(t *testing.T, root string) {
		writeProject(t, root, "")
		app := workspaceApp(t)
		incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
		if _, err := operation.RegisterCase(root, "incident", operation.CaseRegistration{Title: "Original incident"}); err != nil {
			t.Fatalf("register the incident: %v", err)
		}
		selected := app.EditReproducer(desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: incident.Identity,
			Step: reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID}})
		if selected.Reproducer == nil {
			t.Fatalf("select: %+v", selected)
		}
		if built := app.BuildReproducer(desktop.ReproducerRequest{Workspace: root, Case: "incident", Identity: incident.Identity,
			Plan: selected.Reproducer.Plan, Output: "incident-reproducer"}); built.State != desktop.Completed {
			t.Fatalf("build: %+v", built)
		}
	}
	aCase := func(t *testing.T, folder string) { writeCase(t, folder, "case", framed(gridBooking)) }
	buildIndex := func(app *desktop.App, folder, file string, replace bool) saved {
		result := app.BuildIndex(desktop.BuildIndexRequest{Workspace: folder, Case: "case", Output: file,
			Fields: []string{"PID-5"}, Retention: "values", RetainUntil: "indefinite", Replace: replace})
		return saved{result.State, result.Reason, nil}
	}
	projectUnread := "the folder holds no project document this release reads"
	projectDocumentUnread := "directory holds no readable project document"
	revisionsUnread := "the editable project document cannot be read"

	replacesALinkedDocument(t, []replacingSave{
		{name: "SaveTarget", file: "target.json",
			create: func(app *desktop.App, folder, file string) saved { return saveTarget(app, folder, file, "outside-lab") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveTarget(app, folder, file, "workspace-lab")
			}},
		{name: "SaveSecretReference(add)", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				return saveSecret(app, folder, file, func(r *secret.Reference) { r.Name = "second-key" })
			}},
		{name: "SaveSecretReference(update)", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				address := "127.0.0.1:2576"
				result := app.SaveSecretReference(desktop.SecretSaveRequest{Workspace: folder, SecretsFile: file, Reference: reference,
					IsUpdate: true, Change: &desktop.SecretChange{Address: &address}})
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "RotateSecretReference", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				result := app.RotateSecretReference(folder, file, "vault-key")
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "RemoveSecretReference", file: "secrets.json", create: createSecrets,
			replace: func(app *desktop.App, folder, file string) saved {
				result := app.RemoveSecretReference(folder, file, "vault-key")
				return saved{result.State, result.Reason, result.Document}
			}},
		{name: "SaveSendPolicy", file: "send-policy.json",
			create: func(app *desktop.App, folder, file string) saved { return savePolicy(app, folder, file, "10.0.0.0/16") },
			replace: func(app *desktop.App, folder, file string) saved {
				return savePolicy(app, folder, file, "127.0.0.1/32")
			}},
		{name: "SaveResetPlan", file: "reset-plan.json",
			create:  func(app *desktop.App, folder, file string) saved { return savePlan(app, folder, file, "outside-lab") },
			replace: func(app *desktop.App, folder, file string) saved { return savePlan(app, folder, file, "workspace-lab") }},
		{name: "SaveObservationWindow", file: "window.json",
			create: func(app *desktop.App, folder, file string) saved { return saveWindow(app, folder, file, "") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveWindow(app, folder, file, facadeWindowDocument)
			}},
		{name: "SaveObservationSource", file: "source.json",
			create: func(app *desktop.App, folder, file string) saved { return saveSource(app, folder, file, "") },
			replace: func(app *desktop.App, folder, file string) saved {
				return saveSource(app, folder, file, facadeSourceDocument)
			}},
		{name: "SaveItem(project)", file: project.DocumentName, linkRefusal: projectUnread, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				opened := app.OpenNamedProject(folder)
				if opened.Project == nil || opened.Context.ProjectID == "" {
					return saved{opened.State, opened.Reason, nil}
				}
				result := app.SaveItem(desktop.SaveItemRequest{Context: opened.Context, Kind: desktop.ProjectItem, Item: opened.Project.Ref.ID,
					BaseRevision: opened.Project.Ref.Revision, IntentID: "retitle-" + filepath.Base(folder), Draft: desktop.ItemDraft{Project: &desktop.ProjectDraft{
						Name: "Retitled in the workspace", Owner: "integration-team", Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1", Default: true}}}}})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "RecoverProjectDocument", file: project.DocumentName, linkRefusal: projectUnread, prepare: aRecoveryCopy, create: createProject,
			replace: func(app *desktop.App, folder, _ string) saved {
				result := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: folder, Document: project.DocumentName, Digest: earlierDigest})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "SaveNoteItem", file: project.RevisionsDocumentName, linkRefusal: revisionsUnread, prepare: aProject, create: createNote,
			replace: func(app *desktop.App, folder, _ string) saved { return saveNote(app, folder, "Workspace theory") }},
		{name: "RegisterRevision", file: project.RevisionsDocumentName, linkRefusal: revisionsUnread, prepare: aBuiltReproducer, create: createNote,
			replace: func(app *desktop.App, folder, _ string) saved {
				result := app.RegisterRevision(desktop.RevisionRegistration{Workspace: folder, Source: "incident-reproducer", Name: "incident-revision", Parent: "incident"})
				return saved{result.State, result.Reason, nil}
			}},
		{name: "SetProjectQuota", file: project.QuotaDocumentName, linkRefusal: projectDocumentUnread, prepare: aProject,
			create: func(app *desktop.App, folder, _ string) saved {
				aProject(t, folder)
				return setQuota(app, folder, 10_000)
			},
			replace: func(app *desktop.App, folder, _ string) saved { return setQuota(app, folder, 20_000) }},
		// Rebuilding an index removes the one it replaces and creates the new
		// one exclusively; it refuses anything but a regular file there.
		{name: "BuildIndex(Replace)", file: "case.index.json", linkRefusal: "an index destination must be a regular file", prepare: aCase,
			create: func(app *desktop.App, folder, file string) saved {
				aCase(t, folder)
				return buildIndex(app, folder, file, false)
			},
			replace: func(app *desktop.App, folder, file string) saved { return buildIndex(app, folder, file, true) }},
	})
}

func replacesALinkedDocument(t *testing.T, saves []replacingSave) {
	t.Helper()
	links := []struct {
		how      string
		symbolic bool
		plant    func(target, link string) error
	}{
		{"a symbolic link out of the workspace", true, os.Symlink},
		{"a hard link to a file outside the workspace", false, os.Link},
	}
	for _, s := range saves {
		for _, link := range links {
			call := s.name + " over " + link.how
			root, outside := overwriteWorkspaces(t, s.prepare)
			app := workspaceApp(t)
			if created := s.create(app, outside, s.file); created.state != desktop.Completed {
				t.Fatalf("%s cannot write the document outside, so it cannot show a replacement: %+v", s.name, created)
			}
			if err := link.plant(filepath.Join(outside, s.file), filepath.Join(root, s.file)); err != nil {
				t.Fatal(err)
			}
			before := bytesUnder(t, outside)
			got := answeredWithin(t, call, func() saved { return s.replace(app, root, s.file) })
			if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
				t.Errorf("%s changed the document outside the workspace", call)
			}
			if s.linkRefusal != "" && link.symbolic {
				if got.state != desktop.Failed || got.reason != s.linkRefusal {
					t.Errorf("%s: %+v, want the refusal %q", call, got, s.linkRefusal)
				}
				continue
			}
			if got.state != desktop.Completed {
				t.Errorf("%s: %+v, want the entry replaced", call, got)
				continue
			}
			entry, err := os.Lstat(filepath.Join(root, s.file))
			linked, _ := os.Stat(filepath.Join(outside, s.file))
			if err != nil || !entry.Mode().IsRegular() || os.SameFile(entry, linked) {
				t.Errorf("%s left %v at the name, want a regular file of its own", call, entry)
			}
			if _, err := os.Lstat(filepath.Join(root, s.file+".incomplete")); !os.IsNotExist(err) {
				t.Errorf("%s left its replacement's partial file behind", call)
			}
		}
	}
}

// The shell's own documents are written through the one replacement the shell
// document store uses — the same one a workspace save uses — and they are read
// by one rule: a link at a document's name is never read. So a document the
// shell reads before it writes — the saved filters, the working session and the
// editor drafts — reports the link and is left as it
// is, while a selection the person explicitly chooses is written whole,
// replacing the link at its name. Either way the document a link led to keeps
// its bytes.
func TestTheShellsOwnDocumentsNeverFollowALinkAtTheirFile(t *testing.T) {
	workspace, outside, ownState := t.TempDir(), t.TempDir(), t.TempDir()
	readFirst := []string{"filters.json", "session.json", "drafts.json"}
	selections := []string{"operations.json", "commercial.json", "hub.json"}
	configuration := t.TempDir()
	writeDocument(t, configuration, "destinations.json",
		`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test"}`)
	hubConfig := writeHubClientConfig(t, configuration, "https://hub.example.test")
	completes := func(call string, got desktop.State, reason string) {
		t.Helper()
		// An empty workspace is opened, and remembered, as empty.
		if got != desktop.Completed && got != desktop.Empty {
			t.Fatalf("%s: %s %s", call, got, reason)
		}
	}
	// shell wires a window over the documents in folder and selects the
	// test operation policy, which retains the selection there.
	shell := func(folder string) *desktop.App {
		choose := &chooser{files: []string{filepath.Join(configuration, "destinations.json")}}
		app := desktop.NewWithOperationSelection(choose, desktop.ShellDocuments{Folder: folder})
		selected := app.SelectOperationPolicy(testlicense.New(t))
		completes("SelectOperationPolicy", selected.State, selected.Reason)
		return app
	}
	// retain writes every document once.
	retain := func(app *desktop.App, region string) {
		filtered := app.SaveFilter(grid.Filter{Name: region, Kinds: []bundle.EventKind{bundle.Message}})
		completes("SaveFilter", filtered.State, filtered.Reason)
		recorded := app.RecordView(desktop.View{Workspace: workspace, Region: "evidence", Case: region})
		completes("RecordView", recorded.State, recorded.Reason)
		drafted := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote))
		completes("SaveEditorDraft", drafted.State, drafted.Reason)
		chosen := app.ChooseCommercialDestinations()
		completes("ChooseCommercialDestinations", chosen.State, chosen.Reason)
		hub := app.SelectHubConfig(hubConfig)
		completes("SelectHubConfig", hub.State, hub.Reason)
	}
	retain(shell(outside), "outside")
	for _, name := range append(append([]string{}, readFirst...), selections...) {
		if _, err := os.Lstat(filepath.Join(outside, name)); err != nil {
			t.Fatalf("the shell outside wrote no %s: %v", name, err)
		}
		if err := os.Symlink(filepath.Join(outside, name), filepath.Join(ownState, name)); err != nil {
			t.Fatal(err)
		}
	}
	before := bytesUnder(t, outside)

	// The second shell refuses to read a document through the link at its
	// name, so nothing replaces the three documents it reads before it writes,
	// and each is reported rather than followed. The three selections are
	// written whole on the person's explicit choice, replacing their links.
	app := shell(ownState)
	if listed := app.Filters(); listed.State != desktop.Failed || listed.Reason == "" {
		t.Errorf("a linked saved-filter document was read: %+v", listed)
	}
	if recorded := app.RecordView(desktop.View{Workspace: workspace, Region: "evidence", Case: "inside"}); recorded.State != desktop.Failed || recorded.Reason == "" {
		t.Errorf("a linked working session was read or replaced: %+v", recorded)
	}
	if drafted := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); drafted.State != desktop.Failed || drafted.Reason == "" {
		t.Errorf("a linked editor draft store was read or replaced: %+v", drafted)
	}
	chosen := app.ChooseCommercialDestinations()
	completes("ChooseCommercialDestinations", chosen.State, chosen.Reason)
	hub := app.SelectHubConfig(hubConfig)
	completes("SelectHubConfig", hub.State, hub.Reason)

	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Error("a shell document read or written through a link changed the document it led to")
	}
	for _, name := range readFirst {
		if entry, err := os.Lstat(filepath.Join(ownState, name)); err != nil || entry.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is %v, want the link it was planted as: the document was replaced without being read", name, entry)
		}
	}
	for _, name := range selections {
		if entry, err := os.Lstat(filepath.Join(ownState, name)); err != nil || !entry.Mode().IsRegular() {
			t.Errorf("%s is %v, want the shell's own regular file", name, entry)
		}
	}
}
