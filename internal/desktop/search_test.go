package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
)

// searchable is the sample workspace with the frozen regression case also
// registered in a project document, so one folder holds both of the things
// search navigates: entries the folder declares and cases a project records.
func searchable(t *testing.T, app *desktop.App) string {
	t.Helper()
	root := sample(t, app).Workspace.Root
	return writeProject(t, root, registeredRegression)
}

func matched(result desktop.SearchResult, kind desktop.MatchKind, name string) (desktop.Match, bool) {
	for _, match := range result.Matches {
		if match.Kind == kind && match.Name == name {
			return match, true
		}
	}
	return desktop.Match{}, false
}

func TestSearchNavigatesDeclaredEntriesAndRegisteredCases(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := searchable(t, app)

	// An entry the folder declares, found by the name the listing shows.
	entry := app.Search(root, "cancellation")
	if entry.State != desktop.Completed {
		t.Fatalf("a declared entry was not found: %+v", entry)
	}
	match, found := matched(entry, desktop.ArtifactMatch, "cancellation")
	if !found || match.Field != "name" || match.Region != "navigation" {
		t.Fatalf("the entry match does not navigate to the listing: %+v", entry.Matches)
	}

	// Project metadata a person maintains, found by the words they gave it.
	for query, field := range map[string]string{
		"duplicate appointment": "title",
		"scheduling-team":       "owner",
		"INC-4821":              "incident",
		"investigating":         "status",
		"siu-2.5.1-v1":          "interface version",
	} {
		result := app.Search(root, query)
		if result.State != desktop.Completed {
			t.Fatalf("query %q found nothing: %+v", query, result)
		}
		match, found := matched(result, desktop.RegisteredMatch, "regression")
		if !found {
			t.Fatalf("query %q did not find the registered case: %+v", query, result.Matches)
		}
		if match.Field != field || match.Region != "evidence" {
			t.Fatalf("query %q matched %q and navigates to %q: want %q in the evidence pane", query, match.Field, match.Region, field)
		}
	}

	// Case differences in the query never hide a match.
	if lowered := app.Search(root, "inc-4821"); lowered.State != desktop.Completed {
		t.Fatalf("search is case sensitive: %+v", lowered)
	}

	// One result per thing found, so every result is somewhere to go. The
	// regression bundle is both a declared entry and a registered case, and it
	// is named once as each.
	both := app.Search(root, "regression")
	names := make([]string, 0, len(both.Matches))
	for _, match := range both.Matches {
		names = append(names, string(match.Kind)+":"+match.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"artifact:regression", "artifact:" + guide.IndexName, "registered_case:regression"}) {
		t.Fatalf("the regression bundle was not named once as an entry and once as a registered case: %v", names)
	}
}

// Nothing to search for and nothing that matches are both empty, and a person
// has to be able to tell which one happened.
func TestSearchSeparatesNoQueryFromNoMatch(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := searchable(t, app)

	blank := app.Search(root, "   ")
	if blank.State != desktop.Empty || blank.Reason == "" || len(blank.Matches) != 0 {
		t.Fatalf("a blank query was not reported as nothing to search for: %+v", blank)
	}
	absent := app.Search(root, "a-name-this-workspace-does-not-hold")
	if absent.State != desktop.Empty || absent.Reason == "" || len(absent.Matches) != 0 {
		t.Fatalf("an unmatched query was not reported as empty: %+v", absent)
	}
	if blank.Reason == absent.Reason {
		t.Fatalf("nothing to search for and nothing found read the same: %q", blank.Reason)
	}
}

// Search reads the same declarations the listing reads and stops there. It
// verifies no evidence, so an entry it finds is a claim until OpenCase accepts
// it, and it opens no workspace, so it never joins the recent folders.
func TestSearchDeclaresVerifiesNothingAndOpensNothing(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := searchable(t, app)

	// The practice endpoint is a configuration, not evidence. Search still names
	// it, because hiding an entry would imply the folder holds only what matched.
	practice := app.Search(root, "practice")
	match, found := matched(practice, desktop.ArtifactMatch, guide.TargetName)
	if !found || match.Label == "" {
		t.Fatalf("an unsupported entry was hidden from search: %+v", practice)
	}

	// The deliberately invalid bundle declares itself as a case and is found as
	// one: search reports what an entry declares and concludes nothing.
	invalid := app.Search(root, "invalid")
	if _, found := matched(invalid, desktop.ArtifactMatch, "invalid"); !found {
		t.Fatalf("a declared case was not found: %+v", invalid)
	}

	elsewhere := newApp(t, &chooser{})
	if result := elsewhere.Search(root, "regression"); result.State != desktop.Completed {
		t.Fatalf("search needs the folder to have been opened first: %+v", result)
	}
	if recent := elsewhere.RecentWorkspaces(); recent.State != desktop.Empty || len(recent.Roots) != 0 {
		t.Fatalf("searching a folder recorded it as opened: %+v", recent)
	}
}

func TestSearchHoldsTheSameOperationSlot(t *testing.T) {
	parent := t.TempDir()
	root := searchable(t, newApp(t, &chooser{folder: parent}))

	reentrant := &chooser{folder: root}
	app := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	var concurrent desktop.SearchResult
	reentrant.before = func() { concurrent = app.Search(root, "regression") }
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrent.State != desktop.Busy || len(concurrent.Matches) != 0 {
		t.Fatalf("a search ran while another operation held the facade: %+v", concurrent)
	}
	if recovered := app.Search(root, "regression"); recovered.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

func TestSearchRefusesAFolderThatIsNotAWorkspace(t *testing.T) {
	app := newApp(t, &chooser{})
	missing := app.Search(filepath.Join(t.TempDir(), "absent"), "regression")
	if missing.State != desktop.Failed || missing.Reason == "" || missing.Matches == nil {
		t.Fatalf("a missing folder was not refused: %+v", missing)
	}
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("not a workspace\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := app.Search(file, "regression"); result.State != desktop.Failed {
		t.Fatalf("a file was searched as a workspace: %+v", result)
	}
}

// A match names why it matched, never the value that matched. Nothing search
// returns can carry message bytes, field values, or a source path.
func TestSearchCarriesNoMessageContent(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := searchable(t, app)
	encoded, err := json.Marshal(app.Search(root, "regression"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MSH", "SCH", "PID", "SYNTH-", "READMIT"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("a search result exposed message content %q: %s", leaked, encoded)
		}
	}
}

func TestSearchIndexedContentHitsRouteToInspector(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	// In gridWorkspace, "incident" case has "incident.index.json" retaining values for PID[1]-3[1] and MSA[1]-1[1].
	result := app.Search(root, "MRN-1")
	if result.State != desktop.Completed {
		t.Fatalf("search for indexed content failed: %+v", result)
	}
	var contentHit *desktop.Match
	for _, match := range result.Matches {
		if match.Kind == desktop.ContentMatch && match.Name == "incident" {
			contentHit = &match
			break
		}
	}
	if contentHit == nil {
		t.Fatalf("expected content match in incident, got matches: %+v", result.Matches)
	}
	if contentHit.Region != "inspector" {
		t.Errorf("expected Region inspector, got %q", contentHit.Region)
	}
	if contentHit.Occurrence == "" || contentHit.Selector != "PID[1]-3[1]" {
		t.Errorf("expected occurrence and PID[1]-3[1] selector, got %+v", contentHit)
	}
}

// Search takes a person's typing. Any bytes at all must produce one of the
// declared states and name only entries the folder actually holds.
func FuzzSearchQuery(f *testing.F) {
	app := desktop.New(&chooser{folder: f.TempDir()}, filepath.Join(f.TempDir(), "recent.json"), filepath.Join(f.TempDir(), "filters.json"), filepath.Join(f.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(f.TempDir(), "session.json")), "drafts.json"))
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed || created.Workspace == nil {
		f.Fatalf("sample workspace: %+v", created)
	}
	root := created.Workspace.Root
	declared := map[string]bool{"regression": true, "cancellation": true, "invalid": true, "project.json": true, guide.TargetName: true, guide.IndexName: true}
	document := `{"schema":"readmit-project/v1","settings":{"title":"Epic scheduling interface",` +
		`"default_owner":"integration-team","default_interface_version":"siu-2.5.1-v1"},` +
		`"interface_versions":["siu-2.5.1-v1"],"cases":[` + registeredRegression + "]}\n"
	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(document), 0600); err != nil {
		f.Fatal(err)
	}

	for _, seed := range []string{"", " ", "regression", "INC-4821", "*", "\x00", "ÿ", "regression�"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		result := app.Search(root, query)
		switch result.State {
		case desktop.Empty, desktop.Completed:
		default:
			t.Fatalf("query produced state %q against a readable workspace", result.State)
		}
		if result.Matches == nil {
			t.Fatal("a search result carried no match list")
		}
		if (result.State == desktop.Empty) != (len(result.Matches) == 0) {
			t.Fatalf("state %q disagrees with %d matches", result.State, len(result.Matches))
		}
		for _, match := range result.Matches {
			if !declared[match.Name] {
				t.Fatalf("search named %q, which this workspace does not hold", match.Name)
			}
			if match.Field == "" || match.Region == "" || match.Label == "" {
				t.Fatalf("a match is not navigable: %+v", match)
			}
		}
	})
}
