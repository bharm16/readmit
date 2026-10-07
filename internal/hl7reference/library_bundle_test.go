package hl7reference_test

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7reference"
)

func ownedBundle(t *testing.T) ([]byte, string) {
	t.Helper()
	folder := t.TempDir()
	names := []string{}
	for _, edition := range hl7reference.Editions() {
		name := "hl7-" + edition + ".json"
		raw := strings.Replace(catalog, `"edition":"2.5.1"`, `"edition":"`+edition+`"`, 1)
		if err := os.WriteFile(filepath.Join(folder, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	raw, _ := json.Marshal(map[string]any{"schema": "readmit-hl7-reference-library/v1", "catalogs": names})
	if err := os.WriteFile(filepath.Join(folder, "library.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := hl7reference.BuildBundle(folder, &archive); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes(), folder
}

func TestBundledLibraryPreservesAllEditionsAndRefusesChangedCachedContent(t *testing.T) {
	raw, source := ownedBundle(t)
	bundle, err := hl7reference.OpenBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Verify(); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	if err := bundle.Retain(folder); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Entries()) != 14 {
		t.Fatal("incomplete built-in library")
	}
	for _, entry := range bundle.Entries() {
		want, _ := os.ReadFile(filepath.Join(source, entry.File))
		got, err := os.ReadFile(filepath.Join(folder, entry.SHA256+".json"))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("changed %s source content", entry.Edition)
		}
	}
	if err := bundle.Retain(folder); err != nil {
		t.Fatalf("unchanged restart: %v", err)
	}
	entry := bundle.Entries()[0]
	path := filepath.Join(folder, entry.SHA256+".json")
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Retain(folder); err == nil {
		t.Fatal("changed content silently repinned")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "changed" {
		t.Fatal("replaced changed content")
	}
}

func TestBundledLibraryRequiresCompletePinnedInventory(t *testing.T) {
	raw, folder := ownedBundle(t)
	manifest := filepath.Join(folder, "library.json")
	body, _ := os.ReadFile(manifest)
	for _, invalid := range []string{
		strings.Replace(string(body), `"hl7-2.9.json",`, "", 1),
		strings.Replace(string(body), `hl7-2.9.json`, `../hl7-2.9.json`, 1),
		strings.Replace(string(body), `hl7-2.9.json`, `hl7-2.8.2.json`, 1),
	} {
		os.WriteFile(manifest, []byte(invalid), 0600)
		if err := hl7reference.BuildBundle(folder, io.Discard); err == nil {
			t.Fatal("accepted incomplete, escaped or duplicate edition")
		}
	}
	for _, mode := range []string{"extra member", "changed catalog", "wrong edition", "unknown manifest member"} {
		t.Run(mode, func(t *testing.T) {
			original, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			var altered bytes.Buffer
			writer := zip.NewWriter(&altered)
			for _, file := range original.File {
				reader, _ := file.Open()
				content, _ := io.ReadAll(reader)
				reader.Close()
				if mode == "changed catalog" && file.Name == "hl7-2.9.json" {
					content = append(content, ' ')
				}
				if file.Name == "manifest.json" {
					if mode == "wrong edition" {
						content = bytes.Replace(content, []byte(`"edition":"2.9"`), []byte(`"edition":"2.8"`), 1)
					}
					if mode == "unknown manifest member" {
						content = bytes.Replace(content, []byte(`"schema":`), []byte(`"future":null,"schema":`), 1)
					}
				}
				header := &zip.FileHeader{Name: file.Name, Method: zip.Deflate}
				header.SetMode(0600)
				member, _ := writer.CreateHeader(header)
				member.Write(content)
			}
			if mode == "extra member" {
				member, _ := writer.Create("../escaped")
				member.Write([]byte("extra"))
			}
			writer.Close()
			bundle, err := hl7reference.OpenBundle(altered.Bytes())
			if err == nil {
				err = bundle.Verify()
			}
			if err == nil {
				t.Fatal("accepted damaged bundle")
			}
		})
	}
}
