package tests

// The window's import screen hands archives to the same extraction the
// command line runs, so a hostile archive is refused there for the same
// reasons: an entry that walks out of the archive, names an absolute path,
// hides a separator or repeats a name never becomes evidence, the refusal
// happens before a case or receipt exists, and nothing appears beside the
// archive or in the workspace. The refusal names what was wrong, never the
// planted entry name, because a file name can itself be patient data.

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/importer"
)

func TestTheWindowsImportRefusesUnsafeArchivesAndWritesNothing(t *testing.T) {
	const planted = "PLANTED-ENTRY-NAME-111"
	plan := decodeImportPlan(t, writeDocument(t, t.TempDir(), "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[".hl7"]}`))
	workspace := t.TempDir()
	window := desktopApp(t, workspace)
	before := treeOf(t, workspace)
	for name, entries := range map[string][]namedEntry{
		"parent traversal": {{name: "../" + planted + ".hl7", content: importFixture("ONE")}},
		"nested traversal": {{name: "inside/../../" + planted + ".hl7", content: importFixture("ONE")}},
		"absolute name":    {{name: "/tmp/" + planted + ".hl7", content: importFixture("ONE")}},
		"backslash name":   {{name: "inside\\" + planted + ".hl7", content: importFixture("ONE")}},
		"duplicate names":  {{name: planted + ".hl7", content: importFixture("ONE")}, {name: planted + ".hl7", content: importFixture("TWO")}},
	} {
		t.Run(name, func(t *testing.T) {
			archives := t.TempDir()
			archive := filepath.Join(archives, "hostile.zip")
			writeNamedArchive(t, archive, entries)
			beside := treeOf(t, archives)
			preview := window.PreviewImport(desktop.ImportRequest{Workspace: workspace, Mode: "plan", Archives: []string{archive}, Plan: &plan})
			if preview.State != desktop.Failed || strings.Contains(preview.Reason, planted) {
				t.Fatalf("preview of a hostile archive: %+v", preview)
			}
			commit := window.CommitImport(desktop.ImportCommitRequest{Workspace: workspace, Mode: "plan", OutputName: "imported", ReceiptName: "imported-receipt.json",
				Archives: []string{archive}, Plan: &plan})
			if commit.State != desktop.Failed || strings.Contains(commit.Reason, planted) {
				t.Fatalf("commit of a hostile archive: %+v", commit)
			}
			if after := treeOf(t, workspace); !reflect.DeepEqual(before, after) {
				t.Fatalf("a refused import changed the workspace: %v", after)
			}
			if _, err := os.Lstat(filepath.Join(workspace, "imported")); !os.IsNotExist(err) {
				t.Fatal("a refused import left a case directory behind")
			}
			if after := treeOf(t, archives); !reflect.DeepEqual(beside, after) {
				t.Fatalf("a refused import wrote beside the archive: %v", after)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(archives), planted+".hl7")); !os.IsNotExist(err) {
				t.Fatal("an entry escaped the archive")
			}
		})
	}
}

func decodeImportPlan(t *testing.T, path string) importer.Plan {
	t.Helper()
	plan, err := importer.DecodePlan(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

type namedEntry struct {
	name    string
	content string
}

func writeNamedArchive(t *testing.T, path string, entries []namedEntry) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		member, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
