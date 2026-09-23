package artifactdir_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

func TestIdentityUsesDomainAndLengthDelimitedSortedFiles(t *testing.T) {
	files := map[string][]byte{
		"z.bin":           {0x00, 0xff},
		"manifest.json":   []byte("{}\n"),
		"identity.sha256": []byte("completion marker is never identity input\n"),
	}
	const want = "49ab70cd4ae408257f176dcccc1d195fbc02aca83cbc53757b497f09b6dbf52d"
	if got := artifactdir.Identity("readmit-example/v1", files); got != want {
		t.Fatalf("identity = %s, want %s", got, want)
	}
}

func TestWriteFileIsExclusiveAndReadRefusesUnexpectedOrLinkedFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("payloads", 0700); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "manifest.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "payloads/one.bin", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "manifest.json", []byte("changed")); err == nil {
		t.Fatal("exclusive write replaced an existing file")
	}

	layout := artifactdir.Layout{
		Noun:               "example",
		AllowedDirectories: []string{"payloads"},
		RequiredFiles:      []string{"manifest.json"},
		AllowFile: func(name string) bool {
			return name == "manifest.json" || name == "payloads/one.bin"
		},
		MaxFiles:     3,
		MaxFileBytes: 8,
		MaxBytes:     16,
	}
	files, err := artifactdir.Read(dir, layout)
	if err != nil || string(files["payloads/one.bin"]) != "one" {
		t.Fatalf("read: files=%v err=%v", files, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactdir.Read(dir, layout); err == nil {
		t.Fatal("unexpected file accepted")
	}
	if err := os.Remove(filepath.Join(dir, "unexpected")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("manifest.json", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	layout.AllowFile = func(name string) bool {
		return name == "manifest.json" || name == "payloads/one.bin" || name == "linked"
	}
	if _, err := artifactdir.Read(dir, layout); err == nil {
		t.Fatal("symbolic link accepted")
	}
}

func TestWriteCreatesIdentityLastArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	files := map[string][]byte{"manifest.json": []byte("{}\n"), "payloads/one.bin": []byte("one")}
	identity, err := artifactdir.Write(path, artifactdir.WriteOptions{Domain: "readmit-example/v1", Directories: []string{"payloads"}}, files)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil || string(marker) != identity+"\n" {
		t.Fatalf("marker=%q err=%v", marker, err)
	}
	if _, err := artifactdir.Write(path, artifactdir.WriteOptions{Domain: "readmit-example/v1"}, files); err == nil {
		t.Fatal("existing artifact overwritten")
	}
}

// countdown is a context that is cancelled after a fixed number of checks, so
// a test can stop a write between two files without racing it. A write asks
// Err between files and never waits on Done, so Err is all this answers.
type countdown struct {
	context.Context
	checks int
}

func (c *countdown) Err() error {
	if c.checks == 0 {
		return context.Canceled
	}
	c.checks--
	return nil
}

// A write is a long sequence of synced files, so a cancellation is observed
// between them rather than only once all of them are written. What was written
// stays exactly as written and carries no completion marker, so every reader
// refuses it; a cancellation that arrives before anything is created creates
// nothing.
func TestWriteContextStopsBetweenFilesAndRetainsNoCompletionMarker(t *testing.T) {
	files := map[string][]byte{"manifest.json": []byte("{}\n"), "payloads/a.bin": []byte("a"), "payloads/b.bin": []byte("b"), "payloads/c.bin": []byte("c")}
	options := artifactdir.WriteOptions{Domain: "readmit-example/v1", Directories: []string{"payloads"}}

	before := filepath.Join(t.TempDir(), "before")
	if _, err := artifactdir.WriteContext(&countdown{Context: context.Background()}, before, options, files); !errors.Is(err, artifactdir.ErrCancelled) {
		t.Fatalf("a write cancelled before it began answered %v", err)
	}
	if _, err := os.Lstat(before); !os.IsNotExist(err) {
		t.Fatalf("a write cancelled before it began created its destination: %v", err)
	}

	partway := filepath.Join(t.TempDir(), "partway")
	if _, err := artifactdir.WriteContext(&countdown{Context: context.Background(), checks: 3}, partway, options, files); !errors.Is(err, artifactdir.ErrCancelled) {
		t.Fatalf("a write cancelled part way answered %v", err)
	}
	if _, err := os.Lstat(filepath.Join(partway, "identity.sha256")); !os.IsNotExist(err) {
		t.Fatalf("a write cancelled part way wrote its completion marker: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(partway, "manifest.json"))
	if err != nil || string(written) != "{}\n" {
		t.Fatalf("what a cancelled write had written is not retained as written: %q %v", written, err)
	}
	if _, err := os.Lstat(filepath.Join(partway, "payloads", "c.bin")); !os.IsNotExist(err) {
		t.Fatalf("a write kept writing after it was cancelled: %v", err)
	}
	if _, err := artifactdir.Write(partway, options, files); err == nil {
		t.Fatal("an incomplete artifact was overwritten")
	}
}

type recordingFile struct {
	written []byte
	n       int
	err     error
	syncs   int
	syncErr error
	closed  int
}

func (f *recordingFile) Write(p []byte) (int, error) { return f.n, f.err }
func (f *recordingFile) Sync() error                 { f.syncs++; return f.syncErr }
func (f *recordingFile) Close() error                { f.closed++; return nil }

func TestWriteFileSyncChecksForAShortWriteBeforeSyncing(t *testing.T) {
	short := &recordingFile{n: 2}
	if err := artifactdir.WriteFileSync(short, []byte("abcd")); err == nil {
		t.Fatal("a short write reported as complete")
	}
	if short.syncs != 0 {
		t.Fatal("synced after a short write")
	}
	if short.closed != 0 {
		t.Fatal("WriteFileSync closed the caller's file")
	}
	full := &recordingFile{n: 4}
	if err := artifactdir.WriteFileSync(full, []byte("abcd")); err != nil {
		t.Fatalf("full write refused: %v", err)
	}
	if full.syncs != 1 {
		t.Fatal("a full write was not synced")
	}
	failing := &recordingFile{n: 4, syncErr: os.ErrInvalid}
	if err := artifactdir.WriteFileSync(failing, []byte("abcd")); err == nil {
		t.Fatal("a failed sync reported as complete")
	}
}

func TestPublishReplacesACompleteRecordAndRefusesAStaleIncomplete(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := artifactdir.Publish(root, ".record.incomplete", "record.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(".record.incomplete"); err == nil {
		t.Fatal("incomplete file retained after a completed publish")
	}
	data, err := root.OpenFile("record.json", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	read, err := io.ReadAll(data)
	if err != nil || string(read) != "{}\n" {
		t.Fatalf("record.json = %q, %v", read, err)
	}
	stale, err := root.OpenFile(".record.incomplete", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	stale.Close()
	err = artifactdir.Publish(root, ".record.incomplete", "other.json", []byte("{}\n"))
	if !errors.Is(err, artifactdir.ErrCreateFile) {
		t.Fatalf("publish over a stale incomplete file = %v", err)
	}
	if _, err := root.Stat("other.json"); err == nil {
		t.Fatal("a refused publish still renamed its record into place")
	}
}

func TestPublishRetainsTheIncompleteFileWhenTheRenameFails(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := artifactdir.Publish(root, ".record.incomplete", "missing/record.json", []byte("{}\n")); err == nil {
		t.Fatal("a rename into a missing directory reported as complete")
	}
	if _, err := root.Stat(".record.incomplete"); err != nil {
		t.Fatal("incomplete file removed when the caller owns the removal policy")
	}
}
