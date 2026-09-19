package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
)

func rehashCaseVersion(t *testing.T, path, version string) {
	t.Helper()
	files := directoryFiles(t, path)
	delete(files, "identity.sha256")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	h := sha256.New()
	h.Write([]byte(version + "\n"))
	for _, name := range names {
		for _, data := range [][]byte{[]byte(name), files[name]} {
			binary.Write(h, binary.BigEndian, uint64(len(data)))
			h.Write(data)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(hex.EncodeToString(h.Sum(nil))+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDerivedProvenanceHasDistinctVersionAndNoSourceMetadata(t *testing.T) {
	provenance := bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"}
	path, created := write(t, []bundle.Input{{Data: fixture(t, "listen-s12.hl7")}}, provenance)
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Identity != created.Identity || opened.Manifest.Schema != "readmit-case/v3" || opened.Manifest.Provenance.Mode != bundle.Derived || opened.Manifest.Sources[0].Path != "" || opened.Events[0].ImportedAt != nil || opened.Events[0].ObservedAt != nil {
		t.Fatal("derived provenance fabricated source metadata")
	}
	if _, err := bundle.Write(filepath.Join(t.TempDir(), "case"), []bundle.Input{{Path: "original", Data: fixture(t, "listen-s12.hl7")}}, provenance); err == nil {
		t.Fatal("derived writer retained a source path")
	}
	file := filepath.Join(path, "manifest.json")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ from, to string }{
		{`"provenance":{`, `"provenance":{"imported_at":null,`},
		{`"provenance":{`, `"provenance":{"generator":null,`},
		{`"provenance":{`, `"provenance":{"session_id":"",`},
		{`"schema":`, `"observation":null,"schema":`},
		{`"sources":[{`, `"sources":[{"path":"",`},
	} {
		modified := bytes.Replace(original, []byte(change.from), []byte(change.to), 1)
		if err := os.WriteFile(file, modified, 0600); err != nil {
			t.Fatal(err)
		}
		rehashCaseVersion(t, path, "readmit-case/v3")
		if _, err := bundle.Open(path); err == nil {
			t.Fatal("v3 accepted a forbidden legacy member, even after rehashing")
		}
	}
}

func TestLegacyVersionsStillRejectDerivedMemberEvenNull(t *testing.T) {
	for _, version := range []string{"readmit-case/v1", "readmit-case/v2"} {
		t.Run(version, func(t *testing.T) {
			var path string
			if version == "readmit-case/v1" {
				path, _ = write(t, []bundle.Input{{Path: "original", Data: fixture(t, "listen-s12.hl7")}}, imported())
			} else {
				path = recorded(t)
			}
			file := filepath.Join(path, "manifest.json")
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(`"provenance":{`), []byte(`"provenance":{"derivation":null,`), 1)
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			rehashCaseVersion(t, path, version)
			if _, err := bundle.Open(path); err == nil {
				t.Fatal("legacy schema silently gained a v3 member")
			}
		})
	}
}

// The derivation names which transformation wrote a v3 bundle. The set is
// closed, so a case cannot declare a transformation no code here performs, and
// each accepted name still produces evidence carrying no source metadata.
func TestDerivedEvidenceAcceptsOnlyTheNamedTransformations(t *testing.T) {
	for _, derivation := range []string{"readmit-redact/v1", "readmit-reproducer/v1"} {
		t.Run(derivation, func(t *testing.T) {
			path, created := write(t, []bundle.Input{{Data: fixture(t, "listen-s12.hl7")}}, bundle.Provenance{Mode: bundle.Derived, Derivation: derivation})
			opened, err := bundle.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if opened.Identity != created.Identity || opened.Manifest.Provenance.Derivation != derivation || opened.Manifest.Sources[0].Path != "" {
				t.Fatal("derived evidence did not record the transformation that wrote it")
			}
		})
	}
	for _, derivation := range []string{"", "readmit-reproducer/v2", "readmit-unknown/v1"} {
		if _, err := bundle.Write(filepath.Join(t.TempDir(), "case"), []bundle.Input{{Data: fixture(t, "listen-s12.hl7")}}, bundle.Provenance{Mode: bundle.Derived, Derivation: derivation}); err == nil {
			t.Fatalf("derived writer accepted the undeclared derivation %q", derivation)
		}
	}
}
