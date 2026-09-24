package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The command line runs the import and source operations the window runs, so
// both refuse a taken destination the same way, before any evidence is read:
// the case directory as well as the receipt. Every declared input below is
// missing, and it is still the destination that is refused.
func TestImportRefusesATakenCaseBeforeReadingAnyEvidence(t *testing.T) {
	dir := t.TempDir()
	taken := filepath.Join(dir, "taken.case")
	if err := os.Mkdir(taken, 0700); err != nil {
		t.Fatal(err)
	}
	plan := writeDocument(t, dir, "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".hl7"]}`)
	adapter := writeDocument(t, dir, "adapter.json",
		`{"schema":"readmit-engine-export/v1","engine":"mirth","version":"4.5.2","format":"raw","terminator":"cr"}`)
	missing := filepath.Join(dir, "missing.hl7")
	receipt := filepath.Join(dir, "receipt.json")
	for name, args := range map[string][]string{
		"import": {"import", "--plan", plan, "--file", missing, "--output", taken, "--receipt", receipt},
		"engine": {"import", "engine", "--plan", adapter, "--file", missing, "--output", taken},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, args...)
			if code := exitCode(t, err); code != 1 || stdout != "" || stderr != "readmit: the case bundle destination must be a new directory\n" {
				t.Fatalf("a taken case exited %d with %q and %q", code, stdout, stderr)
			}
			if _, err := os.Lstat(receipt); !os.IsNotExist(err) {
				t.Fatal("a refused import wrote a receipt")
			}
			if entries, err := os.ReadDir(taken); err != nil || len(entries) != 0 {
				t.Fatalf("a refused import wrote into the taken case: %v %v", entries, err)
			}
		})
	}
}

// A receipt, a collection receipt and an access diagnosis may be named in
// folders that do not exist yet; the command creates them when it writes the
// document, and creates none when it is refused.
func TestDocumentsCreateTheFoldersTheyAreNamedIn(t *testing.T) {
	directory, declaration := sourceDeclarations(t, map[string]string{"a.hl7": sourceMessage})
	report := filepath.Join(directory, "reports", "access.json")
	if stdout, stderr, err := run(t, append([]string{"source", "diagnose", declaration, "--report", report}, sourcePlanFlags()...)...); err != nil || stderr != "" || !strings.Contains(stdout, "Status: complete\n") {
		t.Fatalf("a diagnosis into a new folder: %v %q %q", err, stdout, stderr)
	}
	collection := filepath.Join(directory, "receipts", "2026", "collection.json")
	staged := filepath.Join(directory, "staged")
	if _, stderr, err := run(t, append([]string{"source", "collect", declaration, "--output", staged, "--receipt", collection}, sourcePlanFlags()...)...); err != nil || stderr != "" {
		t.Fatalf("a collection into a new folder: %v %q", err, stderr)
	}
	imported := filepath.Join(directory, "receipts", "2026", "imports", "incident-receipt.json")
	if stdout, stderr, err := run(t, append([]string{"import", "--folder", staged, "--output", filepath.Join(directory, "incident.case"), "--receipt", imported}, sourcePlanFlags()...)...); err != nil || stderr != "" || !strings.Contains(stdout, "Receipt: readmit-import-receipt/v1\n") {
		t.Fatalf("an import receipt into a new folder: %v %q %q", err, stdout, stderr)
	}
	for _, document := range []string{report, collection, imported} {
		if data, err := os.ReadFile(document); err != nil || !strings.HasPrefix(string(data), `{"schema":"readmit-`) {
			t.Fatalf("%s was not written: %v", filepath.Base(document), err)
		}
		if info, err := os.Stat(filepath.Dir(document)); err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("the folder of %s: %v %v", filepath.Base(document), info, err)
		}
	}

	// Nothing to import: refused, and the receipt's folder is never created.
	empty := t.TempDir()
	refused := filepath.Join(directory, "refused", "receipt.json")
	_, stderr, err := run(t, append([]string{"import", "--folder", empty, "--output", filepath.Join(directory, "empty.case"), "--receipt", refused}, sourcePlanFlags()...)...)
	if err == nil || stderr != "readmit: the declared containers hold no member to import; --preview reports why each entry was excluded\n" {
		t.Fatalf("an import of nothing: %v %q", err, stderr)
	}
	if _, err := os.Lstat(filepath.Dir(refused)); !os.IsNotExist(err) {
		t.Fatal("a refused import created its receipt's folder")
	}
}
