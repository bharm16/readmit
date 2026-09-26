//go:build !windows

package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// A link at a document's name is refused rather than followed, and so is
// anything else at the name that is not a regular file: what the store reads
// is the shell's own document, never whatever the name leads to.
func TestTheStoreRefusesALinkAtAShellDocumentsName(t *testing.T) {
	documents := ShellDocuments{Folder: t.TempDir()}
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.json")
	if err := os.WriteFile(victim, []byte(`{"led":"to"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, documents.path(recentName)); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.read(recentName, maxRecentBytes); !errors.Is(err, errNotADocument) {
		t.Fatalf("a link was read through: %v", err)
	}
	if led, err := os.ReadFile(victim); err != nil || string(led) != `{"led":"to"}` {
		t.Fatalf("reading the link disturbed the document it led to: %q %v", led, err)
	}
	if err := os.Remove(documents.path(recentName)); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(documents.path(recentName), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := documents.read(recentName, maxRecentBytes); !errors.Is(err, errNotADocument) {
		t.Fatalf("a FIFO was read, blocking or otherwise: %v", err)
	}
}

// A document this account cannot read is reported as the permission refusal
// it is, and a store this account cannot write refuses the replacement — the
// two error classes every document's own sentence is chosen from.
func TestTheStoreSeparatesReadPermissionFromWritePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses permissions")
	}
	documents := ShellDocuments{Folder: t.TempDir()}
	if err := documents.write(draftsName, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(documents.path(draftsName), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(documents.path(draftsName), 0600) })
	if _, err := documents.read(draftsName, maxDraftsBytes); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("an unreadable document read as %v", err)
	}
	os.Chmod(documents.path(draftsName), 0600)
	if err := os.Chmod(documents.Folder, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(documents.Folder, 0700) })
	if err := documents.write(filtersName, []byte("{}\n")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("an unwritable store accepted a replacement as %v", err)
	}
}

// A replacement the disk refused is a returned error, never a silent loss:
// what was retained before it stays retained, and the interrupted attempt
// leaves nothing beside it. The disk is full in the one way a test can make it
// full portably — the account's own file size limit — and the limited write
// runs in a child process, because lowering the limit in this process would
// also starve the test framework's own log writes.
func TestTheStoreSeparatesADiskRefusalFromSuccess(t *testing.T) {
	if os.Getenv(storeDiskChild) == "1" {
		storeDiskChildRun(t)
		return
	}
	documents := ShellDocuments{Folder: t.TempDir()}
	before := []byte("{}\n")
	if err := documents.write(sessionName, before); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), storeDiskChild+"=1", storeDiskFolder+"="+documents.Folder)
	output, _ := child.CombinedOutput()
	if !strings.Contains(string(output), "RESULT:"+strconv.Itoa(1)) {
		t.Fatalf("the child did not report a refused write: %s", output)
	}
	if _, err := os.Stat(documents.path(sessionName) + ".incomplete"); !os.IsNotExist(err) {
		t.Fatalf("a refused write left an interrupted attempt beside the document: %v", err)
	}
	after, err := os.ReadFile(documents.path(sessionName))
	if err != nil || string(after) != string(before) {
		t.Fatalf("a refused write changed the retained document: %q", after)
	}
}

const (
	storeDiskChild  = "READMIT_DESKTOP_STORE_DISK_CHILD"
	storeDiskFolder = "READMIT_DESKTOP_STORE_DISK_FOLDER"
)

// storeDiskChildRun performs one store write well inside the document's own
// bound but past the account's file size limit, so the kernel refuses the
// whole document rather than a part of it, and reports the result for the
// parent to assert on. The folder is only ever written before the limit
// drops; the refused replacement must leave the parent's document as it was.
func storeDiskChildRun(t *testing.T) {
	t.Helper()
	documents := ShellDocuments{Folder: os.Getenv(storeDiskFolder)}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Print("RESULT:0\n")
		return
	}
	limit.Cur = 4096
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Print("RESULT:0\n")
		return
	}
	err := documents.write(sessionName, []byte(`{"session":`+strings.Repeat("x", 8192)+`}`+"\n"))
	if err == nil {
		fmt.Print("RESULT:0\n")
		return
	}
	fmt.Print("RESULT:1\n")
}

// A kill during a stream of replacements is the interruption the store exists
// to survive, and this is tested once, here, against the store itself: every
// replacement the child answered before the kill has come back whole, never
// torn, never mixed with another, and never older than one already
// acknowledged. An unacknowledged replacement may come back whole or not at
// all. A kill between staging the replacement and installing it retains the
// interrupted attempt beside the document; it is reported rather than reused,
// and the acknowledged document stays exactly as it came back.
func TestAKillDuringAReplacementLeavesAcknowledgedWorkWhole(t *testing.T) {
	if os.Getenv(storeKillChild) == "1" {
		storeKillLoop(t)
		return
	}
	folder := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), storeKillChild+"=1", storeKillFolder+"="+folder)
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
	for acknowledged < 64 && lines.Scan() {
		revision, ok := strings.CutPrefix(lines.Text(), "ACK ")
		if !ok {
			other = lines.Text()
			continue
		}
		if acknowledged, err = strconv.Atoi(revision); err != nil {
			t.Fatalf("the child acknowledged %q", lines.Text())
		}
	}
	if acknowledged < 64 {
		t.Fatalf("the child acknowledged only %d replacements before it stopped: %s", acknowledged, other)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// Every acknowledgement the child printed before it died is still in the
	// pipe; the newest of them is the replacement that must come back.
	for lines.Scan() {
		if revision, ok := strings.CutPrefix(lines.Text(), "ACK "); ok {
			if later, err := strconv.Atoi(revision); err == nil && later > acknowledged {
				acknowledged = later
			}
		}
	}
	child.Wait()
	killed = true

	documents := ShellDocuments{Folder: folder}
	data, err := documents.read(sessionName, maxSessionBytes)
	if err != nil {
		t.Fatalf("a kill during a replacement left an unreadable document: %v", err)
	}
	body := string(data)
	match := storeKillRevision.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("a kill during a replacement left a torn document: %q", body)
	}
	revision, _ := strconv.Atoi(match[1])
	if len(match[2]) != revision%512 {
		t.Fatalf("a kill during a replacement mixed two documents: %q", body)
	}
	if revision < acknowledged {
		t.Fatalf("revision %d was acknowledged before the kill, but revision %d came back", acknowledged, revision)
	}

	// Retention continues once the store can write again. Where the kill
	// landed is not chosen here: a leftover interrupted attempt refuses the
	// next replacement, and is kept, until it is removed.
	next := storeKillDocument(revision + 1)
	nextErr := documents.write(sessionName, next)
	if _, err := os.Stat(documents.path(sessionName) + ".incomplete"); err == nil {
		if nextErr == nil {
			t.Fatal("an interrupted replacement was reused")
		}
	} else if nextErr != nil {
		t.Fatalf("retention did not continue after the kill: %v", nextErr)
	}
	if nextErr == nil {
		if kept, err := documents.read(sessionName, maxSessionBytes); err != nil || string(kept) != string(next) {
			t.Fatalf("retention after the kill stored %q (%v)", kept, err)
		}
	}
}

// storeKillRevision matches exactly one whole replacement the child wrote: a
// revision number and a body whose length only that revision produces.
var storeKillRevision = regexp.MustCompile(`\A\{"revision":([0-9]+),"body":"(x*)"\}\n\z`)

const storeKillChild = "READMIT_DESKTOP_STORE_KILL_CHILD"
const storeKillFolder = "READMIT_DESKTOP_STORE_KILL_FOLDER"

// storeKillDocument is one whole replacement: a revision number and a body
// whose length only that revision produces, so a torn or mixed write cannot
// read back as any whole document.
func storeKillDocument(revision int) []byte {
	return []byte(`{"revision":` + strconv.Itoa(revision) + `,"body":"` + strings.Repeat("x", revision%512) + `"}` + "\n")
}

// storeKillLoop replaces the document as fast as it can and acknowledges each
// replacement only once the store answered, until it is killed.
func storeKillLoop(t *testing.T) {
	t.Helper()
	documents := ShellDocuments{Folder: os.Getenv(storeKillFolder)}
	for revision := 1; ; revision++ {
		if err := documents.write(sessionName, storeKillDocument(revision)); err != nil {
			t.Fatalf("the child could not replace its document: %v", err)
		}
		fmt.Printf("ACK %d\n", revision)
	}
}
