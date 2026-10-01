package artifactdir_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/profilepackage"
)

func profileImportBytes(t *testing.T) []byte {
	t.Helper()
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw, err := profilepackage.Export(read("local-profile.json"), read("profile-pack.json"), read("profile-version.json"), read("profile-origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestProfileImportSyncsItsCompletionAndBothDirectoryEntries(t *testing.T) {
	raw := profileImportBytes(t)
	parent := caseFolder(t)
	output := filepath.Join(parent, "import")
	synced := map[string]bool{}
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		synced[filepath.Clean(directory)] = true
		if directory == output {
			completed, err := os.ReadFile(filepath.Join(output, "package.json"))
			if err != nil {
				return err
			}
			_, err = profilepackage.Decode(completed)
			return err
		}
		return nil
	}))
	if err := profilepackage.Import(t.Context(), output, raw); err != nil {
		t.Fatal(err)
	}
	if !synced[output] || !synced[parent] {
		t.Fatal("import success did not establish directory durability")
	}
}

func TestProfileImportReportsDirectorySyncFailureAndNeverResumesIntoIt(t *testing.T) {
	raw := profileImportBytes(t)
	for _, failed := range []string{"import", "."} {
		t.Run(failed, func(t *testing.T) {
			parent := caseFolder(t)
			output := filepath.Join(parent, "import")
			restore := artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
				if filepath.Clean(directory) == filepath.Join(parent, failed) {
					return errors.New("fixture sync failure")
				}
				return nil
			})
			err := profilepackage.Import(t.Context(), output, raw)
			restore()
			if err == nil {
				t.Fatal("import reported success despite failed directory durability")
			}
			if err := profilepackage.Import(t.Context(), output, raw); err == nil {
				t.Fatal("import resumed into its previous output")
			}
			if err := profilepackage.Import(t.Context(), filepath.Join(parent, "retry"), raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}
