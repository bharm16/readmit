package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/project"
)

// A readmit-project/v2 document is a project a person named and nothing else:
// it declares no interface revision yet, and a case registered in it may
// leave its interface revision unassigned. No version is invented for either.
func TestAVersionTwoProjectNeedsNoInterfaceRevision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "named")
	empty := project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: "Scheduling QA"}}
	created, err := project.Create(root, empty)
	if err != nil {
		t.Fatalf("an empty v2 project was refused: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(created.Root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"readmit-project/v2","settings":{"title":"Scheduling QA"},"interface_versions":[],"cases":[]}` + "\n"
	if string(raw) != want {
		t.Fatalf("the empty project was written as\n%s\nwant\n%s", raw, want)
	}
	opened, err := project.Open(root)
	if err != nil || opened.Document.Schema != project.SchemaV2 || len(opened.Document.InterfaceVersions) != 0 {
		t.Fatalf("reopened: %+v %v", opened, err)
	}

	registered, entry, err := project.AddCase(opened.Document, project.Revisions{}, project.Case{
		Name: "incident", Identity: "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
		Schema: "readmit-case/v1", Provenance: "imported", Title: "Duplicate appointment",
	})
	if err != nil || entry.InterfaceVersion != "" {
		t.Fatalf("an unassigned interface revision was refused or invented: %+v %v", entry, err)
	}
	if err := opened.Save(registered); err != nil {
		t.Fatal(err)
	}
	// A value that is assigned must still be one the project declares.
	assigned := "siu-2.5.1-v1"
	if _, _, err := project.UpdateCase(registered, "incident", project.Change{InterfaceVersion: &assigned}); err == nil {
		t.Fatal("an undeclared interface revision was accepted")
	}
}

// A readmit-project/v1 document keeps its own rules and its own bytes: it is
// never read as v2, a write that needs the v2 model is refused on it, and
// migrating is an explicit act that changes only the declared contract.
func TestAVersionOneProjectIsReadUnchangedAndMigratedOnlyOnPurpose(t *testing.T) {
	v1 := document()
	data, err := project.Encode(v1)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := project.Decode(data)
	if err != nil || decoded.Schema != project.Schema {
		t.Fatalf("a v1 document did not read as v1: %+v %v", decoded, err)
	}
	unassigned := ""
	if _, _, err := project.UpdateCase(decoded, "regression", project.Change{InterfaceVersion: &unassigned}); err == nil {
		t.Fatal("a v1 project accepted an unassigned interface revision")
	}
	migrated, err := project.Migrate(decoded)
	if err != nil || migrated.Schema != project.SchemaV2 {
		t.Fatalf("migration: %+v %v", migrated, err)
	}
	migrated.Schema = project.Schema
	if again, _ := project.Encode(migrated); string(again) != string(data) {
		t.Fatal("migration changed a member besides the declared contract")
	}
	migrated.Schema = project.SchemaV2
	if _, err := project.Migrate(migrated); !errors.Is(err, project.ErrAlreadyCurrent) {
		t.Fatalf("migrating a v2 document again: %v", err)
	}
	if _, _, err := project.UpdateCase(migrated, "regression", project.Change{InterfaceVersion: &unassigned}); err != nil {
		t.Fatalf("a migrated project refused an unassigned interface revision: %v", err)
	}
}
