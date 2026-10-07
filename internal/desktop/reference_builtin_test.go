package desktop_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7reference"
)

func TestBundledDefinitionsWorkOnFirstLaunchWithoutInstallation(t *testing.T) {
	root := t.TempDir()
	source := ownedReferenceCatalog(t, root)
	body, _ := os.ReadFile(source)
	files := []string{}
	for _, edition := range hl7reference.Editions() {
		name := "hl7-" + edition + ".json"
		if err := os.WriteFile(filepath.Join(root, name), []byte(strings.Replace(string(body), `"edition":"2.5.1"`, `"edition":"`+edition+`"`, 1)), 0600); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	manifest, _ := json.Marshal(map[string]any{"schema": "readmit-hl7-reference-library/v1", "catalogs": files})
	os.WriteFile(filepath.Join(root, "library.json"), manifest, 0600)
	var archive bytes.Buffer
	if err := hl7reference.BuildBundle(root, &archive); err != nil {
		t.Fatal(err)
	}
	documents := desktop.ShellDocuments{Folder: filepath.Join(t.TempDir(), "fresh-shell")}
	for launch := 0; launch < 2; launch++ {
		app := desktop.NewWithBundledReferences(nil, documents, "", archive.Bytes())
		library := app.ReadReferenceLibrary()
		if library.State != desktop.Completed || len(library.Editions) != 14 {
			t.Fatalf("fresh app definitions: %+v", library)
		}
		for _, edition := range hl7reference.Editions() {
			file := filepath.Join(t.TempDir(), "message.hl7")
			original := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|" + edition + "\rZAA|OWNED\r"
			os.WriteFile(file, []byte(original), 0600)
			listed := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
			got := app.InspectFileMessage(desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listed.SHA256, Path: "ZAA[1]-1", ByteOffset: -1, RawOffset: -1})
			if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Reference == nil || got.Inspection.Reference.Record == nil || got.Inspection.Reference.Edition != edition || got.Inspection.Metadata.HL7Version != edition {
				t.Fatalf("%s without install: %+v", edition, got)
			}
			if got.Inspection.Raw != "" || got.Inspection.Decoded != "" {
				t.Fatal("default definitions revealed evidence")
			}
			after, _ := os.ReadFile(file)
			if string(after) != original {
				t.Fatal("default definitions changed the message")
			}
		}
		if _, err := os.Stat(filepath.Join(documents.Folder, "reference-library.json")); !os.IsNotExist(err) {
			t.Fatal("first launch created a manual reference selection")
		}
	}
}

func TestMissingBundledDefinitionsRemainAnExplicitBuildFailure(t *testing.T) {
	app := desktop.NewWithBundledReferences(nil, desktop.ShellDocuments{Folder: t.TempDir()}, "", nil)
	if got := app.ReadReferenceLibrary(); got.State != desktop.Failed || len(got.Editions) != 0 {
		t.Fatalf("missing built-in definitions appeared ready: %+v", got)
	}
}

func TestUnknownMessageEditionOffersBuiltInChoiceWithoutSourcing(t *testing.T) {
	for _, edition := range []string{"", "9.9"} {
		app := desktop.New(nil, desktop.ShellDocuments{Folder: t.TempDir()})
		file := filepath.Join(t.TempDir(), "message.hl7")
		os.WriteFile(file, []byte("MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|"+edition+"\r"), 0600)
		listed := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "auto", Terminator: "auto"})
		result := app.InspectFileMessage(desktop.FileInspectRequest{File: file, Format: "auto", Terminator: "auto", Expect: listed.SHA256, Path: "MSH[1]", ByteOffset: -1, RawOffset: -1})
		if result.State != desktop.Completed || result.Inspection == nil || result.Inspection.Reference == nil {
			t.Fatalf("inspection: %+v", result)
		}
		reason := result.Inspection.Reference.Reason
		if !strings.Contains(reason, "Choose HL7 version") || strings.Contains(reason, "catalog") {
			t.Fatalf("requires user sourcing: %s", reason)
		}
		if edition == "" && !strings.Contains(reason, "does not declare") {
			t.Fatal("missing version is not explained")
		}
		if edition != "" && !strings.Contains(reason, "unavailable") {
			t.Fatal("unavailable version is not explained")
		}
	}
}
