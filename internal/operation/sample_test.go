package operation_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/operation"
)

// frozenFixtures copies the two shipped receiver fixtures into a new folder,
// byte for byte, as a person copies them out of a release archive.
func frozenFixtures(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	for _, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
		data, err := os.ReadFile(filepath.Join("../../testdata/fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// files reads every file of a written case bundle, by its path inside it.
func files(t *testing.T, root string) map[string]string {
	t.Helper()
	read := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		relative, _ := filepath.Rel(root, path)
		read[filepath.ToSlash(relative)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return read
}

// The sample capture is a pure function of the frozen fixture bytes, the
// folder they were read from and the instant it records: two captures of the
// same folder at the same instant are the same case, byte for byte and
// identity for identity, which is what lets the window and the command line
// be compared at all.
func TestCaptureSampleIsTheFrozenFixturesAsOneImportedCase(t *testing.T) {
	fixtures := frozenFixtures(t)
	at := time.Date(2026, 9, 23, 10, 11, 12, 13, time.UTC)
	destination := t.TempDir()
	first, err := operation.CaptureSample(fixtures, filepath.Join(destination, "first"), at)
	if err != nil {
		t.Fatal(err)
	}
	if first.Manifest.Schema != bundle.Schema || first.Manifest.Provenance.Mode != bundle.Imported ||
		first.Manifest.Provenance.ImportedAt == nil || !first.Manifest.Provenance.ImportedAt.Equal(at) {
		t.Fatalf("the sample was not recorded as imported at the given instant: %+v", first.Manifest)
	}
	if len(first.Manifest.Sources) != 2 || len(first.Events) != 2 || first.Counts()[bundle.Message] != 2 {
		t.Fatalf("the sample is not the booking and its reschedule: %+v", first.Manifest.Sources)
	}
	second, err := operation.CaptureSample(fixtures, filepath.Join(destination, "second"), at)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity != second.Identity {
		t.Fatalf("the same fixtures at the same instant made two cases: %s and %s", first.Identity, second.Identity)
	}
	if a, b := files(t, filepath.Join(destination, "first")), files(t, filepath.Join(destination, "second")); len(a) == 0 || len(a) != len(b) {
		t.Fatalf("the two captures hold different files: %v and %v", a, b)
	} else {
		for name, data := range a {
			if b[name] != data {
				t.Errorf("%s differs between two captures of the same fixtures at the same instant", name)
			}
		}
	}
}

// Anything but the pinned bytes is refused before a case exists, in the
// words the command line has always used, and a destination that already
// exists is never written into.
func TestCaptureSampleRefusesAnythingButTheFrozenFixtures(t *testing.T) {
	at := time.Now().UTC()
	changed := frozenFixtures(t)
	if err := os.WriteFile(filepath.Join(changed, "listen-s13.hl7"), []byte("MSH|^~\\&|CHANGED\r"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := frozenFixtures(t)
	if err := os.Remove(filepath.Join(missing, "listen-s12.hl7")); err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct{ fixtures, want string }{
		"changed bytes":   {changed, "sample requires the unchanged frozen synthetic fixtures"},
		"missing fixture": {missing, "frozen sample fixture unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "capture")
			if _, err := operation.CaptureSample(test.fixtures, output, at); err == nil || err.Error() != test.want {
				t.Fatalf("refusal: %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("a refused sample capture left a case behind")
			}
		})
	}
	existing := t.TempDir()
	if _, err := operation.CaptureSample(frozenFixtures(t), existing, at); err == nil {
		t.Fatal("the sample capture wrote into a folder that already existed")
	}
	if entries, err := os.ReadDir(existing); err != nil || len(entries) != 0 {
		t.Fatalf("a refused destination was written into: %v %v", entries, err)
	}
}
