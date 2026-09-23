package artifactdir_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
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

// caseFolder is a new folder to write a case into, resolved as the writer
// resolves its destination, so it names the directories the writer syncs.
func caseFolder(t *testing.T) string {
	t.Helper()
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return folder
}

// writeCase writes a case of eight occurrences, one payload file each, and
// answers what bundle.Write answered.
func writeCase(t *testing.T, path string) (*bundle.Bundle, error) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/case-evidence.mllp")
	if err != nil {
		t.Fatal(err)
	}
	importedAt := time.Date(2030, 3, 4, 5, 6, 7, 0, time.UTC)
	return bundle.Write(path, []bundle.Input{{Path: "/evidence/case-evidence.mllp", Data: raw}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
}

// A case is found through names: its own in the folder that holds it, those of
// its files and of payloads/ in the case directory, and each payload's in
// payloads/. A name survives a power loss only once the directory holding it
// is synced after the name was made. So before the write reports the case
// written, each of those directories is synced holding every name it will
// hold; a directory synced too early, or not at all, would lose a name.
func TestACaseIsReportedWrittenOnlyOnceEveryDirectoryEntryItDependsOnIsSynced(t *testing.T) {
	synced := map[string][]string{}
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		synced[directory] = nil
		for _, entry := range entries {
			synced[directory] = append(synced[directory], entry.Name())
		}
		return nil
	}))
	folder := caseFolder(t)
	path := filepath.Join(folder, "case")
	written, err := writeCase(t, path)
	if err != nil {
		t.Fatal(err)
	}

	depends := map[string][]string{folder: {"case"}}
	err = filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == path {
			return err
		}
		directory := filepath.Dir(name)
		depends[directory] = append(depends[directory], entry.Name())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if payloads := depends[filepath.Join(path, "payloads")]; len(payloads) != len(written.Events) {
		t.Fatalf("the case holds %d payload files for %d occurrences", len(payloads), len(written.Events))
	}
	if len(depends) != 3 {
		t.Fatalf("the case has entries in %d directories, want its folder, itself and payloads/", len(depends))
	}
	for directory, names := range depends {
		for _, name := range names {
			if !slices.Contains(synced[directory], name) {
				relative, _ := filepath.Rel(folder, filepath.Join(directory, name))
				t.Errorf("the case was reported written before the entry naming %s was synced", filepath.ToSlash(relative))
			}
		}
	}
	if _, err := bundle.Open(path); err != nil {
		t.Fatalf("the synced case does not open: %v", err)
	}
}

// A write that cannot sync one of those directories never reports the case
// written.
func TestACaseWhoseDirectoryEntriesCannotBeSyncedIsNeverReportedWritten(t *testing.T) {
	for _, failing := range []struct {
		name      string
		directory func(folder string) string
	}{
		{"payloads", func(folder string) string { return filepath.Join(folder, "case", "payloads") }},
		{"case", func(folder string) string { return filepath.Join(folder, "case") }},
		{"folder", func(folder string) string { return folder }},
	} {
		t.Run(failing.name, func(t *testing.T) {
			folder := caseFolder(t)
			unsyncable := failing.directory(folder)
			t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
				if directory == unsyncable {
					return errors.New("injected directory sync failure")
				}
				return nil
			}))
			if written, err := writeCase(t, filepath.Join(folder, "case")); err == nil {
				t.Fatalf("a case whose %s entries were not synced was reported written as %s", failing.name, written.Identity)
			}
		})
	}
}

// The folder that holds a case is synced last, so a folder the writer can
// create in but cannot open is refused before anything is written, rather
// than after a complete case is left behind that the write could not confirm.
func TestACaseIsRefusedBeforeWritingIntoAFolderItCannotSync(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder its owner can create in but not open")
	}
	folder := caseFolder(t)
	if err := os.Chmod(folder, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(folder, 0700) })
	path := filepath.Join(folder, "case")
	if written, err := writeCase(t, path); err == nil {
		t.Fatalf("a case was reported written into a folder that cannot be synced, as %s", written.Identity)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused write left something behind: %v", err)
	}
}
