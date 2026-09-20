package artifactpath_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactpath"
)

func TestEntryNameAcceptsOneLocalNameOnly(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"case", true},
		{"..hidden", true},
		{"", false},
		{".", false},
		{"..", false},
		{"a/b", false},
		{"/absolute", false},
		{"../escape", false},
	}
	for _, c := range cases {
		err := artifactpath.EntryName(c.name)
		if c.valid && err != nil {
			t.Errorf("EntryName(%q) = %v, want accepted", c.name, err)
		}
		if !c.valid && err == nil {
			t.Errorf("EntryName(%q) accepted", c.name)
		}
	}
}

func TestChildRefusesNamesAndEntriesThatLeaveTheDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "inside"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file.bin"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "inside"), filepath.Join(directory, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Child(directory, "inside"); err != nil {
		t.Fatalf("a real child directory was refused: %v", err)
	}
	for _, name := range []string{"../outside", "file.bin", "linked", ""} {
		if _, err := artifactpath.Child(directory, name); err == nil {
			t.Errorf("Child(%q) accepted", name)
		}
	}
}

func TestDestinationRefusesNamesInsideRetainedEvidence(t *testing.T) {
	base := t.TempDir()
	retained := filepath.Join(base, "retained")
	if err := os.Mkdir(retained, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retained, "identity.sha256"), []byte("completed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(retained, "child")); err == nil {
		t.Fatal("a write inside completed evidence was allowed")
	}
	// A missing completion marker does not make retained evidence writable.
	incomplete := filepath.Join(base, "incomplete")
	if err := os.Mkdir(incomplete, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(incomplete, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"schema":"readmit-case/v3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(incomplete, "child")); err == nil {
		t.Fatal("a write inside incomplete case evidence was allowed")
	}
	// The family rule covers every retained family, not only cases.
	if err := os.WriteFile(manifest, []byte(`{"schema":"readmit-result/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(incomplete, "child")); err == nil {
		t.Fatal("a write inside incomplete result evidence was allowed")
	}
	// A manifest that names no evidence family is not protection by itself.
	if err := os.WriteFile(manifest, []byte(`{"schema":"readmit-synth/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(incomplete, "child")); err != nil {
		t.Fatalf("a write beside a non-evidence manifest was refused: %v", err)
	}
}

func TestDestinationRefusesWritesInsideAProtectedSource(t *testing.T) {
	base := t.TempDir()
	protected := filepath.Join(base, "opened")
	if err := os.Mkdir(protected, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(protected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(protected, "child"), info); err == nil {
		t.Fatal("a write inside the opened case was allowed")
	}
	outside, err := artifactpath.Destination(filepath.Join(base, "new"), info)
	if err != nil {
		t.Fatalf("a write outside the opened case was refused: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if outside != filepath.Join(resolved, "new") {
		t.Fatalf("destination = %q", outside)
	}
}

func TestDestinationRejectsNamesThatAreNotOneNewEntry(t *testing.T) {
	for _, name := range []string{"", ".", "child/..", "child/../grandchild"} {
		if _, err := artifactpath.Destination(name); err == nil {
			t.Errorf("Destination(%q) accepted", name)
		}
	}
	// A trailing separator names the same entry, not a new one beneath it.
	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := artifactpath.Destination(child + "/")
	if err != nil {
		t.Fatalf("a trailing separator was refused: %v", err)
	}
	parent, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(parent, "child") {
		t.Fatalf("destination = %q", resolved)
	}
}

func TestEvidenceFamilyNamesOneFamilyOrNone(t *testing.T) {
	cases := []struct {
		schema string
		family string
	}{
		{"readmit-case/v1", artifactpath.FamilyCase},
		{"readmit-case/v5", artifactpath.FamilyCase},
		{"readmit-run/v2", artifactpath.FamilyRun},
		{"readmit-result/v1", artifactpath.FamilyResult},
		{"readmit-synth/v1", ""},
		{"readmit-case", ""},
		{"readmit-casebook/v1", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := artifactpath.EvidenceFamily(c.schema); got != c.family {
			t.Errorf("EvidenceFamily(%q) = %q, want %q", c.schema, got, c.family)
		}
		if want := c.family != ""; artifactpath.IsEvidenceSchema(c.schema) != want {
			t.Errorf("IsEvidenceSchema(%q) = %v, want %v", c.schema, !want, want)
		}
	}
}
