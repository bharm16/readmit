package operation_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/operation"
)

func TestWriteReportDirectoryCreatesOneNewDirectoryWhole(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "report")
	err := operation.WriteReportDirectory(destination, "diagnosis",
		operation.ReportFile{Name: "report.json", Data: []byte("{}\n")},
		operation.ReportFile{Name: "report.md", Data: []byte("# report\n")},
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"report.json": "{}\n", "report.md": "# report\n"} {
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s was not written whole: %q %v", name, data, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("the report directory is not private: %v %v", info.Mode(), err)
		}
		for _, name := range []string{"report.json", "report.md"} {
			info, err := os.Stat(filepath.Join(destination, name))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("%s is not private: %v %v", name, info.Mode(), err)
			}
		}
	}
}

func TestWriteReportDirectoryNeverOverwritesAndRetainsWhatWasThere(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "report")
	if err := operation.WriteReportDirectory(destination, "diagnosis",
		operation.ReportFile{Name: "report.json", Data: []byte("original\n")}); err != nil {
		t.Fatal(err)
	}
	refused := operation.WriteReportDirectory(destination, "diagnosis",
		operation.ReportFile{Name: "report.json", Data: []byte("replacement\n")})
	if refused == nil || !strings.Contains(refused.Error(), "destination must be new") {
		t.Fatalf("an existing destination was not refused by name: %v", refused)
	}
	data, err := os.ReadFile(filepath.Join(destination, "report.json"))
	if err != nil || string(data) != "original\n" {
		t.Fatalf("a refused write changed the retained report: %q %v", data, err)
	}
	// The diagnostics carry the subject and never a path or a value.
	if strings.Contains(refused.Error(), destination) || strings.Contains(refused.Error(), "original") {
		t.Fatalf("the refusal disclosed a path or a value: %v", refused)
	}
}

func TestWriteReportDirectoryRetainsAPartialDirectoryVisibly(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "report")
	// A member that cannot be created — its name is a path, not an entry of
	// the new directory — interrupts the report after the directory exists.
	err := operation.WriteReportDirectory(destination, "review",
		operation.ReportFile{Name: "review.json", Data: []byte("{}\n")},
		operation.ReportFile{Name: filepath.Join("..", "escape.md"), Data: []byte("outside\n")},
	)
	if err == nil || !strings.Contains(err.Error(), "incomplete report retained") {
		t.Fatalf("an interrupted report was not named as retained: %v", err)
	}
	if _, statErr := os.Stat(destination); statErr != nil {
		t.Fatal("the partial directory was removed rather than retained")
	}
	if data, readErr := os.ReadFile(filepath.Join(destination, "review.json")); readErr != nil || string(data) != "{}\n" {
		t.Fatalf("the member written before the interruption was not retained: %q %v", data, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(destination), "escape.md")); !os.IsNotExist(statErr) {
		t.Fatal("a report member escaped the new directory")
	}
}
