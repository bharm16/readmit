package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// sessionApp binds one facade to one named working-session document, so a test
// can close the shell by dropping the App and open it again over the same file.
func sessionApp(t *testing.T, store string) *desktop.App {
	t.Helper()
	return desktop.New(&chooser{}, filepath.Join(filepath.Dir(store), "recent.json"), filepath.Join(filepath.Dir(store), "filters.json"), store)
}

func sessionStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.json")
}

func draft(root, name, title, body string) desktop.Draft {
	return desktop.Draft{Project: root, Note: project.Note{Name: name, Title: title, Body: body}}
}

// The whole point of the document: work a person had not stored survives the
// process that held it. The shell is closed by dropping the facade entirely and
// opened again over the same file, which is what a restart is.
func TestRecoverSessionRestoresUnstoredWorkAndWhereItWas(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	if restored := app.RecoverSession(); restored.State != desktop.Empty || restored.Session != nil {
		t.Fatalf("a shell that retained nothing restored something: %+v", restored)
	}

	root := t.TempDir()
	view := desktop.View{Workspace: root, Region: "evidence", Case: "regression"}
	if result := app.RecordView(view); result.State != desktop.Completed {
		t.Fatalf("record view: %+v", result)
	}
	// Two drafts of two different notes, recorded out of order, so recovery is
	// shown to return both rather than the last one to be typed.
	if result := app.SaveDraft(draft(root, "vendor-call", "Ask about MSH-15", "they said the")); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	if result := app.SaveDraft(draft(root, "triage", "First pass", "still writing this")); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	// An edit of a note that already has a draft replaces exactly that draft.
	if result := app.SaveDraft(draft(root, "triage", "First pass", "still writing this one")); result.State != desktop.Completed {
		t.Fatalf("replace draft: %+v", result)
	}

	restored := sessionApp(t, store).RecoverSession()
	if restored.State != desktop.Completed || restored.Session == nil {
		t.Fatalf("a restarted shell restored nothing: %+v", restored)
	}
	if restored.Session.Schema != desktop.SessionSchema {
		t.Fatalf("restored session declares %q", restored.Session.Schema)
	}
	if restored.Session.View != view {
		t.Fatalf("restored view %+v, recorded %+v", restored.Session.View, view)
	}
	if len(restored.Session.Drafts) != 2 {
		t.Fatalf("restored %d drafts, retained 2: %+v", len(restored.Session.Drafts), restored.Session.Drafts)
	}
	if restored.Session.Drafts[0].Note.Name != "triage" || restored.Session.Drafts[1].Note.Name != "vendor-call" {
		t.Fatalf("drafts are not held in a stable order: %+v", restored.Session.Drafts)
	}
	if body := restored.Session.Drafts[0].Note.Body; body != "still writing this one" {
		t.Fatalf("the restored draft is not the last edit: %q", body)
	}
	// No run was being watched, so there is nothing to recover and nothing is
	// invented in its place.
	if restored.Run != nil || restored.RunReason != "" {
		t.Fatalf("a session that watched no run reported one: %+v %q", restored.Run, restored.RunReason)
	}

	// Storing the note makes the draft stale, and discarding it is what stops
	// recovery offering back work that is no longer unstored.
	if result := app.DiscardDraft(root, "triage"); result.State != desktop.Completed {
		t.Fatalf("discard draft: %+v", result)
	}
	after := sessionApp(t, store).RecoverSession()
	if after.State != desktop.Completed || after.Session == nil || len(after.Session.Drafts) != 1 {
		t.Fatalf("discarding did not remove exactly one draft: %+v", after)
	}
	if after.Session.Drafts[0].Note.Name != "vendor-call" {
		t.Fatalf("discarding removed the wrong draft: %+v", after.Session.Drafts)
	}
}

// A draft is working text held outside the project, so retaining one reaches no
// project document and no evidence. Storing it is SaveNote, which is a separate
// deliberate step.
func TestRetainingADraftReachesNoProjectDocumentAndNoEvidence(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, project.DocumentName),
		[]byte(`{"schema":"readmit-project/v1","settings":{"title":"Scheduling"},"interface_versions":["2.5.1"],"cases":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.SaveDraft(draft(root, "triage", "First pass", "unstored")); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("retaining a draft wrote into the project folder: %d entries, was %d", len(after), len(before))
	}
	if revisions := app.OpenRevisions(root); revisions.State != desktop.Empty {
		t.Fatalf("an unstored draft reached the editable project document: %+v", revisions)
	}
	if strings.HasPrefix(store, root) {
		t.Fatalf("the working session is retained inside the project: %s", store)
	}
}

// A document this release cannot read is reported and left exactly as written.
// There is no migration and no repair, and an unreadable session never blocks
// the rest of the window or gets replaced by a write.
func TestSessionStoreRefusesUnknownVersionsMembersAndCorruption(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown version":   `{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":""},"drafts":[]}`,
		"unknown member":    `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"drafts":[],"last_seen":"2026-01-01"}`,
		"unknown view":      `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":"","scroll":3},"drafts":[]}`,
		"relative folder":   `{"schema":"readmit-desktop-session/v1","view":{"workspace":"relative","region":"","case":"","run":""},"drafts":[]}`,
		"traversing case":   `{"schema":"readmit-desktop-session/v1","view":{"workspace":"/w","region":"","case":"../elsewhere","run":""},"drafts":[]}`,
		"undeclared region": `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"somewhere","case":"","run":""},"drafts":[]}`,
		"unsorted drafts":   `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"drafts":[{"project":"/p","note":{"name":"b","title":"B","body":""}},{"project":"/p","note":{"name":"a","title":"A","body":""}}]}`,
		"duplicated draft":  `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"drafts":[{"project":"/p","note":{"name":"a","title":"A","body":""}},{"project":"/p","note":{"name":"a","title":"A","body":""}}]}`,
		"relative draft":    `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"drafts":[{"project":"relative","note":{"name":"a","title":"A","body":""}}]}`,
		"truncated":         `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","regi`,
		"not JSON":          "{",
		"empty":             "",
	} {
		t.Run(name, func(t *testing.T) {
			store := sessionStore(t)
			if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			app := sessionApp(t, store)
			if restored := app.RecoverSession(); restored.State != desktop.Failed || restored.Session != nil {
				t.Fatalf("the session store accepted an %s: %+v", name, restored)
			}
			// A write refuses too, rather than replacing a document nobody has
			// read: whatever wrote it keeps whatever it wrote.
			if result := app.SaveDraft(draft(t.TempDir(), "triage", "First pass", "")); result.State != desktop.Failed {
				t.Fatalf("a draft was retained over an unreadable session: %+v", result)
			}
			data, err := os.ReadFile(store)
			if err != nil || string(data) != contents {
				t.Fatalf("the shell overwrote an unreadable session: %q", data)
			}
			// The rest of the window is unaffected, exactly as an unreadable
			// recent list does not stop a workspace from opening.
			if opened := app.OpenWorkspace(t.TempDir()); opened.State != desktop.Empty {
				t.Fatalf("an unreadable session blocked opening a workspace: %+v", opened)
			}
		})
	}
}

// Recording where a viewer is must not become a way to record a path or a name
// the shell would not open. Every refusal keeps the previously retained view.
func TestRecordingAViewRefusesWhatItCouldNotRestore(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	kept := desktop.View{Workspace: root, Region: "evidence", Case: "regression"}
	if result := app.RecordView(kept); result.State != desktop.Completed {
		t.Fatalf("record view: %+v", result)
	}
	for name, view := range map[string]desktop.View{
		"relative workspace":  {Workspace: "relative"},
		"relative run":        {Run: "relative"},
		"case with a parent":  {Workspace: root, Case: "../elsewhere"},
		"case with a path":    {Workspace: root, Case: "nested/case"},
		"case with no folder": {Case: "regression"},
		"undeclared region":   {Region: "somewhere"},
		"control character":   {Workspace: root + "\n/etc"},
		"oversized workspace": {Workspace: "/" + strings.Repeat("d", 5000)},
	} {
		result := app.RecordView(view)
		if result.State != desktop.Failed {
			t.Fatalf("the session recorded a %s: %+v", name, result)
		}
		if result.Session == nil || result.Session.View != kept {
			t.Fatalf("a refused %s did not report the view that stays retained: %+v", name, result.Session)
		}
	}
	if restored := sessionApp(t, store).RecoverSession(); restored.Session == nil || restored.Session.View != kept {
		t.Fatalf("a refused view replaced the retained one: %+v", restored)
	}
}

// A draft is held to exactly the rule a stored note is held to, so retained
// working text is text the project can accept. Past the bound an edit is
// refused rather than another one being dropped.
func TestRetainingADraftIsBoundedAndHeldToTheNoteRule(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	for name, entry := range map[string]desktop.Draft{
		"relative project":   draft("relative", "triage", "First pass", ""),
		"unnamed note":       draft(root, "", "First pass", ""),
		"untitled note":      draft(root, "triage", "", ""),
		"path in the name":   draft(root, "../triage", "First pass", ""),
		"unbounded body":     draft(root, "triage", "First pass", strings.Repeat("x", 4097)),
		"control in body":    draft(root, "triage", "First pass", "a\rb"),
		"invalid UTF-8 body": draft(root, "triage", "First pass", "\xff"),
	} {
		if result := app.SaveDraft(entry); result.State != desktop.Failed {
			t.Fatalf("the session retained a draft with a %s: %+v", name, result)
		}
	}
	for i := range desktop.MaxDrafts {
		if result := app.SaveDraft(draft(root, "note-"+string(rune('a'+i)), "Draft", "")); result.State != desktop.Completed {
			t.Fatalf("draft %d: %+v", i, result)
		}
	}
	full := app.SaveDraft(draft(root, "one-too-many", "Draft", ""))
	if full.State != desktop.Failed || full.Session == nil || len(full.Session.Drafts) != desktop.MaxDrafts {
		t.Fatalf("the bound dropped a retained draft instead of refusing the new one: %+v", full)
	}
}

// Discarding what is not held is refused rather than reported as discarded, so
// the window cannot believe it cleared work that is still retained.
func TestDiscardingADraftThatIsNotHeldIsRefused(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	if result := app.SaveDraft(draft(root, "triage", "First pass", "unstored")); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	for name, discard := range map[string][2]string{
		"another note":    {root, "vendor-call"},
		"another project": {t.TempDir(), "triage"},
	} {
		result := app.DiscardDraft(discard[0], discard[1])
		if result.State != desktop.Failed {
			t.Fatalf("discarding a draft of %s was not refused: %+v", name, result)
		}
		if result.Session == nil || len(result.Session.Drafts) != 1 {
			t.Fatalf("a refused discard changed what is retained: %+v", result.Session)
		}
	}
}

// The session is one owner-readable file replaced in full, so a reader never
// observes a partial document and an interrupted write is reported rather than
// overwritten.
func TestSessionIsWrittenCompletelyAndPrivately(t *testing.T) {
	store := filepath.Join(t.TempDir(), "state", "session.json")
	app := sessionApp(t, store)
	root := t.TempDir()
	if result := app.SaveDraft(draft(root, "triage", "First pass", "unstored")); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	info, err := os.Stat(store)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("the working session is readable beyond its owner: %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	var stored desktop.Session
	if err := json.Unmarshal(data, &stored, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the stored session is not the strict document this release reads: %v", err)
	}
	if stored.Schema != desktop.SessionSchema {
		t.Fatalf("the stored session declares %q", stored.Schema)
	}

	incomplete := store + ".incomplete"
	if err := os.WriteFile(incomplete, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.SaveDraft(draft(root, "vendor-call", "Ask about MSH-15", ""))
	if result.State != desktop.Failed {
		t.Fatalf("a retained interrupted write was reused: %+v", result)
	}
	if retained, err := os.ReadFile(incomplete); err != nil || string(retained) != "partial" {
		t.Fatalf("the interrupted write was overwritten: %q %v", retained, err)
	}
	if kept, err := os.ReadFile(store); err != nil || string(kept) != string(data) {
		t.Fatalf("a refused write changed the retained session: %q", kept)
	}
}

// A run the session names but cannot verify is reported as unverifiable. It is
// never reconstructed, and nothing is resumed or resent to find out more.
func TestRecoveringAnUnverifiableRunReportsItWithoutResuming(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	missing := filepath.Join(t.TempDir(), "job")
	if result := app.RecordView(desktop.View{Run: missing}); result.State != desktop.Completed {
		t.Fatalf("record view: %+v", result)
	}
	restored := app.RecoverSession()
	if restored.State != desktop.Completed || restored.Session == nil {
		t.Fatalf("recovery of the view failed because a run could not be read: %+v", restored)
	}
	if restored.Run != nil {
		t.Fatalf("an unverifiable run was reported as a run: %+v", restored.Run)
	}
	if restored.RunReason == "" {
		t.Fatal("a remembered run was silently absent from the recovery")
	}
	if restored.Session.View.Run != missing {
		t.Fatalf("the recovery lost the run the session was watching: %+v", restored.Session.View)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("recovery created something at the run path: %v", err)
	}
}

// Retaining working state must not wait for the operation slot: a crash while a
// case is being verified is exactly when unstored work has to survive. Recovery
// does claim the slot, because it verifies retained run evidence.
func TestRetainingWorkDoesNotWaitForTheOperationSlot(t *testing.T) {
	store := sessionStore(t)
	root := t.TempDir()
	var app *desktop.App
	var during desktop.SessionResult
	var recovering desktop.RecoveryResult
	reentrant := &chooser{folder: root, before: func() {
		during = app.SaveDraft(draft(root, "triage", "First pass", "typed while busy"))
		recovering = app.RecoverSession()
	}}
	app = desktop.New(reentrant, filepath.Join(filepath.Dir(store), "recent.json"), filepath.Join(filepath.Dir(store), "filters.json"), store)
	if opened := app.SelectWorkspace(); opened.State != desktop.Empty {
		t.Fatalf("select workspace: %+v", opened)
	}
	if during.State != desktop.Completed {
		t.Fatalf("a draft typed while an operation ran was not retained: %+v", during)
	}
	if recovering.State != desktop.Busy {
		t.Fatalf("recovery ran beside another operation: %+v", recovering)
	}
	restored := sessionApp(t, store).RecoverSession()
	if restored.Session == nil || len(restored.Session.Drafts) != 1 {
		t.Fatalf("the draft typed during an operation did not survive: %+v", restored)
	}
}

// Concurrent writes of one document must not interleave. Every one of them
// either stores a complete document or reports a refusal, and what is read back
// is one of the documents that were written.
func TestConcurrentSessionWritesNeverProduceAPartialDocument(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	names := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"}
	var wait sync.WaitGroup
	for _, name := range names {
		wait.Add(1)
		go func() {
			defer wait.Done()
			app.SaveDraft(draft(root, name, "Draft", "unstored"))
		}()
	}
	wait.Wait()
	restored := sessionApp(t, store).RecoverSession()
	if restored.State != desktop.Completed || restored.Session == nil {
		t.Fatalf("concurrent writes left an unreadable session: %+v", restored)
	}
	if len(restored.Session.Drafts) != len(names) {
		t.Fatalf("concurrent writes lost drafts: %+v", restored.Session.Drafts)
	}
}

// A restored draft is the canonical editable note, so the way to finish
// recovering is to store it. This closes the loop the crash opens: the text
// comes back, the person stores it in the project's own document, and the draft
// stops being offered because it is no longer unstored.
func TestARestoredDraftIsStoredThroughTheProjectDocument(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, project.DocumentName),
		[]byte(`{"schema":"readmit-project/v1","settings":{"title":"Scheduling"},"interface_versions":["2.5.1"],"cases":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	unstored := draft(root, "triage", "First pass", "the second S13 keeps the original filler identifier")
	if result := app.SaveDraft(unstored); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}

	restored := sessionApp(t, store).RecoverSession()
	if restored.Session == nil || len(restored.Session.Drafts) != 1 {
		t.Fatalf("the draft was not restored: %+v", restored)
	}
	recovered := restored.Session.Drafts[0]
	stored := app.SaveNote(recovered.Project, recovered.Note)
	if stored.State != desktop.Completed || stored.Revisions == nil || len(stored.Revisions.Notes) != 1 {
		t.Fatalf("the restored draft was not storable as a note: %+v", stored)
	}
	if stored.Revisions.Notes[0].Body != unstored.Note.Body {
		t.Fatalf("storing the restored draft changed its text: %q", stored.Revisions.Notes[0].Body)
	}
	if result := app.DiscardDraft(recovered.Project, recovered.Note.Name); result.State != desktop.Completed {
		t.Fatalf("discard draft: %+v", result)
	}
	// Nothing unstored is left to offer back, and the stored note is where the
	// command line reads it.
	after := sessionApp(t, store).RecoverSession()
	if after.Session != nil && len(after.Session.Drafts) != 0 {
		t.Fatalf("a stored note is still offered as unstored work: %+v", after.Session.Drafts)
	}
	written, err := project.ReadRevisions(root)
	if err != nil || len(written.Notes) != 1 || written.Notes[0].Body != unstored.Note.Body {
		t.Fatalf("the editable project document does not hold the stored note: %+v %v", written, err)
	}
}

// A draft may name the case or revision it is about, and whether the project
// registers that subject is settled when the note is stored, not while it is
// being typed: a draft is written before the project is opened. A store the
// project refuses leaves the text retained, because text the project did not
// take is still unstored work.
func TestADraftKeepsItsSubjectAndSurvivesARefusedStore(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, project.DocumentName),
		[]byte(`{"schema":"readmit-project/v1","settings":{"title":"Scheduling"},"interface_versions":["2.5.1"],"cases":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	about := desktop.Draft{Project: root, Note: project.Note{
		Name: "triage", Subject: "regression", Title: "First pass", Body: "the second S13 keeps the filler identifier"}}
	if result := app.SaveDraft(about); result.State != desktop.Completed {
		t.Fatalf("a draft naming a subject was refused: %+v", result)
	}

	// The project registers no case of that name, so storing it is refused.
	stored := app.SaveNote(root, about.Note)
	if stored.State != desktop.Failed {
		t.Fatalf("a note about unregistered evidence was stored: %+v", stored)
	}
	restored := sessionApp(t, store).RecoverSession()
	if restored.Session == nil || len(restored.Session.Drafts) != 1 {
		t.Fatalf("a refused store discarded the unstored work: %+v", restored)
	}
	if restored.Session.Drafts[0].Note != about.Note {
		t.Fatalf("the retained draft is not what was typed: %+v", restored.Session.Drafts[0].Note)
	}
	if written, err := project.ReadRevisions(root); err != nil || len(written.Notes) != 0 {
		t.Fatalf("a refused store reached the editable project document: %+v %v", written, err)
	}
}
