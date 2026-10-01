package report_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestReturnedDocumentCannotChangeLaterRenderingOrExport(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "fixture")
	if _, err := report.Create(t.Context(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packet, err := report.AssembleRuns(t.Context(), report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "post-fix"), Comparison: filepath.Join(source, "baseline")}, filepath.Join(t.TempDir(), "packet"))
	if err != nil {
		t.Fatal(err)
	}
	authored := report.Authored{Title: "Owned values"}
	document, err := packet.Document(t.Context(), authored)
	if err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for i := range document.Checks {
		check := &document.Checks[i]
		if check.Expected.Count != nil {
			*check.Expected.Count = 987
			changed++
		}
		if check.Observed != nil && check.Observed.Count != nil {
			*check.Observed.Count = 654
			changed++
		}
	}
	for i := range document.Comparison.Checks {
		check := &document.Comparison.Checks[i]
		for _, observed := range []*testrunner.Value{check.BeforeObserved, check.AfterObserved} {
			if observed != nil && observed.Count != nil {
				*observed.Count = 321
				changed++
			}
		}
	}
	if changed < 4 {
		t.Fatal("fixture does not exercise expected, observed and comparison pointer values", changed)
	}
	again, err := packet.Document(t.Context(), authored)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(again)
	if err != nil || !bytes.Equal(original, actual) {
		t.Fatal("mutating a returned document changed later rendering under the same packet identity", err)
	}
	review, err := packet.ExportDocumentReview(t.Context(), filepath.Join(t.TempDir(), "portable"), authored)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := json.Marshal(review.Document)
	if err != nil || !bytes.Equal(original, exported) {
		t.Fatal("mutating a returned document changed the exported reading", err)
	}
}

func TestStructuredReportUsesItsVerifiedSnapshotAndRefusesAnotherFolder(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "fixture")
	if _, err := report.Create(t.Context(), report.Scenario, source); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(t.TempDir(), "passed")
	packet, err := report.AssembleRuns(t.Context(), report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "post-fix")}, packetPath)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "failed")
	if _, err := report.AssembleRuns(t.Context(), report.RunsInput{Case: filepath.Join(source, "reproducer"), Current: filepath.Join(source, "baseline")}, other); err != nil {
		t.Fatal(err)
	}
	authored := report.Authored{Title: "Independent snapshot"}
	if _, err := report.BuildDocument(t.Context(), other, packet, authored); err == nil {
		t.Fatal("a verified packet was paired with another folder's evidence")
	}
	identity := packet.Identity
	// A verified reading has a lifetime independent of its source directory.
	if err := os.Rename(packetPath, filepath.Join(t.TempDir(), "moved")); err != nil {
		t.Fatal(err)
	}
	packet.Identity = "edited caller metadata"
	packet.Manifest.Current.Status = "assertion_failure"
	doc, err := packet.Document(t.Context(), authored)
	if err != nil || doc.PacketIdentity != identity || doc.Result.Outcome != report.OutcomePassed {
		t.Fatal("report lost its verified snapshot or trusted edited metadata", err)
	}
	files, err := packet.DocumentReviewFiles(t.Context(), authored)
	if err != nil {
		t.Fatal(err)
	}
	files["packet/manifest.json"][0] = '!'
	review, err := packet.ExportDocumentReview(t.Context(), filepath.Join(t.TempDir(), "portable"), authored)
	if err != nil || review.Document.PacketIdentity != identity || review.Document.Result.Outcome != report.OutcomePassed {
		t.Fatal("export did not retain the owned reading independently of returned bytes", err)
	}
}
