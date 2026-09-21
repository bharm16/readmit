//go:build !windows

package desktop_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// An unreadable store is reported as permission denied and left exactly as
// written, so a person can fix the account's permissions instead of finding
// their drafts replaced.
func TestEditorDraftsSeparateReadPermissionFromFailure(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	unreadable(t, store)
	restored := app.EditorDrafts()
	if restored.State != desktop.PermissionDenied || restored.Drafts != nil {
		t.Fatalf("an unreadable store was not reported as permission denied: %+v", restored)
	}
	if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.PermissionDenied {
		t.Fatalf("a draft was retained over a store this account cannot read: %+v", result)
	}
}

// A store this account cannot write is permission denied, with what stays
// retained reported rather than the candidate that was refused.
func TestEditorDraftsSeparateWritePermissionFromFailure(t *testing.T) {
	directory := t.TempDir()
	store := filepath.Join(directory, "drafts.json")
	app := draftsApp(t, store)
	if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(directory, 0700) })
	result := app.SaveEditorDraft(editorDraft("test-draft", "readmit-test-draft/v1", answeredTestDraft))
	if result.State != desktop.PermissionDenied || len(result.Drafts) != 1 {
		t.Fatalf("an unwritable store was not reported as permission denied with what is retained: %+v", result)
	}
}

// A store the disk refused is a visible failure, never a silent loss: the
// refused edit is reported, what was retained before it stays retained, and the
// interrupted attempt leaves nothing behind. The disk is full in the one way a
// test can make it full portably — the account's own file size limit — and the
// limited window runs in a child process, because lowering the limit in this
// process would also starve the test framework's own log writes.
func TestEditorDraftsSeparateADiskRefusalFromSuccess(t *testing.T) {
	if os.Getenv(diskFullChild) == "1" {
		diskFullChildRun(t)
		return
	}
	store := draftsStore(t)
	app := draftsApp(t, store)
	if result := app.SaveEditorDraft(editorDraft("note", desktop.NoteDraftSchema, unfinishedNote)); result.State != desktop.Completed {
		t.Fatalf("save draft: %+v", result)
	}
	before, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), diskFullChild+"=1", diskFullStore+"="+store)
	output, _ := child.CombinedOutput()
	if !strings.Contains(string(output), "RESULT:"+string(desktop.Failed)) {
		t.Fatalf("the child did not report a refused write: %s", output)
	}
	if _, err := os.Stat(store + ".incomplete"); !os.IsNotExist(err) {
		t.Fatalf("a refused write left an interrupted attempt beside the store: %v", err)
	}
	after, err := os.ReadFile(store)
	if err != nil || string(after) != string(before) {
		t.Fatalf("a refused write changed the retained drafts: %q", after)
	}
}

const diskFullChild = "READMIT_DRAFTS_DISK_FULL_CHILD"
const diskFullStore = "READMIT_DRAFTS_DISK_FULL_STORE"

// diskFullChildRun performs one save well inside the store's own document
// bound but past the account's file size limit, so the kernel refuses the
// whole document rather than a part of it, and reports the result for the
// parent to assert on. The store is only ever read here: everything the child
// itself writes — its license fixture, its own shell state — happens before
// the limit drops, and the refused save must leave the parent's document
// exactly as the parent read it.
func diskFullChildRun(t *testing.T) {
	t.Helper()
	store := os.Getenv(diskFullStore)
	app := draftsApp(t, store)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Printf("RESULT:getrlimit failed\n")
		return
	}
	limit.Cur = 4096
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Printf("RESULT:setrlimit failed\n")
		return
	}
	oversized := editorDraft("canonical-test", "readmit-test/v1", `"`+strings.Repeat("x", 8192)+`"`)
	result := app.SaveEditorDraft(oversized)
	fmt.Printf("RESULT:%s\n", result.State)
}
