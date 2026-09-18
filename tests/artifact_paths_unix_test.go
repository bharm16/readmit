//go:build !windows

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestReceiverRefusesAliasedEvidenceDestinations(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	_, err := receiver.New(receiver.Config{Mode: observation.Fixed, OutputPath: filepath.Join(real, "case"), ObservationPath: filepath.Join(alias, "case"), MaxMessages: 1, MaxFrameBytes: 4096, IdleTimeout: time.Second})
	if err == nil {
		t.Fatal("receiver startup accepted two names for one evidence destination")
	}
	if _, err := os.Lstat(filepath.Join(real, "case")); !os.IsNotExist(err) {
		t.Fatal("destination collision must be rejected before installing the observation")
	}
}

func TestArtifactReadersResolveRawParentsBeforeJoiningChildren(t *testing.T) {
	packet := filepath.Join(t.TempDir(), "packet")
	if _, err := report.Create(context.Background(), report.Scenario, packet); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Join(packet, "baseline", "run"), alias); err != nil {
		t.Fatal(err)
	}
	raw := alias + "/.."
	actual, err := testrunner.Open(filepath.Join(packet, "baseline"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := testrunner.Open(raw)
	if err != nil || resolved.Identity != actual.Identity {
		t.Errorf("result reader did not open the physical directory: %v", err)
	}
	comparison, err := diff.Compare(diff.Input{Path: raw}, diff.Input{Path: raw}, diff.Options{})
	if err != nil || comparison.Summary.Unchanged != 2 {
		t.Errorf("comparison did not open the physical directory: %v", err)
	}
}

func TestReviewReaderCannotSelectALexicalSibling(t *testing.T) {
	request := redactFixture(t)
	if _, err := redact.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"review", "real/review"} {
		if err := os.CopyFS(filepath.Join(dir, name), os.DirFS(request.Output)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "real", "nested"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "review", "identity.sha256"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{filepath.Join(dir, "real", "review"), dir + "/alias/../review"} {
		if _, err := redact.OpenReview(input); err == nil {
			t.Error("reader accepted a sibling instead of the changed physical review")
		}
	}
}

func TestCaptureRecordsThePhysicalSourceAfterRawParentTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real", "nested"), filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "real", "source.hl7")
	raw, err := os.ReadFile("../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "case")
	if _, _, err := run(t, "capture", dir+"/alias/../source.hl7", "--output", output); err != nil {
		t.Fatal(err)
	}
	b, err := bundle.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest.Sources[0].Path != want {
		t.Fatal("captured bytes were attributed to a different source location")
	}
}

func TestExportResolvesReviewAndPrivateRootsBeforeChildPaths(t *testing.T) {
	request := redactFixture(t)
	review, err := redact.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	aliasParent := func(child string) string {
		t.Helper()
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(child, alias); err != nil {
			t.Fatal(err)
		}
		return alias + "/.."
	}
	reviewAlias := aliasParent(filepath.Join(request.Output, "case"))
	privateAlias := aliasParent(filepath.Join(request.LocalState, "original-proof"))
	for _, part := range []string{"review", "private", "both"} {
		t.Run(part, func(t *testing.T) {
			export := redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: filepath.Join(t.TempDir(), "packet")}
			if part != "private" {
				export.ReviewPath = reviewAlias
			}
			if part != "review" {
				export.LocalState = privateAlias
			}
			manifest, err := redact.Export(context.Background(), export)
			if err != nil || manifest.ApprovedReview != review.Identity {
				t.Fatalf("export did not use the physical approved roots: %v", err)
			}
			if _, err := redact.OpenExport(export.Output); err != nil {
				t.Fatal(err)
			}
		})
	}
}
