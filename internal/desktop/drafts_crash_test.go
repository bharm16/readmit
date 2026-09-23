package desktop_test

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// The environment the re-executed child reads. Like the session crash tests,
// the child is this same test binary, so nothing about the facade changes to
// make the kill reachable.
const (
	draftsCrashChild = "READMIT_DESKTOP_DRAFTS_CRASH_CHILD"
	draftsCrashStore = "READMIT_DESKTOP_DRAFTS_CRASH_STORE"
)

// draftsAcknowledged is how many retentions the parent watches the child
// acknowledge before killing it, so the kill lands inside a stream of edits
// rather than before the first one.
const draftsAcknowledged = 64

// The window claims typed text as saved only once SaveEditorDraft answered
// completed; until then the edit is in flight and unacknowledged. A kill
// separates the two. Every edit the facade acknowledged before the kill comes
// back, because acknowledging it is what made it durable. An edit it had not
// acknowledged may come back whole or not at all — never torn, never mixed with
// another, never older than one already acknowledged.
func TestAcknowledgedEditorDraftsSurviveAKillAndUnacknowledgedOnesAreNeverTorn(t *testing.T) {
	if os.Getenv(draftsCrashChild) == "1" {
		draftsCrashLoop(t)
		return
	}
	store := draftsStore(t)
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), draftsCrashChild+"=1", draftsCrashStore+"="+store)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	t.Cleanup(func() {
		if !killed {
			child.Process.Kill()
			child.Wait()
		}
	})

	acknowledged, other := 0, ""
	lines := bufio.NewScanner(stdout)
	for acknowledged < draftsAcknowledged && lines.Scan() {
		revision, ok := strings.CutPrefix(lines.Text(), "ACK ")
		if !ok {
			other = lines.Text()
			continue
		}
		if acknowledged, err = strconv.Atoi(revision); err != nil {
			t.Fatalf("the child acknowledged %q", lines.Text())
		}
	}
	if acknowledged < draftsAcknowledged {
		t.Fatalf("the child acknowledged only %d edits before it stopped: %s", acknowledged, other)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// Every acknowledgement the child printed before it died is still in the
	// pipe; the newest of them is the edit that must come back.
	for lines.Scan() {
		if revision, ok := strings.CutPrefix(lines.Text(), "ACK "); ok {
			if later, err := strconv.Atoi(revision); err == nil && later > acknowledged {
				acknowledged = later
			}
		}
	}
	child.Wait()
	killed = true

	// Restarting the shell is a new facade over the same local state.
	restarted := draftsApp(t, store)
	restored := restarted.EditorDrafts()
	if restored.State != desktop.Completed || len(restored.Drafts) != 1 {
		t.Fatalf("a kill during retention left %+v", restored)
	}
	var note desktop.NoteDraft
	if err := json.Unmarshal(restored.Drafts[0].Content, &note); err != nil {
		t.Fatalf("the restored draft is not the note that was typed: %v", err)
	}
	match := revisionBody.FindStringSubmatch(note.Body)
	if match == nil {
		t.Fatalf("a kill during retention left a torn body: %q", note.Body)
	}
	revision, _ := strconv.Atoi(match[1])
	if len(match[2]) != revision%512 {
		t.Fatalf("a kill during retention mixed two edits: %q", note.Body)
	}
	if revision < acknowledged {
		t.Fatalf("revision %d was acknowledged before the kill, but revision %d came back", acknowledged, revision)
	}

	// A kill between creating the replacement and installing it retains the
	// interrupted attempt beside the store. It is reported rather than reused,
	// and the acknowledged draft stays exactly as it came back; otherwise
	// retention simply continues under the same identity. Where the kill lands
	// is not chosen here; TestEditorDraftsAreWrittenCompletelyAndPrivately
	// retains an interrupted attempt deliberately.
	next := restored.Drafts[0]
	next.Content = []byte(noteRevision(revision + 1))
	continued := restarted.SaveEditorDraft(next)
	if _, err := os.Stat(store + ".incomplete"); err == nil {
		if continued.State != desktop.Failed || len(continued.Drafts) != 1 || continued.Drafts[0].Content.String() != restored.Drafts[0].Content.String() {
			t.Fatalf("an interrupted retention was reused or hid what is retained: %+v", continued)
		}
	} else if continued.State != desktop.Completed || len(continued.Drafts) != 1 || continued.Drafts[0].ID != next.ID {
		t.Fatalf("retention did not continue after the kill: %+v", continued)
	}
}

// draftsCrashLoop retains a stream of edits of one note and acknowledges each
// one only once the facade answered completed, the same rule the window's
// editors follow, until it is killed.
func draftsCrashLoop(t *testing.T) {
	app := draftsApp(t, os.Getenv(draftsCrashStore))
	draft := editorDraft("note", desktop.NoteDraftSchema, noteRevision(1))
	for revision := 1; ; revision++ {
		draft.Content = []byte(noteRevision(revision))
		retained := app.SaveEditorDraft(draft)
		if retained.State != desktop.Completed || len(retained.Drafts) != 1 {
			t.Fatalf("child could not retain revision %d: %+v", revision, retained)
		}
		draft.ID = retained.Drafts[0].ID
		fmt.Printf("ACK %d\n", revision)
	}
}
