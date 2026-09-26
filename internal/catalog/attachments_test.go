package catalog_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
)

// Attaching stores a copy under a generated name and records the
// association; detaching removes the association and leaves the copy.
func TestAttachmentsAreStoredCopiesAndAssociations(t *testing.T) {
	store, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := "0123456789abcdef01234567"
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	added, err := store.Attach(item, []catalog.NewAttachment{{Name: "ticket.pdf", Type: "pdf", Data: []byte("a")}, {Name: "ticket.pdf", Type: "pdf", Data: []byte("b")}}, now, nil)
	if err != nil || len(added) != 2 || added[0].File == added[1].File {
		t.Fatalf("attach: %+v %v", added, err)
	}
	if data, err := os.ReadFile(store.AttachmentPath(added[1])); err != nil || string(data) != "b" {
		t.Fatalf("stored copy: %q %v", data, err)
	}
	if err := store.Detach(item, added[0].ID); err != nil {
		t.Fatal(err)
	}
	held, err := store.Attachments()
	if err != nil || len(held) != 1 || held[0].ID != added[1].ID {
		t.Fatalf("held: %+v %v", held, err)
	}
	if _, err := os.Stat(store.AttachmentPath(added[0])); err != nil {
		t.Fatal("detaching deleted the stored copy")
	}
	if err := store.Detach(item, added[0].ID); err != catalog.ErrNoAttachment {
		t.Fatalf("detaching twice: %v", err)
	}
	if _, err := store.Attach(item, []catalog.NewAttachment{{Name: "x", Type: "../x", Data: nil}}, now, nil); err == nil {
		t.Fatal("an unsafe type was stored")
	}
	if entries, _ := os.ReadDir(filepath.Join(store.Root(), catalog.Folder, catalog.AttachmentsFolder)); len(entries) != 2 {
		t.Fatalf("a refused attach left a copy: %v", entries)
	}
	// The copy a removed association left, and the lock held while deciding,
	// are what a quota decision leaves out, and a
	// refusal from admit stores nothing.
	var seen catalog.Detached
	refused := errors.New("no room")
	if _, err := store.Attach(item, []catalog.NewAttachment{{Name: "more", Type: "txt", Data: []byte("c")}}, now, func(detached catalog.Detached) error {
		seen = detached
		return refused
	}); err != refused || seen.Files != 2 {
		t.Fatalf("admit: %v %+v", err, seen)
	}
	if entries, _ := os.ReadDir(filepath.Join(store.Root(), catalog.Folder, catalog.AttachmentsFolder)); len(entries) != 2 {
		t.Fatalf("a refused admit stored a copy: %v", entries)
	}
	if got := catalog.AttachmentType(".PDF"); got != "pdf" {
		t.Fatal(got)
	}
	if got := catalog.AttachmentType(".tar.gz"); got != "file" {
		t.Fatal(got)
	}
}
