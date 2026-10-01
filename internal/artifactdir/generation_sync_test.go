package artifactdir_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/casegen"
)

func TestGenerationAnchorsItsNewDirectoryAndReportsParentSyncFailure(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile("../../testdata/casegen/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	generation, err := casegen.Generate(t.Context(), read("request-siu.json"), read("owned/profile-siu.json"), read("owned/pack.json"), casegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable", true: "refused"}[fail], func(t *testing.T) {
			parent := caseFolder(t)
			output := filepath.Join(parent, "generation")
			anchored := false
			restore := artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
				if filepath.Clean(directory) != parent {
					return nil
				}
				if _, err := os.ReadFile(filepath.Join(output, "generation.json")); err != nil {
					return err
				}
				anchored = true
				if fail {
					return errors.New("fixture parent sync failure")
				}
				return nil
			})
			_, err := generation.WriteDirectory(t.Context(), output)
			restore()
			if !anchored || (err != nil) != fail {
				t.Fatal("generation success requires a final parent sync", anchored, err)
			}
			if _, err := generation.WriteDirectory(t.Context(), output); err == nil {
				t.Fatal("generation resumed into retained output")
			}
			if _, err := generation.WriteDirectory(t.Context(), filepath.Join(parent, "fresh")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
