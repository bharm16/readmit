package catalog_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
)

var clock = catalog.Options{Now: func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }}

func sum(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}

// pair is a two-member observation-like draft: a source and a window that
// only mean something together.
func pair(item, base, intent, source, window string) catalog.Draft {
	return catalog.Draft{Kind: "observation", ItemID: item, Base: base, Intent: intent, Digest: sum(item, base, source, window),
		Members: []catalog.Staged{{Role: "source", File: "source.json", Data: []byte(source)}, {Role: "window", File: "window.json", Data: []byte(window)}}}
}

func accept(map[string]string) error { return nil }

func current(t *testing.T, s *catalog.Store, id string) (catalog.Revision, map[string]string) {
	t.Helper()
	document, _, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	index := document.Find(id)
	if index < 0 || document.Items[index].Current() == nil {
		t.Fatalf("no current revision of %s", id)
	}
	revision := *document.Items[index].Current()
	contents := map[string]string{}
	for _, m := range revision.Members {
		data, err := os.ReadFile(s.Path(m))
		if err != nil {
			t.Fatal(err)
		}
		contents[m.Role] = string(data)
	}
	return revision, contents
}

func newStore(t *testing.T) *catalog.Store {
	t.Helper()
	s, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A save stopped before, between or after its member writes, or after they
// verified but before the catalog named them, leaves the previous revision
// current with both of its members; recovery never publishes a revision whose
// members did not all verify, and completes one whose members all did.
func TestAnInterruptedSaveNeverPublishesAMixedRevision(t *testing.T) {
	for _, point := range []string{catalog.PointPending, catalog.PointMember + "source", catalog.PointMember + "window", catalog.PointVerified, catalog.PointPublished} {
		t.Run(point, func(t *testing.T) {
			s := newStore(t)
			first, err := s.Save(pair("", "", "intent-1", "old-source", "old-window"), accept, clock)
			if err != nil {
				t.Fatal(err)
			}
			id := first.Item.ID
			crash := errors.New("crash at " + point)
			options := clock
			options.Fault = func(at string) error {
				if at == point {
					return crash
				}
				return nil
			}
			_, err = s.Save(pair(id, "1", "intent-2", "new-source", "new-window"), accept, options)
			if point != catalog.PointPublished && !errors.Is(err, crash) {
				t.Fatalf("the save went past the crash: %v", err)
			}
			revision, contents := current(t, s, id)
			published := point == catalog.PointPublished
			if !published && (revision.Number != 1 || contents["source"] != "old-source" || contents["window"] != "old-window") {
				t.Fatalf("the old revision is not current before publication: %d %v", revision.Number, contents)
			}
			if published && (revision.Number != 2 || contents["source"] != "new-source" || contents["window"] != "new-window") {
				t.Fatalf("a published revision is not current: %d %v", revision.Number, contents)
			}
			incomplete, err := s.Recover(func(string) catalog.Verifier { return accept }, clock)
			if err != nil {
				t.Fatal(err)
			}
			revision, contents = current(t, s, id)
			switch point {
			case catalog.PointVerified, catalog.PointPublished:
				// Every member was staged and verified: recovery completes it.
				if len(incomplete) != 0 || revision.Number != 2 || contents["source"] != "new-source" || contents["window"] != "new-window" {
					t.Fatalf("a verified save was not completed: %+v %d %v", incomplete, revision.Number, contents)
				}
			case catalog.PointPending:
				if len(incomplete) != 0 || revision.Number != 1 {
					t.Fatalf("a save that wrote nothing left work behind: %+v", incomplete)
				}
			default:
				if len(incomplete) != 1 || incomplete[0].Operation != "intent-2" || revision.Number != 1 ||
					contents["source"] != "old-source" || contents["window"] != "old-window" {
					t.Fatalf("recovery exposed a mixed revision: %+v %d %v", incomplete, revision.Number, contents)
				}
				// Retrying the same submission completes it exactly once.
				retried, err := s.Save(pair(id, "1", "intent-2", "new-source", "new-window"), accept, clock)
				if err != nil || retried.Revision != 2 {
					t.Fatalf("a retried submission: %+v %v", retried, err)
				}
				if again, _ := s.Recover(func(string) catalog.Verifier { return accept }, clock); len(again) != 0 {
					t.Fatalf("a retried submission left work behind: %+v", again)
				}
			}
		})
	}
}

// A member its kind's reader refuses is never published, whatever else of the
// draft was fine.
func TestAMemberItsReaderRefusesIsNeverPublished(t *testing.T) {
	s := newStore(t)
	first, err := s.Save(pair("", "", "intent-1", "old-source", "old-window"), accept, clock)
	if err != nil {
		t.Fatal(err)
	}
	refuse := func(files map[string]string) error {
		if data, _ := os.ReadFile(files["window"]); string(data) == "bad-window" {
			return errors.New("the window reader refuses it")
		}
		return nil
	}
	if _, err := s.Save(pair(first.Item.ID, "1", "intent-2", "new-source", "bad-window"), refuse, clock); !errors.Is(err, catalog.ErrIncomplete) {
		t.Fatalf("a refused member was published: %v", err)
	}
	if revision, contents := current(t, s, first.Item.ID); revision.Number != 1 || contents["source"] != "old-source" {
		t.Fatalf("%d %v", revision.Number, contents)
	}
	if err := s.Discard("intent-2"); err != nil {
		t.Fatal(err)
	}
	if incomplete, _ := s.Recover(func(string) catalog.Verifier { return refuse }, clock); len(incomplete) != 0 {
		t.Fatalf("a discarded save is still incomplete: %+v", incomplete)
	}
}

// Two editors that started from the same revision: the first to save
// publishes, the second is told the current revision and publishes nothing.
// One submission sent twice publishes one revision; a different submission
// under the same identity is refused.
func TestStaleBasesAndRepeatedSubmissions(t *testing.T) {
	s := newStore(t)
	first, err := s.Save(pair("", "", "intent-1", "a", "b"), accept, clock)
	if err != nil {
		t.Fatal(err)
	}
	id := first.Item.ID
	if _, err := s.Save(pair(id, "1", "editor-one", "a2", "b2"), accept, clock); err != nil {
		t.Fatal(err)
	}
	var conflict *catalog.Conflict
	if _, err := s.Save(pair(id, "1", "editor-two", "a3", "b3"), accept, clock); !errors.As(err, &conflict) || conflict.Current != "2" {
		t.Fatalf("a stale base was not a conflict naming the current revision: %v", err)
	}
	again, err := s.Save(pair(id, "1", "editor-one", "a2", "b2"), accept, clock)
	if err != nil || !again.Replayed || again.Revision != 2 {
		t.Fatalf("a repeated submission: %+v %v", again, err)
	}
	if _, err := s.Save(pair(id, "2", "editor-one", "other", "content"), accept, clock); !errors.Is(err, catalog.ErrIntentReused) {
		t.Fatalf("a different submission under one identity: %v", err)
	}
	document, _, _ := s.Read()
	if item := document.Items[document.Find(id)]; len(item.Revisions) != 2 {
		t.Fatalf("revisions: %+v", item.Revisions)
	}
	// A copy is a new object with a new identity; the original is untouched.
	copied, err := s.Save(pair("", "", "copy-1", "a2", "b2"), accept, clock)
	if err != nil || copied.Item.ID == id || copied.Revision != 1 {
		t.Fatalf("a copy: %+v %v", copied, err)
	}
	if revision, _ := current(t, s, id); revision.Number != 2 {
		t.Fatalf("saving a copy changed the original: %d", revision.Number)
	}
}

// The catalog refuses a .readmit entry that is not the project's own folder.
func TestTheManagedFolderIsNeverWrittenThroughALink(t *testing.T) {
	project := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(project, catalog.Folder)); err != nil {
		t.Fatal(err)
	}
	s, err := catalog.Open(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(pair("", "", "intent-1", "a", "b"), accept, clock); err == nil {
		t.Fatal("a save wrote through a linked managed folder")
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("the link target was written: %v", entries)
	}
}

// Two writers of one project that both started from revision 1 — two stores,
// as two processes hold — publish one revision between them: the catalog's
// writer lock makes the second see the first's revision and conflict.
func TestConcurrentWritersNeverBothPassTheSameBase(t *testing.T) {
	s := newStore(t)
	first, err := s.Save(pair("", "", "intent-1", "a", "b"), accept, clock)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(chan error, 8)
	for i := range 8 {
		go func() {
			other, err := catalog.Open(s.Root())
			if err == nil {
				_, err = other.Save(pair(first.Item.ID, "1", "writer-"+string(rune('a'+i)), "x"+string(rune('a'+i)), "y"), accept, clock)
			}
			outcomes <- err
		}()
	}
	published := 0
	for range 8 {
		var conflict *catalog.Conflict
		switch err := <-outcomes; {
		case err == nil:
			published++
		case errors.As(err, &conflict):
		default:
			t.Errorf("a writer failed otherwise: %v", err)
		}
	}
	if revision, _ := current(t, s, first.Item.ID); published != 1 || revision.Number != 2 {
		t.Fatalf("%d writers published from one base; current revision %d", published, revision.Number)
	}
	// A writer that lost the race withdrew what it staged: nothing is left
	// as interrupted work, and no staged file is left beside the revisions.
	if incomplete, err := s.Inspect(func(string) catalog.Verifier { return accept }); err != nil || len(incomplete) != 0 {
		t.Fatalf("lost races left work behind: %+v %v", incomplete, err)
	}
	entries, err := os.ReadDir(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	if staged := len(entries) - 1; staged != 4 {
		t.Fatalf("the project holds %d saved files, want the two revisions' four", staged)
	}
}

// A new save is refused once the project holds as many interrupted saves as
// it keeps, and those saves still list and can be discarded.
func TestInterruptedSavesAreBoundedWithoutBlockingTheirRecovery(t *testing.T) {
	s := newStore(t)
	crash := catalog.Options{Now: clock.Now, Fault: func(at string) error {
		if at == catalog.PointVerified {
			return errors.New("crash")
		}
		return nil
	}}
	for i := range catalog.MaxPending {
		if _, err := s.Save(pair("", "", "held-"+strings.Repeat("x", i+1), "a", string(rune('a'+i%26))+strings.Repeat("b", i)), accept, crash); err == nil {
			t.Fatal("the crash was not reported")
		}
	}
	if _, err := s.Save(pair("", "", "one-more", "a", "b"), accept, clock); !errors.Is(err, catalog.ErrTooManyPending) {
		t.Fatalf("a save past the bound: %v", err)
	}
	incomplete, err := s.Inspect(func(string) catalog.Verifier { return accept })
	if err != nil || len(incomplete) != catalog.MaxPending {
		t.Fatalf("inspect at the bound: %d %v", len(incomplete), err)
	}
	if err := s.Discard(incomplete[0].Operation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(pair("", "", "one-more", "a", "b"), accept, clock); err != nil {
		t.Fatalf("a save after a discard: %v", err)
	}
}

// Inspecting interrupted saves writes nothing, and a retry that stages other
// bytes under the interrupted submission is refused rather than mixed in.
func TestInspectingWritesNothingAndARetryMustBeTheSameSubmission(t *testing.T) {
	s := newStore(t)
	first, err := s.Save(pair("", "", "intent-1", "a", "b"), accept, clock)
	if err != nil {
		t.Fatal(err)
	}
	crash := catalog.Options{Now: clock.Now, Fault: func(at string) error {
		if at == catalog.PointVerified {
			return errors.New("crash")
		}
		return nil
	}}
	draft := pair(first.Item.ID, "1", "intent-2", "c", "d")
	if _, err := s.Save(draft, accept, crash); err == nil {
		t.Fatal("the crash was not reported")
	}
	incomplete, err := s.Inspect(func(string) catalog.Verifier { return accept })
	if err != nil || len(incomplete) != 1 {
		t.Fatalf("inspect: %+v %v", incomplete, err)
	}
	if revision, _ := current(t, s, first.Item.ID); revision.Number != 1 || !s.Pending("intent-2") {
		t.Fatalf("inspecting published or cleared the save: %d", revision.Number)
	}
	other := draft
	other.Members = []catalog.Staged{{Role: "source", File: "source.json", Data: []byte("other")}, draft.Members[1]}
	if _, err := s.Save(other, accept, clock); !errors.Is(err, catalog.ErrIntentReused) {
		t.Fatalf("a retry with other bytes: %v", err)
	}
}
