package desktop

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The eight shell documents share one store, so they share one set of
// guarantees and this is where those guarantees are tested once: what the
// store reads and refuses to read, how it replaces a document, and what the
// remembered selections read and write. The documents' own tests keep their
// decoders.

// A missing document is an absence the store reports as the filesystem does,
// so a caller can treat it as an empty one; a name that cannot be inspected
// keeps the filesystem's error, so a caller can tell a folder this account
// cannot read from any other refusal.
func TestTheStoreReportsAMissingShellDocumentAsAbsent(t *testing.T) {
	documents := ShellDocuments{Folder: t.TempDir()}
	if _, err := documents.read(recentName, maxRecentBytes); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing document read as %v", err)
	}
	if err := documents.write(sessionName, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.read(sessionName, maxSessionBytes); err != nil {
		t.Fatalf("the document the store wrote could not be read back: %v", err)
	}
}

// The one reading rule: the name must hold a regular file within the
// document's bound. Contents past the bound are refused before they are read
// into memory.
func TestTheStoreRefusesAShellDocumentPastItsBound(t *testing.T) {
	documents := ShellDocuments{Folder: t.TempDir()}
	oversized := strings.Repeat("x", maxRecentBytes+1)
	if err := os.WriteFile(documents.path(recentName), []byte(oversized), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.read(recentName, maxRecentBytes); !errors.Is(err, errNotADocument) {
		t.Fatalf("an oversized document read as %v", err)
	}
	if err := os.WriteFile(documents.path(recentName), []byte(strings.Repeat("x", maxRecentBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.read(recentName, maxRecentBytes); err != nil {
		t.Fatalf("a document exactly at its bound was refused: %v", err)
	}
}

// A replacement installs the whole document with owner-only permissions, in a
// folder it creates for itself, and an interrupted replacement is retained
// beside the document, reported rather than reused, and the previous document
// is kept exactly as it was.
func TestTheStoreReplacesAWholeDocumentAndNeverReusesAnInterruptedOne(t *testing.T) {
	documents := ShellDocuments{Folder: filepath.Join(t.TempDir(), "readmit")}
	first := []byte(`{"before":true}` + "\n")
	if err := documents.write(draftsName, first); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(documents.path(draftsName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("the document is readable beyond its owner: %v", info.Mode().Perm())
	}
	folder, err := os.Stat(documents.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if folder.Mode().Perm() != 0700 {
		t.Fatalf("the store's folder is open beyond its owner: %v", folder.Mode().Perm())
	}

	incomplete := documents.path(draftsName) + ".incomplete"
	if err := os.WriteFile(incomplete, []byte("interrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := documents.write(draftsName, []byte(`{"after":true}`+"\n")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("an interrupted replacement was reused: %v", err)
	}
	if kept, err := os.ReadFile(incomplete); err != nil || string(kept) != "interrupted" {
		t.Fatalf("the interrupted replacement was overwritten: %q %v", kept, err)
	}
	if kept, err := os.ReadFile(documents.path(draftsName)); err != nil || string(kept) != string(first) {
		t.Fatalf("a refused replacement changed the document: %q", kept)
	}
}

// A remembered selection is two members and a newline, in contract order, and
// nothing else: the bytes every window reads back are the bytes this release
// has always written.
func TestARememberedSelectionIsWrittenInTheBytesTheContractDeclares(t *testing.T) {
	for _, tc := range []struct {
		selection rememberedSelection
		path      string
		want      string
	}{
		{
			rememberedSelection{schema: operationSelectionSchema, member: "policy"},
			"/activation folder/operation-policy.json",
			`{"schema":"readmit-desktop-operation-selection/v1","policy":"/activation folder/operation-policy.json"}` + "\n",
		},
		{
			rememberedSelection{schema: commercialSelectionSchema, member: "config"},
			"/operator/destinations.json",
			`{"schema":"readmit-desktop-commercial-selection/v1","config":"/operator/destinations.json"}` + "\n",
		},
		{
			rememberedSelection{schema: hubSelectionSchema, member: "config"},
			"/customer/hub-client.json",
			`{"schema":"readmit-desktop-hub-selection/v1","config":"/customer/hub-client.json"}` + "\n",
		},
	} {
		data, err := tc.selection.encode(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tc.want {
			t.Errorf("the selection is %q, want %q", data, tc.want)
		}
		got, err := tc.selection.decode([]byte(tc.want))
		if err != nil || got != tc.path {
			t.Errorf("the written selection read back as %q (%v)", got, err)
		}
	}
}

// A document at a selection's name that is not a selection this release reads
// is refused, whatever the reason: an unknown member, another version, a
// relative path, a missing path, or bytes that are not JSON at all.
func TestTheStoreRefusesADocumentThatIsNotASelectionItReads(t *testing.T) {
	selection := rememberedSelection{schema: hubSelectionSchema, member: "config"}
	for name, data := range map[string]string{
		"unknown member":  `{"schema":"readmit-desktop-hub-selection/v1","config":"/used","connect":true}`,
		"another version": `{"schema":"readmit-desktop-hub-selection/v2","config":"/used"}`,
		"relative path":   `{"schema":"readmit-desktop-hub-selection/v1","config":"hub-client.json"}`,
		"missing path":    `{"schema":"readmit-desktop-hub-selection/v1"}`,
		"not JSON":        "hub-client.json\n",
		"another member":  `{"schema":"readmit-desktop-hub-selection/v1","policy":"/used"}`,
	} {
		if _, err := selection.decode([]byte(data)); err == nil {
			t.Errorf("%s was read as a selection", name)
		}
	}
	if _, err := selection.decode([]byte(`{"schema":"readmit-desktop-hub-selection/v1","config":"/used"}`)); err != nil {
		t.Errorf("the one selection this release reads was refused: %v", err)
	}
}

// Recall answers nothing when there is no document, and the store's own
// refusals — including a document past its bound — reach the caller to report.
func TestASelectionRecallReportsWhatTheStoreRefused(t *testing.T) {
	documents := ShellDocuments{Folder: t.TempDir()}
	selection := rememberedSelection{documents: documents, name: hubSelectionName, schema: hubSelectionSchema, member: "config"}
	if path, err := selection.recall(); path != "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing selection recalled as %q (%v)", path, err)
	}
	if err := os.WriteFile(documents.path(hubSelectionName), []byte(strings.Repeat("x", maxSelectionBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := selection.recall(); !errors.Is(err, errNotADocument) {
		t.Fatalf("an oversized selection recalled as %v", err)
	}
}
