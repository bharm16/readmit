package desktop_test

// An assertion set moves between the window and the command line as bytes.
// The window imports one into its structured draft and exports reviewed bytes
// as a new entry of the workspace, and `readmit explain` is the command that
// reads one. Both read it with `assertion.Decode`, so the window refuses the
// sets the command line refuses, in its words, and an exported set is the
// bytes that were reviewed, which the command line re-decides against a run.
// The run is the one the installed native window retained in September
// (testdata/acceptance/native-109), copied and never rewritten.

import (
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/desktop"
)

// reviewedSet is an assertion set about the retained run, laid out as a
// person wrote it rather than as the structured draft generates one, so an
// export that rewrote it would show.
const reviewedSet = `{"schema": "readmit-assertion-set/v1",
 "name": "Retained reschedule accepted",
 "assertions": [
  {"id": "booking-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "reschedule-accepted", "operator": "field_equals",
   "subject": {"field": {"scope": "observed", "message": "s0001-e000002", "selector": "MSA-1"}},
   "when": null, "expected": {"field": {"state": "present", "text": "AA"}}},
  {"id": "booking-control-echoed", "operator": "values_equal",
   "subject": {"pair": {"left": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-10"},
                        "right": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-2"}}},
   "when": null, "expected": {"holds": true}}
 ]}
`

// occupiedEntry is the window's refusal of a name that is already an entry.
const occupiedEntry = "that name is already an entry of this workspace; an assertion set is written to a new entry"

// explainedWorkspace is a workspace holding a copy of the run the native
// window retained, which `readmit explain` re-decides a set against.
func explainedWorkspace(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	run := filepath.Join(root, "retained.run")
	copyEntry(t, filepath.Join(nativeAcceptance, "post-fix", "run"), run)
	return root, run
}

// explained is `readmit explain`'s reading of one set against the retained run.
func explained(t *testing.T, run, set string) (string, string, error) {
	t.Helper()
	return commandLine(t, "explain", run, "--assertions", set)
}

// Reviewed bytes exported from the advanced path are written exactly, under
// the identity of those bytes, and the command line reads them unchanged: the
// set it re-decides is the one the window named. A name that is already an
// entry is refused whoever wrote it, and nothing changes.
func TestAnAssertionSetExportedFromTheWindowIsReadUnchangedByTheCommandLine(t *testing.T) {
	app := workspaceApp(t)
	root, run := explainedWorkspace(t)

	validated := app.ValidateAssertionSet(reviewedSet)
	if validated.State != desktop.Completed || validated.Document != reviewedSet || validated.Output != "" {
		t.Fatalf("the reviewed set was not accepted: %+v", validated)
	}
	exported := app.ExportAssertionSet(desktop.CanonicalAssertionRequest{Workspace: root, Document: reviewedSet, Output: "reviewed-assertions.json"})
	written := filepath.Join(root, "reviewed-assertions.json")
	if exported.State != desktop.Completed || exported.Output != "reviewed-assertions.json" || exported.Identity != sha256Of([]byte(reviewedSet)) {
		t.Fatalf("the reviewed set was not exported: %+v", exported)
	}
	if read(t, written) != reviewedSet || fileDigest(t, written) != exported.Identity {
		t.Fatal("the export did not write the reviewed bytes")
	}

	stdout, stderr, err := explained(t, run, written)
	if err != nil || stderr != "" {
		t.Fatalf("readmit explain refused the exported set: %v %q", err, stderr)
	}
	for _, line := range []string{
		"Verdict: pass\n",
		"Assertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped\n",
		"Assertion set: Retained reschedule accepted\n",
		"Set contract: " + assertion.Schema + "\n",
		"Set identity: " + exported.Identity + "\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("readmit explain did not read the exported set as the window named it (%q):\n%s", line, stdout)
		}
	}

	// The window's own export, a set somebody wrote by hand and a folder are
	// all entries already, and neither an export nor a save replaces one.
	writeDocument(t, root, "handwritten.json", reviewedSet)
	imported := app.ImportAssertionSet(root, "reviewed-assertions.json")
	if imported.State != desktop.Completed || imported.Set == nil {
		t.Fatalf("the exported set did not import: %+v", imported)
	}
	before := workspaceState(t, root)
	for _, output := range []string{"reviewed-assertions.json", "handwritten.json", "retained.run"} {
		refused := app.ExportAssertionSet(desktop.CanonicalAssertionRequest{Workspace: root, Document: reviewedSet, Output: output})
		if refused.State != desktop.Failed || refused.Reason != occupiedEntry || refused.Output != "" || refused.Identity != "" {
			t.Fatalf("an export over %s was not refused: %+v", output, refused)
		}
		unsaved := app.SaveAssertionSet(desktop.AssertionSetRequest{Workspace: root, Draft: imported.Set.Draft, Output: output})
		if unsaved.State != desktop.Failed || unsaved.Reason != occupiedEntry || unsaved.Set != nil {
			t.Fatalf("a save over %s was not refused: %+v", output, unsaved)
		}
	}
	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatal("a refused export changed the workspace")
	}
}

// The window imports into its structured draft exactly the clauses the
// command line's reader reads, and a set that reader refuses is refused in
// the window by import, validation and export alike, in the reader's words,
// writing nothing.
func TestAnAssertionSetImportedInTheWindowIsTheSetTheCommandLineReads(t *testing.T) {
	app := workspaceApp(t)
	root, run := explainedWorkspace(t)
	writeDocument(t, root, "shipped-assertions.json", fixture(t, "assertion-set.json"))
	writeDocument(t, root, "reviewed-assertions.json", reviewedSet)

	for _, entry := range []string{"shipped-assertions.json", "reviewed-assertions.json"} {
		data := mustRead(t, filepath.Join(root, entry))
		set, err := assertion.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		imported := app.ImportAssertionSet(root, entry)
		if imported.State != desktop.Completed || imported.Set == nil || imported.Set.Draft.Name != set.Name {
			t.Fatalf("%s did not import: %+v", entry, imported)
		}
		if len(imported.Set.Draft.Assertions) != len(set.Assertions) {
			t.Fatalf("%s: the draft holds %d clauses, the reader read %d", entry, len(imported.Set.Draft.Assertions), len(set.Assertions))
		}
		for i, clause := range imported.Set.Draft.Assertions {
			drafted, _ := json.Marshal(clause, json.Deterministic(true))
			decoded, _ := json.Marshal(set.Assertions[i], json.Deterministic(true))
			if string(drafted) != string(decoded) {
				t.Fatalf("%s: the draft holds %s where the reader read %s", entry, drafted, decoded)
			}
		}

		// Saved again from the structured draft, the set is read by the command
		// line exactly as the original is: importing kept every clause. The
		// shipped set also asks collection questions, which the retained run
		// alone cannot answer, and both are refused for that in one sentence.
		resaved := "resaved-" + entry
		saved := app.SaveAssertionSet(desktop.AssertionSetRequest{Workspace: root, Draft: imported.Set.Draft, Output: resaved})
		if saved.State != desktop.Completed || saved.Set == nil {
			t.Fatalf("%s did not save from the draft: %+v", entry, saved)
		}
		original, originalRefusal, originalErr := explained(t, run, filepath.Join(root, entry))
		again, againRefusal, againErr := explained(t, run, filepath.Join(root, resaved))
		if entry == "reviewed-assertions.json" && (originalErr != nil ||
			!strings.Contains(original, "Set identity: "+fileDigest(t, filepath.Join(root, entry))+"\n")) {
			t.Fatalf("readmit explain did not read the reviewed set: %v %s%s", originalErr, original, originalRefusal)
		}
		if withoutSetLines(original) != withoutSetLines(again) || originalRefusal != againRefusal || (originalErr == nil) != (againErr == nil) {
			t.Fatalf("%s explains differently once imported and saved:\n%s%s\n%s%s", entry, original, originalRefusal, again, againRefusal)
		}
	}

	// The shipped refused fixture, a version this release does not read, an
	// unknown member and a document cut short.
	refusedSets := map[string]string{
		"refused-assertions.json":   fixture(t, "assertion-set-refused.json"),
		"next-assertions.json":      strings.Replace(reviewedSet, assertion.Schema, "readmit-assertion-set/v2", 1),
		"unknown-assertions.json":   strings.Replace(reviewedSet, `"name":`, `"owner": "someone", "name":`, 1),
		"truncated-assertions.json": reviewedSet[:len(reviewedSet)/2],
	}
	for entry, content := range refusedSets {
		writeDocument(t, root, entry, content)
	}
	before := workspaceState(t, root)
	for entry, content := range refusedSets {
		imported := app.ImportAssertionSet(root, entry)
		if imported.State != desktop.Failed || imported.Reason == "" || imported.Set != nil {
			t.Fatalf("%s was imported: %+v", entry, imported)
		}
		_, stderr, err := explained(t, run, filepath.Join(root, entry))
		if err == nil || stderr != "readmit: "+imported.Reason+"\n" {
			t.Fatalf("%s: the command line refused it as %q, the window as %q", entry, stderr, imported.Reason)
		}
		if validated := app.ValidateAssertionSet(content); validated.State != desktop.Failed || validated.Reason != imported.Reason {
			t.Fatalf("%s: validation answered %+v, the import %q", entry, validated, imported.Reason)
		}
		exported := app.ExportAssertionSet(desktop.CanonicalAssertionRequest{Workspace: root, Document: content, Output: "exported-" + entry})
		if exported.State != desktop.Failed || exported.Reason != imported.Reason {
			t.Fatalf("%s: the export answered %+v, the import %q", entry, exported, imported.Reason)
		}
		if _, err := os.Lstat(filepath.Join(root, "exported-"+entry)); err == nil {
			t.Fatalf("a refused export of %s wrote an entry", entry)
		}
	}
	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatal("a refused set changed the workspace")
	}
}

// withoutSetLines is an explanation without the lines that name the set's
// file and bytes, which differ between two files holding the same clauses.
func withoutSetLines(explanation string) string {
	var kept []string
	for _, line := range strings.Split(explanation, "\n") {
		if !strings.HasPrefix(line, "Set identity: ") && !strings.HasPrefix(line, "Set path: ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
