package operation_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// stagedCollection collects a directory source holding the named entries
// under plan into dir/staged, with its receipt at dir/collection.json.
func stagedCollection(t *testing.T, dir string, plan importer.Plan, entries map[string]string) (evidencesource.Collection, error) {
	t.Helper()
	export := filepath.Join(dir, "export")
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(export, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: export,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
	options, err := operation.SourceOptions(plan, "")
	if err != nil {
		t.Fatal(err)
	}
	return operation.SourceCollect(context.Background(), source, filepath.Join(dir, "staged"), filepath.Join(dir, "collection.json"), options)
}

// A staged collection is imported under the plan its receipt records — here
// LF terminators and a .txt member no default would name — and the import
// receipt records that plan and the very entries the collection staged.
func TestImportCollectionCommitImportsTheStagedFolderUnderTheRecordedPlan(t *testing.T) {
	dir := t.TempDir()
	plan := importer.Plan{Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.LF,
		Encoding: importer.UTF8, Direction: bundle.Outbound, Members: []string{".txt"}}
	message := "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\nPID|||12345||DOE^JOHN|||M\n"
	collected, err := stagedCollection(t, dir, plan, map[string]string{"one.txt": message, "notes.md": "not evidence"})
	if err != nil {
		t.Fatal(err)
	}
	b, receipt, err := operation.ImportCollectionCommit(context.Background(), filepath.Join(dir, "collection.json"),
		filepath.Join(dir, "staged"), filepath.Join(dir, "case"), filepath.Join(dir, "import.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receipt.Plan, collected.Plan) {
		t.Fatalf("imported under %+v, collected under %+v", receipt.Plan, collected.Plan)
	}
	if len(b.Events) != 1 || len(receipt.Quarantined) != 0 || b.Counts()[bundle.Message] != 1 || b.Events[0].Direction != bundle.Outbound {
		t.Fatalf("the staged message was not imported as collected: %d occurrences, %+v quarantined", len(b.Events), receipt.Quarantined)
	}
	member := receipt.Containers[0].Members[0]
	if entry := collected.Entries[1]; member.Name != entry.Name || member.SHA256 != entry.SHA256 {
		t.Fatalf("imported %+v, collected %+v", member, entry)
	}
}

// Nothing a receipt does not name is imported, and a refusal writes nothing:
// no case, no import receipt.
func TestImportCollectionCommitRefusesWhatItsReceiptDoesNotName(t *testing.T) {
	message := "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||12345||DOE^JOHN|||M\r"
	refuses := func(t *testing.T, dir string, want error) {
		t.Helper()
		output, receipt := filepath.Join(dir, "case"), filepath.Join(dir, "import.json")
		_, _, err := operation.ImportCollectionCommit(context.Background(), filepath.Join(dir, "collection.json"),
			filepath.Join(dir, "staged"), output, receipt)
		if !errors.Is(err, want) {
			t.Fatalf("ImportCollectionCommit = %v, want %v", err, want)
		}
		for _, written := range []string{output, receipt} {
			if _, err := os.Lstat(written); !os.IsNotExist(err) {
				t.Fatalf("a refused import wrote %s", filepath.Base(written))
			}
		}
	}

	t.Run("a collection that did not complete", func(t *testing.T) {
		dir := t.TempDir()
		collected, err := stagedCollection(t, dir, validPlan(), map[string]string{"one.hl7": message, "two.hl7": "\x0b" + message + "\x1c\r"})
		if !errors.Is(err, evidencesource.ErrIncomplete) || collected.Totals.Collected != 1 {
			t.Fatalf("collect: %+v %v", collected.Totals, err)
		}
		refuses(t, dir, evidencesource.ErrNotImportable)
	})

	t.Run("a staged folder holding a member the collection did not collect", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := stagedCollection(t, dir, validPlan(), map[string]string{"one.hl7": message}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "staged", "two.hl7"), []byte(message), 0600); err != nil {
			t.Fatal(err)
		}
		refuses(t, dir, evidencesource.ErrUnrecordedMember)
	})

	t.Run("a receipt edited after the collection wrote it", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := stagedCollection(t, dir, validPlan(), map[string]string{"one.hl7": message}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "collection.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		edited := bytes.Replace(data, []byte(`"sha256":"`), []byte(`"sha256":"0`), 1)
		if err := os.WriteFile(path, edited, 0600); err != nil {
			t.Fatal(err)
		}
		refuses(t, dir, evidencesource.ErrIdentityMismatch)
	})
}
