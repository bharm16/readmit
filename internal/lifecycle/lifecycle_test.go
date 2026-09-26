package lifecycle_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
)

func workspace(t *testing.T) string {
	t.Helper()
	p, err := project.Create(filepath.Join(t.TempDir(), "workspace"), project.Document{Schema: project.Schema, Settings: project.Settings{Title: "Investigation"}, InterfaceVersions: []string{"v1"}})
	if err != nil {
		t.Fatal(err)
	}
	return p.Root
}

// archiveThroughSelection drives Archive the way every caller does: the
// selection comes from PreviewRetirement immediately before the operation.
func archiveThroughSelection(root, destination string, deleteSource bool) (backup.Report, error) {
	retirement, err := lifecycle.PreviewRetirement(context.Background(), root)
	if err != nil {
		return backup.Report{}, err
	}
	return lifecycle.Archive(context.Background(), root, destination, retirement.Selection, deleteSource)
}

func TestArchiveDeleteRestore(t *testing.T) {
	root := workspace(t)
	dest := filepath.Join(t.TempDir(), "archive")
	report, err := archiveThroughSelection(root, dest, false)
	if err != nil || !report.Complete() {
		t.Fatalf("archive: %+v %v", report, err)
	}
	if _, err := project.Open(root); err != nil {
		t.Fatal("archive removed project")
	}
	if _, err := archiveThroughSelection(root, filepath.Join(root, "unsafe"), true); err == nil {
		t.Fatal("deleted into own recovery tree")
	}
	retired := filepath.Join(t.TempDir(), "retired")
	if _, err := archiveThroughSelection(root, retired, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("delete left project")
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if r, err := backup.Restore(context.Background(), retired, restored, time.Now()); err != nil || !r.Complete() {
		t.Fatalf("restore: %+v %v", r, err)
	}
	if p, err := project.Open(restored); err != nil || p.Document.Settings.Title != "Investigation" {
		t.Fatal("identity document lost")
	}
}

// The retirement preview inventories the source and derives the selection; a
// project changed after the preview is refused at archive and at delete,
// before anything is written, and an incompatible project previews as
// incompatible and is refused all the same.
func TestPreviewRetirementBindsArchiveAndDeleteToTheSelection(t *testing.T) {
	t.Run("preview facts and stale refusal", func(t *testing.T) {
		root := workspace(t)
		preview, err := lifecycle.PreviewRetirement(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if !preview.Compatible || preview.Files < 1 || preview.Bytes <= 0 || len(preview.Selection) != 64 {
			t.Fatalf("the preview did not inventory the source: %+v", preview)
		}
		named := false
		for _, document := range preview.Documents {
			named = named || document.Document == project.DocumentName
		}
		if !named {
			t.Fatalf("the preview lost the project document: %+v", preview.Documents)
		}
		archive := filepath.Join(t.TempDir(), "archive")
		report, err := lifecycle.Archive(context.Background(), root, archive, preview.Selection, false)
		if err != nil || !report.Complete() {
			t.Fatalf("archive: %+v %v", report, err)
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("archive removed project")
		}
		opened, err := project.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		changed := opened.Document
		changed.Settings.Title += " changed after the preview"
		if err := project.WriteDocument(root, changed); err != nil {
			t.Fatal(err)
		}
		staleDestination := filepath.Join(t.TempDir(), "stale-archive")
		if _, err := lifecycle.Archive(context.Background(), root, staleDestination, preview.Selection, true); err == nil ||
			!strings.Contains(err.Error(), "changed since") {
			t.Fatalf("a delete bound to a stale preview: %v", err)
		}
		if _, err := os.Lstat(staleDestination); !os.IsNotExist(err) {
			t.Fatal("a refused delete wrote its recovery archive anyway")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("a refused delete removed the project")
		}
		// The same stale selection is refused for an archive that deletes
		// nothing, before any write.
		staleArchive := filepath.Join(t.TempDir(), "stale-archive-two")
		if _, err := lifecycle.Archive(context.Background(), root, staleArchive, preview.Selection, false); err == nil {
			t.Fatal("an archive bound to a stale preview ran")
		}
		if _, err := os.Lstat(staleArchive); !os.IsNotExist(err) {
			t.Fatal("a refused archive wrote a recovery archive anyway")
		}
		fresh, err := lifecycle.PreviewRetirement(context.Background(), root)
		if err != nil || !fresh.Compatible {
			t.Fatalf("fresh preview: %+v %v", fresh, err)
		}
		if fresh.Selection == preview.Selection {
			t.Fatal("a changed project kept the same selection token")
		}
		retired := filepath.Join(t.TempDir(), "retired")
		if _, err := lifecycle.Archive(context.Background(), root, retired, fresh.Selection, true); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("delete left project")
		}
	})
	t.Run("incompatible project", func(t *testing.T) {
		root := workspace(t)
		if err := os.WriteFile(filepath.Join(root, "future.json"), []byte(`{"schema":"readmit-index/v99"}`), 0600); err != nil {
			t.Fatal(err)
		}
		preview, err := lifecycle.PreviewRetirement(context.Background(), root)
		if err != nil || preview.Compatible {
			t.Fatalf("a future index previewed as compatible: %+v %v", preview, err)
		}
		if _, err := lifecycle.Archive(context.Background(), root, filepath.Join(t.TempDir(), "refused"), preview.Selection, true); err == nil {
			t.Fatal("deleted incompatible project")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("refusal changed project")
		}
	})
	t.Run("backup limits at preview time", func(t *testing.T) {
		root := workspace(t)
		oversized := filepath.Join(root, "oversized.bin")
		if err := os.WriteFile(oversized, make([]byte, backup.MaxFileBytes+1), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := lifecycle.PreviewRetirement(context.Background(), root); err == nil {
			t.Fatal("a source beyond the backup limits was previewed")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("the refused preview changed the project")
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		root := workspace(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := lifecycle.PreviewRetirement(ctx, root); err == nil {
			t.Fatal("a cancelled preview succeeded")
		}
		if _, err := lifecycle.Archive(ctx, root, filepath.Join(t.TempDir(), "cancelled"), "any-selection", true); err == nil {
			t.Fatal("cancelled deletion succeeded")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("refusal changed project")
		}
	})
	t.Run("empty selection", func(t *testing.T) {
		root := workspace(t)
		if _, err := lifecycle.Archive(context.Background(), root, filepath.Join(t.TempDir(), "archive"), "", false); err == nil ||
			!strings.Contains(err.Error(), "retirement preview selection") {
			t.Fatalf("an archive without a selection ran: %v", err)
		}
	})
}

func TestPreviewUnsupportedAndCancellation(t *testing.T) {
	root := workspace(t)
	plan, err := lifecycle.Preview(context.Background(), root)
	if err != nil || !plan.Compatible {
		t.Fatalf("preview: %+v %v", plan, err)
	}
	if err := os.WriteFile(filepath.Join(root, "future.json"), []byte(`{"schema":"readmit-index/v99"}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = lifecycle.Preview(context.Background(), root)
	if err != nil || plan.Compatible {
		t.Fatalf("future index accepted: %+v %v", plan, err)
	}
	if _, err := archiveThroughSelection(root, filepath.Join(t.TempDir(), "refused"), true); err == nil {
		t.Fatal("deleted incompatible project")
	}
	os.Remove(filepath.Join(root, "future.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lifecycle.Archive(ctx, root, filepath.Join(t.TempDir(), "cancelled"), "any-selection", true); err == nil {
		t.Fatal("cancelled deletion succeeded")
	}
	if _, err := project.Open(root); err != nil {
		t.Fatal("refusal changed project")
	}
}

func TestDeleteRefusesExistingRetirementAndIncompleteEvidence(t *testing.T) {
	t.Run("retirement destination", func(t *testing.T) {
		root := workspace(t)
		if err := os.Mkdir(root+".retiring", 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := archiveThroughSelection(root, filepath.Join(t.TempDir(), "archive"), true); err == nil {
			t.Fatal("existing retirement directory replaced")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("project removed")
		}
	})
	t.Run("missing evidence", func(t *testing.T) {
		root := workspace(t)
		p, err := project.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		p.Document.Cases = []project.Case{{Name: "missing", Identity: "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df", Schema: "readmit-case/v1", Provenance: "generated", InterfaceVersion: "v1", Title: "Missing", Status: project.StatusOpen}}
		if err := p.Save(p.Document); err != nil {
			t.Fatal(err)
		}
		r, err := archiveThroughSelection(root, filepath.Join(t.TempDir(), "archive"), true)
		if err == nil || r.Complete() {
			t.Fatal("incomplete archive authorized deletion")
		}
		if _, err := project.Open(root); err != nil {
			t.Fatal("missing-evidence project removed")
		}
	})
}

func TestBackupDoesNotCopyFutureIndexValues(t *testing.T) {
	root := workspace(t)
	if err := os.WriteFile(filepath.Join(root, "future.json"), []byte(`{"schema":"readmit-index/v99","patient_value":"private"}`), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "archive")
	r, err := backup.Create(context.Background(), root, dest)
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete() || len(r.Indexes) != 1 || r.Indexes[0].State != backup.IndexUndeclared {
		t.Fatalf("future recipe accepted: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dest, backup.FilesDirectory, "future.json")); !os.IsNotExist(err) {
		t.Fatal("future index values were copied")
	}
}

func TestOversizedFutureIndexCannotAuthorizeDeletion(t *testing.T) {
	root := workspace(t)
	data := append([]byte(`{"schema":"readmit-index/v99","padding":"`), bytes.Repeat([]byte("x"), 17<<20)...)
	data = append(data, []byte(`"}`)...)
	if err := os.WriteFile(filepath.Join(root, "future.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := lifecycle.Preview(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Compatible {
		t.Fatal("oversized future index previewed compatible")
	}
	r, err := backup.Create(context.Background(), root, filepath.Join(t.TempDir(), "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete() || len(r.Indexes) != 1 || r.Indexes[0].State != backup.IndexUndeclared {
		t.Fatal("oversized future index treated as ordinary file")
	}
	if _, err := archiveThroughSelection(root, filepath.Join(t.TempDir(), "archive"), true); err == nil {
		t.Fatal("oversized future index allowed deletion")
	}
	if _, err := project.Open(root); err != nil {
		t.Fatal("source project removed")
	}
}

func TestMalformedDeclaredIndexNeverBecomesRetainedEvidence(t *testing.T) {
	for _, schema := range []string{"readmit-index/v1", "readmit-index/v99"} {
		for _, suffix := range []string{`,"patient_value":"private`, `,"patient_value":"private",!}`} {
			t.Run(schema+suffix, func(t *testing.T) {
				root := workspace(t)
				const name = "derived-without-json-extension"
				data := []byte(`{"schema":"` + schema + `"` + suffix)
				if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
					t.Fatal(err)
				}
				plan, err := lifecycle.Preview(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Compatible {
					t.Fatal("malformed declared index previewed compatible")
				}
				dest := filepath.Join(t.TempDir(), "backup")
				report, err := backup.Create(context.Background(), root, dest)
				if err != nil {
					t.Fatal(err)
				}
				if report.Complete() || len(report.Indexes) != 1 || report.Indexes[0].State != backup.IndexUndeclared {
					t.Fatalf("malformed index was not refused: %+v", report)
				}
				if _, err := os.Stat(filepath.Join(dest, backup.FilesDirectory, name)); !os.IsNotExist(err) {
					t.Fatal("derived patient values copied as ordinary evidence")
				}
				if _, err := archiveThroughSelection(root, filepath.Join(t.TempDir(), "retirement"), true); err == nil {
					t.Fatal("malformed index allowed retirement")
				}
				if _, err := project.Open(root); err != nil {
					t.Fatal("refused retirement removed project")
				}
			})
		}
	}
}
