package project_test

import (
	"errors"
	"github.com/bharm16/readmit/internal/project"
	"os"
	"path/filepath"
	"testing"
)

func TestQuotaIncludesRecoveryAndRefusesWrite(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "project"), document())
	if err != nil {
		t.Fatal(err)
	}
	q := project.Quota{Schema: project.QuotaSchema, MaxFiles: 2, MaxBytes: 100000}
	if err := project.SetQuota(p.Root, q); err != nil {
		t.Fatal(err)
	}
	d := p.Document
	d.Settings.Title = "next"
	if err := p.Save(d); !errors.Is(err, project.ErrQuota) {
		t.Fatalf("wanted quota refusal including recovery, got %v", err)
	}
	reopened, err := project.Open(p.Root)
	if err != nil || reopened.Document.Settings.Title != document().Settings.Title {
		t.Fatal("refused write changed project")
	}
	entries, _ := os.ReadDir(p.Root)
	if len(entries) != 2 {
		t.Fatal("refused write leaked files")
	}
	if _, err := project.CheckQuota(p.Root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, "external"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := project.CheckQuota(p.Root); !errors.Is(err, project.ErrQuota) {
		t.Fatalf("external excess not reported: %v", err)
	}
}

func TestQuotaRefusesByteLimitAndUnsupportedDeclaration(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "project"), document())
	if err != nil {
		t.Fatal(err)
	}
	if err := project.SetQuota(p.Root, project.Quota{Schema: project.QuotaSchema, MaxBytes: 1, MaxFiles: 100}); !errors.Is(err, project.ErrQuota) {
		t.Fatalf("byte quota accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Root, project.QuotaDocumentName)); !os.IsNotExist(err) {
		t.Fatal("refused quota was installed")
	}
	if err := os.WriteFile(filepath.Join(p.Root, project.QuotaDocumentName), []byte(`{"schema":"readmit-project-quota/v99","max_bytes":100000,"max_files":100}`), 0600); err != nil {
		t.Fatal(err)
	}
	d := p.Document
	d.Settings.Title = "changed"
	if err := p.Save(d); !errors.Is(err, project.ErrUnsupportedVersion) {
		t.Fatalf("unknown quota was ignored: %v", err)
	}
}
