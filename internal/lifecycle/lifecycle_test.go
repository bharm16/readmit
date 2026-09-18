package lifecycle_test

import (
	"context"
	"os"
	"path/filepath"
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
func TestArchiveDeleteRestore(t *testing.T) {
	root := workspace(t)
	dest := filepath.Join(t.TempDir(), "archive")
	report, err := lifecycle.Archive(context.Background(), root, dest, false)
	if err != nil || !report.Complete() {
		t.Fatalf("archive: %+v %v", report, err)
	}
	if _, err := project.Open(root); err != nil {
		t.Fatal("archive removed project")
	}
	if _, err := lifecycle.Archive(context.Background(), root, filepath.Join(root, "unsafe"), true); err == nil {
		t.Fatal("deleted into own recovery tree")
	}
	retired := filepath.Join(t.TempDir(), "retired")
	if _, err := lifecycle.Archive(context.Background(), root, retired, true); err != nil {
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
	if _, err := lifecycle.Archive(context.Background(), root, filepath.Join(t.TempDir(), "refused"), true); err == nil {
		t.Fatal("deleted incompatible project")
	}
	os.Remove(filepath.Join(root, "future.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lifecycle.Archive(ctx, root, filepath.Join(t.TempDir(), "cancelled"), true); err == nil {
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
		if _, err := lifecycle.Archive(context.Background(), root, filepath.Join(t.TempDir(), "archive"), true); err == nil {
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
		r, err := lifecycle.Archive(context.Background(), root, filepath.Join(t.TempDir(), "archive"), true)
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
