package report_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/report"
)

// The files a v3 portable review holds, generated in memory, are exactly the
// files ExportDocumentReview writes.
func TestDocumentReviewFilesAreTheExportedReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if _, err := report.Create(ctx, report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.AssembleRuns(ctx, report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "baseline")}, packet); err != nil {
		t.Fatal(err)
	}
	authored := report.Authored{Title: "Reschedule regression", Notes: "Seen in QA."}
	files, err := report.DocumentReviewFiles(ctx, packet, authored)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "review")
	if _, err := report.ExportDocumentReview(ctx, packet, output, authored); err != nil {
		t.Fatal(err)
	}
	written := map[string][]byte{}
	err = filepath.WalkDir(output, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		relative, _ := filepath.Rel(output, path)
		written[filepath.ToSlash(relative)] = data
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(files) {
		t.Fatalf("%d files written, %d generated", len(written), len(files))
	}
	for name, data := range written {
		if !bytes.Equal(files[name], data) {
			t.Fatalf("%s differs from the exported review", name)
		}
	}
}
