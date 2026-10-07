package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// The theme, text size and local reviewer name a person saves are kept in the
// shell's own document, so the next window opens with exactly them. Nothing
// is kept until a person saves, and saving does not wait for the slot.
func TestPreferencesPersistThemeTextScaleAndReviewerAcrossWindows(t *testing.T) {
	state := t.TempDir()
	first := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	if fresh := first.ReadPreferences(); fresh.State != desktop.Completed ||
		fresh.Preferences != (desktop.Preferences{Theme: desktop.SystemTheme, TextScale: 100}) {
		t.Fatalf("a window with nothing saved: %+v", fresh)
	}
	if _, err := os.Lstat(filepath.Join(state, "preferences.json")); err == nil {
		t.Fatal("reading the preferences wrote them")
	}
	// A save works while an operation holds the slot.
	release, held := desktop.HoldSlotForTest(first, "")
	if !held {
		t.Fatal("the slot was not free")
	}
	chosen := desktop.Preferences{Theme: desktop.DarkTheme, TextScale: 175, Reviewer: "Dana Reviewer"}
	saved := first.SavePreferences(chosen)
	release()
	if saved.State != desktop.Completed || saved.Preferences != chosen {
		t.Fatalf("save: %+v", saved)
	}
	info, err := os.Stat(filepath.Join(state, "preferences.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the preferences document: %v %v", info, err)
	}
	next := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	if reopened := next.ReadPreferences(); reopened.State != desktop.Completed || reopened.Preferences != chosen {
		t.Fatalf("the next window: %+v", reopened)
	}
	// A size an earlier release offered between 100% and 200% stays readable
	// and selectable; clearing the reviewer keeps no name.
	legacy := desktop.Preferences{Theme: desktop.LightTheme, TextScale: 110}
	if kept := next.SavePreferences(legacy); kept.State != desktop.Completed || next.ReadPreferences().Preferences != legacy {
		t.Fatalf("a legacy text size: %+v", kept)
	}
	if !fileHolds(t, filepath.Join(state, "preferences.json"), `"schema":"readmit-desktop-preferences/v1"`) {
		t.Fatal("the document does not declare its contract")
	}
}

// A theme the window does not offer, a size outside 50–200% and a reviewer
// name that is not printable are refused, and what was kept is unchanged. A
// document this release cannot read is reported and left as written.
func TestPreferencesRefuseAnUnprintableReviewerAndAnUnofferedTheme(t *testing.T) {
	state := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	kept := desktop.Preferences{Theme: desktop.LightTheme, TextScale: 125, Reviewer: "Dana"}
	if saved := app.SavePreferences(kept); saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	for label, refused := range map[string]desktop.Preferences{
		"an unoffered theme":    {Theme: "sepia", TextScale: 100},
		"a size below 50%":      {Theme: desktop.SystemTheme, TextScale: 49},
		"a size above 200%":     {Theme: desktop.SystemTheme, TextScale: 250},
		"a control character":   {Theme: desktop.SystemTheme, TextScale: 100, Reviewer: "Dana\x1b[2J"},
		"a name past 200 bytes": {Theme: desktop.SystemTheme, TextScale: 100, Reviewer: string(make([]byte, 201))},
		"a line break":          {Theme: desktop.SystemTheme, TextScale: 100, Reviewer: "line\nbreak"},
	} {
		result := app.SavePreferences(refused)
		if result.State != desktop.Failed || result.Reason == "" || result.Preferences != kept {
			t.Errorf("%s: %+v", label, result)
		}
		if read := app.ReadPreferences(); read.Preferences != kept {
			t.Errorf("%s changed what was kept: %+v", label, read)
		}
	}
	path := filepath.Join(state, "preferences.json")
	if err := os.WriteFile(path, []byte(`{"schema":"readmit-desktop-preferences/v9"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if read := app.ReadPreferences(); read.State != desktop.Failed || read.Preferences.Theme != desktop.SystemTheme {
		t.Fatalf("an unreadable document: %+v", read)
	}
	if saved := app.SavePreferences(kept); saved.State != desktop.Failed {
		t.Fatalf("an unreadable document was replaced: %+v", saved)
	}
	if !fileHolds(t, path, "v9") {
		t.Fatal("an unreadable document was not left as written")
	}
}

func fileHolds(t *testing.T, path, text string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(data), text)
}

func TestPreferencesKeepZoomBelowDefaultAcrossWindows(t *testing.T) {
	state := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	for _, scale := range []int{90, 75, 50} {
		expected := desktop.Preferences{Theme: desktop.SystemTheme, TextScale: scale}
		if saved := app.SavePreferences(expected); saved.State != desktop.Completed {
			t.Fatalf("save %d%%: %+v", scale, saved)
		}
		reopened := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
		if got := reopened.ReadPreferences(); got.State != desktop.Completed || got.Preferences != expected {
			t.Fatalf("reopen %d%%: %+v", scale, got)
		}
	}
}
