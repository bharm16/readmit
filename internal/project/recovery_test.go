package project_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveRetainsPreviousDocumentAndRefusesDamagedRecovery(t *testing.T) {
	p, err := project.Create(filepath.Join(t.TempDir(), "workspace"), document())
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(p.Root, project.DocumentName))
	next := p.Document
	next.Settings.Title = "Updated"
	if err := p.Save(next); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(p.Root, "project.json.recovery-*"))
	if len(copies) != 1 {
		t.Fatalf("wanted one recovery copy, got %d", len(copies))
	}
	retained, _ := os.ReadFile(copies[0])
	if !bytes.Equal(before, retained) {
		t.Fatal("prior document changed")
	}
	if err := p.Save(document()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copies[0], []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(next); err == nil {
		t.Fatal("damaged recovery copy accepted")
	}
	current, _ := os.ReadFile(filepath.Join(p.Root, project.DocumentName))
	if !bytes.Equal(before, current) {
		t.Fatal("failed save changed current document")
	}
}

func TestRecoverSelectedRevisionAndPreserveDamagedCurrent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	p, err := project.Create(root, document())
	if err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(root, revisions()); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(root, project.Revisions{Schema: project.RevisionsSchema}); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(root, "revisions.json.recovery-*"))
	if len(copies) != 1 {
		t.Fatal("prior revision not retained")
	}
	digest := strings.TrimPrefix(filepath.Base(copies[0]), "revisions.json.recovery-")
	if err := os.WriteFile(filepath.Join(root, project.RevisionsDocumentName), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := project.Recover(p.Root, project.RevisionsDocumentName, digest); err != nil {
		t.Fatal(err)
	}
	got, err := project.ReadRevisions(root)
	if err != nil || len(got.Notes) != 1 || len(got.Revisions) != 1 {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	copies, _ = filepath.Glob(filepath.Join(root, "revisions.json.recovery-*"))
	if len(copies) != 2 {
		t.Fatal("damaged bytes were not retained alongside earlier versions")
	}
}
