package desktop_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// draftsApp binds one facade to one named editor draft store, so a test can
// close the shell by dropping the App and open it again over the same file.
// The store lives beside the working session the harness is given, which is
// where the application names it.
func draftsApp(t *testing.T, store string) *desktop.App {
	t.Helper()
	return activatedApp(t, &chooser{}, filepath.Join(filepath.Dir(store), "recent.json"), filepath.Join(filepath.Dir(store), "filters.json"), filepath.Join(filepath.Dir(store), "session.json"))
}

func draftsStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "drafts.json")
}

// editorDraft builds one draft of a given kind with its content as JSON text,
// the way the editors hand their work over: content the owning editor's own
// contract declares, never a final artifact.
func editorDraft(kind, contentSchema, content string) desktop.EditorDraft {
	return desktop.EditorDraft{
		Kind:          kind,
		Workspace:     "/investigation",
		Case:          "regression",
		Identity:      "CASE-IDENTITY",
		ContentSchema: contentSchema,
		Content:       jsontext.Value(content),
	}
}

// revisionBody is the unstored text of one edit of a note, as noteRevision
// writes it. Its length changes with every revision, so a document torn
// between two replacements cannot parse as any single one of them.
var revisionBody = regexp.MustCompile(`^revision ([0-9]{6}) (x*)$`)

func noteRevision(revision int) string {
	body := fmt.Sprintf("revision %06d %s", revision, strings.Repeat("x", revision%512))
	encoded, err := json.Marshal(desktop.NoteDraft{Schema: desktop.NoteDraftSchema, Body: body})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

const unfinishedNote = `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"still writing this"}`
const answeredTestDraft = `{"schema":"readmit-test-draft/v1","case":{"entry":"regression","identity":"CASE-IDENTITY"},"name":"","messages":[],"target":"","boundary":"","observation":"","reset":"","expectations":[]}`
const editedReproducerPlan = `{"schema":"readmit-reproducer-plan/v1","case":"regression","steps":[{"operator":"select-occurrence/v1","occurrence":"o0001"}]}`
const canonicalEdits = `"COMPLETE-CANONICAL-DOCUMENT-TEXT"`

// The whole point of the store: an editor's unstored work survives the process
// that held it, including work that could not be a final artifact yet — a note
// with no name and no title, a half-answered test draft, a plan still being
// edited, and a canonical document that would not pass the reader today.
func TestEditorDraftsRoundTripIncludingAnUnfinishedNote(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	if result := app.EditorDrafts(); result.State != desktop.Empty || len(result.Drafts) != 0 {
		t.Fatalf("a shell that retained no editor drafts reported some: %+v", result)
	}

	kept := map[string]desktop.EditorDraft{
		"note":       editorDraft("note", desktop.NoteDraftSchema, unfinishedNote),
		"test":       editorDraft("test-draft", "readmit-test-draft/v1", answeredTestDraft),
		"reproducer": editorDraft("reproducer-plan", "readmit-reproducer-plan/v1", editedReproducerPlan),
		"canonical":  editorDraft("canonical-test", "readmit-test/v1", canonicalEdits),
	}
	ids := map[string]string{}
	held := 0
	seen := map[string]bool{}
	for name, draft := range kept {
		held += 1
		result := app.SaveEditorDraft(draft)
		if result.State != desktop.Completed || len(result.Drafts) != held {
			t.Fatalf("the %s draft was not retained: %+v", name, result)
		}
		for _, heldDraft := range result.Drafts {
			if !seen[heldDraft.ID] {
				ids[name] = heldDraft.ID
				seen[heldDraft.ID] = true
			}
		}
		if ids[name] == "" {
			t.Fatalf("retaining a %s draft minted no identity", name)
		}
	}

	restored := draftsApp(t, store).EditorDrafts()
	if restored.State != desktop.Completed || len(restored.Drafts) != len(kept) {
		t.Fatalf("a restarted shell restored %d drafts, retained %d: %+v", len(restored.Drafts), len(kept), restored)
	}
	for _, draft := range restored.Drafts {
		name := ""
		for candidate, id := range ids {
			if id == draft.ID {
				name = candidate
			}
		}
		if name == "" {
			t.Fatalf("a draft came back under an identity nothing was retained under: %+v", draft)
		}
		was := kept[name]
		if draft.Kind != was.Kind || draft.Workspace != was.Workspace || draft.Case != was.Case ||
			draft.Identity != was.Identity || draft.ContentSchema != was.ContentSchema || draft.Content.String() != was.Content.String() {
			t.Fatalf("the %s draft came back changed: %+v, retained %+v", name, draft, was)
		}
	}

	// An edit of a draft the viewer holds replaces exactly that draft, under
	// the identity it was minted.
	replacement := editorDraft("note", desktop.NoteDraftSchema,
		`{"schema":"readmit-note-draft/v1","name":"triage","subject":"","title":"First pass","body":"still writing this"}`)
	replacement.ID = ids["note"]
	if result := app.SaveEditorDraft(replacement); result.State != desktop.Completed || len(result.Drafts) != len(kept) {
		t.Fatalf("replacing a draft changed how many are retained: %+v", result)
	}
	after := draftsApp(t, store).EditorDrafts()
	for _, draft := range after.Drafts {
		if draft.ID == ids["note"] && draft.Content.String() != replacement.Content.String() {
			t.Fatalf("the retained draft is not the newest edit: %+v", draft)
		}
	}
}

// Retaining work must not wait for the operation slot: a crash while a case is
// being verified is exactly when unstored work has to survive.
func TestRetainingEditorDraftsDoesNotWaitForTheOperationSlot(t *testing.T) {
	store := draftsStore(t)
	root := t.TempDir()
	var app *desktop.App
	var during desktop.EditorDraftsResult
	reentrant := &chooser{folder: root, before: func() {
		during = app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote))
	}}
	app = activatedApp(t, reentrant, filepath.Join(filepath.Dir(store), "recent.json"), filepath.Join(filepath.Dir(store), "filters.json"), filepath.Join(filepath.Dir(store), "session.json"))
	if opened := app.SelectWorkspace(); opened.State != desktop.Empty {
		t.Fatalf("select workspace: %+v", opened)
	}
	if during.State != desktop.Completed {
		t.Fatalf("a draft typed while an operation ran was not retained: %+v", during)
	}
	restored := draftsApp(t, store).EditorDrafts()
	if restored.State != desktop.Completed || len(restored.Drafts) != 1 {
		t.Fatalf("the draft typed during an operation did not survive: %+v", restored)
	}
}

// Concurrent writes of one document must not interleave. Every one of them
// either stores a complete document or reports a refusal, and what is read back
// is one of the documents that were written.
func TestConcurrentEditorDraftWritesNeverProduceAPartialDocument(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	kinds := []string{"note", "test-draft", "reproducer-plan", "canonical-test", "checklist", "finding-review"}
	var wait sync.WaitGroup
	for i, kind := range kinds {
		wait.Add(1)
		go func(i int, kind string) {
			defer wait.Done()
			app.SaveEditorDraft(editorDraft(kind, "readmit-test-draft/v1", answeredTestDraft))
		}(i, kind)
	}
	wait.Wait()
	restored := draftsApp(t, store).EditorDrafts()
	if restored.State != desktop.Completed {
		t.Fatalf("concurrent writes left an unreadable store: %+v", restored)
	}
	if len(restored.Drafts) != len(kinds) {
		t.Fatalf("concurrent writes lost drafts: %+v", restored.Drafts)
	}
}

// A document this release cannot read is reported and left exactly as written.
// There is no migration and no repair, and an unreadable store never blocks the
// rest of the window or gets replaced by a write.
func TestEditorDraftStoreRefusesUnknownVersionsMembersAndCorruption(t *testing.T) {
	held := `{"schema":"readmit-desktop-drafts/v1","drafts":[{"id":"a","kind":"note","workspace":"/w","case":"","identity":"","content_schema":"readmit-note-draft/v1","content":{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":""}}]}`
	for name, contents := range map[string]string{
		"unknown version":   strings.Replace(held, "drafts/v1", "drafts/v2", 1),
		"unknown member":    strings.Replace(held, `"drafts":[`, `"last_seen":"2026-01-01","drafts":[`, 1),
		"unknown in draft":  strings.Replace(held, `"case":""`, `"scroll":3,"case":""`, 1),
		"missing content":   strings.Replace(held, `,"content":{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":""}`, ``, 1),
		"invalid document":  strings.Replace(held, `"content":{`, `"content":<`, 1),
		"unsorted":          `{"schema":"readmit-desktop-drafts/v1","drafts":[{"id":"b","kind":"checklist","workspace":"/w","case":"","identity":"","content_schema":"readmit-checklist/v1","content":{}},{"id":"a","kind":"checklist","workspace":"/w","case":"","identity":"","content_schema":"readmit-checklist/v1","content":{}}]}`,
		"duplicated":        `{"schema":"readmit-desktop-drafts/v1","drafts":[{"id":"a","kind":"checklist","workspace":"/w","case":"","identity":"","content_schema":"readmit-checklist/v1","content":{}},{"id":"a","kind":"checklist","workspace":"/w","case":"","identity":"","content_schema":"readmit-checklist/v1","content":{}}]}`,
		"relative folder":   strings.Replace(held, `"workspace":"/w"`, `"workspace":"relative"`, 1),
		"traversing case":   strings.Replace(held, `"case":""`, `"case":"../elsewhere"`, 1),
		"uppercase kind":    strings.Replace(held, `"kind":"note"`, `"kind":"NOTE"`, 1),
		"control in kind":   strings.Replace(held, `"kind":"note"`, `"kind":"no\u0000te"`, 1),
		"no content schema": strings.Replace(held, `"content_schema":"readmit-note-draft/v1"`, `"content_schema":""`, 1),
		"oversized":         strings.Repeat("a", 1<<20+1),
		"truncated":         `{"schema":"readmit-desktop-drafts/v1","dra`,
		"not JSON":          "{",
		"empty":             "",
	} {
		t.Run(name, func(t *testing.T) {
			store := draftsStore(t)
			if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			app := draftsApp(t, store)
			if restored := app.EditorDrafts(); restored.State != desktop.Failed {
				t.Fatalf("the draft store accepted %s: %+v", name, restored)
			}
			// A write refuses too, rather than replacing a document nobody has
			// read: whatever wrote it keeps whatever it wrote.
			if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.Failed {
				t.Fatalf("a draft was retained over an unreadable store: %+v", result)
			}
			data, err := os.ReadFile(store)
			if err != nil || string(data) != contents {
				t.Fatalf("the shell overwrote an unreadable draft store: %q", data)
			}
			// The rest of the window is unaffected, exactly as an unreadable
			// working session does not stop a workspace from opening.
			if opened := app.OpenWorkspace(t.TempDir()); opened.State != desktop.Empty {
				t.Fatalf("an unreadable draft store blocked opening a workspace: %+v", opened)
			}
		})
	}
}

// The note editor's draft is looser than a stored note in exactly one direction:
// it may still have no name and no title. Whatever it declares must be text the
// project could accept.
func TestANoteDraftMayBeUnfinishedButNotUnacceptable(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	for name, note := range map[string]string{
		"no name or title":   unfinishedNote,
		"body only":          `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"the second S13 keeps the filler"}`,
		"name without title": `{"schema":"readmit-note-draft/v1","name":"triage","subject":"","title":"","body":""}`,
	} {
		if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, note)); result.State != desktop.Completed {
			t.Fatalf("an unfinished note (%s) was not retained: %+v", name, result)
		}
	}
	for name, note := range map[string]string{
		"wrong contract":    `{"schema":"readmit-note/v1","name":"","subject":"","title":"","body":""}`,
		"unknown member":    `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"","tags":[]}`,
		"invalid name":      `{"schema":"readmit-note-draft/v1","name":"has space","subject":"","title":"","body":""}`,
		"oversized name":    `{"schema":"readmit-note-draft/v1","name":"` + strings.Repeat("x", 65) + `","subject":"","title":"","body":""}`,
		"control in body":   `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"a\rb"}`,
		"oversized body":    `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"` + strings.Repeat("x", 4097) + `"}`,
		"invalid UTF-8":     `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"` + "\xff" + `"}`,
		"oversized content": `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"` + strings.Repeat("x", 4096) + `","pad":"` + strings.Repeat("y", 1<<18) + `"}`,
	} {
		if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, note)); result.State != desktop.Failed {
			t.Fatalf("the store retained a note with %s: %+v", name, result)
		}
	}
}

// Past the bound the new draft is refused rather than another one being
// dropped, and past the content bound the draft is refused rather than
// truncated. Both refusals report what stays retained.
func TestRetainingEditorDraftsIsBounded(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	root := t.TempDir()
	for i := range desktop.MaxEditorDrafts {
		draft := editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)
		draft.Workspace = filepath.Join(root, "case-"+string(rune('a'+i)))
		if result := app.SaveEditorDraft(draft); result.State != desktop.Completed {
			t.Fatalf("draft %d: %+v", i, result)
		}
	}
	full := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote))
	if full.State != desktop.Failed || len(full.Drafts) != desktop.MaxEditorDrafts {
		t.Fatalf("the bound dropped a retained draft instead of refusing the new one: %+v", full)
	}
	oversized := editorDraft("canonical-test", "readmit-test/v1", `"`+strings.Repeat("x", 1<<18+1)+`"`)
	if result := app.SaveEditorDraft(oversized); result.State != desktop.Failed || len(result.Drafts) != desktop.MaxEditorDrafts {
		t.Fatalf("the content bound truncated or dropped what is retained: %+v", result)
	}
	if restored := draftsApp(t, store).EditorDrafts(); len(restored.Drafts) != desktop.MaxEditorDrafts {
		t.Fatalf("the refused drafts changed what is retained: %+v", restored)
	}
}

// Discarding what is not held is refused rather than reported as discarded, and
// an edit naming an identity the store no longer holds is refused rather than
// resurrecting a dropped draft: a window that raced a discard is told so.
func TestStaleEditorDraftIdentitiesAreRefused(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	retained := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote))
	if retained.State != desktop.Completed || len(retained.Drafts) != 1 {
		t.Fatalf("save draft: %+v", retained)
	}
	id := retained.Drafts[0].ID

	stale := editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)
	stale.ID = "identity-nothing-was-retained-under"
	if result := app.SaveEditorDraft(stale); result.State != desktop.Failed {
		t.Fatalf("an edit of a dropped draft was not refused: %+v", result)
	}
	if result := app.DiscardEditorDraft("identity-nothing-was-retained-under"); result.State != desktop.Failed {
		t.Fatalf("discarding a draft that is not held was not refused: %+v", result)
	}
	if len(retained.Drafts) != 1 {
		t.Fatalf("a refused stale edit changed what is retained: %+v", retained)
	}

	// Dropping the draft makes every identity that names it stale.
	if result := app.DiscardEditorDraft(id); result.State != desktop.Completed || len(result.Drafts) != 0 {
		t.Fatalf("discard draft: %+v", result)
	}
	if result := app.SaveEditorDraft(stale); result.State != desktop.Failed {
		t.Fatalf("an edit raced past a discard without being told: %+v", result)
	}
	if restored := draftsApp(t, store).EditorDrafts(); len(restored.Drafts) != 0 {
		t.Fatalf("a refused stale edit resurrected the dropped draft: %+v", restored)
	}
}

// The store is one owner-readable file replaced in full, so a reader never
// observes a partial document and an interrupted write is reported rather than
// overwritten or reused.
func TestEditorDraftsAreWrittenCompletelyAndPrivately(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	info, err := os.Stat(store)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("the editor drafts are readable beyond their owner: %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Schema string                `json:"schema"`
		Drafts []desktop.EditorDraft `json:"drafts"`
	}
	if err := json.Unmarshal(data, &stored, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the stored drafts are not the strict document this release reads: %v", err)
	}
	if stored.Schema != desktop.DraftsSchema {
		t.Fatalf("the stored drafts declare %q", stored.Schema)
	}

	incomplete := store + ".incomplete"
	if err := os.WriteFile(incomplete, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.SaveEditorDraft(editorDraft("test-draft", "readmit-test-draft/v1", answeredTestDraft))
	if result.State != desktop.Failed {
		t.Fatalf("a retained interrupted write was reused: %+v", result)
	}
	if retained, err := os.ReadFile(incomplete); err != nil || string(retained) != "partial" {
		t.Fatalf("the interrupted write was overwritten: %q %v", retained, err)
	}
	if kept, err := os.ReadFile(store); err != nil || string(kept) != string(data) {
		t.Fatalf("a refused write changed the retained drafts: %q", kept)
	}
}

// A restored note draft is the same working text the note editor holds, so the
// way to finish recovering is to store it through the project document — and a
// draft may name a subject the project has not been asked about yet, exactly as
// the working session's own note drafts may.
func TestARestoredNoteDraftIsStoredThroughTheProjectDocument(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, project.DocumentName),
		[]byte(`{"schema":"readmit-project/v1","settings":{"title":"Scheduling"},"interface_versions":["2.5.1"],"cases":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := "the second S13 keeps the original filler identifier"
	retained := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema,
		`{"schema":"readmit-note-draft/v1","name":"triage","subject":"","title":"First pass","body":"`+body+`"}`))
	if retained.State != desktop.Completed || len(retained.Drafts) != 1 {
		t.Fatalf("the draft was not retained: %+v", retained)
	}
	rebound := retained.Drafts[0]
	rebound.Workspace = root
	if result := app.SaveEditorDraft(rebound); result.State != desktop.Completed {
		t.Fatalf("the draft was not bound to the project folder: %+v", result)
	}

	restored := draftsApp(t, store).EditorDrafts()
	if restored.State != desktop.Completed || len(restored.Drafts) != 1 {
		t.Fatalf("the draft was not restored: %+v", restored)
	}
	var note project.Note
	if err := json.Unmarshal(restored.Drafts[0].Content, &note); err != nil {
		t.Fatalf("the restored note draft is not the note the editor holds: %v", err)
	}
	if note.Body != body {
		t.Fatalf("the restored draft is not what was typed: %+v", note)
	}
	stored := app.SaveNote(root, note)
	if stored.State != desktop.Completed || stored.Revisions == nil || len(stored.Revisions.Notes) != 1 {
		t.Fatalf("the restored draft was not storable as a note: %+v", stored)
	}
	if result := app.DiscardEditorDraft(restored.Drafts[0].ID); result.State != desktop.Completed {
		t.Fatalf("discard draft: %+v", result)
	}
	after := draftsApp(t, store).EditorDrafts()
	if len(after.Drafts) != 0 {
		t.Fatalf("a stored note is still offered as unstored work: %+v", after.Drafts)
	}
	written, err := project.ReadRevisions(root)
	if err != nil || len(written.Notes) != 1 || written.Notes[0].Body != body {
		t.Fatalf("the editable project document does not hold the stored note: %+v %v", written, err)
	}
}
