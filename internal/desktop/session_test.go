package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// sessionApp binds one facade to one named working-session document, so a test
// can close the shell by dropping the App and open it again over the same file.
func sessionApp(t *testing.T, store string) *desktop.App {
	t.Helper()
	return activatedApp(t, &chooser{}, filepath.Dir(store))
}

func sessionStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.json")
}

// storedSession reads the retained session document as the strict document
// this release writes.
func storedSession(t *testing.T, store string) desktop.Session {
	t.Helper()
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	var stored desktop.Session
	if err := json.Unmarshal(data, &stored, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the stored session is not the strict document this release writes: %v", err)
	}
	return stored
}

func TestSessionStoreRefusesUnknownVersionsMembersAndCorruption(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown version":   `{"schema":"readmit-desktop-session/v4","view":{"workspace":"","region":"","case":"","run":""}}`,
		"unknown member":    `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"last_seen":"2026-01-01"}`,
		"unknown view":      `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":"","scroll":3}}`,
		"relative folder":   `{"schema":"readmit-desktop-session/v1","view":{"workspace":"relative","region":"","case":"","run":""}}`,
		"traversing case":   `{"schema":"readmit-desktop-session/v1","view":{"workspace":"/w","region":"","case":"../elsewhere","run":""}}`,
		"undeclared region": `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"somewhere","case":"","run":""}}`,
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
			// A write refuses rather than replacing a document nobody has
			// read: whatever wrote it keeps whatever it wrote.
			if result := app.RecordView(desktop.View{Workspace: t.TempDir()}); result.State != desktop.Failed || result.Session != nil {
				t.Fatalf("a view was recorded over an unreadable session: %+v", result)
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

// A session an earlier release wrote may carry the note drafts it retained
// there. They are read past, whatever they hold, and never written again.
func TestAnEarlierSessionsDraftsAreReadPastAndNotKept(t *testing.T) {
	store := sessionStore(t)
	earlier := `{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},` +
		`"drafts":[{"project":"/p","note":{"name":"triage","title":"First pass","body":"still writing"}}]}`
	if err := os.WriteFile(store, []byte(earlier), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if result := sessionApp(t, store).RecordView(desktop.View{Workspace: root}); result.State != desktop.Completed {
		t.Fatalf("an earlier session was refused: %+v", result)
	}
	data, err := os.ReadFile(store)
	if err != nil || strings.Contains(string(data), "drafts") || storedSession(t, store).View.Workspace != root {
		t.Fatalf("the rewritten session: %s %v", data, err)
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
	if stored := storedSession(t, store); stored.View != kept {
		t.Fatalf("a refused view replaced the retained one: %+v", stored)
	}
}

// The session is one owner-readable file whose stored bytes are the strict
// document this release reads. What a replacement owes a reader — never a
// partial document, an interrupted write reported rather than reused — is the
// shell document store's, tested once where the store lives.
func TestSessionIsWrittenCompletelyAndPrivately(t *testing.T) {
	store := filepath.Join(t.TempDir(), "state", "session.json")
	app := sessionApp(t, store)
	if result := app.RecordView(desktop.View{Workspace: t.TempDir(), Region: "evidence"}); result.State != desktop.Completed {
		t.Fatalf("record view: %+v", result)
	}
	info, err := os.Stat(store)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("the working session is readable beyond its owner: %v", info.Mode().Perm())
	}
	if stored := storedSession(t, store); stored.Schema != desktop.SessionSchema {
		t.Fatalf("the stored session declares %q", stored.Schema)
	}
}

// A run a window was watching but cannot verify is reported as unverifiable.
// It is never reconstructed, and nothing is resumed or resent to find out more.
func TestRecoveringAnUnverifiableRunReportsItWithoutResuming(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	missing := filepath.Join(t.TempDir(), "job")
	if result := app.RecordView(desktop.View{Run: missing}); result.State != desktop.Completed {
		t.Fatalf("record view: %+v", result)
	}
	reopened := sessionApp(t, store).OpenDurableRun(storedSession(t, store).View.Run)
	if reopened.State != desktop.Failed || reopened.Run != nil || reopened.Reason == "" {
		t.Fatalf("an unverifiable run was reported as a run: %+v", reopened)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("reopening created something at the run path: %v", err)
	}
}

// Recording where a viewer is must not wait for the operation slot: a crash
// while a case is being verified is exactly when that view has to survive.
func TestRetainingWorkDoesNotWaitForTheOperationSlot(t *testing.T) {
	store := sessionStore(t)
	root := t.TempDir()
	var app *desktop.App
	var during desktop.SessionResult
	reentrant := &chooser{folder: root, before: func() {
		during = app.RecordView(desktop.View{Workspace: root, Region: "evidence"})
	}}
	app = activatedApp(t, reentrant, filepath.Dir(store))
	if opened := app.SelectWorkspace(); opened.State != desktop.Empty {
		t.Fatalf("select workspace: %+v", opened)
	}
	if during.State != desktop.Completed || storedSession(t, store).View.Workspace != root {
		t.Fatalf("a view recorded while an operation ran was not retained: %+v", during)
	}
}

// Concurrent writes of one document must not interleave. Every one of them
// either stores a complete document or reports a refusal, and what is read back
// is one of the documents that were written.
func TestConcurrentSessionWritesNeverProduceAPartialDocument(t *testing.T) {
	store := sessionStore(t)
	app := sessionApp(t, store)
	var roots []string
	for range 6 {
		roots = append(roots, t.TempDir())
	}
	var wait sync.WaitGroup
	for _, root := range roots {
		wait.Add(1)
		go func() {
			defer wait.Done()
			app.RecordView(desktop.View{Workspace: root})
		}()
	}
	wait.Wait()
	if stored := storedSession(t, store); !slices.Contains(roots, stored.View.Workspace) {
		t.Fatalf("concurrent writes left a view nobody recorded: %+v", stored)
	}
}

func TestWorkingSessionRestoresNavigationAndReadsV1WithoutRewriting(t *testing.T) {
	store := sessionStore(t)
	root := t.TempDir()
	legacy := `{"schema":"readmit-desktop-session/v1","view":{"workspace":"` + root + `","region":"evidence","case":"case-1","run":""}}`
	if err := os.WriteFile(store, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	app := sessionApp(t, store)
	got := app.WorkingSession()
	if got.State != desktop.Completed || got.Session == nil || got.Session.View.Case != "case-1" {
		t.Fatalf("legacy session was not readable: %+v", got)
	}
	untouched, err := os.ReadFile(store)
	if err != nil || string(untouched) != legacy {
		t.Fatal("reading rewrote the legacy session")
	}
	nav := &desktop.ViewNavigation{Destination: "cases", SourceIdentity: "sha256:" + strings.Repeat("a", 64), Occurrence: "s0001-e000002", FieldPath: "MSH-9.2", Filter: "Only reschedules", ScrollTop: 440}
	if saved := app.RecordView(desktop.View{Workspace: root, Region: "inspector", Case: "case-1", Navigation: nav}); saved.State != desktop.Completed {
		t.Fatalf("navigation was not recorded: %+v", saved)
	}
	reopened := sessionApp(t, store).WorkingSession()
	if reopened.State != desktop.Completed || reopened.Session == nil || reopened.Session.Schema != desktop.SessionSchema || reopened.Session.View.Navigation == nil || !reflect.DeepEqual(reopened.Session.View.Navigation, nav) {
		t.Fatalf("navigation did not reopen: %+v", reopened)
	}
}

func TestWorkingSessionRejectsUnknownConsentAndLegacyNavigationWithoutRewriting(t *testing.T) {
	for _, contents := range []string{
		`{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":"","navigation":null}}`,
		`{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","consent":true}}}`,
		`{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","value":"source text"}}}`,
	} {
		store := sessionStore(t)
		if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		result := sessionApp(t, store).WorkingSession()
		if result.State != desktop.Failed || result.Session != nil {
			t.Fatalf("unknown navigation was accepted: %+v", result)
		}
		raw, err := os.ReadFile(store)
		if err != nil || string(raw) != contents {
			t.Fatal("a refused session was rewritten")
		}
	}
}

func TestWorkingSessionV3RetainsOnlyPinnedReferenceSelectionsAndReadsV2(t *testing.T) {
	store := sessionStore(t)
	legacy := `{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages"}}}`
	if err := os.WriteFile(store, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	app := sessionApp(t, store)
	if result := app.WorkingSession(); result.State != desktop.Completed || result.Session.Schema != desktop.SessionSchema {
		t.Fatalf("v2 was not adapted: %+v", result)
	}
	raw, err := os.ReadFile(store)
	if err != nil || string(raw) != legacy {
		t.Fatal("v2 read changed its bytes")
	}
	pin := desktop.HL7ReferenceSelection{Documentation: filepath.Join(t.TempDir(), "local.txt"), DocumentationIdentity: "sha256:" + strings.Repeat("a", 64)}
	nav := &desktop.ViewNavigation{Destination: "messages", ReferenceSelection: pin}
	if result := app.RecordView(desktop.View{Navigation: nav}); result.State != desktop.Completed {
		t.Fatalf("selection refused: %+v", result)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Completed || result.Session.View.Navigation.ReferenceSelection != pin {
		t.Fatalf("selection did not reopen: %+v", result)
	}
	for _, schema := range []string{"readmit-desktop-session/v2", desktop.SessionSchema} {
		contents := `{"schema":"` + schema + `","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","reference_selection":{"documentation":"/local.txt","documentation_identity":"sha256:` + strings.Repeat("a", 64) + `","text":"private text"}}}}`
		if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Failed {
			t.Fatalf("unversioned or private text accepted: %+v", result)
		}
	}
	contents := `{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","reference_selection":null}}}`
	if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Failed {
		t.Fatalf("v3 member accepted under v2: %+v", result)
	}
}

func TestWorkingSessionRetainsOrderedFileSelectionAndRefusesInvalidSelections(t *testing.T) {
	store := sessionStore(t)
	file := filepath.Join(t.TempDir(), "source.hl7")
	nav := &desktop.ViewNavigation{Destination: "messages", SourceKind: "file", File: file, FileIdentity: strings.Repeat("a", 64), FileMessages: []int{3, 1}}
	app := sessionApp(t, store)
	if result := app.RecordView(desktop.View{Navigation: nav}); result.State != desktop.Completed {
		t.Fatalf("ordered selection refused: %+v", result)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Completed || !slices.Equal(result.Session.View.Navigation.FileMessages, []int{3, 1}) {
		t.Fatalf("selection changed: %+v", result)
	}
	for _, invalid := range [][]int{{1, 1}, {-1}, {1<<20 + 1}, make([]int, 1025)} {
		nav.FileMessages = invalid
		if result := app.RecordView(desktop.View{Navigation: nav}); result.State != desktop.Failed {
			t.Fatalf("invalid selection accepted: %+v", result)
		}
	}
	contents := `{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","file_messages":null}}}`
	if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Failed {
		t.Fatalf("v3 selection accepted under v2: %+v", result)
	}
}

func TestWorkingSessionRetainsOrderedCaseChecksOnlyWithTheirSource(t *testing.T) {
	store := sessionStore(t)
	nav := &desktop.ViewNavigation{Destination: "messages", SourceIdentity: "sha256:" + strings.Repeat("a", 64), CheckedOccurrences: []string{"s0001-e000003", "s0001-e000001"}}
	view := desktop.View{Workspace: t.TempDir(), Case: "captured", Navigation: nav}
	app := sessionApp(t, store)
	if result := app.RecordView(view); result.State != desktop.Completed {
		t.Fatalf("checked occurrences refused: %+v", result)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Completed || !slices.Equal(result.Session.View.Navigation.CheckedOccurrences, nav.CheckedOccurrences) {
		t.Fatalf("checked order changed: %+v", result)
	}
	for _, invalid := range [][]string{{"same", "same"}, {""}, {"bad\nidentity"}, make([]string, 1025)} {
		nav.CheckedOccurrences = invalid
		if result := app.RecordView(view); result.State != desktop.Failed {
			t.Fatalf("invalid checked occurrences accepted: %+v", result)
		}
	}
	nav.CheckedOccurrences = []string{"s0001-e000001"}
	nav.SourceIdentity = ""
	if result := app.RecordView(view); result.State != desktop.Failed {
		t.Fatalf("unbound checked occurrences accepted: %+v", result)
	}
	contents := `{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":"","navigation":{"destination":"messages","checked_occurrences":null}}}`
	if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if result := sessionApp(t, store).WorkingSession(); result.State != desktop.Failed {
		t.Fatalf("v3 checks accepted under v2: %+v", result)
	}
}
