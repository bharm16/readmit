package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestInstalledReferenceLibrarySelectsEveryExactMessageEditionAfterReopen(t *testing.T) {
	root := t.TempDir()
	base := ownedReferenceCatalog(t, root)
	body, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	versions := []string{"2.1", "2.2", "2.3", "2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7", "2.7.1", "2.8", "2.8.1", "2.8.2", "2.9"}
	files := []string{}
	for _, edition := range versions {
		name := "hl7-" + edition + ".json"
		if err := os.WriteFile(filepath.Join(root, name), []byte(strings.Replace(string(body), `"edition":"2.5.1"`, `"edition":"`+edition+`"`, 1)), 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	manifest, _ := json.Marshal(map[string]any{"schema": "readmit-hl7-reference-library/v1", "catalogs": files})
	if err := os.WriteFile(filepath.Join(root, "library.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	documents := desktop.ShellDocuments{Folder: filepath.Join(t.TempDir(), "shell")}
	app := desktop.New(nil, documents)
	if got := app.InstallReferenceLibrary(root); got.State != desktop.Completed || len(got.Editions) != 14 {
		t.Fatalf("install: %+v", got)
	}
	app = desktop.New(nil, documents)
	for _, edition := range versions {
		file := filepath.Join(t.TempDir(), "message.hl7")
		original := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|" + edition + "\rZAA|OWNED\r"
		if err := os.WriteFile(file, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		listed := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
		got := app.InspectFileMessage(desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listed.SHA256, Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1})
		if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Reference == nil || got.Inspection.Reference.Record == nil || got.Inspection.Reference.Edition != edition || got.Inspection.Metadata.HL7Version != edition || got.Inspection.ReferenceCatalog == "" {
			t.Fatalf("%s exact selection: %+v", edition, got)
		}
		if got.Inspection.Raw != "" || got.Inspection.Decoded != "" {
			t.Fatal("automatic reference selection revealed evidence")
		}
		after, _ := os.ReadFile(file)
		if string(after) != original {
			t.Fatal("reference lookup changed source")
		}
	}
}

func TestReferenceLibraryRefusesChangedCatalogAndFailedImportKeepsPreviousSelection(t *testing.T) {
	root := t.TempDir()
	source := ownedReferenceCatalog(t, root)
	documents := desktop.ShellDocuments{Folder: filepath.Join(t.TempDir(), "shell")}
	app := desktop.New(nil, documents)
	installed := app.InstallReferenceCatalog(source)
	if installed.State != desktop.Completed {
		t.Fatalf("install: %+v", installed)
	}
	var path string
	for _, entry := range installed.Editions {
		if entry.Edition == "2.5.1" {
			path = entry.Path
		}
	}
	if path == source || path == "" {
		t.Fatal("installed catalog was not retained in the application store")
	}
	bad := filepath.Join(root, "library.json")
	os.WriteFile(bad, []byte(`{"schema":"readmit-hl7-reference-library/v1","catalogs":["../outside.json"]}`), 0600)
	if got := app.InstallReferenceLibrary(root); got.State == desktop.Completed {
		t.Fatal("accepted escaped library member")
	}
	if got := app.ReadReferenceLibrary(); got.State != desktop.Completed {
		t.Fatalf("failed import removed library: %+v", got)
	}
	body, _ := os.ReadFile(path)
	os.WriteFile(path, append(body, '\n'), 0600)
	file := filepath.Join(root, "message.hl7")
	os.WriteFile(file, []byte("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rZAA|OWNED\r"), 0600)
	listed := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
	got := app.InspectFileMessage(desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listed.SHA256, Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1})
	if got.State != desktop.Completed || got.Inspection.Reference.Record != nil || got.Inspection.Reference.Status != "not_available" {
		t.Fatalf("changed installed content was repinned: %+v", got)
	}
}

func TestSuppliedReferenceLibraryContainsAllRequestedEditionsAndKeepsTerminologyEditionBound(t *testing.T) {
	folder := os.Getenv("READMIT_HL7_REFERENCE_LIBRARY")
	if folder == "" {
		t.Skip("requires explicitly retained local documentation library")
	}
	app := desktop.New(nil, desktop.ShellDocuments{Folder: filepath.Join(t.TempDir(), "shell")})
	installed := app.InstallReferenceLibrary(folder)
	if installed.State != desktop.Completed || len(installed.Editions) != 14 {
		t.Fatalf("install full source library: %+v", installed)
	}
	for _, entry := range installed.Editions {
		if entry.Path == "" {
			t.Fatalf("edition %s has no supplied catalog", entry.Edition)
		}
		got := app.LookupHL7Reference(desktop.HL7ReferenceRequest{Catalog: entry.Path, Identity: entry.Identity, Edition: entry.Edition, Key: "field/MSH/5", Limit: 100})
		if got.State != desktop.Completed || got.Reference == nil || got.Reference.Record == nil || got.Reference.Record.Definition == "" {
			t.Fatalf("%s receiving application definition: %+v", entry.Edition, got)
		}
		if entry.Edition == "2.5.1" || entry.Edition == "2.9" {
			table := app.LookupHL7Reference(desktop.HL7ReferenceRequest{Catalog: entry.Path, Identity: entry.Identity, Edition: entry.Edition, Key: "table/0103", Limit: 100})
			if table.State != desktop.Completed || table.Reference == nil || table.Reference.Record == nil || table.Reference.Record.TableMetadata.CodeSystemOID != "2.16.840.1.113883.18.40" {
				t.Fatalf("sourced processing ID identifiers: %+v", table)
			}
			codes := []string{}
			for _, code := range table.Children {
				codes = append(codes, code.Code)
			}
			expected := "D,P,T"
			if entry.Edition == "2.9" {
				expected = "D,N,P,T,V"
			}
			if strings.Join(codes, ",") != expected {
				t.Fatalf("current terminology replaced %s source values: %v", entry.Edition, codes)
			}
		}
	}
}
